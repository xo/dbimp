package databricks_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databricks"
)

// The tests of this file are the tests that testdata/databricks/features.json names,
// one subtest for each entry, in the order of the file (step 14a). A test of an entry
// that the survey marks no sends the operation and expects the refusal of the server,
// or shows that the server ignores it. Each test makes its own tables, with the
// prefix of this run, and drops them when it ends, even when it fails.

// rowsOf reads the rows of a statement as texts, for a comparison.
func rowsOf(t testing.TB, db *sql.DB, q string, args ...any) [][]any {
	t.Helper()
	return query(t, db, q, args...)
}

// checkRows fails the test for each row that differs from want.
func checkRows(t testing.TB, got, want [][]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !slices.EqualFunc(got[i], want[i], same) {
			t.Errorf("row %d is\n%#v\nwant\n%#v", i, got[i], want[i])
		}
	}
}

// affected returns the count of rows that a result names, and fails the test when it names
// none.
func affected(t testing.TB, res sql.Result) int64 {
	t.Helper()
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected: %v", err)
	}
	return n
}

// TestIntegrationCRUD holds each statement of CRUD of features.json, and the count of rows
// that Exec returns for it (D193 item 8). The subtests run in order, on one table.
func TestIntegrationCRUD(t *testing.T) {
	db := connect(t)
	tbl, tbl2 := table("crud"), table("crud_ctas")
	exec(t, db, "CREATE TABLE "+tbl+" (id INT, s STRING) USING DELTA")
	drop(t, db, "TABLE", tbl, tbl2)
	t.Run("insert", func(t *testing.T) {
		res := exec(t, db, "INSERT INTO "+tbl+" VALUES (?, ?), (?, ?), (?, ?)", int64(1), "a", int64(2), "b", int64(3), "c")
		if n := affected(t, res); n != 3 {
			t.Errorf("the insert changed %d rows, want 3", n)
		}
	})
	t.Run("select", func(t *testing.T) {
		checkRows(t, rowsOf(t, db, "SELECT id, s FROM "+tbl+" ORDER BY id"), [][]any{{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"}})
		checkRows(t, rowsOf(t, db, "SELECT s FROM "+tbl+" WHERE id = ?", int64(2)), [][]any{{"b"}})
	})
	t.Run("update", func(t *testing.T) {
		res := exec(t, db, "UPDATE "+tbl+" SET s = ? WHERE id > ?", "z", int64(1))
		if n := affected(t, res); n != 2 {
			t.Errorf("the update changed %d rows, want 2", n)
		}
		checkRows(t, rowsOf(t, db, "SELECT id, s FROM "+tbl+" ORDER BY id"), [][]any{{int64(1), "a"}, {int64(2), "z"}, {int64(3), "z"}})
		// An update that matches no row changes none, and says so.
		if n := affected(t, exec(t, db, "UPDATE "+tbl+" SET s = 'q' WHERE id < 0")); n != 0 {
			t.Errorf("an update of no row changed %d rows, want 0", n)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if n := affected(t, exec(t, db, "DELETE FROM "+tbl+" WHERE id = ?", int64(1))); n != 1 {
			t.Errorf("the delete changed %d rows, want 1", n)
		}
		checkRows(t, rowsOf(t, db, "SELECT id, s FROM "+tbl+" ORDER BY id"), [][]any{{int64(2), "z"}, {int64(3), "z"}})
	})
	t.Run("merge", func(t *testing.T) {
		res := exec(t, db, "MERGE INTO "+tbl+" t USING (SELECT 2 AS id, 'm' AS s UNION ALL SELECT 99, 'n') u ON t.id = u.id WHEN MATCHED THEN UPDATE SET s = u.s WHEN NOT MATCHED THEN INSERT (id, s) VALUES (u.id, u.s)")
		if n := affected(t, res); n != 2 {
			t.Errorf("the merge changed %d rows, want 2", n)
		}
		checkRows(t, rowsOf(t, db, "SELECT id, s FROM "+tbl+" ORDER BY id"), [][]any{{int64(2), "m"}, {int64(3), "z"}, {int64(99), "n"}})
	})
	t.Run("truncate", func(t *testing.T) {
		res := exec(t, db, "TRUNCATE TABLE "+tbl)
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("RowsAffected of a truncate gave %v, want dbimp.ErrNotSupported", err)
		}
		checkRows(t, rowsOf(t, db, "SELECT count(*) FROM "+tbl), [][]any{{int64(0)}})
	})
	t.Run("create table as select", func(t *testing.T) {
		exec(t, db, "INSERT INTO "+tbl+" VALUES (1, 'a'), (2, 'b')")
		res := exec(t, db, "CREATE TABLE "+tbl2+" AS SELECT id, s FROM "+tbl)
		// The answer names the counts and holds no row, so the count is not known
		// (measured).
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("RowsAffected of a create table as select gave %v, want dbimp.ErrNotSupported", err)
		}
		checkRows(t, rowsOf(t, db, "SELECT id, s FROM "+tbl2+" ORDER BY id"), [][]any{{int64(1), "a"}, {int64(2), "b"}})
	})
}

// infoSchema returns the information_schema of the current catalog, with the catalog
// written out.
func infoSchema(t testing.TB, db *sql.DB) string {
	t.Helper()
	return fmt.Sprint(scalar(t, db, "SELECT current_catalog()")) + ".information_schema"
}

// constraintTypes returns the constraint types of the table, by constraint name.
func constraintTypes(t testing.TB, db *sql.DB, tbl string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range query(t, db, "SELECT constraint_name, constraint_type FROM "+infoSchema(t, db)+".table_constraints WHERE table_schema = current_schema() AND table_name = ?", tbl) {
		out[fmt.Sprint(row[0])] = fmt.Sprint(row[1])
	}
	return out
}

// TestIntegrationSchema holds each operation on a schema of features.json. A key is
// declared and not enforced (measured), so the entries "enforced primary key" and
// "enforced foreign key" send a row that a key would refuse and see that the server
// takes it.
func TestIntegrationSchema(t *testing.T) {
	db := connect(t)
	// The tables that several subtests use are dropped when the whole test ends, and
	// not when the subtest that made them ends.
	outer := t
	t.Run("table", func(t *testing.T) {
		tbl := table("sch_table")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT NOT NULL, s STRING, d DECIMAL(10,2)) USING DELTA")
		drop(t, db, "TABLE", tbl)
		got := query(t, db, "SELECT column_name, data_type FROM "+infoSchema(t, db)+".columns WHERE table_schema = current_schema() AND table_name = ? ORDER BY ordinal_position", tbl)
		checkRows(t, got, [][]any{{"id", "INT"}, {"s", "STRING"}, {"d", "DECIMAL"}})
	})
	pk, fk := table("sch_pk"), table("sch_fk")
	t.Run("primary key", func(t *testing.T) {
		exec(t, db, "CREATE TABLE "+pk+" (id BIGINT NOT NULL, s STRING, CONSTRAINT "+pk+" PRIMARY KEY (id))")
		drop(outer, db, "TABLE", pk)
		if got := constraintTypes(t, db, pk); got[pk] != "PRIMARY KEY" {
			t.Errorf("the constraints are %v, want a primary key named %s", got, pk)
		}
	})
	t.Run("foreign key", func(t *testing.T) {
		if scalar(t, db, "SELECT count(*) FROM "+infoSchema(t, db)+".tables WHERE table_schema = current_schema() AND table_name = ?", pk) == int64(0) {
			t.Skip("the table of the primary key does not exist, because the subtest before it did not run")
		}
		exec(t, db, "CREATE TABLE "+fk+" (id BIGINT NOT NULL, pk_id BIGINT, CONSTRAINT "+fk+"_pk FOREIGN KEY (pk_id) REFERENCES "+pk+" (id))")
		drop(outer, db, "TABLE", fk)
		if got := constraintTypes(t, db, fk); got[fk+"_pk"] != "FOREIGN KEY" {
			t.Errorf("the constraints are %v, want a foreign key named %s_pk", got, fk)
		}
	})
	t.Run("enforced primary key", func(t *testing.T) {
		exec(t, db, "INSERT INTO "+pk+" VALUES (1, 'a'), (1, 'b')")
		if got := scalar(t, db, "SELECT count(*) FROM "+pk+" WHERE id = 1"); got != int64(2) {
			t.Errorf("the table holds %v rows with one key, want 2, because the server does not enforce the key", got)
		}
	})
	t.Run("enforced foreign key", func(t *testing.T) {
		exec(t, db, "INSERT INTO "+fk+" VALUES (1, 99)")
		if got := scalar(t, db, "SELECT count(*) FROM "+fk+" WHERE pk_id = 99"); got != int64(1) {
			t.Errorf("the table holds %v rows of a key with no parent, want 1, because the server does not enforce the key", got)
		}
	})
	t.Run("view", func(t *testing.T) {
		src, v := table("sch_view_src"), table("sch_view")
		exec(t, db, "CREATE TABLE "+src+" (id INT, s STRING)")
		drop(t, db, "TABLE", src)
		exec(t, db, "INSERT INTO "+src+" VALUES (1, 'a'), (2, 'b')")
		exec(t, db, "CREATE VIEW "+v+" AS SELECT id, s FROM "+src+" WHERE id > 1")
		drop(t, db, "VIEW", v)
		checkRows(t, query(t, db, "SELECT id, s FROM "+v), [][]any{{int64(2), "b"}})
	})
	t.Run("temporary view", func(t *testing.T) {
		v := table("sch_temp")
		exec(t, db, "CREATE OR REPLACE TEMPORARY VIEW "+v+" AS SELECT 5 AS a")
		// There is no session, so the view is gone for the next request (measured).
		err := queryError(t.Context(), db, "SELECT * FROM "+v)
		if e := serverError(t, err); !strings.Contains(e.Message, "TABLE_OR_VIEW_NOT_FOUND") {
			t.Errorf("the next request gave %+v, want TABLE_OR_VIEW_NOT_FOUND", *e)
		}
	})
	t.Run("default value", func(t *testing.T) {
		tbl := table("sch_default")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT, n INT DEFAULT 5, s STRING DEFAULT 'x') TBLPROPERTIES ('delta.feature.allowColumnDefaults' = 'supported')")
		drop(t, db, "TABLE", tbl)
		exec(t, db, "INSERT INTO "+tbl+" (id) VALUES (1)")
		checkRows(t, query(t, db, "SELECT id, n, s FROM "+tbl), [][]any{{int64(1), int64(5), "x"}})
	})
	t.Run("generated column", func(t *testing.T) {
		tbl := table("sch_gen")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT, d INT GENERATED ALWAYS AS (id * 2))")
		drop(t, db, "TABLE", tbl)
		exec(t, db, "INSERT INTO "+tbl+" (id) VALUES (1), (2)")
		checkRows(t, query(t, db, "SELECT id, d FROM "+tbl+" ORDER BY id"), [][]any{{int64(1), int64(2)}, {int64(2), int64(4)}})
	})
	t.Run("identity column", func(t *testing.T) {
		tbl := table("sch_identity")
		exec(t, db, "CREATE TABLE "+tbl+" (id BIGINT GENERATED ALWAYS AS IDENTITY, s STRING)")
		drop(t, db, "TABLE", tbl)
		exec(t, db, "INSERT INTO "+tbl+" (s) VALUES ('a'), ('b')")
		got := query(t, db, "SELECT id, s FROM "+tbl+" ORDER BY s")
		if len(got) != 2 {
			t.Fatalf("got %v, want 2 rows", got)
		}
		a, aok := got[0][0].(int64)
		b, bok := got[1][0].(int64)
		// The server makes the values, and promises that they are distinct and rise.
		if !aok || !bok || a >= b || got[0][1] != "a" || got[1][1] != "b" {
			t.Errorf("the identity column gave %v, want two integers that rise with the rows", got)
		}
	})
	t.Run("function", func(t *testing.T) {
		f := table("sch_f")
		exec(t, db, "CREATE OR REPLACE FUNCTION "+f+"(x INT) RETURNS INT RETURN x + 1")
		drop(t, db, "FUNCTION", f)
		if got := scalar(t, db, "SELECT "+f+"(?)", int64(1)); got != int64(2) {
			t.Errorf("the function gave %v, want 2", got)
		}
	})
	alt := table("sch_alter")
	t.Run("alter table add column", func(t *testing.T) {
		exec(t, db, "CREATE TABLE "+alt+" (id INT, s STRING) USING DELTA")
		drop(outer, db, "TABLE", alt, alt+"_renamed")
		exec(t, db, "ALTER TABLE "+alt+" ADD COLUMN n INT")
		checkRows(t, query(t, db, "SELECT column_name FROM "+infoSchema(t, db)+".columns WHERE table_schema = current_schema() AND table_name = ? ORDER BY ordinal_position", alt), [][]any{{"id"}, {"s"}, {"n"}})
	})
	t.Run("alter table rename column", func(t *testing.T) {
		// The rename of a column needs the column mapping of Delta (measured).
		exec(t, db, "ALTER TABLE "+alt+" SET TBLPROPERTIES ('delta.columnMapping.mode' = 'name', 'delta.minReaderVersion' = '2', 'delta.minWriterVersion' = '5')")
		exec(t, db, "ALTER TABLE "+alt+" RENAME COLUMN n TO m")
		checkRows(t, query(t, db, "SELECT column_name FROM "+infoSchema(t, db)+".columns WHERE table_schema = current_schema() AND table_name = ? ORDER BY ordinal_position", alt), [][]any{{"id"}, {"s"}, {"m"}})
	})
	t.Run("alter table drop column", func(t *testing.T) {
		exec(t, db, "ALTER TABLE "+alt+" DROP COLUMN m")
		checkRows(t, query(t, db, "SELECT column_name FROM "+infoSchema(t, db)+".columns WHERE table_schema = current_schema() AND table_name = ? ORDER BY ordinal_position", alt), [][]any{{"id"}, {"s"}})
	})
	t.Run("alter table rename", func(t *testing.T) {
		exec(t, db, "ALTER TABLE "+alt+" RENAME TO "+alt+"_renamed")
		if got := scalar(t, db, "SELECT count(*) FROM "+alt+"_renamed"); got != int64(0) {
			t.Errorf("the renamed table holds %v rows, want 0", got)
		}
	})
}

