package trino_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/trino"
)

// feature is one entry of testdata/trino/features.json. A name that starts with
// a flavor is an entry about that flavor, so the test runs on it and skips on the
// other, with the reason.
type feature struct {
	name string
	// minTrino is the first release of Trino that the entry holds on, or 0.
	minTrino, maxTrino int
	// parallel runs the entry in parallel with the other entries that say so.
	parallel bool
	run      func(t *testing.T, e env)
}

// run runs each entry as a subtest named for it, and skips an entry that is about
// another flavor or another release.
func runFeatures(t *testing.T, features []feature) {
	t.Helper()
	runFeaturesOn(t, newEnv(t), features)
}

// runFeaturesOn is runFeatures on a server that the test opened.
func runFeaturesOn(t *testing.T, e env, features []feature) {
	t.Helper()
	for _, f := range features {
		t.Run(f.name, func(t *testing.T) {
			if f.parallel {
				t.Parallel()
			}
			switch {
			case strings.HasPrefix(f.name, "trino ") && !e.isTrino():
				t.Skipf("the entry is about Trino, and the server is %s", e.name())
			case strings.HasPrefix(f.name, "presto ") && !e.isPresto():
				t.Skipf("the entry is about Presto, and the server is %s", e.name())
			case f.minTrino > 0 && e.major < f.minTrino:
				t.Skipf("the entry is about Trino %d and later, and the server is %s", f.minTrino, e.name())
			case f.maxTrino > 0 && (!e.isTrino() || e.major > f.maxTrino):
				t.Skipf("the entry is about Trino %d and earlier, and the server is %s", f.maxTrino, e.name())
			}
			f.run(t, e)
		})
	}
}

// crudTable makes the table that the tests of CRUD use, with two rows.
func crudTable(t *testing.T, e env, name string) string {
	t.Helper()
	tbl := e.table(t, name)
	e.must(t, "CREATE TABLE "+tbl+" (id integer, v varchar)")
	res := e.must(t, "INSERT INTO "+tbl+" VALUES (1, 'one'), (2, 'two')")
	if n, err := res.RowsAffected(); err != nil || n != 2 {
		t.Fatalf("the insert affected %d rows, %v, want 2", n, err)
	}
	return tbl
}

// wantRows compares the rows of a table, ordered by id, with want.
func wantRows(t *testing.T, e env, tbl string, want [][]any) {
	t.Helper()
	got := e.rows(t, "SELECT id, v FROM "+tbl+" ORDER BY id")
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the rows of %s are %v, want %v", tbl, got, want)
	}
}

// TestIntegrationCRUD holds step 14a for the statements of CRUD. The memory
// catalog is the only one that writes, and it refuses UPDATE, DELETE and MERGE, so
// the tests of those hold the refusal and that the rows are unchanged (D175).
func TestIntegrationCRUD(t *testing.T) {
	two := [][]any{{int64(1), "one"}, {int64(2), "two"}}
	refuse := func(statement string) func(*testing.T, env) {
		return func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			perr := e.refusal(t, fmt.Sprintf(statement, tbl))
			t.Logf("the server refuses: %v", perr)
			wantRows(t, e, tbl, two)
		}
	}
	runFeatures(t, []feature{
		{name: "insert", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			wantRows(t, e, tbl, two)
			// An argument is a literal of EXECUTE, and NULL goes in too.
			res := e.must(t, "INSERT INTO "+tbl+" VALUES (?, ?), (?, ?)", 3, "thr'ee", 4, nil)
			if n, err := res.RowsAffected(); err != nil || n != 2 {
				t.Errorf("the insert with arguments affected %d rows, %v, want 2", n, err)
			}
			wantRows(t, e, tbl, append(two, []any{int64(3), "thr'ee"}, []any{int64(4), nil}))
		}},
		{name: "select", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			got := e.rows(t, "SELECT id, v FROM "+tbl+" WHERE id >= ? ORDER BY id DESC", 1)
			if want := [][]any{{int64(2), "two"}, {int64(1), "one"}}; !reflect.DeepEqual(got, want) {
				t.Errorf("the rows are %v, want %v", got, want)
			}
			if n := e.scalar(t, "SELECT count(*) FROM "+tbl); n != int64(2) {
				t.Errorf("the count is %v, want 2", n)
			}
		}},
		{name: "update", run: refuse("UPDATE %s SET v = 'uno' WHERE id = 1")},
		{name: "delete", run: refuse("DELETE FROM %s WHERE id = 2")},
		{name: "merge", run: refuse("MERGE INTO %s t USING (VALUES (3, 'three')) s(id, v) ON t.id = s.id WHEN MATCHED THEN UPDATE SET v = s.v WHEN NOT MATCHED THEN INSERT (id, v) VALUES (s.id, s.v)")},
		{name: "upsert", run: refuse("INSERT INTO %s VALUES (1, 'x') ON CONFLICT (id) DO UPDATE SET v = 'y'")},
		{name: "delete with no where", run: refuse("DELETE FROM %s")},
		{name: "trino truncate", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.must(t, "TRUNCATE TABLE "+tbl)
			wantRows(t, e, tbl, nil)
		}},
		{name: "presto truncate", run: refuse("TRUNCATE TABLE %s")},
		{name: "create table as select", run: func(t *testing.T, e env) {
			src := crudTable(t, e, t.Name()+"_src")
			dst := e.table(t, t.Name()+"_dst")
			res := e.must(t, "CREATE TABLE "+dst+" AS SELECT id, v FROM "+src+" WHERE id > ?", 0)
			if n, err := res.RowsAffected(); err != nil || n != 2 {
				t.Errorf("the CREATE TABLE AS affected %d rows, %v, want 2", n, err)
			}
			wantRows(t, e, dst, two)
		}},
		{name: "insert from select", run: func(t *testing.T, e env) {
			src := crudTable(t, e, t.Name()+"_src")
			dst := e.table(t, t.Name()+"_dst")
			e.must(t, "CREATE TABLE "+dst+" (id integer, v varchar)")
			e.must(t, "INSERT INTO "+dst+" SELECT id + 10, upper(v) FROM "+src)
			wantRows(t, e, dst, [][]any{{int64(11), "ONE"}, {int64(12), "TWO"}})
		}},
	})
}

