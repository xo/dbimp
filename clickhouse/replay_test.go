package clickhouse //nolint:testpackage // The tests read the version that a connection keeps, which only the package can.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/clickhouse/. Each one decodes a real answer through the driver.

// recorded is the folder of the exchanges of step 6.
const recorded = "../testdata/clickhouse"

// releases are the releases that step 6 recorded, with the version that each one
// answered to SELECT version().
var releases = []struct{ name, version string }{
	{"clickhouse-25.3", "25.3.14.14"},
	{"clickhouse-25.8", "25.8.33.6"},
	{"clickhouse-26.9", "26.9.2.8"},
}

// owned are the keys of the query that the driver sends on every request (D176).
// The recorded requests mostly lack them, and a key of a recording with another
// value is not what the driver sends, so a match leaves them out, unless the test
// names the key as strict.
var owned = []string{
	"query_id",
	"default_format",
	"output_format_json_quote_64bit_integers",
	"output_format_json_quote_denormals",
	"date_time_output_format",
	"output_format_json_named_tuples_as_objects",
	"http_write_exception_in_output_format",
	"enable_http_compression",
}

// matcher returns a match for the recorded exchanges. It compares the method,
// the path, the keys of the query that the driver does not own, and the statement.
// A key in strict must be in the recording with the value that the driver sends.
// The recordings hold the version as SELECT version() AS v, and the driver asks
// SELECT version(). A recording that the server compressed does not answer,
// because the replay does not compress.
func matcher(strict ...string) dbimptest.Match {
	return func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path || ex.Response.Header.Get("Content-Encoding") != "" {
			return false
		}
		statement := string(body)
		isVersion := statement == "SELECT version()"
		if isVersion {
			statement = "SELECT version() AS v"
		}
		if statement != ex.Request.Body {
			return false
		}
		got := r.URL.Query()
		want, err := url.ParseQuery(ex.Request.Query)
		if err != nil {
			return false
		}
		for _, key := range owned {
			if slices.Contains(strict, key) && !isVersion {
				if !want.Has(key) || want.Get(key) != got.Get(key) {
					return false
				}
				continue
			}
			got.Del(key)
			want.Del(key)
		}
		if len(got) != len(want) {
			return false
		}
		for key, values := range got {
			if strings.Join(values, "|") != strings.Join(want[key], "|") {
				return false
			}
		}
		return true
	}
}

// replayDB opens a database on the exchanges of one release, in the database
// dbimp that the recordings used.
func replayDB(t *testing.T, release string, strict ...string) *sql.DB {
	t.Helper()
	return replayMatch(t, release, matcher(strict...))
}

// replayMatch is replayDB for the exchanges that match accepts.
func replayMatch(t *testing.T, release string, match dbimptest.Match) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, recorded, release, match)
	db, err := sql.Open(Name, strings.Replace(srv.URL, "http://", Name+"://default@", 1)+"/dbimp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}

// serverError returns err as a *Error, and fails the test if it is not one.
func serverError(t *testing.T, err error) *Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *Error", err, err)
	}
	return e
}

// TestReplayVersion holds D177: a connection runs SELECT version() when it opens,
// and keeps the answer.
func TestReplayVersion(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		srv := dbimptest.ReplayRelease(t, recorded, rel.name, matcher())
		cfg, err := ParseDSN(strings.Replace(srv.URL, "http://", Name+"://default@", 1) + "/dbimp")
		if err != nil {
			t.Fatal(err)
		}
		c := NewConnector(*cfg)
		t.Cleanup(func() {
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		})
		cn, err := c.Connect(t.Context())
		if err != nil {
			t.Fatalf("%s: %v", rel.name, err)
		}
		k, ok := cn.(*conn)
		if !ok {
			t.Fatalf("%s: the connection is a %T, want a *conn", rel.name, cn)
		}
		if k.version != rel.version {
			t.Errorf("%s: the version is %q, want %q", rel.name, k.version, rel.version)
		}
		if got, want := tagged(rel.version), rel.name == "clickhouse-26.9"; got != want {
			t.Errorf("%s: tagged(%q) = %v, want %v", rel.name, rel.version, got, want)
		}
	}
}

// TestReplayRows holds that a result of 5000 rows arrives whole, in order, with
// the types of its columns (measured).
func TestReplayRows(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		db := replayDB(t, rel.name)
		cols, rows, err := readAll(t, db, "SELECT id, s FROM dbimp.dbimp_big ORDER BY id")
		if err != nil {
			t.Fatalf("%s: %v", rel.name, err)
		}
		if len(cols) != 2 || cols[0] != "id" || cols[1] != "s" {
			t.Errorf("%s: the columns are %q, want id and s", rel.name, cols)
		}
		if len(rows) != 5000 {
			t.Fatalf("%s: read %d rows, want 5000", rel.name, len(rows))
		}
		for i, row := range rows {
			if got := fmt.Sprint(row[0]); got != strconv.Itoa(i) {
				t.Fatalf("%s: row %d has the id %v, want the ids in order", rel.name, i, got)
			}
		}
		if _, ok := rows[4999][1].(string); !ok {
			t.Errorf("%s: the column s is %T, want string", rel.name, rows[4999][1])
		}
	}
}

