package exasol_test

import (
	"database/sql"
	"testing"

	"github.com/xo/dbimp/exasol"
)

// columnNames returns the names of the columns of the table in the schema, in
// order, from the system table that every user reads for what the user sees.
func columnNames(t *testing.T, db *sql.DB, schema, table string) []string {
	t.Helper()
	return read[string](t, db, "SELECT COLUMN_NAME FROM EXA_ALL_COLUMNS WHERE COLUMN_SCHEMA = ? AND COLUMN_TABLE = ? ORDER BY COLUMN_ORDINAL_POSITION", schema, table)
}

// TestIntegrationSchema runs each operation on a schema that testdata/exasol/features.json
// names, and each one that the server refuses.
func TestIntegrationSchema(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		s := e.schema
		t.Run("schema", func(t *testing.T) {
			want(t, "the schemas that the user sees", read[string](t, e.db, "SELECT SCHEMA_NAME FROM EXA_ALL_SCHEMAS WHERE SCHEMA_NAME = ?", s), []string{s})
			// The option WithSchema opens the schema, which is an attribute of the
			// session (docs/EXASOL.md, Statements).
			want(t, "the current schema", read[string](t, e.db, "SELECT CURRENT_SCHEMA", exasol.WithSchema(s)), []string{s})
			if e.p == ordinary {
				forbidden(t, e.db, "CREATE SCHEMA "+s+"_OTHER")
			}
		})
		t.Run("table", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "CREATE TABLE "+s+".T0 (A DECIMAL(9,0))")
			}
			e.ddl(t, "CREATE TABLE "+s+".T (A DECIMAL(9,0), B VARCHAR(10), C TIMESTAMP)")
			want(t, "the columns", columnNames(t, e.db, s, "T"), []string{"A", "B", "C"})
			want(t, "the tables that the user sees", read[string](t, e.db, "SELECT TABLE_NAME FROM EXA_ALL_TABLES WHERE TABLE_SCHEMA = ?", s), []string{"T"})
		})
		t.Run("alter_table", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "ALTER TABLE "+s+".T ADD COLUMN D VARCHAR(5)")
			}
			e.ddl(t,
				"ALTER TABLE "+s+".T ADD COLUMN D VARCHAR(5)",
				"ALTER TABLE "+s+".T MODIFY (B VARCHAR(40))",
				"ALTER TABLE "+s+".T RENAME COLUMN C TO C2",
				"ALTER TABLE "+s+".T DROP COLUMN A",
			)
			want(t, "the columns", columnNames(t, e.db, s, "T"), []string{"B", "C2", "D"})
			want(t, "the size of B", read[int64](t, e.db, "SELECT COLUMN_MAXSIZE FROM EXA_ALL_COLUMNS WHERE COLUMN_SCHEMA = ? AND COLUMN_TABLE = 'T' AND COLUMN_NAME = 'B'", s), []int64{40})
		})
		t.Run("primary_key", func(t *testing.T) {
			e.ddl(t, "CREATE TABLE "+s+".PK (K DECIMAL(9,0) NOT NULL, V VARCHAR(5))", "ALTER TABLE "+s+".PK ADD CONSTRAINT PK_PK PRIMARY KEY (K) ENABLE")
			e.ddl(t, "INSERT INTO "+s+".PK VALUES (1, 'a')")
			refused(t, e.adm, "27002", "constraint violation - primary key", "INSERT INTO "+s+".PK VALUES (1, 'b')")
			want(t, "the constraint", read[string](t, e.db, "SELECT CONSTRAINT_TYPE FROM EXA_ALL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA = ? AND CONSTRAINT_TABLE = 'PK' AND CONSTRAINT_TYPE = 'PRIMARY KEY'", s), []string{"PRIMARY KEY"})
		})
		t.Run("foreign_key", func(t *testing.T) {
			e.ddl(t, "CREATE TABLE "+s+".CH (K DECIMAL(9,0), P DECIMAL(9,0), CONSTRAINT FK_CH FOREIGN KEY (P) REFERENCES "+s+".PK (K) ENABLE)")
			e.ddl(t, "INSERT INTO "+s+".CH VALUES (1, 1)")
			refused(t, e.adm, "27003", "foreign key", "INSERT INTO "+s+".CH VALUES (2, 99)")
			want(t, "the rows", read[int64](t, e.db, "SELECT COUNT(*) FROM "+s+".CH"), []int64{1})
		})
		t.Run("index", func(t *testing.T) {
			refused(t, e.adm, "42000", "syntax error", "CREATE INDEX IX ON "+s+".PK (V)")
		})
		t.Run("unique_constraint", func(t *testing.T) {
			refused(t, e.adm, "0A000", "UNIQUE", "ALTER TABLE "+s+".PK ADD CONSTRAINT UQ UNIQUE (V)")
		})
		t.Run("check_constraint", func(t *testing.T) {
			refused(t, e.adm, "0A000", "check", "CREATE TABLE "+s+".CHK (A DECIMAL(9,0) CHECK (A > 0))")
		})
		t.Run("view", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "CREATE VIEW "+s+".V0 AS SELECT 1 AS A")
			}
			e.ddl(t, "CREATE VIEW "+s+".V1 AS SELECT K, V FROM "+s+".PK WHERE K > 0")
			want(t, "the view", read[string](t, e.db, "SELECT V FROM "+s+".V1"), []string{"a"})
			want(t, "the views that the user sees", read[string](t, e.db, "SELECT VIEW_NAME FROM EXA_ALL_VIEWS WHERE VIEW_SCHEMA = ?", s), []string{"V1"})
		})
		t.Run("default_value", func(t *testing.T) {
			e.ddl(t, "CREATE TABLE "+s+".DF (ID DECIMAL(9,0) IDENTITY, X DECIMAL(9,0) DEFAULT 42, T VARCHAR(10) DEFAULT 'none', TS TIMESTAMP DEFAULT CURRENT_TIMESTAMP)")
			e.ddl(t, "INSERT INTO "+s+".DF (X) VALUES (7)", "INSERT INTO "+s+".DF (T) VALUES ('given')")
			var got [][2]any
			rows, err := e.db.QueryContext(t.Context(), "SELECT X, T FROM "+s+".DF ORDER BY ID")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var r [2]any
				if err := rows.Scan(&r[0], &r[1]); err != nil {
					t.Fatal(err)
				}
				got = append(got, r)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			want(t, "the rows", got, [][2]any{{int64(7), "none"}, {int64(42), "given"}})
			want(t, "the timestamps are set", read[bool](t, e.db, "SELECT TS IS NOT NULL FROM "+s+".DF"), []bool{true, true})
		})
		t.Run("identity_column", func(t *testing.T) {
			// The server makes the value, so Exec has no last insert id.
			res, err := e.adm.ExecContext(t.Context(), "INSERT INTO "+s+".DF (X) VALUES (8)")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := res.LastInsertId(); err == nil {
				t.Error("LastInsertId gave no error, want the error of D207")
			}
			want(t, "the identity values", read[int64](t, e.db, "SELECT ID FROM "+s+".DF ORDER BY ID"), []int64{1, 2, 3})
		})
		t.Run("comment", func(t *testing.T) {
			e.ddl(t, "COMMENT ON TABLE "+s+".DF IS 'generated'")
			want(t, "the comment", read[string](t, e.db, "SELECT TABLE_COMMENT FROM EXA_ALL_TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'DF'", s), []string{"generated"})
		})
		t.Run("distribution_key", func(t *testing.T) {
			e.ddl(t, "CREATE TABLE "+s+".DIST (A DECIMAL(9,0), B DECIMAL(9,0), DISTRIBUTE BY A, PARTITION BY B)")
			want(t, "the distribution key", read[string](t, e.db, "SELECT COLUMN_NAME FROM EXA_ALL_COLUMNS WHERE COLUMN_SCHEMA = ? AND COLUMN_TABLE = 'DIST' AND COLUMN_IS_DISTRIBUTION_KEY = TRUE", s), []string{"A"})
		})
		t.Run("partition_key", func(t *testing.T) {
			want(t, "the partition key", read[string](t, e.db, "SELECT COLUMN_NAME FROM EXA_ALL_COLUMNS WHERE COLUMN_SCHEMA = ? AND COLUMN_TABLE = 'DIST' AND COLUMN_PARTITION_KEY_ORDINAL_POSITION IS NOT NULL", s), []string{"B"})
		})
	})
}
