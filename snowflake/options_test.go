package snowflake //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

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
// answers it with one row of one timestamp_ltz column, which a test reads in the
// time zone of the statement.
type optionServer struct {
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
	_, _ = io.WriteString(w, `{"resultSetMetaData":{"format":"jsonv2","rowType":[{"name":"T","type":"timestamp_ltz","precision":0,"scale":9,"nullable":false}]},"data":[["1791499232.043000000"]]}`)
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
	s := &optionServer{}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return open(t, cfg, srv.URL, true), s
}

// TestConfigIsTheBody holds D183: the settings of the Config go in the body of
// each statement, and a setting that is empty leaves its member out.
func TestConfigIsTheBody(t *testing.T) {
	t.Parallel()
	cfg := config()
	db, s := optionDB(t, cfg)
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); len(got) != 1 || got["statement"] != "SELECT 1" {
		t.Errorf("the body is %v, want the statement only", got)
	}
	cfg.Database, cfg.Schema, cfg.Role, cfg.Warehouse, cfg.TimeZone, cfg.Timeout = "DB", "SC", "RL", "WH", "UTC", 90*time.Second
	db, s = optionDB(t, cfg)
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"statement": "SELECT 1", "database": "DB", "schema": "SC", "role": "RL", "warehouse": "WH", "timeout": float64(90),
		"parameters": map[string]any{"TIMEZONE": "UTC"},
	}
	got := s.last(t)
	if len(got) != len(want) {
		t.Errorf("the body is %v, want %v", got, want)
	}
	for k, v := range want {
		if k == "parameters" {
			if p, ok := got[k].(map[string]any); !ok || p["TIMEZONE"] != "UTC" || len(p) != 1 {
				t.Errorf("parameters is %v, want TIMEZONE", got[k])
			}
			continue
		}
		if got[k] != v {
			t.Errorf("%s is %v, want %v", k, got[k], v)
		}
	}
}

// TestOptionsOfOneStatement holds D109: an option comes from the DSN, then
// from the context, then from an argument, and a later one wins. An option
// leaves no trace on the next statement.
func TestOptionsOfOneStatement(t *testing.T) {
	t.Parallel()
	cfg := config()
	cfg.Role = "FROM_DSN"
	db, s := optionDB(t, cfg)
	ctx := WithOptions(t.Context(), WithRole("FROM_CONTEXT"), WithWarehouse("WH_CONTEXT"), WithDatabase("DB"), WithSchema("SC"), WithTimeout(1500*time.Millisecond))
	if _, err := db.ExecContext(ctx, "SELECT ?", int64(1), WithRole("FROM_ARGUMENT")); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	if got["role"] != "FROM_ARGUMENT" || got["warehouse"] != "WH_CONTEXT" || got["database"] != "DB" || got["schema"] != "SC" {
		t.Errorf("the body is %v", got)
	}
	// 1500 milliseconds round up to 2 seconds, which the server counts.
	if got["timeout"] != float64(2) {
		t.Errorf("the timeout is %v, want 2", got["timeout"])
	}
	b, ok := got["bindings"].(map[string]any)
	if !ok || len(b) != 1 {
		t.Fatalf("the bindings are %v, want one: the option is no argument", got["bindings"])
	}
	if one, ok := b["1"].(map[string]any); !ok || one["type"] != "FIXED" || one["value"] != "1" {
		t.Errorf("the binding is %v", b["1"])
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); got["role"] != "FROM_DSN" || got["warehouse"] != nil || got["timeout"] != nil {
		t.Errorf("the next statement kept an option: %v", got)
	}
}

