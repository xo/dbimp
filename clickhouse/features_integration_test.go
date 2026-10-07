package clickhouse_test

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/clickhouse"
)

// The tests in this file are the tests that features.json names (step 14a). Each
// one runs as the administrator and as the ordinary user, and says so when a
// privilege decides the answer.

// crudTables makes the tables of the tests of CRUD: a parent, and a child that
// refers to it by the column parent_id, with an index on label that a query uses.
// The server has no foreign key, so the child refers to the parent by value only.
func crudTables(t *testing.T, e env) (string, string) {
	t.Helper()
	parent := e.table(t, "parent", "CREATE TABLE %s (id UInt32, name String, v Int64, ts DateTime('UTC')) ENGINE = MergeTree ORDER BY id")
	child := e.table(t, "child", "CREATE TABLE %s (id UInt32, parent_id UInt32, label String, INDEX idx_label label TYPE bloom_filter GRANULARITY 1) ENGINE = MergeTree ORDER BY id")
	return parent, child
}

// at is a time of the tests, in UTC.
func at(sec int) time.Time {
	return time.Date(2026, 10, 7, 12, 0, sec, 0, time.UTC)
}

// seed inserts three parents and three children, with bound arguments.
func seed(t *testing.T, e env, parent, child string) {
	t.Helper()
	for i, name := range []string{"one", "two", "three"} {
		e.exec(t, "INSERT INTO "+parent+" (id, name, v, ts) VALUES (?, ?, ?, ?)", int64(i+1), name, int64(10*(i+1)), at(i))
		e.exec(t, "INSERT INTO "+child+" (id, parent_id, label) VALUES (?, ?, ?)", int64(100+i), int64(i+1), "child of "+name)
	}
}

// TestIntegrationCRUD tests insert, select, update and delete on three tables, and
// the forms that ClickHouse has in place of the ones that it lacks.
func TestIntegrationCRUD(t *testing.T) {
	step := func(t *testing.T, name string, f func(t *testing.T, e env, parent, child string)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			eachPrincipal(t, func(t *testing.T, e env) {
				parent, child := crudTables(t, e)
				f(t, e, parent, child)
			})
		})
	}
	sync2 := clickhouse.WithParameter("mutations_sync", 2)
	rowsOf := func(t *testing.T, e env, query string, args ...any) [][]any {
		t.Helper()
		return e.rows(t, query, args...)
	}
	step(t, "insert", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		res, err := e.db.ExecContext(t.Context(), "INSERT INTO "+parent+" (id, name, v, ts) VALUES (4, 'four', 40, '2026-10-07 12:00:03')")
		if err != nil {
			t.Fatal(err)
		}
		// The server counts no row of a statement for the driver (D176).
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("RowsAffected gave %v, want dbimp.ErrNotSupported", err)
		}
		if got := e.count(t, "SELECT count() FROM "+parent); got != 4 {
			t.Errorf("the parent has %d rows, want 4", got)
		}
		if got := e.count(t, "SELECT count() FROM "+child); got != 3 {
			t.Errorf("the child has %d rows, want 3", got)
		}
		// A NULL for a column that is not Nullable is refused, and a missing value
		// is the default of its type (hard rule 3 and measured).
		e.exec(t, "INSERT INTO "+parent+" (id) VALUES (5)")
		want := []any{int64(5), "", int64(0), at(0).Add(-12 * time.Hour)}
		want[3] = time.Unix(0, 0).UTC()
		if got := rowsOf(t, e, "SELECT id, name, v, ts FROM "+parent+" WHERE id = 5")[0]; !equalValue(got, want) {
			t.Errorf("the row with the defaults is %#v, want %#v", got, want)
		}
	})
	step(t, "select", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		got := rowsOf(t, e, "SELECT id, name, v, ts FROM "+parent+" ORDER BY id")
		want := [][]any{{int64(1), "one", int64(10), at(0)}, {int64(2), "two", int64(20), at(1)}, {int64(3), "three", int64(30), at(2)}}
		if !equalValue(rowsAsAny(got), rowsAsAny(want)) {
			t.Errorf("the rows are %#v, want %#v", got, want)
		}
		if got := rowsOf(t, e, "SELECT name FROM "+parent+" WHERE v >= ? AND name != ? ORDER BY id DESC LIMIT ?", int64(20), "two", int64(5)); !reflect.DeepEqual(got, [][]any{{"three"}}) {
			t.Errorf("the select with a where is %#v", got)
		}
		join := rowsOf(t, e, "SELECT p.name, c.label FROM "+child+" AS c JOIN "+parent+" AS p ON p.id = c.parent_id WHERE c.id = ?", int64(101))
		if !reflect.DeepEqual(join, [][]any{{"two", "child of two"}}) {
			t.Errorf("the join is %#v", join)
		}
		if got := e.count(t, "SELECT count() FROM "+child+" WHERE label = ?", "child of one"); got != 1 {
			t.Errorf("the index of the child found %d rows, want 1", got)
		}
	})
	step(t, "update", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		// The statement of the SQL standard goes to the server as it is (D176). A
		// table with no column _block_number refuses it, and 25.3 does not have it.
		cerr := e.refusal(t, "UPDATE "+parent+" SET name = ? WHERE id = ?", "uno", int64(1))
		wantCode := 48
		if e.is253() {
			wantCode = 62
		}
		if cerr.Code != wantCode {
			t.Errorf("UPDATE ... SET on a table with no block number column gave %+v, want the code %d", cerr, wantCode)
		}
		// The form that works on every release is a mutation, which the setting
		// mutations_sync makes the request wait for.
		e.exec(t, "ALTER TABLE "+parent+" UPDATE name = ?, v = v + ? WHERE id = ?", sync2, "uno", int64(1), int64(1))
		got := rowsOf(t, e, "SELECT id, name, v FROM "+parent+" WHERE id = 1")
		if !reflect.DeepEqual(got, [][]any{{int64(1), "uno", int64(11)}}) {
			t.Errorf("the updated row is %#v", got)
		}
		if got := rowsOf(t, e, "SELECT name FROM "+parent+" WHERE id = 2"); !reflect.DeepEqual(got, [][]any{{"two"}}) {
			t.Errorf("the row that no update touched is %#v", got)
		}
	})
	step(t, "delete", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		e.exec(t, "DELETE FROM "+parent+" WHERE id = ?", int64(2))
		if got := rowsOf(t, e, "SELECT id FROM "+parent+" ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(1)}, {int64(3)}}) {
			t.Errorf("the rows after the delete are %#v", got)
		}
		// A delete with no WHERE is a syntax error, and TRUNCATE empties the table.
		if cerr := e.refusal(t, "DELETE FROM "+parent); cerr.Code != 62 {
			t.Errorf("DELETE with no WHERE gave %+v, want the code 62", cerr)
		}
		if got := e.count(t, "SELECT count() FROM "+parent); got != 2 {
			t.Errorf("the table has %d rows, want 2", got)
		}
	})
	step(t, "upsert", func(t *testing.T, e env, parent, child string) {
		for _, q := range []string{
			"INSERT INTO " + parent + " (id, name, v) VALUES (1, 'a', 1) ON DUPLICATE KEY UPDATE v = 2",
			"REPLACE INTO " + parent + " (id, name, v) VALUES (1, 'a', 1)",
			"INSERT INTO " + parent + " (id, name, v) VALUES (30, 'a', 1) RETURNING id",
		} {
			// The insert reads ON DUPLICATE KEY as the start of a row of values, which is
			// the code 27, and the other two are syntax errors, the code 62.
			if cerr := e.refusal(t, q); cerr.Code != 62 && cerr.Code != 27 {
				t.Errorf("%q gave %+v, want the code 62 or 27", q, cerr)
			}
		}
		// A primary key is a sort key, so two rows with one key are both stored.
		e.exec(t, "INSERT INTO "+parent+" (id, name, v) VALUES (1, 'a', 1), (1, 'b', 2)")
		if got := e.count(t, "SELECT count() FROM "+parent+" WHERE id = 1"); got != 2 {
			t.Errorf("the table holds %d rows of the key 1, want 2", got)
		}
	})
	step(t, "merge", func(t *testing.T, e env, parent, child string) {
		for _, q := range []string{
			"MERGE INTO " + parent + " USING " + child + " ON 1 WHEN MATCHED THEN DELETE",
			"SELECT id FROM " + parent + " FOR UPDATE",
		} {
			if cerr := e.refusal(t, q); cerr.Code != 62 {
				t.Errorf("%q gave %+v, want the code 62", q, cerr)
			}
		}
	})
	step(t, "insert select", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		e.exec(t, "INSERT INTO "+child+" (id, parent_id, label) SELECT number + 10, ?, concat('gen', toString(number)) FROM numbers(3)", int64(2))
		got := rowsOf(t, e, "SELECT id, parent_id, label FROM "+child+" WHERE id BETWEEN 10 AND 12 ORDER BY id")
		want := [][]any{{int64(10), int64(2), "gen0"}, {int64(11), int64(2), "gen1"}, {int64(12), int64(2), "gen2"}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("the rows are %#v, want %#v", got, want)
		}
	})
	step(t, "alter table update", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		e.exec(t, "ALTER TABLE "+parent+" UPDATE name = upper(name) WHERE v >= ?", sync2, int64(20))
		got := rowsOf(t, e, "SELECT name FROM "+parent+" ORDER BY id")
		if !reflect.DeepEqual(got, [][]any{{"one"}, {"TWO"}, {"THREE"}}) {
			t.Errorf("the names are %#v", got)
		}
	})
	step(t, "alter table delete", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		e.exec(t, "ALTER TABLE "+parent+" DELETE WHERE id = ?", sync2, int64(2))
		if got := rowsOf(t, e, "SELECT id FROM "+parent+" ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(1)}, {int64(3)}}) {
			t.Errorf("the rows after the mutation are %#v", got)
		}
	})
	step(t, "lightweight delete", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		e.exec(t, "DELETE FROM "+child+" WHERE parent_id IN (SELECT id FROM "+parent+" WHERE name = ?)", "one")
		if got := e.count(t, "SELECT count() FROM "+child); got != 2 {
			t.Errorf("the child has %d rows, want 2", got)
		}
		// The deleted row is hidden at once, and the mutation that removes it from
		// the parts of the table runs in the background.
		if got := e.count(t, "SELECT count() FROM "+child+" WHERE parent_id = 1"); got != 0 {
			t.Errorf("a deleted row is still there")
		}
	})
	t.Run("lightweight update", func(t *testing.T) {
		eachPrincipal(t, func(t *testing.T, e env) {
			if e.is253() {
				t.Skip("25.3 has no UPDATE statement (the next subtest holds its refusal)")
			}
			lw := e.table(t, "lw", "CREATE TABLE %s (id UInt32, name String) ENGINE = MergeTree ORDER BY id SETTINGS enable_block_number_column = 1, enable_block_offset_column = 1")
			e.exec(t, "INSERT INTO "+lw+" VALUES (1, 'a'), (2, 'b')")
			e.exec(t, "UPDATE "+lw+" SET name = ? WHERE id = ?", sync2, "z", int64(1))
			poll(t, "the update to be visible", func() bool {
				return e.scalar(t, "SELECT name FROM "+lw+" WHERE id = 1") == "z"
			})
			if got := e.scalar(t, "SELECT name FROM "+lw+" WHERE id = 2"); got != "b" {
				t.Errorf("the row that no update touched is %v", got)
			}
		})
	})
	t.Run("25.3 lightweight update", func(t *testing.T) {
		eachPrincipal(t, func(t *testing.T, e env) {
			if !e.is253() {
				t.Skip("only 25.3 refuses UPDATE as a syntax error")
			}
			lw := e.table(t, "lw", "CREATE TABLE %s (id UInt32, name String) ENGINE = MergeTree ORDER BY id")
			if cerr := e.refusal(t, "UPDATE "+lw+" SET name = 'z' WHERE id = 1"); cerr.Code != 62 {
				t.Errorf("UPDATE on 25.3 gave %+v, want the code 62", cerr)
			}
		})
	})
	step(t, "async insert", func(t *testing.T, e env, parent, child string) {
		e.exec(t, "INSERT INTO "+parent+" (id, name, v) VALUES (?, ?, ?)", clickhouse.WithParameter("async_insert", 1), clickhouse.WithParameter("wait_for_async_insert", 1), int64(25), "async", int64(1))
		if got := rowsOf(t, e, "SELECT id, name FROM "+parent+" WHERE id = 25"); !reflect.DeepEqual(got, [][]any{{int64(25), "async"}}) {
			t.Errorf("the row of the async insert is %#v", got)
		}
	})
	step(t, "truncate", func(t *testing.T, e env, parent, child string) {
		seed(t, e, parent, child)
		e.exec(t, "TRUNCATE TABLE "+parent)
		if got := e.count(t, "SELECT count() FROM "+parent); got != 0 {
			t.Errorf("the table has %d rows after TRUNCATE, want 0", got)
		}
		if got := e.count(t, "SELECT count() FROM "+child); got != 3 {
			t.Errorf("the other table has %d rows, want 3", got)
		}
	})
}

