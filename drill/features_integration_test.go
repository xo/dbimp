package drill_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/drill"
)

// These tests hold the entries of testdata/drill/features.json (step 14a).
// Each one runs as each principal. The test of an entry that the survey marks
// no sends the operation, and expects the refusal of the server or shows that
// the server ignores it.

// each runs f as the subtest name, and for each principal under it.
func each(t *testing.T, name string, f func(t *testing.T, p principal, db *sql.DB)) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		forEach(t, f)
	})
}

// employees is a query that makes the table of three employees.
const employees = "SELECT employee_id, full_name FROM cp.`employee.json` LIMIT 3"

// wantEmployees are the rows of employees, in the order of employee_id.
var wantEmployees = [][]any{
	{int64(1), "Sheri Nowmer"},
	{int64(2), "Derrick Whelply"},
	{int64(4), "Michael Spence"},
}

// refusal checks that the server refused a statement with a message that holds
// want, and returns the error.
func refusal(t *testing.T, db *sql.DB, query, want string) {
	t.Helper()
	if e := refused(t, db, query); !strings.Contains(e.Message, want) {
		t.Errorf("%s gave the message %q, want one that holds %q", query, e.Message, want)
	}
}

// missing checks that the table does not exist.
func missing(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	err := queryErr(t.Context(), db, "SELECT * FROM dfs.tmp.`"+table+"`")
	if e, ok := errors.AsType[*drill.Error](err); !ok || !strings.Contains(e.Message, "not found") {
		t.Errorf("a read of the table %s gave %v, want an error that says that it was not found", table, err)
	}
}

// TestIntegrationCRUD holds the statements of CRUD. The server runs SELECT and
// CREATE TABLE AS, and refuses INSERT, UPDATE, DELETE and MERGE (D163).
func TestIntegrationCRUD(t *testing.T) {
	each(t, "create table as", func(t *testing.T, p principal, db *sql.DB) {
		// Three tables, as step 14a asks. The region of each nation is the
		// link between two of them, as a foreign key is in another database.
		emp, nation, region := name(p, "emp"), name(p, "nation"), name(p, "region")
		drop(t.Context(), t, db, emp)
		res := exec(t, db, "CREATE TABLE dfs.tmp.`"+emp+"` AS "+employees)
		t.Cleanup(func() { drop(context.WithoutCancel(t.Context()), t, db, emp) })
		if n, err := res.RowsAffected(); err != nil || n != 3 {
			t.Errorf("CREATE TABLE AS wrote %d, %v, want 3", n, err)
		}
		ctas(t, db, nation, "SELECT n_nationkey, n_regionkey FROM cp.`tpch/nation.parquet` WHERE n_nationkey < 5")
		ctas(t, db, region, "SELECT r_regionkey, convert_from(r_name, 'UTF8') AS r_name FROM cp.`tpch/region.parquet`")
		got := all(t, db, "SELECT employee_id, full_name FROM dfs.tmp.`"+emp+"` ORDER BY employee_id")
		if !reflect.DeepEqual(got, wantEmployees) {
			t.Errorf("the table of employees holds %v, want %v", got, wantEmployees)
		}
		joined := all(t, db, "SELECT n.n_nationkey, r.r_name FROM dfs.tmp.`"+nation+"` n JOIN dfs.tmp.`"+region+"` r ON n.n_regionkey = r.r_regionkey ORDER BY n.n_nationkey")
		want := [][]any{{int64(0), "AFRICA"}, {int64(1), "AMERICA"}, {int64(2), "AMERICA"}, {int64(3), "AMERICA"}, {int64(4), "MIDDLE EAST"}}
		if !reflect.DeepEqual(joined, want) {
			t.Errorf("the join of the tables gave %v, want %v", joined, want)
		}
		drop(t.Context(), t, db, emp)
		missing(t, db, emp)
	})
	each(t, "select", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "sel")
		ctas(t, db, table, employees)
		got := all(t, db, "SELECT employee_id, full_name FROM dfs.tmp.`"+table+"` WHERE employee_id > ? ORDER BY employee_id DESC", int64(1))
		want := [][]any{wantEmployees[2], wantEmployees[1]}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the select gave %v, want %v", got, want)
		}
	})
	// The three refusals leave the rows as they were.
	for _, tt := range []struct{ name, query, want string }{
		{"insert", "INSERT INTO dfs.tmp.`%s` (employee_id, full_name) VALUES (9, 'x')", "immutable or doesn't support inserts"},
		// The message of UPDATE and DELETE differs between two servers of one
		// release: the recording holds a ClassCastException, and a server that
		// started later gave "VALIDATION ERROR: null" (measured), so the test
		// checks the refusal and not its words.
		{"update", "UPDATE dfs.tmp.`%s` SET full_name = 'y' WHERE employee_id = 1", ""},
		{"delete", "DELETE FROM dfs.tmp.`%s` WHERE employee_id = 1", ""},
		{"merge", "MERGE INTO dfs.tmp.`%s` t USING dfs.tmp.`%s` s ON t.employee_id = s.employee_id WHEN MATCHED THEN UPDATE SET full_name = 'x'", ""},
	} {
		each(t, tt.name, func(t *testing.T, p principal, db *sql.DB) {
			table := name(p, tt.name)
			ctas(t, db, table, employees)
			q := strings.ReplaceAll(tt.query, "%s", table)
			if tt.name == "insert" {
				refusal(t, db, q, tt.want)
				refusal(t, db, "INSERT INTO dfs.tmp.`"+table+"` (employee_id, full_name) "+employees, tt.want)
			} else {
				e := refused(t, db, q)
				if !strings.Contains(e.Message, tt.want) {
					t.Errorf("%s gave the message %q, want one that holds %q", q, e.Message, tt.want)
				}
			}
			got := all(t, db, "SELECT employee_id, full_name FROM dfs.tmp.`"+table+"` ORDER BY employee_id")
			if !reflect.DeepEqual(got, wantEmployees) {
				t.Errorf("the rows are %v after the refusal, want %v", got, wantEmployees)
			}
		})
	}
}

