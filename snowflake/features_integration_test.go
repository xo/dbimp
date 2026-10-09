package snowflake_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/snowflake"
)

// codeNoPrivilege is the code of a statement that the role may not run
// (measured).
const codeNoPrivilege = "003001"

// refused fails the test unless err is an error of the server with the code and
// the text. It is for an operation that features.json marks no.
func refused(t testing.TB, err error, code, text string) {
	t.Helper()
	if err == nil {
		t.Errorf("the server did what the survey says that it cannot do: want the refusal %s %q", code, text)
		return
	}
	e := serverError(t, err)
	if e.Code != code || !strings.Contains(e.Message, text) {
		t.Errorf("the refusal is %+v, want the code %s and the text %q", *e, code, text)
	}
}

// create runs a statement that makes an object, and skips the test when the role
// has no privilege to make it (question 3 of docs/SNOWFLAKE.md).
func create(t testing.TB, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query); err != nil {
		if e, ok := errors.AsType[*snowflake.Error](err); ok && e.Code == codeNoPrivilege {
			t.Skipf("the role cannot run %q, so it is not measured: %v", query, err)
		}
		t.Fatalf("%s: %v", query, err)
	}
}

// affected returns the count that the statement changed.
func affected(t testing.TB, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	n, err := exec(t, db, query, args...).RowsAffected()
	if err != nil {
		t.Fatalf("%s: RowsAffected: %v", query, err)
	}
	return n
}

