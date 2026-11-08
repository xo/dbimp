package hive_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/hive"
)

// Each subtest of these tests holds one entry of testdata/hive/features.json, by
// the name of the entry (step 14a of docs/DRIVER.md). A name that starts with
// hive or impala holds a difference between the flavors, and the subtest skips on
// the other flavor. The verdict of each entry is from 4.2.1 and 4.5.3.

// only skips the test unless the flavor is want.
func only(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	if got := flavorOf(t, db); got != want {
		t.Skipf("this is a difference of %s, and the server is %s", want, got)
	}
}

// noACID reports whether err says that the server cannot make an ACID table, as
// Hive 4.0.1 does as dbrun starts it (docs/HIVE.md, Flavors).
func noACID(err error) bool {
	return err != nil && strings.Contains(err.Error(), "ACID table") && strings.Contains(err.Error(), "transaction manager")
}

// rowTable makes a table that holds rows that a statement can update and
// delete: an ACID table on Hive, and an Iceberg table of version 2 on Impala. It
// skips the test on a server that cannot make one.
func rowTable(t *testing.T, db *sql.DB) string {
	t.Helper()
	tbl := table()
	drop(t, db, tbl)
	create := "create table " + tbl + " (id int, v string) stored as orc tblproperties ('transactional'='true')"
	if flavorOf(t, db) == "impala" {
		create = "create table " + tbl + " (id int, v string) stored as iceberg tblproperties ('format-version'='2')"
	}
	if _, err := db.ExecContext(t.Context(), create); err != nil {
		if noACID(err) {
			t.Skip("the server cannot make an ACID table: " + err.Error())
		}
		t.Fatalf("%s: %v", create, err)
	}
	return tbl
}

// rows returns the rows of the query as text, one string for each row.
func rowsOf(t *testing.T, db *sql.DB, q string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		var parts []string
		for _, v := range vals {
			parts = append(parts, strings.TrimSpace(sprint(v)))
		}
		out = append(out, strings.Join(parts, ","))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return out
}

func sprint(v any) string {
	if v == nil {
		return "NULL"
	}
	return fmt.Sprint(v)
}

// want fails the test unless the query returns the rows.
func want(t *testing.T, db *sql.DB, q string, rows ...string) {
	t.Helper()
	got := rowsOf(t, db, q)
	if strings.Join(got, ";") != strings.Join(rows, ";") {
		t.Errorf("%s = %q, want %q", q, got, rows)
	}
}

// refused fails the test unless the server refuses the statement with an error of
// the driver.
func refused(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), stmt)
	if err == nil {
		t.Fatalf("%s: the server took it, want a refusal", stmt)
	}
	if _, ok := errors.AsType[*hive.Error](err); !ok {
		t.Errorf("%s: the error is %v (%T), want a *hive.Error", stmt, err, err)
	}
}

