package solr_test

import (
	"database/sql"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/solr"
)

// optionServer is a fake server. It keeps the path, the form and the
// headers of each request, and answers each statement with one row of one
// BIGINT column a.
type optionServer struct {
	mu       sync.Mutex
	paths    []string
	forms    []url.Values
	headers  []http.Header
	response string
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(b))
	s.mu.Lock()
	s.paths = append(s.paths, r.URL.Path)
	s.forms = append(s.forms, form)
	s.headers = append(s.headers, r.Header.Clone())
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if s.response != "" {
		_, _ = io.WriteString(w, s.response)
		return
	}
	_, _ = io.WriteString(w, head("a")+`{"a":1},`+eof)
}

// open returns a database on the fake server, with the path and the query of
// a DSN.
func (s *optionServer) open(t *testing.T, pathAndQuery string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(solr.Name, strings.Replace(srv.URL, "http://", "solr://u:p@", 1)+pathAndQuery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// requests returns the forms that the server received, and forgets them.
func (s *optionServer) requests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.forms
	s.forms, s.paths, s.headers = nil, nil, nil
	return f
}

// TestRequest holds D166: a statement is the form field stmt of a POST to the
// path of the collection of the DSN, with includeMetadata and the
// aggregationMode of the DSN.
func TestRequest(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	if _, err := db.ExecContext(t.Context(), "SELECT a"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	form, path, h := s.forms[0], s.paths[0], s.headers[0]
	want := url.Values{"stmt": {"SELECT a"}, "includeMetadata": {"true"}, "aggregationMode": {"facet"}}
	switch {
	case !reflect.DeepEqual(form, want):
		t.Errorf("the form is %v, want %v", form, want)
	case path != "/solr/c/sql":
		t.Errorf("the path is %q, want /solr/c/sql", path)
	case h.Get("Content-Type") != "application/x-www-form-urlencoded":
		t.Errorf("the content type is %q, want a form", h.Get("Content-Type"))
	case h.Get("Authorization") != "Basic dTpw":
		t.Errorf("the header Authorization is %q, want basic authentication of u:p", h.Get("Authorization"))
	}
}

// TestArguments holds D166: the server binds no argument, so the driver
// writes each one into the statement as a literal, in the order of its ?.
func TestArguments(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	dec, _, err := apd.NewFromString("12345678901234567890.5")
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 1, 12, 34, 56, 789_000_000, time.FixedZone("x", 7*3600))
	_, err = db.ExecContext(t.Context(), "SELECT a WHERE a = ? AND b = ? AND c = ? AND d = ? AND e = ? AND f = ? AND g = ? AND h = ? AND i = ? AND j = ? -- ? '?'",
		int64(-5), 1.5, "it's", true, nil, day, []byte{0, 1, 2, 0xff},
		uuid.MustParse("8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b"), dec, dbimp.DateOf(day))
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT a WHERE a = -5 AND b = 1.5 AND c = 'it''s' AND d = TRUE AND e = NULL AND f = '2026-10-01T05:34:56.789Z' AND g = 'AAEC/w==' AND h = '8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b' AND i = 12345678901234567890.5 AND j = '2026-10-01T00:00:00.000Z' -- ? '?'"
	if got := s.requests()[0].Get("stmt"); got != want {
		t.Errorf("the statement is\n%s\nwant\n%s", got, want)
	}
}

// TestArgumentsRefused holds D34 and D166 for the arguments that the driver
// refuses before it sends anything.
func TestArgumentsRefused(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	for _, tt := range []struct {
		query string
		args  []any
		want  error
	}{
		{"SELECT a WHERE x = ?", []any{sql.Named("x", 1)}, dbimp.ErrArguments},
		{"SELECT a WHERE x = ?", nil, dbimp.ErrArguments},
		{"SELECT a WHERE x = 1", []any{1}, dbimp.ErrArguments},
		{"SELECT a WHERE x = ?", []any{map[string]any{}}, nil},
		{"SELECT a WHERE x = ?", []any{math.NaN()}, dbimp.ErrNotSupported},
	} {
		_, err := db.ExecContext(t.Context(), tt.query, tt.args...)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("%q with %#v gave %v, want %v", tt.query, tt.args, err, tt.want)
		}
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}

// TestOptionArguments holds that the options of one statement reach its form,
// that the other arguments keep their order, and that the next statement has
// none of them (D109).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	var a int64
	err := db.QueryRowContext(t.Context(), "SELECT a WHERE x = ? AND y = ?", int64(1),
		solr.WithReadonly(true),
		solr.WithParameter("numWorkers", 2),
		solr.WithParameter("workerCollection", "w"),
		solr.WithParameter("zs", []string{"a", "b"}),
		solr.WithParameter("includeMetadata", "true"),
		"it's").Scan(&a)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT a").Scan(&a); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	first, second := reqs[0], reqs[1]
	switch {
	case first.Get("stmt") != "SELECT a WHERE x = 1 AND y = 'it''s'":
		t.Errorf("the statement is %q, want the two arguments in their order", first.Get("stmt"))
	case first.Get("numWorkers") != "2" || first.Get("workerCollection") != "w" || !reflect.DeepEqual(first["zs"], []string{"a", "b"}):
		t.Errorf("the form is %v, want the parameters of WithParameter", first)
	case second.Has("numWorkers") || second.Has("zs"):
		t.Errorf("the second form is %v, want none of the options of the first", second)
	}
}

