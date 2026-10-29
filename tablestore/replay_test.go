package tablestore //nolint:testpackage // The tests read the error types of the package.

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/xo/dbimp"
)

// values runs the query and returns the columns and the rows, each value as the
// Go value that database/sql scans into an *any.
func values(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		row := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return cols, out
}

// TestReplaySelect reads a SELECT of literals. The columns come with their types,
// and each value has the Go type of its column (recorded: "select 1").
func TestReplaySelect(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows := values(t, db, "SELECT 1 AS a, 'x' AS b;")
	if want := []string{"a", "b"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
	if want := [][]any{{int64(1), "x"}}; !reflect.DeepEqual(rows, want) {
		t.Errorf("rows %#v, want %#v", rows, want)
	}
}

// TestReplayComments reads a statement with a comment before it, one inside it
// and a statement that has no semicolon. The server accepts all of them
// (recorded: "a line comment", "a block comment", "select with no semicolon").
func TestReplayComments(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, query := range []string{"-- c\nSELECT 1 AS a;", "/* c */ SELECT 1 AS a;", "SELECT 1 AS a"} {
		if _, rows := values(t, db, query); !reflect.DeepEqual(rows, [][]any{{int64(1)}}) {
			t.Errorf("%q gave %#v, want one row with 1", query, rows)
		}
	}
}

// TestReplayTypes reads a table that holds a value of each type, a row with no
// value but its key, an empty string, a negative zero and the extremes of an
// integer (recorded: "select every row").
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows := values(t, db, "SELECT * FROM dbimp_it_t ORDER BY id;")
	if want := []string{"id", "s", "i", "d", "b", "x"}; !slices.Equal(cols, want) {
		t.Fatalf("columns %q, want %q", cols, want)
	}
	want := [][]any{
		{int64(1), "a,b\"c\n", int64(-1 << 63), 1.5, true, []byte{0, 0xff, 0x10}},
		{int64(2), nil, nil, nil, nil, nil},
		{int64(3), "", int64(0), 0.0, false, []byte{}},
		{int64(4), "é😀", int64(1<<63 - 1), 1e308, true, []byte("abc")},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows %#v, want %#v", rows, want)
	}
}

// TestReplayNoRows reads a result with no rows. The column arrives, and no row
// does (recorded: "a result with no rows").
func TestReplayNoRows(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows := values(t, db, "SELECT id FROM dbimp_it_t WHERE id = -1;")
	if !slices.Equal(cols, []string{"id"}) || len(rows) != 0 {
		t.Errorf("columns %q and %d rows, want id and none", cols, len(rows))
	}
}

// TestReplayColumnNames keeps the order and the names that the statement gives,
// a repeated name and the text of an expression with no name (recorded: "columns
// in order and a repeated name", "no column name").
func TestReplayColumnNames(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, _ := values(t, db, "SELECT 2 AS b, 1 AS a, 3 AS b;")
	if want := []string{"b", "a", "b"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
	cols, _ = values(t, db, "SELECT 1, 'x', 1 + 1;")
	if want := []string{"1", "x", "1 + 1"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
}

// TestReplayDates reads a DATETIME, a TIMESTAMP and a DATE column. Each is stored
// as an integer, and the server sends the instant as microseconds, and the date
// as seconds (recorded: "select the dates as int").
func TestReplayDates(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows := values(t, db, "SELECT * FROM dbimp_it_dt_int;")
	if want := []string{"id", "dt", "ts", "d"}; !slices.Equal(cols, want) {
		t.Fatalf("columns %q, want %q", cols, want)
	}
	if len(rows) != 1 {
		t.Fatalf("read %d rows, want 1", len(rows))
	}
	want := []any{
		int64(1),
		dbimp.LocalDateTimeOf(mustLocal(t, 1761699723123)),
		mustInstant(1761699723123456),
		dbimp.DateOf(mustInstant(28800000000)),
	}
	if !reflect.DeepEqual(rows[0], want) {
		t.Errorf("row %#v, want %#v", rows[0], want)
	}
}

// TestReplayNow reads NOW(), which is a DATETIME column (recorded: "lead: now").
func TestReplayNow(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, rows := values(t, db, "SELECT NOW() AS a;")
	want := dbimp.LocalDateTimeOf(mustLocal(t, 1793265239000000))
	if len(rows) != 1 || !reflect.DeepEqual(rows[0][0], want) {
		t.Errorf("rows %#v, want %v", rows, want)
	}
}

// TestReplayLargeResult reads the result of 2600 rows that the server sent in
// one answer, and reads each value (recorded: "every row of 2600").
func TestReplayLargeResult(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, rows := values(t, db, "SELECT id, s FROM dbimp_it_big;")
	if len(rows) != 2600 {
		t.Fatalf("read %d rows, want 2600", len(rows))
	}
	for i, row := range rows {
		if want := []any{int64(i), "row " + itoa(i)}; !reflect.DeepEqual(row, want) {
			t.Fatalf("row %d is %#v, want %#v", i, row, want)
		}
	}
}

// itoa writes n in base 10.
func itoa(n int) string {
	return string(appendInt10(nil, n))
}

func appendInt10(b []byte, n int) []byte {
	if n >= 10 {
		b = appendInt10(b, n/10)
	}
	return append(b, byte('0'+n%10))
}

// TestReplayTextResults reads the statements that answer text: DESC, SHOW TABLES
// and SHOW CREATE TABLE, and the version (recorded: "describe the mapping", "show
// tables", "lead: show create table", "lead: the version variable").
func TestReplayTextResults(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows := values(t, db, "DESC dbimp_it_t;")
	if want := []string{"Field", "Type", "Null", "Key", "Default", "Extra"}; !slices.Equal(cols, want) {
		t.Errorf("DESC gave the columns %q, want %q", cols, want)
	}
	if len(rows) != 6 || rows[0][0] != "id" || rows[0][1] != "bigint(20)" {
		t.Errorf("DESC gave %#v", rows)
	}
	if _, rows = values(t, db, "SHOW TABLES;"); len(rows) != 1 || rows[0][0] != "dbimp_it_t" {
		t.Errorf("SHOW TABLES gave %#v", rows)
	}
	if cols, rows = values(t, db, "SHOW CREATE TABLE dbimp_it_t;"); len(rows) != 1 || cols[1] != "Create Table" {
		t.Errorf("SHOW CREATE TABLE gave %q and %#v", cols, rows)
	}
	if _, rows = values(t, db, "SELECT @@version;"); len(rows) != 1 || rows[0][0] != "5.7.25-SQLFrontend-b30fece-dirty" {
		t.Errorf("SELECT @@version gave %#v", rows)
	}
}

// TestReplayExec runs the statements that change rows, with their arguments
// written as literals, and reads the count that the answer holds (recorded:
// "insert", "update", "delete"). The server answers only the request that has
// the same text, so each request also shows what the driver wrote for each
// argument.
func TestReplayExec(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		name  string
		query string
		args  []any
	}{
		{"insert", "INSERT INTO dbimp_it_t (id, s) VALUES (?, ?);", []any{int64(100), "x"}},
		{"update", "UPDATE dbimp_it_t SET s = ? WHERE id = ?;", []any{"y", int64(1)}},
		{"delete", "DELETE FROM dbimp_it_t WHERE id = ?;", []any{int64(1)}},
		{"every type", "INSERT INTO dbimp_it_t (id, s, i, d, b, x) VALUES (?, ?, ?, ?, ?, ?);", []any{int64(101), "é", int64(-1 << 63), 1.5, true, []byte{0, 0xff}}},
	} {
		res, err := db.ExecContext(t.Context(), tt.query, tt.args...)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			t.Errorf("%s: %d rows affected and %v, want 1", tt.name, n, err)
		}
		if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: LastInsertId gave %v, want dbimp.ErrNotSupported", tt.name, err)
		}
	}
}