// TestIntegrationSchema holds the statements that make and change a schema.
func TestIntegrationSchema(t *testing.T) {
	// The server refuses a list of columns and each constraint with the same
	// words (recorded: "schema: create table").
	for _, tt := range []struct{ name, query string }{
		{"create table with columns", "CREATE TABLE dfs.tmp.`%s` (id INT)"},
		{"primary key", "CREATE TABLE dfs.tmp.`%s` (id INT PRIMARY KEY)"},
		{"foreign key", "CREATE TABLE dfs.tmp.`%s` (id INT REFERENCES dfs.tmp.`%s` (id))"},
		{"unique constraint", "CREATE TABLE dfs.tmp.`%s` (id INT UNIQUE)"},
	} {
		each(t, tt.name, func(t *testing.T, p principal, db *sql.DB) {
			table := name(p, "cols")
			refusal(t, db, strings.ReplaceAll(tt.query, "%s", table), "Extended columns not allowed")
			missing(t, db, table)
		})
	}
	each(t, "index", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "idx")
		ctas(t, db, table, employees)
		refusal(t, db, "CREATE INDEX "+name(p, "i")+" ON dfs.tmp.`"+table+"` (employee_id)", "Encountered")
	})
	each(t, "view", func(t *testing.T, p principal, db *sql.DB) {
		view := name(p, "view")
		t.Cleanup(func() {
			if _, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP VIEW IF EXISTS dfs.tmp.`"+view+"`"); err != nil {
				t.Errorf("dropping the view: %v", err)
			}
		})
		exec(t, db, "CREATE OR REPLACE VIEW dfs.tmp.`"+view+"` AS "+employees)
		got := all(t, db, "SELECT * FROM dfs.tmp.`"+view+"` ORDER BY employee_id")
		if !reflect.DeepEqual(got, wantEmployees) {
			t.Errorf("the view holds %v, want %v", got, wantEmployees)
		}
		exec(t, db, "DROP VIEW dfs.tmp.`"+view+"`")
		missing(t, db, view)
	})
	each(t, "temporary table", func(t *testing.T, p principal, db *sql.DB) {
		// A temporary table lives as long as the session, and the driver keeps
		// one session for each connection (D165). Another connection has its
		// own session, which does not see the table (recorded: "schema:
		// temporary table read in the next request").
		ctx := t.Context()
		c1, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c1.Close() })
		c2, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c2.Close() })
		// The table is in the temporary workspace, dfs.tmp, and a plain name
		// finds it only with that schema (measured).
		tmp := name(p, "tmp")
		if _, err := c1.ExecContext(ctx, "CREATE TEMPORARY TABLE "+tmp+" AS SELECT 1 AS a FROM (VALUES(1))"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := c1.ExecContext(context.WithoutCancel(ctx), "DROP TABLE dfs.tmp."+tmp); err != nil {
				t.Errorf("dropping the temporary table: %v", err)
			}
		})
		if got := all(t, c1, "SELECT a FROM dfs.tmp."+tmp); !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
			t.Errorf("the connection that made the table reads %v, want 1", got)
		}
		rows, err := c2.QueryContext(ctx, "SELECT a FROM dfs.tmp."+tmp)
		if err == nil {
			defer rows.Close()
			err = rows.Err()
		}
		if e, ok := errors.AsType[*drill.Error](err); !ok || !strings.Contains(e.Message, "not found") {
			t.Errorf("another connection read the temporary table: %v, want an error that says that it was not found", err)
		}
	})
	each(t, "drop table", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "drop")
		ctas(t, db, table, employees)
		got := all(t, db, "DROP TABLE dfs.tmp.`"+table+"`")
		if summary, _ := got[0][1].(string); len(got) != 1 || got[0][0] != true || !strings.Contains(summary, "dropped") {
			t.Errorf("DROP TABLE gave %v, want ok and a summary that says dropped", got)
		}
		missing(t, db, table)
	})
	each(t, "drop table if exists", func(t *testing.T, p principal, db *sql.DB) {
		got := all(t, db, "DROP TABLE IF EXISTS dfs.tmp.`"+name(p, "nope")+"`")
		if summary, _ := got[0][1].(string); len(got) != 1 || got[0][0] != false || !strings.Contains(summary, "not found") {
			t.Errorf("DROP TABLE IF EXISTS gave %v, want ok false and a summary that says not found", got)
		}
	})
	each(t, "create schema for a table", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "schema")
		ctas(t, db, table, "SELECT employee_id FROM cp.`employee.json` LIMIT 2", jsonFormat)
		got := all(t, db, "CREATE OR REPLACE SCHEMA (employee_id BIGINT) FOR TABLE dfs.tmp.`"+table+"`")
		if len(got) != 1 || got[0][0] != true {
			t.Errorf("CREATE SCHEMA gave %v, want ok true", got)
		}
		rows := all(t, db, "SELECT employee_id FROM dfs.tmp.`"+table+"` ORDER BY employee_id")
		if !reflect.DeepEqual(rows, [][]any{{int64(1)}, {int64(2)}}) {
			t.Errorf("the table with a schema holds %v, want 1 and 2", rows)
		}
		exec(t, db, "DROP SCHEMA FOR TABLE dfs.tmp.`"+table+"`")
	})
	each(t, "default value", func(t *testing.T, p principal, db *sql.DB) {
		// A column that the files lack still reads as NULL, with a default
		// value in the schema (recorded: "schema: default value read").
		table := name(p, "default")
		ctas(t, db, table, "SELECT employee_id FROM cp.`employee.json` LIMIT 2", jsonFormat)
		exec(t, db, "CREATE OR REPLACE SCHEMA (employee_id BIGINT, extra VARCHAR NOT NULL DEFAULT 'z') FOR TABLE dfs.tmp.`"+table+"`")
		rows := all(t, db, "SELECT employee_id, extra FROM dfs.tmp.`"+table+"` ORDER BY employee_id")
		if !reflect.DeepEqual(rows, [][]any{{int64(1), nil}, {int64(2), nil}}) {
			t.Errorf("the column with a default value reads %v, want NULL for each row", rows)
		}
	})
	each(t, "partition by", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "part")
		const source = "SELECT employee_id, store_id FROM cp.`employee.json` WHERE store_id < 3"
		drop(t.Context(), t, db, table)
		res := exec(t, db, "CREATE TABLE dfs.tmp.`"+table+"` PARTITION BY (store_id) AS "+source)
		t.Cleanup(func() { drop(context.WithoutCancel(t.Context()), t, db, table) })
		n, err := res.RowsAffected()
		count := all(t, db, "SELECT COUNT(*) AS n FROM dfs.tmp.`"+table+"`")
		want := all(t, db, "SELECT COUNT(*) AS n FROM ("+source+")")
		if err != nil || n == 0 || !reflect.DeepEqual(count, want) || count[0][0] != n {
			t.Errorf("the partitioned table holds %v rows after it wrote %d, %v, want %v", count, n, err, want)
		}
	})
}

