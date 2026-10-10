package databricks

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a statement after its context
// ended, as D67 bounds it for Neo4j, D133 for Pinot and D183 for Snowflake.
const stopTimeout = 5 * time.Second

// maxSmallAnswer is the most of an answer that the driver reads when the
// answer holds no result: the answer to a cancel, and the answer to a poll of
// a statement that still runs. Each one is a small object (measured).
const maxSmallAnswer = 64 << 10

// waitTimeout is the wait that the driver sends with each statement, the most
// that the server allows, which is 50 seconds (measured, D193 item 8). The first
// statement after the warehouse stopped takes about 15 seconds, so the wait
// covers it.
const waitTimeout = 50 * time.Second

// The interval of the poll of a statement that is still pending or running. It
// starts short, so that a statement that ends soon waits little, and it doubles
// up to a limit, so that a long statement costs few requests. The waits are 25
// ms, 50, 100, 200, 400, and then 500 ms for each poll that follows. The values
// are not measured.
const (
	pollMin = 25 * time.Millisecond
	pollMax = 500 * time.Millisecond
)

// pathStatements is the path of the Statement Execution API (measured).
const pathStatements = "/api/2.0/sql/statements"

// Connector opens connections to one workspace. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open it
// with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client

	// pollMin and pollMax are the interval of the poll, which the tests set.
	pollMin, pollMax time.Duration
	// wait waits for the interval of a poll, and is sleep by default. The
	// tests set it.
	wait func(ctx context.Context, d time.Duration) error
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of cfg,
// and fills each field that is zero with its default.
func NewConnector(cfg Config) *Connector {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	t := dbimp.NewTransport(&tls.Config{MinVersion: tls.VersionTLS12})
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the token goes to the host of
		// the DSN only (D193 item 9).
		client:  dbimp.NewClient(t, false),
		pollMin: pollMin,
		pollMax: pollMax,
		wait:    sleep,
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

// do sends a request to the server of the DSN, with the token and the headers
// of the API, and returns the response with any status (D193).
func (c *Connector) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	dbimp.SetAuth(req, dbimp.AuthBearer, "Bearer", "", c.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return dbimp.Send(c.client, req)
}

// post sends POST /api/2.0/sql/statements with the body. It returns the response
// when its status is 200, and any other response is an error whose body is
// closed. A statement that fails in the engine is HTTP 200 too, and the reader
// of the answer finds its error.
func (c *Connector) post(ctx context.Context, body []byte) (*http.Response, error) {
	res, err := c.do(ctx, http.MethodPost, pathStatements, body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return nil, err
	}
	return res, nil
}

// statementPath returns the path of a statement by its id.
func statementPath(id string) string {
	return pathStatements + "/" + url.PathEscape(id)
}

// get sends GET on the statement, which answers at once with its state and,
// when it succeeded, its result (measured). Any status but 200 is an error whose
// body is closed.
func (c *Connector) get(ctx context.Context, id string) (*http.Response, error) {
	res, err := c.do(ctx, http.MethodGet, statementPath(id), nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return nil, fmt.Errorf("reading the statement %s: %w", id, err)
	}
	return res, nil
}

// sleep waits for d, or until ctx ends, and returns the error of ctx then.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// stop sends POST /api/2.0/sql/statements/<id>/cancel, with ctx without its
// end and the limit of a stop, because ctx can have ended (D193). The server
// answers HTTP 200 and {} for a statement that runs, for one that finished and
// for one that does not exist (measured).
func (c *Connector) stop(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	res, err := c.do(ctx, http.MethodPost, statementPath(id)+"/cancel", nil)
	if err != nil {
		return fmt.Errorf("canceling the statement %s: %w", id, err)
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return fmt.Errorf("canceling the statement %s: %w", id, err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxSmallAnswer)); err != nil {
		return fmt.Errorf("reading the answer to the cancel of %s: %w", id, err)
	}
	return nil
}

// run sends the statement and polls it until it ends. It returns the rows of
// the answer that holds the result. If ctx ends while the statement runs on
// the server, it cancels the statement by its id, and returns the error of ctx
// (D36 and D193 item 8). A statement that ended needs no cancel.
func (c *Connector) run(ctx context.Context, body []byte) (*rows, error) {
	res, err := c.post(ctx, body)
	if err != nil {
		return nil, err
	}
	r, id, err := readAnswer(res)
	delay := c.pollMin
	for r == nil && err == nil {
		if err = c.wait(ctx, delay); err == nil {
			if res, err = c.get(ctx, id); err == nil {
				r, _, err = readAnswer(res)
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				// The statement can still run on the server.
				_ = c.stop(ctx, id)
				err = fmt.Errorf("waiting for the statement %s: %w", id, ctx.Err())
			}
			return nil, err
		}
		delay = min(delay*2, c.pollMax)
	}
	return r, err
}
