package libsql_test

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
	"github.com/xo/dbimp/libsql"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/libsql/. Each one decodes a real answer through the driver.

const (
	testdata = "../testdata/libsql"
	release  = "libsql-0.24.33"
)

// canon returns the canonical form of a body, with the baton dropped, so
// that two requests with the same statements compare equal.
func canon(b []byte) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	delete(m, "baton")
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	return string(out)
}

// onlyClose is the canonical body of a request that closes a stream.
const onlyClose = `{"requests":[{"type":"close"}]}`

// replay opens the driver against the recorded exchanges. A request matches
// an exchange of the same principal by its body alone, because step 6 sent
// the statements of Exec to /v2/pipeline, where the driver uses
// /v3/pipeline (D149). A request that only closes a stream matches the one
// that step 6 recorded.
func replay(t *testing.T) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, func(_ *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		if canon(body) == onlyClose {
			return canon(ex.Request.Content()) == onlyClose
		}
		return canon(body) == canon(ex.Request.Content())
	})
	db, err := sql.Open(libsql.Name, strings.Replace(srv.URL, "http://", "libsql://admin:tok@", 1)+"?tls=false")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(t *testing.T, db *sql.DB, query string) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
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

// TestReplayTypes holds D147 for each declared type, as the table of every
// type stored it (recorded: "every type on the cursor").
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	cols, got, err := readAll(t, replay(t), "SELECT * FROM dbimp_types ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Split("id i r t b n bo dt dtm ts j x v", " "); !slices.Equal(cols, want) {
		t.Errorf("the columns are %q, want %q", cols, want)
	}
	day := dbimp.Date{Year: 2026, Month: time.October, Day: 1}
	want := [][]any{
		{int64(1), int64(math.MinInt64), 1.5, "é'\"\\ x", []byte{0, 0xff}, 1.25, true, day,
			dbimp.LocalDateTime{Date: day, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}},
			time.Date(2026, time.October, 1, 12, 34, 56, 123456789, time.FixedZone("", 19800)),
			`{"k":[1,null]}`, int64(7), dbimp.Vector[float32]{1, 2, 3}},
		{int64(2), int64(math.MaxInt64), -math.MaxFloat64, "", []byte{}, 1.2345678901234567e19, false, "not a date",
			int64(1727699696), "x", "[]", "text", dbimp.Vector[float32]{0, 0, 0}},
		{int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
		{int64(4), 1.5, "abc", "42", "text in a blob", "abc", true, int64(42), "yesterday", 1.5, "not json", []byte{1}, nil},
	}
	if len(got) != len(want) {
		t.Fatalf("%d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if !equal(got[i], want[i]) {
			t.Errorf("row %d is\n%#v\nwant\n%#v", i+1, got[i], want[i])
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

// TestReplayErrorAfterRows holds D149: a step_error after some rows of the
// cursor is the error of Rows.Next, and wraps dbimp.ErrIncomplete (recorded:
// "an error inside the rows on the cursor").
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	_, got, err := readAll(t, replay(t), "WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 5) SELECT CASE WHEN v < 4 THEN v ELSE abs(-9223372036854775807 - 1) END FROM n")
	e, ok := errors.AsType[*libsql.Error](err)
	if len(got) != 3 || !ok || e.Message != "SQLite error: integer overflow" || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("read %d rows and %v, want 3 rows and the overflow, which wraps dbimp.ErrIncomplete", len(got), err)
	}
}

// TestReplayTwoSteps reads the first step of a cursor of two steps, which
// the driver never sends, to show that it stops at the end of the first
// (recorded: "feature: a cursor over two steps").
func TestReplayCursor(t *testing.T) {
	t.Parallel()
	cols, got, err := readAll(t, replay(t), "SELECT 1 AS one")
	if err != nil || !slices.Equal(cols, []string{"one"}) || !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
		t.Errorf("the cursor gave %q %#v %v, want one and 1", cols, got, err)
	}
}

