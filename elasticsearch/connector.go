package elasticsearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that closes a cursor after its context
// ended, as D67 bounds it for Neo4j and D133 for Pinot (D167).
const stopTimeout = 5 * time.Second

// maxStopAnswer is the most of the answer to a close that the driver reads.
// The answer is one small object (measured).
const maxStopAnswer = 64 << 10

// Connector opens connections to one server. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open
// it with sql.OpenDB.
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
	if cfg.Auth == "" {
		cfg.Auth = AuthBasic
	}
	if cfg.FetchSize == 0 {
		cfg.FetchSize = defaultFetchSize
	}
	if cfg.TimeZone == "" {
		cfg.TimeZone = defaultTimeZone
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the credentials go to the host
		// of the DSN only (D167).
		client: dbimp.NewClient(t, false),
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

// post sends POST path?format=json with body, and returns a response of JSON
// with a 2xx status. Any other response is an error, and its body is closed.
// If first is true, the request is the start of a statement, and an error
// that comes before the connection was made wraps driver.ErrBadConn, so that
// database/sql tries another connection. A later request never does, because
// the statement reached the server and database/sql would run it again (D8).
func (c *Connector) post(ctx context.Context, path string, body []byte, first bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path+"?format=json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.setAuth(req)
	var res *http.Response
	if first {
		res, err = dbimp.Send(c.client, req)
	} else {
		res, err = c.client.Do(req)
		if err != nil {
			err = fmt.Errorf("sending the request: %w", err)
		}
	}
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		_ = res.Body.Close()
		return nil, &Error{HTTPStatus: res.StatusCode, Reason: "the answer is " + ct + " and not JSON"}
	}
	return res, nil
}

// setAuth sets the credentials on req, as the key auth says (D94 and D167).
func (c *Connector) setAuth(req *http.Request) {
	if c.cfg.Auth == AuthAPIKey {
		dbimp.SetAuth(req, dbimp.AuthBearer, "ApiKey", c.cfg.User, c.cfg.Password)
		return
	}
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
}

// stop sends POST /_sql/close, which closes the cursor, with ctx without its
// end and the limit of a stop, because ctx can have ended (D167). The server
// answers {"succeeded": true}, and {"succeeded": false} for a cursor that is
// closed already (measured), and neither is an error.
func (c *Connector) stop(ctx context.Context, cursor string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	body, err := dbimp.MarshalParams(request{Cursor: cursor}, nil)
	if err != nil {
		return fmt.Errorf("writing the request to close the cursor: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/_sql/close", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("making the request to close the cursor: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.setAuth(req)
	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("closing the cursor: %w", err)
	}
	if err := checkStatus(res); err != nil {
		return fmt.Errorf("closing the cursor: %w", err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxStopAnswer)); err != nil {
		return fmt.Errorf("reading the answer to the close of the cursor: %w", err)
	}
	return nil
}
