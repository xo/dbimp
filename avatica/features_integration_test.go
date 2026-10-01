package avatica_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/avatica"
	"github.com/xo/dbimp/dbimptest"
)

// These tests hold each entry of testdata/avatica/features.json against a
// server (step 14a). An entry of one flavor skips on the other. The
// administrator makes every table.

// call sends the request body of Avatica to the server of p, for a request
// that the driver never sends, such as databaseProperties, and returns the
// status and the answer.
func call(t *testing.T, p principal, body map[string]any) (int, map[string]any) {
	t.Helper()
	cfg, err := avatica.ParseDSN(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	// A cleanup sends closeConnection after the context of the test ends.
	req, err := http.NewRequestWithContext(context.WithoutCancel(t.Context()), http.MethodPost, scheme+"://"+cfg.Host+":"+strconv.Itoa(cfg.Port)+"/", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("reading the answer %s: %v", out, err)
	}
	return res.StatusCode, m
}

// pairs reads the rows of K and V of tb, in the order of K, as k=v.
func pairs(t *testing.T, db *sql.DB, tb string) string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT K, V FROM "+tb+" ORDER BY K")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k int64
		var v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, strconv.FormatInt(k, 10)+"="+v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(out, ",")
}

// refused runs stmt, and fails the test unless the server refuses it with an
// error that holds want.
func refused(t *testing.T, db *sql.DB, stmt, want string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), stmt); !serverError(err, want) {
		t.Errorf("%s gave %v, want the refusal of the server with %q", stmt, err, want)
	}
}

// TestIntegrationCRUD runs each statement of CRUD on two tables, one of
// which refers to the other, and compares each value that a select returns
// with the value written. Phoenix has UPSERT in place of INSERT and UPDATE,
// and refuses both.
func TestIntegrationCRUD(t *testing.T) {
	db := openAs(t, admin)
	f := flavor(t, db)
	parent, child := table("crud_p"), table("crud_c")
	want := func(t *testing.T, w string) {
		t.Helper()
		if got := pairs(t, db, parent); got != w {
			t.Errorf("%s holds %q, want %q", parent, got, w)
		}
	}
	hsql := func(t *testing.T) {
		t.Helper()
		if f != hsqldb {
			t.Skipf("the server runs %s", f)
		}
	}
	phx := func(t *testing.T) {
		t.Helper()
		if f != phoenix {
			t.Skipf("the server runs %s", f)
		}
	}
	if f == hsqldb {
		exec(t, db, "CREATE TABLE "+parent+" (K INTEGER PRIMARY KEY, V VARCHAR(10))")
		exec(t, db, "CREATE TABLE "+child+" (K INTEGER PRIMARY KEY, P INTEGER REFERENCES "+parent+" (K), V VARCHAR(10))")
		drop(t, db, "TABLE "+child, "TABLE "+parent)
	} else {
		exec(t, db, "CREATE TABLE "+parent+" (K INTEGER NOT NULL PRIMARY KEY, V VARCHAR)")
		drop(t, db, "TABLE "+parent)
	}
	t.Run("insert", func(t *testing.T) {
		hsql(t)
		res := exec(t, db, "INSERT INTO "+parent+" VALUES (1, 'a'), (2, 'b'), (3, 'c')")
		if n, _ := res.RowsAffected(); n != 3 {
			t.Errorf("the insert affected %d rows, want 3", n)
		}
		exec(t, db, "INSERT INTO "+child+" VALUES (1, 1, 'x'), (2, 2, 'y')")
	})
	t.Run("select", func(t *testing.T) {
		hsql(t)
		want(t, "1=a,2=b,3=c")
		if got := read[string](t, db, "SELECT C.V FROM "+child+" C JOIN "+parent+" P ON C.P = P.K WHERE P.V = ?", "b"); !reflect.DeepEqual(got, []string{"y"}) {
			t.Errorf("the join gave %q, want y", got)
		}
	})
	t.Run("update", func(t *testing.T) {
		hsql(t)
		res := exec(t, db, "UPDATE "+parent+" SET V = 'B' WHERE K = 2")
		if n, _ := res.RowsAffected(); n != 1 {
			t.Errorf("the update affected %d rows, want 1", n)
		}
		want(t, "1=a,2=B,3=c")
	})
	t.Run("delete", func(t *testing.T) {
		hsql(t)
		exec(t, db, "DELETE FROM "+parent+" WHERE K = 3")
		want(t, "1=a,2=B")
	})
	t.Run("hsqldb merge", func(t *testing.T) {
		hsql(t)
		exec(t, db, "MERGE INTO "+parent+" USING (VALUES (1, 'x'), (4, 'd')) AS S(K, V) ON "+parent+".K = S.K WHEN MATCHED THEN UPDATE SET V = S.V WHEN NOT MATCHED THEN INSERT VALUES S.K, S.V")
		want(t, "1=x,2=B,4=d")
	})
	t.Run("phoenix upsert", func(t *testing.T) {
		phx(t)
		exec(t, db, "UPSERT INTO "+parent+" VALUES (1, 'a')")
		exec(t, db, "UPSERT INTO "+parent+" VALUES (?, ?)", 2, "b")
		exec(t, db, "UPSERT INTO "+parent+" VALUES (3, 'c')")
		exec(t, db, "UPSERT INTO "+parent+" VALUES (2, 'B')")
	})
	t.Run("phoenix select", func(t *testing.T) {
		phx(t)
		want(t, "1=a,2=B,3=c")
	})
	t.Run("phoenix delete", func(t *testing.T) {
		phx(t)
		exec(t, db, "DELETE FROM "+parent+" WHERE K = 3")
		want(t, "1=a,2=B")
	})
	t.Run("phoenix insert", func(t *testing.T) {
		phx(t)
		refused(t, db, "INSERT INTO "+parent+" VALUES (4, 'd')", "Syntax error")
	})
	t.Run("phoenix update", func(t *testing.T) {
		phx(t)
		refused(t, db, "UPDATE "+parent+" SET V = 'Q' WHERE K = 2", "Syntax error")
	})
}

