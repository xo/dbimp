package rqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/rqlite"
)

// fakeServer is a fake node. It keeps the path, the query and the body of
// each request, and answers each with one row.
type fakeServer struct {
	mu   sync.Mutex
	reqs []fakeRequest
}

// fakeRequest is one request that the fake server received.
type fakeRequest struct {
	path, query, body string
	user, password    string
	auth              bool
}

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	user, password, auth := r.BasicAuth()
	s.mu.Lock()
	s.reqs = append(s.reqs, fakeRequest{path: r.URL.Path, query: r.URL.RawQuery, body: string(b), user: user, password: password, auth: auth})
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, head("a")+`[1]`+tail)
}

// open returns a database on the fake server, with the user information
// and the query of a DSN.
func (s *fakeServer) open(t *testing.T, user, query string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(rqlite.Name, strings.Replace(srv.URL, "http://", "rqlite://"+user, 1)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// last returns the last request that the server received.
func (s *fakeServer) last(t *testing.T) fakeRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		t.Fatal("the server received no request")
	}
	return s.reqs[len(s.reqs)-1]
}

// query runs SELECT 1 with args, and reads it to its end.
func query(ctx context.Context, t *testing.T, db *sql.DB, args ...any) error {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT 1", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// TestEndpoints holds D142: a query goes to /db/request, Exec to
// /db/execute, and either one to /db/query with WithReadonly(true). Every
// request asks for blob_array, and sends the credentials by basic
// authentication.
func TestEndpoints(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "u:p@", "")
	for _, tt := range []struct {
		name, path string
		run        func() error
	}{
		{"query", "/db/request", func() error { return query(t.Context(), t, db) }},
		{"exec", "/db/execute", func() error { _, err := db.ExecContext(t.Context(), "SELECT 1"); return err }},
		{"read-only query", "/db/query", func() error {
			return query(t.Context(), t, db, rqlite.WithReadonly(true))
		}},
		{"read-only exec", "/db/query", func() error {
			_, err := db.ExecContext(t.Context(), "SELECT 1", rqlite.WithReadonly(true))
			return err
		}},
		{"ping", "/db/query", func() error { return db.PingContext(t.Context()) }},
	} {
		if err := tt.run(); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		r := s.last(t)
		if r.path != tt.path || r.query != "blob_array" || !r.auth || r.user != "u" || r.password != "p" {
			t.Errorf("%s went to %s?%s as %q:%q (%v), want %s?blob_array as u:p", tt.name, r.path, r.query, r.user, r.password, r.auth, tt.path)
		}
	}
}

// TestNoUserSendsNoCredentials holds D141: a DSN with no user sends no
// credentials.
func TestNoUserSendsNoCredentials(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "", "")
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r := s.last(t); r.auth {
		t.Errorf("sent the credentials %q:%q, want none", r.user, r.password)
	}
}

// TestOptions holds D109, D141 and D145 for each option that sets a key of
// the URL.
func TestOptions(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "", "?level=strong&freshness=2s")
	for _, tt := range []struct {
		name string
		// ctxOpts go through WithOptions, and args are the arguments.
		ctxOpts []rqlite.Option
		args    []any
		want    string
	}{
		{"the DSN", nil, nil, "blob_array&freshness=2s&level=strong"},
		{"WithLevel", nil, []any{rqlite.WithLevel(rqlite.LevelNone)}, "blob_array&freshness=2s&level=none"},
		{"WithFreshness", nil, []any{rqlite.WithFreshness(0)}, "blob_array&level=strong"},
		{"WithTimeout", nil, []any{rqlite.WithTimeout(1500 * time.Millisecond)}, "blob_array&db_timeout=1500ms&freshness=2s&level=strong"},
		{"WithTimeout rounds up", nil, []any{rqlite.WithTimeout(1500*time.Millisecond + time.Microsecond)}, "blob_array&db_timeout=1501ms&freshness=2s&level=strong"},
		{"WithParameter", nil, []any{rqlite.WithParameter("timings", nil), rqlite.WithParameter("level", "weak")}, "blob_array&freshness=2s&level=weak&timings"},
		{"WithOptions", []rqlite.Option{rqlite.WithLevel(rqlite.LevelAuto)}, nil, "blob_array&freshness=2s&level=auto"},
	} {
		if err := query(rqlite.WithOptions(t.Context(), tt.ctxOpts...), t, db, tt.args...); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if r := s.last(t); r.query != tt.want {
			t.Errorf("%s sent %q, want %q", tt.name, r.query, tt.want)
		}
	}
}

// TestDeadline holds D145: the time left before the deadline of the context
// goes as db_timeout, and the shorter of it and WithTimeout goes.
func TestDeadline(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "", "")
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	for _, tt := range []struct {
		name     string
		args     []any
		min, max time.Duration
	}{
		{"the deadline", nil, 50 * time.Second, time.Minute},
		{"a shorter WithTimeout", []any{rqlite.WithTimeout(time.Second)}, time.Second, time.Second},
		{"a longer WithTimeout", []any{rqlite.WithTimeout(time.Hour)}, 50 * time.Second, time.Minute},
	} {
		if err := query(ctx, t, db, tt.args...); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		q := s.last(t).query
		_, v, ok := strings.Cut(q, "db_timeout=")
		d, err := time.ParseDuration(v)
		if !ok || err != nil || d < tt.min || d > tt.max {
			t.Errorf("%s sent %q, want a db_timeout from %v to %v", tt.name, q, tt.min, tt.max)
		}
	}
}

// TestOptionsRefused holds D109: an option that the server cannot honor, or
// whose value the DSN would refuse, fails and sends nothing.
func TestOptionsRefused(t *testing.T) {
	t.Parallel()
	s := &fakeServer{}
	db := s.open(t, "", "")
	for _, tt := range []struct {
		opt  rqlite.Option
		want error
	}{
		{rqlite.WithDatabase("main"), dbimp.ErrNotSupported},
		{rqlite.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{rqlite.WithFreshness(-time.Second), dbimp.ErrInvalidValue},
		{rqlite.WithLevel("bogus"), dbimp.ErrInvalidValue},
	} {
		if err := query(t.Context(), t, db, tt.opt); !errors.Is(err, tt.want) {
			t.Errorf("the option gave %v, want %v", err, tt.want)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) != 0 {
		t.Errorf("the server received %d requests, want none", len(s.reqs))
	}
}