// jsonFormat makes CREATE TABLE AS write JSON files, in which a column can
// change its type between two files.
var jsonFormat = drill.WithParameter("options", map[string]string{"store.format": "json", "drill.exec.http.rest.errors.verbose": "true"})

// complexValues makes the table of complex values: a map, a list of numbers, a
// list of lists, a list of maps and an empty list.
const complexValues = "SELECT 1 AS id, convert_from('{\"k\":[1,2,3],\"o\":{\"s\":\"x\"}}', 'JSON') AS m, convert_from('[1,2,3]', 'JSON') AS l, convert_from('[[1,2],[3]]', 'JSON') AS ll, convert_from('[{\"a\":1},{\"a\":2}]', 'JSON') AS lm, convert_from('[]', 'JSON') AS le FROM (VALUES(1))"

// TestIntegrationFeatures holds the features of Drill that features.json
// lists.
func TestIntegrationFeatures(t *testing.T) {
	each(t, "a file by its path", func(t *testing.T, _ principal, db *sql.DB) {
		got := all(t, db, "SELECT employee_id FROM cp.`employee.json` LIMIT 1")
		if !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
			t.Errorf("the file gave %v, want 1", got)
		}
	})
	each(t, "implicit columns", func(t *testing.T, p principal, db *sql.DB) {
		dir := name(p, "multi")
		ctas(t, db, dir+"/a", "SELECT employee_id AS id FROM cp.`employee.json` LIMIT 2")
		ctas(t, db, dir+"/b", "SELECT employee_id + 10000 AS id FROM cp.`employee.json` LIMIT 2")
		got := all(t, db, "SELECT id, dir0, filename, fqn FROM dfs.tmp.`"+dir+"` ORDER BY id")
		if len(got) != 4 || got[0][1] != "a" || got[3][1] != "b" || !strings.HasSuffix(fmt.Sprint(got[0][3]), "/"+dir+"/a/0_0_0.parquet") {
			t.Errorf("the implicit columns are %v, want the directory a for the first row and b for the last", got)
		}
	})
	each(t, "flatten", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "complex")
		ctas(t, db, table, complexValues)
		got := all(t, db, "SELECT FLATTEN(l) AS f FROM dfs.tmp.`"+table+"`")
		if !reflect.DeepEqual(got, [][]any{{int64(1)}, {int64(2)}, {int64(3)}}) {
			t.Errorf("FLATTEN gave %v, want 1, 2 and 3", got)
		}
	})
	each(t, "kvgen", func(t *testing.T, _ principal, db *sql.DB) {
		got := all(t, db, `SELECT KVGEN(m) AS k FROM (SELECT convert_from('{"a":1,"b":2}', 'JSON') AS m FROM (VALUES(1)))`)
		want := [][]any{{[]any{
			map[string]any{"key": "a", "value": int64(1)},
			map[string]any{"key": "b", "value": int64(2)},
		}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("KVGEN gave %v, want %v", got, want)
		}
	})
	each(t, "a member of a map and a list", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "complex")
		ctas(t, db, table, complexValues)
		got := all(t, db, "SELECT t.m.o.s AS s, t.l[1] AS l1, t.lm[0].a AS a FROM dfs.tmp.`"+table+"` t")
		if !reflect.DeepEqual(got, [][]any{{"x", int64(2), int64(1)}}) {
			t.Errorf("the members are %v, want x, 2 and 1", got)
		}
	})
	each(t, "repeated_count", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "complex")
		ctas(t, db, table, complexValues)
		got := all(t, db, "SELECT REPEATED_COUNT(l) AS n FROM dfs.tmp.`"+table+"`")
		if !reflect.DeepEqual(got, [][]any{{int64(3)}}) {
			t.Errorf("REPEATED_COUNT gave %v, want 3", got)
		}
	})
	each(t, "show schemas", func(t *testing.T, _ principal, db *sql.DB) {
		got := all(t, db, "SHOW SCHEMAS")
		found := false
		for _, row := range got {
			found = found || row[0] == "dfs.tmp"
		}
		if !found {
			t.Errorf("SHOW SCHEMAS gave %v, want a row for dfs.tmp", got)
		}
	})
	each(t, "show files", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "files")
		ctas(t, db, table, employees)
		left, err := tables(t.Context(), db)
		if err != nil || !slices.Contains(left, table) {
			t.Errorf("SHOW FILES gave %v, %v, want a row for %s", left, err, table)
		}
	})
	each(t, "use", func(t *testing.T, p principal, db *sql.DB) {
		// USE holds for the session, and so for the connection (D165).
		table := name(p, "use")
		ctas(t, db, table, employees)
		c, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		got := all(t, c, "USE dfs.tmp")
		if len(got) != 1 || got[0][0] != true || !strings.Contains(fmt.Sprint(got[0][1]), "dfs.tmp") {
			t.Errorf("USE gave %v, want ok and the schema", got)
		}
		if n := all(t, c, "SELECT COUNT(*) FROM `"+table+"`"); n[0][0] != int64(3) {
			t.Errorf("a table with no schema read %v after USE, want 3", n)
		}
	})
	each(t, "describe", func(t *testing.T, p principal, db *sql.DB) {
		// DESCRIBE of a Parquet table gives its columns and no rows (recorded:
		// "feature: describe").
		table := name(p, "describe")
		ctas(t, db, table, employees)
		rows, err := db.QueryContext(t.Context(), "DESCRIBE dfs.tmp.`"+table+"`")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		if !reflect.DeepEqual(cols, []string{"COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE"}) || rows.Next() || rows.Err() != nil {
			t.Errorf("DESCRIBE gave the columns %v, and a row or %v, want the columns and no row", cols, rows.Err())
		}
	})
	each(t, "explain", func(t *testing.T, _ principal, db *sql.DB) {
		got := all(t, db, "EXPLAIN PLAN FOR SELECT employee_id FROM cp.`employee.json` LIMIT 1")
		if len(got) != 1 || !strings.Contains(fmt.Sprint(got[0][0]), "Scan(table=[[cp, employee.json]]") {
			t.Errorf("EXPLAIN gave %v, want a plan that scans employee.json", got)
		}
	})
	each(t, "a table function", func(t *testing.T, _ principal, db *sql.DB) {
		got := all(t, db, "SELECT employee_id FROM table(cp.`employee.json`(type => 'json')) LIMIT 1")
		if !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
			t.Errorf("the table function gave %v, want 1", got)
		}
	})
	each(t, "alter session", func(t *testing.T, _ principal, db *sql.DB) {
		// The session option holds for the connection, and not for another
		// connection (D165).
		ctx := t.Context()
		c1, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c1.Close() })
		c2, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c2.Close() })
		const five = "SELECT employee_id FROM cp.`employee.json` LIMIT 5"
		if _, err := c1.ExecContext(ctx, "ALTER SESSION SET `exec.query.max_rows` = 2"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := c1.ExecContext(context.WithoutCancel(ctx), "ALTER SESSION RESET `exec.query.max_rows`"); err != nil {
				t.Errorf("ALTER SESSION RESET: %v", err)
			}
		})
		if n := len(all(t, c1, five)); n != 2 {
			t.Errorf("the connection that set the option read %d rows, want 2", n)
		}
		if n := len(all(t, c2, five)); n != 5 {
			t.Errorf("another connection read %d rows, want 5", n)
		}
	})
	each(t, "a session between requests", func(t *testing.T, p principal, _ *sql.DB) {
		// Without the cookie, each request starts a session, so the setting of
		// one request does not reach the next (recorded: "feature: a session
		// option in the next request").
		r := newRaw(t, p)
		if a := r.query(t, "ALTER SESSION SET `exec.query.max_rows` = 2"); a.State != "COMPLETED" {
			t.Fatalf("ALTER SESSION gave %+v", a)
		}
		if a := r.query(t, "SELECT employee_id FROM cp.`employee.json` LIMIT 5"); len(a.Rows) != 5 {
			t.Errorf("the next request read %d rows, want 5, because the session ended", len(a.Rows))
		}
	})
	each(t, "analyze table", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "analyze")
		ctas(t, db, table, employees)
		got := all(t, db, "ANALYZE TABLE dfs.tmp.`"+table+"` COMPUTE STATISTICS")
		if len(got) != 1 {
			t.Errorf("ANALYZE TABLE gave %v, want one row", got)
		}
	})
	each(t, "refresh table metadata", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "refresh")
		ctas(t, db, table, employees)
		got := all(t, db, "REFRESH TABLE METADATA dfs.tmp.`"+table+"`")
		if len(got) != 1 || got[0][0] != true {
			t.Errorf("REFRESH TABLE METADATA gave %v, want ok true", got)
		}
	})
	each(t, "sys tables", func(t *testing.T, _ principal, db *sql.DB) {
		got := all(t, db, "SELECT name, val FROM sys.options WHERE name = 'exec.query.max_rows'")
		if !reflect.DeepEqual(got, [][]any{{"exec.query.max_rows", "0"}}) {
			t.Errorf("sys.options gave %v, want the default 0", got)
		}
	})
	each(t, "information_schema", func(t *testing.T, _ principal, db *sql.DB) {
		got := all(t, db, "SELECT SCHEMA_NAME, IS_MUTABLE FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME IN ('dfs.tmp', 'cp.default') ORDER BY SCHEMA_NAME")
		if !reflect.DeepEqual(got, [][]any{{"cp.default", "NO"}, {"dfs.tmp", "YES"}}) {
			t.Errorf("INFORMATION_SCHEMA.SCHEMATA gave %v, want dfs.tmp as the one that can be written", got)
		}
	})
	each(t, "convert_from and convert_to", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "complex")
		ctas(t, db, table, complexValues)
		got := all(t, db, "SELECT convert_from(convert_to(m, 'JSON'), 'UTF8') AS j FROM dfs.tmp.`"+table+"`")
		if len(got) != 1 {
			t.Fatalf("convert_to gave %v, want one row", got)
		}
		var back map[string]any
		text, _ := got[0][0].(string)
		if err := json.Unmarshal([]byte(text), &back); err != nil || !reflect.DeepEqual(back, map[string]any{"k": []any{1.0, 2.0, 3.0}, "o": map[string]any{"s": "x"}}) {
			t.Errorf("the map came back as %v, %v", got[0][0], err)
		}
	})
	each(t, "autoLimit", func(t *testing.T, p principal, _ *sql.DB) {
		// The server keeps the limit in the session, so each case has a
		// database of its own, which has its own sessions (open question 9 of
		// docs/DRILL.md).
		const q = "SELECT employee_id FROM cp.`employee.json`"
		if n := len(all(t, openAs(t, p, ""), q, drill.WithAutoLimit(5))); n != 5 {
			t.Errorf("a limit of 5 gave %d rows, want 5", n)
		}
		if n := len(all(t, openAs(t, p, "?autolimit=7"), q)); n != 7 {
			t.Errorf("a DSN with the limit 7 gave %d rows, want 7", n)
		}
		if n := len(all(t, openAs(t, p, ""), q)); n != 1155 {
			t.Errorf("a query with no limit gave %d rows, want all 1155", n)
		}
	})
	each(t, "defaultSchema", func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "schema")
		ctas(t, db, table, employees)
		q := "SELECT COUNT(*) FROM `" + table + "`"
		// The server keeps the schema in the session, so each case has a
		// database of its own (open question 9 of docs/DRILL.md).
		if got := all(t, openAs(t, p, ""), q, drill.WithDatabase("dfs.tmp")); got[0][0] != int64(3) {
			t.Errorf("a table with the schema of an option gave %v, want 3", got)
		}
		if got := all(t, openAs(t, p, "?schema=dfs.tmp"), q); got[0][0] != int64(3) {
			t.Errorf("a table with the schema of the DSN gave %v, want 3", got)
		}
		if err := queryErr(t.Context(), openAs(t, p, ""), q, drill.WithSchema("nope")); err == nil {
			t.Error("a schema that does not exist gave no error")
		}
	})
	each(t, "options in the body", func(t *testing.T, p principal, _ *sql.DB) {
		// The server keeps a session option of a request for the session, so
		// it stays for the connection that sent it, and another connection
		// does not have it (open question 9 of docs/DRILL.md).
		const q = "SELECT employee_id FROM cp.`employee.json`"
		db := openAs(t, p, "")
		c1, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c1.Close() })
		c2, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c2.Close() })
		if n := len(all(t, c1, q, drill.WithParameter("options", map[string]string{"exec.query.max_rows": "7"}))); n != 7 {
			t.Errorf("exec.query.max_rows of 7 gave %d rows, want 7", n)
		}
		if n := len(all(t, c1, q)); n != 7 {
			t.Errorf("the next statement of the connection read %d rows, want 7, because the session keeps the option", n)
		}
		if n := len(all(t, c2, q)); n != 1155 {
			t.Errorf("another connection read %d rows, want all 1155", n)
		}
		t.Cleanup(func() {
			if _, err := c1.ExecContext(context.WithoutCancel(t.Context()), "ALTER SESSION RESET ALL"); err != nil {
				t.Errorf("ALTER SESSION RESET ALL: %v", err)
			}
		})
	})
	each(t, "userName for impersonation", func(t *testing.T, p principal, db *sql.DB) {
		// Impersonation is off in the image, so the request fails with no
		// reason (recorded: "a user to impersonate").
		err := queryErr(t.Context(), db, "SELECT 1 AS one FROM (VALUES(1))", drill.WithParameter("userName", "dbmeta_user"))
		if e, ok := errors.AsType[*drill.Error](err); !ok || e.HTTPStatus != http.StatusInternalServerError || e.Message != "Query submission failed" {
			t.Errorf("a user to impersonate gave %v, want HTTP 500 and Query submission failed", err)
		}
	})
	each(t, "a type that changes between files", func(t *testing.T, p principal, db *sql.DB) {
		// The second file holds a string where the first holds a number. The
		// server gives NULL for it, with no sign (D165), and typeof shows it.
		dir := name(p, "change")
		ctas(t, db, dir+"/a", "SELECT CAST(1 AS BIGINT) AS x FROM (VALUES(1))", jsonFormat)
		ctas(t, db, dir+"/b", "SELECT 'text' AS x FROM (VALUES(1))", jsonFormat)
		// ORDER BY fails for a column that changes its type, so the order of
		// the rows is the order of the files, and the test sorts them.
		got := all(t, db, "SELECT x, typeof(x) AS t FROM dfs.tmp.`"+dir+"`")
		slices.SortFunc(got, func(a, b []any) int { return strings.Compare(fmt.Sprint(a[1]), fmt.Sprint(b[1])) })
		want := [][]any{{int64(1), "BIGINT"}, {nil, "VARCHAR"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the rows are %v, want %v: NULL for the string, which typeof names", got, want)
		}
	})
	each(t, "the profile of a query", func(t *testing.T, p principal, _ *sql.DB) {
		r := newRaw(t, p)
		a := r.query(t, "SELECT employee_id FROM cp.`employee.json` LIMIT 1")
		status, body := r.do(t, http.MethodGet, "/profiles/"+a.QueryID+".json", nil)
		var prof struct {
			Query string `json:"query"`
			User  string `json:"user"`
		}
		if err := json.Unmarshal(body, &prof); err != nil || status != http.StatusOK || !strings.Contains(prof.Query, "employee.json") || prof.User == "" {
			t.Errorf("the profile gave HTTP %d, %+v, %v, want the query and its user", status, prof, err)
		}
		// A profile that does not exist gives HTTP 500 and text that is not
		// JSON (recorded: "a profile that does not exist").
		if status, _ := r.do(t, http.MethodGet, "/profiles/00000000-0000-0000-0000-000000000000.json", nil); status != http.StatusInternalServerError {
			t.Errorf("a profile that does not exist gave HTTP %d, want 500", status)
		}
	})
	each(t, "cancel by the query id", func(t *testing.T, p principal, db *sql.DB) {
		testCancel(t, p, db)
	})
	each(t, "placeholders", func(t *testing.T, p principal, db *sql.DB) {
		// The server binds no argument (recorded: "a placeholder"), so the
		// driver writes each one as a literal (D165).
		a := newRaw(t, p).query(t, "SELECT ? AS p FROM (VALUES(1))")
		if a.State != "FAILED" || !strings.Contains(a.Message, "Illegal use of dynamic parameter") {
			t.Errorf("the server gave %+v for a placeholder, want FAILED and Illegal use of dynamic parameter", a)
		}
		got := all(t, db, "SELECT ? AS p, ? AS q FROM (VALUES(1))", "it's", int64(7))
		if !reflect.DeepEqual(got, [][]any{{"it's", int64(7)}}) {
			t.Errorf("the arguments came back as %v, want the string and 7", got)
		}
	})
	each(t, "transactions", func(t *testing.T, p principal, db *sql.DB) {
		if tx, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			if err == nil {
				_ = tx.Rollback()
			}
			t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
		}
		if a := newRaw(t, p).query(t, "START TRANSACTION"); a.State != "FAILED" || !strings.Contains(a.Message, "Non-query expression") {
			t.Errorf("the server gave %+v for START TRANSACTION, want FAILED", a)
		}
	})
}