// TestReplayExec holds D152: Exec gives affected_row_count and
// last_insert_rowid, and 0 for a rowid of null (recorded).
func TestReplayExec(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		query          string
		affected, last int64
	}{
		{"INSERT INTO dbimp_crud VALUES (1, 'a'), (2, 'b'), (3, 'c')", 3, 3},
		{"UPDATE dbimp_crud SET v = 'B' WHERE k = 2", 1, 0},
		{"CREATE INDEX dbimp_s_idx ON dbimp_s_parent (n)", 0, 0},
		{"INSERT INTO dbimp_crud VALUES (1, 'x') ON CONFLICT (k) DO UPDATE SET v = excluded.v || '!'", 1, 0},
	} {
		res, err := db.ExecContext(t.Context(), tt.query)
		if err != nil {
			t.Errorf("%s: %v", tt.query, err)
			continue
		}
		n, _ := res.RowsAffected()
		id, _ := res.LastInsertId()
		if n != tt.affected || id != tt.last {
			t.Errorf("%s gave %d rows and the id %d, want %d and %d", tt.query, n, id, tt.affected, tt.last)
		}
	}
}

// TestReplayErrors holds D152: an error of a statement arrives in its result,
// with HTTP 200, and keeps its code (recorded).
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for query, want := range map[string]string{
		"SELEC 1":                    "SQL_PARSE_ERROR",
		"SELECT * FROM dbimp_nosuch": "SQLITE_UNKNOWN",
		"SELECT 1; SELECT 2":         "SQL_MANY_STATEMENTS",
		"":                           "SQL_NO_STATEMENT",
	} {
		_, err := db.ExecContext(t.Context(), query)
		if e, ok := errors.AsType[*libsql.Error](err); !ok || e.Code != want || e.HTTPStatus != http.StatusOK {
			t.Errorf("%q gave %v, want the code %s", query, err, want)
		}
	}
}

// TestReplayRefusals holds what the server answers a token that can only
// read, and a token that is not valid (recorded).
func TestReplayRefusals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		principal, query string
		status           int
	}{
		{"ordinary", "INSERT INTO dbimp_crud VALUES (1, 'a'), (2, 'b'), (3, 'c')", http.StatusForbidden},
		{"administrator", "SELECT 1", http.StatusUnauthorized},
	} {
		srv := dbimptest.ReplayRelease(t, testdata, release, func(_ *http.Request, body []byte, ex *dbimptest.Exchange) bool {
			return ex.Response.Status == tt.status && canon(body) == canon(ex.Request.Content())
		})
		db, err := sql.Open(libsql.Name, strings.Replace(srv.URL, "http://", "libsql://u:tok@", 1)+"?tls=false")
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(t.Context(), tt.query)
		if e, ok := errors.AsType[*libsql.Error](err); !ok || e.HTTPStatus != tt.status || e.Message == "" {
			t.Errorf("%s as %s gave %v, want HTTP %d with its message", tt.query, tt.principal, err, tt.status)
		}
		_ = db.Close()
	}
}

// TestReplayStatements compares the statements that the driver writes with
// the ones that step 6 sent and the server took (recorded: "positional
// parameters" and "named parameters with no prefix"). database/sql takes a
// name with no prefix, and the server adds it.
func TestReplayStatements(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		query string
		args  []any
	}{
		{"SELECT ?, ?, ?, ?, ?, typeof(?)", []any{1, 1.5, "a", []byte{0, 0xff}, nil, 1.0}},
		{"SELECT :a, @b, $c", []any{sql.Named("a", 1), sql.Named("b", "x"), sql.Named("c", 2.5)}},
	} {
		if _, err := db.ExecContext(t.Context(), tt.query, tt.args...); err != nil {
			t.Errorf("%s: %v, want the recorded answer", tt.query, err)
		}
	}
}
