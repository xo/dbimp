package spanner_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/spanner"
)

// dec makes a decimal from text.
func dec(t testing.TB, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// refusedBy fails the test unless err is an error of the server with the status
// INVALID_ARGUMENT and the text. It is for an operation that features.json marks no.
func refusedBy(t testing.TB, err error, text string) {
	t.Helper()
	if err == nil {
		t.Errorf("the server did what the survey says that it cannot do: want the refusal INVALID_ARGUMENT %q", text)
		return
	}
	serr, ok := errors.AsType[*spanner.Error](err)
	// The emulator words its refusals in its own way, or sends none, so the test
	// checks the status only (docs/SPANNER.md, "The emulator").
	if !ok || !statusIs(t, err, "INVALID_ARGUMENT") || !isEmulator(t) && !strings.Contains(serr.Message, text) {
		t.Errorf("the refusal is %v, want INVALID_ARGUMENT and the text %q", err, text)
	}
}

// emulatorHTTP is the HTTP status that the emulator writes for a status, when it
// writes the refusal with no body and so with no name.
var emulatorHTTP = map[string]int{
	"ALREADY_EXISTS":      409,
	"FAILED_PRECONDITION": 400,
	"INVALID_ARGUMENT":    400,
	"OUT_OF_RANGE":        400,
}

// statusIs reports whether err is an error of the server with the status. On the
// emulator, an error that has no name has the HTTP status that the name stands for.
func statusIs(t testing.TB, err error, status string) bool {
	t.Helper()
	serr, ok := errors.AsType[*spanner.Error](err)
	switch {
	case !ok:
		return false
	case serr.Status == status:
		return true
	}
	return isEmulator(t) && serr.Status == "" && serr.HTTPStatus == emulatorHTTP[status]
}

// TestIntegrationCRUD inserts, selects, updates and deletes rows in three tables,
// one of which refers to another and one of which has an index that a query uses, and
// compares each value that a select returns with the value that the test wrote (step
// 14a). The subtests run in order, because each one uses the rows that the one before
// it left. The driver sends SQL only, so the operations that are a call of their own,
// a mutation, a read by key, a partitioned DML statement and a batch, are not
// reachable, and their subtests skip with the reason.
func TestIntegrationCRUD(t *testing.T) {
	db := connect(t)
	parent, child, extra := name("crud_parent"), name("crud_child"), name("crud_extra")
	ddl(t, db, "CREATE TABLE "+parent+" (id INT64 NOT NULL, name STRING(100), score FLOAT64) PRIMARY KEY (id)", "DROP TABLE "+parent)
	ddl(t, db, "CREATE TABLE "+child+" (id INT64 NOT NULL, parent_id INT64 NOT NULL, qty NUMERIC, CONSTRAINT "+name("crud_fk")+" FOREIGN KEY (parent_id) REFERENCES "+parent+" (id)) PRIMARY KEY (id)", "DROP TABLE "+child)
	ddl(t, db, "CREATE TABLE "+extra+" (id INT64 NOT NULL, note STRING(MAX)) PRIMARY KEY (id)", "DROP TABLE "+extra)
	idx := name("crud_idx")
	ddl(t, db, "CREATE INDEX "+idx+" ON "+extra+" (note)", "DROP INDEX "+idx)

	t.Run("insert", func(t *testing.T) {
		if n := affected(t, db, "INSERT INTO "+parent+" (id, name, score) VALUES (?, ?, ?)", int64(1), "ada", 1.5); n != 1 {
			t.Errorf("the first insert changed %d rows, want 1", n)
		}
		if n := affected(t, db, "INSERT INTO "+parent+" (id, name, score) VALUES (2, 'bob', 2.5), (3, 'cy', NULL)"); n != 2 {
			t.Errorf("the insert of two rows changed %d rows, want 2", n)
		}
		if n := affected(t, db, "INSERT INTO "+child+" (id, parent_id, qty) VALUES (?, ?, ?)", int64(10), int64(1), dec(t, "12.50")); n != 1 {
			t.Errorf("the insert of a child changed %d rows, want 1", n)
		}
		exec(t, db, "INSERT INTO "+child+" (id, parent_id, qty) VALUES (11, 1, 3.25), (12, 2, 0.01)")
		exec(t, db, "INSERT INTO "+extra+" (id, note) VALUES (1, 'a'), (2, 'b'), (3, NULL)")
		if _, err := exec(t, db, "INSERT INTO "+parent+" (id) VALUES (4)").LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
		}
		// A foreign key that names no parent is refused, and the refusal rolls the
		// insert back.
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+child+" (id, parent_id) VALUES (99, 999)")
		if !statusIs(t, err, "FAILED_PRECONDITION") {
			t.Errorf("a child with no parent gave %v, want FAILED_PRECONDITION", err)
		}
		if n := count(t, db, "SELECT COUNT(*) FROM "+child+" WHERE id = 99"); n != 0 {
			t.Errorf("the refused child is in the table")
		}
	})
	t.Run("select", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT id, name, score FROM "+parent+" WHERE id <= 3 ORDER BY id")
		want := [][]any{{int64(1), "ada", 1.5}, {int64(2), "bob", 2.5}, {int64(3), "cy", nil}}
		if !sameRows(got, want) {
			t.Errorf("the rows are %#v, want %#v", got, want)
		}
		got = rowsOf(t, db, "SELECT c.id, p.name, c.qty FROM "+child+" c JOIN "+parent+" p ON p.id = c.parent_id WHERE c.id = @id", sql.Named("id", int64(10)))
		if qty, ok := got[0][2].(*apd.Decimal); len(got) != 1 || got[0][0] != int64(10) || got[0][1] != "ada" || !ok || qty.Cmp(dec(t, "12.5")) != 0 {
			t.Errorf("the join gave %#v", got)
		}
		if n := count(t, db, "SELECT COUNT(*) FROM "+extra+"@{FORCE_INDEX="+idx+"} WHERE note = @n", sql.Named("n", "b")); n != 1 {
			t.Errorf("the query through the index found %d rows, want 1", n)
		}
		var name sql.Null[string]
		if err := db.QueryRowContext(t.Context(), "SELECT note FROM "+extra+" WHERE id = 3").Scan(&name); err != nil || name.Valid {
			t.Errorf("a NULL scans as %v, %v, want not valid", name, err)
		}
	})
	t.Run("update", func(t *testing.T) {
		if n := affected(t, db, "UPDATE "+parent+" SET name = ?, score = ? WHERE id = ?", "ada l", 9.5, int64(1)); n != 1 {
			t.Errorf("the update changed %d rows, want 1", n)
		}
		if n := affected(t, db, "UPDATE "+parent+" SET score = 0 WHERE id > 1"); n != 3 {
			t.Errorf("the update of three rows changed %d, want 3", n)
		}
		if n := affected(t, db, "UPDATE "+parent+" SET score = 1 WHERE id = 9999"); n != 0 {
			t.Errorf("the update of no row changed %d, want 0", n)
		}
		got := rowsOf(t, db, "SELECT name, score FROM "+parent+" WHERE id IN (1, 2) ORDER BY id")
		want := [][]any{{"ada l", 9.5}, {"bob", 0.0}}
		if !sameRows(got, want) {
			t.Errorf("the rows are %#v, want %#v", got, want)
		}
	})
	t.Run("delete", func(t *testing.T) {
		// A parent that a child refers to cannot go, and the child can.
		_, err := db.ExecContext(t.Context(), "DELETE FROM "+parent+" WHERE id = 1")
		if !statusIs(t, err, "FAILED_PRECONDITION") {
			t.Errorf("a parent with children gave %v, want FAILED_PRECONDITION", err)
		}
		if n := affected(t, db, "DELETE FROM "+child+" WHERE parent_id = ?", int64(1)); n != 2 {
			t.Errorf("the delete of the children changed %d rows, want 2", n)
		}
		if n := affected(t, db, "DELETE FROM "+parent+" WHERE id = ?", int64(1)); n != 1 {
			t.Errorf("the delete of the parent changed %d rows, want 1", n)
		}
		if n := count(t, db, "SELECT COUNT(*) FROM "+parent+" WHERE id = 1"); n != 0 {
			t.Errorf("the deleted row is still there")
		}
		if n := affected(t, db, "DELETE FROM "+extra+" WHERE TRUE"); n != 3 {
			t.Errorf("the delete of the extra table changed %d rows, want 3", n)
		}
	})
	t.Run("then_return", func(t *testing.T) {
		got := rowsOf(t, db, "INSERT INTO "+parent+" (id, name) VALUES (50, 'ret') THEN RETURN id, name")
		if !sameRows(got, [][]any{{int64(50), "ret"}}) {
			t.Errorf("the insert returned %#v", got)
		}
		var id int64
		if err := db.QueryRowContext(t.Context(), "INSERT INTO "+parent+" (id, name) VALUES (51, 'one') THEN RETURN id").Scan(&id); err != nil || id != 51 {
			t.Errorf("QueryRow of an insert gave %d, %v, want 51", id, err)
		}
		if n := count(t, db, "SELECT COUNT(*) FROM "+parent+" WHERE id = 51"); n != 1 {
			t.Errorf("the insert of QueryRow, which the caller closed after its first row, was not kept")
		}
		got = rowsOf(t, db, "UPDATE "+parent+" SET score = 7 WHERE id >= 50 THEN RETURN id, score")
		if len(got) != 2 {
			t.Errorf("the update returned %d rows, want 2", len(got))
		}
		got = rowsOf(t, db, "DELETE FROM "+parent+" WHERE id >= 50 THEN RETURN id")
		if len(got) != 2 {
			t.Errorf("the delete returned %d rows, want 2", len(got))
		}
	})
	t.Run("mutations", func(t *testing.T) {
		t.Skip("a mutation is a request of the commit call, and the driver sends SQL only (D191 item 1). INSERT, UPDATE and DELETE are the same writes as statements.")
	})
	t.Run("insert_or_update_mutation", func(t *testing.T) {
		// The statement INSERT OR UPDATE does what the mutation does.
		if n := affected(t, db, "INSERT OR UPDATE "+parent+" (id, name) VALUES (60, 'first')"); n != 1 {
			t.Errorf("INSERT OR UPDATE of a new row changed %d rows, want 1", n)
		}
		if n := affected(t, db, "INSERT OR UPDATE "+parent+" (id, name) VALUES (60, 'second')"); n != 1 {
			t.Errorf("INSERT OR UPDATE of a row that exists changed %d rows, want 1", n)
		}
		var name string
		if err := db.QueryRowContext(t.Context(), "SELECT name FROM "+parent+" WHERE id = 60").Scan(&name); err != nil || name != "second" {
			t.Errorf("the row is %q, %v, want second", name, err)
		}
		if n := affected(t, db, "INSERT OR IGNORE "+parent+" (id, name) VALUES (60, 'third')"); n != 0 {
			t.Errorf("INSERT OR IGNORE of a row that exists changed %d rows, want 0", n)
		}
		exec(t, db, "DELETE FROM "+parent+" WHERE id = 60")
	})
	t.Run("replace_mutation", func(t *testing.T) {
		t.Skip("a replace is a mutation, and GoogleSQL has no DML statement for it, so the driver cannot send it (D191 item 1).")
	})
	t.Run("partitioned_dml", func(t *testing.T) {
		t.Skip("a partitioned DML statement needs a transaction of the mode partitionedDml with no commit, and the driver begins read-write and read-only transactions only (D191 item 6).")
	})
	t.Run("batch_dml", func(t *testing.T) {
		t.Skip("a batch is the call executeBatchDml, and the driver sends one statement for each request (D191 item 11).")
	})
	t.Run("read_by_key", func(t *testing.T) {
		t.Skip("a read by key is the call read, and the driver sends SQL only (D191 item 1). A query with a key in WHERE reads the same row.")
	})
	t.Run("streaming_read", func(t *testing.T) {
		t.Skip("a streaming read is the call streamingRead, and the driver sends SQL only (D191 item 1). Every query of the driver uses executeStreamingSql.")
	})
}

