package rqlite_test

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/rqlite"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/rqlite/. Each one decodes a real answer through the driver.

const testdata = "../testdata/rqlite"

// releases are the releases that step 6 recorded.
var releases = []string{"rqlite-9.4.5", "rqlite-10.3.6"}

// canon returns the canonical form of a body of statements, so that two
// bodies that hold the same statements and arguments compare equal.
func canon(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	return string(out)
}

// replayOnly opens the driver against the recorded exchanges of release. A
// request matches an exchange by its statements alone, because the driver
// sends a query to /db/request with blob_array, where step 6 used every
// endpoint (D142). With blobs, only an exchange that asked for blob_array
// matches, and without it, only one that did not. The first exchange that
// matches answers, and the administrator comes first in each recording.
func replayOnly(t *testing.T, release string, blobs bool) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		if strings.Contains(ex.Request.Query, "blob_array") != blobs {
			return false
		}
		if r.Header.Get("Authorization") == "" {
			return false
		}
		return canon(body) == canon(ex.Request.Content())
	})
	db, err := sql.Open(rqlite.Name, strings.Replace(srv.URL, "http://", "rqlite://admin:secret@", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// replay opens the driver against the recorded exchanges of release that
// did not ask for blob_array.
func replay(t *testing.T, release string) *sql.DB {
	t.Helper()
	return replayOnly(t, release, false)
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

// serverError returns err as an *rqlite.Error, and fails the test if it is
// not one.
func serverError(t *testing.T, err error) *rqlite.Error {
	t.Helper()
	e, ok := errors.AsType[*rqlite.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want an *rqlite.Error", err, err)
	}
	return e
}

func date(y int, m time.Month, d int) dbimp.Date {
	return dbimp.Date{Year: y, Month: m, Day: d}
}

// TestReplayTypes holds D140 for each declared type, as the table of every
// type stored it (recorded: "every type as arrays of bytes"). A value of
// another storage class keeps the Go type of its JSON form, and a date that
// the server did not parse is the year 1, as the server sent it.
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayOnly(t, release, true)
		cols, got, err := readAll(t, db, "SELECT * FROM dbimp_types ORDER BY id")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if want := strings.Split("id i bi r d t vc b bo dt dtm ts n dec j u x", " "); !slices.Equal(cols, want) {
			t.Errorf("%s: the columns are %q, want %q", release, cols, want)
		}
		year1 := dbimp.LocalDateTime{Date: date(1, time.January, 1)}
		want := [][]any{
			{int64(1), int64(math.MinInt64), int64(math.MaxInt64), 1.5, 0.1, "é'\"\\ x", "abc", []byte{0x00, 0xff}, true,
				date(2026, time.September, 30),
				dbimp.LocalDateTime{Date: date(2026, time.September, 30), Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}},
				time.Date(2026, time.September, 30, 12, 34, 56, 123456789, time.FixedZone("", 19800)),
				1.25, 12.34, `{"k":[1,null,"s"]}`, "6ba7b810-9dad-11d1-80b4-00c04fd430c8", int64(7)},
			{int64(2), int64(0), int64(0), -math.MaxFloat64, 5e-324, "", "", []byte{}, false,
				date(1000, time.January, 1),
				dbimp.LocalDateTime{Date: date(9999, time.December, 31), Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59}},
				time.Date(2024, time.September, 30, 12, 34, 56, 0, time.UTC),
				12345678901234567000.0, int64(1000), "[]", []byte{0x6b, 0xa7, 0xb8, 0x10}, "text"},
			{int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
			{int64(4), 1.5, int64(12), 1.5, "abc", "42", "longer than ten characters", "text in a blob", true,
				date(1, time.January, 1), year1, time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC),
				"abc", "abc", "not json", "not a uuid", []byte{0x01}},
		}
		if len(got) != len(want) {
			t.Fatalf("%s: %d rows, want %d", release, len(got), len(want))
		}
		for i := range want {
			if !equal(got[i], want[i]) {
				t.Errorf("%s: row %d is\n%#v\nwant\n%#v", release, i+1, got[i], want[i])
			}
		}
	}
}

