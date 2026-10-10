package druid_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/druid"
)

// answer is an answer of one row in arrayLines, with one BIGINT column a
// (measured).
const answer = "[\"a\"]\n[\"LONG\"]\n[\"BIGINT\"]\n[1]\n\n"

// optionServer is a fake server. It keeps the body of each query, the path of
// each cancel and the header Authorization of each request, and answers each
// query with one row. A query whose text is SLOW waits until its request
// ends, and one whose text is ROWS sends one row, and then waits until its
// request ends.
type optionServer struct {
	mu      sync.Mutex
	bodies  []map[string]any
	cancels []string
	auth    []string
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, _ := r.BasicAuth()
	s.mu.Lock()
	s.auth = append(s.auth, user+":"+pass)
	s.mu.Unlock()
	if r.Method == http.MethodDelete {
		s.mu.Lock()
		s.cancels = append(s.cancels, r.URL.Path)
		s.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		return
	}
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.bodies = append(s.bodies, m)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch m["query"] {
	case "SLOW":
		<-r.Context().Done()
		return
	case "ROWS":
		_, _ = io.WriteString(w, "[\"a\"]\n[\"LONG\"]\n[\"BIGINT\"]\n[1]\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
		<-r.Context().Done()
		return
	}
	_, _ = io.WriteString(w, answer)
}

// open returns a database on the fake server, with the query of a DSN.
func (s *optionServer) open(t *testing.T, query string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(druid.Name, strings.Replace(srv.URL, "http://", "druid://u:p@", 1)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// requests returns the bodies that the server received, and forgets them.
func (s *optionServer) requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bodies
	s.bodies = nil
	return b
}

// queryContext returns the context of a body.
func queryContext(b map[string]any) map[string]any {
	m, _ := b["context"].(map[string]any)
	return m
}

// TestRequest holds D164: the body asks for arrayLines with the three rows
// of the header, and gives the query an id of its own and the time zone.
func TestRequest(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	for range 2 {
		if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err != nil {
			t.Fatal(err)
		}
	}
	reqs := s.requests()
	first, second := reqs[0], reqs[1]
	switch fc, sc := queryContext(first), queryContext(second); {
	case first["resultFormat"] != "arrayLines" || first["header"] != true || first["typesHeader"] != true || first["sqlTypesHeader"] != true:
		t.Errorf("the body is %v, want arrayLines with the three rows of the header", first)
	case first["parameters"] != nil:
		t.Errorf("the body is %v, want no parameters for a query with no argument", first)
	case fc["sqlTimeZone"] != "UTC" || fc["timeout"] != nil:
		t.Errorf("the context is %v, want the time zone UTC and no timeout", fc)
	case !strings.HasPrefix(fmt.Sprint(fc["sqlQueryId"]), "dbimp-") || fc["sqlQueryId"] == sc["sqlQueryId"]:
		t.Errorf("the ids of the queries are %v and %v, want an id of its own for each", fc["sqlQueryId"], sc["sqlQueryId"])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.auth {
		if a != "u:p" {
			t.Errorf("the request sent the credentials %q, want u:p", a)
		}
	}
}

// TestOptionArguments holds that the options of one statement reach its body,
// that the other arguments keep their order, and that the next statement has
// none of them (D109).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	var a int64
	err := db.QueryRowContext(t.Context(), "SELECT a FROM t WHERE x = ? AND y = ?", int64(1),
		druid.WithTimeout(1500*time.Microsecond),
		druid.WithTimeZone("Asia/Jakarta"),
		druid.WithReadonly(true),
		druid.WithParameter("dbimpKey", true),
		"it's").Scan(&a)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("the server received %d requests, want 2", len(reqs))
	}
	first, second := reqs[0], reqs[1]
	fc, sc := queryContext(first), queryContext(second)
	params, _ := json.Marshal(first["parameters"], json.Deterministic(true))
	switch {
	case string(params) != `[{"type":"BIGINT","value":1},{"type":"VARCHAR","value":"it's"}]`:
		t.Errorf("the parameters are %s, want the two arguments in their order (D164)", params)
	case fc["timeout"] != 2.0 || fc["sqlTimeZone"] != "Asia/Jakarta" || first["dbimpKey"] != true:
		t.Errorf("the first body is %v, want a timeout of 2 ms, the time zone and dbimpKey", first)
	case sc["timeout"] != nil || sc["sqlTimeZone"] != "UTC" || second["dbimpKey"] != nil:
		t.Errorf("the second body is %v, want none of the options of the first", second)
	}
}

// TestOptionDSN holds that the keys of the DSN reach every query, and that
// WithOptions applies after them.
func TestOptionDSN(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "?timezone=America%2FNew_York&timeout=3s")
	ctx := druid.WithOptions(t.Context(), druid.WithTimeout(time.Second))
	if _, err := db.ExecContext(ctx, "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", druid.WithTimeZone("UTC")); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if c := queryContext(reqs[0]); c["sqlTimeZone"] != "America/New_York" || c["timeout"] != 1000.0 {
		t.Errorf("the first query sent %v, want the time zone of the DSN and a timeout of 1000 ms", c)
	}
	if c := queryContext(reqs[1]); c["sqlTimeZone"] != "UTC" || c["timeout"] != 3000.0 {
		t.Errorf("the second query sent %v, want the time zone of its option and the timeout of the DSN", c)
	}
}

