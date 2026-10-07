package drill

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a query after its context
// ended, and the request that reads the profile of a query, which both run
// with the context without its end (D165).
const stopTimeout = 5 * time.Second

// maxStopAnswer is the most of the answer to a cancel that the driver reads.
// The answer is one line of text (measured).
const maxStopAnswer = 64 << 10

// session is the cookies that the server set for one connection. They hold
// the session on the server: the cookie is named Drill-Session-Id on 1.22.0
// and JSESSIONID on 1.21.2 (measured), so the connection keeps each cookie
// that the server sets, by its name.
type session []*http.Cookie

// update returns the session with the cookies of res set in it.
func (s session) update(res *http.Response) session {
	for _, ck := range res.Cookies() {
		if ck.Value == "" {
			continue
		}
		if i := slices.IndexFunc(s, func(c *http.Cookie) bool { return c.Name == ck.Name }); i >= 0 {
			s = slices.Clone(s)
			s[i] = &http.Cookie{Name: ck.Name, Value: ck.Value}
			continue
		}
		s = append(slices.Clone(s), &http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	return s
}

// Connector opens connections to one server. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open
// it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
	// noStop is true when the driver sends no cancel and reads no profile,
	// for the tests of the contract, whose fake servers answer every request
	// with a result.
	noStop bool
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
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect. The server sends a wrong password
		// to its login page with HTTP 307, and the credentials go to the
		// host of the DSN only (D165).
		client: dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. A connection holds its cookie only
// after its first request, so Connect sends no request.
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

// newRequest returns a request to the server with the credentials, and with
// the cookie of the session, if the connection has one.
func (c *Connector) newRequest(ctx context.Context, method, path string, sess session, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for _, ck := range sess {
		req.AddCookie(ck)
	}
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	return req, nil
}

// query sends POST /query.json with body, and returns a response of JSON with
// a 2xx status. It sets sess to the cookies that the answer sets. Any other
// response is an error, and its body is closed.
func (c *Connector) query(ctx context.Context, body []byte, sess *session) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/query.json", *sess, body)
	if err != nil {
		return nil, err
	}
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	*sess = sess.update(res)
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		_ = res.Body.Close()
		return nil, &Error{HTTPStatus: res.StatusCode, Message: "the answer is " + ct + " and not JSON"}
	}
	return res, nil
}

// stop sends GET /profiles/cancel/<id>, which cancels the query with the
// queryId id, with ctx without its end and the limit of a stop, because ctx
// ended (D165). The server answers HTTP 200 with a line of text, and the
// answer of the query then ends with CANCELED (measured).
func (c *Connector) stop(ctx context.Context, id string, sess session) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	req, err := c.newRequest(ctx, http.MethodGet, "/profiles/cancel/"+url.PathEscape(id), sess, nil)
	if err != nil {
		return fmt.Errorf("making the request to cancel the query %s: %w", id, err)
	}
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return fmt.Errorf("cancelling the query %s: %w", id, err)
	}
	if err := checkStatus(res); err != nil {
		return fmt.Errorf("cancelling the query %s: %w", id, err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxStopAnswer)); err != nil {
		return fmt.Errorf("reading the answer to the cancel of %s: %w", id, err)
	}
	return nil
}

// profile is the part of GET /profiles/<id>.json that the driver reads. A
// query that failed after some rows has its message only here (measured).
type profile struct {
	// Error is the start of the error, such as "SYSTEM ERROR: Drill Remote
	// Exception". The server sets the state of the query to failed before it
	// sets the error (measured).
	Error string `json:"error"`
	// VerboseError holds the cause, such as "(java.lang.NumberFormatException)
	// x", and the Java stack.
	VerboseError string `json:"verboseError"`
}

// message returns the message of the error of the profile: its start, and
// its cause if it has one. It returns the name of the Java class of the
// cause as the second value.
func (p profile) message() (string, string) {
	first := strings.TrimSpace(strings.SplitN(p.Error, "\n", 2)[0])
	for line := range strings.SplitSeq(p.VerboseError, "\n") {
		cause, ok := strings.CutPrefix(strings.TrimSpace(line), "(")
		if !ok {
			continue
		}
		class, text, ok := strings.Cut(cause, ") ")
		if !ok {
			class, text, _ = strings.Cut(cause, ")")
		}
		if first == "" {
			return text, class
		}
		return first + ": " + text, class
	}
	return first, ""
}

