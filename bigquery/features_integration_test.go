package bigquery //nolint:testpackage // The integration tests send raw requests with the token of the connector, which only the package can do.

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// notFoundOnTheEmulator is the reason that the tests give when they skip an operation
// that docs/BIGQUERY.md shows that the emulator lacks.
const notFoundOnTheEmulator = "the emulator refuses or ignores it (docs/BIGQUERY.md, Hosted and emulator)"

// wait reads again until check is true, with a limit on the time, for an effect that
// the service makes after it answers, such as a streaming insert. It never sleeps for a
// fixed time.
func wait(t testing.TB, what string, limit time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %v", what, limit)
		}
		time.Sleep(time.Second)
	}
}

// count returns the count of the rows of a table.
func count(t testing.TB, db *sql.DB, name string) int64 {
	t.Helper()
	n, ok := scalar(t, db, "SELECT COUNT(*) FROM "+name).(int64)
	if !ok {
		t.Fatalf("the count of %s is not an int64", name)
	}
	return n
}

// TestIntegrationCRUD inserts, selects, updates and deletes rows in three tables, one of
// which refers to another, and compares each value that a select returns with the value
// that the test wrote (step 14a). The subtests run in order, because each one uses the
// rows that the one before it left.
func TestIntegrationCRUD(t *testing.T) {
	db := connect(t)
	cfg := itConfig(t)
	parent, child, extra, src := table(t, "crud_parent"), table(t, "crud_child"), table(t, "crud_extra"), table(t, "crud_src")
	drop(t, db, "DROP TABLE IF EXISTS "+child, "DROP TABLE IF EXISTS "+parent, "DROP TABLE IF EXISTS "+extra, "DROP TABLE IF EXISTS "+src)
	exec(t, db, "CREATE TABLE "+parent+" (id INT64, name STRING, score FLOAT64, PRIMARY KEY (id) NOT ENFORCED)")
	fk := ", FOREIGN KEY (parent_id) REFERENCES " + parent + "(id) NOT ENFORCED"
	if isEmulator(cfg) {
		// The emulator refuses a foreign key (recorded: "a create table with a foreign key").
		fk = ""
	}
	exec(t, db, "CREATE TABLE "+child+" (id INT64, parent_id INT64, qty NUMERIC"+fk+")")
	exec(t, db, "CREATE TABLE "+extra+" (id INT64, note STRING)")

	t.Run("insert", func(t *testing.T) {
		affected(t, db, 1, "INSERT INTO "+parent+" (id, name, score) VALUES (?, ?, ?)", int64(1), "ada", 1.5)
		affected(t, db, 2, "INSERT INTO "+parent+" VALUES (2, 'bob', 2.5), (3, 'cy', NULL)")
		affected(t, db, 1, "INSERT INTO "+child+" (id, parent_id, qty) VALUES (?, ?, ?)", int64(10), int64(1), dec(t, "12.50"))
		exec(t, db, "INSERT INTO "+child+" VALUES (11, 1, 3.25), (12, 2, 0.01)")
		if _, err := exec(t, db, "INSERT INTO "+parent+" (id) VALUES (4)").LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
		}
	})
	t.Run("select", func(t *testing.T) {
		got := query(t, db, "SELECT id, name, score FROM "+parent+" ORDER BY id")
		check(t, "the parents", got, [][]any{{int64(1), "ada", 1.5}, {int64(2), "bob", 2.5}, {int64(3), "cy", nil}, {int64(4), nil, nil}})
		join := query(t, db, "SELECT p.name, c.qty FROM "+child+" c JOIN "+parent+" p ON p.id = c.parent_id ORDER BY c.id")
		check(t, "the join", join, [][]any{{"ada", dec(t, "12.5")}, {"ada", dec(t, "3.25")}, {"bob", dec(t, "0.01")}})
		if got := count(t, db, child); got != 3 {
			t.Errorf("the count is %d, want 3", got)
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
		affected(t, db, 2, "UPDATE "+parent+" SET name = ?, score = score + ? WHERE id <= ?", "renamed", 1.0, int64(2))
		check(t, "after the update", query(t, db, "SELECT name, score FROM "+parent+" WHERE id <= 2 ORDER BY id"), [][]any{{"renamed", 2.5}, {"renamed", 3.5}})
		affected(t, db, 0, "UPDATE "+parent+" SET name = 'x' WHERE id = 999")
		// The service refuses an update with no WHERE clause (recorded: bigquery-027).
		_, err := db.ExecContext(t.Context(), "UPDATE "+parent+" SET name = 'x'")
		if err == nil {
			t.Error("an update with no WHERE clause gave no error")
		}
	})
	t.Run("delete", func(t *testing.T) {
		affected(t, db, 1, "DELETE FROM "+child+" WHERE id = ?", int64(12))
		if got := query(t, db, "SELECT id FROM "+child+" WHERE id = 12"); len(got) != 0 {
			t.Errorf("the deleted row is still there: %v", got)
		}
		affected(t, db, 1, "DELETE FROM "+parent+" WHERE id = 4")
		if _, err := db.ExecContext(t.Context(), "DELETE FROM "+parent); err == nil {
			t.Error("a delete with no WHERE clause gave no error")
		}
	})
	t.Run("merge", func(t *testing.T) {
		exec(t, db, "CREATE TABLE "+src+" AS SELECT 3 AS id, 'cyrus' AS name UNION ALL SELECT 40 AS id, 'forty' AS name")
		affected(t, db, 2, "MERGE "+parent+" t USING "+src+" u ON t.id = u.id WHEN MATCHED THEN UPDATE SET name = u.name WHEN NOT MATCHED THEN INSERT (id, name) VALUES (u.id, u.name)")
		check(t, "after the merge", query(t, db, "SELECT id, name FROM "+parent+" WHERE id IN (3, 40) ORDER BY id"), [][]any{{int64(3), "cyrus"}, {int64(40), "forty"}})
	})
	t.Run("insert select", func(t *testing.T) {
		affected(t, db, 3, "INSERT INTO "+extra+" (id, note) SELECT id, name FROM "+parent+" WHERE id IN (1, 2, 3)")
		if got := count(t, db, extra); got != 3 {
			t.Errorf("the count is %d, want 3", got)
		}
	})
	t.Run("truncate table", func(t *testing.T) {
		affected(t, db, 3, "TRUNCATE TABLE "+extra)
		if got := count(t, db, extra); got != 0 {
			t.Errorf("after TRUNCATE the count is %d, want 0", got)
		}
	})
	t.Run("export data", func(t *testing.T) {
		hostedOnly(t, "EXPORT DATA fails on the emulator for lack of credentials")
		b := bucket(t)
		uri := "gs://" + b + "/" + prefix + "/export-*.csv"
		exec(t, db, "EXPORT DATA OPTIONS (uri = '"+uri+"', format = 'CSV', overwrite = true) AS SELECT id, name FROM "+parent+" ORDER BY id")
		// The statement type of the answer is EXPORT_DATA, and the load below reads the files back.
	})
	t.Run("load data", func(t *testing.T) {
		hostedOnly(t, "LOAD DATA loads nothing on the emulator")
		b := bucket(t)
		loaded := table(t, "crud_load")
		drop(t, db, "DROP TABLE IF EXISTS "+loaded)
		exec(t, db, "CREATE TABLE "+loaded+" (id INT64, name STRING)")
		// The export wrote no header, so skip_leading_rows = 0 reads every row.
		exec(t, db, "LOAD DATA INTO "+loaded+" (id INT64, name STRING) FROM FILES (format = 'CSV', uris = ['gs://"+b+"/"+prefix+"/export-*.csv'])")
		if got := count(t, db, loaded); got != count(t, db, parent) {
			t.Errorf("the load read %d rows, want the %d that the export wrote", got, count(t, db, parent))
		}
	})
}

