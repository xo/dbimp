package exasol_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/exasol"
)

// These tests hold each entry of testdata/exasol/features.json against a server
// (step 14a). The administrator makes every table. The ordinary user reads it,
// and the server refuses each write with the code 42500.

// w returns the database that writes: the principal itself for the
// administrator, and the administrator for the ordinary user, who gets the
// refusal of the server for the same statement through forbidden.
func (e env) w() *sql.DB {
	if e.p == ordinary {
		return e.adm
	}
	return e.db
}

// ddl runs the statements as the administrator.
func (e env) ddl(t *testing.T, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		mustExec(t, e.adm, s)
	}
}

// refused makes sure that the server refuses query with an error of the code, and
// of the text that holds msg.
func refused(t *testing.T, db *sql.DB, code, msg, query string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query)
	e, ok := errors.AsType[*exasol.Error](err)
	if !ok || e.Code != code || !strings.Contains(e.Message, msg) {
		t.Errorf("%s gave %v, want an *exasol.Error with the code %s and %q", query, err, code, msg)
	}
}

// affected returns the number of rows that the statement changed.
func affected(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	n, err := mustExec(t, db, query, args...).RowsAffected()
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestIntegrationCRUD runs each statement of CRUD on three tables, one of which
// refers to another, and compares each value that a select returns with the
// value written. Exasol has no index to create (the entry index of
// TestIntegrationSchema), so no table has one.
func TestIntegrationCRUD(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		parent, child, other, gen := e.schema+".P", e.schema+".C", e.schema+".O", e.schema+".GEN"
		e.ddl(t,
			"CREATE TABLE "+parent+" (K DECIMAL(9,0) NOT NULL, V VARCHAR(20))",
			"ALTER TABLE "+parent+" ADD CONSTRAINT PK_P PRIMARY KEY (K) ENABLE",
			"CREATE TABLE "+child+" (K DECIMAL(9,0), P DECIMAL(9,0), V VARCHAR(20), CONSTRAINT FK_C FOREIGN KEY (P) REFERENCES "+parent+" (K) ENABLE)",
			"CREATE TABLE "+other+" (K DECIMAL(9,0), N DECIMAL(10,2))",
			"CREATE TABLE "+gen+" (ID DECIMAL(9,0) IDENTITY, X DECIMAL(9,0) DEFAULT 42, T VARCHAR(10) DEFAULT 'none')",
		)
		pairs := func(table string) []string {
			rows, err := e.db.QueryContext(t.Context(), "SELECT K, V FROM "+table+" ORDER BY K")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var out []string
			for rows.Next() {
				var k int64
				var v sql.Null[string]
				if err := rows.Scan(&k, &v); err != nil {
					t.Fatal(err)
				}
				out = append(out, fmt.Sprintf("%d=%s", k, v.V))
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		}
		t.Run("insert", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "INSERT INTO "+parent+" VALUES (9, 'z')")
			}
			if n := affected(t, e.w(), "INSERT INTO "+parent+" VALUES (?, ?), (?, ?), (?, ?)", 1, "a", 2, "b", 3, "c"); n != 3 {
				t.Errorf("the insert affected %d rows, want 3", n)
			}
			if n := affected(t, e.w(), "INSERT INTO "+child+" VALUES (1, 1, 'x'), (2, 2, 'y')"); n != 2 {
				t.Errorf("the insert of the literals affected %d rows, want 2", n)
			}
			affected(t, e.w(), "INSERT INTO "+other+" VALUES (1, 10.25), (2, 20.50)")
		})
		t.Run("select", func(t *testing.T) {
			want(t, "the rows of the parent", pairs(parent), []string{"1=a", "2=b", "3=c"})
			want(t, "the join", read[string](t, e.db, "SELECT C.V FROM "+child+" C JOIN "+parent+" P ON C.P = P.K WHERE P.V = ?", "b"), []string{"y"})
		})
		t.Run("update", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "UPDATE "+parent+" SET V = 'Q'")
			}
			if n := affected(t, e.w(), "UPDATE "+parent+" SET V = ? WHERE K = ?", "B", 2); n != 1 {
				t.Errorf("the update affected %d rows, want 1", n)
			}
			want(t, "the rows of the parent", pairs(parent), []string{"1=a", "2=B", "3=c"})
		})
		t.Run("merge", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "MERGE INTO "+parent+" T USING (SELECT 3 AS K, 'z' AS V) S ON T.K = S.K WHEN MATCHED THEN UPDATE SET T.V = S.V")
			}
			affected(t, e.w(), "MERGE INTO "+parent+" T USING (SELECT 3 AS K, 'C' AS V) S ON T.K = S.K WHEN MATCHED THEN UPDATE SET T.V = S.V WHEN NOT MATCHED THEN INSERT (K, V) VALUES (S.K, S.V)")
			affected(t, e.w(), "MERGE INTO "+parent+" T USING (SELECT 4 AS K, 'd' AS V) S ON T.K = S.K WHEN MATCHED THEN UPDATE SET T.V = S.V WHEN NOT MATCHED THEN INSERT (K, V) VALUES (S.K, S.V)")
			want(t, "the rows of the parent", pairs(parent), []string{"1=a", "2=B", "3=C", "4=d"})
		})
		t.Run("delete", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "DELETE FROM "+parent+" WHERE K = 4")
			}
			if n := affected(t, e.w(), "DELETE FROM "+parent+" WHERE K = ?", 4); n != 1 {
				t.Errorf("the delete affected %d rows, want 1", n)
			}
			if n := affected(t, e.w(), "DELETE FROM "+parent+" WHERE K = ?", 99); n != 0 {
				t.Errorf("the delete of no row affected %d rows, want 0", n)
			}
			want(t, "the rows of the parent", pairs(parent), []string{"1=a", "2=B", "3=C"})
		})
		t.Run("limit_and_offset", func(t *testing.T) {
			want(t, "LIMIT with OFFSET", read[int64](t, e.db, "SELECT K FROM "+parent+" ORDER BY K LIMIT 2 OFFSET 1"), []int64{2, 3})
			want(t, "LIMIT with a comma", read[int64](t, e.db, "SELECT K FROM "+parent+" ORDER BY K LIMIT 1, 1"), []int64{2})
		})
		t.Run("insert_default_values", func(t *testing.T) {
			affected(t, e.w(), "INSERT INTO "+gen+" DEFAULT VALUES")
			affected(t, e.w(), "INSERT INTO "+gen+" DEFAULT VALUES")
			rows, err := e.db.QueryContext(t.Context(), "SELECT ID, X, T FROM "+gen+" ORDER BY ID")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var got [][3]any
			for rows.Next() {
				var r [3]any
				if err := rows.Scan(&r[0], &r[1], &r[2]); err != nil {
					t.Fatal(err)
				}
				got = append(got, r)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			want(t, "the generated rows", got, [][3]any{{int64(1), int64(42), "none"}, {int64(2), int64(42), "none"}})
		})
		t.Run("truncate", func(t *testing.T) {
			if e.p == ordinary {
				forbidden(t, e.db, "TRUNCATE TABLE "+other)
			}
			if n := affected(t, e.w(), "TRUNCATE TABLE "+other); n != 2 {
				t.Errorf("TRUNCATE affected %d rows, want 2", n)
			}
			want(t, "the count after TRUNCATE", read[int64](t, e.db, "SELECT COUNT(*) FROM "+other), []int64{0})
		})
		t.Run("upsert_as_a_keyword", func(t *testing.T) {
			refused(t, e.adm, "42000", "syntax error", "UPSERT INTO "+parent+" VALUES (1, 'x')")
		})
		t.Run("import_from_a_local_file", func(t *testing.T) {
			refused(t, e.adm, "0A000", "IMPORT and EXPORT of local files", "IMPORT INTO "+parent+" FROM LOCAL CSV FILE 'dbimp-import.csv'")
		})
		t.Run("export_to_a_local_file", func(t *testing.T) {
			refused(t, e.adm, "0A000", "IMPORT and EXPORT of local files", "EXPORT "+parent+" INTO LOCAL CSV FILE 'dbimp-export.csv'")
		})
	})
}