// TestIntegrationFeatures holds each feature of features.json.
func TestIntegrationFeatures(t *testing.T) {
	db := connect(t)
	t.Run("named parameters", func(t *testing.T) {
		got := query(t, db, "SELECT :a AS a, :b AS b, :a AS a2", sql.Named("a", int64(1)), sql.Named("b", "x"))
		checkRows(t, got, [][]any{{int64(1), "x", int64(1)}})
	})
	t.Run("positional parameters", func(t *testing.T) {
		checkRows(t, query(t, db, "SELECT ? AS a, ? AS b", int64(1), "x"), [][]any{{int64(1), "x"}})
	})
	t.Run("identifier clause", func(t *testing.T) {
		tbl := table("feat_ident")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT)")
		drop(t, db, "TABLE", tbl)
		exec(t, db, "INSERT INTO "+tbl+" VALUES (7)")
		checkRows(t, query(t, db, "SELECT id FROM IDENTIFIER(:t)", sql.Named("t", tbl)), [][]any{{int64(7)}})
	})
	t.Run("binary parameter", func(t *testing.T) {
		// The server refuses a BINARY parameter (measured), and the driver refuses it
		// before it sends one.
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT :p AS p", map[string]any{
			"parameters": []any{map[string]any{"name": "p", "value": "DEADBEEF", "type": "BINARY"}},
		}))
		if status != http.StatusOK || state(obj) != "FAILED" {
			t.Errorf("the server answered HTTP %d and the state %q, want a statement that failed", status, state(obj))
		}
		if _, err := db.ExecContext(t.Context(), "SELECT ?", []byte{0xDE}); !errors.Is(err, dbimp.ErrArguments) {
			t.Errorf("the driver gave %v, want dbimp.ErrArguments", err)
		}
	})
	t.Run("complex parameter", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT :p AS p", map[string]any{
			"parameters": []any{map[string]any{"name": "p", "value": "[1]", "type": "ARRAY<INT>"}},
		}))
		if status != http.StatusOK || state(obj) != "FAILED" {
			t.Errorf("the server answered HTTP %d and the state %q, want a statement that failed", status, state(obj))
		}
		if _, err := db.ExecContext(t.Context(), "SELECT ?", []any{int64(1)}); !errors.Is(err, dbimp.ErrArguments) {
			t.Errorf("the driver gave %v, want dbimp.ErrArguments", err)
		}
	})
	t.Run("multi statement request", func(t *testing.T) {
		_, err := db.ExecContext(t.Context(), "SELECT 1 AS a; SELECT 2 AS b")
		if e := serverError(t, err); e.SQLState != "42601" {
			t.Errorf("two statements gave %+v, want a syntax error", *e)
		}
	})
	t.Run("compound statement", func(t *testing.T) {
		checkRows(t, query(t, db, "BEGIN DECLARE x INT DEFAULT 1; SET x = x + 1; SELECT x; END"), [][]any{{int64(2)}})
	})
	t.Run("script variables", func(t *testing.T) {
		tbl := table("feat_script")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT)")
		drop(t, db, "TABLE", tbl)
		// The answer of a script is the result of its last statement (measured).
		checkRows(t, query(t, db, "BEGIN DECLARE n INT DEFAULT 3; INSERT INTO "+tbl+" VALUES (n); SELECT count(*) AS c FROM "+tbl+"; END"), [][]any{{int64(1)}})
	})
	t.Run("sessions", func(t *testing.T) {
		exec(t, db, "DECLARE OR REPLACE VARIABLE "+table("var")+" INT DEFAULT 7")
		err := queryError(t.Context(), db, "SELECT "+table("var"))
		if e := serverError(t, err); e.SQLState == "" {
			t.Errorf("a variable of the first request reached the second: %+v", *e)
		}
	})
	t.Run("session configuration", func(t *testing.T) {
		exec(t, db, "SET TIME ZONE 'Asia/Tokyo'")
		if got := scalar(t, db, "SELECT current_timezone()"); got == "Asia/Tokyo" {
			t.Errorf("the time zone of the first request reached the second: %v", got)
		}
	})
	t.Run("transaction commit", func(t *testing.T) {
		exec(t, db, "BEGIN TRANSACTION")
		_, err := db.ExecContext(t.Context(), "COMMIT")
		if e := serverError(t, err); !strings.Contains(e.Message, "NO_ACTIVE_TRANSACTION") {
			t.Errorf("COMMIT gave %+v, want NO_ACTIVE_TRANSACTION", *e)
		}
	})
	t.Run("transaction rollback", func(t *testing.T) {
		tbl := table("feat_tx")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT)")
		drop(t, db, "TABLE", tbl)
		exec(t, db, "BEGIN TRANSACTION")
		exec(t, db, "INSERT INTO "+tbl+" VALUES (1)")
		exec(t, db, "ROLLBACK")
		// The insert took effect when its request ended, so the rollback did nothing.
		checkRows(t, query(t, db, "SELECT count(*) FROM "+tbl), [][]any{{int64(1)}})
	})
	t.Run("async execution", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT 1 AS a", map[string]any{"wait_timeout": "0s"}))
		id, _ := obj["statement_id"].(string)
		if status != http.StatusOK || state(obj) != "PENDING" || id == "" {
			t.Fatalf("the server answered HTTP %d, the state %q and the id %q, want PENDING", status, state(obj), id)
		}
		if final := pollState(t, id, "SUCCEEDED"); final != "SUCCEEDED" {
			t.Errorf("the statement ended in the state %s, want SUCCEEDED", final)
		}
	})
	t.Run("statement cancel", func(t *testing.T) {
		_, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, heavy, map[string]any{"wait_timeout": "0s"}))
		id, _ := obj["statement_id"].(string)
		if id == "" {
			t.Fatalf("the server gave no id: %v", obj)
		}
		if status, _ := rest(t, http.MethodPost, "/api/2.0/sql/statements/"+id+"/cancel", nil); status != http.StatusOK {
			t.Fatalf("the cancel answered HTTP %d, want 200", status)
		}
		if final := pollState(t, id, "CANCELED"); final != "CANCELED" {
			t.Errorf("the statement ended in the state %s, want CANCELED", final)
		}
	})
	t.Run("row limit", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT id FROM range(3000)", map[string]any{"row_limit": 10}))
		if m, _ := obj["manifest"].(map[string]any); status != http.StatusOK || m["truncated"] != true || m["total_row_count"] != float64(10) {
			t.Errorf("a row limit of 10 gave HTTP %d and the manifest %v, want truncated and 10 rows", status, m)
		}
		// The driver sends no limit, and it refuses a result that the server cut.
		err := queryError(t.Context(), db, "SELECT id FROM range(3000)", databricks.WithParameter("row_limit", 10))
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithParameter(row_limit) gave %v, want dbimp.ErrNotSupported", err)
		}
	})
	t.Run("byte limit", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT id, repeat('x', 300) AS s FROM range(3000)", map[string]any{"byte_limit": 1000}))
		if m, _ := obj["manifest"].(map[string]any); status != http.StatusOK || m["truncated"] != true {
			t.Errorf("a byte limit of 1000 gave HTTP %d and the manifest %v, want truncated", status, m)
		}
	})
	t.Run("external links", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT 1 AS a, 'x' AS b", map[string]any{"disposition": "EXTERNAL_LINKS", "format": "JSON_ARRAY"}))
		res, _ := obj["result"].(map[string]any)
		links, _ := res["external_links"].([]any)
		if status != http.StatusOK || len(links) != 1 {
			t.Fatalf("the server answered HTTP %d and the result %v, want one external link", status, res)
		}
		link, _ := links[0].(map[string]any)
		if u, _ := link["external_link"].(string); !strings.HasPrefix(u, "https://") {
			t.Errorf("the link is %q, want an HTTPS URL", u)
		}
	})
	t.Run("arrow stream", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT 1 AS a", map[string]any{"disposition": "EXTERNAL_LINKS", "format": "ARROW_STREAM"}))
		if m, _ := obj["manifest"].(map[string]any); status != http.StatusOK || m["format"] != "ARROW_STREAM" {
			t.Errorf("the server answered HTTP %d and the manifest %v, want the format ARROW_STREAM", status, m)
		}
	})
	t.Run("csv format", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT 1 AS a", map[string]any{"disposition": "EXTERNAL_LINKS", "format": "CSV"}))
		if m, _ := obj["manifest"].(map[string]any); status != http.StatusOK || m["format"] != "CSV" {
			t.Errorf("the server answered HTTP %d and the manifest %v, want the format CSV", status, m)
		}
	})
	t.Run("inline arrow", func(t *testing.T) {
		status, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT 1 AS a", map[string]any{"disposition": "INLINE", "format": "ARROW_STREAM"}))
		if status != http.StatusBadRequest || obj["error_code"] != "INVALID_PARAMETER_VALUE" {
			t.Errorf("INLINE with ARROW_STREAM gave HTTP %d and %v, want HTTP 400 and INVALID_PARAMETER_VALUE", status, obj)
		}
	})
	t.Run("result chunk fetch", func(t *testing.T) {
		_, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT id FROM range(3000)", nil))
		id, _ := obj["statement_id"].(string)
		status, chunk := rest(t, http.MethodGet, "/api/2.0/sql/statements/"+id+"/result/chunks/0", nil)
		if rowsIn, _ := chunk["data_array"].([]any); status != http.StatusOK || len(rowsIn) != 3000 {
			t.Errorf("the chunk 0 answered HTTP %d with %d rows, want 3000", status, len(rowsIn))
		}
		// A result of one chunk has no chunk 1 (measured). A result of more than one
		// chunk is not read by the driver (D193 item 2).
		if status, _ := rest(t, http.MethodGet, "/api/2.0/sql/statements/"+id+"/result/chunks/1", nil); status != http.StatusBadRequest {
			t.Errorf("the chunk 1 answered HTTP %d, want 400", status)
		}
	})
	t.Run("information schema", func(t *testing.T) {
		got := query(t, db, "SELECT schema_name FROM "+infoSchema(t, db)+".schemata WHERE schema_name = 'information_schema'")
		checkRows(t, got, [][]any{{"information_schema"}})
	})
	t.Run("show statements", func(t *testing.T) {
		cats := query(t, db, "SHOW CATALOGS")
		if len(cats) == 0 {
			t.Error("SHOW CATALOGS gave no catalog")
		}
		if len(query(t, db, "SHOW SCHEMAS")) == 0 {
			t.Error("SHOW SCHEMAS gave no schema")
		}
		_ = query(t, db, "SHOW TABLES")
	})
	t.Run("describe table", func(t *testing.T) {
		tbl := table("feat_describe")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT, s STRING)")
		drop(t, db, "TABLE", tbl)
		checkRows(t, query(t, db, "DESCRIBE TABLE "+tbl), [][]any{{"id", "int", nil}, {"s", "string", nil}})
	})
	t.Run("explain", func(t *testing.T) {
		got := query(t, db, "EXPLAIN SELECT id FROM range(3) WHERE id > 1")
		if len(got) == 0 {
			t.Error("EXPLAIN gave no row")
		}
	})
	t.Run("gzip response", func(t *testing.T) {
		status, hdr, _ := restRaw(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT 1 AS a", nil), "Accept-Encoding", "gzip")
		if status != http.StatusOK || !strings.Contains(hdr.Get("Content-Encoding"), "gzip") {
			t.Errorf("a request for gzip gave HTTP %d and the encoding %q, want gzip", status, hdr.Get("Content-Encoding"))
		}
		// The driver asks for gzip through its transport, and decodes it.
		checkRows(t, query(t, db, "SELECT 1 AS a"), [][]any{{int64(1)}})
	})
	t.Run("version function", func(t *testing.T) {
		got, ok := scalar(t, db, "SELECT version()").(string)
		if !ok || !strings.Contains(got, ".") {
			t.Errorf("the version is %q", got)
		}
	})
	t.Run("default catalog and schema", func(t *testing.T) {
		cfg := integrationConfig(t)
		got := query(t, db, "SELECT current_catalog(), current_schema()")
		if cfg.Catalog != "" && got[0][0] != cfg.Catalog || cfg.Schema != "" && got[0][1] != cfg.Schema {
			t.Errorf("the namespace is %v, want %s.%s", got, cfg.Catalog, cfg.Schema)
		}
	})
	t.Run("affected row counts", func(t *testing.T) {
		tbl := table("feat_counts")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT)")
		drop(t, db, "TABLE", tbl)
		if n := affected(t, exec(t, db, "INSERT INTO "+tbl+" VALUES (1), (2), (3)")); n != 3 {
			t.Errorf("the insert changed %d rows, want 3", n)
		}
		if n := affected(t, exec(t, db, "DELETE FROM "+tbl+" WHERE id > 1")); n != 2 {
			t.Errorf("the delete changed %d rows, want 2", n)
		}
	})
	t.Run("comments", func(t *testing.T) {
		for _, q := range []string{"-- first\nSELECT 1 AS a", "SELECT 1 AS a -- last", "/* first */ SELECT /* middle */ 1 AS a /* last */"} {
			checkRows(t, query(t, db, q), [][]any{{int64(1)}})
		}
	})
	t.Run("trailing semicolon", func(t *testing.T) {
		checkRows(t, query(t, db, "SELECT 1 AS a;"), [][]any{{int64(1)}})
	})
	t.Run("warehouse auto resume", func(t *testing.T) {
		cfg := integrationConfig(t)
		status, obj := rest(t, http.MethodGet, "/api/2.0/sql/warehouses/"+cfg.Warehouse, nil)
		if status != http.StatusOK || obj["auto_resume"] != true {
			t.Errorf("the warehouse answered HTTP %d and auto_resume %v, want true", status, obj["auto_resume"])
		}
	})
	t.Run("statement result retention", func(t *testing.T) {
		// The recordings read a result 21 minutes after its statement (D193 notes). The test
		// reads it at once, which is all that a run can afford.
		_, obj := rest(t, http.MethodPost, "/api/2.0/sql/statements", stmtBody(t, "SELECT 1 AS a", nil))
		id, _ := obj["statement_id"].(string)
		status, again := rest(t, http.MethodGet, "/api/2.0/sql/statements/"+id, nil)
		if status != http.StatusOK || state(again) != "SUCCEEDED" {
			t.Errorf("a read of the statement gave HTTP %d and the state %q, want SUCCEEDED", status, state(again))
		}
	})
	t.Run("query history", func(t *testing.T) {
		status, obj := rest(t, http.MethodGet, "/api/2.0/sql/history/queries?max_results=2", nil)
		if _, ok := obj["res"]; status != http.StatusOK || !ok {
			t.Errorf("the history answered HTTP %d and %v, want a list in res", status, obj)
		}
	})
	t.Run("warehouse api", func(t *testing.T) {
		status, obj := rest(t, http.MethodGet, "/api/2.0/sql/warehouses", nil)
		if _, ok := obj["warehouses"]; status != http.StatusOK || !ok {
			t.Errorf("the list of warehouses answered HTTP %d and %v, want a list", status, obj)
		}
	})
}

// pollState reads the statement until its state is final, or want, or a limit of
// two minutes passes, and returns the state.
func pollState(t testing.TB, id, want string) string {
	t.Helper()
	var last string
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	for delay := 200 * time.Millisecond; ctx.Err() == nil; delay = min(delay*2, 3*time.Second) {
		_, obj := rest(t, http.MethodGet, "/api/2.0/sql/statements/"+id, nil)
		last = state(obj)
		switch last {
		case "SUCCEEDED", "FAILED", "CANCELED", "CLOSED", want:
			return last
		}
		select {
		case <-ctx.Done():
		case <-time.After(delay):
		}
	}
	return last
}