// running returns the ids of the queries that the server runs, whose text holds
// marker. The server answers HTTP 500 now and then while it lists the profiles
// of the queries that end, so the caller reads again (measured).
func (r *raw) running(t *testing.T, marker string) ([]string, bool) {
	t.Helper()
	status, body := r.do(t, http.MethodGet, "/profiles.json", nil)
	if status == http.StatusInternalServerError {
		return nil, false
	}
	var list struct {
		Running []struct {
			QueryID string `json:"queryId"`
			Query   string `json:"query"`
		} `json:"runningQueries"`
	}
	if err := json.Unmarshal(body, &list); err != nil || status != http.StatusOK {
		t.Fatalf("GET /profiles.json gave HTTP %d, %v and %s", status, err, body)
	}
	ids := []string{}
	for _, q := range list.Running {
		if strings.Contains(q.Query, marker) {
			ids = append(ids, q.QueryID)
		}
	}
	return ids, true
}

// waitUntil polls cond until it holds or the time passes.
func waitUntil(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(limit); !cond(); {
		if !time.Now().Before(deadline) {
			t.Fatalf("%s did not happen within %v", what, limit)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// testCancel holds that the query id cancels a query, in two ways. The server
// cancels the query that a client names by its id, and the answer of that
// query then ends with CANCELED (recorded: "cancel the running query"). The
// driver sends that cancel itself when the context of a query ends, and the
// server then stops the query (D165).
func testCancel(t *testing.T, p principal, db *sql.DB) {
	t.Helper()
	r := newRaw(t, p)
	// A query that counts a cross join takes a long time, and sends nothing
	// until it ends, so the id comes from the list of running queries.
	marker := "dbimp_cancel_" + suffix + "_" + p.name
	long := "SELECT COUNT(*) AS " + marker + " FROM cp.`employee.json` a, cp.`employee.json` b, cp.`tpch/nation.parquet` c, cp.`tpch/region.parquet` d, cp.`tpch/region.parquet` e WHERE a.employee_id + b.employee_id + c.n_nationkey + d.r_regionkey + e.r_regionkey > 0"
	done := make(chan reply, 1)
	go func() {
		ctx := context.WithoutCancel(t.Context())
		body, _ := json.Marshal(map[string]any{"queryType": "SQL", "query": long, "options": map[string]string{"planner.enable_nljoin_for_scalar_only": "false"}})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+"/query.json", strings.NewReader(string(body)))
		if err != nil {
			done <- reply{}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(r.user, r.pass)
		res, err := r.client.Do(req)
		if err != nil {
			done <- reply{}
			return
		}
		defer res.Body.Close()
		var a reply
		_ = json.UnmarshalRead(res.Body, &a)
		done <- a
	}()
	var ids []string
	waitUntil(t, 30*time.Second, "the long query starting", func() bool {
		var ok bool
		ids, ok = r.running(t, marker)
		return ok && len(ids) == 1
	})
	status, body := r.do(t, http.MethodGet, "/profiles/cancel/"+ids[0], nil)
	if status != http.StatusOK || !strings.Contains(string(body), "Cancelled query "+ids[0]) {
		t.Errorf("the cancel gave HTTP %d and %q", status, body)
	}
	select {
	case a := <-done:
		if a.State != "CANCELED" || len(a.Rows) != 0 {
			t.Errorf("the answer of the cancelled query is %+v, want CANCELED and no rows", a)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the answer of the cancelled query did not come in 60 seconds")
	}
	// A query that sends its first rows at once, and that the client leaves.
	// The driver cancels it when the context ends, and the server stops it.
	stream := "SELECT a.employee_id AS " + marker + ", b.employee_id AS y FROM cp.`employee.json` a, cp.`employee.json` b, cp.`employee.json` c"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := db.QueryContext(ctx, stream, drill.WithParameter("options", map[string]string{"planner.enable_nljoin_for_scalar_only": "false"}))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reading the first row: %v", rows.Err())
	}
	cancel()
	for rows.Next() {
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the error of the rows is %v, want context.Canceled", err)
	}
	if err := rows.Close(); err != nil {
		t.Errorf("closing the rows: %v", err)
	}
	waitUntil(t, 60*time.Second, "the query leaving the running queries", func() bool {
		ids, ok := r.running(t, marker)
		return ok && len(ids) == 0
	})
}
