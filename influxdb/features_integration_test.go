package influxdb //nolint:testpackage // The tests write literals of line protocol with the writers of the driver.

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// These tests hold step 14a of docs/DRIVER.md: each entry of
// testdata/influxdb/features.json names one of their subtests. An entry that
// a release lacks expects the refusal of that release (measured in step 6).

// refused runs q, and fails unless the server refuses it.
func refused(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), q)
	if _, ok := errors.AsType[*Error](err); !ok {
		t.Errorf("%s gave %v, want the refusal of the server", q, err)
	}
}

// exec runs q, and fails on an error.
func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// column returns column i of each row of every result set of q.
func column(t *testing.T, db *sql.DB, i int, q string, args ...any) []any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	var out []any
	for {
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for j := range vals {
				ptrs[j] = &vals[j]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			out = append(out, vals[i])
		}
		if !rows.NextResultSet() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return out
}

// same fails unless got holds want, in order.
func same(t *testing.T, what string, got []any, want ...any) {
	t.Helper()
	if !slices.EqualFunc(got, want, func(a, b any) bool {
		ta, aok := a.(time.Time)
		tb, bok := b.(time.Time)
		if aok && bok {
			return ta.Equal(tb)
		}
		return a == b
	}) {
		t.Errorf("%s gave %#v, want %#v", what, got, want)
	}
}

// has reports whether got holds v.
func has(got []any, v any) bool {
	return slices.Contains(got, v)
}

// asOrdinary runs f as the ordinary user of InfluxDB 1 and 2, which reads
// and does not write (the dbmeta entry). InfluxDB 3 Core has no such user.
func asOrdinary(t *testing.T, f func(t *testing.T, db *sql.DB)) {
	t.Helper()
	t.Run("ordinary", func(t *testing.T) {
		f(t, openDialect(t, ordinary, InfluxQL, nil))
	})
}

func TestIntegrationCRUD(t *testing.T) {
	n := release(t, admin)
	db := openDialect(t, admin, InfluxQL, nil)
	tables := []string{measurement("crud_a"), measurement("crud_b"), measurement("crud_c")}
	at := func(sec int64) time.Time { return time.Unix(1700000000+sec, 0).UTC() }
	values := func(m string) []any { return column(t, db, 3, `SELECT "host", "v" FROM "`+m+`"`) }
	t.Run("insert", func(t *testing.T) {
		for i, m := range tables {
			exec(t, db, "INSERT "+m+",host=$h v=$v,s=$s $t", sql.Named("h", "a"), sql.Named("v", int64(i)), sql.Named("s", "é"), sql.Named("t", at(0)))
			exec(t, db, "INSERT INTO dbmeta "+m+`,host=b v=`+strconv.Itoa(i+10)+`i,s="x" `+strconv.FormatInt(at(1).UnixNano(), 10))
		}
		asOrdinary(t, func(t *testing.T, udb *sql.DB) {
			refused(t, udb, "INSERT "+tables[0]+",host=c v=1i")
		})
	})
	t.Run("select", func(t *testing.T) {
		for i, m := range tables {
			same(t, m, values(m), int64(i), int64(i+10))
		}
		asOrdinary(t, func(t *testing.T, udb *sql.DB) {
			same(t, "the ordinary user", column(t, udb, 3, `SELECT "host", "v" FROM "`+tables[0]+`"`), int64(0), int64(10))
		})
		if n >= 3 {
			sdb := openDialect(t, admin, SQL, nil)
			same(t, "SQL", column(t, sdb, 0, `SELECT v FROM "`+tables[1]+`" ORDER BY time`), int64(1), int64(11))
		}
	})
	t.Run("update", func(t *testing.T) {
		refused(t, db, `UPDATE "`+tables[0]+`" SET v = 5`)
		if n >= 3 {
			refused(t, openDialect(t, admin, SQL, nil), `UPDATE "`+tables[0]+`" SET v = 5`)
		}
		// A write of the same series and time replaces the value, which is
		// the update of InfluxDB.
		exec(t, db, "INSERT "+tables[0]+",host=a v=$v $t", sql.Named("v", int64(5)), sql.Named("t", at(0)))
		same(t, "the update", values(tables[0]), int64(5), int64(10))
	})
	t.Run("delete", func(t *testing.T) {
		q := `DELETE FROM "` + tables[0] + `" WHERE "host" = 'a'`
		if n >= 3 {
			refused(t, db, q)
			return
		}
		asOrdinary(t, func(t *testing.T, udb *sql.DB) {
			refused(t, udb, q)
		})
		exec(t, db, q)
		same(t, "after the delete", values(tables[0]), int64(10))
	})
	t.Run("select_into", func(t *testing.T) {
		copied := measurement("crud_copy")
		q := `SELECT "v" INTO "` + copied + `" FROM "` + tables[1] + `"`
		if n != 1 {
			refused(t, db, q)
			return
		}
		exec(t, db, q)
		same(t, "the copy", column(t, db, 2, `SELECT "v" FROM "`+copied+`"`), int64(1), int64(11))
	})
	t.Run("drop_series", func(t *testing.T) {
		q := `DROP SERIES FROM "` + tables[1] + `" WHERE "host" = 'b'`
		if n != 1 {
			refused(t, db, q)
			return
		}
		exec(t, db, q)
		same(t, "after DROP SERIES", values(tables[1]), int64(1))
	})
	t.Run("drop_measurement", func(t *testing.T) {
		q := `DROP MEASUREMENT "` + tables[2] + `"`
		if n >= 3 {
			refused(t, db, q)
			return
		}
		exec(t, db, q)
		if has(column(t, db, 1, "SHOW MEASUREMENTS"), tables[2]) {
			t.Errorf("%s is still there after DROP MEASUREMENT", tables[2])
		}
	})
}

