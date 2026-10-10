package athena //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// TestReplaySelect reads a SELECT through the three calls of a statement. The
// first row of a SELECT is the header row, which the driver drops, and each
// value has the Go type of its column (recorded: "a select").
func TestReplaySelect(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows, err := read(t, db, "SELECT 1 AS a, 'x' AS b")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
	if want := [][]string{{"int64(1)", `string("x")`}}; !equalRows(rows, want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
}

// equalRows reports whether two lists of rows are equal.
func equalRows(a, b [][]string) bool {
	return slices.EqualFunc(a, b, slices.Equal[[]string])
}

// TestReplayComments reads a statement with a comment before it and one after
// it, and a statement with a trailing semicolon. The server accepts both
// (recorded: "a select with a comment", "a select with a trailing semicolon").
func TestReplayComments(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, query := range []string{"/* a comment */ SELECT 1 -- and another", "SELECT 1;"} {
		cols, rows, err := read(t, db, query)
		if err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		if want := [][]string{{"int64(1)"}}; !equalRows(rows, want) {
			t.Errorf("%q gave %q, want %q", query, rows, want)
		}
		if len(cols) != 1 {
			t.Errorf("%q gave the columns %q, want one", query, cols)
		}
	}
}

// TestReplayNoRows reads a result with no rows. The columns arrive, and the
// header row is dropped, so there is no row (recorded: "columns of a result with
// no rows").
func TestReplayNoRows(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows, err := read(t, db, "SELECT id, name FROM dbimp_it_ext WHERE id = -1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"id", "name"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
	if len(rows) != 0 {
		t.Errorf("rows %q, want none", rows)
	}
}

// TestReplayColumnsKeepTheirOrderAndNames reads two columns with one name, and a
// column whose name has a capital letter. The server keeps both (recorded:
// "columns that share a name", "a column in lower case"), and so does the driver
// (D18).
func TestReplayColumnsKeepTheirOrderAndNames(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows, err := read(t, db, "SELECT 1 AS a, 2 AS a")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "a"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
	if want := [][]string{{"int64(1)", "int64(2)"}}; !equalRows(rows, want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
	cols, _, err = read(t, db, `SELECT 1 AS "Mixed"`)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Mixed"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
}

// TestReplayEmptyStringAndNull reads an empty string and a NULL, which the wire
// tells apart (recorded: "an empty string and a null in a literal", "select the
// empty string and the null"). A NULL is nil and an empty string is "" (D18).
func TestReplayEmptyStringAndNull(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, rows, err := read(t, db, "SELECT '' AS e, CAST(NULL AS varchar) AS n")
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{`string("")`, "nil"}}; !equalRows(rows, want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
	_, rows, err = read(t, db, "SELECT id, name, name IS NULL AS isnull FROM dbimp_it_ext WHERE id >= 4 ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	// The files of earlier runs repeat each row, because DROP TABLE of an external
	// table leaves its files (recorded: "select the empty string and the null").
	want := [][]string{
		{"int64(4)", `string("")`, "bool(false)"},
		{"int64(5)", "nil", "bool(true)"},
	}
	if got := slices.CompactFunc(rows, slices.Equal[[]string]); !equalRows(got, want) {
		t.Errorf("rows %q, want %q, each repeated", rows, want)
	}
}

// TestReplayUtilityHasNoHeader reads statements of the type UTILITY, whose
// results have no header row (recorded: "show tables", "describe the table"). The
// result of DESCRIBE has three columns and one value in each row, so the other
// two values are NULL.
func TestReplayUtilityHasNoHeader(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows, err := read(t, db, "SHOW TABLES IN dbimp_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || len(rows) == 0 {
		t.Fatalf("SHOW TABLES gave the columns %q and %d rows, want one column and some rows", cols, len(rows))
	}
	if strings.HasPrefix(rows[0][0], `string("tab_name`) {
		t.Errorf("the first row is the header: %q", rows[0])
	}
	cols, rows, err = read(t, db, "DESCRIBE dbimp_it_ext")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"col_name", "data_type", "comment"}; !slices.Equal(cols, want) {
		t.Errorf("columns %q, want %q", cols, want)
	}
	if len(rows) != 2 || rows[0][1] != "nil" || rows[0][2] != "nil" || !strings.HasPrefix(rows[0][0], `string("id`) {
		t.Errorf("rows %q, want two rows that hold the text of the server in the first value and NULL in the others", rows)
	}
}

// TestReplayUpdateCount reads the count of the rows that a statement changed. An
// INSERT answers one column, no row, and the count in UpdateCount (recorded: "an
// insert into the iceberg table", "an update of the iceberg table", "a delete
// from the iceberg table", "a merge into the iceberg table").
func TestReplayUpdateCount(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		query string
		want  int64
	}{
		{"INSERT INTO dbimp_it_ice VALUES (1,'a'),(2,'b'),(3,'c')", 3},
		{"UPDATE dbimp_it_ice SET name = 'z' WHERE id < 3", 2},
		{"DELETE FROM dbimp_it_ice WHERE id = 3", 1},
		{"CREATE TABLE dbimp_it_ctas2 WITH (format='PARQUET') AS SELECT 1 AS id", 1},
	} {
		res, err := db.ExecContext(t.Context(), tt.query)
		if err != nil {
			t.Errorf("%q: %v", tt.query, err)
			continue
		}
		n, err := res.RowsAffected()
		if err != nil || n != tt.want {
			t.Errorf("%q changed %d rows, error %v, want %d", tt.query, n, err, tt.want)
		}
		if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%q: LastInsertId gave %v, want dbimp.ErrNotSupported", tt.query, err)
		}
	}
	// DESCRIBE answers no UpdateCount, so Exec has no count (D178).
	res, err := db.ExecContext(t.Context(), "DESCRIBE dbimp_it_ext")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected of DESCRIBE gave %v, want dbimp.ErrNotSupported", err)
	}
}

