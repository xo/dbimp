package clickhouse_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/clickhouse"
	"github.com/xo/dbimp/dbimptest"
)

// request is a request that a fake server received.
type request struct {
	method string
	query  url.Values
	header http.Header
	body   string
}

// fake is a server that answers with a function and keeps the requests that it
// received. A request of SELECT version(), which each connection sends, gets the
// version of 25.8 and is not kept, so that a test counts only its own.
type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []request
}

// newFake starts a fake server, which closes with the test.
func newFake(t *testing.T, h func(w http.ResponseWriter, r *http.Request, req request)) *fake {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		req := request{r.Method, r.URL.Query(), r.Header.Clone(), string(b)}
		if req.body == "SELECT version()" {
			_, _ = io.WriteString(w, "[\"version()\"]\n[\"String\"]\n[\"25.8.33.6\"]\n")
			return
		}
		f.mu.Lock()
		f.reqs = append(f.reqs, req)
		f.mu.Unlock()
		h(w, r, req)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// requests returns the requests that arrived so far.
func (f *fake) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.reqs...)
}

// open opens a database on the server, which closes with the test. The DSN has
// the user u and the password p.
func (f *fake) open(t *testing.T, path string) *sql.DB {
	t.Helper()
	cfg, err := clickhouse.ParseDSN(strings.Replace(f.srv.URL, "http://", "clickhouse://u:p@", 1) + path)
	if err != nil {
		t.Fatal(err)
	}
	c := clickhouse.NewConnector(*cfg)
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	return db
}

// head is the first two lines of an answer of one Int64 column a.
const head = "[\"a\"]\n[\"Int64\"]\n"

// one answers every request with one row.
func one(w http.ResponseWriter, _ *http.Request, _ request) {
	_, _ = io.WriteString(w, head+"[1]\n")
}

// kills returns the requests that cancel a query.
func kills(reqs []request) []request {
	var out []request
	for _, r := range reqs {
		if strings.HasPrefix(r.body, "KILL QUERY") {
			out = append(out, r)
		}
	}
	return out
}

// waitFor polls until cond is true, for at most 10 seconds, and fails the test
// when it is not. It never sleeps for a fixed time (step 14a).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen in 10 seconds", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// slowRows answers with one row, then waits until the request ends.
func slowRows(w http.ResponseWriter, r *http.Request, _ request) {
	_, _ = io.WriteString(w, head+"[1]\n")
	_ = http.NewResponseController(w).Flush()
	<-r.Context().Done()
}

// TestRequest holds what each request carries (D176 and D177): a POST to / with
// the statement as its body, Basic authentication and no header of the user, the
// format and the five settings, the name of the query, and the database.
func TestRequest(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	db := f.open(t, "/mydb")
	var a int
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) == 0 {
		t.Fatal("no request")
	}
	req := reqs[0]
	if req.method != http.MethodPost || req.body != "SELECT 1" {
		t.Errorf("the request is %s with the body %q, want POST with the statement", req.method, req.body)
	}
	for key, want := range map[string]string{
		"default_format": "JSONCompactEachRowWithNamesAndTypes",
		"output_format_json_quote_64bit_integers":    "0",
		"output_format_json_quote_denormals":         "1",
		"date_time_output_format":                    "iso",
		"output_format_json_named_tuples_as_objects": "0",
		"http_write_exception_in_output_format":      "0",
		"enable_http_compression":                    "0",
		"database":                                   "mydb",
	} {
		if got := req.query.Get(key); got != want {
			t.Errorf("the key %s is %q, want %q (D176)", key, got, want)
		}
	}
	if q := req.query.Get("query_id"); !strings.HasPrefix(q, "dbimp-") || len(q) < 20 {
		t.Errorf("the query_id is %q, want a name that the driver made", q)
	}
	if req.query.Has("wait_end_of_query") {
		t.Error("the request sets wait_end_of_query, which D176 leaves out, so that a result streams")
	}
	if user, pass, ok := parseBasic(req.header); !ok || user != "u" || pass != "p" {
		t.Errorf("the Authorization header holds %q and %q, want Basic with u and p (D177)", user, pass)
	}
	for _, h := range []string{"X-Clickhouse-User", "X-Clickhouse-Key", "X-Clickhouse-Database"} {
		if req.header.Get(h) != "" {
			t.Errorf("the request has the header %s, which the server refuses with a Basic header (D177)", h)
		}
	}
	if req.header.Get("Accept-Encoding") != "gzip" {
		t.Errorf("Accept-Encoding is %q, want the gzip that the transport asks for", req.header.Get("Accept-Encoding"))
	}
}

