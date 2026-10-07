package clickhouse

import (
	"context"
	"crypto/rand"
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
// ended, as D67 bounds it for Neo4j and D133 for Pinot (D176).
const stopTimeout = 5 * time.Second

// maxStopAnswer is the most of the answer to a cancel that the driver reads.
// It is one row that names the query (measured).
const maxStopAnswer = 64 << 10

// idleTimeout is how long the transport keeps an idle connection. The server
// closes an idle connection after 10 seconds on 25.3 and 25.8, and after 30 on
// 26.9 (the header Keep-Alive, measured), and the driver sends no POST again after
// the server closed the connection that it used, so the transport lets go of an
// idle connection before the server does.
const idleTimeout = 5 * time.Second

// format is the format of every answer that the driver reads (D176).
const format = "JSONCompactEachRowWithNamesAndTypes"

// settings are the settings that the driver sends on every request, so that each
// value of the answer is exact (D176): the integers of 64 bits and more are
// numbers and not strings, a NaN and an infinity are strings, an instant is in
// UTC, a tuple is an array, and an error after some rows is the marker and not a
// row of the format.
var settings = [...][2]string{
	{"default_format", format},
	{"output_format_json_quote_64bit_integers", "0"},
	{"output_format_json_quote_denormals", "1"},
	{"date_time_output_format", "iso"},
	{"output_format_json_named_tuples_as_objects", "0"},
	{"http_write_exception_in_output_format", "0"},
	{"enable_http_compression", "0"},
}

// Connector opens connections to one server. It owns its transport, and every
// connection shares it. A caller can build one from a Config and open it with
// sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
	// noStop is true when the driver sends no cancel, and noVersion when a
	// connection asks no version, for the tests of the contract, whose fake
	// servers answer every request with a result.
	noStop    bool
	noVersion bool
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
		if cfg.TLS {
			cfg.Port = defaultTLSPort
		}
	}
	t := dbimp.NewTransport(tlsCfg)
	t.IdleConnTimeout = idleTimeout
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the credentials go to the host of
		// the DSN only (D177).
		client: dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. It runs SELECT version() once for the
// connection, which tells how the server frames an error after some rows (D177).
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	cn := &conn{c: c}
	if c.noVersion {
		return cn, nil
	}
	version, err := cn.readVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("asking the server for its version: %w", err)
	}
	cn.version = version
	return cn, nil
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

// send sends POST / with the statement as its body and the keys of query, and
// returns a response of HTTP 200. Any other response is an error, and its body is
// closed (D176 and D177). The request carries the user and the password in a
// Basic header, and never in the headers X-ClickHouse-User and X-ClickHouse-Key,
// which the server refuses with a Basic header (D177).
func (c *Connector) send(ctx context.Context, statement string, query url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/?"+query.Encode(), strings.NewReader(statement))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	return res, nil
}

// stop runs KILL QUERY for the query that the driver named id, with ctx without
// its end and the limit of a stop, because ctx can have ended (D176). SYNC makes
// the server answer after the query stopped. The answer is empty for a query
// that has ended or that does not exist (measured).
func (c *Connector) stop(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	query := url.Values{"default_format": {format}, "param_id": {escape(id)}}
	res, err := c.send(ctx, "KILL QUERY WHERE query_id = {id:String} SYNC", query)
	if err != nil {
		return fmt.Errorf("cancelling the query %s: %w", id, err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxStopAnswer)); err != nil {
		return fmt.Errorf("reading the answer to the cancel of %s: %w", id, err)
	}
	return nil
}

// newID returns the id that the driver gives a query, so that it can cancel
// the query (D176).
func newID() string {
	return "dbimp-" + strings.ToLower(rand.Text())
}

// watch cancels a query on the server when its context ends before the driver
// read the whole answer, because the server runs a query on when its client
// leaves (D176).
type watch struct {
	stop func() bool
	// now sends the cancel of the query at once, and err returns the error of
	// the context of the query, or nil.
	now  func()
	err  func() error
	done chan struct{}
	once sync.Once
}

// watch returns a watch of ctx for the query that the driver named id. It sends
// no cancel when the connector says so, for the tests.
func (c *Connector) watch(ctx context.Context, id string) *watch {
	w := &watch{done: make(chan struct{}), err: ctx.Err}
	if c.noStop {
		w.stop = func() bool { return true }
		w.now = func() {}
		return w
	}
	// The answer to the cancel holds nothing that the caller needs. The read of
	// the query fails with the error of its context.
	w.now = func() { _ = c.stop(ctx, id) }
	w.stop = context.AfterFunc(ctx, func() {
		defer close(w.done)
		w.now()
	})
	return w
}

// cause returns the error of the context of the query, or nil. A watch that is
// nil has none.
func (w *watch) cause() error {
	if w == nil {
		return nil
	}
	return w.err()
}

// abort ends the watch for rows that the caller closes before the end of the
// answer, and cancels the query on the server (D176). If the context ended
// first, the cancel is sent already, and abort waits until it is done.
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

// end ends the watch. If the context ended first, it waits until the cancel is
// done, so that no request runs on after the query.
func (w *watch) end() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		switch {
		case !w.stop():
			<-w.done
		case w.err() != nil:
			// The context ended, and the transport saw it before the function
			// of the watch started, so stop kept it from running. The query
			// still runs on the server, and the watch cancels it here.
			w.now()
		}
	})
}