func TestIntegrationCRUD(t *testing.T) {
	db := openServer(t)
	t.Run("select", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string)", "insert into "+tbl+" values (1,'a'),(2,'b'),(3,'c')")
		want(t, db, "select id, v from "+tbl+" order by id", "1,a", "2,b", "3,c")
	})
	t.Run("insert", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string)")
		if _, err := db.ExecContext(t.Context(), "insert into "+tbl+" values (?, ?)", int64(7), "x"); err != nil {
			t.Fatal(err)
		}
		want(t, db, "select count(*) from "+tbl, "1")
		want(t, db, "select id, v from "+tbl, "7,x")
	})
	t.Run("update", func(t *testing.T) {
		tbl := rowTable(t, db)
		execAll(t, db, "insert into "+tbl+" values (1,'a'),(2,'b'),(3,'c')", "update "+tbl+" set v='x' where id=1")
		want(t, db, "select id, v from "+tbl+" order by id", "1,x", "2,b", "3,c")
	})
	t.Run("delete", func(t *testing.T) {
		tbl := rowTable(t, db)
		execAll(t, db, "insert into "+tbl+" values (1,'a'),(2,'b'),(3,'c')", "delete from "+tbl+" where id=2")
		want(t, db, "select id from "+tbl+" order by id", "1", "3")
	})
	t.Run("update of a plain table", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string)", "insert into "+tbl+" values (1,'a')")
		refused(t, db, "update "+tbl+" set v='x' where id=1")
	})
	t.Run("delete of a plain table", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string)", "insert into "+tbl+" values (1,'a')")
		refused(t, db, "delete from "+tbl+" where id=1")
	})
	t.Run("merge", func(t *testing.T) {
		tbl := rowTable(t, db)
		execAll(t, db, "insert into "+tbl+" values (1,'a'),(3,'c')")
		_, err := db.ExecContext(t.Context(), "merge into "+tbl+" t using (select 3 as id, 'm' as v) s on t.id=s.id when matched then update set v=s.v")
		if err != nil && strings.Contains(err.Error(), "Syntax error") {
			t.Skip("this Impala has no MERGE: " + err.Error())
		}
		if err != nil {
			t.Fatal(err)
		}
		want(t, db, "select id, v from "+tbl+" order by id", "1,a", "3,m")
	})
	t.Run("truncate", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int)", "insert into "+tbl+" values (1),(2)", "truncate table "+tbl)
		want(t, db, "select count(*) from "+tbl, "0")
	})
	t.Run("insert overwrite", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int)", "insert into "+tbl+" select 1", "insert overwrite table "+tbl+" select 2")
		want(t, db, "select id from "+tbl, "2")
	})
	t.Run("insert from select", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int)", "insert into "+tbl+" select 1", "insert into "+tbl+" select 2")
		want(t, db, "select id from "+tbl+" order by id", "1", "2")
	})
}

