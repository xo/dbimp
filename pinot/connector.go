package pinot

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a query after its context
// ended, as D67 bounds it for Neo4j and D123 for Databend (D133).
const stopTimeout = 5 * time.Second

// Connector opens connections to one Broker. It owns its transport, and every
// connection shares it. A caller can build one from a Config and open it with
// sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
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
	if cfg.Cancel == "" {
		cfg.Cancel = CancelKill
	}
	if cfg.Engine == "" {
		cfg.Engine = EngineMulti
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

// Connect satisfies driver.Connector. A connection holds nothing on the
// server, so it sends no request.
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

// query sends POST /query/sql with body, and returns a response of JSON with
// a 2xx status. Any other response is an error, and its body is closed. An
// error of the query itself arrives with HTTP 200, in the body (measured).
func (c *Connector) query(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/query/sql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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

// stop sends DELETE /query/<id>?client=true, which cancels the query that
// the driver named id, with ctx without its end and the limit of a stop,
// because ctx ended (D133). The answer is text, such as "Cancelled client
// query: <id>", and HTTP 404 for a query that already ended (measured).
func (c *Connector) stop(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/query/"+url.PathEscape(id)+"?client=true", nil)
	if err != nil {
		return fmt.Errorf("making the request to cancel the query %s: %w", id, err)
	}
	dbimp.SetAuth(req, c.cfg.Auth, "Bearer", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return fmt.Errorf("cancelling the query %s: %w", id, err)
	}
	if err := dbimp.CheckStatus(res); err != nil {
		return fmt.Errorf("cancelling the query %s: %w", id, err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		return fmt.Errorf("reading the answer to the cancel of %s: %w", id, err)
	}
	return nil
}