// equal compares two rows, and a time by its instant and its offset.
func equal(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		g, gok := got[i].(time.Time)
		w, wok := want[i].(time.Time)
		if gok && wok {
			_, go1 := g.Zone()
			_, wo := w.Zone()
			if !g.Equal(w) || go1 != wo {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(got[i], want[i]) {
			return false
		}
	}
	return true
}

// TestReplayColumnTypes holds D140 for the scan type and the name of each
// column.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	db := replayOnly(t, "rqlite-10.3.6", true)
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM dbimp_types ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ct := range cts {
		got = append(got, ct.DatabaseTypeName()+" "+ct.ScanType().String())
	}
	want := []string{
		"INTEGER int64", "INTEGER int64", "BIGINT int64", "REAL float64", "DOUBLE float64", "TEXT string",
		"VARCHAR(10) string", "BLOB []uint8", "BOOLEAN bool", "DATE dbimp.Date", "DATETIME dbimp.LocalDateTime",
		"TIMESTAMP time.Time", "NUMERIC interface {}", "DECIMAL(10,2) interface {}", "JSON interface {}",
		"UUID interface {}", "INTEGER int64",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the column types are\n%q\nwant\n%q", got, want)
	}
}

// TestReplayExpressions holds D140 for a column of an expression: its type
// is the storage class of the first row, or "" when that is NULL, and each
// value keeps the Go type of its JSON form (recorded).
func TestReplayExpressions(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			want  [][]any
		}{
			{"SELECT v FROM (SELECT NULL AS v UNION ALL SELECT 1)", [][]any{{nil}, {int64(1)}}},
			// Without blob_array the BLOB of the last row is base64, which the
			// driver cannot tell from text, so every request asks for it
			// (D142).
			{"SELECT v FROM (SELECT 1 AS v UNION ALL SELECT 'a' UNION ALL SELECT 1.5 UNION ALL SELECT x'00')", [][]any{{int64(1)}, {"a"}, {1.5}, {"AA=="}}},
			{"SELECT 9223372036854775808, -9223372036854775809", [][]any{{9223372036854775808.0, -9223372036854775808.0}}},
			{"SELECT 0.0 / 0", [][]any{{nil}}},
		} {
			_, got, err := readAll(t, db, tt.query)
			if err != nil {
				t.Errorf("%s: %s: %v", release, tt.query, err)
				continue
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %s gave %#v, want %#v", release, tt.query, got, tt.want)
			}
		}
	}
}

// TestReplayBlobOfAnExpression shows the fault of the server that the driver
// cannot undo: a BLOB of an expression arrives as text, with U+FFFD in place
// of each byte that is not UTF-8, with blob_array too (recorded).
func TestReplayBlobOfAnExpression(t *testing.T) {
	t.Parallel()
	db := replayOnly(t, "rqlite-10.3.6", true)
	_, got, err := readAll(t, db, "SELECT 1, 1.5, 'a', x'00ff', NULL, true")
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]any{{int64(1), 1.5, "a", "\x00�", nil, int64(1)}}; !reflect.DeepEqual(got, want) {
		t.Errorf("the literals gave %#v, want %#v", got, want)
	}
}

// TestReplayInfinity holds D142: the server fails the whole answer with HTTP
// 500 for an infinity (recorded).
func TestReplayInfinity(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		_, _, err := readAll(t, replay(t, release), "SELECT 1e999")
		if e := serverError(t, err); e.HTTPStatus != http.StatusInternalServerError || !strings.Contains(e.Message, "+Inf") {
			t.Errorf("%s: an infinity gave %v, want HTTP 500 that names +Inf", release, err)
		}
	}
}

// TestReplayErrors holds D142: an error of SQL arrives in the result, with
// HTTP 200, and is the error of the query (recorded).
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for query, want := range map[string]string{
			"SELEC 1":                    `near "SELEC": syntax error`,
			"SELECT * FROM dbimp_nosuch": "no such table: dbimp_nosuch",
			"WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 5) SELECT CASE WHEN v < 4 THEN v ELSE abs(-9223372036854775807 - 1) END FROM n": "integer overflow",
		} {
			_, rows, err := readAll(t, db, query)
			if e := serverError(t, err); e.Message != want || e.HTTPStatus != http.StatusOK || len(rows) != 0 {
				t.Errorf("%s: %s gave %d rows and %v, want no rows and %q", release, query, len(rows), err, want)
			}
			if errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("%s: %s gave %v, which wraps dbimp.ErrIncomplete before any row", release, query, err)
			}
		}
	}
}

// TestReplayWrongPassword holds D141: a wrong password is HTTP 401 with no
// body (recorded).
func TestReplayWrongPassword(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		srv := dbimptest.ReplayRelease(t, testdata, release, func(_ *http.Request, body []byte, ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusUnauthorized && canon(body) == canon(ex.Request.Content())
		})
		db, err := sql.Open(rqlite.Name, strings.Replace(srv.URL, "http://", "rqlite://admin:wrong@", 1))
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = readAll(t, db, "SELECT 1")
		if e := serverError(t, err); e.HTTPStatus != http.StatusUnauthorized {
			t.Errorf("%s: a wrong password gave %v, want HTTP 401", release, err)
		}
		_ = db.Close()
	}
}