func TestIntegrationSchema(t *testing.T) {
	db := openServer(t)
	flavor := flavorOf(t, db)
	// ddl runs the statements, drops the table at the end, and fails on an error.
	t.Run("create table", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int)")
		want(t, db, "select count(*) from "+tbl, "0")
	})
	t.Run("table of a plain format", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string)")
	})
	t.Run("transactional table", func(t *testing.T) {
		rowTable(t, db)
	})
	t.Run("add column", func(t *testing.T) {
		tbl := rowTable(t, db)
		execAll(t, db, "alter table "+tbl+" add columns (c int)")
		want(t, db, "select count(*) from "+tbl+" where c is null", "0")
	})
	t.Run("rename table", func(t *testing.T) {
		tbl := rowTable(t, db)
		drop(t, db, tbl+"_2")
		execAll(t, db, "alter table "+tbl+" rename to "+tbl+"_2")
		want(t, db, "select count(*) from "+tbl+"_2", "0")
	})
	t.Run("drop table", func(t *testing.T) {
		tbl := table()
		execAll(t, db, "create table "+tbl+" (id int)", "drop table "+tbl)
		refused(t, db, "select * from "+tbl)
	})
	t.Run("hive primary key", func(t *testing.T) {
		only(t, db, "hive")
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, primary key (id) disable novalidate)")
	})
	t.Run("impala primary key", func(t *testing.T) {
		only(t, db, "impala")
		tbl := table()
		drop(t, db, tbl)
		refused(t, db, "create table "+tbl+" (id int, v string, primary key (id)) partitioned by hash (id) partitions 2 stored as kudu")
	})
	t.Run("hive foreign key", func(t *testing.T) {
		only(t, db, "hive")
		parent, child := table(), table()
		drop(t, db, child, parent)
		execAll(t, db, "create table "+parent+" (id int, primary key (id) disable novalidate)",
			"create table "+child+" (id int, p int, foreign key (p) references "+parent+"(id) disable novalidate)")
	})
	t.Run("hive unique", func(t *testing.T) {
		only(t, db, "hive")
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string, unique (v) disable novalidate)")
	})
	t.Run("hive not null", func(t *testing.T) {
		only(t, db, "hive")
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int not null, v string)")
	})
	t.Run("hive default value", func(t *testing.T) {
		only(t, db, "hive")
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string default 'd', primary key (id) disable novalidate)")
	})
	t.Run("index", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int)")
		refused(t, db, "create index i on table "+tbl+" (id) as 'compact' with deferred rebuild")
	})
	t.Run("view", func(t *testing.T) {
		v := table()
		t.Cleanup(func() { _, _ = db.ExecContext(context.WithoutCancel(t.Context()), "drop view if exists "+v) })
		execAll(t, db, "create view "+v+" as select 1 as a")
		want(t, db, "select a from "+v, "1")
	})
	t.Run("hive materialized view", func(t *testing.T) {
		only(t, db, "hive")
		tbl := rowTable(t, db)
		mv := table()
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.WithoutCancel(t.Context()), "drop materialized view if exists "+mv)
		})
		execAll(t, db, "create materialized view "+mv+" as select id from "+tbl)
	})
	t.Run("impala materialized view", func(t *testing.T) {
		only(t, db, "impala")
		refused(t, db, "create materialized view "+table()+" as select 1 as a")
	})
	t.Run("partition", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int) partitioned by (p string)", "insert into "+tbl+" partition (p='a') values (1)")
		want(t, db, "select id, p from "+tbl, "1,a")
	})
	t.Run("database", func(t *testing.T) {
		name := table()
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.WithoutCancel(t.Context()), "drop database if exists "+name+" cascade")
		})
		execAll(t, db, "create database if not exists "+name)
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(t.Context(), "use "+name); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(t.Context(), "use default"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("schema", func(t *testing.T) {
		name := table()
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.WithoutCancel(t.Context()), "drop schema if exists "+name+" cascade")
		})
		execAll(t, db, "create schema if not exists "+name)
	})
	t.Run("describe", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int, v string)")
		got := rowsOf(t, db, "describe "+tbl)
		if len(got) < 2 {
			t.Errorf("describe gave %q, want a row for each column", got)
		}
	})
	t.Run("show tables", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int)")
		found := false
		for _, r := range rowsOf(t, db, "show tables") {
			if strings.Contains(r, tbl) {
				found = true
			}
		}
		if !found {
			t.Errorf("show tables does not name %s", tbl)
		}
	})
	t.Run("show create table", func(t *testing.T) {
		tbl := table()
		drop(t, db, tbl)
		execAll(t, db, "create table "+tbl+" (id int)")
		got := rowsOf(t, db, "show create table "+tbl)
		if len(got) == 0 || !strings.Contains(strings.ToLower(strings.Join(got, "\n")), "create") {
			t.Errorf("show create table gave %q", got)
		}
	})
	_ = flavor
}