// parseBasic returns the user and the password of the header Authorization.
func parseBasic(h http.Header) (string, string, bool) {
	r := &http.Request{Header: h}
	return r.BasicAuth()
}

// TestNoCredentials holds that a DSN with no user and no password sends no
// header Authorization, so the server uses its user default.
func TestNoCredentials(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	cfg, err := clickhouse.ParseDSN(strings.Replace(f.srv.URL, "http://", "clickhouse://", 1))
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(clickhouse.NewConnector(*cfg))
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got := f.requests()[0].header.Get("Authorization"); got != "" {
		t.Errorf("the request sends %q, want no Authorization header", got)
	}
}

// TestKillsWhenTheContextEnds holds D176: when the context ends while rows flow,
// the driver sends KILL QUERY for the query that it named, with the user and the
// password, and the read fails with the error of the context.
func TestKillsWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, req request) {
		if strings.HasPrefix(req.body, "KILL QUERY") {
			_, _ = io.WriteString(w, "[\"kill_status\"]\n[\"String\"]\n[\"finished\"]\n")
			return
		}
		slowRows(w, r, req)
	})
	db := f.open(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT sleep(10)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no first row: %v", rows.Err())
	}
	cancel()
	for rows.Next() {
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	rows.Close()
	waitFor(t, "the cancel", func() bool { return len(kills(f.requests())) > 0 })
	reqs := f.requests()
	k := kills(reqs)[0]
	if id := reqs[0].query.Get("query_id"); id == "" || k.query.Get("param_id") != id {
		t.Errorf("the cancel names %q, want the query_id %q of the query", k.query.Get("param_id"), id)
	}
	if k.body != "KILL QUERY WHERE query_id = {id:String} SYNC" {
		t.Errorf("the cancel is %q", k.body)
	}
	if user, pass, ok := parseBasic(k.header); !ok || user != "u" || pass != "p" {
		t.Errorf("the cancel sends the user %q and the password %q, want Basic with u and p", user, pass)
	}
	if n := len(kills(f.requests())); n != 1 {
		t.Errorf("the driver sent %d cancels, want 1", n)
	}
}

// TestKillsWhenTheContextEndsBeforeTheAnswer holds that a query whose answer has
// not begun is cancelled on the server when the context ends, because the server
// runs it on after the client left.
func TestKillsWhenTheContextEndsBeforeTheAnswer(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, req request) {
		if strings.HasPrefix(req.body, "KILL QUERY") {
			return
		}
		<-r.Context().Done()
	})
	db := f.open(t, "")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := db.ExecContext(ctx, "SELECT sleep(10)")
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) {
		t.Errorf("the error is %v, want context.DeadlineExceeded and never driver.ErrBadConn", err)
	}
	waitFor(t, "the cancel", func() bool { return len(kills(f.requests())) > 0 })
}

// TestKillsWhenTheRowsCloseEarly holds D176: rows that the caller closes before
// the end cancel the query, and rows read to the end do not.
func TestKillsWhenTheRowsCloseEarly(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, req request) {
		switch {
		case strings.HasPrefix(req.body, "KILL QUERY"):
			_, _ = io.WriteString(w, "[\"kill_status\"]\n[\"String\"]\n[\"finished\"]\n")
		case req.body == "SELECT done":
			_, _ = io.WriteString(w, head+"[1]\n[2]\n")
		default:
			slowRows(w, r, req)
		}
	})
	db := f.open(t, "")
	rows, err := db.QueryContext(t.Context(), "SELECT done")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
	}
	if err := rows.Close(); err != nil || rows.Err() != nil { //nolint:sqlclosecheck // The test closes the rows here, to see what Close sends.
		t.Fatalf("closing the rows: %v, %v", err, rows.Err())
	}
	if n := len(kills(f.requests())); n != 0 {
		t.Fatalf("rows read to the end sent %d cancels, want none", n)
	}
	rows, err = db.QueryContext(t.Context(), "SELECT slow")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatalf("no first row: %v", rows.Err())
	}
	if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test closes the rows before the end, to see what Close sends.
		t.Fatal(err)
	}
	if n := len(kills(f.requests())); n != 1 {
		t.Errorf("rows closed early sent %d cancels, want 1 before Close returned", n)
	}
	reqs := f.requests()
	var slowID string
	for _, r := range reqs {
		if r.body == "SELECT slow" {
			slowID = r.query.Get("query_id")
		}
	}
	if k := kills(reqs); len(k) != 1 || k[0].query.Get("param_id") != slowID {
		t.Errorf("the cancels are %v, want one for %q", k, slowID)
	}
}

