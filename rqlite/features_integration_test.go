package rqlite_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/rqlite"
)

// These tests hold each entry of testdata/rqlite/features.json against a
// server (step 14a).

// TestIntegrationCRUD runs each statement of CRUD on three tables, one of
// which refers to another, and one of which has an index that a query uses,
// and compares each value that a select returns with the value written.
func TestIntegrationCRUD(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		parent, child, other := table(p, "crud_p"), table(p, "crud_c"), table(p, "crud_o")
		exec(t, db, "CREATE TABLE "+parent+" (k INTEGER PRIMARY KEY, v TEXT)")
		exec(t, db, "CREATE TABLE "+child+" (k INTEGER PRIMARY KEY, p INTEGER REFERENCES "+parent+" (k), v TEXT)")
		exec(t, db, "CREATE TABLE "+other+" (k INTEGER PRIMARY KEY, n INTEGER)")
		exec(t, db, "CREATE INDEX "+other+"_n ON "+other+" (n)")
		pairs := func(tb string) [][2]string {
			var out [][2]string
			rows, err := db.QueryContext(t.Context(), "SELECT k, v FROM "+tb+" ORDER BY k")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var k int64
				var v string
				if err := rows.Scan(&k, &v); err != nil {
					t.Fatal(err)
				}
				out = append(out, [2]string{strconv.FormatInt(k, 10), v})
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		}
		want := func(tb string, w ...[2]string) {
			t.Helper()
			if got := pairs(tb); !reflect.DeepEqual(got, w) && (len(got) != 0 || len(w) != 0) {
				t.Errorf("%s holds %q, want %q", tb, got, w)
			}
		}
		t.Run("insert", func(t *testing.T) {
			res := exec(t, db, "INSERT INTO "+parent+" VALUES (1, 'a'), (2, 'b'), (3, 'c')")
			if n, _ := res.RowsAffected(); n != 3 {
				t.Errorf("the insert affected %d rows, want 3", n)
			}
			exec(t, db, "INSERT INTO "+child+" VALUES (1, 1, 'x'), (2, 2, 'y')")
			exec(t, db, "INSERT INTO "+other+" VALUES (1, 10), (2, 20)")
		})
		t.Run("select", func(t *testing.T) {
			want(parent, [2]string{"1", "a"}, [2]string{"2", "b"}, [2]string{"3", "c"})
			if got := read[string](t, db, "SELECT c.v FROM "+child+" c JOIN "+parent+" p ON c.p = p.k WHERE p.v = 'b'"); !reflect.DeepEqual(got, []string{"y"}) {
				t.Errorf("the join gave %q, want y", got)
			}
			if plan := queryPlan(t, db, "SELECT k FROM "+other+" WHERE n = 20"); !strings.Contains(plan, other+"_n") {
				t.Errorf("the plan is %q, want the index %s_n", plan, other)
			}
		})
		t.Run("update", func(t *testing.T) {
			res := exec(t, db, "UPDATE "+parent+" SET v = 'B' WHERE k = 2")
			if n, _ := res.RowsAffected(); n != 1 {
				t.Errorf("the update affected %d rows, want 1", n)
			}
			want(parent, [2]string{"1", "a"}, [2]string{"2", "B"}, [2]string{"3", "c"})
		})
		t.Run("upsert", func(t *testing.T) {
			exec(t, db, "INSERT INTO "+parent+" VALUES (1, 'z') ON CONFLICT (k) DO UPDATE SET v = excluded.v || '!'")
			want(parent, [2]string{"1", "z!"}, [2]string{"2", "B"}, [2]string{"3", "c"})
		})
		t.Run("replace", func(t *testing.T) {
			exec(t, db, "REPLACE INTO "+parent+" VALUES (3, 'C')")
			want(parent, [2]string{"1", "z!"}, [2]string{"2", "B"}, [2]string{"3", "C"})
		})
		t.Run("returning", func(t *testing.T) {
			rows, err := db.QueryContext(t.Context(), "INSERT INTO "+parent+" VALUES (4, 'd') RETURNING k, v")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var k int64
			var v string
			if !rows.Next() || rows.Scan(&k, &v) != nil || k != 4 || v != "d" {
				t.Errorf("RETURNING gave %d %q %v, want 4 d", k, v, rows.Err())
			}
		})
		t.Run("delete", func(t *testing.T) {
			exec(t, db, "DELETE FROM "+child)
			res := exec(t, db, "DELETE FROM "+parent+" WHERE k >= 3")
			if n, _ := res.RowsAffected(); n != 2 {
				t.Errorf("the delete affected %d rows, want 2", n)
			}
			want(parent, [2]string{"1", "z!"}, [2]string{"2", "B"})
			exec(t, db, "DELETE FROM "+parent)
			exec(t, db, "DELETE FROM "+other)
			want(parent)
			want(child)
			if n := read[int64](t, db, "SELECT count(*) FROM "+other); n[0] != 0 {
				t.Errorf("%s holds %d rows after the delete", other, n[0])
			}
		})
		for _, tb := range []string{child, parent, other} {
			exec(t, db, "DROP TABLE "+tb)
		}
		if n := read[int64](t, db, "SELECT count(*) FROM sqlite_schema WHERE name IN (?, ?, ?)", parent, child, other); n[0] != 0 {
			t.Errorf("%d tables are left after the drop", n[0])
		}
	})
}

