package druid

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a query after its context
// ended, as D67 bounds it for Neo4j and D133 for Pinot (D164).
const stopTimeout = 5 * time.Second

// maxStopAnswer is the most of the answer to a cancel that the driver reads.
// The answer is empty (measured).
const maxStopAnswer = 64 << 10

// Connector opens connections to one server. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open
// it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
	// noStop is true when the driver sends no cancel, for the tests of the
	// contract, whose fake servers answer every request with a result.
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
	if cfg.TimeZone == "" {
		cfg.TimeZone = defaultTimeZone
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the credentials go to the host
		// of the DSN only (D164).
		client: dbimp.NewClient(t, false),
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

// query sends POST /druid/v2/sql with body, and returns a response of JSON
// with a 2xx status. Any other response is an error, and its body is closed.
func (c *Connector) query(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/druid/v2/sql", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		_ = res.Body.Close()
		return nil, &Error{HTTPStatus: res.StatusCode, Message: "the answer is " + ct + " and not JSON"}
	}
	return res, nil
}

// stop sends DELETE /druid/v2/sql/<id>, which cancels the query that the
// driver named id, with ctx without its end and the limit of a stop,
// because ctx ended (D164). The server answers HTTP 202, and HTTP 404 for a
// query that is not running, which is no error (measured).
func (c *Connector) stop(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/druid/v2/sql/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("making the request to cancel the query %s: %w", id, err)
	}
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return fmt.Errorf("cancelling the query %s: %w", id, err)
	}
	if res.StatusCode == http.StatusNotFound {
		_ = res.Body.Close()
		return nil
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

// watch cancels a query on the server when its context ends before the
// driver read the whole answer, because the server runs a query on when its
// client leaves (D164).
type watch struct {
	stop func() bool
	// now sends the cancel of the query at once, and err returns the error
	// of the context of the query, or nil.
	now  func()
	err  func() error
	done chan struct{}
	once sync.Once
}

// watch returns a watch of ctx for the query that the driver named id. It
// sends no cancel when the connector says so, for the tests.
func (c *Connector) watch(ctx context.Context, id string) *watch {
	w := &watch{done: make(chan struct{}), err: ctx.Err}
	if c.noStop {
		w.stop = func() bool { return true }
		w.now = func() {}
		return w
	}
	// The answer to the cancel holds nothing that the caller needs. The read
	// of the query fails with the error of its context.
	w.now = func() { _ = c.stop(ctx, id) }
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
// when the client leaves (D164). If the context ended first, the cancel is
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
		if !w.stop() {
			<-w.done
		}
	})
}