// TestQueryIDOption holds that a caller who names the query with WithParameter
// gets that query cancelled.
func TestQueryIDOption(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, req request) {
		if strings.HasPrefix(req.body, "KILL QUERY") {
			return
		}
		slowRows(w, r, req)
	})
	db := f.open(t, "")
	rows, err := db.QueryContext(t.Context(), "SELECT slow", clickhouse.WithParameter("query_id", "mine-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatalf("no first row: %v", rows.Err())
	}
	if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test closes the rows before the end, to see what Close sends.
		t.Fatal(err)
	}
	reqs := f.requests()
	if got := reqs[0].query.Get("query_id"); got != "mine-1" {
		t.Errorf("the query_id is %q, want mine-1", got)
	}
	if k := kills(reqs); len(k) != 1 || k[0].query.Get("param_id") != "mine-1" {
		t.Errorf("the cancels are %v, want one for mine-1", k)
	}
}

// TestNoRedirect holds D177: the driver follows no redirect, and sends the
// credentials to the host of the DSN only.
func TestNoRedirect(t *testing.T) {
	t.Parallel()
	other := newFake(t, one)
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, _ request) {
		http.Redirect(w, r, other.srv.URL+"/", http.StatusTemporaryRedirect)
	})
	db := f.open(t, "")
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if err == nil {
		t.Fatal("a redirect gave no error")
	}
	if e := (*clickhouse.Error)(nil); !errors.As(err, &e) || e.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("the error is %v, want a *clickhouse.Error with HTTP 307", err)
	}
	if n := len(other.requests()); n != 0 {
		t.Errorf("the host that the redirect named got %d requests, want none", n)
	}
}

// TestNoRetry holds D8: HTTP 429 and HTTP 503 are errors that reach the caller,
// and the driver sends the request once.
func TestNoRetry(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		f := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ request) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "busy", status)
		})
		db := f.open(t, "")
		_, err := db.ExecContext(t.Context(), "SELECT 1")
		se, ok := errors.AsType[*dbimp.StatusError](err)
		if !ok || se.Code != status {
			t.Errorf("HTTP %d: the error is %v, want one that wraps a *dbimp.StatusError", status, err)
		}
		if n := len(f.requests()); n != 1 {
			t.Errorf("HTTP %d: the server got %d requests, want 1", status, n)
		}
	}
}