// TestIntegrationCRUD inserts, selects, updates and deletes rows in three tables, one
// of which refers to another, and compares each value that a select returns with
// the value that the test wrote (step 14a). The subtests run in order, because
// each one uses the rows that the one before it left.
func TestIntegrationCRUD(t *testing.T) {
	db := connect(t)
	parent, child, extra := table("crud_parent"), table("crud_child"), table("crud_extra")
	drop(t, db, child, parent, extra)
	exec(t, db, "CREATE TABLE "+parent+" (id NUMBER(18,0) PRIMARY KEY, name VARCHAR, score FLOAT)")
	exec(t, db, "CREATE TABLE "+child+" (id NUMBER(18,0) PRIMARY KEY, parent_id NUMBER(18,0) REFERENCES "+parent+"(id), qty NUMBER(10,2))")
	exec(t, db, "CREATE TABLE "+extra+" (id NUMBER(18,0), note VARCHAR)")

	t.Run("insert", func(t *testing.T) {
		if n := affected(t, db, "INSERT INTO "+parent+" (id, name, score) VALUES (?, ?, ?)", int64(1), "ada", 1.5); n != 1 {
			t.Errorf("the first insert changed %d rows, want 1", n)
		}
		if n := affected(t, db, "INSERT INTO "+parent+" VALUES (2, 'bob', 2.5), (3, 'cy', NULL)"); n != 2 {
			t.Errorf("the insert of two rows changed %d rows, want 2", n)
		}
		if n := affected(t, db, "INSERT INTO "+child+" (id, parent_id, qty) VALUES (?, ?, ?)", int64(10), int64(1), dec(t, "12.50")); n != 1 {
			t.Errorf("the insert of a child changed %d rows, want 1", n)
		}
		exec(t, db, "INSERT INTO "+child+" VALUES (11, 1, 3.25), (12, 2, 0.01)")
		if _, err := exec(t, db, "INSERT INTO "+parent+" (id) VALUES (4)").LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
		}
	})
	t.Run("select", func(t *testing.T) {
		got := query(t, db, "SELECT id, name, score FROM "+parent+" ORDER BY id")
		want := [][]any{{int64(1), "ada", 1.5}, {int64(2), "bob", 2.5}, {int64(3), "cy", nil}, {int64(4), nil, nil}}
		if len(got) != len(want) {
			t.Fatalf("got %d rows, want %d: %v", len(got), len(want), got)
		}
		for i := range want {
			for j := range want[i] {
				if !same(got[i][j], want[i][j]) {
					t.Errorf("row %d column %d is %#v, want %#v", i, j, got[i][j], want[i][j])
				}
			}
		}
		join := query(t, db, "SELECT p.name, c.qty FROM "+child+" c JOIN "+parent+" p ON p.id = c.parent_id ORDER BY c.id")
		wantJoin := [][]any{{"ada", dec(t, "12.50")}, {"ada", dec(t, "3.25")}, {"bob", dec(t, "0.01")}}
		for i := range wantJoin {
			if len(join) != len(wantJoin) || !same(join[i][0], wantJoin[i][0]) || !same(join[i][1], wantJoin[i][1]) {
				t.Fatalf("the join is %v, want %v", join, wantJoin)
			}
		}
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+child); got != int64(3) {
			t.Errorf("the count is %#v, want int64(3)", got)
		}
		var name sql.Null[string]
		var score sql.Null[float64]
		if err := db.QueryRowContext(t.Context(), "SELECT name, score FROM "+parent+" WHERE id = ?", int64(4)).Scan(&name, &score); err != nil {
			t.Fatal(err)
		}
		if name.Valid || score.Valid {
			t.Errorf("a NULL scanned as %v and %v", name, score)
		}
		if err := db.QueryRowContext(t.Context(), "SELECT name FROM "+parent+" WHERE id = ?", int64(99)).Scan(&name); !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("a select with no row gave %v, want sql.ErrNoRows", err)
		}
	})
	t.Run("update", func(t *testing.T) {
		if n := affected(t, db, "UPDATE "+parent+" SET name = ?, score = score + ? WHERE id <= ?", "renamed", 1.0, int64(2)); n != 2 {
			t.Errorf("the update changed %d rows, want 2", n)
		}
		got := query(t, db, "SELECT name, score FROM "+parent+" WHERE id <= 2 ORDER BY id")
		if len(got) != 2 || got[0][0] != "renamed" || got[0][1] != 2.5 || got[1][1] != 3.5 {
			t.Errorf("after the update the rows are %v", got)
		}
		if n := affected(t, db, "UPDATE "+parent+" SET name = 'x' WHERE id = 999"); n != 0 {
			t.Errorf("an update of no row changed %d rows, want 0", n)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if n := affected(t, db, "DELETE FROM "+child+" WHERE id = ?", int64(12)); n != 1 {
			t.Errorf("the delete changed %d rows, want 1", n)
		}
		if got := query(t, db, "SELECT id FROM "+child+" WHERE id = 12"); len(got) != 0 {
			t.Errorf("the deleted row is still there: %v", got)
		}
		if n := affected(t, db, "DELETE FROM "+parent+" WHERE id = 4"); n != 1 {
			t.Errorf("the delete of a parent changed %d rows, want 1", n)
		}
		// A truncate has a count only when it deleted a row: of an empty table
		// the answer holds none (measured, 2026-10-10).
		exec(t, db, "INSERT INTO "+extra+" VALUES (1, 'a'), (2, 'b')")
		if n := affected(t, db, "TRUNCATE TABLE "+extra); n != 2 {
			t.Errorf("a truncate of two rows changed %d rows, want 2", n)
		}
	})
	t.Run("merge", func(t *testing.T) {
		merge := "MERGE INTO " + parent + " t USING (SELECT ?::NUMBER AS id, ?::VARCHAR AS name) s ON t.id = s.id " +
			"WHEN MATCHED THEN UPDATE SET name = s.name WHEN NOT MATCHED THEN INSERT (id, name) VALUES (s.id, s.name)"
		if n := affected(t, db, merge, int64(3), "merged"); n != 1 {
			t.Errorf("the merge of a row that exists changed %d rows, want 1", n)
		}
		if n := affected(t, db, merge, int64(30), "new"); n != 1 {
			t.Errorf("the merge of a new row changed %d rows, want 1", n)
		}
		got := query(t, db, "SELECT id, name FROM "+parent+" WHERE id IN (3, 30) ORDER BY id")
		if len(got) != 2 || got[0][1] != "merged" || got[1][0] != int64(30) || got[1][1] != "new" {
			t.Errorf("after the merge the rows are %v", got)
		}
	})
	t.Run("insert overwrite", func(t *testing.T) {
		exec(t, db, "INSERT INTO "+extra+" VALUES (1, 'a'), (2, 'b')")
		if n := affected(t, db, "INSERT OVERWRITE INTO "+extra+" VALUES (3, 'c')"); n != 1 {
			t.Errorf("the overwrite changed %d rows, want 1", n)
		}
		got := query(t, db, "SELECT id, note FROM "+extra)
		if len(got) != 1 || got[0][0] != int64(3) || got[0][1] != "c" {
			t.Errorf("after the overwrite the rows are %v, want only 3 and c", got)
		}
	})
	t.Run("multi table insert", func(t *testing.T) {
		m1, m2 := table("crud_m1"), table("crud_m2")
		drop(t, db, m1, m2)
		exec(t, db, "CREATE TABLE "+m1+" (id NUMBER(18,0))")
		exec(t, db, "CREATE TABLE "+m2+" (id NUMBER(18,0))")
		exec(t, db, "INSERT ALL INTO "+m1+" VALUES (id) INTO "+m2+" VALUES (id) SELECT ?::NUMBER AS id", int64(7))
		for _, name := range []string{m1, m2} {
			if got := scalar(t, db, "SELECT id FROM "+name); got != int64(7) {
				t.Errorf("%s holds %#v, want int64(7)", name, got)
			}
		}
	})
	t.Run("copy into", func(t *testing.T) {
		// There is no file to load, so the statement runs and loads no row. PUT of a
		// file is refused by the SQL API, so the test cannot stage one.
		exec(t, db, "COPY INTO "+extra+" FROM @~/"+strings.ToLower(prefix)+"_none/")
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+extra); got != int64(1) {
			t.Errorf("after a copy of no file the table holds %#v rows, want 1", got)
		}
		_, err := db.ExecContext(t.Context(), "PUT file:///tmp/none.csv @~/"+strings.ToLower(prefix)+"/")
		if e := serverError(t, err); e.Code != "391911" {
			t.Errorf("PUT gave %+v, want the code 391911", *e)
		}
	})
}

