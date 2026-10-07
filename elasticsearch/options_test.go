package elasticsearch_test

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/elasticsearch"
)

// TestRequest holds D167: the first request is POST /_sql?format=json with
// the query, the page size and the time zone, and the leniency, the catalog
// and the timeout only when they are set.
func TestRequest(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "u:p@", "")
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	want := map[string]any{"query": "SELECT a FROM t", "fetch_size": float64(1000), "time_zone": "UTC"}
	if !reflect.DeepEqual(reqs[0], want) {
		t.Errorf("the body is %v, want %v", reqs[0], want)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.headers[0]
	if f.queries[0] != "format=json" || h.Get("Content-Type") != "application/json" || h.Get("Accept") != "application/json" {
		t.Errorf("the request has the query %q and the headers %v, want format=json and JSON", f.queries[0], h)
	}
	if user, pass, ok := (&http.Request{Header: h}).BasicAuth(); !ok || user != "u" || pass != "p" {
		t.Errorf("the request sent the credentials %q and %q, want u and p", user, pass)
	}
}

// TestAuthAPIKey holds D94 and D167: auth=apikey sends the password as an
// API key, and no user.
func TestAuthAPIKey(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "ignored:S2V5Ok1l@", "?auth=apikey")
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.headers[0].Get("Authorization"); got != "ApiKey S2V5Ok1l" {
		t.Errorf("the header Authorization is %q, want ApiKey S2V5Ok1l", got)
	}
}

// TestNoCredentials holds that a DSN with no user and no password sends no
// credentials, for a server that runs with security off.
func TestNoCredentials(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.headers[0].Get("Authorization"); got != "" {
		t.Errorf("the header Authorization is %q, want none", got)
	}
}

// TestOptionDSN holds that each key of the DSN reaches the body.
func TestOptionDSN(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "?fetch_size=7&time_zone=Asia%2FJakarta&field_multi_value_leniency=true&catalog=c1")
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"query": "SELECT a FROM t", "fetch_size": float64(7), "time_zone": "Asia/Jakarta",
		"field_multi_value_leniency": true, "catalog": "c1",
	}
	if got := f.requests()[0]; !reflect.DeepEqual(got, want) {
		t.Errorf("the body is %v, want %v", got, want)
	}
}

// TestOptionArguments holds that the options of one statement reach its body,
// that the other arguments keep their order, and that the next statement has
// none of them (D109).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "?fetch_size=7")
	_, err := db.ExecContext(t.Context(), "SELECT ? AS a, ? AS b",
		int64(1),
		elasticsearch.WithFetchSize(3),
		elasticsearch.WithTimeZone("+05:30"),
		elasticsearch.WithFieldMultiValueLeniency(true),
		elasticsearch.WithCatalog("c2"),
		elasticsearch.WithTimeout(1500*time.Millisecond),
		"x",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t"); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	want := map[string]any{
		"query": "SELECT ? AS a, ? AS b", "fetch_size": float64(3), "time_zone": "+05:30",
		"field_multi_value_leniency": true, "catalog": "c2", "request_timeout": "1500ms",
		"params": []any{float64(1), "x"},
	}
	if !reflect.DeepEqual(reqs[0], want) {
		t.Errorf("the body is %v, want %v", reqs[0], want)
	}
	if reqs[1]["fetch_size"] != float64(7) || reqs[1]["time_zone"] != "UTC" || reqs[1]["catalog"] != nil || reqs[1]["request_timeout"] != nil {
		t.Errorf("the next body is %v, want the options of the DSN only", reqs[1])
	}
}

// TestOptionContext holds that WithOptions applies to every statement of a
// context, and that an Option argument wins over it.
func TestOptionContext(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	ctx := elasticsearch.WithOptions(t.Context(), elasticsearch.WithFetchSize(5), elasticsearch.WithTimeZone("Asia/Tokyo"))
	if _, err := db.ExecContext(ctx, "SELECT a FROM t", elasticsearch.WithFetchSize(9)); err != nil {
		t.Fatal(err)
	}
	got := f.requests()[0]
	if got["fetch_size"] != float64(9) || got["time_zone"] != "Asia/Tokyo" {
		t.Errorf("the body is %v, want fetch_size 9 and the time zone Asia/Tokyo", got)
	}
}

// TestOptionParameter holds D109: WithParameter sets any key of the body, and
// replaces a key that the driver sets.
func TestOptionParameter(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	filter := map[string]any{"range": map[string]any{"n": map[string]any{"lte": 3}}}
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t",
		elasticsearch.WithParameter("filter", filter),
		elasticsearch.WithParameter("time_zone", "Europe/Paris"),
		elasticsearch.WithParameter("keep_on_completion", false),
	); err != nil {
		t.Fatal(err)
	}
	got := f.requests()[0]
	if got["time_zone"] != "Europe/Paris" || got["keep_on_completion"] != false || got["fetch_size"] != float64(1000) {
		t.Errorf("the body is %v, want the key time_zone replaced and the other keys kept", got)
	}
	if !reflect.DeepEqual(got["filter"], map[string]any{"range": map[string]any{"n": map[string]any{"lte": float64(3)}}}) {
		t.Errorf("the filter is %v", got["filter"])
	}
}

