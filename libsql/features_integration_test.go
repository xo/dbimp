package libsql_test

import (
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/libsql"
)

// These tests hold each entry of testdata/libsql/features.json against a
// server (step 14a). The administrator makes every table, and the ordinary
// user, whose token can only read, reads it and gets HTTP 403 for each
// write.

// hrana sends body as p to /v3/pipeline over HTTP, for a request of Hrana that the
// driver never sends, such as a batch with conditions, and returns the
// answer.
func hrana(t *testing.T, p principal, body string) (int, string) {
	t.Helper()
	cfg, err := libsql.ParseDSN(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	scheme := "https"
	if cfg.NoTLS {
		scheme = "http"
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, scheme+"://"+cfg.Host+":"+strconv.Itoa(cfg.Port)+"/v3/pipeline", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Password)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

// TestIntegrationCRUD runs each statement of CRUD on three tables, one of
// which refers to another, and one of which has an index that a query uses,
// and compares each value that a select returns with the value written.
func TestIntegrationCRUD(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db, adm *sql.DB) {
		parent, child, other := table(p, "crud_p"), table(p, "crud_c"), table(p, "crud_o")
		exec(t, adm, "CREATE TABLE "+parent+" (k INTEGER PRIMARY KEY, v TEXT)")
		exec(t, adm, "CREATE TABLE "+child+" (k INTEGER PRIMARY KEY, p INTEGER REFERENCES "+parent+" (k), v TEXT)")
		exec(t, adm, "CREATE TABLE "+other+" (k INTEGER PRIMARY KEY, n INTEGER)")
		exec(t, adm, "CREATE INDEX "+other+"_n ON "+other+" (n)")
		// The writer is the administrator. The ordinary user reads, and gets
		// HTTP 403 for each write (measured).
		w := db
		if p == ordinary {
			w = adm
		}
		pairs := func(tb string) string {
			rows, err := db.QueryContext(t.Context(), "SELECT k, v FROM "+tb+" ORDER BY k")
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
		want := func(tb, w string) {
			t.Helper()
			if got := pairs(tb); got != w {
				t.Errorf("%s holds %q, want %q", tb, got, w)
			}
		}
		t.Run("insert", func(t *testing.T) {
			if p == ordinary {
				forbidden(t, db, "INSERT INTO "+parent+" VALUES (9, 'z')")
			}
			res := exec(t, w, "INSERT INTO "+parent+" VALUES (1, 'a'), (2, 'b'), (3, 'c')")
			if n, _ := res.RowsAffected(); n != 3 {
				t.Errorf("the insert affected %d rows, want 3", n)
			}
			exec(t, w, "INSERT INTO "+child+" VALUES (1, 1, 'x'), (2, 2, 'y')")
			exec(t, w, "INSERT INTO "+other+" VALUES (1, 10), (2, 20)")
		})
		t.Run("select", func(t *testing.T) {
			want(parent, "1=a,2=b,3=c")
			if got := read[string](t, db, "SELECT c.v FROM "+child+" c JOIN "+parent+" p ON c.p = p.k WHERE p.v = 'b'"); !reflect.DeepEqual(got, []string{"y"}) {
				t.Errorf("the join gave %q, want y", got)
			}
			if plan := queryPlan(t, db, "SELECT k FROM "+other+" WHERE n = 20"); !strings.Contains(plan, other+"_n") {
				t.Errorf("the plan is %q, want the index %s_n", plan, other)
			}
		})
		t.Run("update", func(t *testing.T) {
			if p == ordinary {
				forbidden(t, db, "UPDATE "+parent+" SET v = 'Q'")
			}
			res := exec(t, w, "UPDATE "+parent+" SET v = 'B' WHERE k = 2")
			if n, _ := res.RowsAffected(); n != 1 {
				t.Errorf("the update affected %d rows, want 1", n)
			}
			want(parent, "1=a,2=B,3=c")
		})
		t.Run("upsert", func(t *testing.T) {
			exec(t, w, "INSERT INTO "+parent+" VALUES (1, 'z') ON CONFLICT (k) DO UPDATE SET v = excluded.v || '!'")
			want(parent, "1=z!,2=B,3=c")
		})
		t.Run("replace", func(t *testing.T) {
			exec(t, w, "REPLACE INTO "+parent+" VALUES (3, 'C')")
			want(parent, "1=z!,2=B,3=C")
		})
		t.Run("returning", func(t *testing.T) {
			if got := read[int64](t, w, "INSERT INTO "+parent+" VALUES (4, 'd') RETURNING k"); !reflect.DeepEqual(got, []int64{4}) {
				t.Errorf("RETURNING gave %v, want 4", got)
			}
		})
		t.Run("delete", func(t *testing.T) {
			if p == ordinary {
				forbidden(t, db, "DELETE FROM "+parent)
			}
			exec(t, w, "DELETE FROM "+child)
			res := exec(t, w, "DELETE FROM "+parent+" WHERE k >= 3")
			if n, _ := res.RowsAffected(); n != 2 {
				t.Errorf("the delete affected %d rows, want 2", n)
			}
			want(parent, "1=z!,2=B")
			exec(t, w, "DELETE FROM "+parent)
			exec(t, w, "DELETE FROM "+other)
			want(parent, "")
			want(child, "")
		})
		for _, tb := range []string{child, parent, other} {
			exec(t, adm, "DROP TABLE "+tb)
		}
		if n := read[int64](t, db, "SELECT count(*) FROM sqlite_schema WHERE name IN (?, ?, ?)", parent, child, other); n[0] != 0 {
			t.Errorf("%d tables are left after the drop", n[0])
		}
	})
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

// TestIntegrationSchema makes each object of a schema that the survey marks,
// as the administrator, and shows that the server keeps to it.
func TestIntegrationSchema(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db, adm *sql.DB) {
		tb := table(p, "s")
		t.Run("table", func(t *testing.T) {
			if p == ordinary {
				forbidden(t, db, "CREATE TABLE "+tb+"_x (a)")
			}
			exec(t, adm, "CREATE TABLE "+tb+" (id INTEGER PRIMARY KEY, name TEXT UNIQUE, n INTEGER DEFAULT 7 CHECK (n > 0), g INTEGER GENERATED ALWAYS AS (n * 2) VIRTUAL)")
			exec(t, adm, "CREATE INDEX "+tb+"_n ON "+tb+" (n)")
		})
		t.Run("primary_key", func(t *testing.T) {
			exec(t, adm, "INSERT INTO "+tb+" (id, name) VALUES (1, 'a')")
			refused(t, adm, "INSERT INTO "+tb+" (id, name) VALUES (1, 'b')", "UNIQUE constraint failed")
		})
		t.Run("unique_constraint", func(t *testing.T) {
			refused(t, adm, "INSERT INTO "+tb+" (id, name) VALUES (2, 'a')", "UNIQUE constraint failed")
		})
		t.Run("check_constraint", func(t *testing.T) {
			refused(t, adm, "INSERT INTO "+tb+" (id, name, n) VALUES (3, 'c', 0)", "CHECK constraint failed")
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
			// Foreign keys are on (measured).
			exec(t, adm, "CREATE TABLE "+tb+"_c (id INTEGER PRIMARY KEY, parent INTEGER REFERENCES "+tb+" (id))")
			refused(t, adm, "INSERT INTO "+tb+"_c VALUES (1, 99)", "FOREIGN KEY constraint failed")
		})
		t.Run("view", func(t *testing.T) {
			exec(t, adm, "CREATE VIEW "+tb+"_v AS SELECT id, name FROM "+tb)
			if got := read[string](t, db, "SELECT name FROM "+tb+"_v"); !reflect.DeepEqual(got, []string{"a"}) {
				t.Errorf("the view gave %q, want a", got)
			}
		})
		t.Run("without_rowid", func(t *testing.T) {
			exec(t, adm, "CREATE TABLE "+tb+"_w (id INTEGER PRIMARY KEY, seen INTEGER) WITHOUT ROWID")
		})
		t.Run("trigger", func(t *testing.T) {
			exec(t, adm, "CREATE TRIGGER "+tb+"_t AFTER INSERT ON "+tb+" BEGIN INSERT INTO "+tb+"_w VALUES (new.id, 1); END")
			exec(t, adm, "INSERT INTO "+tb+" (id, name) VALUES (4, 'd')")
			if got := read[int64](t, db, "SELECT id FROM "+tb+"_w"); !reflect.DeepEqual(got, []int64{4}) {
				t.Errorf("the trigger wrote %v, want 4", got)
			}
		})
		t.Run("alter_column", func(t *testing.T) {
			exec(t, adm, "ALTER TABLE "+tb+" ALTER COLUMN n TO n INTEGER DEFAULT 8")
			if got := read[string](t, db, "SELECT dflt_value FROM pragma_table_info(?) WHERE name = 'n'", tb); !reflect.DeepEqual(got, []string{"8"}) {
				t.Errorf("the default after ALTER COLUMN is %q, want 8", got)
			}
		})
		t.Run("strict_table", func(t *testing.T) {
			exec(t, adm, "CREATE TABLE "+tb+"_st (i INTEGER, a ANY) STRICT")
			refused(t, adm, "INSERT INTO "+tb+"_st VALUES ('a', 1)", "cannot store TEXT value in INTEGER column")
			// ANY reads by the storage class of each value (D140).
			exec(t, adm, "INSERT INTO "+tb+"_st VALUES (1, 'x'), (2, 2.5)")
			if got := read[any](t, db, "SELECT a FROM "+tb+"_st ORDER BY i"); !reflect.DeepEqual(got, []any{"x", 2.5}) {
				t.Errorf("the ANY column gave %#v, want x and 2.5", got)
			}
		})
		exec(t, adm, "DROP VIEW IF EXISTS "+tb+"_v")
		for _, name := range []string{tb + "_c", tb + "_w", tb + "_st", tb} {
			exec(t, adm, "DROP TABLE IF EXISTS "+name)
		}
	})
}

