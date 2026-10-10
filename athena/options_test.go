package athena //nolint:testpackage // The tests read the body that the connector sends.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// starts runs the statement query with args on a fake server that answers a
// SELECT, and returns the body of the StartQueryExecution that it received, or
// the error of the statement.
func starts(ctx context.Context, t *testing.T, cfg Config, query string, args ...any) (map[string]any, error) {
	t.Helper()
	f := newFake(t, selectOne(t))
	db := open(t, cfg, f.srv.URL)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if got := f.targets(); len(got) != 0 {
			t.Errorf("the statement failed with %v, and the driver sent %q", err, got)
		}
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, r := range f.requests() {
		if r.target != "StartQueryExecution" {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(r.body), &body); err != nil {
			t.Fatalf("the start %s is not JSON: %v", r.body, err)
		}
		return body, nil
	}
	t.Fatal("the driver sent no start")
	return nil, nil
}

// TestOptionsOfTheDSN holds that the keys of the DSN are in the request: the
// workgroup, the database and the catalog in the context of the query, and the
// output location in the result configuration (recorded: "a select", "a catalog
// in the context", "another result configuration").
func TestOptionsOfTheDSN(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.Output, cfg.Catalog = "s3://bucket/out/", "dbimp-cw"
	body, err := starts(t.Context(), t, cfg, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"QueryString":           "SELECT 1",
		"WorkGroup":             "dbimp",
		"QueryExecutionContext": map[string]any{"Database": "dbimp_test", "Catalog": "dbimp-cw"},
		"ResultConfiguration":   map[string]any{"OutputLocation": "s3://bucket/out/"},
	}
	delete(body, "ClientRequestToken")
	if !reflect.DeepEqual(body, want) {
		t.Errorf("the body is %v, want %v", body, want)
	}
}

// TestRequestLeavesOutWhatIsEmpty holds that a Config with no workgroup, no
// database, no catalog and no output sends none of them, so Athena uses the
// workgroup primary and the catalog AwsDataCatalog (recorded: "the primary
// workgroup").
func TestRequestLeavesOutWhatIsEmpty(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.WorkGroup, cfg.Database = "", ""
	body, err := starts(t.Context(), t, cfg, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	delete(body, "ClientRequestToken")
	if want := map[string]any{"QueryString": "SELECT 1"}; !reflect.DeepEqual(body, want) {
		t.Errorf("the body is %v, want %v", body, want)
	}
}

// TestOptionsForOneStatement sets each option of the DSN that can change for one
// statement, with an argument of the type Option, and holds that the option is in
// the request and that it is not an argument of the statement (D109).
func TestOptionsForOneStatement(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opt  Option
		key  string
		want any
	}{
		{"WithDatabase", WithDatabase("other"), "QueryExecutionContext", map[string]any{"Database": "other"}},
		{"WithWorkGroup", WithWorkGroup("wg"), "WorkGroup", "wg"},
		{"WithOutput", WithOutput("s3://b/p/"), "ResultConfiguration", map[string]any{"OutputLocation": "s3://b/p/"}},
		{"WithCatalog", WithCatalog("dbimp-cw"), "QueryExecutionContext", map[string]any{"Database": "dbimp_test", "Catalog": "dbimp-cw"}},
		{"WithParameter", WithParameter("ResultReuseConfiguration", map[string]any{"ResultReuseByAgeConfiguration": map[string]any{"Enabled": true, "MaxAgeInMinutes": 60}}), "ResultReuseConfiguration", map[string]any{"ResultReuseByAgeConfiguration": map[string]any{"Enabled": true, "MaxAgeInMinutes": float64(60)}}},
		{"WithParameter replaces a key of the driver", WithParameter("WorkGroup", "replaced"), "WorkGroup", "replaced"},
		{"WithParameter replaces the parameters", WithParameter("ExecutionParameters", []string{"'z'"}), "ExecutionParameters", []any{"'z'"}},
		{"WithTimeout of zero", WithTimeout(0), "WorkGroup", "dbimp"},
		{"WithReadonly of false", WithReadonly(false), "WorkGroup", "dbimp"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, err := starts(t.Context(), t, testConfig(), "SELECT ?", tt.opt, int64(5))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body[tt.key], tt.want) {
				t.Errorf("%s is %v, want %v", tt.key, body[tt.key], tt.want)
			}
			if tt.key != "ExecutionParameters" && !reflect.DeepEqual(body["ExecutionParameters"], []any{"5"}) {
				t.Errorf("ExecutionParameters is %v, want [5]: the option is no parameter", body["ExecutionParameters"])
			}
		})
	}
}

