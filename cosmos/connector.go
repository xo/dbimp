package cosmos

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/xo/dbimp"
)

// Connector opens connections to one account. It owns its transport, and
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
		// The emulator makes a certificate for itself, so a caller that sets
		// Insecure accepts it (D190). The default verifies the certificate.
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.Insecure} //nolint:gosec // D190 lets a caller accept the certificate of the emulator, and only a caller that asks.
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the signature goes to the host
		// of the DSN only (D190).
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

// query is one query of a statement: the container that it reads, the body,
// and the headers that its options set.
type query struct {
	database  string
	container string
	body      []byte
	pageSize  int
	key       string
	hasKey    bool
}

// path returns the path of the documents of the container of q.
func (q *query) path() string {
	return "/dbs/" + url.PathEscape(q.database) + "/colls/" + url.PathEscape(q.container) + "/docs"
}

// docs sends q, as the query of a page. token is the header X-Ms-Continuation
// of the page before, and "" for the first page. It returns a response with a
// 2xx status. Any other response is an error, and its body is closed.
func (c *Connector) docs(ctx context.Context, q *query, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+q.path(), bytes.NewReader(q.body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/query+json")
	req.Header.Set("X-Ms-Documentdb-Isquery", "True")
	if q.hasKey {
		req.Header.Set("X-Ms-Documentdb-Partitionkey", q.key)
	} else {
		req.Header.Set("X-Ms-Documentdb-Query-Enablecrosspartition", "True")
	}
	if q.pageSize != 0 {
		req.Header.Set("X-Ms-Max-Item-Count", strconv.Itoa(q.pageSize))
	}
	if token != "" {
		req.Header.Set("X-Ms-Continuation", token)
	}
	return c.do(req)
}

// do signs req, sends it once, and checks its status.
func (c *Connector) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Accept", "application/json")
	if err := sign(req, c.cfg.Key, c.now()); err != nil {
		return nil, err
	}
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	return res, nil
}
