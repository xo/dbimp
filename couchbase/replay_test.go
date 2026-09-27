package couchbase_test

import (
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/couchbase"
	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from real servers,
// under testdata/couchbase/. Each one decodes a real response through the
// driver.

const testdata = "../testdata/couchbase"

// replayDB opens the driver against the recordings of one release.
func replayDB(t *testing.T, release string) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, nil)
	db := openAt(t, srv.URL)
	t.Cleanup(func() { db.Close() })
	return db
}

// rowsOf runs query and returns its columns and every row, read into *any.
func rowsOf(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		row := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range dest {
			dest[i] = &row[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

func TestReplayColumnOrder(t *testing.T) {
	t.Parallel()
	for release, want := range map[string][]string{
		"couchbase-8.0.3":  {"b", "a", "c"},
		"couchbase-7.6.12": {"b", "a", "c"},
		// 7.2.9 sends the columns in the order of their names (D39).
		"couchbase-7.2.9": {"a", "b", "c"},
	} {
		cols, rows, err := rowsOf(t, replayDB(t, release), "SELECT 2 AS b, 1 AS a, 3 AS c")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if !slices.Equal(cols, want) {
			t.Errorf("%s: the columns are %q, want %q", release, cols, want)
		}
		byName := map[string]any{}
		for i, c := range cols {
			byName[c] = rows[0][i]
		}
		if byName["a"] != int64(1) || byName["b"] != int64(2) {
			t.Errorf("%s: the row is %v", release, rows[0])
		}
	}
}

func TestReplayValues(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "couchbase-8.0.3")
	cols, rows, err := rowsOf(t, db, `SELECT 1 AS i, -1.5 AS f, "s" AS s, true AS b, null AS n, MISSING AS m, [1, 2] AS a, {"x": 1} AS o, 9007199254740993 AS big, 9223372036854775807 AS maxint, 123456789012345678901234567890 AS huge, 0.1 AS tenth, "" AS e, "2026-09-27T07:06:47.658Z" AS t, BASE64_ENCODE("x") AS bin, "663ca2de-2148-49f5-8763-80f1dc4fa5fe" AS u`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for i, c := range cols {
		got[c] = rows[0][i]
	}
	want := map[string]any{
		"i": int64(1), "f": -1.5, "s": "s", "b": true, "n": nil, "m": nil,
		"a": []any{int64(1), int64(2)}, "o": map[string]any{"x": int64(1)},
		"big": int64(9007199254740993), "maxint": int64(9223372036854775807),
		"tenth": 0.1, "e": "",
		"t": "2026-09-27T07:06:47.658Z", "u": "663ca2de-2148-49f5-8763-80f1dc4fa5fe",
	}
	for k, v := range want {
		if !reflect.DeepEqual(got[k], v) {
			t.Errorf("%s is %#v (%T), want %#v (%T)", k, got[k], got[k], v, v)
		}
	}
	if !slices.Contains(cols, "m") {
		t.Error("the MISSING column is not a column, but the signature names it (D18)")
	}
	// The server rounds this integer to a float64, and sends the digits of the
	// rounded value as an integer, which is too large for an int64.
	if d, ok := got["huge"].(*apd.Decimal); !ok || d.String() != "123456789012345680000000000000" {
		t.Errorf("huge is %#v (%T), want the *apd.Decimal of the digits that the server sent", got["huge"], got["huge"])
	}
}

func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "couchbase-8.0.3")
	rows, err := db.QueryContext(t.Context(), "SELECT 2 AS b, 1 AS a, 3 AS c") //nolint:rowserrcheck // The test reads the types of the columns, and no row.
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, ct := range types {
		if ct.DatabaseTypeName() != "NUMBER" {
			t.Errorf("%s has the type %q, want NUMBER", ct.Name(), ct.DatabaseTypeName())
		}
	}
}

// A null signature has rows for CREATE INDEX, which are objects, and for
// INFER, which are arrays.
func TestReplayNullSignature(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "couchbase-8.0.3")
	cols, rows, err := rowsOf(t, db, "CREATE INDEX dbimp_orders_person ON dbmeta.dbimp.orders(person)")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cols, []string{"id", "name", "state"}) || len(rows) != 1 || rows[0][2] != "online" {
		t.Errorf("CREATE INDEX gave %q and %v, want the columns id, name and state of one index that is online", cols, rows)
	}
	cols, rows, err = rowsOf(t, db, "INFER dbmeta.dbimp.people")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || len(rows) != 1 {
		t.Fatalf("INFER gave %q and %d rows, want one column and one row", cols, len(rows))
	}
	if _, ok := rows[0][0].([]any); !ok {
		t.Errorf("INFER gave a %T, want the array of the schemas", rows[0][0])
	}
}