// TestIntegrationSchema makes each kind of object that features.json marks yes, uses it,
// and drops it. An entry marked no sends the operation and expects the refusal.
func TestIntegrationSchema(t *testing.T) {
	db := connect(t)
	cfg := itConfig(t)
	base := table(t, "sch_base")
	drop(t, db, "DROP TABLE IF EXISTS "+base)
	exec(t, db, "CREATE TABLE "+base+" (id INT64, doc STRING, v ARRAY<FLOAT64>, PRIMARY KEY (id) NOT ENFORCED)")
	exec(t, db, "INSERT INTO "+base+" (id, doc, v) VALUES (1, 'alpha beta', [1.0, 2.0]), (2, 'gamma', [3.0, 4.0])")

	t.Run("table", func(t *testing.T) {
		name := table(t, "sch_ctas")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" AS SELECT id FROM "+base)
		if got := count(t, db, name); got != 2 {
			t.Errorf("the count is %d, want 2", got)
		}
		if !isEmulator(cfg) {
			// The emulator answers duplicate for CREATE OR REPLACE (recorded: bigquery-287 on the service).
			exec(t, db, "CREATE OR REPLACE TABLE "+name+" AS SELECT 1 AS id")
			if got := count(t, db, name); got != 1 {
				t.Errorf("after CREATE OR REPLACE the count is %d, want 1", got)
			}
		}
		exec(t, db, "DROP TABLE IF EXISTS "+table(t, "sch_nosuch"))
		if _, err := db.ExecContext(t.Context(), "CREATE TABLE "+name+" (id INT64)"); err == nil {
			t.Error("CREATE TABLE of a table that exists gave no error")
		}
	})
	t.Run("primary key", func(t *testing.T) {
		hostedOnly(t, "the emulator enforces the key in its SQLite, and the service does not")
		name := table(t, "sch_pk")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" (id INT64, PRIMARY KEY (id) NOT ENFORCED)")
		// The key is not enforced, so a duplicate goes in.
		exec(t, db, "INSERT INTO "+name+" (id) VALUES (1), (1)")
		if got := count(t, db, name); got != 2 {
			t.Errorf("the count is %d, want 2, because the key is not enforced", got)
		}
	})
	t.Run("foreign key", func(t *testing.T) {
		hostedOnly(t, "the emulator refuses a foreign key")
		pk, fk := table(t, "sch_fkp"), table(t, "sch_fkc")
		drop(t, db, "DROP TABLE IF EXISTS "+fk, "DROP TABLE IF EXISTS "+pk)
		exec(t, db, "CREATE TABLE "+pk+" (id INT64, PRIMARY KEY (id) NOT ENFORCED)")
		exec(t, db, "CREATE TABLE "+fk+" (id INT64, FOREIGN KEY (id) REFERENCES "+pk+"(id) NOT ENFORCED)")
		// The key is not enforced, so a row with no parent goes in.
		exec(t, db, "INSERT INTO "+fk+" (id) VALUES (99)")
		got := query(t, db, "SELECT column_name FROM `"+cfg.Project+"`."+cfg.Dataset+".INFORMATION_SCHEMA.KEY_COLUMN_USAGE WHERE table_name = '"+prefix+"_sch_fkc'")
		if len(got) == 0 {
			t.Error("INFORMATION_SCHEMA.KEY_COLUMN_USAGE holds no key of the table")
		}
	})
	t.Run("unique constraint", func(t *testing.T) {
		// BigQuery has no UNIQUE (recorded: bigquery-290), and the service refuses it with a syntax error.
		name := table(t, "sch_uq")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		_, err := db.ExecContext(t.Context(), "CREATE TABLE "+name+" (id INT64 UNIQUE)")
		if err == nil {
			t.Error("the service accepted UNIQUE, so the survey is wrong")
		}
	})
	t.Run("search index", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_search")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" (id INT64, doc STRING)")
		exec(t, db, "INSERT INTO "+name+" (id, doc) VALUES (1, 'alpha beta'), (2, 'gamma')")
		exec(t, db, "CREATE SEARCH INDEX "+prefix+"_sch_six ON "+name+" (doc)")
		check(t, "SEARCH", query(t, db, "SELECT id FROM "+name+" WHERE SEARCH(doc, 'alpha')"), [][]any{{int64(1)}})
	})
	t.Run("vector index", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		// An index of the type IVF needs 5000 rows (recorded: bigquery-403), so the service
		// refuses it for this table, and the refusal shows that the service knows the index.
		_, err := db.ExecContext(t.Context(), "CREATE VECTOR INDEX "+prefix+"_sch_vix ON "+base+" (v) OPTIONS (index_type = 'IVF', distance_type = 'EUCLIDEAN')")
		if err == nil {
			exec(t, db, "DROP VECTOR INDEX IF EXISTS "+prefix+"_sch_vix ON "+base)
			return
		}
		if e := serverError(t, err); !strings.Contains(e.Message, "smaller than min allowed") {
			t.Errorf("the refusal is %+v, want the one about the count of rows", *e)
		}
	})
	t.Run("view", func(t *testing.T) {
		name := table(t, "sch_view")
		drop(t, db, "DROP VIEW IF EXISTS "+name)
		exec(t, db, "CREATE VIEW "+name+" AS SELECT id FROM "+base)
		check(t, "the view", query(t, db, "SELECT id FROM "+name+" ORDER BY id"), [][]any{{int64(1)}, {int64(2)}})
	})
	t.Run("materialized view", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_mv")
		drop(t, db, "DROP MATERIALIZED VIEW IF EXISTS "+name)
		exec(t, db, "CREATE MATERIALIZED VIEW "+name+" AS SELECT id, COUNT(*) AS c FROM "+base+" GROUP BY id")
		if got := count(t, db, name); got != 2 {
			t.Errorf("the count of the view is %d, want 2", got)
		}
	})
	t.Run("default value", func(t *testing.T) {
		name := table(t, "sch_def")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" (id INT64 DEFAULT 5, d DATE)")
		exec(t, db, "INSERT INTO "+name+" (d) VALUES (DATE '2024-01-02')")
		check(t, "the default", query(t, db, "SELECT id FROM "+name), [][]any{{int64(5)}})
	})
	t.Run("partitioning", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_part")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" (id INT64, d DATE) PARTITION BY d")
		exec(t, db, "INSERT INTO "+name+" (id, d) VALUES (1, DATE '2024-01-02'), (2, DATE '2024-01-03')")
		check(t, "one partition", query(t, db, "SELECT id FROM "+name+" WHERE d = DATE '2024-01-03'"), [][]any{{int64(2)}})
	})
	t.Run("clustering", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_clu")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" (id INT64, d DATE) PARTITION BY d CLUSTER BY id")
		exec(t, db, "INSERT INTO "+name+" (id, d) VALUES (1, DATE '2024-01-02')")
		if got := count(t, db, name); got != 1 {
			t.Errorf("the count is %d, want 1", got)
		}
	})
	t.Run("snapshot", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_snap")
		// The account lacks the right to drop a snapshot or to set its expiry, so the
		// snapshot stays until the dataset expires it (7 days).
		exec(t, db, "CREATE SNAPSHOT TABLE "+name+" CLONE "+base)
		// The account of the recorded run cannot delete a snapshot (recorded: bigquery-450),
		// so a refusal of the drop is logged and a person drops the snapshot.
		t.Cleanup(func() {
			if _, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP SNAPSHOT TABLE IF EXISTS "+name); err != nil {
				t.Logf("the account cannot drop the snapshot %s, so a person must: %v", name, err)
			}
		})
		if got := count(t, db, name); got != 2 {
			t.Errorf("the count of the snapshot is %d, want 2", got)
		}
	})
	t.Run("clone", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_clone")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" CLONE "+base)
		if got := count(t, db, name); got != 2 {
			t.Errorf("the count of the clone is %d, want 2", got)
		}
	})
	t.Run("function", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_fn")
		drop(t, db, "DROP FUNCTION IF EXISTS "+name)
		exec(t, db, "CREATE FUNCTION "+name+"(x INT64) RETURNS INT64 AS (x + 1)")
		if got := scalar(t, db, "SELECT "+name+"(41)"); got != int64(42) {
			t.Errorf("the function gave %v, want 42", got)
		}
	})
	t.Run("table function", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_tf")
		drop(t, db, "DROP TABLE FUNCTION IF EXISTS "+name)
		exec(t, db, "CREATE TABLE FUNCTION "+name+"(x INT64) AS SELECT x AS a")
		check(t, "the table function", query(t, db, "SELECT a FROM "+name+"(7)"), [][]any{{int64(7)}})
	})
	t.Run("procedure", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := table(t, "sch_proc")
		drop(t, db, "DROP PROCEDURE IF EXISTS "+name)
		exec(t, db, "CREATE PROCEDURE "+name+"() BEGIN SELECT 1; END")
		check(t, "the procedure", query(t, db, "CALL "+name+"()"), [][]any{{int64(1)}})
	})
	t.Run("external table", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		b := bucket(t)
		name := table(t, "sch_ext")
		drop(t, db, "DROP EXTERNAL TABLE IF EXISTS "+name)
		exec(t, db, "EXPORT DATA OPTIONS (uri = 'gs://"+b+"/"+prefix+"/ext-*.csv', format = 'CSV', overwrite = true) AS SELECT id, doc FROM "+base+" ORDER BY id")
		exec(t, db, "CREATE EXTERNAL TABLE "+name+" (id INT64, doc STRING) OPTIONS (format = 'CSV', uris = ['gs://"+b+"/"+prefix+"/ext-*.csv'])")
		if got := count(t, db, name); got != 2 {
			t.Errorf("the count of the external table is %d, want 2", got)
		}
	})
	t.Run("dataset", func(t *testing.T) {
		hostedOnly(t, "the emulator refuses DROP SCHEMA")
		ds := "dbimp_it_" + suffix + "_ds"
		full := "`" + cfg.Project + "`." + ds
		drop(t, db, "DROP SCHEMA IF EXISTS "+full+" CASCADE")
		exec(t, db, "CREATE SCHEMA "+full)
		exec(t, db, "CREATE TABLE "+full+".t (id INT64)")
		exec(t, db, "INSERT INTO "+full+".t (id) VALUES (1)")
		if got := count(t, db, full+".t"); got != 1 {
			t.Errorf("the count is %d, want 1", got)
		}
	})
	t.Run("alter table add column", func(t *testing.T) {
		hostedOnly(t, "the emulator does not add the column")
		name := table(t, "sch_alter")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" (id INT64)")
		exec(t, db, "ALTER TABLE "+name+" ADD COLUMN n INT64")
		exec(t, db, "INSERT INTO "+name+" (id, n) VALUES (1, 2)")
		check(t, "the new column", query(t, db, "SELECT id, n FROM "+name), [][]any{{int64(1), int64(2)}})
	})
	t.Run("alter table rename", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		from, to := table(t, "sch_rn1"), prefix+"_sch_rn2"
		drop(t, db, "DROP TABLE IF EXISTS "+from, "DROP TABLE IF EXISTS "+table(t, "sch_rn2"))
		exec(t, db, "CREATE TABLE "+from+" (id INT64)")
		exec(t, db, "ALTER TABLE "+from+" RENAME TO "+to)
		if got := count(t, db, table(t, "sch_rn2")); got != 0 {
			t.Errorf("the count of the renamed table is %d, want 0", got)
		}
	})
	t.Run("alter table drop column", func(t *testing.T) {
		hostedOnly(t, "DROP COLUMN fails on the emulator")
		name := table(t, "sch_dropcol")
		drop(t, db, "DROP TABLE IF EXISTS "+name)
		exec(t, db, "CREATE TABLE "+name+" (id INT64, n INT64)")
		exec(t, db, "ALTER TABLE "+name+" DROP COLUMN n")
		if cols := query(t, db, "SELECT * FROM "+name); len(cols) != 0 {
			t.Errorf("the table holds rows: %v", cols)
		}
	})
}