// TestIntegrationSchema makes each kind of object that features.json names, with
// DDL, reads it back from INFORMATION_SCHEMA, uses it, and drops it. Each subtest
// drops what it makes.
func TestIntegrationSchema(t *testing.T) {
	db := connect(t)
	main := name("sch_main")
	ddl(t, db, "CREATE TABLE "+main+" (id INT64 NOT NULL, s STRING(MAX), n INT64) PRIMARY KEY (id)", "DROP TABLE "+main)
	exec(t, db, "INSERT INTO "+main+" (id, s, n) VALUES (1, 'a', 10), (2, 'b', NULL), (3, 'c', 30)")

	t.Run("table", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT column_name, spanner_type, is_nullable FROM information_schema.columns WHERE table_name = @t ORDER BY ordinal_position", sql.Named("t", main))
		want := [][]any{{"id", "INT64", "NO"}, {"s", "STRING(MAX)", "YES"}, {"n", "INT64", "YES"}}
		if !sameRows(got, want) {
			t.Errorf("the columns are %#v, want %#v", got, want)
		}
	})
	t.Run("primary_key", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT column_name FROM information_schema.index_columns WHERE table_name = @t AND index_name = 'PRIMARY_KEY' ORDER BY ordinal_position", sql.Named("t", main))
		if !sameRows(got, [][]any{{"id"}}) {
			t.Errorf("the primary key is %#v, want id", got)
		}
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+main+" (id) VALUES (1)")
		if !statusIs(t, err, "ALREADY_EXISTS") {
			t.Errorf("a duplicate key gave %v, want ALREADY_EXISTS", err)
		}
	})
	t.Run("interleaved_table", func(t *testing.T) {
		kid := name("sch_kid")
		ddl(t, db, "CREATE TABLE "+kid+" (id INT64 NOT NULL, kid INT64 NOT NULL, v STRING(MAX)) PRIMARY KEY (id, kid), INTERLEAVE IN PARENT "+main+" ON DELETE CASCADE", "DROP TABLE "+kid)
		got := rowsOf(t, db, "SELECT parent_table_name, on_delete_action FROM information_schema.tables WHERE table_name = @t", sql.Named("t", kid))
		if !sameRows(got, [][]any{{main, "CASCADE"}}) {
			t.Errorf("the table is %#v, want a child of %s with CASCADE", got, main)
		}
		exec(t, db, "INSERT INTO "+kid+" (id, kid, v) VALUES (3, 1, 'x')")
		exec(t, db, "DELETE FROM "+main+" WHERE id = 3")
		if n := count(t, db, "SELECT COUNT(*) FROM "+kid); n != 0 {
			t.Errorf("the child rows stayed after the delete of the parent: %d", n)
		}
		exec(t, db, "INSERT INTO "+main+" (id, s, n) VALUES (3, 'c', 30)")
	})
	t.Run("foreign_key", func(t *testing.T) {
		ref := name("sch_ref")
		ddl(t, db, "CREATE TABLE "+ref+" (id INT64 NOT NULL, mid INT64, CONSTRAINT "+name("sch_fk")+" FOREIGN KEY (mid) REFERENCES "+main+" (id)) PRIMARY KEY (id)", "DROP TABLE "+ref)
		exec(t, db, "INSERT INTO "+ref+" (id, mid) VALUES (1, 1)")
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+ref+" (id, mid) VALUES (2, 999)")
		if !statusIs(t, err, "FAILED_PRECONDITION") {
			t.Errorf("a missing parent gave %v, want FAILED_PRECONDITION", err)
		}
		got := rowsOf(t, db, "SELECT constraint_type FROM information_schema.table_constraints WHERE table_name = @t AND constraint_type = 'FOREIGN KEY'", sql.Named("t", ref))
		if len(got) != 1 {
			t.Errorf("the table has %d foreign keys, want 1", len(got))
		}
	})
	t.Run("check_constraint", func(t *testing.T) {
		chk := name("sch_chk")
		ddl(t, db, "CREATE TABLE "+chk+" (id INT64 NOT NULL, qty INT64, CONSTRAINT "+name("sch_pos")+" CHECK (qty > 0)) PRIMARY KEY (id)", "DROP TABLE "+chk)
		exec(t, db, "INSERT INTO "+chk+" (id, qty) VALUES (1, 5)")
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+chk+" (id, qty) VALUES (2, -1)")
		if !statusIs(t, err, "OUT_OF_RANGE") {
			t.Errorf("a row that breaks the check gave %v, want OUT_OF_RANGE", err)
		}
	})
	t.Run("index", func(t *testing.T) {
		idx := name("sch_idx")
		ddl(t, db, "CREATE INDEX "+idx+" ON "+main+" (s DESC)", "DROP INDEX "+idx)
		if n := count(t, db, "SELECT COUNT(*) FROM "+main+"@{FORCE_INDEX="+idx+"} WHERE s = 'a'"); n != 1 {
			t.Errorf("the query through the index found %d rows, want 1", n)
		}
		err := failure(t, db, "SELECT COUNT(*) FROM "+main+"@{FORCE_INDEX="+name("sch_nosuch")+"}")
		refusedBy(t, err, "does not have an index")
	})
	t.Run("unique_index", func(t *testing.T) {
		idx := name("sch_uniq")
		ddl(t, db, "CREATE UNIQUE INDEX "+idx+" ON "+main+" (s)", "DROP INDEX "+idx)
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+main+" (id, s) VALUES (100, 'a')")
		if !statusIs(t, err, "ALREADY_EXISTS") {
			t.Errorf("a duplicate in a unique index gave %v, want ALREADY_EXISTS", err)
		}
	})
	t.Run("null_filtered_index", func(t *testing.T) {
		hostedOnly(t, "the emulator refuses FORCE_INDEX on a null filtered index that it cannot prove to hold the rows")
		idx := name("sch_nf")
		ddl(t, db, "CREATE NULL_FILTERED INDEX "+idx+" ON "+main+" (n)", "DROP INDEX "+idx)
		if n := count(t, db, "SELECT COUNT(*) FROM "+main+"@{FORCE_INDEX="+idx+"} WHERE n IS NOT NULL"); n != 2 {
			t.Errorf("the index holds %d rows with a value, want 2", n)
		}
		got := rowsOf(t, db, "SELECT is_null_filtered FROM information_schema.indexes WHERE index_name = @i", sql.Named("i", idx))
		if !sameRows(got, [][]any{{true}}) {
			t.Errorf("is_null_filtered is %#v, want true", got)
		}
	})
	t.Run("storing_index", func(t *testing.T) {
		idx := name("sch_store")
		ddl(t, db, "CREATE INDEX "+idx+" ON "+main+" (n) STORING (s)", "DROP INDEX "+idx)
		got := rowsOf(t, db, "SELECT s FROM "+main+"@{FORCE_INDEX="+idx+"} WHERE n = 10")
		if !sameRows(got, [][]any{{"a"}}) {
			t.Errorf("the stored column reads %#v, want a", got)
		}
	})
	t.Run("default_value", func(t *testing.T) {
		tbl := name("sch_def")
		ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, d STRING(MAX) DEFAULT ('x')) PRIMARY KEY (id)", "DROP TABLE "+tbl)
		exec(t, db, "INSERT INTO "+tbl+" (id) VALUES (1)")
		exec(t, db, "INSERT INTO "+tbl+" (id, d) VALUES (2, 'y')")
		got := rowsOf(t, db, "SELECT d FROM "+tbl+" ORDER BY id")
		if !sameRows(got, [][]any{{"x"}, {"y"}}) {
			t.Errorf("the defaults are %#v, want x and y", got)
		}
	})
	t.Run("generated_column", func(t *testing.T) {
		tbl := name("sch_gen")
		ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, g INT64 AS (id * 2) STORED) PRIMARY KEY (id)", "DROP TABLE "+tbl)
		exec(t, db, "INSERT INTO "+tbl+" (id) VALUES (21)")
		if n := count(t, db, "SELECT g FROM "+tbl); n != 42 {
			t.Errorf("the generated column is %d, want 42", n)
		}
		_, err := db.ExecContext(t.Context(), "INSERT INTO "+tbl+" (id, g) VALUES (1, 1)")
		if err == nil {
			t.Error("a write to a generated column gave no error")
		}
	})
	t.Run("sequence", func(t *testing.T) {
		seq, tbl := name("sch_seq"), name("sch_seqtab")
		ddl(t, db, "CREATE SEQUENCE "+seq+" OPTIONS (sequence_kind = 'bit_reversed_positive')", "DROP SEQUENCE "+seq)
		ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 DEFAULT (GET_NEXT_SEQUENCE_VALUE(SEQUENCE "+seq+")), v STRING(MAX)) PRIMARY KEY (id)", "DROP TABLE "+tbl)
		var id1, id2 int64
		if err := db.QueryRowContext(t.Context(), "INSERT INTO "+tbl+" (v) VALUES ('a') THEN RETURN id").Scan(&id1); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(t.Context(), "INSERT INTO "+tbl+" (v) VALUES ('b') THEN RETURN id").Scan(&id2); err != nil {
			t.Fatal(err)
		}
		if id1 <= 0 || id2 <= 0 || id1 == id2 {
			t.Errorf("the sequence gave %d and %d, want two different positive numbers", id1, id2)
		}
	})
	t.Run("change_stream", func(t *testing.T) {
		cs := name("sch_cs")
		ddl(t, db, "CREATE CHANGE STREAM "+cs+" FOR "+main, "DROP CHANGE STREAM "+cs)
		got := rowsOf(t, db, "SELECT change_stream_name FROM information_schema.change_streams WHERE change_stream_name = @c", sql.Named("c", cs))
		if !sameRows(got, [][]any{{cs}}) {
			t.Errorf("the change streams are %#v, want %s", got, cs)
		}
	})
	t.Run("alter_table_add_column", func(t *testing.T) {
		ddl(t, db, "ALTER TABLE "+main+" ADD COLUMN extra STRING(MAX)", "ALTER TABLE "+main+" DROP COLUMN extra")
		exec(t, db, "UPDATE "+main+" SET extra = 'e' WHERE id = 1")
		if got := rowsOf(t, db, "SELECT extra FROM "+main+" WHERE id = 1"); !sameRows(got, [][]any{{"e"}}) {
			t.Errorf("the new column reads %#v, want e", got)
		}
	})
	t.Run("alter_table_drop_column", func(t *testing.T) {
		exec(t, db, "ALTER TABLE "+main+" ADD COLUMN gone INT64")
		exec(t, db, "ALTER TABLE "+main+" DROP COLUMN gone")
		err := failure(t, db, "SELECT gone FROM "+main)
		refusedBy(t, err, "Unrecognized name")
	})
	t.Run("drop_table", func(t *testing.T) {
		tbl := name("sch_drop")
		exec(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL) PRIMARY KEY (id)")
		exec(t, db, "DROP TABLE "+tbl)
		err := failure(t, db, "SELECT * FROM "+tbl)
		refusedBy(t, err, "Table not found")
		// A drop of an object that does not exist fails in the operation, with the
		// code 5, and the IF EXISTS form does not (recorded).
		_, err = db.ExecContext(t.Context(), "DROP TABLE "+tbl)
		if serr, ok := errors.AsType[*spanner.Error](err); !ok || serr.Code != 5 || serr.Status != "NOT_FOUND" {
			t.Errorf("a drop of a missing table gave %v, want the code 5", err)
		}
		exec(t, db, "DROP TABLE IF EXISTS "+tbl)
	})
	t.Run("view", func(t *testing.T) {
		v := name("sch_view")
		ddl(t, db, "CREATE VIEW "+v+" SQL SECURITY INVOKER AS SELECT t.id AS id, t.s AS s FROM "+main+" t", "DROP VIEW "+v)
		if n := count(t, db, "SELECT COUNT(*) FROM "+v); n != 3 {
			t.Errorf("the view holds %d rows, want 3", n)
		}
		got := rowsOf(t, db, "SELECT security_type FROM information_schema.views WHERE table_name = @v", sql.Named("v", v))
		if !sameRows(got, [][]any{{"INVOKER"}}) {
			t.Errorf("the view is %#v, want INVOKER", got)
		}
	})
	t.Run("search_index", func(t *testing.T) {
		doc, si := name("sch_doc"), name("sch_si")
		ddl(t, db, "CREATE TABLE "+doc+" (id INT64 NOT NULL, body STRING(MAX), body_tokens TOKENLIST AS (TOKENIZE_FULLTEXT(body)) HIDDEN) PRIMARY KEY (id)", "DROP TABLE "+doc)
		ddl(t, db, "CREATE SEARCH INDEX "+si+" ON "+doc+" (body_tokens)", "DROP SEARCH INDEX "+si)
		exec(t, db, "INSERT INTO "+doc+" (id, body) VALUES (1, 'hello world'), (2, 'goodbye')")
		got := rowsOf(t, db, "SELECT id FROM "+doc+" WHERE SEARCH(body_tokens, 'hello')")
		if !sameRows(got, [][]any{{int64(1)}}) {
			t.Errorf("the search found %#v, want 1", got)
		}
	})
	t.Run("row_deletion_policy", func(t *testing.T) {
		tbl := name("sch_ttl")
		ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, ts TIMESTAMP NOT NULL) PRIMARY KEY (id), ROW DELETION POLICY (OLDER_THAN(ts, INTERVAL 30 DAY))", "DROP TABLE "+tbl)
		got := rowsOf(t, db, "SELECT row_deletion_policy_expression FROM information_schema.tables WHERE table_name = @t", sql.Named("t", tbl))
		if !sameRows(got, [][]any{{"OLDER_THAN(ts, INTERVAL 30 DAY)"}}) {
			t.Errorf("the policy is %#v", got)
		}
	})
	t.Run("named_schema", func(t *testing.T) {
		sch := name("sch_named")
		ddl(t, db, "CREATE SCHEMA "+sch, "DROP SCHEMA "+sch)
		tbl := sch + ".t1"
		ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, s STRING(MAX)) PRIMARY KEY (id)", "DROP TABLE "+tbl)
		exec(t, db, "INSERT INTO "+tbl+" (id, s) VALUES (1, 'x')")
		if got := rowsOf(t, db, "SELECT s FROM "+tbl); !sameRows(got, [][]any{{"x"}}) {
			t.Errorf("the table of the named schema reads %#v, want x", got)
		}
	})
}