// TestIntegrationSchema makes each object of a schema that the survey names,
// and shows that the server keeps to it.
func TestIntegrationSchema(t *testing.T) {
	db := openAs(t, admin)
	f := flavor(t, db)
	on := func(t *testing.T, want string) {
		t.Helper()
		if f != want {
			t.Skipf("the server runs %s", f)
		}
	}
	parent, child, view, seq := table("s_parent"), table("s_child"), table("s_view"), table("s_seq")
	// The later subtests use the table, so the test drops it at its end.
	top := t
	t.Run("hsqldb table", func(t *testing.T) {
		on(t, hsqldb)
		exec(t, db, "CREATE TABLE "+parent+" (ID INTEGER PRIMARY KEY, NAME VARCHAR(20) UNIQUE, N INTEGER DEFAULT 7 CHECK (N > 0))")
		drop(top, db, "TABLE "+parent+" CASCADE")
	})
	t.Run("hsqldb primary key", func(t *testing.T) {
		on(t, hsqldb)
		exec(t, db, "INSERT INTO "+parent+" (ID, NAME) VALUES (1, 'a')")
		refused(t, db, "INSERT INTO "+parent+" (ID, NAME) VALUES (1, 'b')", "integrity constraint violation")
	})
	t.Run("hsqldb foreign key", func(t *testing.T) {
		on(t, hsqldb)
		exec(t, db, "CREATE TABLE "+child+" (ID INTEGER PRIMARY KEY, P INTEGER REFERENCES "+parent+" (ID))")
		drop(t, db, "TABLE "+child)
		exec(t, db, "INSERT INTO "+child+" VALUES (1, 1)")
		refused(t, db, "INSERT INTO "+child+" VALUES (2, 99)", "foreign key")
	})
	t.Run("hsqldb index", func(t *testing.T) {
		on(t, hsqldb)
		exec(t, db, "CREATE INDEX "+parent+"_N ON "+parent+" (N)")
	})
	t.Run("hsqldb unique constraint", func(t *testing.T) {
		on(t, hsqldb)
		refused(t, db, "INSERT INTO "+parent+" (ID, NAME) VALUES (2, 'a')", "unique constraint")
	})
	t.Run("hsqldb check constraint", func(t *testing.T) {
		on(t, hsqldb)
		refused(t, db, "INSERT INTO "+parent+" (ID, NAME, N) VALUES (3, 'c', -1)", "check constraint")
	})
	t.Run("hsqldb default value", func(t *testing.T) {
		on(t, hsqldb)
		if got := read[int64](t, db, "SELECT N FROM "+parent+" WHERE ID = 1"); !reflect.DeepEqual(got, []int64{7}) {
			t.Errorf("the default gave %v, want 7", got)
		}
	})
	t.Run("hsqldb view", func(t *testing.T) {
		on(t, hsqldb)
		exec(t, db, "CREATE VIEW "+view+" AS SELECT ID, NAME FROM "+parent)
		drop(t, db, "VIEW "+view)
		if got := read[string](t, db, "SELECT NAME FROM "+view); !reflect.DeepEqual(got, []string{"a"}) {
			t.Errorf("the view gave %q, want a", got)
		}
	})
	t.Run("hsqldb sequence", func(t *testing.T) {
		on(t, hsqldb)
		exec(t, db, "CREATE SEQUENCE "+seq+" START WITH 5")
		drop(t, db, "SEQUENCE "+seq)
		if got := read[int64](t, db, "VALUES (NEXT VALUE FOR "+seq+")"); !reflect.DeepEqual(got, []int64{5}) {
			t.Errorf("the sequence gave %v, want 5", got)
		}
	})
	t.Run("phoenix table", func(t *testing.T) {
		on(t, phoenix)
		exec(t, db, "CREATE TABLE "+parent+" (ID INTEGER NOT NULL PRIMARY KEY, NAME VARCHAR, N INTEGER DEFAULT 7)")
		drop(top, db, "TABLE IF EXISTS "+parent+" CASCADE")
	})
	t.Run("phoenix primary key", func(t *testing.T) {
		on(t, phoenix)
		exec(t, db, "UPSERT INTO "+parent+" (ID, NAME) VALUES (1, 'a')")
		exec(t, db, "UPSERT INTO "+parent+" (ID, NAME) VALUES (1, 'b')")
		if got := read[string](t, db, "SELECT NAME FROM "+parent); !reflect.DeepEqual(got, []string{"b"}) {
			t.Errorf("the table holds %q, want one row of b, because the key is one row", got)
		}
	})
	t.Run("phoenix default value", func(t *testing.T) {
		on(t, phoenix)
		if got := read[int64](t, db, "SELECT N FROM "+parent+" WHERE ID = 1"); !reflect.DeepEqual(got, []int64{7}) {
			t.Errorf("the default gave %v, want 7", got)
		}
	})
	t.Run("phoenix index", func(t *testing.T) {
		on(t, phoenix)
		exec(t, db, "CREATE INDEX "+parent+"_NAME ON "+parent+" (NAME)")
	})
	t.Run("phoenix view", func(t *testing.T) {
		on(t, phoenix)
		exec(t, db, "CREATE VIEW "+view+" AS SELECT * FROM "+parent)
		drop(t, db, "VIEW IF EXISTS "+view)
		if got := read[string](t, db, "SELECT NAME FROM "+view); !reflect.DeepEqual(got, []string{"b"}) {
			t.Errorf("the view gave %q, want b", got)
		}
	})
	t.Run("phoenix sequence", func(t *testing.T) {
		on(t, phoenix)
		exec(t, db, "CREATE SEQUENCE "+seq+" START WITH 5")
		drop(t, db, "SEQUENCE IF EXISTS "+seq)
		if got := read[int64](t, db, "SELECT NEXT VALUE FOR "+seq+" FROM "+parent+" LIMIT 1"); !reflect.DeepEqual(got, []int64{5}) {
			t.Errorf("the sequence gave %v, want 5", got)
		}
	})
	t.Run("phoenix foreign key", func(t *testing.T) {
		on(t, phoenix)
		refused(t, db, "CREATE TABLE "+child+" (ID INTEGER NOT NULL PRIMARY KEY, P INTEGER REFERENCES "+parent+" (ID))", "Syntax error")
	})
	t.Run("phoenix check constraint", func(t *testing.T) {
		on(t, phoenix)
		refused(t, db, "CREATE TABLE "+child+" (ID INTEGER NOT NULL PRIMARY KEY, N INTEGER CHECK (N > 0))", "Syntax error")
	})
}

