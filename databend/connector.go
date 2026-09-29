package databend

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that kills a query on the server after its
// context ended, as D67 bounds it for Neo4j (D123).
const stopTimeout = 5 * time.Second

// Connector opens connections to one database. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open
// it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client

	// mu guards zone, the timezone of the server, which the connector reads
	// once, for a Timestamp in a session that names no timezone.
	mu   sync.Mutex
	zone *time.Location
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg, and fills each field that is zero with its default.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Database == "" {
		cfg.Database = defaultDatabase
	}
	if cfg.Cancel == "" {
		cfg.Cancel = CancelKill
	}
	if cfg.Auth == "" {
		cfg.Auth = AuthBasic
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		client:    dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. A connection holds its session, which
// starts as the session of the DSN (D122).
func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	return &conn{c: c}, nil
}

// Driver satisfies driver.Connector.
func (c *Connector) Driver() driver.Driver {
	return Driver{}
}

// Close closes the idle connections of the transport.
func (c *Connector) Close() error {
	c.transport.CloseIdleConnections()
	return nil
}

// do sends one request to path, with body as JSON when it is not nil, and
// with the headers h. It returns a response of JSON with a 2xx status. Any
// other response is an error, and its body is closed.
func (c *Connector) do(ctx context.Context, method, path string, body []byte, h http.Header) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	maps.Copy(req.Header, h)
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	dbimp.SetAuth(req, c.cfg.Auth, "Bearer", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		_ = res.Body.Close()
		return nil, &Error{HTTPStatus: http.StatusBadGateway, Code: http.StatusBadGateway, Message: "the answer is " + ct + " and not JSON"}
	}
	return res, nil
}

// get sends GET to uri, a path that a response named, and closes the answer,
// for the kill and the final URIs, whose answer holds nothing that the
// driver reads.
func (c *Connector) get(ctx context.Context, uri string, h http.Header) error {
	res, err := c.do(ctx, http.MethodGet, uri, nil, h)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		return fmt.Errorf("reading the answer to %s: %w", uri, err)
	}
	return nil
}

// stop sends GET to uri, the kill or the final URI of a query, with ctx
// without its end and the limit of a stop, because ctx can have ended
// (D123).
func (c *Connector) stop(ctx context.Context, uri string, h http.Header) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	return c.get(ctx, uri, h)
}
