package bigquery

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a job after its context ended,
// as D67 bounds it for Neo4j and D133 for Pinot (D189).
const stopTimeout = 5 * time.Second

// maxSmallAnswer is the most of an answer that the driver reads when the answer
// holds no result: the answer to a cancel. It is a small object (recorded:
// bigquery-213).
const maxSmallAnswer = 64 << 10

// firstWait is how long the service waits for a job before it answers
// jobComplete false, in milliseconds. A job that the driver does not know yet
// cannot be canceled, so the wait is short (D189). The service waits 10 seconds
// by default. The value is the choice of the driver and it is not measured.
const firstWait = 3000

// pollWait is how long one poll of a job waits at the service, in milliseconds.
const pollWait = 5000

// The interval between two polls of a job that still runs. It starts short, so
// that a job that ends soon waits little, and it doubles up to a limit, so that
// a long job costs few requests. The values are not measured.
const (
	pollMin = 25 * time.Millisecond
	pollMax = 500 * time.Millisecond
)

// timestampFormat is the form that the driver asks for a timestamp in (D189).
const timestampFormat = "ISO8601_STRING"

// Connector opens connections to one project. It owns its transport, and every
// connection shares it, with the access token. A caller can build one from a
// Config and open it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
	signer    *signer
	// signErr is the error of the key file, and nil when the file is valid. The
	// connector keeps it, because NewConnector returns no error.
	signErr error

	mu      sync.Mutex
	token   string
	expires time.Time

	// now returns the time of a token, and the time package by default. The
	// tests set it.
	now func() time.Time
	// pollMin and pollMax are the interval of the poll, which the tests set.
	pollMin, pollMax time.Duration
	// wait waits for the interval of a poll, and is sleep by default. The tests
	// set it.
	wait func(ctx context.Context, d time.Duration) error
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of cfg.
// It reads the key file of cfg, if there is one, and a key file that is not
// valid fails the first Connect.
func NewConnector(cfg Config) *Connector {
	t := dbimp.NewTransport(&tls.Config{MinVersion: tls.VersionTLS12})
	c := &Connector{
		cfg:       cfg,
		base:      cfg.base(),
		transport: t,
		// The driver follows no redirect, so the token goes to the host of the
		// DSN only (D189).
		client:  dbimp.NewClient(t, false),
		pollMin: pollMin,
		pollMax: pollMax,
		wait:    sleep,
	}
	if cfg.CredentialFile != "" && !cfg.DisableAuth {
		c.signer, c.signErr = readSigner(cfg.CredentialFile)
	}
	return c
}

// Connect satisfies driver.Connector. A connection holds nothing on the
// server, so it sends no request.
func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	if c.signErr != nil && c.cfg.AccessToken == "" {
		return nil, c.signErr
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

// projectPath returns the path of a resource of the project, such as
// /bigquery/v2/projects/p/queries.
func (c *Connector) projectPath(elem ...string) string {
	var b strings.Builder
	b.WriteString("/bigquery/v2/projects/" + url.PathEscape(c.cfg.Project))
	for _, e := range elem {
		b.WriteString("/" + url.PathEscape(e))
	}
	return b.String()
}

// do sends a request to the server of the DSN, with the token and the headers
// of the API, and returns the response with any status (D189).
func (c *Connector) do(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Response, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	token, err := c.bearer(ctx)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return dbimp.Send(c.client, req)
}

// start sends POST /queries with the body. It returns the response when its
// status is 200. Any other response is an error, and its body is closed.
func (c *Connector) start(ctx context.Context, body []byte) (*http.Response, error) {
	res, err := c.do(ctx, http.MethodPost, c.projectPath("queries"), nil, body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return nil, err
	}
	return res, nil
}

// results sends GET /queries/{jobId}, which is jobs.getQueryResults. It reads
// a page of a result with its token, and it polls a job that still runs. It
// returns the response when its status is 200. Any other response is an error,
// and its body is closed. Every call sends the location of the job (D189).
func (c *Connector) results(ctx context.Context, job jobRef, token string, o options, wait int) (*http.Response, error) {
	q := url.Values{"formatOptions.timestampOutputFormat": {timestampFormat}}
	if job.location != "" {
		q.Set("location", job.location)
	}
	if token != "" {
		q.Set("pageToken", token)
	}
	if o.maxResults > 0 {
		q.Set("maxResults", strconv.Itoa(o.maxResults))
	}
	if wait > 0 {
		q.Set("timeoutMs", strconv.Itoa(wait))
	}
	res, err := c.do(ctx, http.MethodGet, c.projectPath("queries", job.id), q, nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		var serr *Error
		if errors.As(err, &serr) && serr.JobID == "" {
			serr.JobID = job.id
		}
		return nil, err
	}
	return res, nil
}

// stop sends POST /jobs/{jobId}/cancel, which is jobs.cancel, with ctx without
// its end and the limit of a stop, because ctx can have ended (D189). The
// service answers HTTP 200 for a job that runs and for one that ended
// (recorded: bigquery-210 and bigquery-213). The cancel takes effect a little
// after its answer.
func (c *Connector) stop(ctx context.Context, job jobRef) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	var q url.Values
	if job.location != "" {
		q = url.Values{"location": {job.location}}
	}
	res, err := c.do(ctx, http.MethodPost, c.projectPath("jobs", job.id, "cancel"), q, []byte("{}"))
	if err != nil {
		return fmt.Errorf("canceling the job %s: %w", job.id, err)
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return fmt.Errorf("canceling the job %s: %w", job.id, err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxSmallAnswer)); err != nil {
		return fmt.Errorf("reading the answer to the cancel of %s: %w", job.id, err)
	}
	return nil
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
