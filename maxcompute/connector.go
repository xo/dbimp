package maxcompute

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json/v2"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a statement after its context
// ended, as D67 bounds it for Neo4j, D133 for Pinot and D183 for Snowflake.
const stopTimeout = 5 * time.Second

// getTries is how many times the driver sends a GET that a reset stopped. A GET
// reads a state and changes nothing, so a second try is safe. The Jakarta
// endpoint resets many connections (recorded in testdata/maxcompute). A POST is
// never sent again, because it can have run (D8).
const getTries = 3

// The interval of the poll of a task that has not ended. It starts short, so that
// a statement that ends soon waits little, and it doubles up to a limit, so that
// a long statement costs few requests. The values are not measured.
const (
	pollMin = 100 * time.Millisecond
	pollMax = time.Second
)

// timezoneSetting is the setting of the server that names the time zone of a
// statement (recorded: "the project").
const timezoneSetting = "odps.sql.timezone"

// Connector opens connections to one project. It owns its transport, and every
// connection shares it. A caller can build one from a Config and open it with
// sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client

	// pollMin and pollMax are the interval of the poll, which the tests set.
	pollMin, pollMax time.Duration
	// wait waits for an interval, and is sleep by default. The tests set it.
	wait func(ctx context.Context, d time.Duration) error

	// zones holds the name of the time zone of each project that the driver
	// asked the server for.
	mu    sync.Mutex
	zones map[string]string
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of cfg,
// and fills each field that is zero with its default.
func NewConnector(cfg Config) *Connector {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.Region == "" {
		cfg.Region = regionOf(cfg.Host)
	}
	t := dbimp.NewTransport(&tls.Config{MinVersion: tls.VersionTLS12})
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the secret never leaves the host of
		// the DSN (D221 item 2).
		client:  dbimp.NewClient(t, false),
		pollMin: pollMin,
		pollMax: pollMax,
		wait:    sleep,
		zones:   map[string]string{},
	}
}

// Connect satisfies driver.Connector. A connection holds nothing on the server,
// so it sends no request.
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

// do sends a signed request to the server of the DSN, and returns the response
// with any status. The path is the resource, such as /projects/p/instances, and
// the query is its raw query, if it has one (D218 item 3).
func (c *Connector) do(ctx context.Context, method, path, query string, body []byte) (*http.Response, error) {
	u := c.base + basePath + path
	if query != "" {
		u += "?" + query
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/xml")
	}
	sign(req, c.cfg.ID, c.cfg.Secret, c.cfg.Region, time.Now())
	return dbimp.Send(c.client, req)
}

// badConnTries is how many times the driver sends a request that made no
// connection, which can be any request, because such a request never reached the
// server (D8).
const badConnTries = 5

// send sends a request with do. A request that made no connection is sent again
// up to badConnTries times, and so is a GET that a reset or a timeout of the
// handshake stopped, up to getTries times. A POST that a reset stopped after the
// connection was made is never sent again, because it can have run (D8 and D221
// item 4).
func (c *Connector) send(ctx context.Context, method, path, query string, body []byte) (*http.Response, error) {
	for try := 1; ; try++ {
		res, err := c.do(ctx, method, path, query, body)
		again := errors.Is(err, driver.ErrBadConn) && try < badConnTries ||
			method == http.MethodGet && try < getTries && transient(ctx, err)
		if err == nil || !again {
			return res, err
		}
		if err := c.wait(ctx, c.pollMin); err != nil {
			return nil, err
		}
	}
}

// get sends GET with send.
func (c *Connector) get(ctx context.Context, path, query string) (*http.Response, error) {
	return c.send(ctx, http.MethodGet, path, query, nil)
}

// transient reports whether err is a failure of the connection that a second try
// can cure: a reset, or a timeout of the handshake while ctx is still open.
func transient(ctx context.Context, err error) bool {
	var ne net.Error
	return errors.Is(err, syscall.ECONNRESET) || (ctx.Err() == nil && errors.As(err, &ne) && ne.Timeout())
}