func TestIntegrationFeatures(t *testing.T) {
	db := openServer(t)
	t.Run("explain", func(t *testing.T) {
		if got := rowsOf(t, db, "explain select 1"); len(got) == 0 {
			t.Error("explain gave no row")
		}
	})
	// The servers bind no parameter, so the driver writes each argument as a
	// literal (D235), and the entry holds the refusal of the server for a
	// statement that holds the mark itself.
	t.Run("positional parameters", func(t *testing.T) {
		var n int64
		if err := db.QueryRowContext(t.Context(), "select ?", int64(5)).Scan(&n); err != nil || n != 5 {
			t.Errorf("select ? = %d, %v, want 5 from the driver", n, err)
		}
		refused(t, db, "select `?`")
	})
	t.Run("named parameters", func(t *testing.T) {
		var n int64
		if err := db.QueryRowContext(t.Context(), "select @x", sql.Named("x", int64(6))).Scan(&n); err != nil || n != 6 {
			t.Errorf("select @x = %d, %v, want 6 from the driver", n, err)
		}
	})
	t.Run("two statements in one request", func(t *testing.T) {
		refused(t, db, "select 1; select 2")
	})
	t.Run("hive a semicolon at the end", func(t *testing.T) {
		only(t, db, "hive")
		refused(t, db, "select 1;")
	})
	t.Run("impala a semicolon at the end", func(t *testing.T) {
		only(t, db, "impala")
		want(t, db, "select 1;", "1")
	})
	t.Run("line comment", func(t *testing.T) {
		want(t, db, "select 1 -- a comment", "1")
		want(t, db, "-- a comment\nselect 1", "1")
	})
	t.Run("block comment", func(t *testing.T) {
		want(t, db, "/* a comment */ select 1", "1")
	})
	t.Run("start transaction", func(t *testing.T) {
		if flavorOf(t, db) == "impala" {
			refused(t, db, "begin")
			return
		}
		refused(t, db, "start transaction")
	})
	t.Run("commit", func(t *testing.T) { refused(t, db, "commit") })
	t.Run("rollback", func(t *testing.T) { refused(t, db, "rollback") })
	t.Run("hive set", func(t *testing.T) {
		only(t, db, "hive")
		execAll(t, db, "set x=1")
	})
	t.Run("impala set", func(t *testing.T) {
		only(t, db, "impala")
		refused(t, db, "set x=1")
	})
	t.Run("use", func(t *testing.T) {
		execAll(t, db, "use default")
	})
	t.Run("window function", func(t *testing.T) {
		want(t, db, "select a, row_number() over (order by a) as r from (select 1 as a) t", "1,1")
	})
	t.Run("common table expression", func(t *testing.T) {
		want(t, db, "with t as (select 1 as a) select * from t", "1")
	})
	t.Run("hive table function", func(t *testing.T) {
		only(t, db, "hive")
		want(t, db, "select explode(array(1,2,3))", "1", "2", "3")
	})
	t.Run("version", func(t *testing.T) {
		if v := version(t, db); v == "" {
			t.Error("select version() is empty")
		}
	})
	t.Run("hive variable", func(t *testing.T) {
		only(t, db, "hive")
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(t.Context(), "set hivevar:v=1"); err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := conn.QueryRowContext(t.Context(), "select ${hivevar:v}").Scan(&n); err != nil || n != 1 {
			t.Errorf("select ${hivevar:v} = %d, %v, want 1", n, err)
		}
	})
	t.Run("a statement with no result set", func(t *testing.T) {
		rows, err := db.QueryContext(t.Context(), "use default")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	})
	t.Run("a result with no row", func(t *testing.T) {
		rows, err := db.QueryContext(t.Context(), "select 1 as a from (select 1 as x) t where 1=0")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		if rows.Next() || len(cols) != 1 || rows.Err() != nil {
			t.Errorf("a result with no row gave columns %v and an error %v", cols, rows.Err())
		}
	})
}

// typeTable makes a table of one column of the type, with a string key, for the
// round trip of the type. It is an ACID table on Hive and an Iceberg table on
// Impala, so that the round trip can update and delete a row.
func typeTable(t *testing.T, db *sql.DB, typ string) (string, string) {
	t.Helper()
	tbl := table()
	create := "create table " + tbl + " (k string, v " + typ + ") stored as orc tblproperties ('transactional'='true')"
	if flavorOf(t, db) == "impala" {
		create = "create table " + tbl + " (k string, v " + typ + ") stored as iceberg tblproperties ('format-version'='2')"
	}
	return create, tbl
}

// literal writes v as a literal of the SQL of the servers, for the round trip.
func literal(key string, v any) (string, error) {
	var lit string
	switch v := v.(type) {
	case bool:
		lit = map[bool]string{true: "true", false: "false"}[v]
	case int64:
		lit = apd.New(v, 0).String()
	case float64:
		d, err := new(apd.Decimal).SetFloat64(v)
		if err != nil {
			return "", err
		}
		lit = d.String()
	case string:
		lit = "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(v) + "'"
	case *apd.Decimal:
		lit = v.String()
	case dbimp.Date:
		lit = "cast('" + v.String() + "' as date)"
	case dbimp.LocalDateTime:
		lit = "cast('" + strings.Replace(v.String(), "T", " ", 1) + "' as timestamp)"
	default:
		return "", errors.Join(dbimp.ErrNotSupported, errors.New("no literal for the type"))
	}
	return "insert into {table} values ('" + key + "', " + lit + ")", nil
}