// TestIntegrationFeatures uses each feature that features.json marks yes, and compares
// what it returns.
func TestIntegrationFeatures(t *testing.T) {
	db := connect(t)
	cfg := itConfig(t)
	base := table(t, "ft_base")
	drop(t, db, "DROP TABLE IF EXISTS "+base)
	exec(t, db, "CREATE TABLE "+base+" (id INT64, s STRING)")
	exec(t, db, "INSERT INTO "+base+" (id, s) VALUES (1, 'a'), (2, 'b'), (3, 'c')")

	t.Run("dry run", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		rows, err := db.QueryContext(t.Context(), "SELECT id, s FROM "+base, WithParameter("dryRun", true))
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		if len(cols) != 2 || rows.Next() || rows.Err() != nil {
			t.Errorf("a dry run gave the columns %v, a row, or the error %v", cols, rows.Err())
		}
	})
	t.Run("query cache", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		exec(t, db, "SELECT COUNT(*) FROM "+base, WithParameter("useQueryCache", false))
		exec(t, db, "SELECT COUNT(*) FROM "+base, WithParameter("useQueryCache", true))
	})
	t.Run("sessions", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		// The driver can create a session, and it carries none across requests (D189 item 3), so a
		// temporary table of a session is not there for the next statement.
		exec(t, db, "SELECT 1", WithParameter("createSession", true))
	})
	t.Run("multi statement script", func(t *testing.T) {
		cols, rows := scriptResult(t, db, "SELECT 1 AS a; SELECT 2 AS b")
		if len(cols) != 1 || cols[0] != "b" || len(rows) != 1 || rows[0][0] != int64(2) {
			t.Errorf("the script gave %v and %v, want the rows of the last statement", cols, rows)
		}
	})
	t.Run("script variables", func(t *testing.T) {
		hostedOnly(t, "on the emulator a variable replaces its name in later requests")
		check(t, "the variable", query(t, db, "DECLARE dbimp_v INT64 DEFAULT 5; SET dbimp_v = dbimp_v + 1; SELECT dbimp_v AS a"), [][]any{{int64(6)}})
		// The variable of the script does not stay (recorded: bigquery-243).
		check(t, "the name as a column", query(t, db, "SELECT 1 AS dbimp_v"), [][]any{{int64(1)}})
	})
	t.Run("transaction commit", func(t *testing.T) {
		hostedOnly(t, "the emulator refuses ROLLBACK and keeps the row of a failed script")
		tx := table(t, "ft_tx")
		drop(t, db, "DROP TABLE IF EXISTS "+tx)
		exec(t, db, "CREATE TABLE "+tx+" (id INT64)")
		exec(t, db, "BEGIN TRANSACTION; INSERT INTO "+tx+" (id) VALUES (1); COMMIT TRANSACTION")
		if got := count(t, db, tx); got != 1 {
			t.Errorf("after the commit the count is %d, want 1", got)
		}
	})
	t.Run("transaction rollback", func(t *testing.T) {
		hostedOnly(t, "the emulator refuses ROLLBACK and keeps the row of a failed script")
		tx := table(t, "ft_txr")
		drop(t, db, "DROP TABLE IF EXISTS "+tx)
		exec(t, db, "CREATE TABLE "+tx+" (id INT64)")
		exec(t, db, "BEGIN TRANSACTION; INSERT INTO "+tx+" (id) VALUES (2); ROLLBACK TRANSACTION")
		if got := count(t, db, tx); got != 0 {
			t.Errorf("after the rollback the count is %d, want 0", got)
		}
		// A script with an error inside the transaction leaves no row (recorded: bigquery-268).
		if _, err := db.ExecContext(t.Context(), "BEGIN TRANSACTION; INSERT INTO "+tx+" (id) VALUES (3); SELECT 1 / 0; COMMIT TRANSACTION"); err == nil {
			t.Error("a script with a division by zero gave no error")
		}
		if got := count(t, db, tx); got != 0 {
			t.Errorf("after the failed script the count is %d, want 0", got)
		}
	})
	t.Run("wildcard table", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		w1, w2 := table(t, "ft_wc1"), table(t, "ft_wc2")
		drop(t, db, "DROP TABLE IF EXISTS "+w1, "DROP TABLE IF EXISTS "+w2)
		exec(t, db, "CREATE TABLE "+w1+" AS SELECT 1 AS id")
		exec(t, db, "CREATE TABLE "+w2+" AS SELECT 2 AS id")
		if got := count(t, db, "`"+cfg.Project+"."+cfg.Dataset+"."+prefix+"_ft_wc*`"); got != 2 {
			t.Errorf("the wildcard table holds %d rows, want 2", got)
		}
	})
	t.Run("time travel", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		check(t, "time travel", query(t, db, "SELECT COUNT(*) FROM "+base+" FOR SYSTEM_TIME AS OF CURRENT_TIMESTAMP()"), [][]any{{int64(3)}})
	})
	t.Run("qualify", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		check(t, "QUALIFY", query(t, db, "SELECT id FROM "+base+" QUALIFY ROW_NUMBER() OVER (ORDER BY id) = 1"), [][]any{{int64(1)}})
	})
	t.Run("select except", func(t *testing.T) {
		cols, _ := scriptResult(t, db, "SELECT * EXCEPT (s) FROM "+base)
		if len(cols) != 1 || cols[0] != "id" {
			t.Errorf("SELECT * EXCEPT gave the columns %v", cols)
		}
	})
	t.Run("select replace", func(t *testing.T) {
		check(t, "REPLACE", query(t, db, "SELECT * REPLACE ('r' AS s) FROM "+base+" WHERE id = 1"), [][]any{{int64(1), "r"}})
	})
	t.Run("unnest", func(t *testing.T) {
		check(t, "UNNEST", query(t, db, "SELECT x FROM UNNEST([1, 2, 3]) AS x ORDER BY x"), [][]any{{int64(1)}, {int64(2)}, {int64(3)}})
		check(t, "IN UNNEST", query(t, db, "SELECT id FROM "+base+" WHERE id IN UNNEST([1, 3]) ORDER BY id"), [][]any{{int64(1)}, {int64(3)}})
	})
	t.Run("safe prefix", func(t *testing.T) {
		check(t, "SAFE", query(t, db, "SELECT SAFE_CAST('x' AS INT64) AS a, SAFE_DIVIDE(1, 0) AS b, SAFE.PARSE_DATE('%Y', 'x') AS c"), [][]any{{nil, nil, nil}})
	})
	t.Run("pipe syntax", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		check(t, "pipe", query(t, db, "FROM "+base+" |> WHERE id = 1 |> SELECT id"), [][]any{{int64(1)}})
	})
	t.Run("named parameters", func(t *testing.T) {
		check(t, "named", query(t, db, "SELECT s FROM "+base+" WHERE id = @id", sql.Named("id", int64(2))), [][]any{{"b"}})
	})
	t.Run("positional parameters", func(t *testing.T) {
		check(t, "positional", query(t, db, "SELECT s FROM "+base+" WHERE id = ?", int64(3)), [][]any{{"c"}})
	})
	t.Run("labels", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		label := "dbimp_label_" + suffix
		exec(t, db, "SELECT 1", WithParameter("labels", map[string]string{"dbimp_test": label}))
		q := "SELECT COUNT(*) FROM `region-us`.INFORMATION_SCHEMA.JOBS_BY_PROJECT WHERE creation_time > TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 1 HOUR) AND EXISTS (SELECT 1 FROM UNNEST(labels) AS l WHERE l.key = 'dbimp_test' AND l.value = @label)"
		wait(t, "the job with its label in INFORMATION_SCHEMA", 2*time.Minute, func() bool {
			n, _ := scalar(t, db, q, sql.Named("label", label)).(int64)
			return n > 0
		})
	})
	t.Run("maximum bytes billed", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		_, err := db.ExecContext(t.Context(), "SELECT * FROM "+base, WithParameter("maximumBytesBilled", "1"), WithParameter("useQueryCache", false))
		if e := serverError(t, err); e.Reason != "bytesBilledLimitExceeded" {
			t.Errorf("the error is %+v, want bytesBilledLimitExceeded", *e)
		}
	})
	t.Run("location", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		// The location of the dataset is US or EU in the recorded run, and a job in another location than the
		// dataset fails, so the test sends the location of the DSN, or US.
		loc := cfg.Location
		if loc == "" {
			loc = "US"
		}
		if got := scalar(t, db, "SELECT 1", WithLocation(loc)); got != int64(1) {
			t.Errorf("SELECT 1 in %s gave %v", loc, got)
		}
	})
	t.Run("request id", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		tx := table(t, "ft_req")
		drop(t, db, "DROP TABLE IF EXISTS "+tx)
		exec(t, db, "CREATE TABLE "+tx+" (id INT64)")
		id := "dbimp-" + suffix
		for range 2 {
			// The second request has the same id and the same body, and the service runs it once
			// (recorded: bigquery-380 to bigquery-382). It answers jobComplete false with the count,
			// which the driver reads as the end.
			if _, err := db.ExecContext(t.Context(), "INSERT INTO "+tx+" (id) VALUES (1)", WithParameter("requestId", id)); err != nil {
				t.Fatal(err)
			}
		}
		if got := count(t, db, tx); got != 1 {
			t.Errorf("the count is %d, want 1, because the service ran the insert once", got)
		}
	})
	t.Run("default dataset", func(t *testing.T) {
		name := prefix + "_ft_dsd"
		full := table(t, "ft_dsd")
		drop(t, db, "DROP TABLE IF EXISTS "+full)
		// The driver sends the dataset of the DSN, so a name with no dataset works.
		exec(t, db, "CREATE TABLE "+name+" (id INT64)", WithDatabase(cfg.Dataset))
		if got := count(t, db, name); got != 0 {
			t.Errorf("the count is %d, want 0", got)
		}
	})
	t.Run("async execution", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		id := "dbimp-job-" + suffix
		body := `{"configuration":{"query":{"query":"SELECT 1 AS a","useLegacySql":false}},"jobReference":{"projectId":"` + cfg.Project + `","jobId":"` + id + `"}}`
		if status, out := raw(t, http.MethodPost, "/bigquery/v2/projects/"+cfg.Project+"/jobs", []byte(body), nil); status != 200 {
			t.Fatalf("jobs.insert gave %d: %s", status, out)
		}
		wait(t, "the job to be DONE", time.Minute, func() bool {
			_, out := raw(t, http.MethodGet, "/bigquery/v2/projects/"+cfg.Project+"/jobs/"+id, nil, nil)
			return strings.Contains(string(out), `"state": "DONE"`)
		})
		status, out := raw(t, http.MethodGet, "/bigquery/v2/projects/"+cfg.Project+"/queries/"+id, nil, nil)
		if status != 200 || !strings.Contains(string(out), `"jobComplete": true`) {
			t.Errorf("getQueryResults gave %d: %s", status, out)
		}
	})
	t.Run("job cancel", func(t *testing.T) {
		hostedOnly(t, "the emulator reports every job as done")
		id := "dbimp-slow-" + suffix
		qb, err := json.Marshal(map[string]any{
			"configuration": map[string]any{"query": map[string]any{"query": slowQuery, "useLegacySql": false, "useQueryCache": false}},
			"jobReference":  map[string]any{"projectId": cfg.Project, "jobId": id},
		})
		if err != nil {
			t.Fatal(err)
		}
		if status, out := raw(t, http.MethodPost, "/bigquery/v2/projects/"+cfg.Project+"/jobs", qb, nil); status != 200 {
			t.Fatalf("jobs.insert gave %d: %s", status, out)
		}
		if status, out := raw(t, http.MethodPost, "/bigquery/v2/projects/"+cfg.Project+"/jobs/"+id+"/cancel", []byte("{}"), nil); status != 200 {
			t.Fatalf("jobs.cancel gave %d: %s", status, out)
		}
		// The cancel takes effect a little after its answer (recorded: bigquery-210 to bigquery-212).
		wait(t, "the job to stop", time.Minute, func() bool {
			status, out := raw(t, http.MethodGet, "/bigquery/v2/projects/"+cfg.Project+"/queries/"+id, nil, nil)
			return status == 499 && strings.Contains(string(out), "stopped")
		})
	})
	t.Run("result paging", func(t *testing.T) {
		big := table(t, "ft_big")
		drop(t, db, "DROP TABLE IF EXISTS "+big)
		exec(t, db, "CREATE TABLE "+big+" AS SELECT id, CONCAT('row', CAST(id AS STRING)) AS s FROM UNNEST(GENERATE_ARRAY(1, 3000)) AS id")
		for _, size := range []int{0, 500, 1000} {
			rows := query(t, db, "SELECT id, s FROM "+big+" ORDER BY id", WithMaxResults(size))
			if len(rows) != 3000 {
				t.Errorf("a page of %d gave %d rows, want 3000", size, len(rows))
				continue
			}
			for i, r := range rows {
				if r[0] != int64(i+1) {
					t.Fatalf("a page of %d: the row %d is %v", size, i, r)
				}
			}
		}
	})
	t.Run("integer timestamps", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		// The driver asks for ISO8601_STRING, so the request for the integer form goes by hand.
		status, out := raw(t, http.MethodPost, "/bigquery/v2/projects/"+cfg.Project+"/queries",
			[]byte(`{"query":"SELECT TIMESTAMP '9999-12-31 23:59:59.999999+00' AS ts","useLegacySql":false,"formatOptions":{"useInt64Timestamp":true}}`), nil)
		if status != 200 || !strings.Contains(string(out), "253402300799999999") {
			t.Errorf("useInt64Timestamp gave %d: %s", status, out)
		}
	})
	t.Run("information schema", func(t *testing.T) {
		got := query(t, db, "SELECT table_name, table_type FROM `"+cfg.Project+"`."+cfg.Dataset+".INFORMATION_SCHEMA.TABLES WHERE table_name = '"+prefix+"_ft_base'")
		check(t, "TABLES", got, [][]any{{prefix + "_ft_base", "BASE TABLE"}})
		cols := query(t, db, "SELECT column_name, data_type FROM `"+cfg.Project+"`."+cfg.Dataset+".INFORMATION_SCHEMA.COLUMNS WHERE table_name = '"+prefix+"_ft_base' ORDER BY ordinal_position")
		check(t, "COLUMNS", cols, [][]any{{"id", "INT64"}, {"s", "STRING"}})
	})
	t.Run("streaming insert", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		name := prefix + "_ft_stream"
		drop(t, db, "DROP TABLE IF EXISTS "+table(t, "ft_stream"))
		exec(t, db, "CREATE TABLE "+table(t, "ft_stream")+" (id INT64, s STRING)")
		path := "/bigquery/v2/projects/" + cfg.Project + "/datasets/" + cfg.Dataset + "/tables/" + name + "/insertAll"
		status, out := raw(t, http.MethodPost, path, []byte(`{"rows":[{"json":{"id":1,"s":"a"}},{"json":{"id":2,"s":"b"}}]}`), nil)
		if status != 200 || strings.Contains(string(out), "insertErrors") {
			t.Fatalf("insertAll gave %d: %s", status, out)
		}
		// A row with a field that does not exist is HTTP 200 with insertErrors (recorded: bigquery-361), so a
		// caller reads the body and not the code.
		status, out = raw(t, http.MethodPost, path, []byte(`{"rows":[{"json":{"nosuch":1}}]}`), nil)
		if status != 200 || !strings.Contains(string(out), "insertErrors") {
			t.Errorf("a row with a field that does not exist gave %d: %s", status, out)
		}
		// The service writes a streamed row to a buffer, which a query reads after a short time.
		wait(t, "the streamed rows to be readable", 2*time.Minute, func() bool {
			return count(t, db, table(t, "ft_stream")) == 2
		})
	})
	t.Run("gzip request body", func(t *testing.T) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(`{"query":"SELECT 1 AS a","useLegacySql":false}`))
		_ = zw.Close()
		h := http.Header{"Content-Encoding": {"gzip"}}
		status, out := raw(t, http.MethodPost, "/bigquery/v2/projects/"+cfg.Project+"/queries", buf.Bytes(), h)
		if status != 200 || !strings.Contains(string(out), "jobComplete") {
			t.Errorf("a gzip body gave %d: %s", status, out)
		}
		// A body that says gzip and is not gzip is HTTP 400 (recorded: bigquery-364).
		status, _ = raw(t, http.MethodPost, "/bigquery/v2/projects/"+cfg.Project+"/queries", []byte(`{"query":"SELECT 1"}`), h)
		if status != 400 {
			t.Errorf("a body that is not gzip gave %d, want 400", status)
		}
	})
	t.Run("execute immediate", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		check(t, "EXECUTE IMMEDIATE", query(t, db, "EXECUTE IMMEDIATE 'SELECT 1 AS a'"), [][]any{{int64(1)}})
	})
	t.Run("assert", func(t *testing.T) {
		hostedOnly(t, notFoundOnTheEmulator)
		exec(t, db, "ASSERT 1 = 1 AS 'one is one'")
		_, err := db.ExecContext(t.Context(), "ASSERT 1 = 2 AS 'one is not two'")
		if e := serverError(t, err); !strings.Contains(e.Message, "one is not two") {
			t.Errorf("the assertion gave %+v, want its text", *e)
		}
	})
}

// scriptResult runs a statement and returns the columns and the rows of its result.
func scriptResult(t testing.TB, db *sql.DB, q string) ([]string, [][]any) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols, out
}

// raw sends one request to the service with the token of the connector, and returns the
// status and the body. It is for the requests that the driver never sends: jobs.insert,
// jobs.get, jobs.cancel, tabledata.insertAll, and a body that is gzip.
func raw(t testing.TB, method, path string, body []byte, header http.Header) (int, []byte) {
	t.Helper()
	c := NewConnector(itConfig(t))
	t.Cleanup(func() { _ = c.Close() })
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	defer cancel()
	token, err := c.bearer(ctx)
	if err != nil {
		t.Fatalf("logging in: %v", err)
	}
	u := c.base + path
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	maps.Copy(req.Header, header)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the answer to %s %s: %v", method, path, err)
	}
	return res.StatusCode, out
}
