package athena

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/xo/dbimp"
)

// Raw sends the operation op of the API of Athena, such as
// CreatePreparedStatement or BatchGetQueryExecution, with body, as the connector
// of cfg sends a statement. It returns the status and the body of the
// answer, whatever the status is. The integration tests use it for what the
// driver does not send: the calls of the API that are no statement, and a start
// with a token that the test chooses.
func Raw(ctx context.Context, cfg Config, op string, body []byte) (int, []byte, error) {
	c := NewConnector(cfg)
	defer c.transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/", bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", targetPrefix+op)
	if cfg.Token != "" {
		req.Header.Set(securityHeader, cfg.Token)
	}
	dbimp.SignV4(req, body, cfg.User, cfg.Password, cfg.Region, service, c.now())
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