// TestIntegrationSchema runs each operation on the schema that features.json names.
// The keys are declared and not enforced, an index is refused, and an object that
// the role has no privilege to make is skipped with that reason.
func TestIntegrationSchema(t *testing.T) {
	db := connect(t)
	pk, fk := table("schema_pk"), table("schema_fk")
	drop(t, db, fk, pk)
	t.Run("table", func(t *testing.T) {
		exec(t, db, "CREATE TABLE "+pk+" (id NUMBER(18,0) PRIMARY KEY, u VARCHAR UNIQUE, d NUMBER(18,0) DEFAULT 5)")
		got := query(t, db, "SELECT COUNT(*) FROM "+pk)
		if len(got) != 1 || got[0][0] != int64(0) {
			t.Errorf("a new table holds %v", got)
		}
		res, err := db.ExecContext(t.Context(), "DROP TABLE IF EXISTS "+table("schema_nosuch"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("RowsAffected of DDL gave %v, want dbimp.ErrNotSupported", err)
		}
	})
	t.Run("primary key", func(t *testing.T) {
		// A primary key is declared and not enforced: two equal rows go in.
		exec(t, db, "INSERT INTO "+pk+" (id, u) VALUES (1, 'a'), (1, 'a')")
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+pk+" WHERE id = 1"); got != int64(2) {
			t.Errorf("a key that the server does not enforce kept %#v rows, want 2", got)
		}
	})
	t.Run("unique constraint", func(t *testing.T) {
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+pk+" WHERE u = 'a'"); got != int64(2) {
			t.Errorf("a unique column that the server does not enforce holds %#v rows, want 2", got)
		}
	})
	t.Run("foreign key", func(t *testing.T) {
		exec(t, db, "CREATE TABLE "+fk+" (id NUMBER(18,0), pk NUMBER(18,0) REFERENCES "+pk+"(id))")
		// The parent 999 does not exist, and the insert goes through.
		exec(t, db, "INSERT INTO "+fk+" VALUES (1, 999)")
		if got := scalar(t, db, "SELECT pk FROM "+fk); got != int64(999) {
			t.Errorf("the child holds %#v, want int64(999)", got)
		}
	})
	t.Run("default value", func(t *testing.T) {
		if got := scalar(t, db, "SELECT d FROM "+pk+" LIMIT 1"); got != int64(5) {
			t.Errorf("the default is %#v, want int64(5)", got)
		}
	})
	t.Run("clustering key", func(t *testing.T) {
		cl := table("schema_cl")
		drop(t, db, cl)
		exec(t, db, "CREATE TABLE "+cl+" (id NUMBER(18,0)) CLUSTER BY (id)")
		exec(t, db, "INSERT INTO "+cl+" VALUES (2), (1)")
		got := query(t, db, "SELECT id FROM "+cl+" ORDER BY id")
		if len(got) != 2 || got[0][0] != int64(1) || got[1][0] != int64(2) {
			t.Errorf("the clustered table holds %v", got)
		}
	})
	t.Run("index", func(t *testing.T) {
		_, err := db.ExecContext(t.Context(), "CREATE INDEX "+table("schema_ix")+" ON "+pk+" (id)")
		refused(t, err, "000002", "SECONDARY INDEX")
	})
	t.Run("materialized view", func(t *testing.T) {
		mv := table("schema_mv")
		_, err := db.ExecContext(t.Context(), "CREATE OR REPLACE MATERIALIZED VIEW "+mv+" AS SELECT id FROM "+pk)
		if err == nil {
			_, _ = db.ExecContext(t.Context(), "DROP MATERIALIZED VIEW IF EXISTS "+mv)
		}
		refused(t, err, "000002", "MATERIALIZED VIEWS")
	})
	t.Run("view", func(t *testing.T) {
		view := table("schema_view")
		create(t, db, "CREATE OR REPLACE VIEW "+view+" AS SELECT id FROM "+pk)
		t.Cleanup(func() { _, _ = db.ExecContext(context.WithoutCancel(t.Context()), "DROP VIEW IF EXISTS "+view) })
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+view); got != int64(2) {
			t.Errorf("the view holds %#v rows, want 2", got)
		}
	})
	t.Run("dynamic table", func(t *testing.T) {
		dt := table("schema_dt")
		create(t, db, "CREATE OR REPLACE DYNAMIC TABLE "+dt+" TARGET_LAG = '1 hour' WAREHOUSE = "+integrationConfig(t).Warehouse+" AS SELECT id FROM "+pk)
		t.Cleanup(func() { _, _ = db.ExecContext(context.WithoutCancel(t.Context()), "DROP DYNAMIC TABLE IF EXISTS "+dt) })
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+dt); got != int64(2) {
			t.Errorf("the dynamic table holds %#v rows, want 2", got)
		}
	})
	t.Run("sequence", func(t *testing.T) {
		seq := table("schema_seq")
		create(t, db, "CREATE OR REPLACE SEQUENCE "+seq)
		t.Cleanup(func() { _, _ = db.ExecContext(context.WithoutCancel(t.Context()), "DROP SEQUENCE IF EXISTS "+seq) })
		got := query(t, db, "SELECT "+seq+".NEXTVAL, "+seq+".NEXTVAL")
		// NEXTVAL is a NUMBER of 38 digits, so each value is a decimal.
		if len(got) != 1 || !same(got[0][0], dec(t, "1")) || !same(got[0][1], dec(t, "2")) {
			t.Errorf("the sequence gave %v, want 1 and 2", got)
		}
	})
	t.Run("stream", func(t *testing.T) {
		st := table("schema_st")
		create(t, db, "CREATE OR REPLACE STREAM "+st+" ON TABLE "+pk)
		t.Cleanup(func() { _, _ = db.ExecContext(context.WithoutCancel(t.Context()), "DROP STREAM IF EXISTS "+st) })
		exec(t, db, "INSERT INTO "+pk+" (id, u) VALUES (7, 'z')")
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+st); got != int64(1) {
			t.Errorf("the stream holds %#v changes, want 1", got)
		}
	})
}

