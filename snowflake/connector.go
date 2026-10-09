package snowflake

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a statement after its context
// ended, as D67 bounds it for Neo4j and D133 for Pinot (D183).
const stopTimeout = 5 * time.Second

// maxSmallAnswer is the most of an answer that the driver reads when the
// answer holds no result: the answer to a cancel, and the answer to a poll
// of a statement that still runs. Each one is a small object (measured).
const maxSmallAnswer = 64 << 10

// The interval of the poll of a statement that still runs. It starts short,
// so that a statement that ends soon waits little, and it doubles up to a
// limit, so that a long statement costs few requests. The waits are 25 ms, 50,
// 100, 200, 400, and then 500 ms for each poll that follows (D183). The values
// are not measured.
const (
	pollMin = 25 * time.Millisecond
	pollMax = 500 * time.Millisecond
)

// pathStatements is the path of the SQL API (measured).
const pathStatements = "/api/v2/statements"

// Connector opens connections to one account. It owns its transport, and
// every connection shares it, with the token that the driver signs. A caller
// can build one from a Config and open it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
	signer    *signer
	// signErr is the error of the private key, and nil when the key is
	// valid. The connector keeps it, because NewConnector returns no error.
	signErr error

	mu      sync.Mutex
	token   string
	expires time.Time

	// now returns the time of a token, and the time package by default. The
	// tests set it.
	now func() time.Time
	// noStop is true when the driver sends no cancel, for the tests of the
	// contract, whose fake servers answer every request with a result.
	noStop bool
	// pollMin and pollMax are the interval of the poll, which the tests set.
	pollMin, pollMax time.Duration
	// wait waits for the interval of a poll, and is sleep by default. The
	// tests set it.
	wait func(ctx context.Context, d time.Duration) error
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg, and fills each field that is zero with its default. A private key that
// is not valid fails the first Connect.
func NewConnector(cfg Config) *Connector {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	t := dbimp.NewTransport(&tls.Config{MinVersion: tls.VersionTLS12})
	c := &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the token goes to the host of
		// the DSN only (D183).
		client:  dbimp.NewClient(t, false),
		pollMin: pollMin,
		pollMax: pollMax,
		wait:    sleep,
	}
	key, err := parseKey(cfg.Password)
	if err == nil {
		c.signer, err = newSigner(key, cfg.account(), cfg.User)
	}
	c.signErr = err
	return c
}

// Connect satisfies driver.Connector. A connection holds nothing on the
// server, so it sends no request.
func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	if c.signErr != nil {
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

// bearer returns the token of the user. It signs a new one when it has none,
// and when less than five minutes of the old one are left (D183).
func (c *Connector) bearer() (string, error) {
	if c.signErr != nil {
		return "", c.signErr
	}
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" || !now.Before(c.expires.Add(-tokenRenew)) {
		token, expires, err := c.signer.sign(now)
		if err != nil {
			return "", err
		}
		c.token, c.expires = token, expires
	}
	return c.token, nil
}

// do sends a request to the server of the DSN, with the token and the
// headers of the API, and returns the response with any status (D183).
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
	token, err := c.bearer()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Snowflake-Authorization-Token-Type", "KEYPAIR_JWT")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return dbimp.Send(c.client, req)
}

// start sends POST /api/v2/statements?async=true with the body, so that the
// client always holds the handle of the statement (D183). It returns the
// response when its status is 200, a statement that finished, or 202, one that
// runs. Any other response is an error, and its body is closed.
func (c *Connector) start(ctx context.Context, body []byte) (*http.Response, error) {
	res, err := c.do(ctx, http.MethodPost, pathStatements, url.Values{"async": {"true"}}, body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, http.StatusOK, http.StatusAccepted); err != nil {
		return nil, err
	}
	return res, nil
}

// statementPath returns the path of a statement by its handle.
func statementPath(handle string) string {
	return pathStatements + "/" + url.PathEscape(handle)
}

// status sends GET on the statement, and returns a response when its status is
// one of ok. Any other response is an error, and its body is closed.
func (c *Connector) status(ctx context.Context, handle string, query url.Values, ok ...int) (*http.Response, error) {
	res, err := c.do(ctx, http.MethodGet, statementPath(handle), query, nil)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, ok...); err != nil {
		return nil, err
	}
	return res, nil
}

// partition fetches the partition n of the result of a statement, which is
// an object with data and nothing else (measured). The server compresses it
// with gzip, and the transport decompresses it.
func (c *Connector) partition(ctx context.Context, handle string, n int) (*http.Response, error) {
	res, err := c.status(ctx, handle, url.Values{"partition": {strconv.Itoa(n)}}, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("fetching the partition %d: %w", n, err)
	}
	return res, nil
}

// readHandle reads the handle from the body of the answer of HTTP 202, and
// closes the body.
func readHandle(res *http.Response) (string, error) {
	defer res.Body.Close()
	var body struct {
		Handle string `json:"statementHandle"`
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxSmallAnswer), &body); err != nil {
		return "", fmt.Errorf("reading the answer of a statement that runs: %w", err)
	}
	if body.Handle == "" {
		return "", fmt.Errorf("reading the answer of a statement that runs: no statementHandle: %w", dbimp.ErrInvalidValue)
	}
	return body.Handle, nil
}