// TestIntegrationSchema makes each object of a schema that the survey marks,
// and shows that the server keeps to it.
func TestIntegrationSchema(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table(p, "s")
		t.Run("table", func(t *testing.T) {
			exec(t, db, "CREATE TABLE "+tb+" (id INTEGER PRIMARY KEY, name TEXT UNIQUE, n INTEGER DEFAULT 7 CHECK (n > 0), g INTEGER GENERATED ALWAYS AS (n * 2) VIRTUAL)")
			// A read of the table before its index hides the index from the
			// plans of later reads (measured), so the index comes first.
			exec(t, db, "CREATE INDEX "+tb+"_n ON "+tb+" (n)")
		})
		t.Run("primary_key", func(t *testing.T) {
			exec(t, db, "INSERT INTO "+tb+" (id, name) VALUES (1, 'a')")
			refused(t, db, "INSERT INTO "+tb+" (id, name) VALUES (1, 'b')", "UNIQUE constraint failed")
		})
		t.Run("unique_constraint", func(t *testing.T) {
			refused(t, db, "INSERT INTO "+tb+" (id, name) VALUES (2, 'a')", "UNIQUE constraint failed")
		})
		t.Run("check_constraint", func(t *testing.T) {
			refused(t, db, "INSERT INTO "+tb+" (id, name, n) VALUES (3, 'c', 0)", "CHECK constraint failed")
		})
		t.Run("default_value", func(t *testing.T) {
			if n := read[int64](t, db, "SELECT n FROM "+tb+" WHERE id = 1"); n[0] != 7 {
				t.Errorf("the default gave %d, want 7", n[0])
			}
		})
		t.Run("generated_column", func(t *testing.T) {
			if g := read[int64](t, db, "SELECT g FROM "+tb+" WHERE id = 1"); g[0] != 14 {
				t.Errorf("the generated column gave %d, want 14", g[0])
			}
		})
		t.Run("index", func(t *testing.T) {
			if plan := queryPlan(t, db, "SELECT id FROM "+tb+" WHERE n = 7"); !strings.Contains(plan, tb+"_n") {
				t.Errorf("the plan is %q, want the index", plan)
			}
		})
		t.Run("foreign_key", func(t *testing.T) {
			// Foreign keys are off, and PRAGMA foreign_keys would turn them on
			// for every client of the server (docs/RQLITE.md). So the test
			// shows the reference with foreign_key_check.
			exec(t, db, "CREATE TABLE "+tb+"_c (id INTEGER PRIMARY KEY, parent INTEGER REFERENCES "+tb+" (id))")
			exec(t, db, "INSERT INTO "+tb+"_c VALUES (1, 99)")
			if got := read[string](t, db, "SELECT \"table\" FROM pragma_foreign_key_check(?)", tb+"_c"); len(got) != 1 {
				t.Errorf("foreign_key_check gave %q, want the broken row", got)
			}
		})
		t.Run("view", func(t *testing.T) {
			exec(t, db, "CREATE VIEW "+tb+"_v AS SELECT id, name FROM "+tb)
			if got := read[string](t, db, "SELECT name FROM "+tb+"_v"); !reflect.DeepEqual(got, []string{"a"}) {
				t.Errorf("the view gave %q, want a", got)
			}
		})
		t.Run("without_rowid", func(t *testing.T) {
			exec(t, db, "CREATE TABLE "+tb+"_w (id INTEGER PRIMARY KEY, seen INTEGER) WITHOUT ROWID")
		})
		t.Run("trigger", func(t *testing.T) {
			exec(t, db, "CREATE TRIGGER "+tb+"_t AFTER INSERT ON "+tb+" BEGIN INSERT INTO "+tb+"_w VALUES (new.id, 1); END")
			exec(t, db, "INSERT INTO "+tb+" (id, name) VALUES (4, 'd')")
			if got := read[int64](t, db, "SELECT id FROM "+tb+"_w"); !reflect.DeepEqual(got, []int64{4}) {
				t.Errorf("the trigger wrote %v, want 4", got)
			}
		})
		t.Run("strict_table", func(t *testing.T) {
			exec(t, db, "CREATE TABLE "+tb+"_st (i INTEGER, a ANY) STRICT")
			refused(t, db, "INSERT INTO "+tb+"_st VALUES ('a', 1)", "cannot store TEXT value in INTEGER column")
			// ANY is not a supported type, and its values read by their JSON
			// form (D140).
			exec(t, db, "INSERT INTO "+tb+"_st VALUES (1, 'x'), (2, 2.5)")
			rows, err := db.QueryContext(t.Context(), "SELECT a FROM "+tb+"_st ORDER BY i")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var got []any
			for rows.Next() {
				var v any
				if err := rows.Scan(&v); err != nil {
					t.Fatal(err)
				}
				got = append(got, v)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []any{"x", 2.5}) {
				t.Errorf("the ANY column gave %#v, want x and 2.5", got)
			}
		})
		exec(t, db, "DROP VIEW IF EXISTS "+tb+"_v")
		for _, name := range []string{tb + "_c", tb + "_w", tb + "_st", tb} {
			exec(t, db, "DROP TABLE IF EXISTS "+name)
		}
	})
}