// TestIntegrationFeatures holds each feature of the survey.
func TestIntegrationFeatures(t *testing.T) {
	db := openAs(t, admin)
	f := flavor(t, db)
	on := func(t *testing.T, want string) {
		t.Helper()
		if f != want {
			t.Skipf("the server runs %s", f)
		}
	}
	many := "SELECT V FROM UNNEST(SEQUENCE_ARRAY(1, 250, 1)) AS T(V)"
	t.Run("frames and fetch", func(t *testing.T) {
		on(t, hsqldb)
		ctx := avatica.WithOptions(t.Context(), avatica.WithFrameSize(100))
		var n int64
		rows, err := db.QueryContext(ctx, many)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err != nil || n != 250 {
			t.Errorf("read %d rows and %v, want 250 in three frames", n, err)
		}
	})
	t.Run("max row count", func(t *testing.T) {
		on(t, hsqldb)
		if got := read[int64](t, db, many, avatica.WithParameter("maxRowCount", 10)); len(got) != 10 {
			t.Errorf("maxRowCount 10 gave %d rows, want 10", len(got))
		}
	})
	t.Run("prepare and execute", func(t *testing.T) {
		var got []int64
		if f == hsqldb {
			got = read[int64](t, db, "VALUES (CAST(? AS BIGINT) + 1)", 41)
		} else {
			got = read[int64](t, db, "SELECT COLUMN_COUNT FROM SYSTEM.\"CATALOG\" WHERE TABLE_NAME = ? LIMIT 1", "CATALOG")
		}
		if len(got) != 1 || f == hsqldb && got[0] != 42 {
			t.Errorf("the statement with an argument gave %v", got)
		}
	})
	t.Run("commit and rollback", func(t *testing.T) {
		tb := table("tx")
		ins := "INSERT INTO " + tb + " VALUES (?)"
		if f == phoenix {
			exec(t, db, "CREATE TABLE "+tb+" (K INTEGER NOT NULL PRIMARY KEY)")
			ins = "UPSERT INTO " + tb + " VALUES (?)"
		} else {
			exec(t, db, "CREATE TABLE "+tb+" (K INTEGER PRIMARY KEY)")
		}
		drop(t, db, "TABLE "+tb)
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), ins, 1); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		tx, err = db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), ins, 2); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if got := read[int64](t, db, "SELECT K FROM "+tb); !reflect.DeepEqual(got, []int64{2}) {
			t.Errorf("after the rollback and the commit, the table holds %v, want 2", got)
		}
		// autoCommit is on again after the transaction (D159).
		exec(t, db, strings.Replace(ins, "?", "3", 1))
		if got := read[int64](t, db, "SELECT COUNT(*) FROM "+tb); !reflect.DeepEqual(got, []int64{2}) {
			t.Errorf("after a write outside the transaction, the table holds %v rows, want 2", got)
		}
	})
	t.Run("read-only connection", func(t *testing.T) {
		on(t, hsqldb)
		tb := table("ro")
		exec(t, db, "CREATE TABLE "+tb+" (K INTEGER PRIMARY KEY)")
		drop(t, db, "TABLE "+tb)
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (1)")
		if !serverError(err, "read-only SQL-transaction") {
			t.Errorf("a write in a read-only transaction gave %v, want the refusal of the server", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		exec(t, db, "INSERT INTO "+tb+" VALUES (1)")
	})
	t.Run("metadata of the database", func(t *testing.T) {
		id := "dbimp-it-" + suffix
		if status, m := call(t, admin, map[string]any{"request": "openConnection", "connectionId": id}); status != http.StatusOK {
			t.Fatalf("openConnection gave %d %v", status, m)
		}
		t.Cleanup(func() { call(t, admin, map[string]any{"request": "closeConnection", "connectionId": id}) })
		status, m := call(t, admin, map[string]any{"request": "databaseProperties", "connectionId": id})
		props, _ := m["map"].(map[string]any)
		if status != http.StatusOK || props["GET_DATABASE_PRODUCT_NAME"] == nil {
			t.Errorf("databaseProperties gave %d %v, want the name of the product", status, m)
		}
	})
	t.Run("phoenix salted table", func(t *testing.T) {
		on(t, phoenix)
		tb := table("salt")
		exec(t, db, "CREATE TABLE "+tb+" (K INTEGER NOT NULL PRIMARY KEY, V VARCHAR) SALT_BUCKETS = 2")
		drop(t, db, "TABLE "+tb)
		exec(t, db, "UPSERT INTO "+tb+" VALUES (1, 'a')")
		exec(t, db, "UPSERT INTO "+tb+" VALUES (2, 'b')")
		if got := pairs(t, db, tb); got != "1=a,2=b" {
			t.Errorf("the salted table holds %q, want 1=a,2=b", got)
		}
	})
	t.Run("phoenix row timestamp", func(t *testing.T) {
		on(t, phoenix)
		tb := table("rt")
		exec(t, db, "CREATE TABLE "+tb+" (T TIMESTAMP NOT NULL, K INTEGER NOT NULL CONSTRAINT PK PRIMARY KEY (T ROW_TIMESTAMP, K))")
		drop(t, db, "TABLE "+tb)
		// An upsert that leaves out T fails in the server with
		// ArrayIndexOutOfBoundsException (measured), so the test names it.
		exec(t, db, "UPSERT INTO "+tb+" (T, K) VALUES (?, 1)", dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: time.October, Day: 1}})
		if got := read[dbimp.LocalDateTime](t, db, "SELECT T FROM "+tb+" WHERE K = 1"); len(got) != 1 || got[0].Date.Year != 2026 {
			t.Errorf("the row timestamp is %v, want 2026-10-01", got)
		}
	})
}

