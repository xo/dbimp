package dynamodb

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// targetPrefix starts the name of each operation, in the header X-Amz-Target
// (recorded).
const targetPrefix = "DynamoDB_20120810."

// Connector opens connections to one endpoint. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open
// it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
	// now returns the time of a signature. It is time.Now, except in a test.
	now func() time.Time
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg.
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
		// The driver follows no redirect, so the signature goes to the host
		// of the DSN only (D169).
		client: dbimp.NewClient(t, false),
		now:    time.Now,
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

// call sends POST / with the operation op, signs it, and returns a response
// with a 2xx status and the content type of DynamoDB. Any other response is
// an error, and its body is closed.
func (c *Connector) call(ctx context.Context, op string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", targetPrefix+op)
	c.cfg.sign(req, body, c.now())
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/x-amz-json") && !strings.HasPrefix(ct, "application/json") {
		_ = res.Body.Close()
		return nil, &Error{HTTPStatus: res.StatusCode, Message: "the answer is " + ct + " and not JSON"}
	}
	return res, nil
}

// sign sets the session token of cfg, when it has one, and signs req, whose
// body is body, so that the signature covers the header X-Amz-Security-Token.
// No error holds the token (D94).
func (cfg *Config) sign(req *http.Request, body []byte, now time.Time) {
	if cfg.Token != "" {
		req.Header.Set(securityHeader, cfg.Token)
	}
	dbimp.SignV4(req, body, cfg.User, cfg.Password, cfg.Region, service, now)
}

// securityHeader is the header that carries the session token of temporary
// credentials.
const securityHeader = "X-Amz-Security-Token"
