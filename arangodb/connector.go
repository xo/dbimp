package arangodb

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xo/dbimp"
)

// Connector opens connections to one database. It owns its transport, and
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
	if cfg.Database == "" {
		cfg.Database = defaultDatabase
	}
	if cfg.Cancel == "" {
		cfg.Cancel = CancelTag
	}
	if cfg.Batch == 0 {
		cfg.Batch = defaultBatch
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

// Connect satisfies driver.Connector. A connection holds no state on the
// server, except a transaction while one is open. Each connection has a
// random id, which names its queries for cancel=tag (D90).
func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	return &conn{c: c, id: rand.Text()}, nil
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

// auth sets the credentials on req, as the key auth says (D94).
func (c *Connector) auth(req *http.Request) {
	dbimp.SetAuth(req, c.cfg.Auth, "Bearer", c.cfg.User, c.cfg.Password)
}

// do sends one request to path, under the API of the database, with body
// written as JSON when it is not nil, and with the header x-arango-trx-id
// when trx is not "". It returns a response of JSON with a 2xx status. Any
// other response is an error, and its body is closed.
func (c *Connector) do(ctx context.Context, method, path string, body any, trx string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("writing the request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if trx != "" {
		req.Header.Set("X-Arango-Trx-Id", trx)
	}
	c.auth(req)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		_ = res.Body.Close()
		return nil, &Error{HTTPStatus: http.StatusBadGateway, Message: "the answer is " + ct + " and not JSON"}
	}
	return res, nil
}

// call sends one request, and decodes its answer into out, which is small,
// such as the list of the collections.
func (c *Connector) call(ctx context.Context, method, path string, body, out any, trx string) error {
	res, err := c.do(ctx, method, path, body, trx)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		if _, err := io.Copy(io.Discard, res.Body); err != nil {
			return fmt.Errorf("reading the answer to %s %s: %w", method, path, err)
		}
		return nil
	}
	if err := json.UnmarshalRead(res.Body, out); err != nil {
		return fmt.Errorf("reading the answer to %s %s: %w", method, path, err)
	}
	return nil
}