// TestOptionContext holds that WithParameter("context") replaces the whole
// context that the driver sends, as D109 says for a key of the driver.
func TestOptionContext(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", druid.WithParameter("context", map[string]any{"sqlOuterLimit": 5})); err != nil {
		t.Fatal(err)
	}
	if c := queryContext(s.requests()[0]); len(c) != 1 || c["sqlOuterLimit"] != 5.0 {
		t.Errorf("the context is %v, want the context of WithParameter", c)
	}
}

// TestOptionValues holds that WithDatabase fails with dbimp.ErrNotSupported
// (D164), that an option with a value that the DSN refuses fails with
// dbimp.ErrInvalidValue, and that neither sends anything.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	for _, tt := range []struct {
		opt  druid.Option
		want error
	}{
		{druid.WithDatabase("druid"), dbimp.ErrNotSupported},
		{druid.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{druid.WithTimeZone(""), dbimp.ErrInvalidValue},
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", druid.WithReadonly(false), druid.WithTimeout(0)); err != nil {
		t.Errorf("WithReadonly(false) and a timeout of zero gave %v, want no error", err)
	}
	if reqs := s.requests(); len(reqs) != 1 {
		t.Errorf("the server received %v, want only the last query", reqs)
	}
}

// TestArguments holds D164 for the arguments that the driver refuses before
// it sends anything.
func TestArguments(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	for _, tt := range []struct {
		arg  any
		want error
	}{
		{sql.Named("x", 1), dbimp.ErrArguments},
		{[]byte{1}, dbimp.ErrNotSupported},
		{map[string]any{}, nil},
	} {
		_, err := db.ExecContext(t.Context(), "SELECT a FROM t WHERE x = ?", tt.arg)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("the argument %#v gave %v, want %v", tt.arg, err, tt.want)
		}
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}

// TestResult holds D163: Exec reads the result, and the result has no count
// of rows and no id of an insert.
func TestResult(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	res, err := db.ExecContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected gave %v, want dbimp.ErrNotSupported", err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}
	if q := s.requests()[1]["query"]; q != "SELECT 1" {
		t.Errorf("Ping sent %q, want SELECT 1", q)
	}
}

// TestCancel holds D164: when the context ends before the answer arrives, or
// while the rows arrive, the driver cancels the query by the id that it sent,
// and it sends no cancel for a query that it read to its end or closed.
func TestCancel(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, err := db.ExecContext(ctx, "SLOW")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	cancelRows(t, db)
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	closeEarly(t, db)
	reqs := s.requests()
	var want []string
	// The queries that were cancelled are the first, the second and the
	// fourth: the one that ran to its end needs no cancel.
	for _, i := range []int{0, 1, 3} {
		b := reqs[i]
		want = append(want, "/druid/v2/sql/"+fmt.Sprint(queryContext(b)["sqlQueryId"]))
	}
	s.mu.Lock()
	got := s.cancels
	s.mu.Unlock()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the driver sent the cancels %q, want %q", got, want)
	}
}

// cancelRows reads the first row of a query, and then ends its context.
func cancelRows(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := db.QueryContext(ctx, "ROWS")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reading the first row: %v", rows.Err())
	}
	cancel()
	for rows.Next() {
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the error of the rows is %v, want context.Canceled", err)
	}
}

// closeEarly closes the rows of a query before it reads them.
func closeEarly(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if err := rows.Err(); err != nil {
		t.Error(err)
	}
}

// TestRedirect holds D164: the driver follows no redirect, so it sends the
// credentials to the host of the DSN only, and a redirect is an error.
func TestRedirect(t *testing.T) {
	t.Parallel()
	var other sync.Mutex
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		other.Lock()
		reached = true
		other.Unlock()
	}))
	t.Cleanup(target.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/druid/v2/sql", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(druid.Name, strings.Replace(srv.URL, "http://", "druid://u:secret@", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.ExecContext(t.Context(), "SELECT 1")
	if e, ok := errors.AsType[*druid.Error](err); !ok || e.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("a redirect gave %v, want the error of HTTP 307", err)
	}
	other.Lock()
	defer other.Unlock()
	if reached {
		t.Error("the driver followed the redirect to another host")
	}
}

// TestLargeResult holds D25: the driver reads a result of 64 MiB one row at
// a time, and the memory in use stays far below the size of the result. It
// does not run in parallel, so that the memory of other tests does not count.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const rowsCount = 1 << 20
	row := "[" + `"` + strings.Repeat("x", 58) + `"` + "]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[\"s\"]\n[\"STRING\"]\n[\"VARCHAR\"]\n")
		for range rowsCount {
			if _, err := io.WriteString(w, row); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, "\n")
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(druid.Name, strings.Replace(srv.URL, "http://", "druid://", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rows, err := db.QueryContext(t.Context(), "SELECT s FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	gauge := dbimptest.NewHeapGauge()
	var (
		n int
		s string
	)
	for rows.Next() {
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		if n++; n%(rowsCount/4) == 0 {
			gauge.Sample()
		}
	}
	if err := rows.Err(); err != nil || n != rowsCount {
		t.Fatalf("read %d rows and %v, want %d rows", n, err, rowsCount)
	}
	gauge.Check(t)
}