// profileWait and profileMaxWait are the first and the longest wait between
// two reads of a profile that has no error yet, and profileLimit is the most
// time that the driver waits for it.
const (
	profileWait    = 10 * time.Millisecond
	profileMaxWait = 250 * time.Millisecond
	profileLimit   = 2 * time.Second
)

// profile reads GET /profiles/<id>.json, with ctx without its end and the
// limit of a stop, because the message of a query that failed must reach the
// caller even when the context ended (D165). The server ends the answer
// before it writes the error into the profile, so the driver reads the
// profile again, with a longer wait each time, until it holds an error or
// profileLimit passes. A profile that holds no error then is no failure of
// the read, and its message is empty (measured).
func (c *Connector) profile(ctx context.Context, id string, sess session) (profile, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	timer := time.NewTimer(0)
	defer timer.Stop()
	deadline := time.Now().Add(profileLimit)
	wait := profileWait
	for {
		p, err := c.readProfile(ctx, id, sess)
		if err != nil || p.Error != "" || !time.Now().Add(wait).Before(deadline) {
			return p, err
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			return p, fmt.Errorf("reading the profile of the query %s: %w", id, ctx.Err())
		case <-timer.C:
		}
		wait = min(wait*2, profileMaxWait)
	}
}

// readProfile sends GET /profiles/<id>.json once. The answer is read as a
// stream and never held whole.
func (c *Connector) readProfile(ctx context.Context, id string, sess session) (profile, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/profiles/"+url.PathEscape(id)+".json", sess, nil)
	if err != nil {
		return profile{}, fmt.Errorf("making the request for the profile of the query %s: %w", id, err)
	}
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return profile{}, fmt.Errorf("reading the profile of the query %s: %w", id, err)
	}
	if err := checkStatus(res); err != nil {
		return profile{}, fmt.Errorf("reading the profile of the query %s: %w", id, err)
	}
	defer res.Body.Close()
	var p profile
	if err := json.UnmarshalRead(res.Body, &p); err != nil {
		return profile{}, fmt.Errorf("reading the profile of the query %s: %w", id, err)
	}
	return p, nil
}

// watch cancels a query on the server when its context ends before the
// driver read the whole answer, because the server runs a query on when its
// client leaves (D165). It holds no context, only functions that close over
// it (hard rule 4).
type watch struct {
	stop func() bool
	// now sends the cancel of the query at once, err returns the error of
	// the context of the query, or nil, and profile reads the profile of the
	// query.
	now     func()
	err     func() error
	profile func() (profile, error)
	done    chan struct{}
	once    sync.Once
}

// watch returns a watch of ctx for the query with the queryId id, which a
// connection with the session sess runs. It sends no cancel when the
// connector says so, for the tests.
func (c *Connector) watch(ctx context.Context, id string, sess session) *watch {
	w := &watch{done: make(chan struct{}), err: ctx.Err}
	w.profile = func() (profile, error) {
		if c.noStop {
			return profile{}, fmt.Errorf("reading the profile of the query %s: the connector reads none: %w", id, dbimp.ErrNotSupported)
		}
		return c.profile(ctx, id, sess)
	}
	if c.noStop {
		w.stop = func() bool { return true }
		w.now = func() {}
		return w
	}
	// The answer to the cancel holds nothing that the caller needs. The read
	// of the query fails with the error of its context.
	w.now = func() { _ = c.stop(ctx, id, sess) }
	w.stop = context.AfterFunc(ctx, func() {
		defer close(w.done)
		w.now()
	})
	return w
}

// cause returns the error of the context of the query, or nil. A watch that
// is nil has none.
func (w *watch) cause() error {
	if w == nil {
		return nil
	}
	return w.err()
}

// abort ends the watch for rows that the caller closes before the end of the
// answer, and cancels the query on the server, because the server runs it on
// when the client leaves (D165). If the context ended first, the cancel is
// sent already, and abort waits until it is done.
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
// is done, so that no request runs on after the query.
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
			// The query still runs on the server, and the watch cancels it
			// here.
			w.now()
		}
	})
}