// heavy is a statement that runs for many seconds, so a test can end it.
const heavy = "SELECT COUNT(*) FROM (SELECT * FROM EXA_SQL_KEYWORDS A, EXA_SQL_KEYWORDS B, EXA_SQL_KEYWORDS C, EXA_SQL_KEYWORDS D)"

// pin returns one connection of the database, which holds the session of the
// test.
func pin(t *testing.T, db *sql.DB) *sql.Conn {
	t.Helper()
	c, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// TestIntegrationFeatures runs each feature of the server that
// testdata/exasol/features.json names, and each one that the server lacks.
func TestIntegrationFeatures(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		s := e.schema
		t.Run("script", func(t *testing.T) {
			e.ddl(t, "CREATE OR REPLACE LUA SCRIPT "+s+".SCR () RETURNS TABLE AS\nreturn query([[SELECT 1 AS A, 'x' AS B]])\n/")
			if e.p == ordinary {
				forbidden(t, e.db, "EXECUTE SCRIPT "+s+".SCR()")
				return
			}
			var a int64
			var b string
			if err := e.db.QueryRowContext(t.Context(), "EXECUTE SCRIPT "+s+".SCR()").Scan(&a, &b); err != nil || a != 1 || b != "x" {
				t.Errorf("the script gave %d, %q, %v, want 1, x", a, b, err)
			}
		})
		t.Run("udf_script", func(t *testing.T) {
			e.ddl(t, "CREATE OR REPLACE LUA SCALAR SCRIPT "+s+".DOUBLE_IT (X DOUBLE) RETURNS DOUBLE AS\nfunction run(ctx)\n  return ctx.X * 2\nend\n/")
			if e.p == ordinary {
				forbidden(t, e.db, "SELECT "+s+".DOUBLE_IT(21)")
				return
			}
			want(t, "the function", read[float64](t, e.db, "SELECT "+s+".DOUBLE_IT(?) AS R", 21.0), []float64{42})
		})
		t.Run("virtual_schema", func(t *testing.T) {
			refused(t, e.adm, "04000", "Could not find adapter script", "CREATE VIRTUAL SCHEMA "+s+"_VS USING "+s+".NOADAPTER")
		})
		t.Run("connect_by", func(t *testing.T) {
			want(t, "the levels", read[int64](t, e.db, "SELECT LEVEL FROM DUAL CONNECT BY LEVEL <= ? ORDER BY 1", 5), []int64{1, 2, 3, 4, 5})
		})
		t.Run("preferring", func(t *testing.T) {
			want(t, "the preferred row", read[int64](t, e.db, "SELECT * FROM (SELECT 1 AS A UNION ALL SELECT 2) PREFERRING HIGH A"), []int64{2})
		})
		t.Run("qualify", func(t *testing.T) {
			want(t, "the qualified row", read[int64](t, e.db, "SELECT 1 AS A QUALIFY ROW_NUMBER() OVER (ORDER BY A) = 1"), []int64{1})
		})
		t.Run("local_time_zone_type", func(t *testing.T) {
			e.ddl(t, "CREATE TABLE "+s+".LTZ (T TIMESTAMP WITH LOCAL TIME ZONE)", "INSERT INTO "+s+".LTZ VALUES (CONVERT_TZ(TIMESTAMP '2026-10-11 12:34:56', 'UTC', SESSIONTIMEZONE))")
			c1, c2 := pin(t, e.db), pin(t, e.db)
			var inDefault, inTokyo time.Time
			if err := c1.QueryRowContext(t.Context(), "SELECT T FROM "+s+".LTZ").Scan(&inDefault); err != nil {
				t.Fatal(err)
			}
			if _, err := c2.ExecContext(t.Context(), "ALTER SESSION SET TIME_ZONE = 'ASIA/TOKYO'"); err != nil {
				t.Fatal(err)
			}
			if err := c2.QueryRowContext(t.Context(), "SELECT T FROM "+s+".LTZ").Scan(&inTokyo); err != nil {
				t.Fatal(err)
			}
			if !inDefault.Equal(inTokyo) || inTokyo.Location().String() != "Asia/Tokyo" || inTokyo.UTC().Format("15:04:05") != "12:34:56" {
				t.Errorf("the TSL is %v in the zone of the session and %v in Tokyo, want one instant, 12:34:56 in UTC", inDefault, inTokyo)
			}
		})
		t.Run("parallel_connections", func(t *testing.T) {
			r := dialRaw(t, e.dsn, 5, nil)
			ans := r.call(map[string]any{"command": "requestParallelConnections", "numRequestedConnections": 2})
			data, _ := ans["responseData"].(map[string]any)
			if ans["status"] != "ok" || data["numOpenConnections"] != 1.0 {
				t.Errorf("the answer is %v, want ok with numOpenConnections 1", ans)
			}
		})
		t.Run("execute_batch", func(t *testing.T) {
			r := dialRaw(t, e.dsn, 3, nil)
			ans := r.call(map[string]any{"command": "executeBatch", "sqlTexts": []string{"SELECT 1", "INSERT INTO " + s + ".LTZ VALUES (NULL)", "SELECT 2 AS X"}})
			list := results(t, ans)
			if e.p == ordinary {
				if ans["status"] != "error" || errorCode(ans) != "42500" || len(list) != 1 {
					t.Errorf("the answer is %v, want an error 42500 after the first result", ans)
				}
				return
			}
			var types []string
			for _, r := range list {
				m, _ := r.(map[string]any)
				typ, _ := m["resultType"].(string)
				types = append(types, typ)
			}
			want(t, "the result types", types, []string{"resultSet", "rowCount", "resultSet"})
		})
		t.Run("abort_query", func(t *testing.T) {
			c := pin(t, e.db)
			ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
			defer cancel()
			_, err := c.ExecContext(ctx, heavy)
			if x, ok := errors.AsType[*exasol.Error](err); !errors.Is(err, context.DeadlineExceeded) || !ok || x.Code != exasol.CodeAborted {
				t.Errorf("the error is %v, want context.DeadlineExceeded with the code R0003", err)
			}
			// The connection works after the abort, and its answer is its own.
			want(t, "the statement after the abort", read[int64](t, c, "SELECT 7"), []int64{7})
		})
		t.Run("query_timeout", func(t *testing.T) {
			db := openDSN(t, e.dsn+"&querytimeout=1")
			c := pin(t, db)
			_, err := c.ExecContext(t.Context(), heavy)
			if x, ok := errors.AsType[*exasol.Error](err); !ok || x.Code != exasol.CodeTimeout {
				t.Errorf("the error is %v, want an *exasol.Error with the code R0001", err)
			}
			want(t, "the statement after the timeout", read[int64](t, c, "SELECT 8"), []int64{8})
		})
		t.Run("result_set_row_limit", func(t *testing.T) {
			got := read[int64](t, e.db, "SELECT LEVEL FROM DUAL CONNECT BY LEVEL <= 10 ORDER BY 1", exasol.WithParameter("attributes", map[string]any{"resultSetMaxRows": 3}))
			want(t, "the rows", got, []int64{1, 2, 3})
		})
		t.Run("compression", func(t *testing.T) {
			const q = "SELECT LEVEL AS N, 'row ' || LEVEL AS T FROM DUAL CONNECT BY LEVEL <= 3000 ORDER BY 1"
			with := readTexts(t, openDSN(t, e.dsn), q)
			without := readTexts(t, openDSN(t, e.dsn+"&compression=false"), q)
			if len(with) != 3000 {
				t.Fatalf("read %d rows, want 3000", len(with))
			}
			want(t, "the rows with compression and without", with, without)
		})
		t.Run("transactions", func(t *testing.T) {
			testTransactions(t, e)
		})
		t.Run("savepoint", func(t *testing.T) {
			refused(t, e.adm, "0A000", "savepoint", "SAVEPOINT S1")
		})
		t.Run("isolation_level", func(t *testing.T) {
			refused(t, e.adm, "0A000", "isolation level", "SET TRANSACTION ISOLATION LEVEL SERIALIZABLE")
			if tx, err := e.db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable}); !errors.Is(err, dbimp.ErrNotSupported) {
				if err == nil {
					_ = tx.Rollback()
				}
				t.Errorf("BeginTx with an isolation level gave %v, want %v", err, dbimp.ErrNotSupported)
			}
		})
		t.Run("login_with_a_token", func(t *testing.T) {
			dialRaw(t, e.dsn, 3, func(r *rawSession, _ *exasol.Config) {
				// loginToken is the first message in place of login, and the
				// server answers it with the status ok (recorded: "login with a
				// token that is wrong").
				ans := r.call(map[string]any{"command": "loginToken", "protocolVersion": 3})
				if ans["status"] != "ok" {
					t.Errorf("loginToken answered %v, want ok", ans)
				}
				ans = r.call(map[string]any{"accessToken": "not a token", "useCompression": false})
				if ans["status"] != "error" || errorCode(ans) != "08004" {
					t.Errorf("a wrong token answered %v, want an error 08004", ans)
				}
			})
		})
		t.Run("idle_timeout", func(t *testing.T) {
			// The session ends after the idle time with no close frame, so the
			// next statement on it fails, and the connection is dropped (recorded:
			// "lead: a statement after the idle time").
			db := openDSN(t, e.dsn)
			c := pin(t, db)
			if _, err := c.ExecContext(t.Context(), "ALTER SESSION SET IDLE_TIMEOUT = 1"); err != nil {
				t.Fatal(err)
			}
			time.Sleep(3 * time.Second)
			if _, err := c.ExecContext(t.Context(), "SELECT 1"); err == nil {
				t.Error("a statement after the idle time succeeded, want an error")
			}
			_ = c.Close()
			if err := db.PingContext(t.Context()); err != nil {
				t.Errorf("Ping after the idle time: %v, want a new connection", err)
			}
		})
		t.Run("metadata_commands", func(t *testing.T) {
			r := dialRaw(t, e.dsn, 3, nil)
			ans := r.call(map[string]any{"command": "getSchemas", "schemaFilter": s})
			var names []any
			for _, res := range results(t, ans) {
				rs, _ := res.(map[string]any)["resultSet"].(map[string]any)
				data, _ := rs["data"].([]any)
				if len(data) > 0 {
					names, _ = data[0].([]any)
				}
			}
			if !slices.Contains(names, any(s)) {
				t.Errorf("getSchemas gave %v, want the schema %s", ans, s)
			}
		})
		t.Run("prepared_statement", func(t *testing.T) {
			e.ddl(t, "CREATE TABLE "+s+".PS (K DECIMAL(9,0), V VARCHAR(20))")
			if e.p == ordinary {
				_, err := e.db.ExecContext(t.Context(), "INSERT INTO "+s+".PS VALUES (?, ?)", 1, "x")
				if x, ok := errors.AsType[*exasol.Error](err); !ok || x.Code != "42500" {
					t.Errorf("the insert gave %v, want the code 42500", err)
				}
			}
			ins, err := e.adm.PrepareContext(t.Context(), "INSERT INTO "+s+".PS VALUES (?, ?)")
			if err != nil {
				t.Fatal(err)
			}
			defer ins.Close()
			for i, v := range []string{"a", "b", "c"} {
				if _, err := ins.ExecContext(t.Context(), i+1, v); err != nil {
					t.Fatal(err)
				}
			}
			sel, err := e.db.PrepareContext(t.Context(), "SELECT V FROM "+s+".PS WHERE K = ?")
			if err != nil {
				t.Fatal(err)
			}
			defer sel.Close()
			for k, v := range map[int]string{1: "a", 3: "c", 2: "b"} {
				var got string
				if err := sel.QueryRowContext(t.Context(), k).Scan(&got); err != nil || got != v {
					t.Errorf("the statement for %d gave %q, %v, want %q", k, got, err, v)
				}
			}
			if bad, err := e.adm.PrepareContext(t.Context(), "SELEC 1"); err == nil {
				defer bad.Close()
				t.Error("a statement with a syntax error was prepared")
			}
		})
	})
}