// TestIntegrationFeatures uses each feature of libSQL that the survey marks,
// and shows the refusal of each that it marks no.
func TestIntegrationFeatures(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db, adm *sql.DB) {
		tb := table(p, "f")
		exec(t, adm, "CREATE TABLE "+tb+" (id INTEGER PRIMARY KEY, e F32_BLOB(3))")
		t.Run("vector_index", func(t *testing.T) {
			exec(t, adm, "CREATE INDEX "+tb+"_idx ON "+tb+" (libsql_vector_idx(e))")
			exec(t, adm, "INSERT INTO "+tb+" VALUES (1, vector32('[1,0,0]')), (2, vector32('[0,1,0]'))")
			if got := read[int64](t, db, "SELECT id FROM vector_top_k(?, vector32('[1,0.1,0]'), 1)", tb+"_idx"); !reflect.DeepEqual(got, []int64{1}) {
				t.Errorf("vector_top_k gave %v, want 1", got)
			}
		})
		t.Run("random_rowid", func(t *testing.T) {
			exec(t, adm, "CREATE TABLE "+tb+"_rr (a) RANDOM ROWID")
			exec(t, adm, "INSERT INTO "+tb+"_rr VALUES (1)")
			if got := read[int64](t, db, "SELECT rowid > 1 FROM "+tb+"_rr"); !reflect.DeepEqual(got, []int64{1}) {
				t.Errorf("the random rowid gave %v", got)
			}
		})
		t.Run("attach", func(t *testing.T) {
			refused(t, db, "ATTACH DATABASE 'other' AS other", "unsupported statement")
		})
		t.Run("user_function_in_wasm", func(t *testing.T) {
			refused(t, db, "CREATE FUNCTION dbimp_f LANGUAGE wasm AS X'00'", "SQL_PARSE_ERROR")
		})
		t.Run("begin_concurrent", func(t *testing.T) {
			refused(t, db, "BEGIN CONCURRENT", "SQL_PARSE_ERROR")
		})
		t.Run("namespaces", func(t *testing.T) {
			// The server runs without namespaces, so it ignores x-namespace
			// and answers from the default database (measured).
			if got := read[int64](t, db, "SELECT count(*) FROM sqlite_schema WHERE name = ?", tb, libsql.WithNamespace("dbimp_other")); !reflect.DeepEqual(got, []int64{1}) {
				t.Errorf("another namespace saw %v tables named %s, want the default database", got, tb)
			}
		})
		t.Run("transaction_over_a_stream", func(t *testing.T) {
			tx, err := adm.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(t.Context(), "DELETE FROM "+tb); err != nil {
				t.Fatal(err)
			}
			var in int64
			if err := tx.QueryRowContext(t.Context(), "SELECT count(*) FROM "+tb).Scan(&in); err != nil || in != 0 {
				t.Errorf("the transaction saw %d rows and %v, want 0", in, err)
			}
			if n := read[int64](t, db, "SELECT count(*) FROM "+tb); n[0] == 0 {
				t.Error("a read outside the transaction saw its delete")
			}
		})
		t.Run("read-only_transaction", func(t *testing.T) {
			// The server takes a write inside BEGIN TRANSACTION READONLY
			// (measured), so the driver refuses ReadOnly (D150).
			if _, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true}); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("a read-only transaction gave %v, want %v", err, dbimp.ErrNotSupported)
			}
		})
		t.Run("cursor", func(t *testing.T) {
			if got := read[int64](t, db, "SELECT id FROM "+tb+" ORDER BY id"); !reflect.DeepEqual(got, []int64{1, 2}) {
				t.Errorf("the cursor gave %v, want 1 and 2", got)
			}
		})
		t.Run("named_parameters", func(t *testing.T) {
			if got := read[string](t, db, "SELECT :a || @b || $c", sql.Named("a", "x"), sql.Named("b", "y"), sql.Named("c", "z")); !reflect.DeepEqual(got, []string{"xyz"}) {
				t.Errorf("the named parameters gave %q, want xyz", got)
			}
		})
		t.Run("batch_conditions", func(t *testing.T) {
			status, body := hrana(t, p, `{"baton":null,"requests":[{"type":"batch","batch":{"steps":[{"stmt":{"sql":"SELECT 1"}},{"stmt":{"sql":"SELECT 2"},"condition":{"type":"not","cond":{"type":"ok","step":0}}}]}},{"type":"close"}]}`)
			if status != http.StatusOK || !strings.Contains(body, `"step_results":[{`) || !strings.Contains(body, `},null]`) {
				t.Errorf("the batch gave HTTP %d %s, want the second step skipped", status, body)
			}
		})
		t.Run("stored_statement", func(t *testing.T) {
			status, body := hrana(t, p, `{"baton":null,"requests":[{"type":"store_sql","sql_id":1,"sql":"SELECT ? + 1"},{"type":"execute","stmt":{"sql_id":1,"args":[{"type":"integer","value":"41"}]}},{"type":"close"}]}`)
			if status != http.StatusOK || !strings.Contains(body, `"value":"42"`) {
				t.Errorf("the stored statement gave HTTP %d %s, want 42", status, body)
			}
		})
		t.Run("describe", func(t *testing.T) {
			status, body := hrana(t, p, `{"baton":null,"requests":[{"type":"describe","sql":"SELECT id FROM `+tb+` WHERE id = ?"},{"type":"close"}]}`)
			if status != http.StatusOK || !strings.Contains(body, `"is_readonly":true`) {
				t.Errorf("describe gave HTTP %d %s, want a read-only statement", status, body)
			}
		})
		t.Run("sequence", func(t *testing.T) {
			status, body := hrana(t, p, `{"baton":null,"requests":[{"type":"sequence","sql":"SELECT 1; SELECT 2"},{"type":"close"}]}`)
			if status != http.StatusOK || !strings.Contains(body, `"type":"sequence"`) {
				t.Errorf("the sequence gave HTTP %d %s", status, body)
			}
		})
		exec(t, adm, "DROP TABLE IF EXISTS "+tb+"_rr")
		exec(t, adm, "DROP TABLE IF EXISTS "+tb)
	})
}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type of the
// type table (D147), as the administrator. The ordinary user can write
// nothing, so it reads what the administrator wrote in TestIntegrationScan.
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
			{Name: "unicode", In: "é 日本 🙂"}, {Name: "quotes", In: `say 'hi' "x" \ bye`}, {Name: "hex form", In: "x'41'"},
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
			{Name: "two", In: int64(2), Want: true},
		}, nil},
		{"DATE", "DATE", []dbimptest.Value{
			{Name: "null", In: nil}, {Name: "first", In: day(1000, 1, 1)}, {Name: "last", In: day(9999, 12, 31)},
			{Name: "not a date", In: "yesterday"},
		}, nil},
		{"DATETIME", "DATETIME", []dbimptest.Value{
			{Name: "null", In: nil},
			{Name: "nanoseconds", In: dbimp.LocalDateTime{Date: day(2026, 10, 1), Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}}},
			{Name: "midnight", In: dbimp.LocalDateTime{Date: day(1000, 1, 1)}},
		}, nil},
		{"TIMESTAMP", "TIMESTAMP", []dbimptest.Value{
			{Name: "null", In: nil},
			{Name: "utc", In: time.Date(2026, 10, 1, 12, 34, 56, 123456789, time.UTC)},
			{Name: "offset", In: time.Date(2026, 10, 1, 12, 34, 56, 0, time.FixedZone("", 5*3600+1800))},
		}, sameInstant},
		{"F32_BLOB", "F32_BLOB(3)", []dbimptest.Value{
			{Name: "null", In: nil},
			{Name: "vector", In: vectorBytes(1.5, -2, 3), Want: dbimp.Vector[float32]{1.5, -2, 3}},
			{Name: "zero", In: vectorBytes(0, 0, 0), Want: dbimp.Vector[float32]{0, 0, 0}},
		}, nil},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			p := admin
			db := openAs(t, p)
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
	}
}

// vectorBytes returns the bytes of a vector of float32, in little-endian
// order, as F32_BLOB holds them.
func vectorBytes(vals ...float32) []byte {
	var b []byte
	for _, v := range vals {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	return b
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