// handleLink finds the handle of the statement in the header Link of an
// answer, whose first member is the first partition, such as
// </api/v2/statements/01c7...?requestId=...&partition=0>; rel="first"
// (measured). It returns "" when the header has none.
var handleLink = regexp.MustCompile(`</api/v2/statements/([^/?>]+)[?>]`)

// linkHandle returns the handle that the header Link of res holds, and "".
func linkHandle(res *http.Response) string {
	m := handleLink.FindStringSubmatch(res.Header.Get("Link"))
	if m == nil {
		return ""
	}
	h, err := url.PathUnescape(m[1])
	if err != nil {
		return ""
	}
	return h
}

// await polls the statement until it ends. It returns the answer of HTTP 200,
// which holds the result, and any other final status is an error: a statement
// that failed answers HTTP 422, and one that someone canceled answers HTTP 422
// with the code 000604 (measured). The interval of the poll grows (D183).
func (c *Connector) await(ctx context.Context, handle string) (*http.Response, error) {
	delay := c.pollMin
	for {
		if err := c.wait(ctx, delay); err != nil {
			return nil, fmt.Errorf("waiting for the statement %s: %w", handle, err)
		}
		res, err := c.status(ctx, handle, nil, http.StatusOK, http.StatusAccepted)
		if err != nil {
			// A statement that fails while it runs answers with a code and a
			// message only, with no SQLSTATE and no handle (measured,
			// 2026-10-10). The driver holds the handle.
			if serr, ok := errors.AsType[*Error](err); ok && serr.Handle == "" {
				serr.Handle = handle
			}
			return nil, err
		}
		if res.StatusCode == http.StatusOK {
			return res, nil
		}
		// The statement runs, and the body names it again.
		_, err = io.Copy(io.Discard, io.LimitReader(res.Body, maxSmallAnswer))
		if cerr := res.Body.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, fmt.Errorf("reading the status of the statement %s: %w", handle, err)
		}
		delay = min(delay*2, c.pollMax)
	}
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

// stop sends POST /api/v2/statements/<handle>/cancel, with ctx without its
// end and the limit of a stop, because ctx can have ended (D183). The server
// answers HTTP 200 for a statement that runs and for one that is gone
// (measured).
func (c *Connector) stop(ctx context.Context, handle string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	res, err := c.do(ctx, http.MethodPost, statementPath(handle)+"/cancel", nil, nil)
	if err != nil {
		return fmt.Errorf("canceling the statement %s: %w", handle, err)
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return fmt.Errorf("canceling the statement %s: %w", handle, err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxSmallAnswer)); err != nil {
		return fmt.Errorf("reading the answer to the cancel of %s: %w", handle, err)
	}
	return nil
}

// watch cancels a statement on the server when its context ends before the
// driver read the whole result, and when the caller closes the rows before
// the end (D183).
type watch struct {
	stop func() bool
	// now sends the cancel of the statement at once, and err returns the
	// error of the context of the statement, or nil.
	now  func()
	err  func() error
	done chan struct{}
	once sync.Once
	// handle is the handle of the statement, and empty until the driver
	// knows it.
	handle atomic.Pointer[string]
}

// watch returns a watch of ctx for the statement with the handle, which can
// be empty when the driver learns it later with setHandle. It sends no cancel
// when the connector says so, for the tests.
func (c *Connector) watch(ctx context.Context, handle string) *watch {
	w := &watch{done: make(chan struct{}), err: ctx.Err}
	w.setHandle(handle)
	if c.noStop {
		w.stop = func() bool { return true }
		w.now = func() {}
		return w
	}
	// The answer to the cancel holds nothing that the caller needs. The read
	// of the statement fails with the error of its context.
	w.now = func() {
		if h := w.id(); h != "" {
			_ = c.stop(ctx, h)
		}
	}
	w.stop = context.AfterFunc(ctx, func() {
		defer close(w.done)
		w.now()
	})
	return w
}

// setHandle records the handle of the statement.
func (w *watch) setHandle(handle string) {
	if w != nil && handle != "" {
		w.handle.Store(&handle)
	}
}

// id returns the handle of the statement, or "".
func (w *watch) id() string {
	if h := w.handle.Load(); h != nil {
		return *h
	}
	return ""
}

// cause returns the error of the context of the statement, or nil. A watch
// that is nil has none.
func (w *watch) cause() error {
	if w == nil {
		return nil
	}
	return w.err()
}

// abort ends the watch for rows that the caller closes before the end of the
// result, and cancels the statement on the server (D183). If the context
// ended first, the cancel is sent already, and abort waits until it is done.
func (w *watch) abort() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		if w.stop() {
			w.now()
			return
		}
		<-w.done
	})
}

// end ends the watch. If the context ended first, it waits until the cancel
// is done, so that no request runs on after the statement.
func (w *watch) end() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		switch {
		case !w.stop():
			<-w.done
		case w.err() != nil:
			// The context ended, and the transport saw it before the
			// function of the watch started, so stop kept it from running.
			// The statement can still run on the server, and the watch
			// cancels it here.
			w.now()
		}
	})
}