func TestIntegrationSchema(t *testing.T) {
	n := release(t, admin)
	db := openDialect(t, admin, InfluxQL, nil)
	name := "dbimp_it_" + suffix
	t.Run("create_database", func(t *testing.T) {
		if n != 1 {
			refused(t, db, "CREATE DATABASE "+name)
			return
		}
		exec(t, db, "CREATE DATABASE "+name)
		if !has(column(t, db, 1, "SHOW DATABASES"), name) {
			t.Errorf("SHOW DATABASES has no %s", name)
		}
		asOrdinary(t, func(t *testing.T, udb *sql.DB) {
			refused(t, udb, "CREATE DATABASE "+name+"_x")
		})
	})
	t.Run("drop_database", func(t *testing.T) {
		if n != 1 {
			refused(t, db, "DROP DATABASE "+name)
			return
		}
		exec(t, db, "DROP DATABASE "+name)
		if has(column(t, db, 1, "SHOW DATABASES"), name) {
			t.Errorf("SHOW DATABASES still has %s", name)
		}
	})
	t.Run("retention_policy", func(t *testing.T) {
		q := "CREATE RETENTION POLICY " + name + " ON dbmeta DURATION 1d REPLICATION 1"
		if n != 1 {
			refused(t, db, q)
			return
		}
		exec(t, db, q)
		if !has(column(t, db, 1, "SHOW RETENTION POLICIES"), name) {
			t.Errorf("SHOW RETENTION POLICIES has no %s", name)
		}
		exec(t, db, "DROP RETENTION POLICY "+name+" ON dbmeta")
	})
	t.Run("continuous_query", func(t *testing.T) {
		q := "CREATE CONTINUOUS QUERY " + name + ` ON dbmeta BEGIN SELECT mean("v") INTO "` + measurement("cq") + `" FROM "` + measurement("crud_a") + `" GROUP BY time(1h) END`
		if n != 1 {
			refused(t, db, q)
			return
		}
		exec(t, db, q)
		if !has(column(t, db, 1, "SHOW CONTINUOUS QUERIES"), name) {
			t.Errorf("SHOW CONTINUOUS QUERIES has no %s", name)
		}
		exec(t, db, "DROP CONTINUOUS QUERY "+name+" ON dbmeta")
	})
	sqlOnly := func(t *testing.T) *sql.DB {
		t.Helper()
		if n < 3 {
			t.Skip("SQL needs InfluxDB 3 or later (D78)")
		}
		return openDialect(t, admin, SQL, nil)
	}
	t.Run("information_schema", func(t *testing.T) {
		sdb := sqlOnly(t)
		m := measurement("schema")
		write(t, m+",host=a v=1i,s=\"x\" 1700000000000000000")
		got := column(t, sdb, 0, "SELECT column_name FROM information_schema.columns WHERE table_name = $t ORDER BY column_name", sql.Named("t", m))
		same(t, "the columns", got, "host", "s", "time", "v")
	})
	for _, tt := range []struct{ name, q string }{
		{"primary_key", "CREATE TABLE dbimp_t (a INT PRIMARY KEY)"},
		{"index", "CREATE INDEX dbimp_i ON types (f)"},
		{"unique_constraint", "CREATE TABLE dbimp_t (a INT UNIQUE)"},
		{"view", "CREATE VIEW dbimp_v AS SELECT 1 AS a"},
		{"default_value", "CREATE TABLE dbimp_t (a INT DEFAULT 1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			refused(t, sqlOnly(t), tt.q)
		})
	}
}