// TestIntegrationSchema holds step 14a for the operations on a schema.
func TestIntegrationSchema(t *testing.T) { //nolint:maintidx // One table, an entry for each operation of the survey.
	refuse := func(statement string) func(*testing.T, env) {
		return func(t *testing.T, e env) {
			tbl := e.table(t, t.Name())
			perr := e.refusal(t, fmt.Sprintf(statement, tbl))
			t.Logf("the server refuses: %v", perr)
		}
	}
	runFeatures(t, []feature{
		{name: "table", run: func(t *testing.T, e env) {
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer, v varchar)")
			if n := e.scalar(t, "SELECT count(*) FROM information_schema.tables WHERE table_name = ?", tbl); n != int64(1) {
				t.Errorf("the table is listed %v times, want once", n)
			}
			e.must(t, "DROP TABLE "+tbl)
			if n := e.scalar(t, "SELECT count(*) FROM information_schema.tables WHERE table_name = ?", tbl); n != int64(0) {
				t.Errorf("the dropped table is listed %v times, want none", n)
			}
		}},
		{name: "trino not null", run: func(t *testing.T, e env) {
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer NOT NULL)")
			e.refused(t, "INSERT INTO "+tbl+" VALUES (NULL)")
			e.must(t, "INSERT INTO "+tbl+" VALUES (1)")
			if got := e.scalar(t, "SELECT is_nullable FROM information_schema.columns WHERE table_name = ? AND column_name = 'id'", tbl); got != "NO" {
				t.Errorf("is_nullable is %v, want NO", got)
			}
		}},
		{name: "presto not null", run: refuse("CREATE TABLE %s (id integer NOT NULL)")},
		{name: "primary key", run: refuse("CREATE TABLE %s (id integer PRIMARY KEY, v varchar)")},
		{name: "foreign key", run: func(t *testing.T, e env) {
			parent := crudTable(t, e, t.Name()+"_parent")
			child := e.table(t, t.Name()+"_child")
			e.refused(t, "CREATE TABLE "+child+" (id integer, FOREIGN KEY (id) REFERENCES "+parent+" (id))")
		}},
		{name: "unique", run: refuse("CREATE TABLE %s (id integer, UNIQUE (id))")},
		{name: "index", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.refused(t, "CREATE INDEX i ON "+tbl+" (id)")
		}},
		{name: "trino 476 default value", maxTrino: 476, run: refuse("CREATE TABLE %s (id integer, v varchar DEFAULT 'x')")},
		{name: "trino 483 default value", minTrino: 483, run: func(t *testing.T, e env) {
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer, v varchar DEFAULT 'x')")
			e.must(t, "INSERT INTO "+tbl+" (id) VALUES (1)")
			wantRows(t, e, tbl, [][]any{{int64(1), "x"}})
		}},
		{name: "presto default value", run: func(t *testing.T, e env) {
			// Presto accepts the clause, and stores NULL in place of the default.
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer, v varchar DEFAULT 'x')")
			e.must(t, "INSERT INTO "+tbl+" (id) VALUES (1)")
			wantRows(t, e, tbl, [][]any{{int64(1), nil}})
		}},
		{name: "view", run: func(t *testing.T, e env) {
			src := crudTable(t, e, t.Name()+"_src")
			view := e.table(t, t.Name())
			e.must(t, "CREATE VIEW "+view+" AS SELECT id FROM "+src+" WHERE id < 2")
			if got := e.rows(t, "SELECT id FROM "+view); !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
				t.Errorf("the view holds %v, want id 1", got)
			}
			e.must(t, "DROP VIEW "+view)
		}},
		{name: "trino materialized view", run: refuse("CREATE MATERIALIZED VIEW %s AS SELECT 1 AS a")},
		{name: "presto materialized view", run: func(t *testing.T, e env) {
			mv := e.table(t, t.Name())
			e.must(t, "CREATE MATERIALIZED VIEW "+mv+" AS SELECT 1 AS a")
			t.Cleanup(func() {
				_, _ = e.db.ExecContext(context.WithoutCancel(t.Context()), "DROP MATERIALIZED VIEW IF EXISTS "+mv)
			})
			if got := e.scalar(t, "SELECT a FROM "+mv); got != int64(1) {
				t.Errorf("the materialized view holds %v, want 1", got)
			}
		}},
		{name: "trino comment", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.must(t, "COMMENT ON TABLE "+tbl+" IS 'a comment'")
			e.must(t, "COMMENT ON COLUMN "+tbl+".id IS 'the key'")
			if got := e.scalar(t, "SELECT comment FROM system.metadata.table_comments WHERE catalog_name = 'memory' AND schema_name = ? AND table_name = ?", schemaName, tbl); got != "a comment" {
				t.Errorf("the comment is %v, want a comment", got)
			}
		}},
		{name: "presto comment", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.refused(t, "COMMENT ON TABLE "+tbl+" IS 'a comment'")
		}},
		{name: "trino add column", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.must(t, "ALTER TABLE "+tbl+" ADD COLUMN w integer")
			if got := e.rows(t, "SELECT id, v, w FROM "+tbl+" WHERE id = 1"); !reflect.DeepEqual(got, [][]any{{int64(1), "one", nil}}) {
				t.Errorf("the new column holds %v, want NULL", got)
			}
		}},
		{name: "presto add column", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.refused(t, "ALTER TABLE "+tbl+" ADD COLUMN w integer")
		}},
		{name: "trino rename column", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.must(t, "ALTER TABLE "+tbl+" RENAME COLUMN v TO w")
			if got := e.rows(t, "SELECT w FROM "+tbl+" WHERE id = 1"); !reflect.DeepEqual(got, [][]any{{"one"}}) {
				t.Errorf("the renamed column holds %v, want one", got)
			}
		}},
		{name: "presto rename column", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.refused(t, "ALTER TABLE "+tbl+" RENAME COLUMN v TO w")
		}},
		{name: "drop column", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			e.refused(t, "ALTER TABLE "+tbl+" DROP COLUMN v")
		}},
		{name: "rename table", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			renamed := e.table(t, t.Name()+"_renamed")
			e.must(t, "ALTER TABLE "+tbl+" RENAME TO "+renamed)
			wantRows(t, e, renamed, [][]any{{int64(1), "one"}, {int64(2), "two"}})
		}},
		{name: "schema", run: func(t *testing.T, e env) {
			name := schemaName + "_s"
			e.must(t, "CREATE SCHEMA "+name)
			t.Cleanup(func() { _, _ = e.db.ExecContext(context.WithoutCancel(t.Context()), "DROP SCHEMA IF EXISTS "+name) })
			if n := e.scalar(t, "SELECT count(*) FROM information_schema.schemata WHERE schema_name = ?", name); n != int64(1) {
				t.Errorf("the schema is listed %v times, want once", n)
			}
			e.must(t, "DROP SCHEMA "+name)
			if n := e.scalar(t, "SELECT count(*) FROM information_schema.schemata WHERE schema_name = ?", name); n != int64(0) {
				t.Errorf("the dropped schema is listed %v times, want none", n)
			}
		}},
		{name: "show create table", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			got, ok := e.scalar(t, "SHOW CREATE TABLE "+tbl).(string)
			if !ok || !strings.Contains(strings.ToLower(got), "create table") || !strings.Contains(got, tbl) {
				t.Errorf("SHOW CREATE TABLE gave %q, want the statement of the table %s", got, tbl)
			}
		}},
		{name: "information schema columns", run: func(t *testing.T, e env) {
			tbl := crudTable(t, e, t.Name())
			got := e.rows(t, "SELECT column_name, ordinal_position, data_type FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position", schemaName, tbl)
			if want := [][]any{{"id", int64(1), "integer"}, {"v", int64(2), "varchar"}}; !reflect.DeepEqual(got, want) {
				t.Errorf("the columns are %v, want %v", got, want)
			}
		}},
	})
}

