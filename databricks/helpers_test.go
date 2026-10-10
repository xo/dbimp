package databricks //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp/dbimptest"
)

// testdata holds the exchanges that step 6 recorded from a workspace.
const testdata = "../testdata/databricks"

// testHost is the host of the workspace of the tests. The recordings name it
// dbc-00000000-0000, and a test never sends a request to it, because each
// connector of the tests points at a fake server.
const testHost = "dbc-00000000-0000.cloud.databricks.com"

// testToken is the token of the tests. It is no real token.
const testToken = "dapi-test-token"

// recordedConfig is the configuration that the recorder used (step 6): the
// warehouse and the namespace that the recorded bodies hold.
func recordedConfig() Config {
	return Config{Host: testHost, Token: testToken, Warehouse: "1111222233334444", Catalog: "workspace", Schema: "dbimp"}
}

// fastPoll is the interval of the poll of the tests, so that no test waits a
// fixed time.
const fastPoll = time.Millisecond

// connector returns a connector for cfg that talks to the fake server at base.
func connector(cfg Config, base string) *Connector {
	c := NewConnector(cfg)
	c.base = base
	c.pollMin, c.pollMax = fastPoll, 4*fastPoll
	return c
}

// open returns a database on the fake server at base, with the connector of
// cfg.
func open(t *testing.T, cfg Config, base string) *sql.DB {
	t.Helper()
	c := connector(cfg, base)
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}

// replay opens the driver against the recorded exchanges, which the fake server
// of dbimptest answers by the method, the path and the body of the request.
func replay(t *testing.T) *sql.DB {
	t.Helper()
	srv := dbimptest.Replay(t, testdata, nil)
	return open(t, recordedConfig(), srv.URL)
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	return readAllContext(t, t.Context(), db, query, args...)
}

// readAllContext is readAll with a context.
func readAllContext(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}

// serverError returns err as a *Error, and fails the test if it is not one.
func serverError(t *testing.T, err error) *Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *Error", err, err)
	}
	return e
}

// dec makes a decimal from its text.
func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("reading %q as a decimal: %v", s, err)
	}
	return d
}

// equal reports whether two values are the same: a decimal by its text, and a
// time by its instant and its offset.
func equal(a, b any) bool {
	switch x := a.(type) {
	case *apd.Decimal:
		y, ok := b.(*apd.Decimal)
		return ok && x.String() == y.String()
	case time.Time:
		y, ok := b.(time.Time)
		if !ok {
			return false
		}
		_, ox := x.Zone()
		_, oy := y.Zone()
		return x.Equal(y) && ox == oy
	case []any:
		y, ok := b.([]any)
		return ok && slices.EqualFunc(x, y, equal)
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if w, ok := y[k]; !ok || !equal(v, w) {
				return false
			}
		}
		return true
	case []byte:
		y, ok := b.([]byte)
		return ok && x != nil == (y != nil) && string(x) == string(y)
	}
	return a == b
}

// checkRows fails the test for each row that differs from want.
func checkRows(t *testing.T, got, want [][]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !slices.EqualFunc(got[i], want[i], equal) {
			t.Errorf("row %d is\n%#v\nwant\n%#v", i, got[i], want[i])
		}
	}
}

// exchange reads the recorded exchange whose file name starts with its number,
// such as 26 for databricks-026-post--api-2-0-sql-statements.json.
func exchange(tb testing.TB, n int) *dbimptest.Exchange {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, fmt.Sprintf("databricks-%03d-*.json", n)))
	if err != nil || len(paths) != 1 {
		tb.Fatalf("finding the recorded exchange %d: %v, %v", n, paths, err)
	}
	ex, err := dbimptest.ReadExchange(paths[0])
	if err != nil {
		tb.Fatal(err)
	}
	return ex
}

// queryError runs a statement, reads its rows to the end, and returns the error that
// the statement or the rows gave. It is for a test that holds an error.
func queryError(ctx context.Context, db *sql.DB, query string, args ...any) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// script is a fake server that answers its requests with recorded exchanges.
// Each request takes the next answer of its list, or the last one when the
// list is used up. A request to a path that ends in /cancel gets the answer
// of cancels. The server keeps a log of the requests.
type script struct {
	t       testing.TB
	answers []*dbimptest.Exchange
	cancels *dbimptest.Exchange

	mu     sync.Mutex
	log    []string
	bodies []string
	// seen is closed when the server has answered its second request, which is
	// the first poll.
	seen chan struct{}
	once sync.Once
}

// newScript starts a script that answers with the exchanges n, in order. The
// answer to a cancel is the recorded answer to a cancel of a statement that
// runs, which is {}.
func newScript(t *testing.T, n ...int) (*script, *httptest.Server) {
	t.Helper()
	s := &script{t: t, cancels: exchange(t, 183), seen: make(chan struct{})}
	for _, i := range n {
		s.answers = append(s.answers, exchange(t, i))
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *script) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading a request to the fake server: %v", err)
	}
	s.mu.Lock()
	entry := r.Method + " " + r.URL.Path
	s.log = append(s.log, entry)
	s.bodies = append(s.bodies, string(body))
	i := len(s.log) - 1
	s.mu.Unlock()
	if i == 1 {
		s.once.Do(func() { close(s.seen) })
	}
	ex := s.cancels
	if !strings.HasSuffix(r.URL.Path, "/cancel") {
		ex = s.answers[min(i, len(s.answers)-1)]
	}
	for key, vals := range ex.Response.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.Header().Del("Content-Length")
	w.WriteHeader(ex.Response.Status)
	_, _ = w.Write(ex.Response.Content())
}

// requests returns the method and the path of each request that the server
// got.
func (s *script) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.log)
}

// cancelCount returns how many requests were cancels.
func (s *script) cancelCount() int {
	n := 0
	for _, entry := range s.requests() {
		if strings.HasSuffix(entry, "/cancel") {
			n++
		}
	}
	return n
}

// ensure the interface.
var _ http.Handler = (*script)(nil)
