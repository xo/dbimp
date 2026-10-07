package drill_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/drill"
)

// answer is an answer of one row with one BIGINT column a, with the id id, as
// the server writes it (measured).
func answer(id string) string {
	return head(id) + `{"a":1}` + "\n]\n" + `,"queryState":"COMPLETED"` + "\n}\n"
}

// head is the start of an answer of one BIGINT column a, up to the start of
// its rows.
func head(id string) string {
	return `{"queryId":"` + id + `"` + "\n" + `,"columns":["a"]` + "\n" + `,"metadata":["BIGINT"]` + "\n" +
		`,"attemptedAutoLimit":0` + "\n" + `,"rows":[` + "\n"
}

// optionServer is a fake server. It keeps the body of each query, the path of
// each cancel and each profile, and the headers of each request, and answers
// each query with one row. A query whose text is SLOW waits until its request
// ends, with no answer. One whose text is ROWS sends its head and one row, and
// then waits until its request ends. One whose text is FAIL sends two rows,
// and then FAILED. One whose text is NOREASON fails with no message and no
// rows.
type optionServer struct {
	mu       sync.Mutex
	bodies   []map[string]any
	cancels  []string
	profiles []string
	auth     []string
	cookies  []string
	ids      int
	// rowIDs are the ids of the queries ROWS, in order.
	rowIDs []string
	// late is true when the first read of each profile finds no error yet, as
	// the server writes the error after it ends the answer (measured).
	late bool
	// cookie is the name of the cookie of the session, which is
	// Drill-Session-Id on 1.22.0 and JSESSIONID on 1.21.2 (measured).
	cookie string
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, pass, _ := r.BasicAuth()
	name := s.cookie
	if name == "" {
		name = "Drill-Session-Id"
	}
	cookie := ""
	if c, err := r.Cookie(name); err == nil {
		cookie = c.Value
	}
	s.mu.Lock()
	s.auth = append(s.auth, user+":"+pass)
	s.cookies = append(s.cookies, cookie)
	s.ids++
	n := s.ids
	s.mu.Unlock()
	if cookie == "" {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "session" + strconv.Itoa(n)})
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/profiles/cancel/"):
		s.mu.Lock()
		s.cancels = append(s.cancels, strings.TrimPrefix(r.URL.Path, "/profiles/cancel/"))
		s.mu.Unlock()
		_, _ = io.WriteString(w, "Cancelled query on locally running node.")
		return
	case strings.HasPrefix(r.URL.Path, "/profiles/"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/profiles/"), ".json")
		s.mu.Lock()
		first := !slices.Contains(s.profiles, id)
		s.profiles = append(s.profiles, id)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if s.late && first {
			_, _ = io.WriteString(w, `{"id":{"part1":1},"state":4,"error":null,"verboseError":null}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":{"part1":1},"state":4,"error":"SYSTEM ERROR: Drill Remote Exception\n\n\nPlease, refer to logs for more information.","verboseError":"SYSTEM ERROR: Drill Remote Exception\n\n\n  (java.lang.NumberFormatException) x\n    org.apache.drill.exec.expr.fn.impl.StringFunctionHelpers.nfeI():93\n"}`)
		return
	case r.URL.Path == "/status.json":
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\n  \"status\" : \"Running!\"\n}")
		return
	}
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.bodies = append(s.bodies, m)
	s.mu.Unlock()
	id := "id-" + strconv.Itoa(n)
	if m["query"] == "ROWS" {
		s.mu.Lock()
		s.rowIDs = append(s.rowIDs, id)
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	switch m["query"] {
	case "SLOW":
		<-r.Context().Done()
		return
	case "ROWS":
		_, _ = io.WriteString(w, head(id)+`{"a":1}`+"\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
		<-r.Context().Done()
		return
	case "FAIL":
		_, _ = io.WriteString(w, head(id)+`{"a":1}`+"\n"+`,{"a":2}`+"\n]\n"+`,"queryState":"FAILED"`+"\n}\n")
		return
	case "NOREASON":
		_, _ = io.WriteString(w, `{"queryId":"`+id+`"`+"\n"+`,"queryState":"FAILED"`+"\n}\n")
		return
	}
	_, _ = io.WriteString(w, answer(id))
}

// open returns a database on the fake server, with the query of a DSN.
func (s *optionServer) open(t *testing.T, query string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://u:p@", 1)+query)
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

// options returns the options of a body.
func options(b map[string]any) map[string]any {
	m, _ := b["options"].(map[string]any)
	return m
}

// TestRequest holds D165: the body asks for SQL with the verbose option, and
// leaves out the schema and the limit that the DSN does not set.
func TestRequest(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	b := s.requests()[0]
	got, _ := json.Marshal(b, json.Deterministic(true))
	if want := `{"options":{"drill.exec.http.rest.errors.verbose":"true"},"query":"SELECT a FROM t","queryType":"SQL"}`; string(got) != want {
		t.Errorf("the body is %s, want %s", got, want)
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
		drill.WithSchema("dfs.tmp"),
		drill.WithAutoLimit(5),
		drill.WithParameter("dbimpKey", true),
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
	switch {
	case first["query"] != "SELECT a FROM t WHERE x = 1 AND y = 'it''s'":
		t.Errorf("the query is %q, want the arguments as literals in their order (D165)", first["query"])
	case first["defaultSchema"] != "dfs.tmp" || first["autoLimit"] != 5.0 || first["dbimpKey"] != true:
		t.Errorf("the first body is %v, want the schema, the limit and dbimpKey", first)
	case second["defaultSchema"] != nil || second["autoLimit"] != nil || second["dbimpKey"] != nil:
		t.Errorf("the second body is %v, want none of the options of the first", second)
	}
}

// TestOptionDSN holds that the keys of the DSN reach every query, and that
// WithOptions and WithDatabase apply after them.
func TestOptionDSN(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "?schema=cp&autolimit=100")
	ctx := drill.WithOptions(t.Context(), drill.WithAutoLimit(7))
	if _, err := db.ExecContext(ctx, "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", drill.WithDatabase("dfs.tmp")); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if r := reqs[0]; r["defaultSchema"] != "cp" || r["autoLimit"] != 7.0 {
		t.Errorf("the first query sent %v, want the schema of the DSN and the limit of the context", r)
	}
	if r := reqs[1]; r["defaultSchema"] != "dfs.tmp" || r["autoLimit"] != 100.0 {
		t.Errorf("the second query sent %v, want the schema of its option and the limit of the DSN", r)
	}
}

// TestOptionParameter holds that WithParameter("options") replaces the whole
// options that the driver sends, as D109 says for a key of the driver, so
// that the verbose option goes too.
func TestOptionParameter(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", drill.WithParameter("options", map[string]string{"exec.query.max_rows": "7"})); err != nil {
		t.Fatal(err)
	}
	if o := options(s.requests()[0]); len(o) != 1 || o["exec.query.max_rows"] != "7" {
		t.Errorf("the options are %v, want the options of WithParameter", o)
	}
}

// TestOptionValues holds that an option that the server cannot honor fails
// with dbimp.ErrNotSupported (D109), that an option with a value that the DSN
// refuses fails with dbimp.ErrInvalidValue, and that neither sends anything.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	for _, tt := range []struct {
		opt  drill.Option
		want error
	}{
		{drill.WithTimeout(time.Second), dbimp.ErrNotSupported},
		{drill.WithReadonly(true), dbimp.ErrNotSupported},
		{drill.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{drill.WithAutoLimit(-1), dbimp.ErrInvalidValue},
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", drill.WithReadonly(false), drill.WithTimeout(0), drill.WithAutoLimit(0)); err != nil {
		t.Errorf("WithReadonly(false), a timeout of zero and a limit of zero gave %v, want no error", err)
	}
	if reqs := s.requests(); len(reqs) != 1 {
		t.Errorf("the server received %v, want only the last query", reqs)
	}
}

