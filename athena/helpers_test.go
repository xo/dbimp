package athena //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from Amazon Athena,
// under testdata/athena/. Each one decodes a real answer through the driver. A
// test never holds an answer that it wrote as a string, except the answers of
// the contract, which have the form of a page of results.

// testdata is the folder of the recordings.
const testdata = "../testdata/athena"

// testConfig returns the configuration of the tests: the workgroup and the
// database of the recording, and a fake key pair.
func testConfig() Config {
	return Config{
		Host:      "athena.us-east-1.amazonaws.com",
		TLS:       true,
		Region:    "us-east-1",
		User:      "AKIAEXAMPLE",
		Password:  "secret",
		WorkGroup: "dbimp",
		Database:  "dbimp_test",
	}
}

// noWait is the wait of the tests: it returns at once, or with the error of the
// context, so that no test waits a fixed time.
func noWait(ctx context.Context, _ time.Duration) error {
	return ctx.Err()
}

// connectorAt returns a connector for cfg that talks to the fake server at
// rawurl, which is an http:// URL.
func connectorAt(tb testing.TB, cfg Config, rawurl string) *Connector {
	tb.Helper()
	u, err := url.Parse(rawurl)
	if err != nil {
		tb.Fatal(err)
	}
	cfg.TLS = false
	cfg.Host = u.Hostname()
	if p := u.Port(); p != "" {
		cfg.Port, err = strconv.Atoi(p)
		if err != nil {
			tb.Fatal(err)
		}
	}
	c := NewConnector(cfg)
	c.wait = noWait
	c.now = func() time.Time { return time.Date(2026, 10, 10, 8, 51, 56, 0, time.UTC) }
	return c
}

// open returns a database on the fake server at rawurl, with the connector of
// cfg.
func open(tb testing.TB, cfg Config, rawurl string) *sql.DB {
	tb.Helper()
	c := connectorAt(tb, cfg, rawurl)
	db := sql.OpenDB(c)
	tb.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}

// match matches the operation and the body of a request with a recorded one. It
// leaves out ClientRequestToken, which is new for each statement (D192), and
// MaxResults, which the recorder sent for some pages only (the driver always
// sends it).
func match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path ||
		r.Header.Get("X-Amz-Target") != strings.Join(ex.Request.Header["X-Amz-Target"], ",") {
		return false
	}
	got, want := bodyOf(body), bodyOf(ex.Request.Content())
	return got != nil && reflect.DeepEqual(got, want)
}

// bodyOf returns the members of a JSON body, without ClientRequestToken, or nil
// when the body is not an object.
func bodyOf(body []byte) map[string]any {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil
	}
	delete(m, "ClientRequestToken")
	delete(m, "MaxResults")
	return m
}

// replay returns a database on a fake server that replays every recorded
// exchange.
func replay(t *testing.T) *sql.DB {
	t.Helper()
	srv := dbimptest.Replay(t, testdata, match)
	return open(t, testConfig(), srv.URL)
}

// exchange returns the exchange of the file with the number n.
func exchange(tb testing.TB, n int) *dbimptest.Exchange {
	tb.Helper()
	ex, err := dbimptest.ReadExchange(fmt.Sprintf("%s/athena-%03d-post.json", testdata, n))
	if err != nil {
		tb.Fatal(err)
	}
	return ex
}

// request is a request that a fake server received.
type request struct {
	target string
	header http.Header
	body   string
}

// fake is a server that keeps the requests that it received, and answers each
// one with a function of the number of the request, from 0.
type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []request
}

// newFake starts a fake server, which closes with the test. The handler h
// writes the answer.
func newFake(t *testing.T, h func(w http.ResponseWriter, r request, n int)) *fake {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		n := len(f.reqs)
		req := request{target: strings.TrimPrefix(r.Header.Get("X-Amz-Target"), targetPrefix), header: r.Header.Clone(), body: string(b)}
		f.reqs = append(f.reqs, req)
		f.mu.Unlock()
		h(w, req, n)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// requests returns the requests that arrived so far.
func (f *fake) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

// targets returns the operations of the requests that arrived so far, in order.
func (f *fake) targets() []string {
	var out []string
	for _, r := range f.requests() {
		out = append(out, r.target)
	}
	return out
}

// serve writes the recorded response ex to w, as the server sent it.
func serve(w http.ResponseWriter, ex *dbimptest.Exchange) {
	for key, vals := range ex.Response.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.Header().Del("Content-Length")
	w.WriteHeader(ex.Response.Status)
	_, _ = w.Write(ex.Response.Content())
}

// sequence returns a handler that answers the requests with the recorded
// exchanges numbered ns, in order, and fails the test when the operation of a
// request is not the operation of its exchange or when the requests outnumber
// the exchanges. The last exchange answers every later request when more is
// true.
func sequence(t *testing.T, more bool, ns ...int) func(w http.ResponseWriter, r request, n int) {
	t.Helper()
	exs := make([]*dbimptest.Exchange, len(ns))
	for i, n := range ns {
		exs[i] = exchange(t, n)
	}
	return func(w http.ResponseWriter, r request, n int) {
		i := n
		if i >= len(exs) {
			if !more {
				t.Errorf("request %d (%s) has no exchange", n, r.target)
				http.Error(w, "no exchange", http.StatusTeapot)
				return
			}
			i = len(exs) - 1
		}
		if want := strings.TrimPrefix(strings.Join(exs[i].Request.Header["X-Amz-Target"], ""), targetPrefix); r.target != want {
			t.Errorf("request %d is %s, want %s", n, r.target, want)
		}
		serve(w, exs[i])
	}
}

// show writes v with its Go type, so that a test compares the type of a value as
// well as its text.
func show(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case string:
		return fmt.Sprintf("string(%q)", v)
	case []byte:
		return fmt.Sprintf("[]byte(%x)", v)
	case time.Time:
		return fmt.Sprintf("time.Time(%s in %s)", v.Format("2006-01-02T15:04:05.999999999Z07:00"), v.Location())
	case interface{ Text(format byte) string }:
		return fmt.Sprintf("%T(%s)", v, v.Text('f'))
	case fmt.Stringer:
		return fmt.Sprintf("%T(%s)", v, v.String())
	}
	return fmt.Sprintf("%T(%v)", v, v)
}

// read runs a query and returns its columns and its rows, each value written by
// show. A row is read into a *any for each column, as a caller does.
func read(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]string, error) {
	t.Helper()
	return readContext(t.Context(), t, db, query, args...)
}

// readContext is read with the context ctx.
func readContext(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]string, error) {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scanning %q: %v", query, err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = show(v)
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

// selectOne returns a handler that answers each operation with the recorded
// answer of its own to "SELECT 1 AS a, 'x' AS b", whatever the statement and the
// id: the files 17, 18 and 19 hold the answers to StartQueryExecution,
// GetQueryExecution and GetQueryResults (recorded: "a select").
func selectOne(t *testing.T) func(w http.ResponseWriter, r request, n int) {
	t.Helper()
	answers := map[string]*dbimptest.Exchange{
		"StartQueryExecution": exchange(t, 17),
		"GetQueryExecution":   exchange(t, 18),
		"GetQueryResults":     exchange(t, 19),
	}
	return func(w http.ResponseWriter, r request, _ int) {
		ex, ok := answers[r.target]
		if !ok {
			t.Errorf("the request %s has no answer", r.target)
			http.Error(w, "no answer", http.StatusTeapot)
			return
		}
		serve(w, ex)
	}
}