// TestOptionValues holds D109: an option that the server cannot honor fails
// with dbimp.ErrNotSupported, and a value that the DSN refuses fails with
// dbimp.ErrInvalidValue. WithReadonly changes nothing, because every
// statement is read-only (D163).
func TestOptionValues(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	for _, tt := range []struct {
		name string
		opt  elasticsearch.Option
		want error
	}{
		{"WithDatabase", elasticsearch.WithDatabase("d"), dbimp.ErrNotSupported},
		{"WithTimeout", elasticsearch.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"WithFetchSize", elasticsearch.WithFetchSize(0), dbimp.ErrInvalidValue},
		{"WithTimeZone", elasticsearch.WithTimeZone(""), dbimp.ErrInvalidValue},
		{"WithReadonly", elasticsearch.WithReadonly(true), nil},
		{"WithTimeout of zero", elasticsearch.WithTimeout(0), nil},
	} {
		_, err := db.ExecContext(t.Context(), "SELECT a FROM t", tt.opt)
		if tt.want == nil && err != nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
	}
	if got := f.requests(); len(got) != 2 {
		t.Errorf("the server received %d requests, want 2: a refused option sends none", len(got))
	}
	// WithTimeout rounds up to the next millisecond.
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", elasticsearch.WithTimeout(1500*time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	if got := f.requests()[0]["request_timeout"]; got != "2ms" {
		t.Errorf("request_timeout is %v for 1.5ms, want 2ms", got)
	}
}

// TestArguments holds D167: the arguments are the array params, in order. A
// named argument fails, and so does a uint64 above the range of int64, a
// []byte and a NaN.
func TestArguments(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	when := time.Date(2026, 10, 1, 12, 34, 56, 123456789, time.FixedZone("", 7*3600))
	if _, err := db.ExecContext(t.Context(), "SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?",
		nil, true, int64(-5), uint64(7), 1.5, "it's", when, dbimp.Date{Year: 2026, Month: 10, Day: 1}, uint32(3)); err != nil {
		t.Fatal(err)
	}
	want := []any{nil, true, float64(-5), float64(7), 1.5, "it's", "2026-10-01T05:34:56.123456789Z", "2026-10-01", float64(3)}
	if got := f.requests()[0]["params"]; !reflect.DeepEqual(got, want) {
		t.Errorf("params is %v, want %v", got, want)
	}
	for _, tt := range []struct {
		name string
		arg  any
		want error
	}{
		{"a named argument", sql.Named("x", 1), dbimp.ErrArguments},
		{"a uint64 above int64", uint64(math.MaxUint64), dbimp.ErrNotSupported},
		{"a uint64 above int64 by one", uint64(math.MaxInt64) + 1, dbimp.ErrNotSupported},
		{"bytes", []byte("x"), dbimp.ErrNotSupported},
		{"NaN", math.NaN(), dbimp.ErrInvalidValue},
		{"an infinity", math.Inf(-1), dbimp.ErrInvalidValue},
	} {
		if _, err := db.ExecContext(t.Context(), "SELECT ?", tt.arg); !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, err := db.ExecContext(t.Context(), "SELECT ?", uint64(math.MaxInt64)); err != nil {
		t.Errorf("the largest uint64 that fits int64: %v", err)
	}
}

// TestResult holds that Exec reads the whole result, and that the result
// has no id of an insert and no rows affected (D163).
func TestResult(t *testing.T) {
	t.Parallel()
	f := &fake{handle: threePages}
	db := f.open(t, "", "")
	res, err := db.ExecContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected gave %v, want dbimp.ErrNotSupported", err)
	}
	if got := len(f.requests()); got != 3 {
		t.Errorf("Exec sent %d requests, want 3: it reads every page", got)
	}
}

// TestPingAndPrepare holds that Ping sends SELECT 1, and that a prepared
// statement runs as its text with its arguments each time.
func TestPingAndPrepare(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := f.requests()[0]["query"]; got != "SELECT 1" {
		t.Errorf("Ping sent %v, want SELECT 1", got)
	}
	stmt, err := db.PrepareContext(t.Context(), "SELECT ?")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	for _, v := range []int64{1, 2} {
		if _, err := stmt.ExecContext(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	reqs := f.requests()
	if len(reqs) != 2 || !reflect.DeepEqual(reqs[1]["params"], []any{float64(2)}) {
		t.Errorf("the statement sent %v, want two requests and the second with the argument 2", reqs)
	}
}

// TestNoPasswordInErrors holds that no error holds the password (D8).
func TestNoPasswordInErrors(t *testing.T) {
	t.Parallel()
	db, err := sql.Open(elasticsearch.Name, "elasticsearch://u:hunter2@127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.ExecContext(t.Context(), "SELECT 1")
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("the error is %v, which holds the password or is nil", err)
	}
}