// TestIntegrationRoundTrip stores each type of the type table, and reads it
// back (D155). A type of one flavor skips on the other. A type that the
// server cannot write in JSON fails the whole answer.
func TestIntegrationRoundTrip(t *testing.T) {
	db := openAs(t, admin)
	f := flavor(t, db)
	day := func(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
	long := strings.Repeat("xé", 500)
	for _, tt := range []struct {
		typ, sqlType, flavor string
		values               []dbimptest.Value
		equal                func(got, want any) bool
		// value writes the bound value in the statement, for a type that the
		// driver does not bind, such as an ARRAY.
		value   string
		literal func(v any) (string, error)
	}{
		{typ: "TINYINT", sqlType: "TINYINT", values: []dbimptest.Value{{Name: "null"}, {Name: "smallest", In: int64(-128)}, {Name: "largest", In: int64(127)}}},
		{typ: "SMALLINT", sqlType: "SMALLINT", values: []dbimptest.Value{{Name: "null"}, {Name: "smallest", In: int64(math.MinInt16)}, {Name: "largest", In: int64(math.MaxInt16)}}},
		{typ: "INTEGER", sqlType: "INTEGER", values: []dbimptest.Value{{Name: "null"}, {Name: "smallest", In: int64(math.MinInt32)}, {Name: "largest", In: int64(math.MaxInt32)}}},
		{typ: "BIGINT", sqlType: "BIGINT", values: []dbimptest.Value{{Name: "null"}, {Name: "smallest", In: int64(math.MinInt64)}, {Name: "largest", In: int64(math.MaxInt64)}, {Name: "unsigned", In: uint64(7), Want: int64(7)}}},
		{typ: "DECIMAL", sqlType: "DECIMAL(38,10)", flavor: hsqldb, values: []dbimptest.Value{
			{Name: "null"}, {Name: "every digit", In: dec(t, "1234567890123456789012345678.0123456789")},
			{Name: "negative", In: dec(t, "-0.5")}, {Name: "zero", In: dec(t, "0")},
		}, equal: sameDecimal},
		{typ: "DOUBLE", sqlType: "DOUBLE", values: []dbimptest.Value{
			{Name: "null"}, {Name: "half", In: 0.5}, {Name: "largest", In: math.MaxFloat64}, {Name: "smallest", In: 5e-324}, {Name: "last digit", In: 0.1 + 0.2},
		}},
		{typ: "FLOAT", sqlType: "FLOAT", flavor: phoenix, values: []dbimptest.Value{{Name: "null"}, {Name: "half", In: 0.5}, {Name: "one", In: 1.0}}},
		{typ: "BOOLEAN", sqlType: "BOOLEAN", values: []dbimptest.Value{{Name: "null"}, {Name: "false", In: false}, {Name: "true", In: true}}},
		{typ: "CHAR", sqlType: "CHAR(3)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "full", In: "abc"}, {Name: "short", In: "ab", Want: map[string]any{hsqldb: "ab ", phoenix: "ab"}[f]},
		}},
		{typ: "VARCHAR", sqlType: "VARCHAR(2000)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "unicode", In: "é 日本 🙂"}, {Name: "quotes", In: `say 'hi' "x" \ bye`}, {Name: "long", In: long},
		}},
		// Phoenix reads a NULL in a BINARY as zero bytes of its length
		// (measured).
		{typ: "BINARY", sqlType: "BINARY(2)", flavor: phoenix, values: []dbimptest.Value{{Name: "null", Want: []byte{0, 0}}, {Name: "bytes", In: []byte{0, 0xff}}, {Name: "text", In: []byte("ab")}}},
		{typ: "VARBINARY", sqlType: "VARBINARY(2000)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "bytes", In: []byte{0, 0xff}}, {Name: "long", In: []byte(long)},
		}},
		{typ: "DATE", sqlType: "DATE", values: []dbimptest.Value{
			{Name: "null"}, {Name: "today", In: day(2026, 10, 1)}, {Name: "epoch", In: day(1970, 1, 1)}, {Name: "last", In: day(9999, 12, 31)}, {Name: "before the epoch", In: day(1900, 2, 28)},
		}},
		{typ: "TIME", sqlType: "TIME", values: []dbimptest.Value{
			{Name: "null"}, {Name: "midnight", In: dbimp.LocalTime{}}, {Name: "noon", In: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56}}, {Name: "last", In: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59}},
		}},
		{typ: "TIMESTAMP", sqlType: "TIMESTAMP", values: []dbimptest.Value{
			{Name: "null"},
			{Name: "milliseconds", In: dbimp.LocalDateTime{Date: day(2026, 10, 1), Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}}},
			{Name: "a time in UTC", In: time.Date(2026, 10, 1, 12, 34, 56, 0, time.UTC), Want: dbimp.LocalDateTime{Date: day(2026, 10, 1), Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56}}},
			{Name: "before the epoch", In: dbimp.LocalDateTime{Date: day(1900, 2, 28), Time: dbimp.LocalTime{Hour: 1}}},
		}},
		// The driver binds no ARRAY (D158), so the statement makes the array
		// of the integers from 1 to the bound value.
		{typ: "ARRAY", sqlType: "INTEGER ARRAY", flavor: hsqldb, value: "SEQUENCE_ARRAY(1, CAST(? AS INTEGER), 1)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "one", In: int64(1), Want: []any{int64(1)}}, {Name: "three", In: int64(3), Want: []any{int64(1), int64(2), int64(3)}},
		}, literal: func(v any) (string, error) {
			if v == nil {
				return "NULL", nil
			}
			n, ok := v.(int64)
			if !ok {
				return "", fmt.Errorf("writing the array of %T: %w", v, dbimp.ErrInvalidValue)
			}
			return "SEQUENCE_ARRAY(1, " + strconv.FormatInt(n, 10) + ", 1)", nil
		}},
		{typ: "UUID", sqlType: "UUID", flavor: hsqldb, values: []dbimptest.Value{
			{Name: "null"}, {Name: "uuid", In: uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")}, {Name: "nil", In: uuid.UUID{}},
		}},
		// The driver binds no interval (D158), so the statement casts the
		// text of HSQLDB.
		{typ: "INTERVAL", sqlType: "INTERVAL DAY TO SECOND", flavor: hsqldb, value: "CAST(? AS INTERVAL DAY TO SECOND)", values: []dbimptest.Value{
			{Name: "null"},
			{Name: "day and seconds", In: "1 02:03:04.500000", Want: dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute + 4500*time.Millisecond)}},
			{Name: "negative", In: "-2 00:00:01", Want: dbimp.Interval{Days: -2, Nanoseconds: -int64(time.Second)}},
		}, literal: func(v any) (string, error) {
			if v == nil {
				return "NULL", nil
			}
			s, ok := v.(string)
			if !ok {
				return "", fmt.Errorf("writing the interval of %T: %w", v, dbimp.ErrInvalidValue)
			}
			return "INTERVAL '" + s + "' DAY TO SECOND", nil
		}},
		{typ: "UNSIGNED_INT", sqlType: "UNSIGNED_INT", flavor: phoenix, values: []dbimptest.Value{{Name: "null"}, {Name: "zero", In: int64(0)}, {Name: "largest", In: int64(math.MaxInt32)}}},
		{typ: "UNSIGNED_LONG", sqlType: "UNSIGNED_LONG", flavor: phoenix, values: []dbimptest.Value{{Name: "null"}, {Name: "zero", In: int64(0)}, {Name: "largest", In: int64(math.MaxInt64)}}},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			if tt.flavor != "" && tt.flavor != f {
				t.Skipf("the server runs %s, and %s is a type of %s", f, tt.typ, tt.flavor)
			}
			tb := table("rt_" + tt.typ)
			value := tt.value
			if value == "" {
				value = "?"
			}
			lit := tt.literal
			switch {
			case lit == nil && f == phoenix:
				lit = phoenixLiteral
			case lit == nil:
				lit = literal
			}
			c := dbimptest.RoundTripCase{
				Type:     tt.typ,
				Setup:    []string{"CREATE TABLE " + tb + " (K VARCHAR(40) NOT NULL PRIMARY KEY, V " + tt.sqlType + ")"},
				Teardown: []string{"DROP TABLE " + tb},
				Insert:   "INSERT INTO " + tb + " (K, V) VALUES (?, " + value + ")",
				Literal: func(key string, v any) (string, error) {
					l, err := lit(v)
					if err != nil {
						return "", err
					}
					return "INSERT INTO " + tb + " (K, V) VALUES (" + quote(key) + ", " + l + ")", nil
				},
				Select: "SELECT V FROM " + tb + " WHERE K = ?",
				Update: "UPDATE " + tb + " SET V = " + value + " WHERE K = ?",
				Delete: "DELETE FROM " + tb + " WHERE K = ?",
				Values: tt.values,
				Equal:  tt.equal,
			}
			if f == phoenix {
				// Phoenix writes with UPSERT, which sets the value of a key.
				c.Insert = "UPSERT INTO " + tb + " (K, V) VALUES (?, " + value + ")"
				c.Update = "UPSERT INTO " + tb + " (V, K) VALUES (" + value + ", ?)"
				c.Literal = func(key string, v any) (string, error) {
					l, err := lit(v)
					if err != nil {
						return "", err
					}
					return "UPSERT INTO " + tb + " (K, V) VALUES (" + quote(key) + ", " + l + ")", nil
				}
			}
			dbimptest.RoundTrip(t, db, c)
		})
	}
	// The server fails the whole answer for these types, because its JSON
	// encoder cannot write them (D155).
	for _, tt := range []struct{ typ, query string }{
		{"TIMESTAMP WITH TIME ZONE", "VALUES (TIMESTAMP '2026-10-01 12:34:56+05:30')"},
		{"TIME WITH TIME ZONE", "VALUES (TIME '12:34:56+05:30')"},
		{"CLOB", "VALUES (CAST('x' AS CLOB))"},
		{"BLOB", "VALUES (CAST(X'01' AS BLOB))"},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			if f != hsqldb {
				t.Skipf("the server runs %s", f)
			}
			_, err := db.ExecContext(t.Context(), tt.query)
			if e, ok := errors.AsType[*avatica.Error](err); !ok || e.HTTPStatus != http.StatusInternalServerError {
				t.Errorf("%s gave %v, want the failure of the server", tt.query, err)
			}
		})
	}
}

