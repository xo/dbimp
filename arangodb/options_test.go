package arangodb_test

import (
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
	"github.com/xo/dbimp/arangodb"
)

// sent is a request that the fake server of the option tests received.
type sent struct {
	method string
	path   string
	body   map[string]any
}

// optionServer is a fake server for the option tests. It answers each query
// with one row, and each call of a transaction with its id.
type optionServer struct {
	mu   sync.Mutex
	reqs []sent
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.reqs = append(s.reqs, sent{method: r.Method, path: r.URL.Path, body: m})
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/_api/collection"):
		_, _ = io.WriteString(w, `{"result":[{"name":"c"}]}`)
	case strings.Contains(r.URL.Path, "/_api/transaction"):
		_, _ = io.WriteString(w, `{"result":{"id":"t1","status":"running"}}`)
	default:
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"result":[1],"hasMore":false,"extra":{"stats":{"writesExecuted":0}}}`)
	}
}

// open returns a database of one connection on the fake server.
func (s *optionServer) open(t *testing.T) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(arangodb.Name, strings.Replace(srv.URL, "http://", "arangodb://root:secret@", 1)+"/dbmeta?cancel=none")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	return db
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
	s := &optionServer{}
	db := s.open(t)
	var one int64
	err := db.QueryRowContext(t.Context(), "RETURN @1 + @2", int64(1),
		arangodb.WithTimeout(1500*time.Millisecond),
		arangodb.WithParameter("ttl", 60),
		arangodb.WithParameter("batchSize", 7),
		arangodb.WithBatch(3),
		arangodb.WithDatabase("other"),
		int64(2)).Scan(&one)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "RETURN 1").Scan(&one); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("the server received %d requests, want 2", len(reqs))
	}
	first := reqs[0]
	opts, _ := first.body["options"].(map[string]any)
	vars, _ := first.body["bindVars"].(map[string]any)
	switch {
	case first.path != "/_db/other/_api/cursor":
		t.Errorf("the first query went to %s, want /_db/other/_api/cursor", first.path)
	case opts["maxRuntime"] != 1.5, first.body["ttl"] != 60.0, first.body["batchSize"] != 7.0:
		t.Errorf("the first body is %v, want maxRuntime 1.5, ttl 60, and the batchSize 7 of WithParameter", first.body)
	case len(vars) != 2 || vars["1"] != 1.0 || vars["2"] != 2.0:
		t.Errorf("the first query has the bind variables %v, want 1 and 2", vars)
	}
	second := reqs[1]
	if second.path != "/_db/dbmeta/_api/cursor" || second.body["batchSize"] != 1000.0 || second.body["ttl"] != nil {
		t.Errorf("the second query is %v at %s, want the batchSize 1000 at /_db/dbmeta/_api/cursor", second.body, second.path)
	}
	if opts, _ := second.body["options"].(map[string]any); opts["maxRuntime"] != nil {
		t.Errorf("the second query has the options %v, want no maxRuntime", opts)
	}
}

// TestOptionTransaction holds that WithDatabase and WithReadonly through
// WithOptions reach BeginTx, that each statement of the transaction runs in
// its database, and that WithReadonly holds for a statement only in a
// read-only transaction (D109).
func TestOptionTransaction(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t)
	var one int64
	if err := db.QueryRowContext(t.Context(), "RETURN 1", arangodb.WithReadonly(true)).Scan(&one); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("WithReadonly outside a transaction gave %v, want dbimp.ErrNotSupported", err)
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
	tx, err := db.BeginTx(arangodb.WithOptions(t.Context(), arangodb.WithDatabase("other"), arangodb.WithReadonly(true)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRowContext(t.Context(), "RETURN 1", arangodb.WithReadonly(true)).Scan(&one); err != nil {
		t.Errorf("WithReadonly in a read-only transaction: %v", err)
	}
	if err := tx.QueryRowContext(t.Context(), "RETURN 1", arangodb.WithDatabase("third")).Scan(&one); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a statement of the transaction in another database gave %v, want dbimp.ErrNotSupported", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range s.requests() {
		got = append(got, r.method+" "+r.path)
		if strings.HasSuffix(r.path, "/begin") {
			cols, _ := r.body["collections"].(map[string]any)
			if write, _ := cols["write"].([]any); len(write) != 0 {
				t.Errorf("the transaction began with %v, want no collection for write", r.body)
			}
		}
	}
	want := []string{
		"POST /_db/other/_api/transaction/begin",
		"POST /_db/other/_api/cursor",
		"PUT /_db/other/_api/transaction/t1",
	}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("the requests were %v, want %v", got, want)
	}
}

// TestOptionValues holds that an option with a value that the DSN refuses
// fails the statement, and sends nothing.
func TestOptionValues(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t)
	for _, opt := range []arangodb.Option{arangodb.WithBatch(0), arangodb.WithCancel("metadata"), arangodb.WithTimeout(-time.Second)} {
		var one int64
		if err := db.QueryRowContext(t.Context(), "RETURN 1", opt).Scan(&one); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("an option with a bad value gave %v, want dbimp.ErrInvalidValue", err)
		}
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}