// TestOptionParameterReplaces holds D109: a key named by WithParameter
// replaces the key that the driver sets, as the mode that the DSN refuses.
func TestOptionParameterReplaces(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	if _, err := db.ExecContext(t.Context(), "SELECT a", solr.WithParameter("aggregationMode", "map_reduce"), solr.WithParameter("includeMetadata", false)); err != nil {
		t.Fatal(err)
	}
	if f := s.requests()[0]; f.Get("aggregationMode") != "map_reduce" || f.Get("includeMetadata") != "false" {
		t.Errorf("the form is %v, want aggregationMode map_reduce and includeMetadata false", f)
	}
}

// TestOptionContext holds that WithOptions applies to each statement of its
// context, and WithDatabase names the collection of the path (D166).
func TestOptionContext(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	ctx := solr.WithOptions(t.Context(), solr.WithDatabase("other"))
	if _, err := db.ExecContext(ctx, "SELECT a"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !reflect.DeepEqual(s.paths, []string{"/solr/other/sql", "/solr/c/sql"}) {
		t.Errorf("the paths are %q, want the collection of WithDatabase and then the one of the DSN", s.paths)
	}
}

// TestNoCollection holds D166: a DSN with no path, and a statement with no
// WithDatabase, fail before they send anything. WithDatabase gives the
// collection.
func TestNoCollection(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "")
	if _, err := db.ExecContext(t.Context(), "SELECT a"); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("a statement with no collection gave %v, want dbimp.ErrInvalidValue", err)
	}
	if err := db.PingContext(t.Context()); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("Ping with no collection gave %v, want dbimp.ErrInvalidValue", err)
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a", solr.WithDatabase("c")); err != nil {
		t.Errorf("a statement with WithDatabase gave %v, want no error", err)
	}
}

// TestOptionValues holds that WithTimeout fails with dbimp.ErrNotSupported
// for a timeout other than zero, because timeAllowed cut nothing (D166), that
// a value that the DSN refuses fails with dbimp.ErrInvalidValue, and that
// neither sends anything.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	for _, tt := range []struct {
		opt  solr.Option
		want error
	}{
		{solr.WithTimeout(time.Second), dbimp.ErrNotSupported},
		{solr.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{solr.WithDatabase(""), dbimp.ErrInvalidValue},
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT a", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a", solr.WithReadonly(false), solr.WithTimeout(0)); err != nil {
		t.Errorf("WithReadonly(false) and a timeout of zero gave %v, want no error", err)
	}
	if reqs := s.requests(); len(reqs) != 1 {
		t.Errorf("the server received %v, want only the last statement", reqs)
	}
}