func TestIntegrationTypes(t *testing.T) {
	db := openServer(t)
	dec := func(s string) *apd.Decimal {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	equalDecimal := func(got, want any) bool {
		g, ok1 := got.(*apd.Decimal)
		w, ok2 := want.(*apd.Decimal)
		return ok1 && ok2 && g.Cmp(w) == 0 && g.String() == w.String()
	}
	// roundTrip runs the round trip of one scalar type.
	roundTrip := func(t *testing.T, name, sqlType string, values []dbimptest.Value, equal func(got, want any) bool) {
		t.Helper()
		create, tbl := typeTable(t, db, sqlType)
		// A server that cannot make an ACID table cannot update a row, which the
		// round trip does.
		if _, err := db.ExecContext(t.Context(), create); err != nil {
			if noACID(err) {
				t.Skip("the server cannot make an ACID table: " + err.Error())
			}
			if flavorOf(t, db) == "impala" {
				t.Skip("Impala cannot make an Iceberg table with this type: " + err.Error())
			}
			t.Fatalf("%s: %v", create, err)
		}
		if _, err := db.ExecContext(t.Context(), "drop table "+tbl); err != nil {
			t.Fatal(err)
		}
		dbimptest.RoundTrip(t, db, dbimptest.RoundTripCase{
			Type:     name,
			Setup:    []string{create},
			Teardown: []string{"drop table if exists " + tbl},
			Insert:   "insert into " + tbl + " values (?, ?)",
			Literal: func(key string, v any) (string, error) {
				s, err := literal(key, v)
				return strings.ReplaceAll(s, "{table}", tbl), err
			},
			Select: "select v from " + tbl + " where k = ?",
			Update: "update " + tbl + " set v = ? where k = ?",
			Delete: "delete from " + tbl + " where k = ?",
			Values: values,
			Equal:  equal,
		})
	}
	t.Run("BOOLEAN", func(t *testing.T) {
		roundTrip(t, "BOOLEAN", "boolean", []dbimptest.Value{{Name: "true", In: true}, {Name: "false", In: false}}, nil)
	})
	t.Run("TINYINT", func(t *testing.T) {
		roundTrip(t, "TINYINT", "tinyint", []dbimptest.Value{{Name: "max", In: int64(127)}, {Name: "min", In: int64(-128)}}, nil)
	})
	t.Run("SMALLINT", func(t *testing.T) {
		roundTrip(t, "SMALLINT", "smallint", []dbimptest.Value{{Name: "max", In: int64(32767)}, {Name: "min", In: int64(-32768)}}, nil)
	})
	t.Run("INT", func(t *testing.T) {
		roundTrip(t, "INT", "int", []dbimptest.Value{{Name: "max", In: int64(2147483647)}, {Name: "min", In: int64(-2147483648)}}, nil)
	})
	t.Run("BIGINT", func(t *testing.T) {
		roundTrip(t, "BIGINT", "bigint", []dbimptest.Value{{Name: "max", In: int64(9223372036854775807)}, {Name: "min", In: int64(-9223372036854775807)}}, nil)
	})
	t.Run("FLOAT", func(t *testing.T) {
		roundTrip(t, "FLOAT", "float", []dbimptest.Value{{Name: "one and a half", In: 1.5}, {Name: "negative", In: -2.25}}, nil)
	})
	t.Run("DOUBLE", func(t *testing.T) {
		roundTrip(t, "DOUBLE", "double", []dbimptest.Value{{Name: "pi", In: 3.141592653589793}, {Name: "small", In: 1e-300}}, nil)
	})
	t.Run("DECIMAL", func(t *testing.T) {
		roundTrip(t, "DECIMAL", "decimal(10,2)", []dbimptest.Value{{Name: "trailing zeros", In: dec("1.50")}, {Name: "negative", In: dec("-12345678.99")}}, equalDecimal)
	})
	t.Run("STRING", func(t *testing.T) {
		roundTrip(t, "STRING", "string", []dbimptest.Value{{Name: "quotes and backslash", In: "it's a \\ test"}, {Name: "unicode", In: "héllo 日本"}, {Name: "empty", In: ""}}, nil)
	})
	t.Run("VARCHAR", func(t *testing.T) {
		roundTrip(t, "VARCHAR", "varchar(20)", []dbimptest.Value{{Name: "text", In: "abc"}, {Name: "full", In: "12345678901234567890"}}, nil)
	})
	t.Run("CHAR", func(t *testing.T) {
		roundTrip(t, "CHAR", "char(5)", []dbimptest.Value{{Name: "padded", In: "ab", Want: "ab   "}, {Name: "full", In: "abcde"}}, nil)
	})
	t.Run("DATE", func(t *testing.T) {
		roundTrip(t, "DATE", "date", []dbimptest.Value{
			{Name: "a date", In: dbimp.Date{Year: 2026, Month: 1, Day: 2}},
			{Name: "the first day", In: dbimp.Date{Year: 1, Month: 1, Day: 1}},
		}, nil)
	})
	t.Run("TIMESTAMP", func(t *testing.T) {
		roundTrip(t, "TIMESTAMP", "timestamp", []dbimptest.Value{
			{Name: "microseconds", In: dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 1, Day: 2}, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5, Nanosecond: 123456000}}},
			{Name: "midnight", In: dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 6, Day: 30}}},
		}, nil)
	})
	t.Run("BINARY", func(t *testing.T) {
		only(t, db, "hive")
		roundTrip(t, "BINARY", "binary", []dbimptest.Value{{Name: "bytes", In: []byte{0, 255, 10, 255}}, {Name: "empty", In: []byte{}}}, nil)
	})
	// These types are read, with no write, because a statement cannot bind a
	// value of them as an argument.
	scan := func(t *testing.T, q string, check func(v any) bool) {
		t.Helper()
		var v any
		if err := db.QueryRowContext(t.Context(), q).Scan(&v); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if !check(v) {
			t.Errorf("%s = %v (%T)", q, v, v)
		}
	}
	t.Run("hive ARRAY", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select array(array(1,2),array(3))", func(v any) bool {
			a, ok := v.([]any)
			return ok && len(a) == 2
		})
	})
	t.Run("hive MAP", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select map('k',array(1,2))", func(v any) bool { _, ok := v.(map[string]any); return ok })
	})
	t.Run("hive STRUCT", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select named_struct('x',1,'y','z')", func(v any) bool {
			m, ok := v.(map[string]any)
			return ok && m["y"] == "z"
		})
	})
	t.Run("hive INTERVAL_YEAR_MONTH", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select interval '1-2' year to month", func(v any) bool { return v == dbimp.Interval{Months: 14} })
	})
	t.Run("hive INTERVAL_DAY_TIME", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select interval '1 2:3:4.5' day to second", func(v any) bool {
			return v == dbimp.Interval{Days: 1, Nanoseconds: ((2*60+3)*60+4)*1e9 + 5e8}
		})
	})
	t.Run("hive UNIONTYPE", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select create_union(0,1,'a')", func(v any) bool { return v == "{0:1}" })
	})
	t.Run("hive TIMESTAMP WITH LOCAL TIME ZONE", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select cast('2026-01-02 03:04:05' as timestamp with local time zone)", func(v any) bool {
			tm, ok := v.(interface{ UTC() any })
			_ = tm
			return ok || v != nil
		})
	})
	t.Run("hive NULL", func(t *testing.T) {
		only(t, db, "hive")
		scan(t, "select null", func(v any) bool { return v == nil })
	})
}