func TestIntegrationFeatures(t *testing.T) {
	n := release(t, admin)
	db := openDialect(t, admin, InfluxQL, nil)
	m := measurement("features")
	exec(t, db, "INSERT "+m+",host=a v=1.5,n=1i 1700000000000000000\n"+
		"# a comment\n"+
		m+",host=b v=2.5,n=2i 1700000030000000000\n"+
		m+",host=a v=3.5,n=3i 1700003600000000000")
	sqlOnly := func(t *testing.T) *sql.DB {
		t.Helper()
		if n < 3 {
			t.Skip("SQL needs InfluxDB 3 or later (D78)")
		}
		return openDialect(t, admin, SQL, nil)
	}
	window := `time >= '2023-11-14T22:00:00Z' AND time < '2023-11-15T00:00:00Z'`
	t.Run("line_protocol", func(t *testing.T) {
		same(t, "the lines", column(t, db, 2, `SELECT "n" FROM "`+m+`"`), int64(1), int64(2), int64(3))
	})
	t.Run("group_by_time", func(t *testing.T) {
		same(t, "the counts", column(t, db, 2, `SELECT count("n") FROM "`+m+`" WHERE `+window+` GROUP BY time(1h)`), int64(2), int64(1))
	})
	t.Run("fill", func(t *testing.T) {
		got := column(t, db, 2, `SELECT count("n") FROM "`+m+`" WHERE `+window+` GROUP BY time(30m) fill(0)`)
		same(t, "the counts", got, int64(2), int64(0), int64(1), int64(0))
	})
	t.Run("slimit_and_soffset", func(t *testing.T) {
		q := `SELECT "n" FROM "` + m + `" GROUP BY "host" SLIMIT 1 SOFFSET 1`
		if n >= 3 {
			refused(t, db, q)
			return
		}
		same(t, "the second series", column(t, db, 1, q), "b")
	})
	t.Run("tz_clause", func(t *testing.T) {
		got := column(t, db, 1, `SELECT "n" FROM "`+m+`" LIMIT 1 tz('Asia/Tokyo')`)
		at, ok := got[0].(time.Time)
		if _, off := at.Zone(); !ok || off != 9*3600 {
			t.Errorf("the time is %v, want the offset of Asia/Tokyo", got[0])
		}
	})
	t.Run("epoch_precision", func(t *testing.T) {
		// The driver sends no epoch, and reads the time in RFC 3339 with its
		// nanoseconds as a time.Time, which keeps every precision.
		exec(t, db, "INSERT "+m+",host=c n=4i 1700000000123456789")
		got := column(t, db, 1, `SELECT "n" FROM "`+m+`" WHERE "host" = 'c'`)
		same(t, "the time", got, time.Unix(0, 1700000000123456789).UTC())
	})
	t.Run("influxql_subquery", func(t *testing.T) {
		same(t, "the maximum", column(t, db, 2, `SELECT max("m") FROM (SELECT mean("v") AS "m" FROM "`+m+`" GROUP BY "host")`), 2.5)
	})
	t.Run("show_databases", func(t *testing.T) {
		if !has(column(t, db, 1, "SHOW DATABASES"), "dbmeta") {
			t.Error("SHOW DATABASES has no dbmeta")
		}
		asOrdinary(t, func(t *testing.T, udb *sql.DB) {
			same(t, "the ordinary user", column(t, udb, 1, "SHOW DATABASES"), "dbmeta")
		})
	})
	t.Run("show_measurements", func(t *testing.T) {
		if !has(column(t, db, 1, "SHOW MEASUREMENTS"), m) {
			t.Errorf("SHOW MEASUREMENTS has no %s", m)
		}
	})
	t.Run("show_tag_keys", func(t *testing.T) {
		same(t, "the tag keys", column(t, db, 1, `SHOW TAG KEYS FROM "`+m+`"`), "host")
	})
	t.Run("show_tag_values", func(t *testing.T) {
		got := column(t, db, 2, `SHOW TAG VALUES FROM "`+m+`" WITH KEY = "host"`)
		if n >= 3 {
			// InfluxDB 3 answers with no series (measured).
			same(t, "the tag values on InfluxDB 3", got)
			return
		}
		same(t, "the tag values", got, "a", "b", "c")
	})
	t.Run("show_field_keys", func(t *testing.T) {
		got := column(t, db, 1, `SHOW FIELD KEYS FROM "`+m+`"`)
		same(t, "the field keys", got, "n", "v")
	})
	t.Run("show_series", func(t *testing.T) {
		q := `SHOW SERIES FROM "` + m + `"`
		if n >= 3 {
			refused(t, db, q)
			return
		}
		same(t, "the series", column(t, db, 1, q), m+",host=a", m+",host=b", m+",host=c")
	})
	t.Run("show_retention_policies", func(t *testing.T) {
		got := column(t, db, 1, "SHOW RETENTION POLICIES")
		if len(got) == 0 {
			t.Error("SHOW RETENTION POLICIES gave no policy")
		}
	})
	t.Run("date_bin", func(t *testing.T) {
		sdb := sqlOnly(t)
		got := column(t, sdb, 1, `SELECT date_bin(INTERVAL '1 hour', time) AS b, count(*) AS c FROM "`+m+`" WHERE host != 'c' GROUP BY 1 ORDER BY 1`)
		same(t, "the counts", got, int64(2), int64(1))
	})
	t.Run("selector_functions", func(t *testing.T) {
		sdb := sqlOnly(t)
		got := column(t, sdb, 0, `SELECT selector_last(v, time)['value'] AS l FROM "`+m+`"`)
		same(t, "the last value", got, 3.5)
	})
	cache := func(t *testing.T, kind string, body string) {
		t.Helper()
		cfg := config(t, admin)
		if err := call(t.Context(), cfg, http.MethodPost, "/api/v3/configure/"+kind, body); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("last_cache", func(t *testing.T) {
		sdb := sqlOnly(t)
		cache(t, "last_cache", `{"db":"dbmeta","table":"`+m+`","name":"lc"}`)
		// The cache holds the writes after it, and a write needs its flush
		// of the WAL before a read sees it.
		exec(t, db, "INSERT "+m+",host=a v=9.5,n=9i")
		poll(t, func() bool {
			return has(column(t, sdb, 0, `SELECT v FROM last_cache('`+m+`', 'lc')`), 9.5)
		})
	})
	t.Run("distinct_cache", func(t *testing.T) {
		sdb := sqlOnly(t)
		cache(t, "distinct_cache", `{"db":"dbmeta","table":"`+m+`","name":"dc","columns":["host"]}`)
		exec(t, db, "INSERT "+m+",host=z v=1.5,n=1i")
		poll(t, func() bool {
			return has(column(t, sdb, 0, `SELECT host FROM distinct_cache('`+m+`', 'dc')`), "z")
		})
	})
	t.Run("time_column", func(t *testing.T) {
		got := column(t, db, 1, `SELECT "n" FROM "`+m+`" LIMIT 1`)
		same(t, "the time", got, time.Unix(1700000000, 0).UTC())
	})
	t.Run("several_statements", func(t *testing.T) {
		got := column(t, db, 0, `SELECT "n" FROM "`+m+`" LIMIT 1; SHOW TAG KEYS FROM "`+m+`"`)
		same(t, "the names of the two sets", got, m, m)
	})
	t.Run("parameters", func(t *testing.T) {
		same(t, "InfluxQL", column(t, db, 2, `SELECT "n" FROM "`+m+`" WHERE "host" = $h`, sql.Named("h", "b")), int64(2))
		if n >= 3 {
			sdb := openDialect(t, admin, SQL, nil)
			same(t, "SQL", column(t, sdb, 0, `SELECT n FROM "`+m+`" WHERE host = $1`, "b"), int64(2))
		}
	})
	t.Run("time_parameter", func(t *testing.T) {
		got := column(t, db, 2, `SELECT "n" FROM "`+m+`" WHERE time > $t AND time < '2024-01-01T00:00:00Z'`, sql.Named("t", time.Unix(1700000001, 0)))
		same(t, "InfluxQL", got, int64(2), int64(3))
	})
	t.Run("array_parameter", func(t *testing.T) {
		_, err := db.ExecContext(t.Context(), `SELECT "n" FROM "`+m+`" WHERE "host" = $h`, sql.Named("h", []any{"a", "b"}))
		if err == nil {
			t.Error("an array parameter gave no error")
		}
	})
	t.Run("chunked_responses", func(t *testing.T) {
		big := measurement("chunks")
		var b strings.Builder
		for i := range 12000 {
			fmt.Fprintf(&b, "%s n=%di %d\n", big, i, int64(1600000000+i)*1e9)
		}
		exec(t, db, "INSERT "+b.String())
		got, err := sets(t, db, `SELECT "n" FROM "`+big+`"`)
		// InfluxDB 1 answers in two chunks, and the series continues across
		// them (D83).
		if err != nil || !slices.Equal(got, []int{12000}) {
			t.Errorf("12000 rows gave %v and %v, want one set of 12000 rows", got, err)
		}
	})
}

// poll runs ok until it holds, for 30 seconds at most, for a write that a
// read sees only after the flush of the WAL of InfluxDB 3.
func poll(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatal("the read did not see the write in 30 seconds")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// roundTrip returns the round trip of one type in the dialect d, into the
// measurement m (D86). The value is the field v, the tag t or the time of
// the point. A field k keeps each point alive when the value is NULL.
func roundTrip(n int, d, typ, m string, values []dbimptest.Value) dbimptest.RoundTripCase {
	const at = "1700000000000000000"
	c := dbimptest.RoundTripCase{
		Type:   typ,
		Named:  true,
		Values: values,
		// InfluxDB 3 serves a write after the flush of its WAL.
		Wait: 10 * time.Second,
		SkipUpdate: func(_, to dbimptest.Value) string {
			if to.In == nil {
				return "a write that leaves a field out keeps its old value, so no point is updated to NULL (D86)"
			}
			return ""
		},
	}
	value, lit := "v=$value,k=1i "+at, func(v any) (string, error) {
		if v == nil {
			return "k=1i " + at, nil
		}
		s, err := fieldLiteral(v)
		return "v=" + s + ",k=1i " + at, err
	}
	sel, col := `SELECT "v", "k" FROM "`+m+`" WHERE "key" = $key`, 2
	if d == SQL {
		sel, col = `SELECT v FROM "`+m+`" WHERE key = $key`, 0
	}
	switch typ {
	case "tag":
		value, lit = "k=1i "+at, func(v any) (string, error) { return "k=1i " + at, nil }
		c.Insert = "INSERT " + m + ",key=$key,t=$value " + value
		c.Literal = func(key string, v any) (string, error) {
			k, err := tagLiteral(key)
			if err != nil || v == nil {
				return "INSERT " + m + ",key=" + k + " k=1i " + at, err
			}
			tv, err := tagLiteral(v)
			return "INSERT " + m + ",key=" + k + ",t=" + tv + " k=1i " + at, err
		}
		sel = `SELECT "t", "k" FROM "` + m + `" WHERE "key" = $key`
		if d == SQL {
			sel = `SELECT t FROM "` + m + `" WHERE key = $key`
		}
		c.SkipUpdate = func(dbimptest.Value, dbimptest.Value) string {
			return "a tag is part of the series, so a new value is a new point and not an update (D86)"
		}
	case "timestamp":
		c.Insert = "INSERT " + m + ",key=$key k=1i $value"
		c.Literal = func(key string, v any) (string, error) {
			k, err := tagLiteral(key)
			if err != nil {
				return "", err
			}
			ts, err := timestampLiteral(v)
			return "INSERT " + m + ",key=" + k + " k=1i " + ts, err
		}
		sel, col = `SELECT "k" FROM "`+m+`" WHERE "key" = $key`, 1
		if d == SQL {
			sel, col = `SELECT time FROM "`+m+`" WHERE key = $key`, 0
		}
		c.SkipUpdate = func(dbimptest.Value, dbimptest.Value) string {
			return "the time names the point, so a new time is a new point and not an update (D86)"
		}
		c.Equal = func(got, want any) bool {
			g, ok := got.(time.Time)
			w, wok := want.(time.Time)
			return ok && wok && g.Equal(w)
		}
	default:
		c.Insert = "INSERT " + m + ",key=$key " + value
		c.Literal = func(key string, v any) (string, error) {
			k, err := tagLiteral(key)
			if err != nil {
				return "", err
			}
			s, err := lit(v)
			return "INSERT " + m + ",key=" + k + " " + s, err
		}
	}
	c.Update, c.Select, c.Column = c.Insert, sel, col
	// SQL names only the columns that a write made, and the first value is
	// NULL, so a point of another key makes the column of the value first.
	seed := map[string]string{
		"null": "v=1.5", "float": "v=1.5", "integer": "v=1i", "unsigned integer": "v=1u",
		"string": `v="s"`, "boolean": "v=true", "tag": "", "timestamp": "",
	}[typ]
	if typ == "tag" {
		c.Setup = []string{"INSERT " + m + ",key=seed,t=s k=1i 1"}
	} else if seed != "" {
		c.Setup = []string{"INSERT " + m + ",key=seed " + seed + ",k=1i 1"}
	}
	if n < 3 {
		c.Delete = `DELETE FROM "` + m + `" WHERE "key" = $key`
		if d == SQL {
			c.Delete = ""
		}
	}
	return c
}

func TestIntegrationRoundTrip(t *testing.T) {
	n := release(t, admin)
	long := strings.Repeat("xé", 5000)
	for _, d := range dialects(t, admin) {
		iql := d == InfluxQL
		// InfluxQL decodes a number by its text (D83), so a whole float and
		// a small unsigned integer read back as an int64.
		whole := func(v any, i int64) any {
			if iql {
				return i
			}
			return v
		}
		types := []struct {
			typ    string
			values []dbimptest.Value
		}{
			{"null", []dbimptest.Value{{Name: "null", In: nil}, {Name: "one and a half", In: 1.5}}},
			{"float", []dbimptest.Value{
				{Name: "null", In: nil},
				{Name: "zero", In: 0.0, Want: whole(0.0, 0)},
				{Name: "smallest", In: -math.MaxFloat64},
				{Name: "largest", In: math.MaxFloat64},
				{Name: "smallest step", In: math.SmallestNonzeroFloat64},
				{Name: "last digit", In: 0.1 + 0.2},
			}},
			{"integer", []dbimptest.Value{
				{Name: "null", In: nil},
				{Name: "zero", In: int64(0)},
				{Name: "smallest", In: int64(math.MinInt64)},
				{Name: "largest", In: int64(math.MaxInt64)},
			}},
			{"unsigned integer", []dbimptest.Value{
				{Name: "null", In: nil},
				{Name: "zero", In: uint64(0), Want: whole(uint64(0), 0)},
				{Name: "above int64", In: uint64(1) << 63},
				{Name: "largest", In: uint64(math.MaxUint64)},
			}},
			{"string", []dbimptest.Value{
				{Name: "null", In: nil},
				{Name: "empty", In: ""},
				{Name: "long", In: long},
				{Name: "unicode", In: "é 日本 🙂"},
				{Name: "quotes", In: `say "hi", a=b \ bye`},
			}},
			{"boolean", []dbimptest.Value{{Name: "null", In: nil}, {Name: "false", In: false}, {Name: "true", In: true}}},
			{"tag", []dbimptest.Value{
				{Name: "null", In: nil},
				{Name: "one", In: "a"},
				{Name: "long", In: long[:1000]},
				{Name: "escapes", In: "é 日本, =x"},
			}},
			// The time of a point cannot be NULL: the server gives a point
			// with no time the time of its write.
			{"timestamp", []dbimptest.Value{
				{Name: "epoch", In: time.Unix(0, 0).UTC()},
				{Name: "nanosecond and zone", In: time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.FixedZone("", 9*3600))},
				{Name: "largest", In: time.Unix(0, math.MaxInt64-1).UTC()},
			}},
		}
		for _, tt := range types {
			t.Run(d+"/"+strings.ReplaceAll(tt.typ, " ", "_"), func(t *testing.T) {
				if tt.typ == "unsigned integer" && n == 1 {
					t.Skip("InfluxDB 1 has no unsigned integer (measured)")
				}
				db := openDialect(t, admin, d, nil)
				m := measurement("rt_" + strings.ReplaceAll(tt.typ, " ", "_") + "_" + d)
				dbimptest.RoundTrip(t, db, roundTrip(n, d, tt.typ, m, tt.values))
			})
		}
	}
	t.Run("ordinary", func(t *testing.T) {
		config(t, ordinary)
		t.Skip("the ordinary user of InfluxDB 1 and 2 reads and does not write, so it cannot run a round trip. TestIntegrationTypes reads each type as that user")
	})
}
