package pinot_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/pinot"
)

// fakeServer is a fake Broker. It keeps the body of each query and the path
// of each cancel, and answers each query with one row. A query whose text is
// "SLOW" waits until its request ends.
type fakeServer struct {
	mu      sync.Mutex
	bodies  []map[string]any
	cancels []string
}

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.mu.Lock()
		s.cancels = append(s.cancels, r.URL.Path+"?"+r.URL.RawQuery)
		s.mu.Unlock()
		_, _ = io.WriteString(w, "Cancelled client query")
		return
	}
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.bodies = append(s.bodies, m)
	s.mu.Unlock()
	if m["sql"] == "SLOW" {
		<-r.Context().Done()
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, head("a")+`[1]`+tail)
}

// open returns a database on the fake server, with the query of a DSN.
func (s *fakeServer) open(t *testing.T, query string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(pinot.Name, strings.Replace(srv.URL, "http://", "pinot://u:p@", 1)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// requests returns the bodies that the server received, and forgets them.
func (s *fakeServer) requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bodies
	s.bodies = nil
	return b
}

// queryOptions returns the query options of a body, by their keys.
func queryOptions(t *testing.T, b map[string]any) map[string]string {
	t.Helper()
	s, _ := b["queryOptions"].(string)
	m := map[string]string{}
	for opt := range strings.SplitSeq(s, ";") {
		k, v, ok := strings.Cut(opt, "=")
		if !ok {
			t.Fatalf("the query options %q hold %q, which is not key=value", s, opt)
		}
		m[k] = v
	}
	return m
}

// TestOptionArguments holds that the options of one statement reach its body,
// that the other arguments keep their order, and that the next statement has
// none of them (D109).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "")
	var a int64
	err := db.QueryRowContext(t.Context(), "SELECT a FROM t WHERE x = ? AND y = ?", int64(1),
		pinot.WithTimeout(1500*time.Microsecond),
		pinot.WithEngine(pinot.EngineSingle),
		pinot.WithReadonly(true),
		pinot.WithParameter("trace", true),
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
	fo, so := queryOptions(t, first), queryOptions(t, second)
	switch {
	case first["sql"] != "SELECT a FROM t WHERE x = 1 AND y = 'it''s'":
		t.Errorf("the first statement is %q, want the arguments as literals (D132)", first["sql"])
	case fo["timeoutMs"] != "2", fo["useMultistageEngine"] != "false", first["trace"] != true:
		t.Errorf("the first body is %v, want a timeout of 2 ms, the single-stage engine and trace", first)
	case fo["enableNullHandling"] != "true" || !strings.HasPrefix(fo["clientQueryId"], "dbimp-"):
		t.Errorf("the first body is %v, want null handling and the id of the query (D130 and D133)", first)
	case so["timeoutMs"] != "" || so["useMultistageEngine"] != "true" || second["trace"] != nil:
		t.Errorf("the second body is %v, want none of the options of the first", second)
	case fo["clientQueryId"] == so["clientQueryId"]:
		t.Errorf("both queries have the id %q, want an id of their own", fo["clientQueryId"])
	}
}

// TestOptionDSN holds that the keys of the DSN reach every query, and that
// WithOptions applies after them.
func TestOptionDSN(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "?engine=single")
	ctx := pinot.WithOptions(t.Context(), pinot.WithTimeout(time.Second))
	if _, err := db.ExecContext(ctx, "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", pinot.WithEngine(pinot.EngineMulti)); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if o := queryOptions(t, reqs[0]); o["useMultistageEngine"] != "false" || o["timeoutMs"] != "1000" {
		t.Errorf("the first query sent %v, want the single-stage engine and a timeout of 1000 ms", o)
	}
	if o := queryOptions(t, reqs[1]); o["useMultistageEngine"] != "true" {
		t.Errorf("the second query sent %v, want the multi-stage engine of its option", o)
	}
}

// TestOptionQueryOptions holds that WithParameter("queryOptions") replaces
// every query option that the driver sends, as D109 says for a key of the
// driver.
func TestOptionQueryOptions(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "")
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", pinot.WithParameter("queryOptions", "maxRowsInJoin=1000")); err != nil {
		t.Fatal(err)
	}
	if b := s.requests()[0]; b["queryOptions"] != "maxRowsInJoin=1000" {
		t.Errorf("the body is %v, want the query options of WithParameter", b)
	}
}

// TestOptionValues holds that WithDatabase fails with dbimp.ErrNotSupported
// (D129), that an option with a value that the DSN refuses fails with
// dbimp.ErrInvalidValue, and that neither sends anything.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "")
	for _, tt := range []struct {
		opt  pinot.Option
		want error
	}{
		{pinot.WithDatabase("default"), dbimp.ErrNotSupported},
		{pinot.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{pinot.WithCancel("tag"), dbimp.ErrInvalidValue},
		{pinot.WithEngine("v2"), dbimp.ErrInvalidValue},
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}

// TestArguments holds D132 for the arguments that the driver refuses, and
// that a query with no argument goes as it is.
func TestArguments(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "")
	for _, tt := range []struct {
		query string
		args  []any
		want  error
	}{
		{"SELECT a FROM t WHERE x = ?", []any{sql.Named("x", 1)}, dbimp.ErrArguments},
		{"SELECT a FROM t WHERE x = ?", nil, nil},
		{"SELECT a FROM t WHERE x = ? AND y = ?", []any{1}, dbimp.ErrArguments},
		{"SELECT a FROM t WHERE x = ?", []any{1, 2}, dbimp.ErrArguments},
		{"SELECT a FROM t WHERE x = ?", []any{struct{}{}}, nil},
	} {
		_, err := db.ExecContext(t.Context(), tt.query, tt.args...)
		switch {
		case tt.want == nil && len(tt.args) == 0 && err != nil:
			t.Errorf("%q with no argument gave %v", tt.query, err)
		case tt.want != nil && !errors.Is(err, tt.want):
			t.Errorf("%q with %v gave %v, want %v", tt.query, tt.args, err, tt.want)
		case tt.want == nil && len(tt.args) > 0 && err == nil:
			t.Errorf("%q with %v gave no error", tt.query, tt.args)
		}
	}
	reqs := s.requests()
	if len(reqs) != 1 || reqs[0]["sql"] != "SELECT a FROM t WHERE x = ?" {
		t.Errorf("the server received %v, want the one query with no argument, as it is", reqs)
	}
}

// TestCancel holds D133: when the context ends before the answer arrives, the
// driver cancels the query by the id that it sent, and with cancel=none it
// sends nothing.
func TestCancel(t *testing.T) {
	t.Parallel()
	for _, how := range []string{pinot.CancelKill, pinot.CancelNone} {
		s := &fakeServer{}
		db := s.open(t, "?cancel="+how)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		_, err := db.ExecContext(ctx, "SLOW")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: the error is %v, want context.DeadlineExceeded", how, err)
		}
		id := queryOptions(t, s.requests()[0])["clientQueryId"]
		s.mu.Lock()
		got := s.cancels
		s.mu.Unlock()
		want := []string{"/query/" + id + "?client=true"}
		if how == pinot.CancelNone {
			want = nil
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: the driver sent the cancels %q, want %q", how, got, want)
		}
	}
}