// TestWithParameter holds that WithParameter writes a session parameter in
// parameters as text, replaces the time zone of the DSN, and refuses the
// parameters that change how the driver reads a value, with no request.
func TestWithParameter(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter("QUERY_TAG", "dbimp"), WithParameter("ROWS_PER_RESULTSET", 10), WithParameter("USE_CACHED_RESULT", false), WithTimeZone("UTC")); err != nil {
		t.Fatal(err)
	}
	p, _ := s.last(t)["parameters"].(map[string]any)
	if len(p) != 4 || p["QUERY_TAG"] != "dbimp" || p["ROWS_PER_RESULTSET"] != "10" || p["USE_CACHED_RESULT"] != "false" || p["TIMEZONE"] != "UTC" {
		t.Errorf("the parameters are %v", p)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithTimeZone("UTC"), WithParameter("timezone", "Asia/Jakarta")); err != nil {
		t.Fatal(err)
	}
	p, _ = s.last(t)["parameters"].(map[string]any)
	if len(p) != 1 || p["timezone"] != "Asia/Jakarta" {
		t.Errorf("the parameters are %v, want WithParameter to replace the time zone", p)
	}
	before := s.count()
	for _, name := range []string{
		"DATE_OUTPUT_FORMAT", "TIME_OUTPUT_FORMAT", "TIMESTAMP_OUTPUT_FORMAT", "TIMESTAMP_LTZ_OUTPUT_FORMAT",
		"TIMESTAMP_NTZ_OUTPUT_FORMAT", "TIMESTAMP_TZ_OUTPUT_FORMAT", "BINARY_OUTPUT_FORMAT",
		"GEOGRAPHY_OUTPUT_FORMAT", "GEOMETRY_OUTPUT_FORMAT", "MULTI_STATEMENT_COUNT", "date_output_format", "Multi_Statement_Count",
	} {
		_, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter(name, "x"))
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithParameter(%q) gave %v, want dbimp.ErrNotSupported", name, err)
		}
	}
	for _, name := range []string{"", "A B", "A=B", `A"B`} {
		if _, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter(name, "x")); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("WithParameter(%q) gave %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithParameter("QUERY_TAG", nil)); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("WithParameter with no value gave %v, want dbimp.ErrInvalidValue", err)
	}
	if got := s.count(); got != before {
		t.Errorf("a refused option sent %d statements", got-before)
	}
}

// TestOptionsRefused holds D109: an option that the server cannot honor
// fails with dbimp.ErrNotSupported, and a value that the DSN refuses fails with
// dbimp.ErrInvalidValue, with no request.
func TestOptionsRefused(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	for _, tt := range []struct {
		name string
		opt  Option
		want error
	}{
		{"WithReadonly(true)", WithReadonly(true), dbimp.ErrNotSupported},
		{"WithTimeout(-1)", WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"WithTimeZone(nonsense)", WithTimeZone("Not/AZone"), dbimp.ErrInvalidValue},
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

// TestTimeZoneOfATimestamp holds that a timestamp_ltz value is an instant in
// the time zone that the statement or the Config names, and in time.Local when
// neither names one. Each case names its zone, so that no case depends on the
// machine.
func TestTimeZoneOfATimestamp(t *testing.T) {
	t.Parallel()
	db, _ := optionDB(t, config())
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("the host has no time zone database: %v", err)
	}
	zoned := config()
	zoned.TimeZone = "Asia/Jakarta"
	zonedDB, _ := optionDB(t, zoned)
	for _, tt := range []struct {
		name string
		db   *sql.DB
		opts []any
		want *time.Location
	}{
		{"none named", db, nil, time.Local},
		{"Local named", db, []any{WithTimeZone("Local")}, time.Local},
		{"Asia/Jakarta named", db, []any{WithTimeZone("Asia/Jakarta")}, jakarta},
		{"UTC named", db, []any{WithTimeZone("UTC")}, time.UTC},
		{"the Config names Asia/Jakarta", zonedDB, nil, jakarta},
		{"the statement replaces the Config", zonedDB, []any{WithTimeZone("UTC")}, time.UTC},
	} {
		var got time.Time
		if err := tt.db.QueryRowContext(t.Context(), "SELECT t", tt.opts...).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if !got.Equal(time.Unix(1791499232, 43000000)) || got.Location().String() != tt.want.String() {
			t.Errorf("%s: the value is %v, want the instant in %v", tt.name, got, tt.want)
		}
	}
}

// TestPingRunsSelectOne holds that Ping runs SELECT 1 (D183).
func TestPingRunsSelectOne(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	if err := db.PingContext(context.WithoutCancel(t.Context())); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t)["statement"]; got != "SELECT 1" {
		t.Errorf("Ping ran %v, want SELECT 1", got)
	}
}
