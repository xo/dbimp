package bigquery //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
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

// optionServer is a fake server that keeps the body of each request and answers
// it with the recorded answer of a plain statement.
type optionServer struct {
	t      *testing.T
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
	serve(w, exchange(s.t, 17))
}

// last returns the body of the last request.
func (s *optionServer) last(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		t.Fatal("the server got no request")
	}
	return s.bodies[len(s.bodies)-1]
}

// count returns the number of requests that the server got.
func (s *optionServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

// optionDB returns a database on a fake server, with the configuration cfg.
func optionDB(t *testing.T, cfg Config) (*sql.DB, *optionServer) {
	t.Helper()
	s := &optionServer{t: t}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return open(t, cfg, srv.URL), s
}

// TestConfigIsTheBody holds D189: the settings of the Config go in the body of
// each statement, and a setting that is empty leaves its member out.
func TestConfigIsTheBody(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"query": "SELECT 1", "useLegacySql": false, "timeoutMs": float64(3000),
		"formatOptions": map[string]any{"timestampOutputFormat": "ISO8601_STRING"},
	}
	if got := s.last(t); !same(got, want) {
		t.Errorf("the body is %v, want %v", got, want)
	}
	cfg := config()
	cfg.Dataset, cfg.Location, cfg.MaxResults, cfg.Timeout = "ds", "EU", 100, 90*time.Second
	db, s = optionDB(t, cfg)
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	want["defaultDataset"] = map[string]any{"projectId": testProject, "datasetId": "ds"}
	want["location"] = "EU"
	want["maxResults"] = float64(100)
	want["jobTimeoutMs"] = float64(90000)
	if got := s.last(t); !same(got, want) {
		t.Errorf("the body is %v, want %v", got, want)
	}
}

// TestOptionsOfOneStatement holds D109: an option comes from the DSN, then from
// the context, then from an argument, and a later one wins. An option leaves no
// trace on the next statement, and it is not a parameter of the statement.
func TestOptionsOfOneStatement(t *testing.T) {
	t.Parallel()
	cfg := config()
	cfg.Location = "FROM_DSN"
	db, s := optionDB(t, cfg)
	ctx := WithOptions(t.Context(), WithLocation("FROM_CONTEXT"), WithDatabase("ds"), WithMaxResults(10), WithTimeout(1500*time.Millisecond))
	if _, err := db.ExecContext(ctx, "SELECT ?", int64(1), WithLocation("FROM_ARGUMENT"), WithMaxResults(20)); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	if got["location"] != "FROM_ARGUMENT" || got["maxResults"] != float64(20) || got["jobTimeoutMs"] != float64(1500) {
		t.Errorf("the body is %v", got)
	}
	if ds, _ := got["defaultDataset"].(map[string]any); ds["datasetId"] != "ds" {
		t.Errorf("defaultDataset is %v, want ds", got["defaultDataset"])
	}
	// The option is not a parameter: the statement has one.
	if ps, _ := got["queryParameters"].([]any); len(ps) != 1 || got["parameterMode"] != "POSITIONAL" {
		t.Errorf("the parameters are %v in the mode %v, want one positional parameter", got["queryParameters"], got["parameterMode"])
	}
	// The next statement holds the options of the DSN again.
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	got = s.last(t)
	if got["location"] != "FROM_DSN" {
		t.Errorf("the location of the next statement is %v, want the one of the DSN", got["location"])
	}
	if _, ok := got["maxResults"]; ok {
		t.Errorf("the next statement holds maxResults: %v", got)
	}
	// The timeout rounds up to a millisecond.
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithTimeout(1500*time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t)["jobTimeoutMs"]; got != float64(2) {
		t.Errorf("1500 microseconds gave %v, want 2 milliseconds", got)
	}
}

