package maxcompute_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/maxcompute"
)

// script makes the statement run in script mode, which the caller sets (D220 item
// 3).
var script = maxcompute.WithParameter("odps.sql.submit.mode", "script")

// transactional is the table that UPDATE, DELETE and MERGE need: a table with a
// primary key and the property transactional (recorded: "update on a plain
// table").
const transactional = "(id BIGINT NOT NULL, v STRING, PRIMARY KEY (id)) TBLPROPERTIES ('transactional'='true')"

// ints is a query of 15000 rows, which is more than the server returns.
const ints = "SELECT pos FROM (SELECT posexplode(split(repeat('a,', 15000), ',')) AS (pos, v)) t"

// feature is one entry of the survey that is not a type, and the statements that
// show the verdict of the server for it (step 14a). A case with fail set sends
// the statement of the entry, and expects the refusal of the server, which is the
// verdict no.
type feature struct {
	name string
	// tables are the tables that the case makes with the text after the name, and
	// drops at its end.
	tables map[string]string
	// setup are statements that run first, and run is the statement of the entry.
	setup []string
	run   string
	args  []any
	// fail is true when the server refuses run.
	fail bool
	// check runs after run, and tests the effect.
	check func(t *testing.T, db *sql.DB)
}

// rowsOf runs a query and returns how many rows it gave.
func rowsOf(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// queryErr runs a query, reads its rows, and returns the error of the query or of
// the rows.
func queryErr(t *testing.T, db *sql.DB, query string, args ...any) error {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return err //nolint:wrapcheck // The test reads the error of the driver as it is.
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err() //nolint:wrapcheck // The test reads the error of the driver as it is.
}

// want is a check that the query gives n rows.
func want(query string, n int) func(*testing.T, *sql.DB) {
	return func(t *testing.T, db *sql.DB) {
		t.Helper()
		if got := rowsOf(t, db, query); got != n {
			t.Errorf("%s gave %d rows, want %d", query, got, n)
		}
	}
}

// features are the cases of every entry of testdata/maxcompute/features.json that
// is not a type. The types are in TestIntegrationRoundTrip.
func features() []feature {
	const t = "dbimp_it_f_"
	return []feature{
		{name: "select", run: "SELECT 1 AS a"},
		{name: "insert", tables: map[string]string{t + "ins": "(id BIGINT, v STRING)"}, run: "INSERT INTO " + t + "ins VALUES (1, 'a')", check: want("SELECT * FROM "+t+"ins", 1)},
		{name: "insert overwrite", tables: map[string]string{t + "ovw": "(id BIGINT, v STRING)"}, setup: []string{"INSERT INTO " + t + "ovw VALUES (1, 'a'), (2, 'b')"},
			run: "INSERT OVERWRITE TABLE " + t + "ovw SELECT 9, 'o'", check: want("SELECT * FROM "+t+"ovw", 1)},
		{name: "multi insert", tables: map[string]string{t + "mi0": "(id BIGINT, v STRING)", t + "mi1": "(id BIGINT, v STRING)", t + "mi2": "(id BIGINT, v STRING)"},
			setup: []string{"INSERT INTO " + t + "mi0 VALUES (1, 'a')"},
			run:   "FROM " + t + "mi0 INSERT INTO TABLE " + t + "mi1 SELECT id, v INSERT INTO TABLE " + t + "mi2 SELECT id + 1, v", check: want("SELECT * FROM "+t+"mi2", 1)},
		{name: "update", tables: map[string]string{t + "upd": transactional}, setup: []string{"INSERT INTO " + t + "upd VALUES (1, 'a'), (2, 'b')"},
			run: "UPDATE " + t + "upd SET v = 'z' WHERE id = 1", check: want("SELECT * FROM "+t+"upd WHERE v = 'z'", 1)},
		{name: "delete", tables: map[string]string{t + "del": transactional}, setup: []string{"INSERT INTO " + t + "del VALUES (1, 'a'), (2, 'b')"},
			run: "DELETE FROM " + t + "del WHERE id = 2", check: want("SELECT * FROM "+t+"del", 1)},
		{name: "merge", tables: map[string]string{t + "mrg": transactional}, setup: []string{"INSERT INTO " + t + "mrg VALUES (1, 'a')"},
			run:   "MERGE INTO " + t + "mrg t USING (SELECT 1 AS id, 'm' AS nm UNION ALL SELECT 3, 'n') s ON t.id = s.id WHEN MATCHED THEN UPDATE SET v = s.nm WHEN NOT MATCHED THEN INSERT VALUES (s.id, s.nm)",
			check: want("SELECT * FROM "+t+"mrg", 2)},
		{name: "truncate table", tables: map[string]string{t + "trn": "(id BIGINT)"}, setup: []string{"INSERT INTO " + t + "trn VALUES (1)"},
			run: "TRUNCATE TABLE " + t + "trn", check: want("SELECT * FROM "+t+"trn", 0)},
		{name: "create table", run: "CREATE TABLE " + t + "crt (id BIGINT, v STRING)", tables: map[string]string{}, check: func(t *testing.T, db *sql.DB) {
			t.Helper()
			mustExec(t, db, "DROP TABLE IF EXISTS "+"dbimp_it_f_crt")
		}},
		{name: "alter table add columns", tables: map[string]string{t + "alt": "(id BIGINT)"}, run: "ALTER TABLE " + t + "alt ADD COLUMNS (extra STRING)", check: want("SELECT id, extra FROM "+t+"alt", 0)},
		{name: "alter table rename", tables: map[string]string{t + "rn0": "(id BIGINT)"}, setup: []string{"DROP TABLE IF EXISTS " + t + "rn1"},
			run: "ALTER TABLE " + t + "rn0 RENAME TO " + t + "rn1", check: func(tt *testing.T, db *sql.DB) {
				tt.Helper()
				want("SELECT * FROM "+t+"rn1", 0)(tt, db)
				mustExec(tt, db, "DROP TABLE IF EXISTS "+t+"rn1")
			}},
		{name: "create table as select", setup: []string{"DROP TABLE IF EXISTS " + t + "cts"}, run: "CREATE TABLE " + t + "cts AS SELECT 1 AS a", check: func(tt *testing.T, db *sql.DB) {
			tt.Helper()
			want("SELECT * FROM "+t+"cts", 1)(tt, db)
			mustExec(tt, db, "DROP TABLE IF EXISTS "+t+"cts")
		}},
		{name: "create table like", tables: map[string]string{t + "lk0": "(id BIGINT, v STRING)"}, setup: []string{"DROP TABLE IF EXISTS " + t + "lk1"}, run: "CREATE TABLE " + t + "lk1 LIKE " + t + "lk0",
			check: func(tt *testing.T, db *sql.DB) {
				tt.Helper()
				want("SELECT id, v FROM "+t+"lk1", 0)(tt, db)
				mustExec(tt, db, "DROP TABLE IF EXISTS "+t+"lk1")
			}},
		{name: "transactional table", run: "CREATE TABLE " + t + "tx " + transactional, setup: []string{"DROP TABLE IF EXISTS " + t + "tx"}, check: func(tt *testing.T, db *sql.DB) {
			tt.Helper()
			mustExec(tt, db, "DROP TABLE IF EXISTS "+t+"tx")
		}},
		{name: "primary key", run: "CREATE TABLE " + t + "pk " + transactional, setup: []string{"DROP TABLE IF EXISTS " + t + "pk"}, check: func(tt *testing.T, db *sql.DB) {
			tt.Helper()
			mustExec(tt, db, "DROP TABLE IF EXISTS "+t+"pk")
		}},
		{name: "foreign key", tables: map[string]string{t + "fk0": transactional}, run: "CREATE TABLE " + t + "fk1 (id BIGINT, FOREIGN KEY (id) REFERENCES " + t + "fk0 (id))", fail: true},
		{name: "unique constraint", run: "CREATE TABLE " + t + "uq (id BIGINT UNIQUE)", fail: true},
		{name: "default value", tables: map[string]string{t + "dfl": "(id BIGINT, v STRING DEFAULT 'd')"}, setup: []string{"INSERT INTO " + t + "dfl (id) VALUES (1)"},
			run: "SELECT v FROM " + t + "dfl", check: want("SELECT v FROM "+t+"dfl WHERE v = 'd'", 1)},
		{name: "index", tables: map[string]string{t + "idx": "(id BIGINT)"}, run: "CREATE INDEX " + t + "ix ON " + t + "idx (id)", fail: true},
		{name: "view", tables: map[string]string{t + "vw0": "(id BIGINT)"}, setup: []string{"DROP VIEW IF EXISTS " + t + "vw1"}, run: "CREATE VIEW " + t + "vw1 AS SELECT id FROM " + t + "vw0",
			check: func(tt *testing.T, db *sql.DB) {
				tt.Helper()
				want("SELECT id FROM "+t+"vw1", 0)(tt, db)
				mustExec(tt, db, "DROP VIEW IF EXISTS "+t+"vw1")
			}},
		{name: "materialized view", tables: map[string]string{t + "mv0": "(id BIGINT)"}, setup: []string{"DROP MATERIALIZED VIEW IF EXISTS " + t + "mv1"}, run: "CREATE MATERIALIZED VIEW " + t + "mv1 AS SELECT id FROM " + t + "mv0",
			check: func(tt *testing.T, db *sql.DB) {
				tt.Helper()
				mustExec(tt, db, "DROP MATERIALIZED VIEW IF EXISTS "+t+"mv1")
			}},
		{name: "add partition", tables: map[string]string{t + "prt": "(id BIGINT) PARTITIONED BY (ds STRING)"}, run: "ALTER TABLE " + t + "prt ADD PARTITION (ds='2026')",
			check: func(tt *testing.T, db *sql.DB) {
				tt.Helper()
				if got := textOf(tt, db, "SHOW PARTITIONS "+t+"prt"); !strings.Contains(got, "ds=2026") {
					tt.Errorf("SHOW PARTITIONS gave %q, want the partition", got)
				}
			}},
		{name: "clustered table", tables: map[string]string{t + "cls": "(id BIGINT, v STRING) CLUSTERED BY (id) SORTED BY (id) INTO 4 BUCKETS"}, run: "SELECT 1 AS a"},
		{name: "schema", run: "CREATE SCHEMA dbimp_it_f_schema", fail: true},
		{name: "script mode", run: "SET odps.sql.type.system.odps2=true; SELECT 1 AS a", args: []any{script}},
		{name: "set flags in the text", run: "SET odps.sql.type.system.odps2=true; SELECT 1 AS a", fail: true},
		{name: "mapjoin hint", tables: map[string]string{t + "mj": "(id BIGINT)"}, run: "SELECT /*+ mapjoin(b) */ a.id FROM " + t + "mj a JOIN " + t + "mj b ON a.id = b.id"},
		{name: "explain output for the types of a result", run: "EXPLAIN OUTPUT SELECT 1 AS a, 'x' AS b", check: func(tt *testing.T, db *sql.DB) {
			tt.Helper()
			if got := textOf(tt, db, "EXPLAIN OUTPUT SELECT 1 AS a, 'x' AS b"); !strings.Contains(got, "a\tINT") || !strings.Contains(got, "b\tSTRING") {
				tt.Errorf("EXPLAIN OUTPUT gave %q, want the types", got)
			}
		}},
		{name: "typeof function", run: "SELECT TYPEOF(1) AS a", fail: true},
		{name: "output format setting with a header", run: "SET odps.sql.select.output.format={\"needHeader\":true,\"fieldDelim\":\",\"}; SELECT 1 AS a", args: []any{script}},
		{name: "declare of a variable", run: "DECLARE @p BIGINT = 123; SELECT @p AS a", args: []any{script}, fail: true},
		{name: "limit with an offset", run: ints + " ORDER BY pos LIMIT 10000, 5", check: func(tt *testing.T, db *sql.DB) {
			tt.Helper()
			want(ints+" ORDER BY pos LIMIT 10000, 5", 5)(tt, db)
		}},
		{name: "row number slices", run: "SELECT pos FROM (SELECT pos, ROW_NUMBER() OVER (ORDER BY pos) AS rn FROM (" + ints + ") t) u WHERE rn BETWEEN 10001 AND 10005",
			check: want("SELECT pos FROM (SELECT pos, ROW_NUMBER() OVER (ORDER BY pos) AS rn FROM ("+ints+") t) u WHERE rn BETWEEN 10001 AND 10005", 5)},
		{name: "hint for the row limit of a result", run: ints, args: []any{maxcompute.WithParameter("odps.sql.read.table.max.row", 50000)}, check: func(tt *testing.T, db *sql.DB) {
			tt.Helper()
			// The hint changes nothing: the result still holds 10,000 rows.
			if got := rowsOf(tt, db, ints, maxcompute.WithParameter("odps.sql.read.table.max.row", 50000)); got != 10000 {
				tt.Errorf("the result has %d rows, want the 10000 of the cap", got)
			}
		}},
		{name: "hint for the auto limit", run: ints, args: []any{maxcompute.WithParameter("odps.sql.select.auto.limit", 50000)}},
		{name: "setting for the CSV null", run: "SELECT NULL AS a", args: []any{maxcompute.WithParameter("odps.sql.result.csv.null", "NULL")}, check: func(tt *testing.T, db *sql.DB) {
			tt.Helper()
			var v any = "unset"
			if err := db.QueryRowContext(tt.Context(), "SELECT CAST(NULL AS STRING) AS a", maxcompute.WithParameter("odps.sql.result.csv.null", "NULL")).Scan(&v); err != nil || v != nil {
				tt.Errorf("the NULL is %v and %v, want nil", v, err)
			}
		}},
	}
}

// mustExec runs a statement and fails the test if it fails.
func mustExec(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	if _, err := db.ExecContext(context.WithoutCancel(t.Context()), stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

// textOf runs a statement that answers text and returns the text.
func textOf(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	var text string
	if err := db.QueryRowContext(t.Context(), query).Scan(&text); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return text
}

// TestIntegrationFeatures runs the statement of each entry of the survey that is
// not a type, on the project, and checks that the server did what the verdict of
// the entry says (step 14a). An entry with the verdict no is a statement that the
// test sends and the server refuses.
func TestIntegrationFeatures(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	for _, f := range features() {
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			for name, def := range f.tables {
				mustExec(t, db, "DROP TABLE IF EXISTS "+name)
				mustExec(t, db, "CREATE TABLE "+name+" "+def)
				t.Cleanup(func() { mustExec(t, db, "DROP TABLE IF EXISTS "+name) })
			}
			for _, stmt := range f.setup {
				mustExec(t, db, stmt)
			}
			_, err := db.ExecContext(t.Context(), f.run, f.args...)
			switch {
			case f.fail && err == nil:
				t.Fatalf("%s: the server took a statement that the survey says it refuses", f.run)
			case !f.fail && err != nil:
				t.Fatalf("%s: %v", f.run, err)
			}
			if f.check != nil {
				f.check(t, db)
			}
		})
	}
}
