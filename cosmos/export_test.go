package cosmos

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Raw sends one request of the REST API of Cosmos DB, signed with the master
// key of cfg, and returns the status, the headers and the body of the answer,
// whatever the status is. path is the path of the resource, such as
// /dbs/db/colls/c/docs, and header holds the headers of the request, such as
// the content type. The integration tests make their databases, their
// containers and their documents with it, because the SQL of Cosmos DB has no
// statement that writes (D190), and they use it for what the driver does not
// send, such as a batch, a patch and a stored procedure.
func Raw(ctx context.Context, cfg Config, method, path string, header map[string]string, body []byte) (int, http.Header, []byte, error) {
	return RawWith(ctx, cfg, method, path, header, body, false, time.Time{})
}

// RawWith is Raw for a request that has no signature, if unsigned is true, or
// whose signature has the date at, if at is not zero. The integration tests
// send such a request to see what the server does with it.
func RawWith(ctx context.Context, cfg Config, method, path string, header map[string]string, body []byte, unsigned bool, at time.Time) (int, http.Header, []byte, error) {
	c := NewConnector(cfg)
	if !at.IsZero() {
		c.now = func() time.Time { return at }
	}
	defer c.transport.CloseIdleConnections()
	u, err := url.Parse(c.base + path)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("reading the path: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if !unsigned {
		if err := sign(req, cfg.Key, c.now()); err != nil {
			return 0, nil, nil, err
		}
	}
	res, err := c.client.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("sending the request: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("reading the answer: %w", err)
	}
	return res.StatusCode, res.Header, b, nil
}

// WrapTransport replaces the transport of the client of c with the one that f
// returns for it, so that a test can count the requests that the connector
// sends.
func (c *Connector) WrapTransport(f func(http.RoundTripper) http.RoundTripper) {
	c.client.Transport = f(c.client.Transport)
}
