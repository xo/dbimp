package athena

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// service is the name of the service in the scope of each signature, and
// targetPrefix starts the name of each operation, in the header X-Amz-Target
// (recorded: the credential scope is 20261010/us-east-1/athena/aws4_request).
const (
	service      = "athena"
	targetPrefix = "AmazonAthena."
)

// securityHeader is the header that carries the session token of temporary
// credentials.
const securityHeader = "X-Amz-Security-Token"

// stopTimeout bounds the request that stops a query after its context ended,
// as D67 bounds the rollback of Neo4j and D133 the cancel of Pinot (D192).
const stopTimeout = 5 * time.Second

// maxAnswer is the most of the answer to a call that holds no rows that the
// driver reads. The answer to GetQueryExecution holds the text of the query,
// at most 262144 bytes, twice, and each byte can be written with six (recorded:
// "a long query text").
const maxAnswer = 4 << 20

// The interval of the poll of a query that still runs. It starts at 100 ms, so
// that a short query waits little, and it doubles up to one second, so that a
// long query costs few calls (D192 item 8). The recorded queries took 242 ms to
// 8.8 seconds.
const (
	pollMin = 100 * time.Millisecond
	pollMax = time.Second
)

// pageSize is MaxResults of each GetQueryResults, which is the most that the
// server allows and its default (recorded: "a page larger than the limit",
// "the default page").
const pageSize = 1000

// Connector opens connections to one endpoint. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open it
// with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client

	// now returns the time of a signature, and time.Now by default. The tests
	// set it.
	now func() time.Time
	// pollMin and pollMax are the interval of the poll, which the tests set.
	pollMin, pollMax time.Duration
	// wait waits for the interval of a poll, and is sleep by default. The
	// tests set it.
	wait func(ctx context.Context, d time.Duration) error
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
		// The driver follows no redirect, so the signature goes to the host of
		// the DSN only (D192).
		client:  dbimp.NewClient(t, false),
		now:     time.Now,
		pollMin: pollMin,
		pollMax: pollMax,
		wait:    sleep,
	}
}

// Connect satisfies driver.Connector. A connection holds nothing on the
// server, so it sends no request. It fails when the Config has no credentials,
// because the driver reads none from the environment (D7).
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

// call sends POST / with the operation op, signs it, and returns a response
// with a 2xx status. Any other response is an error, and its body is closed.
// started is true when the call follows a StartQueryExecution that the server
// accepted. The statement is then on the server, so an error of the connection
// must not be driver.ErrBadConn, because database/sql runs the statement
// again (D8).
func (c *Connector) call(ctx context.Context, op string, body []byte, started bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", targetPrefix+op)
	if c.cfg.Token != "" {
		req.Header.Set(securityHeader, c.cfg.Token)
	}
	dbimp.SignV4(req, body, c.cfg.User, c.cfg.Password, c.cfg.Region, service, c.now())
	var res *http.Response
	if started {
		if res, err = c.client.Do(req); err != nil {
			return nil, fmt.Errorf("sending the request %s: %w", op, err)
		}
	} else if res, err = dbimp.Send(c.client, req); err != nil {
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

// callJSON sends the operation op with the body that req encodes to, and
// decodes the small answer into out, which can be nil.
func (c *Connector) callJSON(ctx context.Context, op string, req, out any, started bool) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("writing the request %s: %w", op, err)
	}
	res, err := c.call(ctx, op, body, started)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxAnswer)); err != nil {
			return fmt.Errorf("reading the answer to %s: %w", op, err)
		}
		return nil
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxAnswer), out); err != nil {
		return fmt.Errorf("reading the answer to %s: %w", op, err)
	}
	return nil
}

// newToken returns a ClientRequestToken, which is 40 characters long, where
// the server asks for 32 or more (recorded). A new token for each statement
// keeps the server from taking two statements for one (D192).
func newToken() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// start sends StartQueryExecution with body, and returns the id of the query.
// The server answers before the query runs (recorded: "a select").
func (c *Connector) start(ctx context.Context, body []byte) (string, error) {
	res, err := c.call(ctx, "StartQueryExecution", body, false)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		ID string `json:"QueryExecutionId"`
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxAnswer), &out); err != nil {
		// The statement can be on the server, so this is no bad connection.
		return "", fmt.Errorf("reading the answer to StartQueryExecution: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("reading the answer to StartQueryExecution: no QueryExecutionId: %w", dbimp.ErrInvalidValue)
	}
	return out.ID, nil
}

// execution is the answer of GetQueryExecution. The answer has a second object,
// QueryExecutionDetail, with a shorter copy of the same facts, which the driver
// ignores (recorded).
type execution struct {
	QueryExecution struct {
		StatementType string `json:"StatementType"`
		Status        struct {
			State             string `json:"State"`
			StateChangeReason string `json:"StateChangeReason"`
			AthenaError       *struct {
				ErrorCategory int    `json:"ErrorCategory"`
				ErrorMessage  string `json:"ErrorMessage"`
				ErrorType     int    `json:"ErrorType"`
			} `json:"AthenaError"`
		} `json:"Status"`
	} `json:"QueryExecution"`
}

// await polls the query until it ends. It returns the execution of a query that
// succeeded. A query that failed or was canceled is an *Error. When the context
// ends, or a poll fails, the query can still run on the server, so await stops
// it, as D36 asks (D192 item 8). The interval of the poll grows.
func (c *Connector) await(ctx context.Context, id string) (*execution, error) {
	delay := c.pollMin
	for {
		if err := c.wait(ctx, delay); err != nil {
			c.stop(ctx, id)
			return nil, fmt.Errorf("waiting for the query %s: %w", id, err)
		}
		var q execution
		if err := c.callJSON(ctx, "GetQueryExecution", map[string]string{"QueryExecutionId": id}, &q, true); err != nil {
			c.stop(ctx, id)
			return nil, err
		}
		switch q.QueryExecution.Status.State {
		case stateSucceeded:
			return &q, nil
		case stateFailed, stateCancelled:
			return nil, stateError(id, &q)
		case stateQueued, stateRunning:
		default:
			c.stop(ctx, id)
			return nil, fmt.Errorf("polling the query %s: the state %q is not one that the driver knows: %w", id, q.QueryExecution.Status.State, dbimp.ErrInvalidValue)
		}
		delay = min(delay*2, c.pollMax)
	}
}

// stop sends StopQueryExecution, with ctx without its end and with the limit of
// a stop, because ctx can have ended. It is a best effort: the answer is {} for
// a query that runs and for one that ended (recorded), and an error here must
// not hide the error that led to the stop, so it is dropped.
func (c *Connector) stop(ctx context.Context, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	_ = c.callJSON(ctx, "StopQueryExecution", map[string]string{"QueryExecutionId": id}, nil, true)
}

// results sends GetQueryResults for the page after token, or for the first page
// when token is empty, and returns the response.
func (c *Connector) results(ctx context.Context, id, token string) (*http.Response, error) {
	body, err := json.Marshal(struct {
		ID    string `json:"QueryExecutionId"`
		Max   int    `json:"MaxResults"`
		Token string `json:"NextToken,omitzero"`
	}{id, pageSize, token})
	if err != nil {
		return nil, fmt.Errorf("writing the request GetQueryResults: %w", err)
	}
	return c.call(ctx, "GetQueryResults", body, true)
}

// sleep waits for d, or until ctx ends, and returns the error of ctx then.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}