// TestNothingSentForABadStatement holds that a statement whose arguments do not
// match sends no request, and that a context that ended sends none.
func TestNothingSentForABadStatement(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	db := f.open(t, "")
	if _, err := db.ExecContext(t.Context(), "SELECT ?, ?", 1); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("the error is %v, want dbimp.ErrArguments", err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT ?", struct{}{}); err == nil {
		t.Error("an argument of a type with no parameter gave no error")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.ExecContext(ctx, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("the server got %d requests, want none", n)
	}
}

// TestParametersReachTheServer holds D176: a statement with arguments sends the
// typed parameters, and the values are in the query string.
func TestParametersReachTheServer(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	db := f.open(t, "")
	if _, err := db.ExecContext(t.Context(), "INSERT INTO t VALUES (?, ?, @x)", int64(7), "a b", sql.Named("x", 1.5)); err != nil {
		t.Fatal(err)
	}
	req := f.requests()[0]
	if want := "INSERT INTO t VALUES ({p1:Int64}, {p2:String}, {p3:Float64})"; req.body != want {
		t.Errorf("the statement is %q, want %q", req.body, want)
	}
	for k, want := range map[string]string{"param_p1": "7", "param_p2": "a b", "param_p3": "1.5"} {
		if got := req.query.Get(k); got != want {
			t.Errorf("%s is %q, want %q", k, got, want)
		}
	}
}

// TestOptions holds D109 for each option: it reaches the server as the setting
// that it names, a later one wins, and a value that the DSN would refuse is an
// error.
func TestOptions(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	db := f.open(t, "/dsndb")
	run := func(ctx context.Context, args ...any) request {
		t.Helper()
		before := len(f.requests())
		if _, err := db.ExecContext(ctx, "SELECT 1", args...); err != nil {
			t.Fatal(err)
		}
		reqs := f.requests()
		if len(reqs) != before+1 {
			t.Fatalf("the server got %d requests, want 1", len(reqs)-before)
		}
		return reqs[before]
	}
	if got := run(t.Context()).query.Get("database"); got != "dsndb" {
		t.Errorf("the database is %q, want the one of the DSN", got)
	}
	r := run(t.Context(), clickhouse.WithDatabase("argdb"), clickhouse.WithTimeout(1500*time.Millisecond), clickhouse.WithReadonly(true),
		clickhouse.WithParameter("max_threads", 3), clickhouse.WithParameter("use_query_cache", true), clickhouse.WithParameter("output_format_json_quote_denormals", false))
	for k, want := range map[string]string{
		"database":                           "argdb",
		"max_execution_time":                 "1.5",
		"readonly":                           "1",
		"max_threads":                        "3",
		"use_query_cache":                    "1",
		"output_format_json_quote_denormals": "0",
	} {
		if got := r.query.Get(k); got != want {
			t.Errorf("%s is %q, want %q", k, got, want)
		}
	}
	// An option of the context comes before an argument.
	ctx := clickhouse.WithOptions(t.Context(), clickhouse.WithDatabase("ctxdb"), clickhouse.WithParameter("max_threads", 1))
	r = run(ctx, clickhouse.WithParameter("max_threads", 2))
	if r.query.Get("database") != "ctxdb" || r.query.Get("max_threads") != "2" {
		t.Errorf("the database is %q and max_threads is %q, want ctxdb and 2", r.query.Get("database"), r.query.Get("max_threads"))
	}
	// The database of the DSN is back for the next statement, and no option stays.
	r = run(t.Context())
	if r.query.Get("database") != "dsndb" || r.query.Has("max_threads") || r.query.Has("readonly") || r.query.Has("max_execution_time") {
		t.Errorf("the next statement has %v, want the database of the DSN and no option", r.query)
	}
	// An empty database removes the key.
	if run(t.Context(), clickhouse.WithDatabase("")).query.Has("database") {
		t.Error("WithDatabase with no name left the key database")
	}
	for name, opt := range map[string]clickhouse.Option{
		"a negative timeout": clickhouse.WithTimeout(-time.Second),
		"no value":           clickhouse.WithParameter("max_threads", nil),
		"no name":            clickhouse.WithParameter("", 1),
		"a name with an &":   clickhouse.WithParameter("a&b", 1),
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT 1", opt); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("%s: the error is %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
}

// TestTwoQueriesAtOnce holds that two queries run at the same time on one
// database, each on a connection of its own.
func TestTwoQueriesAtOnce(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ request) {
		arrived <- struct{}{}
		<-release
		_, _ = io.WriteString(w, head+"[1]\n")
	})
	db := f.open(t, "")
	for range 2 {
		wg.Go(func() {
			var a int
			if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
				t.Error(err)
			}
		})
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("the second query did not reach the server while the first one ran")
		}
	}
	close(release)
	wg.Wait()
}

// TestConnectorOwnsItsTransport holds that Close of the connector lets go of the
// idle connections, so that no goroutine is left running (step 12).
func TestConnectorOwnsItsTransport(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) == "SELECT version()" {
			_, _ = io.WriteString(w, "[\"v\"]\n[\"String\"]\n[\"25.8.33.6\"]\n")
			return
		}
		_, _ = io.WriteString(w, head+"[1]\n")
	}))
	t.Cleanup(srv.Close)
	cfg, err := clickhouse.ParseDSN(strings.Replace(srv.URL, "http://", "clickhouse://", 1))
	if err != nil {
		t.Fatal(err)
	}
	c := clickhouse.NewConnector(*cfg)
	db := sql.OpenDB(c)
	var a int
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("closing the connector a second time: %v", err)
	}
}