// TestReplayErrorBeforeRows holds that an error before any row is the error of
// QueryContext, with its code and its name, and the status of the response
// (measured).
func TestReplayErrorBeforeRows(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		db := replayDB(t, rel.name)
		_, _, err := readAll(t, db, "SELECT * FROM dbimp.nosuch")
		if err == nil {
			t.Fatalf("%s: a query of a table that does not exist gave no error", rel.name)
		}
		e := serverError(t, err)
		if e.Code != 60 || e.Name != "UNKNOWN_TABLE" || e.HTTPStatus != http.StatusNotFound {
			t.Errorf("%s: the error is %+v, want the code 60, UNKNOWN_TABLE and HTTP 404", rel.name, e)
		}
		if !strings.Contains(e.Message, "dbimp.nosuch") || strings.Contains(e.Message, "version") {
			t.Errorf("%s: the message is %q, want the text with the name of the table and no version", rel.name, e.Message)
		}
		if _, ok := errors.AsType[*dbimp.StatusError](err); !ok {
			t.Errorf("%s: the error wraps no *dbimp.StatusError", rel.name)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete (D107)", rel.name)
		}
	}
}

// TestReplayErrorAfterRows holds D176: on 25.3 and 25.8 an error after some rows
// is the marker in a stream with HTTP 200, and it reaches the caller after the
// rows, wrapped with dbimp.ErrIncomplete. On 26.9 the server answers HTTP 500
// with the rows before the text, and the status is an error even though rows
// came first.
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	const statement = "SELECT number, throwIf(number = 5) FROM numbers(10)"
	for _, rel := range releases {
		// Whether the rows come before the error depends on timing on 25.3 and 25.8
		// (measured), so the test takes the recording in which they did.
		base := matcher("http_write_exception_in_output_format")
		db := replayMatch(t, rel.name, func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
			return base(r, body, ex) && (rel.name == "clickhouse-26.9" || strings.Contains(ex.Response.Body, "3, 0]") || strings.Contains(ex.Response.Body, `3", 0]`) || string(body) == "SELECT version()")
		})
		_, rows, err := readAll(t, db, statement, WithParameter("max_block_size", 2))
		if err == nil {
			t.Fatalf("%s: read %d rows and no error", rel.name, len(rows))
		}
		e := serverError(t, err)
		if e.Code != 395 || e.Name != "FUNCTION_THROW_IF_VALUE_IS_NON_ZERO" {
			t.Errorf("%s: the error is %+v, want the code 395", rel.name, e)
		}
		if rel.name == "clickhouse-26.9" {
			if e.HTTPStatus != http.StatusInternalServerError || len(rows) != 0 || errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("%s: HTTP %d, %d rows and %v, want HTTP 500 as the error of QueryContext, with no row", rel.name, e.HTTPStatus, len(rows), err)
			}
			continue
		}
		if e.HTTPStatus != http.StatusOK || len(rows) != 4 || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: HTTP %d, %d rows and %v, want HTTP 200, the 4 rows before the error, and dbimp.ErrIncomplete", rel.name, e.HTTPStatus, len(rows), err)
		}
	}
}

// TestReplayNoColumns holds that a statement with no result, whose answer is
// empty, runs with Exec and with Query, and that the result has no count (D176).
func TestReplayNoColumns(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		db := replayDB(t, rel.name)
		res, err := db.ExecContext(t.Context(), "DROP DATABASE IF EXISTS dbimp SYNC", WithDatabase(""))
		if err != nil {
			t.Fatalf("%s: %v", rel.name, err)
		}
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: RowsAffected gave %v, want dbimp.ErrNotSupported", rel.name, err)
		}
		if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: LastInsertId gave %v, want dbimp.ErrNotSupported", rel.name, err)
		}
		cols, rows, err := readAll(t, db, "DROP DATABASE IF EXISTS dbimp SYNC", WithDatabase(""))
		if err != nil || len(cols) != 0 || len(rows) != 0 {
			t.Errorf("%s: a query with no result gave %q, %v and %v, want no column and no row", rel.name, cols, rows, err)
		}
	}
}

// TestReplayWrongPassword holds that a wrong password is an error that carries
// its code and its status.
func TestReplayWrongPassword(t *testing.T) {
	t.Parallel()
	srv := dbimptest.ReplayRelease(t, recorded, "clickhouse-25.8", func(r *http.Request, _ []byte, ex *dbimptest.Exchange) bool {
		return ex.Response.Status == http.StatusUnauthorized && ex.Request.Method == r.Method
	})
	db, err := sql.Open(Name, strings.Replace(srv.URL, "http://", Name+"://default:wrong@", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	err = db.PingContext(t.Context())
	if err == nil {
		t.Fatal("a wrong password gave no error")
	}
	if e := serverError(t, err); e.HTTPStatus != http.StatusUnauthorized || e.Code != 194 {
		t.Errorf("the error is %+v, want HTTP 401 and the code 194", e)
	}
}

// TestReplayErrorHoldsNoPassword holds that no error of a request holds the
// password of the DSN (step 12).
func TestReplayErrorHoldsNoPassword(t *testing.T) {
	t.Parallel()
	for _, port := range []int{1, 65535} {
		db, err := sql.Open(Name, Name+"://u:s3cret@127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		ctx, cancel := context.WithTimeout(t.Context(), 5e9)
		err = db.PingContext(ctx)
		cancel()
		if err == nil || strings.Contains(err.Error(), "s3cret") {
			t.Errorf("the error of a request to port %d is %v, want one with no password", port, err)
		}
	}
}