// BEGIN WORK has the signature "json", and its row is an object that holds
// the txid, which each later statement of the transaction carries.
func TestReplayTransaction(t *testing.T) {
	t.Parallel()
	srv := dbimptest.ReplayRelease(t, testdata, "couchbase-8.0.3", nil)
	db, err := sql.Open("couchbase", "couchbase://u:p@"+strings.TrimPrefix(srv.URL, "http://")+"/?durability_level=none")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPSERT INTO dbmeta.dbimp.people (KEY, VALUE) VALUES ("tx1", {"v": 1})`,
		"SAVEPOINT s1",
		`UPSERT INTO dbmeta.dbimp.people (KEY, VALUE) VALUES ("tx2", {"v": 2})`,
		"ROLLBACK WORK TO SAVEPOINT s1",
	} {
		if _, err := tx.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestReplayRaw(t *testing.T) {
	t.Parallel()
	cols, rows, err := rowsOf(t, replayDB(t, "couchbase-8.0.3"), "SELECT RAW a FROM ARRAY_RANGE(0, 3) AS a")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || len(rows) != 3 || rows[2][0] != int64(2) {
		t.Errorf("SELECT RAW gave %q and %v, want one column of 0, 1 and 2", cols, rows)
	}
}

func TestReplayMissingField(t *testing.T) {
	t.Parallel()
	cols, rows, err := rowsOf(t, replayDB(t, "couchbase-8.0.3"), `SELECT d.x, d.y, d.z FROM dbmeta AS d USE KEYS "dbimp::probe"`)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cols, []string{"x", "y", "z"}) || len(rows) != 1 {
		t.Fatalf("the result is %q %v", cols, rows)
	}
	if rows[0][0] != int64(1) || rows[0][1] != nil || rows[0][2] != nil {
		t.Errorf("the row is %v, want 1, NULL and MISSING as nil (D39)", rows[0])
	}
}

func TestReplayArguments(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "couchbase-8.0.3")
	_, rows, err := rowsOf(t, db, "SELECT $1 AS a, ? AS b, $2 AS c", 1, `it's "quoted"`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows[0], []any{int64(1), int64(1), `it's "quoted"`}) {
		t.Errorf("the row is %v", rows[0])
	}
	_, rows, err = rowsOf(t, db, "SELECT $name AS a, $obj AS b",
		sql.Named("name", "x"), sql.Named("obj", map[string]any{"k": []any{1, nil}}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rows[0], []any{"x", map[string]any{"k": []any{int64(1), nil}}}) {
		t.Errorf("the row is %v", rows[0])
	}
}

func TestReplaySyntaxError(t *testing.T) {
	t.Parallel()
	_, _, err := rowsOf(t, replayDB(t, "couchbase-8.0.3"), "SELEKT 1")
	var e couchbase.Error
	if !errors.As(err, &e) || e.Code != 3000 {
		t.Errorf("the error is %v, want the code 3000 before any row", err)
	}
}

func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	_, rows, err := rowsOf(t, replayDB(t, "couchbase-8.0.3"),
		`SELECT RAW CASE WHEN a < 3 THEN a ELSE ABORT("boom") END FROM ARRAY_RANGE(0, 5) AS a`)
	var e couchbase.Error
	if len(rows) != 3 || !errors.Is(err, dbimp.ErrIncomplete) || !errors.As(err, &e) || e.Code != 5011 {
		t.Errorf("read %d rows and %v, want 3 rows and the code 5011 as an incomplete result (D42)", len(rows), err)
	}
}

func TestReplayTimeout(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "couchbase-8.0.3")
	ctx := couchbase.WithOptions(t.Context(), couchbase.WithTimeout(50e6))
	rows, err := db.QueryContext(ctx, "SELECT COUNT(*) AS c FROM ARRAY_RANGE(0, 3000) AS a, ARRAY_RANGE(0, 3000) AS b")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	var e couchbase.Error
	if !errors.As(rows.Err(), &e) || e.Code != 1080 {
		t.Errorf("the error is %v, want the code 1080", rows.Err())
	}
}

func TestReplayStopped(t *testing.T) {
	t.Parallel()
	srv := dbimptest.ReplayRelease(t, testdata, "couchbase-8.0.3", func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		return strings.Contains(ex.Request.Body, "dbimp-cancel") && strings.Contains(ex.Request.Body, "COUNT")
	})
	db := openAt(t, srv.URL)
	defer db.Close()
	_, _, err := rowsOf(t, db, "any statement")
	var errs *couchbase.ResponseError
	if !errors.Is(err, dbimp.ErrIncomplete) || !errors.As(err, &errs) || errs.Status != "stopped" {
		t.Errorf("a stopped query gave %v, want an incomplete result with the status stopped (D42)", err)
	}
}

func TestReplayWrite(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "couchbase-8.0.3")
	res, err := db.ExecContext(t.Context(), `UPSERT INTO dbmeta (KEY, VALUE) VALUES ("dbimp::probe", {"x": 1, "y": null})`)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Errorf("RowsAffected = %d, %v, want 1", n, err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId = %v, want ErrNotSupported", err)
	}
}

func TestReplayDuplicateKey(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "couchbase-8.0.3")
	_, err := db.ExecContext(t.Context(), `INSERT INTO dbmeta.dbimp.people (KEY, VALUE) VALUES ("p1", {"name": "again"})`)
	var e couchbase.Error
	if !errors.As(err, &e) || e.Code != 12009 {
		t.Errorf("an insert of a key that exists gave %v, want the code 12009", err)
	}
}

func TestReplayAuthentication(t *testing.T) {
	t.Parallel()
	srv := dbimptest.ReplayRelease(t, testdata, "couchbase-8.0.3", func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		return ex.Response.Status == 401 && strings.Contains(ex.Request.Body, `"SELECT 1 AS a"`)
	})
	db := openAt(t, srv.URL)
	defer db.Close()
	_, _, err := rowsOf(t, db, "SELECT 1 AS a")
	var e couchbase.Error
	if !errors.As(err, &e) || e.Code != 2120 {
		t.Errorf("a wrong password gave %v, want the code 2120", err)
	}
}

func TestReplayMissingRole(t *testing.T) {
	t.Parallel()
	srv := dbimptest.ReplayRelease(t, testdata, "couchbase-8.0.3", func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		return ex.Response.Status == 401 && strings.Contains(ex.Request.Body, "system:user_info")
	})
	db := openAt(t, srv.URL)
	defer db.Close()
	_, _, err := rowsOf(t, db, "SELECT * FROM system:user_info")
	var errs *couchbase.ResponseError
	if !errors.As(err, &errs) || errs.HTTPStatus != 401 || len(errs.Errs) == 0 || errs.Errs[0].Code != 13014 {
		t.Errorf("a missing role gave %v, want HTTP 401 and the code 13014", err)
	}
}