// rowsAsAny returns rows as one []any, for equalValue.
func rowsAsAny(rows [][]any) []any {
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out
}

// TestIntegrationSchema tests the schema statements that ClickHouse has, and
// those that it accepts and ignores.
func TestIntegrationSchema(t *testing.T) {
	step := func(t *testing.T, name string, f func(t *testing.T, e env)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			eachPrincipal(t, f)
		})
	}
	showCreate := func(t *testing.T, e env, table string) string {
		t.Helper()
		return e.scalar(t, "SHOW CREATE TABLE "+table).(string) //nolint:forcetypeassert // SHOW CREATE TABLE gives one string.
	}
	step(t, "table", func(t *testing.T, e env) {
		tbl := e.table(t, "tbl", "CREATE TABLE %s (a Int32, b String DEFAULT 'x', c Nullable(Float64)) ENGINE = MergeTree ORDER BY a")
		e.exec(t, "CREATE TABLE IF NOT EXISTS "+tbl+" (z Int8) ENGINE = Memory")
		e.exec(t, "ALTER TABLE "+tbl+" ADD COLUMN d Int32 DEFAULT 1", clickhouse.WithParameter("alter_sync", 2))
		e.exec(t, "ALTER TABLE "+tbl+" RENAME COLUMN d TO e", clickhouse.WithParameter("alter_sync", 2))
		e.exec(t, "ALTER TABLE "+tbl+" DROP COLUMN e", clickhouse.WithParameter("alter_sync", 2))
		rows := e.rows(t, "DESCRIBE TABLE "+tbl)
		var names []string
		for _, r := range rows {
			names = append(names, r[0].(string)) //nolint:forcetypeassert // The first column of DESCRIBE is the name.
		}
		if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
			t.Errorf("the columns are %q, want a, b and c", names)
		}
		if cerr := e.refusal(t, "CREATE TABLE "+tbl+" (a Int8) ENGINE = Memory"); cerr.Code != 57 {
			t.Errorf("a table that exists gave %+v, want the code 57", cerr)
		}
		if cerr := e.refusal(t, "ALTER TABLE "+tbl+" MODIFY COLUMN a Int64", clickhouse.WithParameter("alter_sync", 2)); cerr.Code != 524 {
			t.Errorf("a change to a column of the key gave %+v, want the code 524", cerr)
		}
	})
	step(t, "primary key", func(t *testing.T, e env) {
		tbl := e.table(t, "pk", "CREATE TABLE %s (a UInt32, b String, c Int32) ENGINE = MergeTree PRIMARY KEY a ORDER BY (a, b)")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 'a', 1), (1, 'a', 2)")
		if got := e.count(t, "SELECT count() FROM "+tbl); got != 2 {
			t.Errorf("the table holds %d rows of one key, want 2, because the key is not unique", got)
		}
		if !strings.Contains(showCreate(t, e, tbl), "PRIMARY KEY a") {
			t.Errorf("SHOW CREATE TABLE has no PRIMARY KEY a")
		}
	})
	step(t, "foreign key", func(t *testing.T, e env) {
		pk := e.table(t, "fkp", "CREATE TABLE %s (a UInt32) ENGINE = MergeTree ORDER BY a")
		fk := e.table(t, "fk", "CREATE TABLE %s (a UInt32, p UInt32, FOREIGN KEY (p) REFERENCES "+pk+" (a)) ENGINE = MergeTree ORDER BY a")
		// The server accepts the clause and drops it, so a row with no parent is
		// stored (measured).
		if strings.Contains(strings.ToUpper(showCreate(t, e, fk)), "FOREIGN") {
			t.Errorf("the server kept the foreign key")
		}
		e.exec(t, "INSERT INTO "+fk+" VALUES (1, 999)")
		if got := e.count(t, "SELECT count() FROM "+fk+" WHERE p = 999"); got != 1 {
			t.Errorf("the row with no parent was not stored")
		}
		if cerr := e.refusal(t, "ALTER TABLE "+pk+" ADD CONSTRAINT fk FOREIGN KEY (a) REFERENCES "+fk+" (a)"); cerr.Code != 62 {
			t.Errorf("a foreign key by ALTER gave %+v, want the code 62", cerr)
		}
	})
	step(t, "index", func(t *testing.T, e env) {
		tbl := e.table(t, "idx", "CREATE TABLE %s (id UInt32, label String, INDEX idx_label label TYPE bloom_filter GRANULARITY 1) ENGINE = MergeTree ORDER BY id SETTINGS index_granularity = 8192")
		e.exec(t, "INSERT INTO "+tbl+" SELECT number, toString(number) FROM numbers(100000)")
		e.exec(t, "ALTER TABLE "+tbl+" ADD INDEX idx_id id TYPE minmax GRANULARITY 1", clickhouse.WithParameter("alter_sync", 2))
		var plan strings.Builder
		for _, r := range e.rows(t, "EXPLAIN indexes = 1 SELECT id FROM "+tbl+" WHERE label = ?", "42") {
			plan.WriteString(r[0].(string) + "\n") //nolint:forcetypeassert // EXPLAIN gives strings.
		}
		if !strings.Contains(plan.String(), "idx_label") || !strings.Contains(plan.String(), "Skip") {
			t.Errorf("the plan of the query does not use the index:\n%s", plan.String())
		}
		e.exec(t, "ALTER TABLE "+tbl+" DROP INDEX idx_id", clickhouse.WithParameter("alter_sync", 2))
	})
	step(t, "unique constraint", func(t *testing.T, e env) {
		for _, ddl := range []string{
			"CREATE TABLE " + e.name("uq") + " (a UInt32, UNIQUE (a)) ENGINE = MergeTree ORDER BY a",
			"CREATE TABLE " + e.name("uq") + " (a UInt32 UNIQUE) ENGINE = MergeTree ORDER BY a",
			"CREATE TABLE " + e.name("uq") + " (a UInt32, INDEX i a TYPE unique GRANULARITY 1) ENGINE = MergeTree ORDER BY a",
		} {
			if cerr := e.refusal(t, ddl); cerr.Code != 62 && cerr.Code != 36 && cerr.Code != 80 {
				t.Errorf("%q gave %+v, want a refusal", ddl, cerr)
			}
		}
		// A CHECK constraint is enforced.
		chk := e.table(t, "chk", "CREATE TABLE %s (a Int32, CONSTRAINT pos CHECK a > 0) ENGINE = MergeTree ORDER BY a")
		if cerr := e.refusal(t, "INSERT INTO "+chk+" VALUES (-1)"); cerr.Code != 469 {
			t.Errorf("a row that breaks a CHECK gave %+v, want the code 469", cerr)
		}
	})
	step(t, "view", func(t *testing.T, e env) {
		src := e.table(t, "vsrc", "CREATE TABLE %s (id UInt32, name String) ENGINE = MergeTree ORDER BY id")
		e.exec(t, "INSERT INTO "+src+" VALUES (1, 'a'), (2, 'b')")
		view := e.table(t, "view", "CREATE VIEW %s AS SELECT id, name FROM "+src+" WHERE id > 1")
		if got := e.rows(t, "SELECT id, name FROM "+view); !reflect.DeepEqual(got, [][]any{{int64(2), "b"}}) {
			t.Errorf("the view gives %#v", got)
		}
	})
	step(t, "materialized view", func(t *testing.T, e env) {
		src := e.table(t, "mvsrc", "CREATE TABLE %s (id UInt32, name String) ENGINE = MergeTree ORDER BY id")
		mv := e.table(t, "mv", "CREATE MATERIALIZED VIEW %s ENGINE = MergeTree ORDER BY id AS SELECT id, upper(name) AS name FROM "+src)
		e.exec(t, "INSERT INTO "+src+" VALUES (1, 'a'), (2, 'b')")
		if got := e.rows(t, "SELECT id, name FROM "+mv+" ORDER BY id"); !reflect.DeepEqual(got, [][]any{{int64(1), "A"}, {int64(2), "B"}}) {
			t.Errorf("the materialized view gives %#v", got)
		}
	})
	step(t, "default value", func(t *testing.T, e env) {
		tbl := e.table(t, "def", "CREATE TABLE %s (a Int32 DEFAULT 5, b String DEFAULT 'x', c Int32 MATERIALIZED a * 2, d Int32 ALIAS a + 1, e DateTime DEFAULT now()) ENGINE = MergeTree ORDER BY tuple()")
		e.exec(t, "INSERT INTO "+tbl+" (a) VALUES (?)", int64(1))
		e.exec(t, "INSERT INTO "+tbl+" (b) VALUES (?)", "y")
		got := e.rows(t, "SELECT a, b FROM "+tbl+" ORDER BY a")
		if !reflect.DeepEqual(got, [][]any{{int64(1), "x"}, {int64(5), "y"}}) {
			t.Errorf("the rows with defaults are %#v", got)
		}
	})
	step(t, "materialized column", func(t *testing.T, e env) {
		tbl := e.table(t, "matcol", "CREATE TABLE %s (a Int32, c Int32 MATERIALIZED a * 2) ENGINE = MergeTree ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" (a) VALUES (?)", int64(4))
		if got := e.rows(t, "SELECT * FROM "+tbl); !reflect.DeepEqual(got, [][]any{{int64(4)}}) {
			t.Errorf("SELECT * gives %#v, want the column a only, because a materialized column is left out", got)
		}
		if got := e.scalar(t, "SELECT c FROM "+tbl); got != int64(8) {
			t.Errorf("the materialized column is %#v, want 8", got)
		}
	})
	step(t, "alias column", func(t *testing.T, e env) {
		tbl := e.table(t, "alias", "CREATE TABLE %s (a Int32, d Int32 ALIAS a + 1) ENGINE = MergeTree ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" (a) VALUES (?)", int64(4))
		if got := e.scalar(t, "SELECT d FROM "+tbl); got != int64(5) {
			t.Errorf("the alias column is %#v, want 5", got)
		}
	})
	step(t, "ephemeral column", func(t *testing.T, e env) {
		tbl := e.table(t, "eph", "CREATE TABLE %s (a Int32, b Int32 EPHEMERAL, c Int32 DEFAULT b + 1) ENGINE = MergeTree ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" (a, b) VALUES (?, ?)", int64(1), int64(10))
		if got := e.rows(t, "SELECT a, c FROM "+tbl); !reflect.DeepEqual(got, [][]any{{int64(1), int64(11)}}) {
			t.Errorf("the rows are %#v, want 1 and 11", got)
		}
	})
	step(t, "projection", func(t *testing.T, e env) {
		tbl := e.table(t, "proj", "CREATE TABLE %s (id UInt32, label String) ENGINE = MergeTree ORDER BY id")
		e.exec(t, "ALTER TABLE "+tbl+" ADD PROJECTION p (SELECT label, count() GROUP BY label)", clickhouse.WithParameter("alter_sync", 2))
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 'a'), (2, 'a'), (3, 'b')")
		if got := e.rows(t, "SELECT label, count() AS n FROM "+tbl+" GROUP BY label ORDER BY label"); !reflect.DeepEqual(got, [][]any{{"a", uint64(2)}, {"b", uint64(1)}}) {
			t.Errorf("the group by is %#v", got)
		}
		e.exec(t, "ALTER TABLE "+tbl+" DROP PROJECTION IF EXISTS p", clickhouse.WithParameter("alter_sync", 2))
	})
	step(t, "dictionary", func(t *testing.T, e env) {
		dict := e.name("dict")
		e.dropAtEnd(t, dict)
		e.exec(t, "CREATE DICTIONARY "+dict+" (id UInt64, name String DEFAULT 'none') PRIMARY KEY id SOURCE(NULL()) LAYOUT(FLAT()) LIFETIME(0)")
		if !e.admin() {
			// The ordinary user needs the grant dictGet for a lookup, which dbrun
			// does not give it (measured).
			_, err := e.db.QueryContext(t.Context(), "SELECT dictGetOrDefault(?, 'name', toUInt64(1), 'missing')", dict) //nolint:rowserrcheck,sqlclosecheck // The statement fails.
			if cerr, ok := errors.AsType[*clickhouse.Error](err); !ok || cerr.Code != 497 {
				t.Errorf("a lookup by the ordinary user gave %v, want the code 497", err)
			}
			return
		}
		if got := e.scalar(t, "SELECT dictGetOrDefault(?, 'name', toUInt64(1), 'missing')", dict); got != "missing" {
			t.Errorf("a lookup in the dictionary gave %#v, want missing", got)
		}
	})
	step(t, "partition", func(t *testing.T, e env) {
		tbl := e.table(t, "part", "CREATE TABLE %s (a UInt32, d Date) ENGINE = MergeTree PARTITION BY toYYYYMM(d) ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, '2026-01-05'), (2, '2026-02-05'), (3, '2026-02-06')")
		if got := e.count(t, "SELECT count(DISTINCT _partition_id) FROM "+tbl); got != 2 {
			t.Errorf("the table has %d partitions, want 2", got)
		}
	})
	step(t, "drop partition", func(t *testing.T, e env) {
		tbl := e.table(t, "droppart", "CREATE TABLE %s (a UInt32, d Date) ENGINE = MergeTree PARTITION BY toYYYYMM(d) ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, '2026-01-05'), (2, '2026-02-05')")
		e.exec(t, "ALTER TABLE "+tbl+" DROP PARTITION 202601")
		if got := e.rows(t, "SELECT a FROM "+tbl); !reflect.DeepEqual(got, [][]any{{int64(2)}}) {
			t.Errorf("the rows after the drop of a partition are %#v", got)
		}
	})
	step(t, "codec", func(t *testing.T, e env) {
		tbl := e.table(t, "codec", "CREATE TABLE %s (a UInt32 CODEC(Delta, ZSTD(3)), s String CODEC(LZ4)) ENGINE = MergeTree ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" SELECT number, toString(number) FROM numbers(1000)")
		if got := e.rows(t, "SELECT count(), max(s) FROM "+tbl); !reflect.DeepEqual(got, [][]any{{uint64(1000), "999"}}) {
			t.Errorf("the table gives %#v", got)
		}
	})
	step(t, "replacing merge tree", func(t *testing.T, e env) {
		tbl := e.table(t, "rep", "CREATE TABLE %s (a UInt32, v String, ver UInt32) ENGINE = ReplacingMergeTree(ver) ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 'a', 1)")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 'b', 2), (2, 'c', 1)")
		if got := e.rows(t, "SELECT a, v FROM "+tbl+" FINAL ORDER BY a"); !reflect.DeepEqual(got, [][]any{{int64(1), "b"}, {int64(2), "c"}}) {
			t.Errorf("the rows with FINAL are %#v", got)
		}
	})
	step(t, "collapsing merge tree", func(t *testing.T, e env) {
		tbl := e.table(t, "col", "CREATE TABLE %s (a UInt32, v Int32, sign Int8) ENGINE = CollapsingMergeTree(sign) ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 5, 1), (1, 5, -1), (2, 7, 1)")
		if got := e.rows(t, "SELECT a, v FROM "+tbl+" FINAL ORDER BY a"); !reflect.DeepEqual(got, [][]any{{int64(2), int64(7)}}) {
			t.Errorf("the rows with FINAL are %#v", got)
		}
	})
	step(t, "kafka engine", func(t *testing.T, e env) {
		ddl := "CREATE TABLE " + e.name("kafka") + " (a Int32) ENGINE = Kafka SETTINGS kafka_broker_list = '127.0.0.1:1', kafka_topic_list = 't', kafka_group_name = 'g', kafka_format = 'CSV'"
		if !e.admin() {
			// The ordinary user has no grant on the source.
			if cerr := e.refusal(t, ddl); cerr.Code != 497 {
				t.Errorf("the Kafka engine gave %+v for the ordinary user, want the code 497", cerr)
			}
			return
		}
		e.dropAtEnd(t, e.name("kafka"))
		e.exec(t, ddl)
		if got := e.scalar(t, "SELECT engine FROM system.tables WHERE database = ? AND name = ?", e.database, prefix+"_kafka"); got != "Kafka" {
			t.Errorf("the engine is %v, want Kafka", got)
		}
	})
	step(t, "live view", func(t *testing.T, e env) {
		if e.atLeast(26, 9) {
			t.Skip("26.9 has no live view (the next subtest holds its refusal)")
		}
		cerr := e.refusal(t, "CREATE LIVE VIEW "+e.name("live")+" AS SELECT 1 AS a")
		if cerr.Code != 344 {
			t.Errorf("a live view with no setting gave %+v, want the code 344", cerr)
		}
		view := e.name("live")
		e.dropAtEnd(t, view)
		e.exec(t, "CREATE LIVE VIEW "+view+" AS SELECT 1 AS a", clickhouse.WithParameter("allow_experimental_live_view", 1))
		if got := e.scalar(t, "SELECT a FROM "+view); got != int64(1) {
			t.Errorf("the live view gives %#v", got)
		}
	})
	step(t, "26.9 live view", func(t *testing.T, e env) {
		if !e.atLeast(26, 9) {
			t.Skip("only 26.9 refuses a live view as a syntax error")
		}
		if cerr := e.refusal(t, "CREATE LIVE VIEW "+e.name("live")+" AS SELECT 1 AS a", clickhouse.WithParameter("allow_experimental_live_view", 1)); cerr.Code != 62 {
			t.Errorf("a live view on 26.9 gave %+v, want the code 62", cerr)
		}
	})
	step(t, "window view", func(t *testing.T, e env) {
		src := e.table(t, "winsrc", "CREATE TABLE %s (a Int32) ENGINE = Memory")
		cerr := e.refusal(t, "CREATE WINDOW VIEW "+e.name("win")+" ENGINE = Memory AS SELECT count() AS c, tumbleStart(w) AS s FROM "+src+" GROUP BY tumble(now(), INTERVAL 1 SECOND) AS w")
		if cerr.Code != 62 && cerr.Code != 344 {
			t.Errorf("a window view gave %+v, want a refusal", cerr)
		}
	})
}

// gzipped returns the gzip form of b.
func gzipped(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gunzipped returns the bytes that the gzip form b holds.
func gunzipped(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("reading gzip: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading gzip: %v", err)
	}
	return out
}

// TestIntegrationFeatures tests each feature of the server that features.json
// marks yes or no, with the driver when the driver has a way to use it, and with
// requests of net/http when it has not, to see what the server answers.
func TestIntegrationFeatures(t *testing.T) { //nolint:maintidx // One table of subtests, an entry for each feature of features.json.
	step := func(t *testing.T, name string, f func(t *testing.T, e env)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			eachPrincipal(t, f)
		})
	}
	format := func(name string) url.Values { return url.Values{"default_format": {name}} }
	// formatRefused holds that the driver reads one format, and that a statement
	// that names another gets dbimp.ErrNotSupported.
	formatRefused := func(t *testing.T, e env, name string) {
		t.Helper()
		_, err := e.db.ExecContext(t.Context(), "SELECT 1 AS a", clickhouse.WithParameter("default_format", name))
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("a statement with the format %s gave %v, want dbimp.ErrNotSupported", name, err)
		}
	}
	step(t, "JSON format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a, 'x' AS b", format("JSON"))
		if r.status != 200 || !strings.Contains(r.header.Get("Content-Type"), "application/json") || !strings.Contains(string(r.body), `"meta"`) || !strings.Contains(string(r.body), `"data"`) {
			t.Errorf("the answer is %d %q", r.status, r.body)
		}
		formatRefused(t, e, "JSON")
	})
	step(t, "JSONEachRow format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a, 'x' AS b", format("JSONEachRow"))
		if r.status != 200 || strings.TrimSpace(string(r.body)) != `{"a":1,"b":"x"}` {
			t.Errorf("the answer is %d %q", r.status, r.body)
		}
		formatRefused(t, e, "JSONEachRow")
	})
	step(t, "RowBinaryWithNamesAndTypes format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a", format("RowBinaryWithNamesAndTypes"))
		if r.status != 200 || string(r.body) != "\x01\x01a\x05UInt8\x01" {
			t.Errorf("the answer is %d %q", r.status, r.body)
		}
		formatRefused(t, e, "RowBinaryWithNamesAndTypes")
	})
	step(t, "Native format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a", format("Native"))
		if r.status != 200 || string(r.body) != "\x01\x01\x01a\x05UInt8\x01" {
			t.Errorf("the answer is %d %q", r.status, r.body)
		}
		formatRefused(t, e, "Native")
	})
	step(t, "Parquet format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a", format("Parquet"))
		if r.status != 200 || !bytes.HasPrefix(r.body, []byte("PAR1")) || !bytes.HasSuffix(r.body, []byte("PAR1")) {
			t.Errorf("the answer is %d %q", r.status, r.body)
		}
		formatRefused(t, e, "Parquet")
	})
	step(t, "CSV format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a, 'x,y' AS b", format("CSV"))
		if r.status != 200 || string(r.body) != "1,\"x,y\"\n" {
			t.Errorf("the answer is %d %q", r.status, r.body)
		}
		formatRefused(t, e, "CSV")
	})
	step(t, "Arrow format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a", format("Arrow"))
		if r.status != 200 || !bytes.HasPrefix(r.body, []byte("ARROW1")) {
			t.Errorf("the answer is %d %q", r.status, r.body)
		}
		formatRefused(t, e, "Arrow")
	})
	step(t, "format clause", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a FORMAT CSV", format("JSON"))
		if r.status != 200 || string(r.body) != "1\n" {
			t.Errorf("the clause did not win over the key: %d %q", r.status, r.body)
		}
		// The driver reads the response header X-ClickHouse-Format, so a statement
		// with a FORMAT clause is refused and not misread.
		_, err := e.db.ExecContext(t.Context(), "SELECT 1 AS a FORMAT CSV")
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("a statement with a FORMAT clause gave %v, want dbimp.ErrNotSupported", err)
		}
	})
	step(t, "default format", func(t *testing.T, e env) {
		r := e.post(t, "SELECT 1 AS a, 'x' AS b", nil)
		if r.status != 200 || string(r.body) != "1\tx\n" || !strings.Contains(r.header.Get("Content-Type"), "text/tab-separated-values") {
			t.Errorf("the answer with no format is %d %q (%s)", r.status, r.body, r.header.Get("Content-Type"))
		}
		if got := r.header.Get("X-Clickhouse-Format"); got != "TabSeparated" {
			t.Errorf("X-ClickHouse-Format is %q, want TabSeparated", got)
		}
	})
	step(t, "final", func(t *testing.T, e env) {
		tbl := e.table(t, "final", "CREATE TABLE %s (a UInt32, v String, ver UInt32) ENGINE = ReplacingMergeTree(ver) ORDER BY a")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 'a', 1)")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 'b', 2)")
		if got := e.count(t, "SELECT count() FROM "+tbl); got != 2 {
			t.Errorf("the table holds %d rows before a merge, want 2", got)
		}
		if got := e.rows(t, "SELECT v FROM "+tbl+" FINAL"); !reflect.DeepEqual(got, [][]any{{"b"}}) {
			t.Errorf("FINAL gives %#v, want b", got)
		}
	})
	step(t, "sample", func(t *testing.T, e env) {
		tbl := e.table(t, "sample", "CREATE TABLE %s (id UInt64) ENGINE = MergeTree ORDER BY intHash32(id) SAMPLE BY intHash32(id)")
		e.exec(t, "INSERT INTO "+tbl+" SELECT number FROM numbers(10000)")
		n := e.count(t, "SELECT count() FROM "+tbl+" SAMPLE 1 / 10")
		if n < 100 || n > 3000 {
			t.Errorf("a sample of a tenth holds %d rows of 10000", n)
		}
	})
	step(t, "prewhere", func(t *testing.T, e env) {
		tbl := e.table(t, "prewhere", "CREATE TABLE %s (id UInt64, s String) ENGINE = MergeTree ORDER BY id")
		e.exec(t, "INSERT INTO "+tbl+" SELECT number, toString(number) FROM numbers(100)")
		got := e.rows(t, "SELECT id FROM "+tbl+" PREWHERE id < ? WHERE s != ? ORDER BY id", int64(3), "1")
		if !reflect.DeepEqual(got, [][]any{{uint64(0)}, {uint64(2)}}) {
			t.Errorf("PREWHERE gives %#v", got)
		}
	})
	step(t, "typed parameters", func(t *testing.T, e env) {
		checkParameters(t, e)
	})
	step(t, "query id", func(t *testing.T, e env) {
		if got := e.scalar(t, "SELECT queryID()", clickhouse.WithParameter("query_id", "dbimp-it-"+suffix+"-named")); got != "dbimp-it-"+suffix+"-named" {
			t.Errorf("queryID() is %v, want the name that the option gave", got)
		}
		got, _ := e.scalar(t, "SELECT queryID()").(string)
		if !strings.HasPrefix(got, "dbimp-") || len(got) < 20 {
			t.Errorf("queryID() is %q, want a name that the driver made", got)
		}
		r := e.post(t, "SELECT 1", url.Values{"query_id": {"dbimp-it-" + suffix + "-raw"}})
		if r.header.Get("X-Clickhouse-Query-Id") != "dbimp-it-"+suffix+"-raw" {
			t.Errorf("the answer names the query %q", r.header.Get("X-Clickhouse-Query-Id"))
		}
	})
	step(t, "session id", func(t *testing.T, e env) {
		sid := "dbimp-it-" + suffix + "-" + e.p.name
		q := func(extra string) url.Values {
			return url.Values{"session_id": {sid}, "max_block_size": {extra}}
		}
		if r := e.post(t, "SET max_result_rows = 77", q("1000")); r.status != 200 {
			t.Fatalf("SET gave %d %q", r.status, r.body)
		}
		if r := e.post(t, "SELECT getSetting('max_result_rows')", q("1000")); strings.TrimSpace(string(r.body)) != "77" {
			t.Errorf("the setting in the session is %q, want 77", r.body)
		}
		if r := e.post(t, "SELECT getSetting('max_result_rows')", nil); strings.TrimSpace(string(r.body)) == "77" {
			t.Errorf("the setting stayed in a request that has no session")
		}
		e.post(t, "SELECT 1", url.Values{"session_id": {sid}, "close_session": {"1"}})
	})
	step(t, "temporary table", func(t *testing.T, e env) {
		sid := "dbimp-it-" + suffix + "-tmp-" + e.p.name
		sess := url.Values{"session_id": {sid}}
		if r := e.post(t, "CREATE TEMPORARY TABLE tmp_a (a Int8)", sess); r.status != 200 {
			t.Fatalf("CREATE TEMPORARY TABLE gave %d %q", r.status, r.body)
		}
		e.post(t, "INSERT INTO tmp_a VALUES (1), (2)", sess)
		if r := e.post(t, "SELECT a FROM tmp_a ORDER BY a", sess); string(r.body) != "1\n2\n" {
			t.Errorf("the temporary table gives %q", r.body)
		}
		if r := e.post(t, "SELECT a FROM tmp_a", nil); r.status != http.StatusNotFound {
			t.Errorf("a request with no session read the temporary table: %d %q", r.status, r.body)
		}
		e.post(t, "SELECT 1", url.Values{"session_id": {sid}, "close_session": {"1"}})
	})
	step(t, "kill query", func(t *testing.T, e env) {
		id := "dbimp-it-" + suffix + "-kill-" + e.p.name
		done := make(chan error, 1)
		go func() {
			rows, err := e.db.QueryContext(t.Context(), "SELECT sleepEachRow(0.2) FROM numbers(300)", clickhouse.WithParameter("query_id", id), clickhouse.WithParameter("max_block_size", 1))
			if err != nil {
				done <- err
				return
			}
			defer rows.Close()
			for rows.Next() {
			}
			done <- rows.Err()
		}()
		poll(t, "the query to run", func() bool {
			return e.count(t, "SELECT count() FROM system.processes WHERE query_id = ?", id) > 0
		})
		rows := e.rows(t, "KILL QUERY WHERE query_id = ? SYNC", id)
		if len(rows) != 1 || rows[0][0] != "finished" || rows[0][1] != id {
			t.Errorf("KILL QUERY gave %#v, want one row with the status finished", rows)
		}
		err := <-done
		cerr, ok := errors.AsType[*clickhouse.Error](err)
		if !ok || cerr.Code != 394 {
			t.Errorf("the killed query ended with %v, want the code 394", err)
		}
		if none := e.rows(t, "KILL QUERY WHERE query_id = 'dbimp-none' SYNC"); len(none) != 0 {
			t.Errorf("KILL QUERY of a query that does not exist gave %#v, want no row", none)
		}
	})
	step(t, "x-clickhouse-user header", func(t *testing.T, e env) {
		h := http.Header{"X-Clickhouse-User": {e.cfg.User}, "X-Clickhouse-Key": {e.cfg.Password}}
		r := e.send(t, http.MethodPost, "/", nil, h, []byte("SELECT currentUser()"))
		if r.status != 200 || strings.TrimSpace(string(r.body)) != e.cfg.User {
			t.Errorf("the headers of the user gave %d %q", r.status, r.body)
		}
		// The server refuses the headers with a Basic header, which is why the driver
		// sends the Basic header only (D177).
		h.Set("Authorization", "Basic "+basic(e.cfg.User, e.cfg.Password))
		r = e.send(t, http.MethodPost, "/", nil, h, []byte("SELECT 1"))
		if r.status != http.StatusForbidden || r.header.Get("X-Clickhouse-Exception-Code") != "516" {
			t.Errorf("both forms gave %d %q, want HTTP 403 and the code 516", r.status, r.body)
		}
	})
	step(t, "x-clickhouse-database header", func(t *testing.T, e env) {
		r := e.send(t, http.MethodPost, "/", nil, http.Header{"X-Clickhouse-Database": {e.database}}, []byte("SELECT currentDatabase()"))
		if strings.TrimSpace(string(r.body)) != e.database {
			t.Errorf("the database is %q, want %q", r.body, e.database)
		}
	})
	step(t, "x-clickhouse-format header", func(t *testing.T, e env) {
		r := e.send(t, http.MethodPost, "/", nil, http.Header{"X-Clickhouse-Format": {"JSONCompact"}}, []byte("SELECT 1 AS a"))
		if !strings.Contains(string(r.body), `"meta"`) || r.header.Get("X-Clickhouse-Format") != "JSONCompact" {
			t.Errorf("the header of the format gave %q", r.body)
		}
	})
	step(t, "x-clickhouse-query-id header", func(t *testing.T, e env) {
		id := "dbimp-it-" + suffix + "-hdr-" + e.p.name
		r := e.send(t, http.MethodPost, "/", nil, http.Header{"X-Clickhouse-Query-Id": {id}}, []byte("SELECT 1"))
		if r.header.Get("X-Clickhouse-Query-Id") != id {
			t.Errorf("the answer names the query %q, want %q", r.header.Get("X-Clickhouse-Query-Id"), id)
		}
	})
	step(t, "x-clickhouse-summary header", func(t *testing.T, e env) {
		tbl := e.table(t, "summary", "CREATE TABLE %s (a Int32) ENGINE = MergeTree ORDER BY a")
		r := e.post(t, "INSERT INTO "+tbl+" VALUES (1), (2), (3)", nil)
		if !strings.Contains(r.header.Get("X-Clickhouse-Summary"), `"written_rows":"3"`) {
			t.Errorf("the summary of an insert is %q", r.header.Get("X-Clickhouse-Summary"))
		}
		r = e.post(t, "SELECT sum(a) FROM "+tbl, nil)
		if !strings.Contains(r.header.Get("X-Clickhouse-Summary"), `"read_rows":"3"`) {
			t.Errorf("the summary of a select is %q", r.header.Get("X-Clickhouse-Summary"))
		}
	})
	step(t, "progress headers", func(t *testing.T, e env) {
		r := e.post(t, "SELECT number FROM numbers(1000000) FORMAT Null", url.Values{"send_progress_in_http_headers": {"1"}, "http_headers_progress_interval_ms": {"0"}, "max_block_size": {"100000"}})
		if r.status != 200 || len(r.header.Values("X-Clickhouse-Progress")) == 0 {
			t.Errorf("the answer has no X-ClickHouse-Progress: %d %v", r.status, r.header)
		}
	})
	step(t, "response compression", func(t *testing.T, e env) {
		q := url.Values{"enable_http_compression": {"1"}, "default_format": {"CSV"}}
		r := e.send(t, http.MethodPost, "/", q, http.Header{"Accept-Encoding": {"gzip"}}, []byte("SELECT number FROM numbers(3)"))
		if r.header.Get("Content-Encoding") != "gzip" || string(gunzipped(t, r.body)) != "0\n1\n2\n" {
			t.Errorf("the compressed answer is %q (%s)", r.body, r.header.Get("Content-Encoding"))
		}
		// The transport of the driver asks for gzip and reads it, so a caller who
		// turns the setting on gets the same rows.
		rows := e.rows(t, "SELECT number FROM numbers(1000)", clickhouse.WithParameter("enable_http_compression", 1))
		if len(rows) != 1000 || rows[999][0] != uint64(999) {
			t.Errorf("the driver read %d rows of a compressed answer", len(rows))
		}
		// An error is compressed too, and the driver reads it.
		if cerr := e.refusal(t, "SELECT * FROM "+e.name("nosuch"), clickhouse.WithParameter("enable_http_compression", 1)); cerr.Code != 60 {
			t.Errorf("an error in a compressed answer gave %+v", cerr)
		}
	})
	step(t, "request compression", func(t *testing.T, e env) {
		r := e.send(t, http.MethodPost, "/", nil, http.Header{"Content-Encoding": {"gzip"}}, gzipped(t, []byte("SELECT 41 + 1")))
		if r.status != 200 || strings.TrimSpace(string(r.body)) != "42" {
			t.Errorf("a gzip body gave %d %q", r.status, r.body)
		}
		r = e.send(t, http.MethodPost, "/", nil, http.Header{"Content-Encoding": {"gzip"}}, []byte("SELECT 1"))
		if code := r.header.Get("X-Clickhouse-Exception-Code"); r.status != http.StatusInternalServerError || code != "271" && code != "354" {
			t.Errorf("a body that is not gzip gave %d %q, want HTTP 500 and the code 271 or 354", r.status, r.body)
		}
	})
	step(t, "native compression", func(t *testing.T, e env) {
		r := e.post(t, "SELECT number FROM numbers(500)", url.Values{"default_format": {"Native"}, "compress": {"1"}})
		// A block of the compressed form starts with a checksum of 16 bytes and a
		// byte for the method, which is 0x82 for LZ4, and 0x90 for ZSTD on 26.9.
		if r.status != 200 || len(r.body) < 17 || r.body[16] != 0x82 && r.body[16] != 0x90 {
			t.Errorf("the answer is %d, %d bytes, method %x", r.status, len(r.body), r.body[min(16, len(r.body)-1)])
		}
	})
	step(t, "transactions", func(t *testing.T, e env) {
		if _, err := e.db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported (D177)", err)
		}
		r := e.post(t, "BEGIN TRANSACTION", nil)
		if r.status != http.StatusNotImplemented || r.header.Get("X-Clickhouse-Exception-Code") != "48" {
			t.Errorf("BEGIN gave %d %q, want HTTP 501 and the code 48", r.status, r.body)
		}
	})
	step(t, "multi statement", func(t *testing.T, e env) {
		if cerr := e.refusal(t, "SELECT 1; SELECT 2"); cerr.Code != 62 || cerr.HTTPStatus != http.StatusBadRequest {
			t.Errorf("two statements gave %+v, want the code 62 and HTTP 400", cerr)
		}
		// A semicolon at the end is fine.
		e.exec(t, "SELECT 1;")
	})
	step(t, "external tables", func(t *testing.T, e env) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("ext", "ext")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("1,x\n2,y\n"))
		_ = mw.Close()
		q := url.Values{"query": {"SELECT a, b FROM ext ORDER BY a"}, "ext_structure": {"a Int32, b String"}, "ext_format": {"CSV"}}
		r := e.send(t, http.MethodPost, "/", q, http.Header{"Content-Type": {mw.FormDataContentType()}}, body.Bytes())
		if r.status != 200 || string(r.body) != "1\tx\n2\ty\n" {
			t.Errorf("the external table gave %d %q", r.status, r.body)
		}
	})
	step(t, "wait end of query", func(t *testing.T, e env) {
		q := url.Values{"max_block_size": {"2"}, "wait_end_of_query": {"1"}}
		r := e.post(t, "SELECT number, throwIf(number = 5) FROM numbers(10)", q)
		if r.status != http.StatusInternalServerError || !strings.Contains(string(r.body), "Code: 395") || strings.Contains(string(r.body), "0\t0") {
			t.Errorf("the answer is %d %q, want HTTP 500 and the error with no row", r.status, r.body)
		}
		_, err := e.db.ExecContext(t.Context(), "SELECT number, throwIf(number = 5) FROM numbers(10)", clickhouse.WithParameter("wait_end_of_query", 1), clickhouse.WithParameter("max_block_size", 2))
		if cerr, ok := errors.AsType[*clickhouse.Error](err); !ok || cerr.Code != 395 || errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("the driver got %v, want the code 395 before any row", err)
		}
	})
	step(t, "buffer size", func(t *testing.T, e env) {
		q := url.Values{"buffer_size": {"10000000"}, "wait_end_of_query": {"1"}, "default_format": {"Null"}}
		r := e.post(t, "SELECT number FROM numbers(100000)", q)
		if !strings.Contains(r.header.Get("X-Clickhouse-Summary"), `"result_rows":"100000"`) {
			t.Errorf("the summary of a buffered answer is %q, want the whole result", r.header.Get("X-Clickhouse-Summary"))
		}
	})
	step(t, "100 continue", func(t *testing.T, e env) {
		r := e.send(t, http.MethodPost, "/", nil, http.Header{"Expect": {"100-continue"}}, []byte("SELECT 1"))
		if r.status != 200 || strings.TrimSpace(string(r.body)) != "1" {
			t.Errorf("a request with Expect gave %d %q", r.status, r.body)
		}
	})
	step(t, "quote 64 bit integers", func(t *testing.T, e env) {
		q := func(v string) url.Values {
			return url.Values{"default_format": {"JSONCompactEachRow"}, "output_format_json_quote_64bit_integers": {v}}
		}
		if r := e.post(t, "SELECT toUInt64(18446744073709551615)", q("1")); strings.TrimSpace(string(r.body)) != `["18446744073709551615"]` {
			t.Errorf("the quoted form is %q", r.body)
		}
		if r := e.post(t, "SELECT toUInt64(18446744073709551615)", q("0")); strings.TrimSpace(string(r.body)) != `[18446744073709551615]` {
			t.Errorf("the plain form is %q", r.body)
		}
		// The driver reads both.
		for _, v := range []int{0, 1} {
			got := e.scalar(t, "SELECT toUInt64(18446744073709551615)", clickhouse.WithParameter("output_format_json_quote_64bit_integers", v))
			if got != uint64(18446744073709551615) {
				t.Errorf("with the setting %d, the value is %#v", v, got)
			}
		}
	})
	step(t, "ping endpoint", func(t *testing.T, e env) {
		r := e.send(t, http.MethodGet, "/ping", nil, nil, nil)
		if r.status != 200 || string(r.body) != "Ok.\n" {
			t.Errorf("GET /ping gave %d %q", r.status, r.body)
		}
	})
	step(t, "replicas status endpoint", func(t *testing.T, e env) {
		r := e.send(t, http.MethodGet, "/replicas_status", nil, nil, nil)
		if r.status != 200 || string(r.body) != "Ok.\n" {
			t.Errorf("GET /replicas_status gave %d %q", r.status, r.body)
		}
	})
	step(t, "get statement", func(t *testing.T, e env) {
		r := e.send(t, http.MethodGet, "/", url.Values{"query": {"SELECT 1 + 1"}}, nil, nil)
		if r.status != 200 || strings.TrimSpace(string(r.body)) != "2" {
			t.Errorf("GET with a query gave %d %q", r.status, r.body)
		}
		// A GET makes the request readonly.
		r = e.send(t, http.MethodGet, "/", url.Values{"query": {"CREATE TABLE " + e.name("get") + " (a Int8) ENGINE = Memory"}}, nil, nil)
		if r.status != http.StatusInternalServerError || r.header.Get("X-Clickhouse-Exception-Code") != "164" {
			t.Errorf("a CREATE by GET gave %d %q, want HTTP 500 and the code 164", r.status, r.body)
		}
	})
	step(t, "readonly", func(t *testing.T, e env) {
		tbl := e.table(t, "ro", "CREATE TABLE %s (a Int8) ENGINE = Memory")
		if got := e.scalar(t, "SELECT 1", clickhouse.WithReadonly(true)); got != int64(1) {
			t.Errorf("a read with WithReadonly gave %#v", got)
		}
		if cerr := e.refusal(t, "INSERT INTO "+tbl+" VALUES (1)", clickhouse.WithReadonly(true)); cerr.Code != 164 {
			t.Errorf("a write with WithReadonly gave %+v, want the code 164", cerr)
		}
		if got := e.count(t, "SELECT count() FROM "+tbl); got != 0 {
			t.Errorf("the refused write left %d rows", got)
		}
		// The settings in the query string do not make readonly=1 refuse the
		// statement, so the settings of the driver go with it (measured).
		if r := e.post(t, "SELECT 1", url.Values{"readonly": {"1"}, "max_threads": {"2"}}); r.status != 200 {
			t.Errorf("readonly=1 with a setting in the query gave %d %q, want 200", r.status, r.body)
		}
		// A SETTINGS clause is a change of a setting in the text of the statement,
		// which readonly=1 refuses.
		if cerr := e.refusal(t, "SELECT 1 SETTINGS max_threads = 1", clickhouse.WithReadonly(true)); cerr.Code != 164 {
			t.Errorf("a SETTINGS clause with WithReadonly gave %+v, want the code 164", cerr)
		}
	})
	step(t, "max result rows", func(t *testing.T, e env) {
		_, err := e.db.QueryContext(t.Context(), "SELECT number FROM numbers(1000)", clickhouse.WithParameter("max_result_rows", 100), clickhouse.WithParameter("result_overflow_mode", "throw")) //nolint:rowserrcheck,sqlclosecheck // The statement fails, and no rows come, or the error comes after them.
		if err != nil {
			if cerr, ok := errors.AsType[*clickhouse.Error](err); !ok || cerr.Code != 396 {
				t.Errorf("a result over the cap gave %v, want the code 396", err)
			}
			return
		}
		t.Error("a result over the cap gave no error")
	})
	step(t, "max execution time", func(t *testing.T, e env) {
		r := e.post(t, "SELECT sleep(3) + sleep(3)", url.Values{"max_execution_time": {"1"}})
		if r.header.Get("X-Clickhouse-Exception-Code") != "159" && !strings.Contains(string(r.body), "Code: 159") {
			t.Errorf("max_execution_time gave %d %q, want the code 159", r.status, r.body)
		}
	})
	step(t, "Nullable", func(t *testing.T, e env) {
		rows := e.rows(t, "SELECT CAST(NULL AS Nullable(Int32)) AS a, CAST(5 AS Nullable(Int32)) AS b, CAST('' AS Nullable(String)) AS c")
		if !reflect.DeepEqual(rows, [][]any{{nil, int64(5), ""}}) {
			t.Errorf("the nullable values are %#v, want nil, 5 and the empty string, which is not NULL", rows)
		}
		if cerr := e.refusal(t, "SELECT CAST(NULL AS Nullable(Array(Int32)))"); cerr.Code != 43 {
			t.Errorf("Nullable(Array) gave %+v, want the code 43", cerr)
		}
	})
	step(t, "LowCardinality", func(t *testing.T, e env) {
		rows := e.rows(t, "SELECT toLowCardinality('x') AS a, CAST(NULL AS LowCardinality(Nullable(String))) AS b")
		if !reflect.DeepEqual(rows, [][]any{{"x", nil}}) {
			t.Errorf("the values are %#v", rows)
		}
		cts, err := queryTypes(t, e, "SELECT toLowCardinality('x') AS a")
		if err != nil || cts[0].DatabaseTypeName() != "STRING" || cts[0].ScanType() != reflect.TypeFor[string]() {
			t.Errorf("the type of a LowCardinality(String) is %v, %v, want STRING and string", cts, err)
		}
	})
	step(t, "SimpleAggregateFunction", func(t *testing.T, e env) {
		tbl := e.table(t, "saf", "CREATE TABLE %s (k UInt32, m SimpleAggregateFunction(max, Int32)) ENGINE = AggregatingMergeTree ORDER BY k")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, 5), (1, 9), (2, -3)")
		got := e.rows(t, "SELECT k, max(m) FROM "+tbl+" GROUP BY k ORDER BY k")
		if !reflect.DeepEqual(got, [][]any{{int64(1), int64(9)}, {int64(2), int64(-3)}}) {
			t.Errorf("the maxima are %#v", got)
		}
		cts, err := queryTypes(t, e, "SELECT m FROM "+tbl)
		if err != nil || cts[0].DatabaseTypeName() != "INT32" || cts[0].ScanType() != reflect.TypeFor[int64]() {
			t.Errorf("the type of the column is %v, %v, want INT32 and int64", cts, err)
		}
	})
	step(t, "Nested", func(t *testing.T, e env) {
		tbl := e.table(t, "nested", "CREATE TABLE %s (id UInt32, nest Nested(a Int32, b String)) ENGINE = MergeTree ORDER BY id")
		e.exec(t, "INSERT INTO "+tbl+" VALUES (1, [1, 2], ['p', 'q'])")
		rows, err := e.db.QueryContext(t.Context(), "SELECT * FROM "+tbl)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		if !reflect.DeepEqual(cols, []string{"id", "nest.a", "nest.b"}) {
			t.Errorf("the columns are %q, want id, nest.a and nest.b in that order", cols)
		}
		if !rows.Next() {
			t.Fatalf("no row: %v", rows.Err())
		}
		var id int64
		var a, b []any
		if err := rows.Scan(&id, &a, &b); err != nil || !reflect.DeepEqual(a, []any{int64(1), int64(2)}) || !reflect.DeepEqual(b, []any{"p", "q"}) {
			t.Errorf("the row is %d, %v, %v, %v", id, a, b, err)
		}
	})
	step(t, "26.9 exception tag", func(t *testing.T, e env) {
		if !e.atLeast(26, 9) {
			t.Skip("only 26.9 sends the header X-ClickHouse-Exception-Tag")
		}
		r := e.post(t, "SELECT 1", nil)
		if len(r.header.Get("X-Clickhouse-Exception-Tag")) != 16 {
			t.Errorf("the tag is %q, want 16 letters on every answer", r.header.Get("X-Clickhouse-Exception-Tag"))
		}
	})
	step(t, "25.8 exception tag", func(t *testing.T, e env) {
		if e.atLeast(26, 9) {
			t.Skip("only the releases before 26.9 send no tag")
		}
		r := e.post(t, "SELECT 1", nil)
		if got := r.header.Get("X-Clickhouse-Exception-Tag"); got != "" {
			t.Errorf("the tag is %q, want none", got)
		}
	})
	step(t, "url table function", func(t *testing.T, e env) {
		_, err := e.db.QueryContext(t.Context(), "SELECT 1 FROM url('http://127.0.0.1:1/x', CSV, 'a Int8')", clickhouse.WithParameter("http_max_tries", 1), clickhouse.WithParameter("http_connection_timeout", 1)) //nolint:rowserrcheck,sqlclosecheck // The statement fails.
		cerr, ok := errors.AsType[*clickhouse.Error](err)
		if !ok {
			t.Fatalf("url() gave %v, want the error of the server", err)
		}
		if e.admin() && cerr.Code == 497 || !e.admin() && cerr.Code != 497 {
			t.Errorf("url() gave %+v for %s, want the code 497 for the ordinary user and a network error for the administrator", cerr, e.p.name)
		}
	})
	step(t, "s3 table function", func(t *testing.T, e env) {
		_, err := e.db.QueryContext(t.Context(), "SELECT 1 FROM s3('http://127.0.0.1:1/bucket/key', 'CSV', 'a Int8')", clickhouse.WithParameter("http_max_tries", 1), clickhouse.WithParameter("s3_retry_attempts", 1), clickhouse.WithParameter("http_connection_timeout", 1)) //nolint:rowserrcheck,sqlclosecheck // The statement fails.
		cerr, ok := errors.AsType[*clickhouse.Error](err)
		if !ok {
			t.Fatalf("s3() gave %v, want the error of the server", err)
		}
		// The administrator gets a network error, or on 26.9 the code 497, because
		// the server refuses to use its own credentials for a user query.
		if !e.admin() && cerr.Code != 497 {
			t.Errorf("s3() gave %+v for the ordinary user, want the code 497", cerr)
		}
	})
}