// TestIntegrationFeatures uses each feature of rqlite that the survey marks,
// and shows the refusal of each that it marks no.
func TestIntegrationFeatures(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table(p, "f")
		// A column with no declared type names the type text for a BLOB in
		// its first row, and the server writes that BLOB as text (measured),
		// so v is a BLOB, which keeps every storage class.
		exec(t, db, "CREATE TABLE "+tb+" (k INTEGER PRIMARY KEY, v BLOB)")
		t.Cleanup(func() { _, _ = db.ExecContext(t.Context(), "DROP TABLE IF EXISTS "+tb) })
		t.Run("read_consistency_level", func(t *testing.T) {
			for _, l := range []string{rqlite.LevelNone, rqlite.LevelWeak, rqlite.LevelLinearizable, rqlite.LevelStrong, rqlite.LevelAuto} {
				if got := read[int64](t, db, "SELECT 1", rqlite.WithLevel(l)); len(got) != 1 {
					t.Errorf("the level %s gave %v", l, got)
				}
			}
		})
		t.Run("freshness", func(t *testing.T) {
			if got := read[int64](t, db, "SELECT 1", rqlite.WithLevel(rqlite.LevelNone), rqlite.WithFreshness(time.Second)); len(got) != 1 {
				t.Errorf("a read with freshness gave %v", got)
			}
		})
		t.Run("queued_write", func(t *testing.T) {
			exec(t, db, "INSERT INTO "+tb+" VALUES (100, 'q')", rqlite.WithParameter("queue", nil), rqlite.WithParameter("wait", nil))
			if got := read[string](t, db, "SELECT v FROM "+tb+" WHERE k = 100", rqlite.WithLevel(rqlite.LevelStrong)); !reflect.DeepEqual(got, []string{"q"}) {
				t.Errorf("the queued write left %q, want q", got)
			}
		})
		t.Run("transaction_of_one_request", func(t *testing.T) {
			_, err := db.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (1, 'a'); INSERT INTO "+tb+" VALUES (1, 'b')", rqlite.WithParameter("transaction", nil))
			if err == nil {
				t.Error("two inserts of one key in a transaction gave no error")
			}
			if n := read[int64](t, db, "SELECT count(*) FROM "+tb+" WHERE k = 1"); n[0] != 0 {
				t.Errorf("the failed transaction left %d rows, want none", n[0])
			}
		})
		t.Run("transaction_across_requests", func(t *testing.T) {
			// A BEGIN stays open on the one write connection of the server for
			// every client, so the driver has no transactions (D144).
			if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("BeginTx gave %v, want %v", err, dbimp.ErrNotSupported)
			}
		})
		t.Run("rewrite_of_random_and_time", func(t *testing.T) {
			exec(t, db, "INSERT INTO "+tb+" VALUES (2, random())")
			if got := read[string](t, db, "SELECT typeof(v) FROM "+tb+" WHERE k = 2"); !reflect.DeepEqual(got, []string{"integer"}) {
				t.Errorf("random() wrote %q, want an integer", got)
			}
		})
		t.Run("blob_as_an_array", func(t *testing.T) {
			exec(t, db, "INSERT INTO "+tb+" VALUES (3, x'00ff')")
			if got := read[any](t, db, "SELECT v FROM "+tb+" WHERE k = 3"); !reflect.DeepEqual(got, []any{[]byte{0, 0xff}}) {
				t.Errorf("the BLOB gave %#v, want 00ff", got)
			}
		})
		t.Run("associative_form", func(t *testing.T) {
			// The associative form sorts the columns (measured), which hard
			// rule 3 forbids, so the driver cannot read it.
			if _, err := column[int64](t.Context(), db, "SELECT 1 AS b, 2 AS a", rqlite.WithParameter("associative", nil)); err == nil {
				t.Error("the associative form gave no error")
			}
		})
		t.Run("unified_endpoint", func(t *testing.T) {
			if got := read[int64](t, db, "INSERT INTO "+tb+" VALUES (4, 'd') RETURNING k"); !reflect.DeepEqual(got, []int64{4}) {
				t.Errorf("RETURNING gave %v, want 4", got)
			}
		})
		t.Run("named_parameters", func(t *testing.T) {
			got := read[string](t, db, "SELECT :a || $b || @c", sql.Named("a", "x"), sql.Named("b", "y"), sql.Named("c", "z"))
			if !reflect.DeepEqual(got, []string{"xyz"}) {
				t.Errorf("the named parameters gave %q, want xyz", got)
			}
		})
		t.Run("time_in_the_database", func(t *testing.T) {
			_, err := db.ExecContext(t.Context(), "INSERT INTO "+tb+" (k, v) WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 1000000000) SELECT 1000, count(*) FROM n", rqlite.WithTimeout(500*time.Millisecond))
			// The driver ends the request at the timeout too (D146), so
			// either error can come first. db_timeout stops the write on the
			// server in both cases (measured).
			if e, ok := errors.AsType[*rqlite.Error](err); !errors.Is(err, context.DeadlineExceeded) && (!ok || e.Message != "execute timeout") {
				t.Errorf("a long write with WithTimeout gave %v, want the end of the request or execute timeout", err)
			}
			// Every write runs on one connection, so a later write waits for
			// the end of the one that timed out.
			exec(t, db, "DELETE FROM "+tb+" WHERE k = -1")
			if got := read[int64](t, db, "SELECT k FROM "+tb+" WHERE k = 1000"); len(got) != 0 {
				t.Errorf("the write that timed out left %v", got)
			}
		})
		t.Run("raft_index", func(t *testing.T) {
			exec(t, db, "INSERT INTO "+tb+" VALUES (5, 'e')", rqlite.WithParameter("raft_index", nil))
		})
		t.Run("backup", func(t *testing.T) {
			// The driver sends no backup, so the test asks for it over HTTP.
			// Only a user with the permission backup can read it (measured).
			u, err := url.Parse(dsn(t, p))
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+u.Host+"/db/backup?fmt=sql", nil)
			if err != nil {
				t.Fatal(err)
			}
			pw, _ := u.User.Password()
			req.SetBasicAuth(u.User.Username(), pw)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			want := http.StatusOK
			if p == ordinary {
				want = http.StatusUnauthorized
			}
			if res.StatusCode != want {
				t.Errorf("the backup gave HTTP %d, want %d", res.StatusCode, want)
			}
		})
		t.Run("rqlite_functions", func(t *testing.T) {
			refused(t, db, "SELECT rqlite_version()", "no such function: rqlite_version")
		})
	})
}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type of the
// type table (D140).
func TestIntegrationRoundTrip(t *testing.T) {
	long := strings.Repeat("xé", 5000)
	day := func(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
	for _, tt := range []struct {
		typ, sqlType string
		values       []dbimptest.Value
		equal        func(got, want any) bool
	}{
		{"INTEGER", "INTEGER", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "zero", In: int64(0)},
			{Name: "smallest", In: int64(math.MinInt64)}, {Name: "largest", In: int64(math.MaxInt64)},
			// SQLite keeps the storage class of a value that is not an
			// integer (D140).
			{Name: "text", In: "abc"},
		}, nil},
		{"REAL", "REAL", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "half", In: 0.5}, {Name: "one", In: 1.0},
			{Name: "largest", In: math.MaxFloat64}, {Name: "smallest", In: 5e-324}, {Name: "last digit", In: 0.1 + 0.2},
		}, nil},
		{"TEXT", "TEXT", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "empty", In: ""}, {Name: "long", In: long},
			{Name: "unicode", In: "é 日本 🙂"}, {Name: "quotes", In: `say 'hi' "x" \ bye`},
			// The server binds x'...' as a BLOB, so the driver writes it as a
			// literal (D143).
			{Name: "hex form", In: "x'41'"},
		}, nil},
		{"BLOB", "BLOB", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "bytes", In: []byte{0, 0xff}}, {Name: "empty", In: []byte{}},
			{Name: "long", In: []byte(long)},
		}, nil},
		{"NUMERIC", "NUMERIC", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "integer", In: int64(12)}, {Name: "fraction", In: 1.25},
			{Name: "integral real", In: 3.0, Want: int64(3)}, {Name: "text", In: "abc"},
		}, nil},
		{"BOOLEAN", "BOOLEAN", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "false", In: false}, {Name: "true", In: true},
		}, nil},
		{"DATE", "DATE", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "first", In: day(1000, 1, 1)}, {Name: "last", In: day(9999, 12, 31)},
			{Name: "today", In: day(2026, 9, 30)},
		}, nil},
		{"DATETIME", "DATETIME", []dbimptest.Value{
			{Name: "null", In: nil},
			{Name: "nanoseconds", In: dbimp.LocalDateTime{Date: day(2026, 9, 30), Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}}},
			{Name: "midnight", In: dbimp.LocalDateTime{Date: day(1000, 1, 1)}},
		}, nil},
		{"TIMESTAMP", "TIMESTAMP", []dbimptest.Value{
			{Name: "null", In: nil},
			{Name: "utc", In: time.Date(2026, 9, 30, 12, 34, 56, 123456789, time.UTC)},
			{Name: "offset", In: time.Date(2026, 9, 30, 12, 34, 56, 0, time.FixedZone("", 5*3600+1800))},
		}, sameInstant},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				tb := table(p, "rt_"+strings.ToLower(tt.typ))
				dbimptest.RoundTrip(t, db, dbimptest.RoundTripCase{
					Type:     tt.typ,
					Setup:    []string{"CREATE TABLE " + tb + " (k TEXT PRIMARY KEY, v " + tt.sqlType + ")"},
					Teardown: []string{"DROP TABLE IF EXISTS " + tb},
					Insert:   "INSERT INTO " + tb + " (k, v) VALUES (?, ?)",
					Literal: func(key string, v any) (string, error) {
						lit, err := literal(v)
						if err != nil {
							return "", err
						}
						return "INSERT INTO " + tb + " (k, v) VALUES (" + quote(key) + ", " + lit + ")", nil
					},
					Select: "SELECT v FROM " + tb + " WHERE k = ?",
					Update: "UPDATE " + tb + " SET v = ? WHERE k = ?",
					Delete: "DELETE FROM " + tb + " WHERE k = ?",
					Values: tt.values,
					Equal:  tt.equal,
				})
			})
		})
	}
}

// queryPlan returns the details of the plan of query, joined.
func queryPlan(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, notused int64
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(details, "; ")
}

// sameInstant compares two times by their instant and their offset, and any
// other value with reflect.DeepEqual.
func sameInstant(got, want any) bool {
	g, gok := got.(time.Time)
	w, wok := want.(time.Time)
	if !gok || !wok {
		return reflect.DeepEqual(got, want)
	}
	_, go1 := g.Zone()
	_, wo := w.Zone()
	return g.Equal(w) && go1 == wo
}

// literal writes v as a literal of SQLite, for the round trip.
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
	case float64:
		s := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return s, nil
	case string:
		return quote(v), nil
	case []byte:
		return "X'" + hex.EncodeToString(v) + "'", nil
	case time.Time:
		return quote(v.Format(time.RFC3339Nano)), nil
	case dbimp.Date:
		return quote(v.String()), nil
	case dbimp.LocalDateTime:
		return quote(v.String()), nil
	}
	return "", dbimp.Unsupported("a literal of " + reflect.TypeOf(v).String())
}

// quote returns s in single quotes, with each ' doubled.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