// TestArguments holds D165 for the arguments that the driver refuses before
// it sends anything.
func TestArguments(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	for _, tt := range []struct {
		arg  any
		want error
	}{
		// database/sql refuses a slice before the driver sees it.
		{[]any{1}, nil},
		{dbimp.OffsetTime{}, dbimp.ErrNotSupported},
		{dbimp.Interval{Months: 1, Days: 1}, dbimp.ErrInvalidValue},
		{sql.Named("x", 1), dbimp.ErrArguments},
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

// TestResult holds that Exec reads the result, that it has no id of an insert,
// and that Ping runs a query.
func TestResult(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	res, err := db.ExecContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected gave %v, want dbimp.ErrNotSupported for a statement with no count", err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}
	if reqs := s.requests(); len(reqs) != 2 || reqs[1]["query"] != "SELECT 1 AS one FROM (VALUES(1))" {
		t.Errorf("Ping sent %v, want the query SELECT 1 AS one FROM (VALUES(1))", reqs)
	}
}

// TestErrorAfterRowsReadsTheProfile holds D165: an error after some rows has
// no message in the answer, so the driver reads the profile of the query by
// its queryId, and the error wraps dbimp.ErrIncomplete. An error before any
// row with no message reads the profile too, and wraps nothing.
func TestErrorAfterRowsReadsTheProfile(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	rows, err := db.QueryContext(t.Context(), "FAIL")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	err = rows.Err()
	e, ok := errors.AsType[*drill.Error](err)
	if n != 2 || !ok || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Fatalf("read %d rows and the error %v, want 2 rows and a *drill.Error that wraps dbimp.ErrIncomplete", n, err)
	}
	if e.Kind != "SYSTEM ERROR" || e.Exception != "java.lang.NumberFormatException" || !strings.HasSuffix(e.Message, "(java.lang.NumberFormatException) x") && !strings.HasSuffix(e.Message, ": x") {
		t.Errorf("the error is %+v, want the message and the exception from the profile", *e)
	}
	err = queryErr(t.Context(), db, "NOREASON")
	if e, ok := errors.AsType[*drill.Error](err); !ok || e.Kind != "SYSTEM ERROR" || errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("an error before any row gave %v, want a *drill.Error from the profile that does not wrap dbimp.ErrIncomplete", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.profiles) != 2 {
		t.Errorf("the driver read the profiles %q, want 2", s.profiles)
	}
}

// TestProfileWaitsForTheError holds that the driver reads the profile again
// when its first read finds no error, because the server ends the answer
// before it writes the error into the profile (measured).
func TestProfileWaitsForTheError(t *testing.T) {
	t.Parallel()
	s := &optionServer{late: true}
	db := s.open(t, "")
	err := queryErr(t.Context(), db, "NOREASON")
	if e, ok := errors.AsType[*drill.Error](err); !ok || e.Kind != "SYSTEM ERROR" {
		t.Errorf("a failure with a late profile gave %v, want a *drill.Error from the profile", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.profiles) != 2 {
		t.Errorf("the driver read the profile %d times, want 2", len(s.profiles))
	}
}

// TestCancel holds D165: when the context ends while the rows arrive, the
// driver cancels the query by the queryId of the first batch. Before the
// first batch it has no id, and sends no cancel. It sends no cancel for a
// query that it read to its end. Rows that the caller closes before the end
// cancel the query too.
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
	if got := s.cancelList(); len(got) != 0 {
		t.Errorf("the driver sent the cancels %q before the first batch, want none", got)
	}
	cancelRows(t, db)
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	closeEarly(t, db)
	s.mu.Lock()
	want := append([]string(nil), s.rowIDs...)
	s.mu.Unlock()
	if len(want) != 2 {
		t.Fatalf("the server received %d queries of ROWS, want 2", len(want))
	}
	if got := s.cancelList(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("the driver sent the cancels %q, want %q", got, want)
	}
}

// cancelList returns the ids of the cancels that the server received.
func (s *optionServer) cancelList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cancels...)
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