// TestIntegrationFeatures uses each feature that features.json marks yes, and
// compares what it returns.
func TestIntegrationFeatures(t *testing.T) {
	db := connect(t)
	t.Run("time travel", func(t *testing.T) {
		tt := table("feat_tt")
		drop(t, db, tt)
		exec(t, db, "CREATE TABLE "+tt+" (id NUMBER(18,0))")
		exec(t, db, "INSERT INTO "+tt+" VALUES (1)")
		at, ok := scalar(t, db, "SELECT CURRENT_TIMESTAMP()::TIMESTAMP_LTZ").(time.Time)
		if !ok {
			t.Fatal("the time of the server is not a time.Time")
		}
		exec(t, db, "INSERT INTO "+tt+" VALUES (2)")
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+tt+" AT(TIMESTAMP => ?::TIMESTAMP_LTZ)", at); got != int64(1) {
			t.Errorf("the table at %v held %#v rows, want 1", at, got)
		}
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+tt); got != int64(2) {
			t.Errorf("the table holds %#v rows now, want 2", got)
		}
	})
	t.Run("clone", func(t *testing.T) {
		src, dst := table("feat_src"), table("feat_clone")
		drop(t, db, dst, src)
		exec(t, db, "CREATE TABLE "+src+" (id NUMBER(18,0))")
		exec(t, db, "INSERT INTO "+src+" VALUES (1), (2), (3)")
		exec(t, db, "CREATE TABLE "+dst+" CLONE "+src)
		exec(t, db, "INSERT INTO "+src+" VALUES (4)")
		if got := scalar(t, db, "SELECT COUNT(*) FROM "+dst); got != int64(3) {
			t.Errorf("the clone holds %#v rows, want 3: it does not follow its source", got)
		}
	})
	t.Run("flatten", func(t *testing.T) {
		got := query(t, db, "SELECT value FROM TABLE(FLATTEN(INPUT => PARSE_JSON('[1,\"x\",null]'))) ORDER BY index")
		if len(got) != 3 || got[0][0] != int64(1) || got[1][0] != "x" || got[2][0] != nil {
			t.Errorf("flatten gave %v, want 1, x and NULL", got)
		}
	})
	t.Run("qualify", func(t *testing.T) {
		q := table("feat_q")
		drop(t, db, q)
		exec(t, db, "CREATE TABLE "+q+" (id NUMBER(18,0), grp VARCHAR)")
		exec(t, db, "INSERT INTO "+q+" VALUES (1, 'a'), (2, 'a'), (3, 'b')")
		got := query(t, db, "SELECT id FROM "+q+" QUALIFY ROW_NUMBER() OVER (PARTITION BY grp ORDER BY id) = 1 ORDER BY id")
		if len(got) != 2 || got[0][0] != int64(1) || got[1][0] != int64(3) {
			t.Errorf("qualify gave %v, want 1 and 3", got)
		}
	})
	t.Run("variant path", func(t *testing.T) {
		v := table("feat_v")
		drop(t, db, v)
		exec(t, db, "CREATE TABLE "+v+" (v VARIANT)")
		exec(t, db, "INSERT INTO "+v+" SELECT PARSE_JSON(?)", `{"a":{"b":[10,20]}}`)
		got := query(t, db, "SELECT v:a.b[1], v:a.b[1]::NUMBER(18,0), v:missing FROM "+v)
		if len(got) != 1 || got[0][0] != int64(20) || got[0][1] != int64(20) || got[0][2] != nil {
			t.Errorf("the paths gave %v, want 20, 20 and NULL", got)
		}
	})
	t.Run("multi statement count", func(t *testing.T) {
		// The server needs MULTI_STATEMENT_COUNT for two statements, and answers with
		// the handles of the children, which the driver does not read (D183), so the
		// driver refuses the parameter.
		_, err := db.ExecContext(t.Context(), "SELECT 1; SELECT 2", snowflake.WithParameter("MULTI_STATEMENT_COUNT", 2))
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("MULTI_STATEMENT_COUNT gave %v, want dbimp.ErrNotSupported", err)
		}
		_, err = db.ExecContext(t.Context(), "SELECT 1; SELECT 2")
		if e := serverError(t, err); e.Code != "000008" {
			t.Errorf("two statements with no count gave %+v, want the code 000008", *e)
		}
	})
	t.Run("async execution", func(t *testing.T) {
		start := time.Now()
		if got := scalar(t, db, "SELECT SYSTEM$WAIT(2)"); got != "waited 2 seconds" {
			t.Errorf("the statement gave %#v", got)
		}
		if took := time.Since(start); took < 2*time.Second {
			t.Errorf("the statement took %v, want at least 2 seconds: the driver did not wait for it", took)
		}
	})
	t.Run("result partitions", func(t *testing.T) {
		for _, rows := range []int{20000, 120000} {
			got := query(t, db, fmt.Sprintf("SELECT seq4() AS n, uuid_string() AS u FROM TABLE(GENERATOR(ROWCOUNT => %d))", rows))
			if len(got) != rows {
				t.Fatalf("a result of %d rows has %d", rows, len(got))
			}
			seen := make(map[int64]bool, rows)
			for _, row := range got {
				n, ok := row[0].(int64)
				if !ok || n < 0 || n >= int64(rows) || seen[n] {
					t.Fatalf("the row %v is out of range or repeated", row)
				}
				seen[n] = true
				if u, ok := row[1].(string); !ok || len(u) != 36 {
					t.Fatalf("the row %v has no UUID", row)
				}
			}
		}
	})
}