// TestReplayDDL runs a statement that holds no rows and no column, and reads its
// count, which is 0 (recorded: "create the mapping of the table of types", "drop
// the mapping").
func TestReplayDDL(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, query := range []string{
		"CREATE TABLE dbimp_it_t (id BIGINT, s MEDIUMTEXT, i BIGINT, d DOUBLE, b BOOL, x MEDIUMBLOB, PRIMARY KEY(id));",
		"DROP MAPPING TABLE IF EXISTS dbimp_it_t;",
	} {
		res, err := db.ExecContext(t.Context(), query)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if n, err := res.RowsAffected(); err != nil || n != 0 {
			t.Errorf("%s: %d rows affected and %v, want 0", query, n, err)
		}
	}
}

// TestReplayErrors reads the recorded errors of the server. Each is an *Error
// with the status and the code of the answer, and none is a refusal of the
// credential (recorded: "a missing table", "a syntax error", "two statements").
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		query string
		code  string
		text  string
	}{
		{"SELECT * FROM dbimp_it_missing;", "OTSParameterInvalid", "doesn't exist"},
		{"SELEC 1;", "OTSParameterInvalid", "error in your SQL syntax"},
		{"SELECT nope FROM dbimp_it_t;", "OTSParameterInvalid", "Unknown column"},
		{"SELECT 1 AS a; SELECT 2 AS a;", "OTSParameterInvalid", "more than one query"},
		{"BEGIN;", "OTSUnsupportOperation", "not supported"},
		{"INSERT INTO dbimp_it_t (id, s, i) VALUES (100, NULL, 5);", "OTSParameterInvalid", "Duplicate entry"},
	} {
		_, err := db.ExecContext(t.Context(), tt.query)
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("%s: the error is %v, want a *Error", tt.query, err)
			continue
		}
		if e.Code != tt.code || e.HTTPStatus != 400 || !contains(e.Message, tt.text) {
			t.Errorf("%s: the error is %d %s %q, want 400 %s with %q", tt.query, e.HTTPStatus, e.Code, e.Message, tt.code, tt.text)
		}
		if errors.Is(err, dbimp.ErrAuthentication) {
			t.Errorf("%s: the error matches dbimp.ErrAuthentication", tt.query)
		}
	}
}

// contains reports whether s holds sub.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestReplayNoTransaction holds that a transaction never starts, and that a
// statement does not hold one (D20).
func TestReplayNoTransaction(t *testing.T) {
	t.Parallel()
	db := replay(t)
	tx, err := db.BeginTx(context.Background(), nil)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("BeginTx returned a transaction")
	}
	if !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("the error is %v, want dbimp.ErrNotSupported", err)
	}
}