// TestReplayParameters holds D143: the server binds each argument that the
// driver sends (recorded).
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			args  []any
			want  []any
		}{
			{"SELECT ?, ?, ?, ?, ?", []any{1, 1.5, "a", true, nil}, []any{int64(1), 1.5, "a", int64(1), nil}},
			{"SELECT :a, :b, $c, @d", []any{sql.Named("a", 1), sql.Named("b", "x"), sql.Named("c", 2.5), sql.Named("d", nil)}, []any{int64(1), "x", 2.5, nil}},
			{"SELECT ?, typeof(?)", []any{int64(math.MaxInt64), int64(math.MaxInt64)}, []any{int64(math.MaxInt64), "integer"}},
			{"SELECT ?, typeof(?)", []any{1.0, 1.0}, []any{1.0, "real"}},
			{"SELECT ?2, ?1", []any{"a", "b"}, []any{"b", "a"}},
			{"SELECT '?', ? -- ?", []any{1}, []any{"?", int64(1)}},
		} {
			_, got, err := readAll(t, db, tt.query, tt.args...)
			if err != nil {
				t.Errorf("%s: %s: %v", release, tt.query, err)
				continue
			}
			if len(got) != 1 || !reflect.DeepEqual(got[0], tt.want) {
				t.Errorf("%s: %s gave %#v, want %#v", release, tt.query, got, tt.want)
			}
		}
	}
}

// TestReplayExec holds D142: Exec gives the counts of the server for a
// statement that counts rows, and 0 for any other, whose counts are those of
// an earlier write (recorded).
func TestReplayExec(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query          string
			affected, last int64
		}{
			{"INSERT INTO dbimp_crud VALUES (1, 'a'), (2, 'b'), (3, 'c')", 3, 3},
			{"UPDATE dbimp_crud SET v = 'B' WHERE k = 2", 1, 3},
			{"UPDATE dbimp_types SET i = i WHERE id < 0", 0, 1},
			{"CREATE INDEX dbimp_s_idx ON dbimp_s_parent (n)", 0, 0},
			{"CREATE VIEW dbimp_s_view AS SELECT id, name FROM dbimp_s_parent", 0, 0},
		} {
			res, err := db.ExecContext(t.Context(), tt.query)
			if err != nil {
				t.Errorf("%s: %s: %v", release, tt.query, err)
				continue
			}
			n, _ := res.RowsAffected()
			id, _ := res.LastInsertId()
			if n != tt.affected || id != tt.last {
				t.Errorf("%s: %s gave %d rows and the id %d, want %d and %d", release, tt.query, n, id, tt.affected, tt.last)
			}
		}
	}
}

// TestReplayReturning holds D142: a query of a write with RETURNING returns
// its rows (recorded: "crud: returning on request").
func TestReplayReturning(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		_, got, err := readAll(t, replay(t, release), "UPDATE dbimp_crud SET v = 'E' WHERE k = 5 RETURNING k, v")
		if err != nil {
			t.Errorf("%s: %v", release, err)
			continue
		}
		if want := [][]any{{int64(5), "E"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: RETURNING gave %#v, want %#v", release, got, want)
		}
	}
}

// TestReplayStatements holds what the server does with the text of one
// statement (recorded): two reads give the result of the last, and an empty
// statement gives no columns.
func TestReplayStatements(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		cols, got, err := readAll(t, db, "SELECT 1 AS a; SELECT 2 AS b")
		if err != nil || !slices.Equal(cols, []string{"b"}) || !reflect.DeepEqual(got, [][]any{{int64(2)}}) {
			t.Errorf("%s: two statements gave %q %#v %v, want the column b and 2", release, cols, got, err)
		}
		for _, q := range []string{"", "-- nothing"} {
			cols, got, err := readAll(t, db, q)
			if err != nil || len(cols) != 0 || len(got) != 0 {
				t.Errorf("%s: %q gave %q %#v %v, want no columns and no rows", release, q, cols, got, err)
			}
		}
	}
}

// TestReplayLargeResult holds D25: a result of 20000 rows reads to its end,
// one token at a time (recorded).
func TestReplayLargeResult(t *testing.T) {
	t.Parallel()
	db := replay(t, "rqlite-10.3.6")
	_, got, err := readAll(t, db, "WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 20000) SELECT v FROM n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20000 || got[0][0] != int64(1) || got[19999][0] != int64(20000) {
		t.Errorf("read %d rows, want 1 to 20000", len(got))
	}
}