// TestWithParameter holds D109 and D189: WithParameter sets a member of the
// request, and it replaces a member that the driver writes.
func TestWithParameter(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	if _, err := db.ExecContext(t.Context(), "SELECT 1",
		WithParameter("labels", map[string]string{"team": "dbimp"}),
		WithParameter("maximumBytesBilled", "10485760"),
		WithParameter("useQueryCache", false),
		WithParameter("requestId", "dbimp-request-1"),
		WithParameter("dryRun", true),
		WithParameter("jobTimeoutMs", 1234),
		WithParameter("maxResults", 5),
	); err != nil {
		t.Fatal(err)
	}
	got := s.last(t)
	if labels, _ := got["labels"].(map[string]any); labels["team"] != "dbimp" {
		t.Errorf("labels is %v", got["labels"])
	}
	for key, want := range map[string]any{"maximumBytesBilled": "10485760", "useQueryCache": false, "requestId": "dbimp-request-1", "dryRun": true, "jobTimeoutMs": float64(1234), "maxResults": float64(5)} {
		if got[key] != want {
			t.Errorf("%s is %v, want %v", key, got[key], want)
		}
	}
	if got["query"] != "SELECT 1" || got["useLegacySql"] != false {
		t.Errorf("the body lost a member of the driver: %v", got)
	}
}

// TestOptionsRefused holds D109: an option that the service cannot honor fails
// with dbimp.ErrNotSupported, and a value that the DSN refuses fails with
// dbimp.ErrInvalidValue. Nothing is sent.
func TestOptionsRefused(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	for _, tt := range []struct {
		name string
		opt  Option
		want error
	}{
		{"readonly", WithReadonly(true), dbimp.ErrNotSupported},
		{"negative timeout", WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"negative page", WithMaxResults(-1), dbimp.ErrInvalidValue},
		{"huge page", WithMaxResults(1 << 30), dbimp.ErrInvalidValue},
		{"the text", WithParameter("query", "SELECT 2"), dbimp.ErrNotSupported},
		{"the parameters", WithParameter("queryParameters", []any{}), dbimp.ErrNotSupported},
		{"the mode", WithParameter("parameterMode", "NAMED"), dbimp.ErrNotSupported},
		{"the dialect", WithParameter("useLegacySql", true), dbimp.ErrNotSupported},
		{"the form of a value", WithParameter("formatOptions", map[string]any{"useInt64Timestamp": true}), dbimp.ErrNotSupported},
		{"the form of the result", WithParameter("queryResultsFormat", "ARROW"), dbimp.ErrNotSupported},
		{"a member with no value", WithParameter("labels", nil), dbimp.ErrInvalidValue},
		{"a member with no name", WithParameter("", 1), dbimp.ErrInvalidValue},
		{"a member with a bad name", WithParameter("a b", 1), dbimp.ErrInvalidValue},
	} {
		_, err := db.ExecContext(t.Context(), "SELECT 1", tt.opt)
		if !isErr(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", tt.name, err, tt.want)
		}
	}
	if n := s.count(); n != 0 {
		t.Errorf("the server got %d requests, want none", n)
	}
	if _, err := db.ExecContext(WithOptions(t.Context(), WithReadonly(true)), "SELECT 1"); !isNotSupported(err) {
		t.Errorf("WithReadonly through the context gave %v, want dbimp.ErrNotSupported", err)
	}
	// The options of the other statements stay in force: a plain one runs.
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Errorf("a plain statement after the refusals: %v", err)
	}
}

// TestPingIsADryRun holds D189: Ping runs SELECT 1 as a dry run, which costs
// nothing, and it fails with the error of the service.
func TestPingIsADryRun(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t); got["query"] != "SELECT 1" || got["dryRun"] != true {
		t.Errorf("Ping sent %v, want SELECT 1 as a dry run", got)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		serve(w, exchange(t, 219))
	}))
	t.Cleanup(srv.Close)
	bad := open(t, config(), srv.URL)
	if _, ok := errAs(bad.PingContext(t.Context())); !ok {
		t.Error("Ping of a server that refuses the login gave no *Error")
	}
}

// TestTimeoutOfTheFirstWait holds that the first request asks the service to
// wait a few seconds, which bounds the time in which a job has no id (D189).
func TestTimeoutOfTheFirstWait(t *testing.T) {
	t.Parallel()
	db, s := optionDB(t, config())
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got := s.last(t)["timeoutMs"]; got != float64(firstWait) {
		t.Errorf("timeoutMs is %v, want %d", got, firstWait)
	}
}

// isErr reports whether err wraps want.
func isErr(err, want error) bool {
	return errors.Is(err, want)
}