// closeEarly closes the rows of a query after its first row.
func closeEarly(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "ROWS")
	if err != nil {
		t.Fatal(err)
	}
	// The deferred Close ends the rows after the first one, and TestCancel
	// reads the cancel that it sent.
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reading the first row: %v", rows.Err())
	}
}

// TestSession holds D165: a connection keeps the cookie of its session, so
// that ALTER SESSION reaches the next statement of the connection, and two
// connections have two sessions. The cookie is named Drill-Session-Id on 1.22.0
// and JSESSIONID on 1.21.2 (measured), and the connection keeps either.
func TestSession(t *testing.T) {
	t.Parallel()
	for _, cookie := range []string{"Drill-Session-Id", "JSESSIONID"} {
		t.Run(cookie, func(t *testing.T) {
			t.Parallel()
			s := &optionServer{cookie: cookie}
			db := s.open(t, "")
			ctx := t.Context()
			c1, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer c1.Close()
			c2, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer c2.Close()
			for _, c := range []*sql.Conn{c1, c2, c1, c2, c1} {
				if _, err := c.ExecContext(ctx, "SELECT a FROM t"); err != nil {
					t.Fatal(err)
				}
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			got := s.cookies
			if len(got) != 5 || got[0] != "" || got[1] != "" || got[2] == "" || got[3] == "" || got[2] == got[3] || got[2] != got[4] {
				t.Errorf("the connections sent the cookies %q, want none at first, then one for each connection", got)
			}
		})
	}
}

// TestRedirect holds D165: the driver follows no redirect, so it sends the
// credentials to the host of the DSN only, and a redirect to the login page
// is the error of a wrong password.
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
		if r.URL.Path == "/status.json" {
			http.Redirect(w, r, target.URL+"/query.json", http.StatusTemporaryRedirect)
			return
		}
		http.Redirect(w, r, "/mainLogin?redirect=%2Fquery.json", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://u:secret@", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.ExecContext(t.Context(), "SELECT 1")
	if e, ok := errors.AsType[*drill.Error](err); !ok || e.HTTPStatus != http.StatusTemporaryRedirect || !errors.Is(err, drill.ErrLogin) {
		t.Errorf("a redirect to the login page gave %v, want the error of HTTP 307 that is drill.ErrLogin", err)
	}
	if strings.Contains(fmt.Sprint(err), "secret") {
		t.Errorf("the error %v holds the password", err)
	}
	if err := db.PingContext(t.Context()); err == nil {
		t.Error("a redirect to another host gave no error")
	}
	other.Lock()
	defer other.Unlock()
	if reached {
		t.Error("the driver followed the redirect to another host")
	}
}

// TestStatusErrors holds the errors of a request that the server cannot
// start (measured): the message of the JSON, the text of a plain answer, and
// the text of the status for a page of HTML.
func TestStatusErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		status int
		ctype  string
		body   string
		want   string
	}{
		{http.StatusInternalServerError, "application/json", "{\n  \"errorMessage\" : \"Query submission failed\"\n}", "drill: 500: Query submission failed"},
		{http.StatusBadRequest, "text/plain", "Unrecognized field \"x\"", "drill: 400: Unrecognized field \"x\""},
		{http.StatusNotFound, "text/html", "<html>nope</html>", "drill: 404: Not Found"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tt.ctype)
			w.WriteHeader(tt.status)
			_, _ = io.WriteString(w, tt.body)
		}))
		db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://", 1))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(t.Context(), "SELECT 1")
		var se *dbimp.StatusError
		if err == nil || err.Error() != tt.want || !errors.As(err, &se) || se.Code != tt.status {
			t.Errorf("status %d gave %v, want %q that wraps the status", tt.status, err, tt.want)
		}
		db.Close()
		srv.Close()
	}
}

// TestLargeResult holds D25: the driver reads a result of 64 MiB one row at
// a time, and the memory in use stays far below the size of the result. It
// does not run in parallel, so that the memory of other tests does not count.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const rowsCount = 1 << 20
	row := `{"s":"` + strings.Repeat("x", 40) + `"}` + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"queryId":"x","columns":["s"],"metadata":["VARCHAR"],"attemptedAutoLimit":0,"rows":[`+"\n")
		for i := range rowsCount {
			sep := ","
			if i == 0 {
				sep = ""
			}
			if _, err := io.WriteString(w, sep+row); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `],"queryState":"COMPLETED"}`)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rows, err := db.QueryContext(t.Context(), "SELECT s FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var (
		n    int
		peak uint64
		s    string
	)
	for rows.Next() {
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		if n++; n%(rowsCount/4) == 0 {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapInuse)
		}
	}
	if err := rows.Err(); err != nil || n != rowsCount {
		t.Fatalf("read %d rows and %v, want %d rows", n, err, rowsCount)
	}
	if peak > 16<<20 {
		t.Errorf("the heap in use reached %d bytes while the driver read a result of 64 MiB, want at most 16 MiB (D25)", peak)
	}
}
