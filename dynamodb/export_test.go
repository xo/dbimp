package dynamodb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// Raw sends the operation op of the API of DynamoDB, such as CreateTable or
// BatchExecuteStatement, with body, as the connector of cfg would send a
// statement. It returns the status and the body of the answer, whatever the
// status is. The integration tests make their tables with it, because
// PartiQL has no DDL (D171), and they use it for what the driver does not
// send, such as a batch and a transaction.
func Raw(ctx context.Context, cfg Config, op string, body []byte) (int, []byte, error) {
	c := NewConnector(cfg)
	defer c.transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/", bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", targetPrefix+op)
	cfg.sign(req, body, c.now())
	res, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("sending the request: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("reading the answer: %w", err)
	}
	return res.StatusCode, b, nil
}

// WrapTransport replaces the transport of the client of c with the one that f
// returns for it, so that a test can count the requests that the connector
// sends.
func (c *Connector) WrapTransport(f func(http.RoundTripper) http.RoundTripper) {
	c.client.Transport = f(c.client.Transport)
}
