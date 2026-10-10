package databricks //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// optionServer is a fake server that keeps the body of each statement and
// answers it with the recorded answer 21, which holds two columns and one row.
type optionServer struct {
	answer string

	mu     sync.Mutex
	bodies []map[string]any
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.bodies = append(s.bodies, m)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s.answer)
}

// last returns the body of the last statement.
func (s *optionServer) last(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		t.Fatal("the server got no statement")
	}
	return s.bodies[len(s.bodies)-1]
}

// count returns the number of statements that the server got.
func (s *optionServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

// optionDB returns a database on a fake server, with the configuration cfg.
func optionDB(t *testing.T, cfg Config) (*sql.DB, *optionServer) {
	t.Helper()
	s := &optionServer{answer: string(exchange(t, 21).Response.Content())}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return open(t, cfg, srv.URL), s
}

// TestConfigIsTheBody holds D193 item 5: the catalog and the schema of the Config go
// in the body of each statement, with the warehouse and the wait, and a setting
// that is empty leaves its member out.
func TestConfigIsTheBody(t *testing.T) {
	t.Parallel()
	cfg := Config{Host: testHost, Token: testToken, Warehouse: "abc"}
	db, s := optionDB(t, cfg)
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	if len(got) != 3 || got["statement"] != "SELECT 1" || got["warehouse_id"] != "abc" || got["wait_timeout"] != "50s" {
		t.Errorf("the body is %v, want the warehouse, the statement and the wait", got)
	}
	cfg.Catalog, cfg.Schema, cfg.Timeout = "main", "sales", 90*time.Second
	db, s = optionDB(t, cfg)
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	got = s.last(t)
	if len(got) != 5 || got["catalog"] != "main" || got["schema"] != "sales" {
		t.Errorf("the body is %v, want the catalog and the schema too", got)
	}
	// The timeout is longer than the wait, so the wait stays 50 seconds.
	if got["wait_timeout"] != "50s" {
		t.Errorf("the wait is %v, want 50s", got["wait_timeout"])
	}
}

// TestOptionsOfOneStatement holds D109: an option comes from the DSN, then from
// the context, then from an argument, and a later one wins. An option leaves no
// trace on the next statement.
func TestOptionsOfOneStatement(t *testing.T) {
	t.Parallel()
	cfg := recordedConfig()
	cfg.Catalog = "from_dsn"
	db, s := optionDB(t, cfg)
	ctx := WithOptions(t.Context(), WithCatalog("from_context"), WithSchema("schema_context"), WithTimeout(time.Minute))
	if _, err := db.ExecContext(ctx, "SELECT ?", int64(1), WithCatalog("from_argument"), WithDatabase("database_argument")); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	if got["catalog"] != "from_argument" || got["schema"] != "database_argument" {
		t.Errorf("the body is %v", got)
	}
	// The option is no argument.
	p, ok := got["parameters"].([]any)
	if !ok || len(p) != 1 {
		t.Fatalf("the parameters are %v, want one: the option is no argument", got["parameters"])
	}
	if one, ok := p[0].(map[string]any); !ok || one["type"] != "BIGINT" || one["value"] != "1" {
		t.Errorf("the parameter is %v", p[0])
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); got["catalog"] != "from_dsn" || got["schema"] != "dbimp" {
		t.Errorf("the next statement kept an option: %v", got)
	}
}

// TestWithParameter holds that WithParameter writes a member of the body and
// replaces a member of the driver with the same name, and that it refuses the
// members that change how the driver sends or reads, with no request.
func TestWithParameter(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, recordedConfig())
	tags := []any{map[string]any{"key": "origin", "value": "dbimp"}}
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter("query_tags", tags), WithParameter("catalog", "replaced")); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	if got["catalog"] != "replaced" || got["schema"] != "dbimp" {
		t.Errorf("the body is %v, want the catalog replaced", got)
	}
	if list, ok := got["query_tags"].([]any); !ok || len(list) != 1 {
		t.Errorf("query_tags is %v", got["query_tags"])
	}
	before := s.count()
	for _, name := range []string{
		"warehouse_id", "statement", "parameters", "wait_timeout", "on_wait_timeout", "row_limit", "byte_limit", "disposition", "format",
	} {
		_, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter(name, "x"))
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithParameter(%q) gave %v, want dbimp.ErrNotSupported", name, err)
		}
	}
	for _, name := range []string{"", "a b", `a"b`, "a=b"} {
		if _, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter(name, "x")); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("WithParameter(%q) gave %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter("query_tags", nil)); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("WithParameter with no value gave %v, want dbimp.ErrInvalidValue", err)
	}
	if got := s.count(); got != before {
		t.Errorf("a refused option sent %d statements", got-before)
	}
}

// TestOptionsRefused holds D109: an option that the server cannot honor fails
// with dbimp.ErrNotSupported, and a value that the DSN refuses fails with
// dbimp.ErrInvalidValue, with no request.
func TestOptionsRefused(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, recordedConfig())
	for _, tt := range []struct {
		name string
		opt  Option
		want error
	}{
		{"WithReadonly(true)", WithReadonly(true), dbimp.ErrNotSupported},
		{"WithTimeout(-1)", WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"WithTimeout(too long)", WithTimeout(maxTimeout + time.Hour), dbimp.ErrInvalidValue},
	} {
		_, err := db.ExecContext(t.Context(), "SELECT 1", tt.opt)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s gave %v, want %v", tt.name, err, tt.want)
		}
	}
	if got := s.count(); got != 0 {
		t.Errorf("a refused option sent %d statements", got)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithReadonly(false), WithTimeout(0)); err != nil {
		t.Errorf("options that change nothing gave %v", err)
	}
}

// TestPingRunsSelectOne holds that Ping runs SELECT 1.
func TestPingRunsSelectOne(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, recordedConfig())
	if err := db.PingContext(context.WithoutCancel(t.Context())); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t)["statement"]; got != "SELECT 1" {
		t.Errorf("Ping ran %v, want SELECT 1", got)
	}
}
