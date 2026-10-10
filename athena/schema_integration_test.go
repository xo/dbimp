package athena_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// TestIntegrationSchema makes tables, views and partitions, and sends the
// operations of a schema that Athena refuses: each refusal is an error of the
// server at the start of the statement, and no table is made. The tables are
// dropped when each test ends.
func TestIntegrationSchema(t *testing.T) {
	db := connect(t)
	// has reports whether SHOW TABLES lists name.
	has := func(t *testing.T, name string) bool {
		t.Helper()
		for _, row := range rowsOf(t, db, "SHOW TABLES") {
			if row[0] == name {
				return true
			}
		}
		return false
	}
	// refused sends the statement, and holds that the server refuses it at the
	// start with HTTP 400, the code MALFORMED_QUERY and no table (recorded: "a
	// primary key on a hive table").
	refused := func(t *testing.T, name, stmt string) {
		t.Helper()
		dropLater(t, db, table(name))
		_, err := db.ExecContext(t.Context(), stmt)
		aerr := refusal(t, err)
		if aerr.HTTPStatus != http.StatusBadRequest {
			t.Errorf("the status is %d, want 400", aerr.HTTPStatus)
		}
		if has(t, table(name)) {
			t.Errorf("the table %s exists after the server refused the statement", table(name))
		}
	}
	t.Run("table", func(t *testing.T) {
		name := iceberg(t, db, "schema_t")
		got := rowsOf(t, db, "SELECT column_name, data_type FROM information_schema.columns WHERE table_name = ? ORDER BY ordinal_position", name)
		want := [][]any{{"id", "integer"}, {"name", "varchar"}}
		if !equalRows(got, want) {
			t.Errorf("the columns are %v, want %v", got, want)
		}
		if !has(t, name) {
			t.Errorf("SHOW TABLES does not list %s", name)
		}
	})
	t.Run("external_table", func(t *testing.T) {
		name := external(t, db, "schema_e")
		var ddl []string
		for _, row := range rowsOf(t, db, "SHOW CREATE TABLE "+name) {
			ddl = append(ddl, fmt.Sprint(row[0]))
		}
		if text := strings.Join(ddl, "\n"); !strings.Contains(text, "CREATE EXTERNAL TABLE") || !strings.Contains(text, "`"+name+"`") {
			t.Errorf("SHOW CREATE TABLE gave %q, want an external table named %s", text, name)
		}
	})
	t.Run("drop_table", func(t *testing.T) {
		name := external(t, db, "schema_d")
		if !has(t, name) {
			t.Fatalf("SHOW TABLES does not list %s before the drop", name)
		}
		exec(t, db, "DROP TABLE "+name)
		if has(t, name) {
			t.Errorf("SHOW TABLES lists %s after the drop", name)
		}
		// A second drop of a table that is gone names it, and DROP TABLE IF EXISTS
		// does not fail.
		if _, err := db.ExecContext(t.Context(), "DROP TABLE "+name); err == nil {
			t.Error("DROP TABLE of a table that is gone gave no error")
		}
		exec(t, db, "DROP TABLE IF EXISTS "+name)
	})
	t.Run("primary_key", func(t *testing.T) {
		n := "schema_pk"
		refused(t, n, "CREATE EXTERNAL TABLE "+table(n)+" (id int, PRIMARY KEY (id)) STORED AS PARQUET LOCATION '"+location(t, n)+"'")
		n = "schema_pk2"
		refused(t, n, "CREATE TABLE "+table(n)+" (id int, PRIMARY KEY (id)) LOCATION '"+location(t, n)+"' TBLPROPERTIES ('table_type'='ICEBERG')")
	})
	t.Run("foreign_key", func(t *testing.T) {
		parent := external(t, db, "schema_fk_parent")
		n := "schema_fk"
		refused(t, n, "CREATE EXTERNAL TABLE "+table(n)+" (id int, FOREIGN KEY (id) REFERENCES "+parent+" (id)) STORED AS PARQUET LOCATION '"+location(t, n)+"'")
	})
	t.Run("index", func(t *testing.T) {
		name := external(t, db, "schema_ix")
		_, err := db.ExecContext(t.Context(), "CREATE INDEX "+table("ix")+" ON "+name+" (id)")
		aerr := refusal(t, err)
		if !strings.Contains(aerr.Message, "Queries of this type are not supported") {
			t.Errorf("the error is %v, want the text of the server for a statement it does not support", aerr)
		}
	})
	t.Run("unique_constraint", func(t *testing.T) {
		n := "schema_uq"
		refused(t, n, "CREATE EXTERNAL TABLE "+table(n)+" (id int UNIQUE) STORED AS PARQUET LOCATION '"+location(t, n)+"'")
	})
	t.Run("default_value", func(t *testing.T) {
		n := "schema_df"
		refused(t, n, "CREATE EXTERNAL TABLE "+table(n)+" (id int DEFAULT 1) STORED AS PARQUET LOCATION '"+location(t, n)+"'")
		// NOT NULL is refused the same way, with the same text (recorded: "a not
		// null column").
		n = "schema_nn"
		refused(t, n, "CREATE EXTERNAL TABLE "+table(n)+" (id int NOT NULL) STORED AS PARQUET LOCATION '"+location(t, n)+"'")
	})
	t.Run("view", func(t *testing.T) {
		name := table("schema_view")
		dropLater(t, db, name)
		exec(t, db, "CREATE OR REPLACE VIEW "+name+" AS SELECT 1 AS a, 'x' AS b")
		if got := rowsOf(t, db, "SELECT a, b FROM "+name); !equalRows(got, [][]any{{int64(1), "x"}}) {
			t.Errorf("the view holds %v, want one row", got)
		}
		exec(t, db, "DROP VIEW "+name)
		if has(t, name) {
			t.Errorf("SHOW TABLES lists the view %s after the drop", name)
		}
	})
	t.Run("partitions", func(t *testing.T) {
		name := table("schema_part")
		dropLater(t, db, name)
		exec(t, db, "CREATE EXTERNAL TABLE "+name+" (id int) PARTITIONED BY (p string) STORED AS PARQUET LOCATION '"+location(t, "schema_part")+"'")
		exec(t, db, "INSERT INTO "+name+" SELECT 1, 'a'")
		exec(t, db, "ALTER TABLE "+name+" ADD IF NOT EXISTS PARTITION (p='b')")
		var got []string
		for _, row := range rowsOf(t, db, "SHOW PARTITIONS "+name) {
			got = append(got, fmt.Sprint(row[0]))
		}
		slices.Sort(got)
		if want := []string{"p=a", "p=b"}; !slices.Equal(got, want) {
			t.Errorf("the partitions are %v, want %v", got, want)
		}
		if got := rowsOf(t, db, "SELECT id, p FROM "+name+" WHERE p = 'a'"); !equalRows(got, [][]any{{int64(1), "a"}}) {
			t.Errorf("the partition a holds %v, want one row", got)
		}
	})
	t.Run("bucketing", func(t *testing.T) {
		name := table("schema_bk")
		dropLater(t, db, name)
		exec(t, db, "CREATE EXTERNAL TABLE "+name+" (id int, name string) CLUSTERED BY (id) INTO 4 BUCKETS STORED AS PARQUET LOCATION '"+location(t, "schema_bk")+"'")
		var ddl []string
		for _, row := range rowsOf(t, db, "SHOW CREATE TABLE "+name) {
			ddl = append(ddl, fmt.Sprint(row[0]))
		}
		if text := strings.Join(ddl, "\n"); !strings.Contains(text, "CLUSTERED BY") || !strings.Contains(text, "4 BUCKETS") {
			t.Errorf("SHOW CREATE TABLE gave %q, want the buckets", text)
		}
	})
}

// equalRows reports whether two lists of rows hold equal values.
func equalRows(a, b [][]any) bool {
	return slices.EqualFunc(a, b, slices.Equal[[]any])
}