// readTexts runs query and returns its second column, as text, for each row.
func readTexts(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n int64
		var v string
		if err := rows.Scan(&n, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// testTransactions holds D209 on the server (recorded: "turn autocommit off",
// "rollback", "DDL in a transaction", "a failed statement in a transaction"). The
// ordinary user begins and ends a transaction, and the server refuses its
// writes.
func testTransactions(t *testing.T, e env) {
	t.Helper()
	s := e.schema
	e.ddl(t, "CREATE TABLE "+s+".TX (K DECIMAL(9,0) NOT NULL)", "ALTER TABLE "+s+".TX ADD CONSTRAINT PK_TX PRIMARY KEY (K) ENABLE")
	count := func(db *sql.Conn) int64 {
		t.Helper()
		return read[int64](t, db, "SELECT COUNT(*) FROM "+s+".TX")[0]
	}
	other := pin(t, e.adm)
	c := pin(t, e.db)
	tx, err := c.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.p == ordinary {
		_, err := tx.ExecContext(t.Context(), "INSERT INTO "+s+".TX VALUES (1)")
		if x, ok := errors.AsType[*exasol.Error](err); !ok || x.Code != "42500" {
			t.Errorf("the insert in the transaction gave %v, want the code 42500", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		want(t, "the statement after the transaction", read[int64](t, c, "SELECT 1"), []int64{1})
		return
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+s+".TX VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if n := count(other); n != 0 {
		t.Errorf("another session sees %d rows before the commit, want 0", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := count(other); n != 1 {
		t.Errorf("another session sees %d rows after the commit, want 1", n)
	}
	// A rollback undoes the rows, and the DDL of the transaction.
	tx, err = c.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"INSERT INTO " + s + ".TX VALUES (2)", "CREATE TABLE " + s + ".TXDDL (A DECIMAL(9,0))"} {
		if _, err := tx.ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n := count(c); n != 1 {
		t.Errorf("%d rows after the rollback, want 1", n)
	}
	want(t, "the table of the rolled back DDL", read[string](t, c, "SELECT TABLE_NAME FROM EXA_ALL_TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = 'TXDDL'", s), nil)
	// A failed statement does not end the transaction.
	tx, err = c.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+s+".TX VALUES (3)"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+s+".TX VALUES (1)"); err == nil {
		t.Error("a duplicate key was inserted in the transaction")
	} else if x, ok := errors.AsType[*exasol.Error](err); !ok || x.Code != "27002" {
		t.Errorf("the duplicate key gave %v, want the code 27002", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := count(other); n != 2 {
		t.Errorf("%d rows after the commit of a transaction with a failed statement, want 2", n)
	}
	// Autocommit is on again: a statement outside a transaction is visible at
	// once.
	if _, err := c.ExecContext(t.Context(), "INSERT INTO "+s+".TX VALUES (9)"); err != nil {
		t.Fatal(err)
	}
	if n := count(other); n != 3 {
		t.Errorf("another session sees %d rows after an insert with autocommit, want 3", n)
	}
}

// TestIntegrationAliases holds the entries of the integer and float aliases of
// testdata/exasol/features.json: the server has no integer type on the wire, so
// each alias arrives as the DECIMAL or the DOUBLE that it stands for, and the Go
// type follows the precision of the column (D206).
func TestIntegrationAliases(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		s := e.schema
		e.ddl(t, "CREATE TABLE "+s+".AL (TINYINT_ TINYINT, SMALLINT_ SMALLINT, INT_ INT, BIGINT_ BIGINT, REAL_ REAL)")
		rows, err := e.db.QueryContext(t.Context(), "SELECT * FROM "+s+".AL")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		types, err := rows.ColumnTypes()
		if err != nil {
			t.Fatal(err)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		type info struct {
			db, scan string
			p, sc    int64
		}
		wantTypes := map[string]info{
			"TINYINT":  {"DECIMAL", "int64", 3, 0},
			"SMALLINT": {"DECIMAL", "int64", 9, 0},
			"INT":      {"DECIMAL", "int64", 18, 0},
			"BIGINT":   {"DECIMAL", "*apd.Decimal", 36, 0},
			"REAL":     {"DOUBLE", "float64", 0, 0},
		}
		for _, ct := range types {
			name := strings.TrimSuffix(ct.Name(), "_")
			t.Run(name, func(t *testing.T) {
				p, sc, _ := ct.DecimalSize()
				got := info{ct.DatabaseTypeName(), ct.ScanType().String(), p, sc}
				if got != wantTypes[name] {
					t.Errorf("the alias %s arrived as %+v, want %+v", name, got, wantTypes[name])
				}
			})
		}
	})
}
