package rqlite

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/xo/dbimp"
)

// The endpoints of the HTTP API (D142).
const (
	// pathRequest runs reads and writes. A query goes there.
	pathRequest = "/db/request"
	// pathExecute runs writes. Exec goes there.
	pathExecute = "/db/execute"
	// pathQuery runs reads on a read-only connection. A statement with
	// WithReadonly(true) goes there.
	pathQuery = "/db/query"
)

// Connector opens connections to one node. It owns its transport, and every
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
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the credentials go to the host
		// of the DSN only (D141).
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

// send sends POST path?keys with body, and returns a response of JSON with a
// 2xx status. Any other response is an error, and its body is closed. An
// error of SQL arrives with HTTP 200, in the body (measured).
func (c *Connector) send(ctx context.Context, path string, keys url.Values, body []byte) (*http.Response, error) {
	u := c.base + path
	if len(keys) > 0 {
		u += "?" + encodeKeys(keys)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
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

// encodeKeys writes keys as the query of a URL. A key with an empty value is
// a flag, which the server reads by its presence (measured), so it goes with
// no "=".
func encodeKeys(keys url.Values) string {
	var b strings.Builder
	for _, k := range sortedKeys(keys) {
		for _, v := range keys[k] {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			if v != "" {
				b.WriteByte('=')
				b.WriteString(url.QueryEscape(v))
			}
		}
	}
	return b.String()
}