// TestResultAndPing holds D163: Exec reads the result, and the result has no
// count of rows and no id of an insert. Ping counts the collection of the DSN.
func TestResultAndPing(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/a`b")
	res, err := db.ExecContext(t.Context(), "SELECT a")
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
	if q := s.requests()[1].Get("stmt"); q != "SELECT count(*) FROM `a``b`" {
		t.Errorf("Ping sent %q, want the count of the collection", q)
	}
}

// TestPrepare holds that a prepared statement sends its text and its
// arguments each time it runs.
func TestPrepare(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t, "/c")
	st, err := db.PrepareContext(t.Context(), "SELECT a WHERE x = ?")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, v := range []int{1, 2} {
		var a int64
		if err := st.QueryRowContext(t.Context(), v).Scan(&a); err != nil || a != 1 {
			t.Fatalf("running the statement gave %d and %v", a, err)
		}
	}
	reqs := s.requests()
	if reqs[0].Get("stmt") != "SELECT a WHERE x = 1" || reqs[1].Get("stmt") != "SELECT a WHERE x = 2" {
		t.Errorf("the statements are %q and %q", reqs[0].Get("stmt"), reqs[1].Get("stmt"))
	}
}

// TestNoRedirect holds D166: the driver follows no redirect, so it sends the
// credentials to the host of the DSN only (recorded: "a path with no slash").
func TestNoRedirect(t *testing.T) {
	t.Parallel()
	var other int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { other++ }))
	t.Cleanup(target.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(solr.Name, strings.Replace(srv.URL, "http://", "solr://u:p@", 1)+"/c")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.ExecContext(t.Context(), "SELECT a")
	var serr *solr.Error
	if !errors.As(err, &serr) || serr.HTTPStatus != http.StatusFound || other != 0 {
		t.Errorf("the error is %v and the other host got %d requests, want a *solr.Error with HTTP 302 and none", err, other)
	}
}

// drain runs the statement and reads every row, and returns the count of rows
// and the first error, from the query or from the rows.
func drain(t *testing.T, db *sql.DB, stmt string) (int, error) {
	t.Helper()
	_, rows, err := tryQuery(t.Context(), db, stmt)
	return len(rows), err
}

// TestCutAnswer holds D21: an answer that ends before its tuple EOF fails
// with ErrCut, and after a row it wraps dbimp.ErrIncomplete too.
func TestCutAnswer(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, body string
		rows       int
		incomplete bool
	}{
		{"before the first row", head("a"), 0, false},
		{"after a row", head("a") + `{"a":1},`, 1, true},
		{"in a row", head("a") + `{"a":1},{"a":`, 1, true},
		{"before the tuple EOF", head("a") + `{"a":1}]}}`, 1, true},
	} {
		s := &optionServer{response: tt.body}
		n, err := drain(t, s.open(t, "/c"), "SELECT a")
		if !errors.Is(err, solr.ErrCut) || n != tt.rows || errors.Is(err, dbimp.ErrIncomplete) != tt.incomplete {
			t.Errorf("%s: read %d rows and %v, want %d rows, solr.ErrCut, and ErrIncomplete %v", tt.name, n, err, tt.rows, tt.incomplete)
		}
	}
}

// TestAnswerShapes holds that the driver reads an answer with extra keys, and
// refuses one that is not an answer.
func TestAnswerShapes(t *testing.T) {
	t.Parallel()
	ok := `{"responseHeader":{"status":0},"result-set":{"extra":[1],"docs":[{"isMetadata":true,"fields":["a"],"aliases":{"a":"a"}},{"a":7},{"EOF":true,"RESPONSE_TIME":1}]}}`
	s := &optionServer{response: ok}
	var a int64
	if err := s.open(t, "/c").QueryRowContext(t.Context(), "SELECT a").Scan(&a); err != nil || a != 7 {
		t.Errorf("an answer with extra keys gave %d and %v, want 7", a, err)
	}
	for _, body := range []string{`[]`, `{}`, `{"result-set":[]}`, `{"result-set":{"docs":{}}}`, `{"result-set":{"docs":[1]}}`, `{"result-set":{"docs":[{"a":1}]}}`, head("a") + `{"zz":1},` + eof} {
		s := &optionServer{response: body}
		if _, err := drain(t, s.open(t, "/c"), "SELECT a"); err == nil {
			t.Errorf("the answer %s gave no error", body)
		}
	}
}

// TestLargeResult holds D25: the driver reads a result of 64 MiB one row at a
// time, and the memory in use stays far below the size of the result. It does
// not run in parallel, so that the memory of other tests does not count.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const rowsCount = 1 << 20
	row := `{"s":"` + strings.Repeat("x", 40) + `"},`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, head("s"))
		for range rowsCount {
			if _, err := io.WriteString(w, row); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, eof)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(solr.Name, strings.Replace(srv.URL, "http://", "solr://", 1)+"/c")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rows, err := db.QueryContext(t.Context(), "SELECT s")
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
