package tablestore

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net/http"
	"time"

	"github.com/xo/dbimp"
)

// Connector opens connections to one endpoint. It owns its transport, and every
// connection shares it. A caller can build one from a Config and open it with
// sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client

	// now returns the time of a signature, and time.Now by default. The tests set
	// it.
	now func() time.Time
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of cfg.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the signature goes to the host of the
		// DSN only (D222).
		client: dbimp.NewClient(t, false),
		now:    time.Now,
	}
}

// Connect satisfies driver.Connector. A connection holds nothing on the server,
// so it sends no request. It fails when the Config has no credentials, because the
// driver reads none from the environment (D7).
func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	if c.cfg.User == "" || c.cfg.Password == "" {
		return nil, fmt.Errorf("opening a connection: %w", ErrNoCredentials)
	}
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

// call sends POST /<action> with the protobuf message body, signs it, and returns
// a response with a 2xx status. Any other response is an error, and its body is
// closed. The connection of a statement that reached the server can break before
// the answer, and then the statement can have run, so only an error before the
// request reached the server is driver.ErrBadConn (D8 and dbimp.Send).
func (c *Connector) call(ctx context.Context, action string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/"+action, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	sign(req, body, c.cfg.User, c.cfg.Password, c.cfg.Instance, c.now())
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	return res, nil
}
