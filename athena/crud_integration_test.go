package athena_test

import (
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/xo/dbimp/athena"
)

// iceberg makes an Iceberg table with the columns id and name, in a location of
// this run. Only an Iceberg table takes UPDATE, DELETE and MERGE (recorded: "an
// update of the external table", "an update of the iceberg table"). The table is
// dropped when the test ends.
func iceberg(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	full := table(name)
	dropLater(t, db, full)
	exec(t, db, "DROP TABLE IF EXISTS "+full)
	exec(t, db, "CREATE TABLE "+full+" (id int, name string) LOCATION '"+location(t, name)+"' TBLPROPERTIES ('table_type'='ICEBERG')")
	return full
}

// external makes a Hive external table with the columns id and name, as text
// files in a location of this run, which takes INSERT and SELECT only. Each run
// has its own location, because DROP TABLE leaves the files (recorded: "a select
// from the external table").
func external(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	full := table(name)
	dropLater(t, db, full)
	exec(t, db, "DROP TABLE IF EXISTS "+full)
	exec(t, db, "CREATE EXTERNAL TABLE "+full+" (id int, name string) ROW FORMAT DELIMITED FIELDS TERMINATED BY ',' STORED AS TEXTFILE LOCATION '"+location(t, name)+"'")
	return full
}

// TestIntegrationCRUD inserts rows, selects them, updates them, selects them
// again, deletes them, and selects to see that they are gone, on three tables:
// two of Iceberg and one external table of Hive. Neither has a foreign key or an
// index, because Athena refuses both (see TestIntegrationSchema). Each value that
// a select returns is compared with the value that the test wrote.
func TestIntegrationCRUD(t *testing.T) {
	db := connect(t)
	a, b, c := iceberg(t, db, "crud_a"), iceberg(t, db, "crud_b"), external(t, db, "crud_c")
	want := func(t *testing.T, tbl string, rows [][]any) {
		t.Helper()
		got := rowsOf(t, db, "SELECT id, name FROM "+tbl+" ORDER BY id")
		if len(got) == 0 && len(rows) == 0 {
			return
		}
		if !reflect.DeepEqual(got, rows) {
			t.Errorf("%s holds %v, want %v", tbl, got, rows)
		}
	}
	t.Run("insert", func(t *testing.T) {
		for _, tbl := range []string{a, b, c} {
			res := exec(t, db, "INSERT INTO "+tbl+" VALUES (1,'a'),(2,'b'),(3,'c')")
			if n := affected(t, res); n != 3 {
				t.Errorf("INSERT into %s changed %d rows, want 3", tbl, n)
			}
		}
		// A bound argument takes the same path.
		if n := affected(t, exec(t, db, "INSERT INTO "+a+" VALUES (?, ?)", 4, "d")); n != 1 {
			t.Errorf("INSERT with arguments changed %d rows, want 1", n)
		}
	})
	t.Run("select", func(t *testing.T) {
		want(t, a, [][]any{{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"}, {int64(4), "d"}})
		want(t, c, [][]any{{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"}})
		got := rowsOf(t, db, "SELECT name FROM "+a+" WHERE id = ?", 2)
		if !reflect.DeepEqual(got, [][]any{{"b"}}) {
			t.Errorf("the row with the id 2 is %v, want b", got)
		}
	})
	t.Run("update", func(t *testing.T) {
		if n := affected(t, exec(t, db, "UPDATE "+a+" SET name = 'z' WHERE id < 3")); n != 2 {
			t.Errorf("UPDATE changed %d rows, want 2", n)
		}
		want(t, a, [][]any{{int64(1), "z"}, {int64(2), "z"}, {int64(3), "c"}, {int64(4), "d"}})
		// An external table of Hive refuses it (recorded: "an update of the external
		// table").
		_, err := db.ExecContext(t.Context(), "UPDATE "+c+" SET name = 'z' WHERE id = 1")
		if aerr := refusalOfQuery(t, err); aerr.ErrorType != 1200 {
			t.Errorf("UPDATE of an external table gave %+v, want the type 1200", *aerr)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if n := affected(t, exec(t, db, "DELETE FROM "+a+" WHERE id = 3")); n != 1 {
			t.Errorf("DELETE changed %d rows, want 1", n)
		}
		want(t, a, [][]any{{int64(1), "z"}, {int64(2), "z"}, {int64(4), "d"}})
		if n := affected(t, exec(t, db, "DELETE FROM "+a+" WHERE id >= 1")); n != 3 {
			t.Errorf("DELETE of every row changed %d rows, want 3", n)
		}
		want(t, a, nil)
		_, err := db.ExecContext(t.Context(), "DELETE FROM "+c+" WHERE id = 1")
		_ = refusalOfQuery(t, err)
	})
	t.Run("merge", func(t *testing.T) {
		exec(t, db, "INSERT INTO "+a+" VALUES (1,'a'),(2,'b')")
		merge := "MERGE INTO " + a + " t USING (SELECT 1 AS id, 'm' AS name) s ON t.id = s.id WHEN MATCHED THEN UPDATE SET name = s.name WHEN NOT MATCHED THEN INSERT (id, name) VALUES (s.id, s.name)"
		if n := affected(t, exec(t, db, merge)); n != 1 {
			t.Errorf("MERGE changed %d rows, want 1", n)
		}
		want(t, a, [][]any{{int64(1), "m"}, {int64(2), "b"}})
		// The second table keeps its rows, so the changes above touched one table only.
		want(t, b, [][]any{{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"}})
	})
	t.Run("insert_overwrite", func(t *testing.T) {
		// The parser refuses INSERT OVERWRITE (recorded: "insert overwrite"), so
		// the entry is no.
		_, err := db.ExecContext(t.Context(), "INSERT OVERWRITE INTO "+c+" SELECT 2, 'a'")
		aerr := refusal(t, err)
		if aerr.HTTPStatus != http.StatusBadRequest {
			t.Errorf("the status is %d, want 400", aerr.HTTPStatus)
		}
	})
	t.Run("create_table_as_select", func(t *testing.T) {
		name := table("crud_ctas")
		dropLater(t, db, name)
		// With no external location, the table is in the location that Athena
		// chooses for the workgroup (recorded: "a create table as select with no
		// location").
		res := exec(t, db, "CREATE TABLE "+name+" WITH (format='PARQUET') AS SELECT id, name FROM "+b)
		if n := affected(t, res); n != 3 {
			t.Errorf("CREATE TABLE AS SELECT changed %d rows, want 3", n)
		}
		want(t, name, [][]any{{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"}})
	})
	t.Run("unload", func(t *testing.T) {
		// UNLOAD refuses a directory that exists, so the target is new for each run
		// (recorded: "unload").
		res := exec(t, db, "UNLOAD (SELECT id, name FROM "+b+") TO '"+location(t, "unload")+"' WITH (format='PARQUET')")
		if n := affected(t, res); n != 3 {
			t.Errorf("UNLOAD wrote %d rows, want 3", n)
		}
	})
	t.Run("select_again_after_delete", func(t *testing.T) {
		// Every row is gone after the last delete, and the table is still there.
		exec(t, db, "DELETE FROM "+b+" WHERE id >= 1")
		want(t, b, nil)
	})
}

// refusalOfQuery fails the test unless err is the error of a query that failed,
// and returns it.
func refusalOfQuery(t *testing.T, err error) *athena.Error {
	t.Helper()
	var aerr *athena.Error
	if !errors.As(err, &aerr) || aerr.State != "FAILED" {
		t.Fatalf("the error is %v, want the failure of a query", err)
	}
	return aerr
}
