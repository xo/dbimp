package libsql

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/xo/dbimp"
)

// The endpoints of Hrana over HTTP (D149).
const (
	// pathCursor streams the rows of a batch as lines of JSON.
	pathCursor = "/v3/cursor"
	// pathPipeline runs requests on a stream, and answers them whole.
	pathPipeline = "/v3/pipeline"
)

// closeTimeout bounds the request that closes a stream after a cursor, as
// D123 bounds a request for Databend (D149).
const closeTimeout = 5 * time.Second

// Connector opens connections to one server. It owns its transport, and every
// connection shares it. A caller can build one from a Config and open it with
// sql.OpenDB.
type Connector struct {
	cfg       Config
	base      *url.URL
	transport *http.Transport
	client    *http.Client
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg, and fills each field that is zero with its default.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if !cfg.NoTLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Auth == "" {
		cfg.Auth = AuthBearer
	}
	t := dbimp.NewTransport(tlsCfg)
	base, _ := url.Parse(cfg.baseURL())
	return &Connector{
		cfg:       cfg,
		base:      base,
		transport: t,
		// The driver follows no redirect, so the token goes to the host of
		// the DSN only (D151).
		client: dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. A connection holds nothing on the
// server until a transaction opens a stream, so it sends no request.
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

// stream is a stream of Hrana: the baton of its last answer, or "" before
// its first request, the URL that its requests go to, and its namespace.
type stream struct {
	baton     string
	base      *url.URL
	namespace string
}

// newStream returns a stream that has sent no request, in namespace.
func (c *Connector) newStream(namespace string) *stream {
	return &stream{base: c.base, namespace: namespace}
}

// batonJSON returns the baton of s as JSON: a string, or null for a new
// stream.
func (s *stream) batonJSON() any {
	if s.baton == "" {
		return nil
	}
	return s.baton
}

// answer takes the baton and the base_url of an answer. A base_url on another
// scheme, host or port than the DSN is an error, so the token never leaves
// the host of the DSN (D151).
func (s *stream) answer(baton, baseURL string) error {
	s.baton = baton
	if baseURL == "" {
		return nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("reading the base_url %q: %w", baseURL, dbimp.ErrInvalidValue)
	}
	if u.Scheme != s.base.Scheme || u.Hostname() != s.base.Hostname() || portOf(u) != portOf(s.base) {
		return fmt.Errorf("following the base_url %s: it names another host than the DSN (D151): %w", u.Redacted(), dbimp.ErrNotSupported)
	}
	s.base = u
	return nil
}

// post sends POST path with body to the URL of s, and returns a response
// with a 2xx status. Any other response is an error, and its body is closed.
// An error of a statement arrives with HTTP 200, in the body (measured).
func (c *Connector) post(ctx context.Context, s *stream, path string, body []byte) (*http.Response, error) {
	u := s.base.JoinPath(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.namespace != "" {
		req.Header.Set("X-Namespace", s.namespace)
	}
	dbimp.SetAuth(req, c.cfg.Auth, "Bearer", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	return res, nil
}

// portOf returns the port of u, or the port of its scheme when it names
// none.
func portOf(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