// projectPath returns the path of the instances of a project.
func projectPath(project string) string {
	return "/projects/" + url.PathEscape(project) + "/instances"
}

// currProject returns the query that names the project of a request, which the
// server needs next to the path (recorded).
func currProject(project string) string {
	return "curr_project=" + url.QueryEscape(project)
}

// task is the answer to a statement that ended: the head that the driver read,
// and the body that holds the rest of its result.
type task struct {
	*answer

	body io.ReadCloser
}

// close closes the body of the answer.
func (t *task) close() error {
	if err := t.body.Close(); err != nil {
		return fmt.Errorf("closing the answer: %w", err)
	}
	return nil
}

// finish reads what follows the result, which is a few closing tags, so that the
// connection goes back to the pool (D36), and closes the body. A caller uses it
// for a small result that it read to its end.
func (t *task) finish() error {
	_, _ = io.Copy(io.Discard, io.LimitReader(t.body, maxDrain))
	return t.close()
}

// newTask reads the head of the answer in body. It closes the body and returns
// the error of a task that did not succeed, which its result explains.
func newTask(body io.ReadCloser) (*task, error) {
	a, err := readAnswer(body)
	if err != nil {
		_ = body.Close()
		return nil, err
	}
	t := &task{answer: a, body: body}
	if a.status != "" && a.status != stateSuccess {
		text, _ := a.readText()
		_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrain))
		_ = body.Close()
		return nil, taskError(a.status, text)
	}
	return t, nil
}

// run sends the statement, waits until it ends, and returns its result. If ctx
// ends while the statement runs on the server, it cancels the statement by its
// instance, and returns the error of ctx (D36 and D220 item 7).
func (c *Connector) run(ctx context.Context, project, sql string, settings map[string]string) (*task, error) {
	body, err := taskBody(sql, settings)
	if err != nil {
		return nil, err
	}
	res, err := c.send(ctx, http.MethodPost, projectPath(project), currProject(project), body)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, http.StatusOK, http.StatusCreated); err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusOK {
		// A statement of DDL answers at once, with its result.
		return newTask(res.Body)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<10))
	_ = res.Body.Close()
	path, err := instancePath(res.Header.Get("Location"))
	if err != nil {
		return nil, err
	}
	if err := c.poll(ctx, project, path); err != nil {
		if ctx.Err() != nil {
			// The statement can still run on the server.
			_ = c.stop(ctx, project, path)
		}
		return nil, err
	}
	res, err = c.get(ctx, path, "result&"+currProject(project))
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return nil, fmt.Errorf("reading the result of %s: %w", path, err)
	}
	return newTask(res.Body)
}

// instancePath returns the resource of the instance that the header Location
// names, with the path of the endpoint removed. It refuses a Location that does
// not name an instance.
func instancePath(location string) (string, error) {
	u, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("reading the Location of the instance: %w", err)
	}
	path, ok := strings.CutPrefix(u.Path, basePath)
	if !ok || !strings.Contains(path, "/instances/") {
		return "", fmt.Errorf("reading the Location of the instance: %q names no instance: %w", u.Path, dbimp.ErrInvalidValue)
	}
	return path, nil
}

// poll asks for the state of the task until it ends. It returns nil when the
// task ended in any state, and the caller reads the result for the state.
func (c *Connector) poll(ctx context.Context, project, path string) error {
	delay := c.pollMin
	for {
		res, err := c.get(ctx, path, "taskstatus&"+currProject(project))
		if err != nil {
			return err
		}
		if err := checkStatus(res, http.StatusOK); err != nil {
			return fmt.Errorf("reading the state of %s: %w", path, err)
		}
		a, err := readAnswer(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return err
		}
		switch a.status {
		case stateSuccess, stateFailed, stateCancelled:
			return nil
		case stateRunning, stateWaiting, "":
		default:
			return fmt.Errorf("reading the state of %s: the state %q is not one that the driver knows: %w", path, a.status, dbimp.ErrNotSupported)
		}
		if err := c.wait(ctx, delay); err != nil {
			return err
		}
		delay = min(delay*2, c.pollMax)
	}
}