// queryTypes returns the column types of a query.
func queryTypes(t *testing.T, e env, query string) ([]*sql.ColumnType, error) {
	t.Helper()
	rows, err := e.db.QueryContext(t.Context(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	for rows.Next() {
	}
	return cts, rows.Err()
}

// basic returns the value of a Basic header for a user and a password.
func basic(user, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
}

// smallestFloat is what the server reads the text 5e-324 as: 0 on 25.3 and 25.8,
// and the smallest float on 26.9 (measured).
func smallestFloat(e env) float64 {
	if e.atLeast(26, 9) {
		return math.SmallestNonzeroFloat64
	}
	return 0
}

// checkParameters binds each Go type of D176 as a typed parameter, and reads the
// value back with a select of the parameter, so the type that the driver chose
// and the text that it wrote are both what the server reads. A string has a tab,
// a new line, a backslash, quotes and a NUL, and a time has its nanoseconds.
func checkParameters(t *testing.T, e env) {
	t.Helper()
	d := func(s string) *apd.Decimal {
		v, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	big256, _ := new(big.Int).SetString("-57896044618658097711785492504343953926634992332820282019728792003956564819968", 10)
	big256u, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
	tricky := "a\tb\nc\\d'e\"f\x00g\r\u00e9 \\N"
	for _, tt := range []struct {
		name string
		in   any
		want any
	}{
		{"true", true, true},
		{"int64 min", int64(math.MinInt64), int64(math.MinInt64)},
		{"uint64 max", uint64(math.MaxUint64), uint64(math.MaxUint64)},
		{"uint64 above int64", uint64(1 << 63), uint64(1 << 63)},
		{"float64", 0.1, 0.1},
		{"float64 max", math.MaxFloat64, math.MaxFloat64},
		{"float64 min", math.SmallestNonzeroFloat64, smallestFloat(e)},
		{"negative zero", math.Copysign(0, -1), math.Copysign(0, -1)},
		{"NaN", math.NaN(), math.NaN()},
		{"infinity", math.Inf(1), math.Inf(1)},
		{"string", tricky, tricky},
		{"empty string", "", ""},
		{"text of a NULL", `\N`, `\N`},
		{"bytes", []byte("\xff\xfe abc"), "\xff\xfe abc"},
		{"time", time.Date(2026, 10, 7, 12, 34, 56, 123456789, time.FixedZone("x", 7*3600)), time.Date(2026, 10, 7, 5, 34, 56, 123456789, time.UTC)},
		{"date", dbimp.Date{Year: 2299, Month: 12, Day: 31}, dbimp.Date{Year: 2299, Month: 12, Day: 31}},
		{"uuid", uuid.MustParse("61f0c404-5cb3-11e7-907b-a6006ad3dba0"), uuid.MustParse("61f0c404-5cb3-11e7-907b-a6006ad3dba0")},
		{"IPv4", netip.MustParseAddr("192.168.0.1"), netip.MustParseAddr("192.168.0.1")},
		{"IPv6", netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::1")},
		{"IPv4 in IPv6", netip.MustParseAddr("::ffff:1.2.3.4"), netip.MustParseAddr("::ffff:1.2.3.4")},
		{"decimal", d("-12345678901234567890.123456789012345678"), d("-12345678901234567890.123456789012345678")},
		{"decimal of 76 digits", d("123456789012345678901234567890123456.1234567890123456789012345678901234567890"), d("123456789012345678901234567890123456.1234567890123456789012345678901234567890")},
		{"big integer min", big256, big256},
		{"big integer max", big256u, big256u},
		{"strings", []string{"a", tricky, ""}, []any{"a", tricky, ""}},
		{"ints", []int64{1, -2}, []any{int64(1), int64(-2)}},
		{"floats", []float64{1.5, math.Inf(-1)}, []any{1.5, math.Inf(-1)}},
		{"bools", []bool{true, false}, []any{true, false}},
		{"list with NULL", []any{int64(1), nil}, []any{int64(1), nil}},
		{"list of lists", []any{[]string{"a"}, []string{}}, []any{[]any{"a"}, []any{}}},
		{"empty list", []any{}, []any{}},
		{"map", map[string]any{"a": int64(1), "b'c": int64(2)}, map[string]any{"a": int64(1), "b'c": int64(2)}},
		{"nil", nil, nil},
	} {
		got := e.scalar(t, "SELECT ? AS v", tt.in)
		if !equalValue(got, tt.want) {
			t.Errorf("%s: the parameter came back as %#v (%T), want %#v (%T)", tt.name, got, got, tt.want, tt.want)
		}
	}
	// Two parameters of one name are one, and a name that the statement does not
	// use is an error.
	if got := e.scalar(t, "SELECT @a + @a + @b", sql.Named("a", int64(1)), sql.Named("b", int64(10))); got != int64(12) {
		t.Errorf("named parameters gave %#v, want 12", got)
	}
	// The server converts the parameter of one type for a column of another.
	if got := e.scalar(t, "SELECT toInt8(?)", int64(5)); got != int64(5) {
		t.Errorf("a converted parameter gave %#v", got)
	}
	// The types that the server chose are the types of the parameters.
	if got := e.scalar(t, "SELECT toTypeName(?)", int64(5)); got != "Int64" {
		t.Errorf("the type of an int64 is %v, want Int64", got)
	}
	if got := e.scalar(t, "SELECT toTypeName(?)", time.Now()); got != "DateTime64(9, 'UTC')" {
		t.Errorf("the type of a time is %v, want DateTime64(9, 'UTC')", got)
	}
}