// TestOptionsOrder holds D109: an option comes from the DSN, then from the
// context through WithOptions, then from an argument, and a later one wins. No
// option stays on the connection for the next statement.
func TestOptionsOrder(t *testing.T) {
	t.Parallel()
	f := newFake(t, selectOne(t))
	db := open(t, testConfig(), f.srv.URL)
	ctx := WithOptions(t.Context(), WithWorkGroup("from-context"), WithDatabase("db-context"))
	ctx = WithOptions(ctx, WithDatabase("db-context-2"))
	for _, args := range [][]any{nil, {WithWorkGroup("from-argument")}} {
		if _, _, err := readContext(ctx, t, db, "SELECT 1", args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := read(t, db, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range f.requests() {
		if r.target != "StartQueryExecution" {
			continue
		}
		var body struct {
			WorkGroup string
			Context   struct{ Database string } `json:"QueryExecutionContext"`
		}
		if err := json.Unmarshal([]byte(r.body), &body); err != nil {
			t.Fatal(err)
		}
		got = append(got, body.WorkGroup+"/"+body.Context.Database)
	}
	want := []string{"from-context/db-context-2", "from-argument/db-context-2", "dbimp/dbimp_test"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the workgroup and the database are %q, want %q", got, want)
	}
}

// TestOptionsThatTheServerCannotHonor holds D109: an option that Athena cannot
// honor fails the statement with dbimp.ErrNotSupported, and names the option,
// and an option with a value that the DSN refuses fails with
// dbimp.ErrInvalidValue. Neither sends a request.
func TestOptionsThatTheServerCannotHonor(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opt  Option
		want error
	}{
		{"a timeout", WithTimeout(time.Minute), dbimp.ErrNotSupported},
		{"readonly", WithReadonly(true), dbimp.ErrNotSupported},
		{"a negative timeout", WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"a parameter with no name", WithParameter("", 1), dbimp.ErrInvalidValue},
		{"a parameter with no value", WithParameter("Name", nil), dbimp.ErrInvalidValue},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := starts(t.Context(), t, testConfig(), "SELECT 1", tt.opt)
			if !errors.Is(err, tt.want) {
				t.Errorf("the error is %v, want %v", err, tt.want)
			}
		})
	}
	_, err := starts(t.Context(), t, testConfig(), "SELECT 1", WithTimeout(time.Minute))
	if err == nil || !strings.Contains(err.Error(), "WithTimeout") {
		t.Errorf("the error %v does not name the option WithTimeout", err)
	}
}

// TestOptionsInTheContextApplyToExec holds that Exec and a prepared statement
// take the options the same way as Query does.
func TestOptionsInTheContextApplyToExec(t *testing.T) {
	t.Parallel()
	f := newFake(t, selectOne(t))
	db := open(t, testConfig(), f.srv.URL)
	ctx := WithOptions(t.Context(), WithWorkGroup("exec-group"))
	if _, err := db.ExecContext(ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	stmt, err := db.PrepareContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if _, err := stmt.ExecContext(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range f.requests() {
		if r.target == "StartQueryExecution" {
			n++
			var body struct{ WorkGroup string }
			if err := json.Unmarshal([]byte(r.body), &body); err != nil || body.WorkGroup != "exec-group" {
				t.Errorf("the start %s has the workgroup %q, want exec-group", r.body, body.WorkGroup)
			}
		}
	}
	if n != 3 {
		t.Errorf("the driver sent %d starts, want 3", n)
	}
}