// TestIntegrationFeatures uses each feature that features.json names. A feature
// that the driver sets through WithParameter is a member of a request. A feature that
// is a call of its own, which the driver does not send, skips with the reason.
func TestIntegrationFeatures(t *testing.T) {
	db := connect(t)
	main := name("feat_main")
	ddl(t, db, "CREATE TABLE "+main+" (id INT64 NOT NULL, s STRING(MAX), n INT64) PRIMARY KEY (id)", "DROP TABLE "+main)
	exec(t, db, "INSERT INTO "+main+" (id, s, n) VALUES (1, 'a', 10), (2, 'b', 20), (3, 'c', 30)")

	t.Run("named_parameters", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT s FROM "+main+" WHERE id = @id AND n > @min", sql.Named("id", int64(2)), sql.Named("min", int64(5)))
		if !sameRows(got, [][]any{{"b"}}) {
			t.Errorf("the named parameters gave %#v, want b", got)
		}
		got = rowsOf(t, db, "SELECT s FROM "+main+" WHERE id = ? AND n > ?", int64(3), int64(5))
		if !sameRows(got, [][]any{{"c"}}) {
			t.Errorf("the positional arguments, which the driver writes as named parameters, gave %#v, want c", got)
		}
	})
	t.Run("positional_parameters", func(t *testing.T) {
		hostedOnly(t, "the emulator refuses a ? with HTTP 400 and no body, so the test cannot read its text")
		// The driver writes ? as @p1. WithParameter sends a ? as it stands.
		err := failure(t, db, "SELECT 1", spanner.WithParameter("sql", "SELECT ? AS p"), spanner.WithParameter("params", map[string]any{"p": "1"}), spanner.WithParameter("paramTypes", map[string]any{"p": map[string]any{"code": "INT64"}}))
		refusedBy(t, err, "Positional parameters are not supported")
	})
	t.Run("struct_parameter", func(t *testing.T) {
		t.Skip("the driver binds no STRUCT parameter, because D191 item 10 covers the 13 types of docs/SPANNER.md, and a struct is not a column type.")
	})
	t.Run("array_parameter", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT id FROM "+main+" WHERE id IN UNNEST(@ids) ORDER BY id", sql.Named("ids", []int64{1, 3}))
		if !sameRows(got, [][]any{{int64(1)}, {int64(3)}}) {
			t.Errorf("the array parameter gave %#v, want 1 and 3", got)
		}
		got = rowsOf(t, db, "SELECT ARRAY_LENGTH(@e), @n IS NULL", sql.Named("e", []int64{}), sql.Named("n", []string(nil)))
		if len(got) != 1 || got[0][0] != int64(0) {
			t.Errorf("an empty array gave %#v, want a length of 0", got)
		}
	})
	t.Run("array_of_struct", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT ARRAY(SELECT AS STRUCT id, s FROM "+main+" WHERE id <= 2 ORDER BY id) AS a")
		want := [][]any{{[]any{[]any{int64(1), "a"}, []any{int64(2), "b"}}}}
		if !sameRows(got, want) {
			t.Errorf("the array of structs is %#v, want %#v", got, want)
		}
	})
	t.Run("multiplexed_sessions", func(t *testing.T) {
		// Several connections share the multiplexed session of the connector.
		db.SetMaxOpenConns(3)
		defer db.SetMaxOpenConns(0)
		errs := make(chan error, 3)
		for range 3 {
			go func() { errs <- db.PingContext(t.Context()) }()
		}
		for range 3 {
			if err := <-errs; err != nil {
				t.Error(err)
			}
		}
	})
	t.Run("batch_create_sessions", func(t *testing.T) {
		t.Skip("the driver makes one multiplexed session for each connector (D191 item 3), so it never calls batchCreateSessions.")
	})
	t.Run("session_labels", func(t *testing.T) {
		t.Skip("the driver makes its session with no label (D191 item 3), so a caller cannot set one.")
	})
	t.Run("multi_statement_request", func(t *testing.T) {
		hostedOnly(t, "the emulator refuses two statements with HTTP 400 and no body, and the service answers HTTP 501")
		err := failure(t, db, "SELECT 1; SELECT 2")
		serr, ok := errors.AsType[*spanner.Error](err)
		if !ok || serr.HTTPStatus != 501 || !strings.Contains(serr.Message, "single statements") {
			t.Errorf("two statements in one request gave %v, want HTTP 501", err)
		}
	})
	t.Run("read_only_transaction", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, "SELECT COUNT(*) FROM "+main); n != 3 {
			t.Errorf("the count is %d, want 3", n)
		}
		var n1, n2 int64
		if err := tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+main).Scan(&n1); err != nil {
			t.Fatal(err)
		}
		exec(t, db, "INSERT INTO "+main+" (id, s, n) VALUES (4, 'd', 40)")
		if err := tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+main).Scan(&n2); err != nil {
			t.Fatal(err)
		}
		if n1 != 3 || n2 != 3 {
			t.Errorf("the read-only transaction read %d and then %d, want 3 and 3, because it reads one snapshot", n1, n2)
		}
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+main+" (id) VALUES (5)"); err == nil {
			t.Error("a write in a read-only transaction gave no error")
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("the commit of a read-only transaction: %v", err)
		}
		exec(t, db, "DELETE FROM "+main+" WHERE id = 4")
	})
	t.Run("exact_staleness", func(t *testing.T) {
		ctx := spanner.WithOptions(t.Context(), spanner.WithParameter("transaction", map[string]any{
			"singleUse": map[string]any{"readOnly": map[string]any{"exactStaleness": "1s", "returnReadTimestamp": true}},
		}))
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil || n != 1 {
			t.Errorf("a read with exactStaleness gave %d, %v", n, err)
		}
	})
	t.Run("read_timestamp", func(t *testing.T) {
		hostedOnly(t, "the emulator answered a read at the time of the client with a row that a DELETE had removed, and refuses a read at an earlier time with HTTP 400 and no body")
		ts := time.Now().UTC().Format(time.RFC3339Nano)
		ctx := spanner.WithOptions(t.Context(), spanner.WithParameter("transaction", map[string]any{
			"singleUse": map[string]any{"readOnly": map[string]any{"readTimestamp": ts}},
		}))
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+main).Scan(&n); err != nil || n != 3 {
			t.Errorf("a read at %s gave %d, %v, want 3", ts, n, err)
		}
	})
	t.Run("read_write_transaction", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE "+main+" SET n = n + 1 WHERE id = 1"); err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := tx.QueryRowContext(t.Context(), "SELECT n FROM "+main+" WHERE id = 1").Scan(&n); err != nil || n != 11 {
			t.Errorf("the transaction reads %d, %v, want its own write, 11", n, err)
		}
		if outside := count(t, db, "SELECT n FROM "+main+" WHERE id = 1"); outside != 10 {
			t.Errorf("a read outside the transaction gave %d, want 10, because the transaction has not committed", outside)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, "SELECT n FROM "+main+" WHERE id = 1"); n != 11 {
			t.Errorf("after the commit the row is %d, want 11", n)
		}
		exec(t, db, "UPDATE "+main+" SET n = 10 WHERE id = 1")
	})
	t.Run("inline_begin", func(t *testing.T) {
		// The driver begins a transaction with beginTransaction and never inside a
		// statement. WithParameter can still send the member.
		ctx := spanner.WithOptions(t.Context(), spanner.WithParameter("transaction", map[string]any{"begin": map[string]any{"readWrite": map[string]any{}}}))
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil || n != 1 {
			t.Errorf("a statement that begins a transaction gave %d, %v", n, err)
		}
	})
	t.Run("rollback", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+main+" (id, s) VALUES (70, 'r')"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, "SELECT COUNT(*) FROM "+main+" WHERE id = 70"); n != 0 {
			t.Errorf("the rolled back row is there")
		}
	})
	t.Run("aborted_transaction", func(t *testing.T) {
		// A transaction that a conflict wounds gets ABORTED, at a statement or at the
		// commit, and the caller runs it again (recorded: "commit the older
		// transaction"). The conflict does not come on every try, so the test tries
		// a few times.
		aborted := false
		for attempt := 0; attempt < 5 && !aborted; attempt++ {
			older, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			younger, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := younger.ExecContext(t.Context(), "UPDATE "+main+" SET n = n + 1 WHERE id = 2"); err != nil {
				aborted = aborted || errors.Is(err, spanner.ErrAborted)
			}
			if _, err := older.ExecContext(t.Context(), "UPDATE "+main+" SET n = n + 1 WHERE id = 2"); err != nil {
				aborted = aborted || errors.Is(err, spanner.ErrAborted)
			}
			for _, tx := range []*sql.Tx{younger, older} {
				if err := tx.Commit(); err != nil {
					if !errors.Is(err, spanner.ErrAborted) {
						t.Errorf("a commit gave %v, want nil or ErrAborted", err)
					}
					aborted = true
				}
			}
		}
		if !aborted {
			t.Skip("no conflict aborted a transaction in five tries")
		}
		exec(t, db, "UPDATE "+main+" SET n = 20 WHERE id = 2")
	})
	t.Run("commit_statistics", func(t *testing.T) {
		ctx := spanner.WithOptions(t.Context(), spanner.WithParameter("returnCommitStats", true))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE "+main+" SET n = n WHERE id = 1"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("the commit with returnCommitStats: %v", err)
		}
	})
	t.Run("isolation_level", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE "+main+" SET n = n WHERE id = 1"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("the commit of a repeatable read transaction: %v", err)
		}
		if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted}); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("read committed gave %v, want dbimp.ErrNotSupported", err)
		}
	})
	t.Run("precommit_token", func(t *testing.T) {
		// A read-write transaction on a multiplexed session commits with the
		// precommit token of its last statement. A driver that sends none gets an
		// answer with no commit timestamp.
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if _, err := tx.ExecContext(t.Context(), "UPDATE "+main+" SET n = n WHERE id = 1"); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("the commit on the multiplexed session: %v", err)
		}
	})
	t.Run("partitioned_query", func(t *testing.T) {
		t.Skip("partitionQuery is a call of its own, and the driver sends SQL only (D191 item 1).")
	})
	t.Run("partitioned_read", func(t *testing.T) {
		t.Skip("partitionRead is a call of its own, and the driver sends SQL only (D191 item 1).")
	})
	for _, mode := range []struct{ name, mode string }{{"query_mode_plan", "PLAN"}, {"query_mode_profile", "PROFILE"}, {"query_mode_with_stats", "WITH_STATS"}} {
		t.Run(mode.name, func(t *testing.T) {
			got := rowsOf(t, db, "SELECT id, s FROM "+main+" WHERE id = 1", spanner.WithParameter("queryMode", mode.mode))
			if mode.mode == "PLAN" && len(got) != 0 {
				t.Errorf("a plan returned %d rows, want none", len(got))
			}
			if mode.mode != "PLAN" && len(got) != 1 {
				t.Errorf("the mode %s returned %d rows, want 1", mode.mode, len(got))
			}
		})
	}
	t.Run("query_options", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT 1", spanner.WithParameter("queryOptions", map[string]any{"optimizerVersion": "latest"}))
		if len(got) != 1 {
			t.Errorf("a query with queryOptions returned %d rows, want 1", len(got))
		}
	})
	t.Run("request_options", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT 1", spanner.WithParameter("requestOptions", map[string]any{"priority": "PRIORITY_LOW", "requestTag": "dbimp-test"}))
		if len(got) != 1 {
			t.Errorf("a query with requestOptions returned %d rows, want 1", len(got))
		}
	})
	t.Run("information_schema", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT table_name FROM information_schema.tables WHERE table_name = @t", sql.Named("t", main))
		if !sameRows(got, [][]any{{main}}) {
			t.Errorf("the catalog lists %#v, want %s", got, main)
		}
	})
	t.Run("spanner_sys", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT CAST(MAX(version) AS STRING) AS version FROM spanner_sys.supported_optimizer_versions")
		if len(got) != 1 || got[0][0] == nil {
			t.Errorf("the supported optimizer versions are %#v", got)
		}
	})
	t.Run("streaming_result", func(t *testing.T) {
		// The emulator limits GENERATE_ARRAY to 16000 elements.
		streamRows := int64(20000)
		if isEmulator(t) {
			streamRows = 16000
		}
		rows, err := db.QueryContext(t.Context(), fmt.Sprintf("SELECT x, REPEAT('y', 200) AS pad FROM UNNEST(GENERATE_ARRAY(1, %d)) AS x ORDER BY x", streamRows))
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := int64(0)
		for rows.Next() {
			var (
				x   int64
				pad string
			)
			if err := rows.Scan(&x, &pad); err != nil {
				t.Fatal(err)
			}
			if n++; x != n || len(pad) != 200 {
				t.Fatalf("row %d is %d with %d characters, want %d with 200", n, x, len(pad), n)
			}
		}
		if err := rows.Err(); err != nil || n != streamRows {
			t.Errorf("read %d rows, error %v, want %d", n, err, streamRows)
		}
	})
	t.Run("resume_token_in_the_stream", func(t *testing.T) {
		t.Skip("the driver never resumes a stream (D191 item 4), so a result that breaks returns its error, and the resumeToken of a message is skipped.")
	})
	t.Run("gzip_response", func(t *testing.T) {
		// The transport asks for gzip and decompresses it, so a large result
		// reads the same. docs/SPANNER.md records the gzip answer for 2000 rows.
		rows := rowsOf(t, db, "SELECT x, REPEAT('y', 200) AS pad FROM UNNEST(GENERATE_ARRAY(1, 2000)) AS x")
		if len(rows) != 2000 {
			t.Errorf("read %d rows, want 2000", len(rows))
		}
	})
	t.Run("delete_session", func(t *testing.T) {
		t.Skip("a multiplexed session cannot be deleted (recorded: \"deleteSession of the multiplexed session\"), and the driver makes no other.")
	})
	t.Run("ddl_operation", func(t *testing.T) {
		tbl := name("feat_ddl")
		ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL) PRIMARY KEY (id)", "DROP TABLE "+tbl)
		if n := count(t, db, "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = @t", sql.Named("t", tbl)); n != 1 {
			t.Errorf("the table is not there when the DDL statement returns")
		}
	})
	t.Run("ddl_operation_id", func(t *testing.T) {
		// The driver names each operation, so two statements never share an id.
		a, b := name("feat_ida"), name("feat_idb")
		ddl(t, db, "CREATE TABLE "+a+" (id INT64 NOT NULL) PRIMARY KEY (id)", "DROP TABLE "+a)
		ddl(t, db, "CREATE TABLE "+b+" (id INT64 NOT NULL) PRIMARY KEY (id)", "DROP TABLE "+b)
	})
	t.Run("operation_list", func(t *testing.T) {
		t.Skip("listing operations is a call of the admin API, and the driver reads only the operation of its own DDL statement.")
	})
	t.Run("chunked_value", func(t *testing.T) {
		got := rowsOf(t, db, "SELECT ARRAY_TO_STRING(ARRAY(SELECT REPEAT('x', 1000000) FROM UNNEST(GENERATE_ARRAY(1, 3))), '') AS big, CAST(1 AS INT64) AS n")
		if s, ok := got[0][0].(string); len(got) != 1 || !ok || len(s) != 3000000 || got[0][1] != int64(1) {
			t.Errorf("the big string has %d characters, want 3000000", len(s))
		}
		got = rowsOf(t, db, "SELECT FROM_BASE64(ARRAY_TO_STRING(ARRAY(SELECT REPEAT('QUJD', 250000) FROM UNNEST(GENERATE_ARRAY(1, 2))), '')) AS big")
		if b, ok := got[0][0].([]byte); !ok || len(b) != 1500000 || string(b[:6]) != "ABCABC" {
			t.Errorf("the big BYTES value has %d bytes, want 1500000 of ABC", len(b))
		}
	})
	t.Run("statement_hint", func(t *testing.T) {
		got := rowsOf(t, db, "@{USE_ADDITIONAL_PARALLELISM=TRUE} SELECT id FROM "+main+" ORDER BY id")
		if len(got) != 3 {
			t.Errorf("a query with a statement hint returned %d rows, want 3", len(got))
		}
	})
	t.Run("dml_hint", func(t *testing.T) {
		_, err := db.ExecContext(t.Context(), "@{USE_ADDITIONAL_PARALLELISM=TRUE} UPDATE "+main+" SET n = n WHERE id = 1")
		refusedBy(t, err, "Unsupported hint")
	})
	t.Run("select_for_update", func(t *testing.T) {
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var s string
		if err := tx.QueryRowContext(t.Context(), "SELECT s FROM "+main+" WHERE id = 1 FOR UPDATE").Scan(&s); err != nil || s != "a" {
			t.Errorf("SELECT FOR UPDATE gave %q, %v, want a", s, err)
		}
		err = failure(t, db, "SELECT s FROM "+main+" WHERE id = 1 FOR UPDATE")
		refusedBy(t, err, "FOR UPDATE")
	})
	t.Run("read_lock_mode", func(t *testing.T) {
		ctx := spanner.WithOptions(t.Context(), spanner.WithParameter("options", map[string]any{"readWrite": map[string]any{"readLockMode": "OPTIMISTIC"}}))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE "+main+" SET n = n WHERE id = 1"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("the commit with the lock mode OPTIMISTIC: %v", err)
		}
	})
	t.Run("exclude_transaction_from_change_streams", func(t *testing.T) {
		ctx := spanner.WithOptions(t.Context(), spanner.WithParameter("options", map[string]any{"readWrite": map[string]any{}, "excludeTxnFromChangeStreams": true}))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE "+main+" SET n = n WHERE id = 1"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("the commit of a transaction that leaves the change streams: %v", err)
		}
	})
	t.Run("max_commit_delay", func(t *testing.T) {
		ctx := spanner.WithOptions(t.Context(), spanner.WithParameter("maxCommitDelay", "0.05s"))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE "+main+" SET n = n WHERE id = 1"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("the commit with maxCommitDelay: %v", err)
		}
	})
	t.Run("directed_read_options", func(t *testing.T) {
		ctx := spanner.WithOptions(t.Context(),
			spanner.WithParameter("transaction", map[string]any{"singleUse": map[string]any{"readOnly": map[string]any{"strong": true}}}),
			spanner.WithParameter("directedReadOptions", map[string]any{"includeReplicas": map[string]any{"replicaSelections": []any{map[string]any{"type": "READ_WRITE"}}}}),
		)
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil || n != 1 {
			t.Errorf("a read with directedReadOptions gave %d, %v", n, err)
		}
	})
	t.Run("interval_column", func(t *testing.T) {
		hostedOnly(t, "the emulator fails a CREATE TABLE with an INTERVAL column with INTERNAL, and not with the code 12")
		// A column cannot have the type INTERVAL. The call passes, and the operation
		// fails with the code 12.
		tbl := name("feat_iv")
		_, err := db.ExecContext(t.Context(), "CREATE TABLE "+tbl+" (id INT64 NOT NULL, iv INTERVAL) PRIMARY KEY (id)")
		if err == nil {
			_, _ = db.ExecContext(context.WithoutCancel(t.Context()), "DROP TABLE "+tbl)
			t.Fatal("the server made a table with an INTERVAL column, which the survey says it cannot")
		}
		if serr, ok := errors.AsType[*spanner.Error](err); !ok || serr.Code != 12 || !strings.Contains(serr.Message, "INTERVAL") {
			t.Errorf("the refusal is %v, want the code 12 and the text INTERVAL", err)
		}
		// The type exists in an expression.
		got := rowsOf(t, db, "SELECT INTERVAL 1 DAY AS d")
		if !sameRows(got, [][]any{{dbimp.Interval{Days: 1}}}) {
			t.Errorf("an INTERVAL expression is %#v, want one day", got)
		}
	})
}