// TestIntegrationFeatures holds step 14a for the features of the servers: the
// parameters, the transactions, the session, the headers, the cancel and the
// differences between the flavors.
func TestIntegrationFeatures(t *testing.T) { //nolint:maintidx // One table, an entry for each feature of the survey.
	runFeatures(t, []feature{
		{name: "trino execute immediate", run: func(t *testing.T, e env) {
			if got := e.scalar(t, "EXECUTE IMMEDIATE 'SELECT 1 + 1'"); got != int64(2) {
				t.Errorf("EXECUTE IMMEDIATE gave %v, want 2", got)
			}
		}},
		{name: "presto execute immediate", run: func(t *testing.T, e env) {
			e.refused(t, "EXECUTE IMMEDIATE 'SELECT 1'")
		}},
		{name: "prepared statement header", run: func(t *testing.T, e env) {
			// The driver names its statement in the header for each argument.
			if got := e.scalar(t, "SELECT ? + ?", 1, 2); got != int64(3) {
				t.Errorf("a statement with arguments gave %v, want 3", got)
			}
			// A prepared statement of database/sql runs the same way.
			stmt, err := e.db.PrepareContext(t.Context(), "SELECT upper(?)")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			for _, in := range []string{"a", "b'c"} {
				var got string
				if err := stmt.QueryRowContext(t.Context(), in).Scan(&got); err != nil || got != strings.ToUpper(in) {
					t.Errorf("the prepared statement gave %q, %v, want %q", got, err, strings.ToUpper(in))
				}
			}
		}},
		{name: "positional parameters", run: func(t *testing.T, e env) {
			got := e.rows(t, "SELECT ?, ?, ?, ?, ?, ?, ?", true, int64(-7), 1.5, "é'\"\\", []byte{0, 255}, nil, time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("", 3600)))
			if len(got) != 1 {
				t.Fatalf("got %d rows, want 1", len(got))
			}
			want := []any{true, int64(-7), 1.5, "é'\"\\", []byte{0, 255}, nil, time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("", 3600))}
			for i, w := range want {
				if !equalValues(got[0][i], w) {
					t.Errorf("argument %d came back as %#v, want %#v", i, got[0][i], w)
				}
			}
			// The server counts the parameters, so a missing one is its error.
			e.refused(t, "SELECT ?, ?", 1)
		}},
		{name: "prepare", run: func(t *testing.T, e env) {
			conn := e.pinned(t)
			if _, err := conn.ExecContext(t.Context(), "PREPARE dbimp_s FROM SELECT ? + 1"); err != nil {
				t.Fatal(err)
			}
			var got int64
			if err := conn.QueryRowContext(t.Context(), "EXECUTE dbimp_s USING 4").Scan(&got); err != nil || got != 5 {
				t.Errorf("EXECUTE gave %d, %v, want 5", got, err)
			}
		}},
		{name: "deallocate", run: func(t *testing.T, e env) {
			conn := e.pinned(t)
			if _, err := conn.ExecContext(t.Context(), "PREPARE dbimp_d FROM SELECT 1"); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(t.Context(), "DEALLOCATE PREPARE dbimp_d"); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(t.Context(), "EXECUTE dbimp_d"); err == nil {
				t.Error("EXECUTE ran a statement that DEALLOCATE removed")
			}
		}},
		{name: "describe input", run: func(t *testing.T, e env) {
			conn := e.pinned(t)
			if _, err := conn.ExecContext(t.Context(), "PREPARE dbimp_i FROM SELECT ?, ?"); err != nil {
				t.Fatal(err)
			}
			rows, err := conn.QueryContext(t.Context(), "DESCRIBE INPUT dbimp_i")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			n := 0
			for rows.Next() {
				n++
			}
			if err := rows.Err(); err != nil || n != 2 {
				t.Errorf("DESCRIBE INPUT gave %d rows, %v, want one for each parameter", n, err)
			}
		}},
		{name: "named parameters", run: func(t *testing.T, e env) {
			if _, err := e.db.ExecContext(t.Context(), "SELECT :a", sql.Named("a", 1)); !errors.Is(err, dbimp.ErrArguments) {
				t.Errorf("a named argument gave %v, want dbimp.ErrArguments", err)
			}
			e.refused(t, "SELECT :a")
		}},
		{name: "trino parameter in limit", run: func(t *testing.T, e env) {
			if got := e.rows(t, "SELECT x FROM UNNEST(sequence(1, 10)) AS t(x) ORDER BY x LIMIT ?", 3); len(got) != 3 {
				t.Errorf("LIMIT ? gave %d rows, want 3", len(got))
			}
		}},
		{name: "presto parameter in limit", run: func(t *testing.T, e env) {
			e.refused(t, "SELECT x FROM UNNEST(sequence(1, 10)) AS t(x) LIMIT ?", 3)
		}},
		{name: "start transaction", run: func(t *testing.T, e env) {
			tx, err := e.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			var got int64
			if err := tx.QueryRowContext(t.Context(), "SELECT 41 + 1").Scan(&got); err != nil || got != 42 {
				t.Errorf("a select in a transaction gave %d, %v, want 42", got, err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			// The transaction is over, and its id goes out of the headers.
			if got := e.scalar(t, "SELECT 1"); got != int64(1) {
				t.Errorf("a statement after the commit gave %v", got)
			}
		}},
		{name: "start transaction with no header", run: func(t *testing.T, e env) {
			res := e.raw(t, rawRequest{statement: "START TRANSACTION"})
			if res.errName == "" {
				t.Errorf("START TRANSACTION with no header of the transaction ran: %+v", res)
			}
			t.Logf("the server answers %s: %s", res.errName, res.errMessage)
		}},
		{name: "read only transaction", run: func(t *testing.T, e env) {
			tx, err := e.db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			var got int64
			if err := tx.QueryRowContext(t.Context(), "SELECT count(*) FROM information_schema.tables").Scan(&got); err != nil {
				t.Errorf("a select in a read only transaction: %v", err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "nested transaction", run: func(t *testing.T, e env) {
			conn := e.pinned(t)
			tx, err := conn.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			// The connection holds the transaction, so the server refuses another.
			if _, err := tx.ExecContext(t.Context(), "START TRANSACTION"); err == nil {
				t.Error("START TRANSACTION ran inside a transaction")
			}
		}},
		{name: "trino write in a transaction", run: func(t *testing.T, e env) {
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer)")
			tx, err := e.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.ExecContext(t.Context(), "INSERT INTO "+tbl+" VALUES (1)")
			if perr, ok := errors.AsType[*trino.Error](err); !ok || perr.Name != "AUTOCOMMIT_WRITE_CONFLICT" {
				t.Errorf("a write in a transaction gave %v, want AUTOCOMMIT_WRITE_CONFLICT, because the memory catalog takes writes with autocommit only", err)
			}
			// The transaction is aborted, and each later statement fails.
			if _, err := tx.ExecContext(t.Context(), "SELECT 1"); err == nil {
				t.Error("a statement ran in a transaction that the failed write aborted")
			}
			if err := tx.Rollback(); err != nil {
				t.Errorf("the rollback of the aborted transaction: %v", err)
			}
			if n := e.scalar(t, "SELECT count(*) FROM "+tbl); n != int64(0) {
				t.Errorf("the table holds %v rows, want none", n)
			}
		}},
		{name: "presto write in a transaction", run: func(t *testing.T, e env) {
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer)")
			tx, err := e.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+tbl+" VALUES (1)"); err != nil {
				t.Fatalf("a write in a transaction: %v", err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if n := e.scalar(t, "SELECT count(*) FROM "+tbl); n != int64(1) {
				t.Errorf("the table holds %v rows, want 1", n)
			}
		}},
		{name: "trino rollback of a write", run: func(t *testing.T, e env) {
			// A write in a transaction is refused, so there is nothing to roll back,
			// and the table stays empty (see trino write in a transaction).
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer)")
			tx, err := e.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = tx.ExecContext(t.Context(), "INSERT INTO "+tbl+" VALUES (1)")
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if n := e.scalar(t, "SELECT count(*) FROM "+tbl); n != int64(0) {
				t.Errorf("the table holds %v rows after the rollback, want none", n)
			}
		}},
		{name: "presto rollback of a write", run: func(t *testing.T, e env) {
			// The memory catalog of Presto does not undo a write on ROLLBACK, so
			// the driver cannot make it atomic. The row stays (docs/TRINO.md, Transactions).
			tbl := e.table(t, t.Name())
			e.must(t, "CREATE TABLE "+tbl+" (id integer)")
			tx, err := e.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+tbl+" VALUES (1)"); err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if n := e.scalar(t, "SELECT count(*) FROM "+tbl); n != int64(1) {
				t.Errorf("the table holds %v rows after the rollback, want 1: the memory catalog keeps the write", n)
			}
		}},
		{name: "session property", run: func(t *testing.T, e env) {
			conn := e.pinned(t)
			if _, err := conn.ExecContext(t.Context(), "SET SESSION query_max_run_time = '5m'"); err != nil {
				t.Fatal(err)
			}
			var name, value string
			rows, err := conn.QueryContext(t.Context(), "SHOW SESSION LIKE 'query_max_run_time'")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() {
				t.Fatalf("SHOW SESSION gave no row: %v", rows.Err())
			}
			cols, _ := rows.Columns()
			ptrs := make([]any, len(cols))
			vals := make([]any, len(cols))
			for i := range ptrs {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			name, _ = vals[0].(string)
			value, _ = vals[1].(string)
			if name != "query_max_run_time" || value != "5m" {
				t.Errorf("SHOW SESSION gave %v, want query_max_run_time = 5m, which the connection sent back", vals)
			}
			rows.Close()
			if _, err := conn.ExecContext(t.Context(), "RESET SESSION query_max_run_time"); err != nil {
				t.Fatal(err)
			}
			var after string
			if err := conn.QueryRowContext(t.Context(), "SELECT current_user").Scan(&after); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "session property header", run: func(t *testing.T, e env) {
			rows := e.rows(t, "SHOW SESSION LIKE 'query_max_run_time'", trino.WithParameter("query_max_run_time", "7m"))
			if len(rows) != 1 || rows[0][1] != "7m" {
				t.Errorf("SHOW SESSION gave %v, want 7m from the option", rows)
			}
			if _, err := e.db.ExecContext(t.Context(), "SELECT 1", trino.WithParameter("nosuch_property", "1")); err == nil {
				t.Error("a property that the server does not know ran")
			}
			// The DSN holds a property too.
			db := sql.OpenDB(connect(t, e.dsn, func(c *trino.Config) { c.Session = map[string]string{"query_max_run_time": "9m"} }))
			defer db.Close()
			var name, value, def, typ, desc any
			if err := db.QueryRowContext(t.Context(), "SHOW SESSION LIKE 'query_max_run_time'").Scan(&name, &value, &def, &typ, &desc); err != nil {
				t.Fatalf("reading SHOW SESSION: %v", err)
			}
			if value != "9m" {
				t.Errorf("the property of the DSN is %v, want 9m", value)
			}
		}},
		{name: "use", run: func(t *testing.T, e env) {
			conn := e.pinned(t)
			if _, err := conn.ExecContext(t.Context(), "USE memory.information_schema"); err != nil {
				t.Fatal(err)
			}
			var n int64
			if err := conn.QueryRowContext(t.Context(), "SELECT count(*) FROM tables WHERE table_schema = 'information_schema'").Scan(&n); err != nil || n == 0 {
				t.Errorf("a table of the schema after USE gave %d, %v", n, err)
			}
			// The connection goes back to the pool, and the next caller starts in the
			// schema of the DSN.
			conn.Close()
			if e.isTrino() {
				if got := e.scalar(t, "SELECT current_schema"); got != schemaName {
					t.Errorf("the schema of a new caller is %v, want %s", got, schemaName)
				}
			}
		}},
		{name: "trino set path", run: func(t *testing.T, e env) {
			// Trino takes SET PATH only from a client that sends the capability PATH,
			// and the driver sends none, so it refuses (D175).
			e.refused(t, "SET PATH memory.default")
			res := e.raw(t, rawRequest{statement: "SET PATH memory.default", headers: map[string]string{e.prefix() + "Client-Capabilities": "PATH"}})
			if res.errName != "" || len(res.headers.Values(e.prefix()+"Set-Path")) == 0 {
				t.Errorf("SET PATH with the capability PATH gave %+v, want the header Set-Path", res)
			}
		}},
		{name: "presto set path", run: func(t *testing.T, e env) {
			e.refused(t, "SET PATH memory.default")
		}},
		{name: "trino set time zone", run: func(t *testing.T, e env) {
			conn := e.pinned(t)
			if _, err := conn.ExecContext(t.Context(), "SET TIME ZONE 'Asia/Jakarta'"); err != nil {
				t.Fatal(err)
			}
			var got string
			if err := conn.QueryRowContext(t.Context(), "SELECT current_timezone()").Scan(&got); err != nil || got != "Asia/Jakarta" {
				t.Errorf("the time zone is %q, %v, want Asia/Jakarta", got, err)
			}
		}},
		{name: "presto set time zone", run: func(t *testing.T, e env) {
			e.refused(t, "SET TIME ZONE 'Asia/Jakarta'")
		}},
		{name: "time zone header", run: func(t *testing.T, e env) {
			db := sql.OpenDB(connect(t, e.dsn, func(c *trino.Config) { c.TimeZone = "Asia/Jakarta" }))
			defer db.Close()
			var got string
			if err := db.QueryRowContext(t.Context(), "SELECT current_timezone()").Scan(&got); err != nil || got != "Asia/Jakarta" {
				t.Errorf("the time zone of the DSN is %q, %v, want Asia/Jakarta", got, err)
			}
			if err := db.QueryRowContext(t.Context(), "SELECT current_timezone()", trino.WithTimeZone("UTC")).Scan(&got); err != nil || got != "UTC" {
				t.Errorf("the time zone of the option is %q, %v, want UTC", got, err)
			}
		}},
		{name: "roles", run: func(t *testing.T, e env) {
			e.refused(t, "CREATE ROLE dbimp_role")
		}},
		{name: "trino set session authorization", run: func(t *testing.T, e env) {
			e.refused(t, "SET SESSION AUTHORIZATION alice")
		}},
		{name: "presto set session authorization", run: func(t *testing.T, e env) {
			e.refused(t, "SET SESSION AUTHORIZATION alice")
		}},
		{name: "trino client capabilities", run: func(t *testing.T, e env) {
			// The driver sends PARAMETRIC_DATETIME, so a time keeps nine digits.
			got := e.scalar(t, "SELECT TIME '12:34:56.123456789'")
			if want := (dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}); got != want {
				t.Errorf("the time is %v, want %v", got, want)
			}
			// A fraction with a digit beyond the ninth is an error, and so is a
			// column of nine digits with the digits that fit.
			if err := e.readErr(t, "SELECT TIME '12:34:56.123456789012'"); !errors.Is(err, dbimp.ErrInvalidValue) {
				t.Errorf("a time of twelve digits gave %v, want dbimp.ErrInvalidValue (D175)", err)
			}
			if got := e.scalar(t, "SELECT TIME '12:34:56.123456789000'"); got != (dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}) {
				t.Errorf("a time with zero digits beyond the ninth is %v", got)
			}
		}},
		{name: "presto client capabilities", run: func(t *testing.T, e env) {
			// Presto ignores the capability and keeps three digits of a time.
			res := e.raw(t, rawRequest{statement: "SELECT TIME '12:34:56.123456789012'", headers: map[string]string{"X-Presto-Client-Capabilities": "PARAMETRIC_DATETIME"}})
			if res.errName == "" {
				t.Errorf("a time of twelve digits ran on Presto: %+v", res)
			}
		}},
		{name: "query data encoding", run: func(t *testing.T, e env) {
			res := e.raw(t, rawRequest{statement: "SELECT 1 AS a", headers: map[string]string{e.prefix() + "Query-Data-Encoding": "json+zstd"}})
			if res.errName != "" || len(res.rows) != 1 {
				t.Errorf("a statement with an encoding that the server lacks gave %+v, want plain JSON", res)
			}
		}},
		{name: "trino target result size", run: func(t *testing.T, e env) {
			base := e.pageSizes(t, "")
			with := e.pageSizes(t, "targetResultSize=100kB&maxWait=1s")
			t.Logf("the largest page is %d rows, and %d with targetResultSize", base, with)
			if with < base/2 {
				t.Errorf("targetResultSize made the largest page %d rows from %d, and Trino ignores it", with, base)
			}
		}},
		{name: "presto target result size", run: func(t *testing.T, e env) {
			base := e.pageSizes(t, "")
			with := e.pageSizes(t, "targetResultSize=100kB&maxWait=1s")
			t.Logf("the largest page is %d rows, and %d with targetResultSize", base, with)
			if with >= base {
				t.Errorf("targetResultSize made the largest page %d rows from %d, want smaller pages", with, base)
			}
		}},
		{name: "trino page size header", run: func(t *testing.T, e env) { e.pageSizeHeader(t) }},
		{name: "presto page size header", run: func(t *testing.T, e env) { e.pageSizeHeader(t) }},
		{name: "cancel by next uri", run: func(t *testing.T, e env) {
			mark := marker(t)
			rows, err := e.db.QueryContext(t.Context(), bigQuery(mark))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() {
				t.Fatalf("reading the first row: %v", rows.Err())
			}
			if got := e.queryState(t, mark); got != "RUNNING" {
				t.Fatalf("the query is %q before the close, want RUNNING", got)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			// The server never stops a query that nobody polls (measured), so the
			// state changes only if the driver sent DELETE to the nextUri (D175).
			if got := e.waitState(t, mark, "FAILED"); got != "FAILED" {
				t.Errorf("the query is %q 15 seconds after the rows closed, want FAILED", got)
			}
		}},
		{name: "cancel by query id", run: func(t *testing.T, e env) {
			mark := marker(t)
			rows, err := e.db.QueryContext(t.Context(), bigQuery(mark))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() {
				t.Fatalf("reading the first row: %v", rows.Err())
			}
			id := e.scalar(t, "SELECT query_id FROM system.runtime.queries WHERE starts_with(query, ?)", mark)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, e.base(t)+"/v1/query/"+fmt.Sprint(id), nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set(e.prefix()+"User", "trino")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != http.StatusNoContent {
				t.Errorf("DELETE of the query gave HTTP %d, want 204", res.StatusCode)
			}
			if got := e.waitState(t, mark, "FAILED"); got != "FAILED" {
				t.Errorf("the query is %q after the cancel, want FAILED", got)
			}
		}},
		{name: "gzip", run: func(t *testing.T, e env) {
			res := e.raw(t, rawRequest{statement: "SELECT 1 AS a", headers: map[string]string{"Accept-Encoding": "gzip"}})
			if res.header.Get("Content-Encoding") != "gzip" || len(res.rows) != 1 {
				t.Errorf("a statement with gzip gave %+v, want a gzip answer", res)
			}
			// The driver gets gzip from its transport with no header of its own.
			if got := e.scalar(t, "SELECT n FROM UNNEST(sequence(1, 5000)) AS t(n) WHERE n = 4999"); got != int64(4999) {
				t.Errorf("the answer is %v", got)
			}
		}},
		{name: "trino password over http", run: func(t *testing.T, e env) {
			res := e.raw(t, rawRequest{statement: "SELECT 1", noUser: true, headers: map[string]string{"Authorization": basic("trino", "secret")}})
			if res.status != http.StatusUnauthorized {
				t.Errorf("a password over HTTP gave %+v, want HTTP 401", res)
			}
			db := sql.OpenDB(connect(t, e.dsn, func(c *trino.Config) { c.Password = "secret" }))
			defer db.Close()
			_, err := db.ExecContext(t.Context(), "SELECT 1")
			if perr, ok := errors.AsType[*trino.Error](err); !ok || perr.HTTPStatus != http.StatusUnauthorized {
				t.Errorf("the driver with a password over HTTP gave %v, want HTTP 401", err)
			}
		}},
		{name: "presto password over http", run: func(t *testing.T, e env) {
			// Presto ignores the Authorization header.
			db := sql.OpenDB(connect(t, e.dsn, func(c *trino.Config) { c.Password = "secret" }))
			defer db.Close()
			if err := db.PingContext(t.Context()); err != nil {
				t.Errorf("the ping with a password over HTTP to Presto gave %v, want the password ignored", err)
			}
		}},
		{name: "trino user from basic authentication", run: func(t *testing.T, e env) {
			res := e.raw(t, rawRequest{statement: "SELECT current_user", noUser: true, headers: map[string]string{"Authorization": basic("alice", "")}})
			if res.errName != "" || len(res.rows) != 1 || res.rows[0][0] != "alice" {
				t.Errorf("a statement with the user in basic authentication gave %+v, want alice", res)
			}
		}},
		{name: "presto user from basic authentication", run: func(t *testing.T, e env) {
			res := e.raw(t, rawRequest{statement: "SELECT current_user", noUser: true, headers: map[string]string{"Authorization": basic("alice", "")}})
			if res.status != http.StatusBadRequest {
				t.Errorf("a statement with the user in basic authentication gave %+v, want HTTP 400", res)
			}
		}},
	})
}

// pageSizes sends a statement of 2300 rows of 200 bytes, with query added to each poll,
// and returns the count of rows of its largest page.
func (e env) pageSizes(t *testing.T, query string) int {
	t.Helper()
	res := e.raw(t, rawRequest{statement: e.pagesTable(t), pollQuery: query})
	if res.errName != "" || len(res.rows) != 2300 {
		t.Fatalf("the statement gave %d rows and the error %q", len(res.rows), res.errName)
	}
	largest := 0
	for _, n := range res.pageSizes {
		largest = max(largest, n)
	}
	return largest
}

// pagesTable makes a table of 3000 rows, and returns the statement that reads 2300 rows
// of 200 bytes from it, which the servers send in pages (measured).
func (e env) pagesTable(t *testing.T) string {
	t.Helper()
	tbl := e.table(t, t.Name()+"_pages")
	e.must(t, "CREATE TABLE IF NOT EXISTS "+tbl+" AS SELECT n, 400 - n AS z, rpad('row', 100, 'x') AS s FROM UNNEST(sequence(1, 3000)) AS t(n)")
	return "SELECT n, rpad('x', 200, 'y') AS s FROM " + tbl + " ORDER BY n LIMIT 2300"
}

// pageSizeHeader holds that a header cannot set the size of a page on either flavor.
func (e env) pageSizeHeader(t *testing.T) {
	t.Helper()
	statement := e.pagesTable(t)
	base := e.raw(t, rawRequest{statement: statement})
	with := e.raw(t, rawRequest{statement: statement, headers: map[string]string{e.prefix() + "Max-Size": "1kB", e.prefix() + "Target-Result-Size": "1kB"}})
	if len(base.rows) != 2300 || len(with.rows) != 2300 {
		t.Fatalf("the statements gave %d and %d rows, want 2300", len(base.rows), len(with.rows))
	}
	largest := func(sizes []int) int { return max(0, slicesMax(sizes)) }
	if b, w := largest(base.pageSizes), largest(with.pageSizes); w < b/2 {
		t.Errorf("a header made the largest page %d rows from %d, and the servers ignore it", w, b)
	}
}

// slicesMax returns the largest of sizes, and 0 for none.
func slicesMax(sizes []int) int {
	m := 0
	for _, n := range sizes {
		m = max(m, n)
	}
	return m
}