// sameDecimal compares two decimals by their value, and any other value
// with reflect.DeepEqual.
func sameDecimal(got, want any) bool {
	g, gok := got.(*apd.Decimal)
	w, wok := want.(*apd.Decimal)
	if gok && wok {
		return g.Cmp(w) == 0
	}
	return reflect.DeepEqual(got, want)
}

// literal writes v as a literal of SQL, for the round trip.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		if v {
			return "TRUE", nil
		}
		return "FALSE", nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'E', -1, 64), nil
	case *apd.Decimal:
		return v.Text('f'), nil
	case string:
		return quote(v), nil
	case []byte:
		return "X'" + hex.EncodeToString(v) + "'", nil
	case uuid.UUID:
		return "CAST('" + v.String() + "' AS UUID)", nil
	case dbimp.Date:
		return "DATE '" + v.String() + "'", nil
	case dbimp.LocalTime:
		return "TIME '" + v.String() + "'", nil
	case dbimp.LocalDateTime:
		return "TIMESTAMP '" + strings.Replace(v.String(), "T", " ", 1) + "'", nil
	case time.Time:
		return "TIMESTAMP '" + v.UTC().Format("2006-01-02 15:04:05.999999999") + "'", nil
	}
	return "", fmt.Errorf("writing a literal of %T: %w", v, dbimp.ErrNotSupported)
}

// phoenixLiteral writes v as a literal of Phoenix, which has no literal of
// bytes, and reads a time of day with a date (measured).
func phoenixLiteral(v any) (string, error) {
	switch v := v.(type) {
	case []byte:
		return "", fmt.Errorf("writing a literal of bytes: Phoenix has none: %w", dbimp.ErrNotSupported)
	case dbimp.LocalTime:
		return "TO_TIME('1970-01-01 " + v.String() + "')", nil
	}
	return literal(v)
}

// quote returns s in single quotes, with each ' doubled.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
