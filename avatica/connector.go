package avatica

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// closeTimeout bounds a request that the driver sends after the context of a
// statement ended, such as closeStatement, as D123 bounds one for Databend
// (D159).
const closeTimeout = 5 * time.Second

// Connector opens connections to one server. It owns its transport, and every
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
	if cfg.Auth == "" {
		cfg.Auth = AuthNone
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		client:    dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. It opens a connection of Avatica with
// an id of its own, and sets autoCommit on, because the Phoenix Query Server
// opens one with it off (D157).
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	id := "dbimp-" + strings.ToLower(rand.Text())
	info := map[string]any{}
	if c.cfg.User != "" {
		info["user"] = c.cfg.User
	}
	if c.cfg.Password != "" {
		info["password"] = c.cfg.Password
	}
	if _, err := c.call(ctx, map[string]any{"request": "openConnection", "connectionId": id, "info": info}); err != nil {
		return nil, fmt.Errorf("opening a connection: %w", err)
	}
	cn := &conn{c: c, id: id, ctx: context.WithoutCancel(ctx)}
	p, err := cn.sync(ctx, connProps{AutoCommit: new(true)})
	if err != nil {
		return nil, errors.Join(err, cn.close(ctx))
	}
	if p.ReadOnly != nil {
		cn.readOnly = *p.ReadOnly
	}
	if p.TransactionIsolation != nil {
		cn.isolation = *p.TransactionIsolation
	}
	return cn, nil
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

// post sends the request req, and returns a response with a 2xx status. Any
// other response is an error, and its body is closed. The body goes with
// each character outside ASCII escaped (D158).
func (c *Connector) post(ctx context.Context, req map[string]any) (*http.Response, error) {
	b, err := json.Marshal(req, json.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base, bytes.NewReader(ascii(b)))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	hr.Header.Set("Content-Type", "application/json")
	if c.cfg.Auth == AuthBasic {
		dbimp.SetAuth(hr, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	}
	res, err := dbimp.Send(c.client, hr)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	return res, nil
}

// call sends req, and returns the whole answer, which is small, for a call
// that holds no rows, such as openConnection or commit.
func (c *Connector) call(ctx context.Context, req map[string]any) (map[string]any, error) {
	res, err := c.post(ctx, req)
	if err != nil {
		return nil, err
	}
	st := dbimp.NewStream(res.Body)
	defer st.Close()
	v, err := st.Decoder().ReadValue()
	if err != nil {
		return nil, fmt.Errorf("reading the answer to %v: %w", req["request"], err)
	}
	var m map[string]any
	if err := json.Unmarshal(v, &m); err != nil {
		return nil, fmt.Errorf("reading the answer to %v: %w", req["request"], err)
	}
	if err := st.End(); err != nil {
		return nil, err
	}
	return m, nil
}

// merge returns req with the keys of params, which replace those of req.
func merge(req, params map[string]any) map[string]any {
	maps.Copy(req, params)
	return req
}
