package spanner_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/xo/dbimp"
)

// TestIntegrationNullParameters binds a NULL for each type in a SELECT and in an
// INSERT into a table of its own (D195 item 2). The driver sends a nil argument with
// no paramType. If the server refuses one, the driver must send STRING for a NULL
// that it cannot type, and this test names the column that failed.
func TestIntegrationNullParameters(t *testing.T) {
	db := connect(t)
	tbl := name("nulls")
	ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, i INT64, f FLOAT64, f32 FLOAT32, n NUMERIC, b BOOL, s STRING(MAX), y BYTES(MAX), d DATE, ts TIMESTAMP, j JSON, u UUID, ai ARRAY<INT64>, as_ ARRAY<STRING(MAX)>, af ARRAY<FLOAT64>, ab ARRAY<BOOL>, ay ARRAY<BYTES(MAX)>, ad ARRAY<DATE>, ats ARRAY<TIMESTAMP>, an ARRAY<NUMERIC>, aj ARRAY<JSON>, au ARRAY<UUID>) PRIMARY KEY (id)", "DROP TABLE "+tbl)
	cols := []string{"i", "f", "f32", "n", "b", "s", "y", "d", "ts", "j", "u", "ai", "as_", "af", "ab", "ay", "ad", "ats", "an", "aj", "au"}
	typed := map[string]any{
		"i": int64(1), "f": 1.5, "f32": float32(1.5), "n": dec(t, "1.5"), "b": true, "s": "x", "y": []byte("x"),
		"d": dbimp.Date{Year: 2024, Month: 1, Day: 2}, "ts": time.Unix(0, 0).UTC(), "j": map[string]any{"a": int64(1)},
		"u": uuid.UUID{1},
	}
	var got any
	if err := db.QueryRowContext(t.Context(), "SELECT @p", sql.Named("p", nil)).Scan(&got); err != nil {
		t.Errorf("SELECT of an untyped NULL: %v", err)
	}
	for i, c := range cols {
		t.Run(c, func(t *testing.T) {
			id := int64(i + 1)
			if _, err := db.ExecContext(t.Context(), "INSERT INTO "+tbl+" (id, "+c+") VALUES (@id, @p)", sql.Named("id", id), sql.Named("p", nil)); err != nil {
				t.Errorf("INSERT of an untyped NULL into %s: %v", c, err)
			}
			if v, ok := typed[c]; ok {
				// A typed value goes in, and a NULL replaces it.
				id += 1000
				exec(t, db, "INSERT INTO "+tbl+" (id, "+c+") VALUES (@id, @p)", sql.Named("id", id), sql.Named("p", v))
				exec(t, db, "UPDATE "+tbl+" SET "+c+" = @p WHERE id = @id", sql.Named("p", nil), sql.Named("id", id))
				if rows := rowsOf(t, db, "SELECT "+c+" FROM "+tbl+" WHERE id = @id", sql.Named("id", id)); len(rows) != 1 || rows[0][0] != nil {
					t.Errorf("the column %s reads %v, want NULL", c, rows)
				}
			}
		})
	}
	t.Run("typed nil slices", func(t *testing.T) {
		for name, v := range map[string]any{
			"int64": []int64(nil), "string": []string(nil), "float64": []float64(nil), "float32": []float32(nil), "bool": []bool(nil),
			"bytes": [][]byte(nil), "date": []dbimp.Date(nil), "time": []time.Time(nil),
		} {
			var isNull bool
			if err := db.QueryRowContext(t.Context(), "SELECT @p IS NULL", sql.Named("p", v)).Scan(&isNull); err != nil || !isNull {
				t.Errorf("a nil %s slice: %v, %v, want a NULL array", name, isNull, err)
			}
		}
	})
}

// TestIntegrationIsolationLevels opens a transaction at each level that the driver
// allows, writes and reads in it, and commits (D195 and D191 item 12). If the service
// refuses REPEATABLE_READ, BeginTx must return dbimp.ErrNotSupported, and the test
// logs that and passes.
func TestIntegrationIsolationLevels(t *testing.T) {
	db := connect(t)
	tbl := name("iso")
	ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, n INT64) PRIMARY KEY (id)", "DROP TABLE "+tbl)
	exec(t, db, "INSERT INTO "+tbl+" (id, n) VALUES (1, 0)")
	for _, level := range []sql.IsolationLevel{sql.LevelDefault, sql.LevelSerializable, sql.LevelRepeatableRead} {
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: level})
		if err != nil {
			if level == sql.LevelRepeatableRead && errors.Is(err, dbimp.ErrNotSupported) {
				t.Logf("the service refuses %s: %v", level, err)
				continue
			}
			t.Errorf("%s: %v", level, err)
			continue
		}
		var n int64
		if err := tx.QueryRowContext(t.Context(), "SELECT n FROM "+tbl+" WHERE id = 1").Scan(&n); err != nil {
			t.Errorf("%s: read: %v", level, err)
		}
		if _, err := tx.ExecContext(t.Context(), "UPDATE "+tbl+" SET n = @n WHERE id = 1", sql.Named("n", n+1)); err != nil {
			t.Errorf("%s: write: %v", level, err)
		}
		if err := tx.Commit(); err != nil {
			t.Errorf("%s: commit: %v", level, err)
		} else {
			t.Logf("the service accepts %s", level)
		}
	}
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted}); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("read committed gave %v, want dbimp.ErrNotSupported", err)
	}
}

// TestIntegrationDMLCount runs a DML statement with a count outside a transaction and
// in one, so the main session can verify that executeStreamingSql returns
// stats.rowCountExact for DML. The recordings sent DML to executeSql only, and the
// documentation of PartialResultSet says that its stats member carries the count of a
// DML statement. If this test fails for want of a count, send DML to executeSql.
func TestIntegrationDMLCount(t *testing.T) {
	db := connect(t)
	tbl := name("dmlcount")
	ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, n INT64) PRIMARY KEY (id)", "DROP TABLE "+tbl)
	if n := affected(t, db, "INSERT INTO "+tbl+" (id, n) VALUES (1, 0), (2, 0), (3, 0)"); n != 3 {
		t.Errorf("INSERT of three rows counted %d, want 3", n)
	}
	if n := affected(t, db, "UPDATE "+tbl+" SET n = 1 WHERE id < 3"); n != 2 {
		t.Errorf("UPDATE of two rows counted %d, want 2", n)
	}
	if n := affected(t, db, "UPDATE "+tbl+" SET n = 1 WHERE id = 99"); n != 0 {
		t.Errorf("UPDATE of no row counted %d, want 0", n)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tx.ExecContext(t.Context(), "DELETE FROM "+tbl+" WHERE id >= 2")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 2 {
		t.Errorf("DELETE in a transaction counted %d, %v, want 2", n, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM "+tbl); n != 1 {
		t.Errorf("the table holds %d rows, want 1", n)
	}
}