// stop sends PUT with the state Terminated to the instance, with ctx without
// its end and the limit of a stop, because ctx can have ended (D220 item 7). The
// server answers 202 (recorded: "cancel: terminate it").
func (c *Connector) stop(ctx context.Context, project, path string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	res, err := c.do(ctx, http.MethodPut, path, currProject(project), []byte(terminateBody))
	if err != nil {
		return fmt.Errorf("canceling %s: %w", path, err)
	}
	if err := checkStatus(res, http.StatusOK, http.StatusAccepted); err != nil {
		return fmt.Errorf("canceling %s: %w", path, err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<10))
	_ = res.Body.Close()
	return nil
}

// terminateBody is the body of the request that cancels an instance.
const terminateBody = `<?xml version="1.0" encoding="UTF-8"?><Instance><Status>Terminated</Status></Instance>`

// taskBody returns the XML body that creates an instance for the statement, with
// the settings of the server if there are any (recorded: "select 1", "select
// with settings in the task").
func taskBody(sql string, settings map[string]string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Instance><Job><Priority>9</Priority><Tasks><SQL><Name>AnonymousSQLTask</Name><Config>`)
	b.WriteString(`<Property><Name>type</Name><Value>sql</Value></Property>`)
	if len(settings) > 0 {
		text, err := json.Marshal(settings, json.Deterministic(true))
		if err != nil {
			return nil, fmt.Errorf("writing the settings: %w", err)
		}
		b.WriteString(`<Property><Name>settings</Name><Value>`)
		if err := xml.EscapeText(&b, text); err != nil {
			return nil, fmt.Errorf("writing the settings: %w", err)
		}
		b.WriteString(`</Value></Property>`)
	}
	b.WriteString(`</Config><Query>`)
	if err := xml.EscapeText(&b, []byte(terminate(sql))); err != nil {
		return nil, fmt.Errorf("writing the statement: %w", err)
	}
	b.WriteString(`</Query></SQL></Tasks></Job></Instance>`)
	return b.Bytes(), nil
}

// terminate returns the statement with the semicolon that the server needs at its
// end (recorded: "select with a trailing semicolon missing"). A statement that
// has none gets it on a line of its own, so that a comment at the end of the
// statement does not swallow it.
func terminate(sql string) string {
	if strings.HasSuffix(strings.TrimRight(sql, " \t\r\n"), ";") {
		return sql
	}
	return sql + "\n;"
}

// zone returns the time zone of the statements of project. It is the setting
// odps.sql.timezone if the settings hold it, and the property of the project
// with that name otherwise (D219 item 2).
func (c *Connector) zone(ctx context.Context, project string, settings map[string]string) (*time.Location, error) {
	name := settings[timezoneSetting]
	if name == "" {
		var err error
		if name, err = c.projectZone(ctx, project); err != nil {
			return nil, err
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("loading the time zone %q of the project %s: %w", name, project, err)
	}
	return loc, nil
}

// projectZone returns the name of the time zone of a project, from the server
// once, and from memory after that.
func (c *Connector) projectZone(ctx context.Context, project string) (string, error) {
	c.mu.Lock()
	name, ok := c.zones[project]
	c.mu.Unlock()
	if ok {
		return name, nil
	}
	res, err := c.get(ctx, "/projects/"+url.PathEscape(project), "")
	if err != nil {
		return "", err
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return "", fmt.Errorf("reading the project %s: %w", project, err)
	}
	defer res.Body.Close()
	if name, err = property(res.Body, timezoneSetting); err != nil {
		return "", err
	}
	if name == "" {
		name = "UTC"
	}
	c.mu.Lock()
	c.zones[project] = name
	c.mu.Unlock()
	return name, nil
}