// TestReplayInsertAsQuery reads the rows of an INSERT with Query. The result has
// the column rows and no row, and the driver drops no row, because it has none
// to drop (recorded: "an insert into the external table").
func TestReplayInsertAsQuery(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, rows, err := read(t, db, "INSERT INTO dbimp_it_ext VALUES (1,'a'),(2,'b'),(3,'c')")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"rows"}; !slices.Equal(cols, want) || len(rows) != 0 {
		t.Errorf("columns %q and rows %q, want the column rows and no row", cols, rows)
	}
}

// TestReplayColumnTypes reads the metadata of the columns: the name of the type,
// the length, the precision and the scale, and whether the column can be NULL.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	db := replay(t)
	// The statement is the one that the recording holds, with every type in one
	// row (recorded: "every type in one row").
	rows, err := db.QueryContext(t.Context(), queryOf(t, 160))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ct := range types {
		names = append(names, ct.DatabaseTypeName())
	}
	want := []string{
		"TINYINT", "SMALLINT", "INTEGER", "BIGINT", "FLOAT", "DOUBLE", "DECIMAL", "VARCHAR", "CHAR", "BOOLEAN",
		"DATE", "TIME", "TIMESTAMP", "TIMESTAMP WITH TIME ZONE", "VARBINARY", "ARRAY", "MAP", "ROW", "JSON",
		"IPADDRESS", "UUID", "INTERVAL DAY TO SECOND", "INTERVAL YEAR TO MONTH", "INTEGER",
	}
	if !slices.Equal(names, want) {
		t.Errorf("type names %q, want %q", names, want)
	}
	if p, s, ok := types[6].DecimalSize(); !ok || p != 38 || s != 2 {
		t.Errorf("the decimal has the precision %d and the scale %d, ok %v, want 38, 2", p, s, ok)
	}
	if n, ok := types[8].Length(); !ok || n != 4 {
		t.Errorf("the char has the length %d, ok %v, want 4", n, ok)
	}
	if n, ok := types[7].Length(); !ok || n != 1<<63-1 {
		t.Errorf("the varchar has the length %d, ok %v, want the largest", n, ok)
	}
	for i, ct := range types {
		if nullable, ok := ct.Nullable(); !ok || !nullable {
			t.Errorf("column %d: Nullable gave %v, %v, want true, true", i, nullable, ok)
		}
	}
}

// TestReplayOpenFailsWithoutCredentials makes sure that a connector with no
// access key opens no connection, and sends nothing, because the driver reads no
// credential from the environment (D7).
func TestReplayOpenFailsWithoutCredentials(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, _ request, _ int) { t.Error("the connector sent a request") })
	cfg := testConfig()
	cfg.User, cfg.Password = "", ""
	db := open(t, cfg, f.srv.URL)
	if err := db.PingContext(context.Background()); !errors.Is(err, ErrNoCredentials) {
		t.Errorf("Ping gave %v, want ErrNoCredentials", err)
	}
}

// queryOf returns the statement of the StartQueryExecution that the file n
// holds.
func queryOf(t *testing.T, n int) string {
	t.Helper()
	var body struct {
		QueryString string
	}
	if err := json.Unmarshal(exchange(t, n).Request.Content(), &body); err != nil {
		t.Fatal(err)
	}
	return body.QueryString
}
