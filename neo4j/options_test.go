package neo4j_test

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
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/neo4j"
)

// sent is a request that the fake server of the option tests received.
type sent struct {
	path string
	body map[string]any
}

// optionServer is a fake server for the option tests. It answers GET / as
// step 6 recorded it for one release, and every statement with one row.
type optionServer struct {
	discovery *dbimptest.Exchange

	mu   sync.Mutex
	reqs []sent
}

// newOptionServer returns an optionServer that answers GET / as release
// does.
func newOptionServer(t *testing.T, release string) *optionServer {
	t.Helper()
	s := &optionServer{}
	exs, _ := exchanges(t, release)
	for _, ex := range exs {
		if ex.Request.Method == http.MethodGet && ex.Request.Path == "/" {
			s.discovery = ex
		}
	}
	if s.discovery == nil {
		t.Fatalf("the recordings of %s hold no GET /", release)
	}
	return s
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", s.discovery.Response.Header.Get("Content-Type"))
		w.WriteHeader(s.discovery.Response.Status)
		_, _ = w.Write(s.discovery.Response.Content())
		return
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, sent{path: r.URL.Path, body: m})
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/vnd.neo4j.query")
	switch {
	case strings.HasSuffix(r.URL.Path, "/tx"):
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"data":{"fields":[],"values":[]},"transaction":{"id":"t1"}}`)
	case strings.HasSuffix(r.URL.Path, "/commit"):
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"fields":[],"values":[]}}`)
	default:
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"data":{"fields":["a"],"values":[[{"$type":"Integer","_value":"1"}]]}}`)
	}
}

// open returns a database of one connection on the fake server.
func (s *optionServer) open(t *testing.T) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db := open(t, srv.URL, "?cancel=none")
	db.SetMaxOpenConns(1)
	return db
}

// one runs a statement that returns one integer, and fails the test on an
// error.
func one(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	var v int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
}

// requests returns what the server received, in order, and forgets it.
func (s *optionServer) requests() []sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs := s.reqs
	s.reqs = nil
	return reqs
}

// TestOptionArguments holds that the options of one statement reach its
// request as the keys of the body and the path, that the other arguments
// keep their order, and that the options do not reach the next statement
// (D109).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	s := newOptionServer(t, ceiling)
	db := s.open(t)
	one(t.Context(), t, db, "RETURN $1 + $2 AS a", int64(1),
		neo4j.WithTimeout(1500*time.Millisecond),
		neo4j.WithReadonly(true),
		neo4j.WithParameter("includeCounters", true),
		neo4j.WithDatabase("other"),
		int64(2))
	one(t.Context(), t, db, "RETURN 1 AS a")
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("the server received %d statements, want 2", len(reqs))
	}
	first := reqs[0]
	params, _ := first.body["parameters"].(map[string]any)
	switch {
	case first.path != "/db/other/query/v2":
		t.Errorf("the first statement went to %s, want /db/other/query/v2", first.path)
	case first.body["maxExecutionTime"] != 2.0, first.body["accessMode"] != "READ", first.body["includeCounters"] != true:
		t.Errorf("the first body is %v, want maxExecutionTime 2, the time rounded up to seconds, accessMode READ and includeCounters true", first.body)
	case len(params) != 2 || params["1"] == nil || params["2"] == nil:
		t.Errorf("the first statement has the parameters %v, want 1 and 2", params)
	}
	second := reqs[1]
	if second.path != "/db/dbmeta/query/v2" || len(second.body) != 1 {
		t.Errorf("the second statement is %v at %s, want only its statement at /db/dbmeta/query/v2", second.body, second.path)
	}
}

// TestOptionContext holds that the options of a context reach each statement
// and transaction started with it, and that an argument wins over the
// context (D109).
func TestOptionContext(t *testing.T) {
	t.Parallel()
	s := newOptionServer(t, ceiling)
	db := s.open(t)
	ctx := neo4j.WithOptions(t.Context(), neo4j.WithDatabase("other"), neo4j.WithReadonly(true))
	one(ctx, t, db, "RETURN 1 AS a", neo4j.WithDatabase("third"))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var one int64
	if err := tx.QueryRowContext(t.Context(), "RETURN 1 AS a").Scan(&one); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(t.Context(), "RETURN 1 AS a", neo4j.WithDatabase("third")).Scan(&one); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a statement of the transaction in another database gave %v, want dbimp.ErrNotSupported", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, r := range s.requests() {
		paths = append(paths, r.path)
		if strings.HasSuffix(r.path, "/tx") && r.body["accessMode"] != "READ" {
			t.Errorf("the transaction began with %v, want accessMode READ", r.body)
		}
	}
	want := []string{"/db/third/query/v2", "/db/other/query/v2/tx", "/db/other/query/v2/tx/t1", "/db/other/query/v2/tx/t1/commit"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Errorf("the requests went to %v, want %v", paths, want)
	}
}

// TestOptionTimeoutFloor holds that WithTimeout fails with
// dbimp.ErrNotSupported on a release that ignores maxExecutionTime, and
// sends nothing (D109).
func TestOptionTimeoutFloor(t *testing.T) {
	t.Parallel()
	s := newOptionServer(t, floor)
	db := s.open(t)
	var one int64
	err := db.QueryRowContext(t.Context(), "RETURN 1 AS a", neo4j.WithTimeout(time.Second)).Scan(&one)
	if !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("WithTimeout on %s gave %v, want dbimp.ErrNotSupported", floor, err)
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}

// TestOptionParameterReplaces holds that WithParameter replaces a key that
// the driver sets itself, as in Couchbase (D109).
func TestOptionParameterReplaces(t *testing.T) {
	t.Parallel()
	s := newOptionServer(t, ceiling)
	db := s.open(t)
	one(t.Context(), t, db, "RETURN 1 AS a", neo4j.WithReadonly(true), neo4j.WithParameter("accessMode", "WRITE"))
	if reqs := s.requests(); len(reqs) != 1 || reqs[0].body["accessMode"] != "WRITE" {
		t.Errorf("the server received %v, want accessMode WRITE", reqs)
	}
}

// TestOptionValues holds that an option with a value that the DSN refuses
// fails the statement, and sends nothing.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := newOptionServer(t, ceiling)
	db := s.open(t)
	for _, opt := range []neo4j.Option{neo4j.WithCancel("kill"), neo4j.WithTimeout(-time.Second)} {
		var one int64
		if err := db.QueryRowContext(t.Context(), "RETURN 1 AS a", opt).Scan(&one); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("an option with a bad value gave %v, want dbimp.ErrInvalidValue", err)
		}
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}
