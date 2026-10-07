package solr

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/xo/dbimp"
)

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
	if cfg.Mode == "" {
		cfg.Mode = ModeFacet
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the credentials go to the host
		// of the DSN only (D166).
		client: dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. A connection holds only the schemas
// that it read, so it sends no request.
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

// send sends a request with the credentials of the DSN, and returns a
// response with a 2xx status. Any other response is an error, and its body is
// closed.
func (c *Connector) send(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	return res, nil
}

// query sends POST /solr/<collection>/sql with form, and returns a response
// of JSON with a 2xx status.
func (c *Connector) query(ctx context.Context, collection string, form url.Values) (*http.Response, error) {
	res, err := c.send(ctx, http.MethodPost, "/solr/"+url.PathEscape(collection)+"/sql", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		_ = res.Body.Close()
		return nil, &Error{HTTPStatus: res.StatusCode, Message: "the answer is " + ct + " and not JSON"}
	}
	return res, nil
}

// luke sends GET /solr/<collection>/admin/luke, which names each field of the
// schema and its type, and returns the response. Both principals can read it
// (measured).
func (c *Connector) luke(ctx context.Context, collection string) (*http.Response, error) {
	q := url.Values{"show": {"schema"}, "numTerms": {"0"}}
	return c.send(ctx, http.MethodGet, "/solr/"+url.PathEscape(collection)+"/admin/luke", q, nil, "")
}
