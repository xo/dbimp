package databend_test

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// These tests hold step 14a of docs/DRIVER.md: each entry of
// testdata/databend/features.json names one of their subtests. The codes are
// the ones that step 6 recorded: 1005 for a syntax error, 1063 for a refused
// privilege, 1006 for a bad argument, 1404 for a feature of the Enterprise
// Edition, 1702 for a task and 3901 for a storage that the server lacks.

// super runs stmt, which needs the privilege Super on 1.2.881: the
// administrator runs it, and the ordinary user gets 1063 (recorded).
func super(t *testing.T, p principal, db *sql.DB, stmt string) bool {
	t.Helper()
	if p == ordinary {
		refused(t, db, stmt, 1063)
		return false
	}
	exec(t, db, stmt)
	return true
}

func TestIntegrationCRUD(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table("crud_" + p.name)
		exec(t, db, "CREATE OR REPLACE TABLE "+tb+" (k INT, v STRING)")
		t.Run("insert", func(t *testing.T) {
			res := exec(t, db, "INSERT INTO "+tb+" VALUES (?, ?), (?, ?)", int64(1), "a", int64(2), "b")
			if n, err := res.RowsAffected(); err != nil || n != 2 {
				t.Errorf("the insert affected %d, %v, want 2", n, err)
			}
		})
		t.Run("select", func(t *testing.T) {
			same(t, "the select", column(t, db, "SELECT v FROM "+tb+" ORDER BY k"), "a", "b")
		})
		t.Run("update", func(t *testing.T) {
			exec(t, db, "UPDATE "+tb+" SET v = ? WHERE k = ?", "c", int64(1))
			same(t, "the update", column(t, db, "SELECT v FROM "+tb+" WHERE k = 1"), "c")
		})
		t.Run("replace_into", func(t *testing.T) {
			exec(t, db, "REPLACE INTO "+tb+" ON (k) VALUES (1, 'r')")
			same(t, "the replace", column(t, db, "SELECT v FROM "+tb+" WHERE k = 1"), "r")
		})
		t.Run("merge_into", func(t *testing.T) {
			res := exec(t, db, "MERGE INTO "+tb+" AS t USING (SELECT 1 AS k, 'm' AS v UNION ALL SELECT 5, 'n') AS s ON t.k = s.k WHEN MATCHED THEN UPDATE SET t.v = s.v WHEN NOT MATCHED THEN INSERT (k, v) VALUES (s.k, s.v)")
			if n, err := res.RowsAffected(); err != nil || n != 2 {
				t.Errorf("the merge affected %d, %v, want 2", n, err)
			}
			same(t, "the merge", column(t, db, "SELECT v FROM "+tb+" ORDER BY k"), "m", "b", "n")
		})
		t.Run("multi-table_insert", func(t *testing.T) {
			exec(t, db, "INSERT ALL INTO "+tb+" VALUES (k, v) SELECT 8 AS k, 'x' AS v")
			same(t, "the multi-table insert", column(t, db, "SELECT v FROM "+tb+" WHERE k = 8"), "x")
		})
		t.Run("copy_into", func(t *testing.T) {
			stage := prefix + "stage"
			if !super(t, p, db, "CREATE OR REPLACE STAGE "+stage) {
				return
			}
			exec(t, db, "COPY INTO @"+stage+" FROM "+tb)
			exec(t, db, "COPY INTO "+tb+" FROM @"+stage+" FILE_FORMAT = (TYPE = PARQUET) FORCE = TRUE")
			same(t, "the rows after the copy", column(t, db, "SELECT count(*) FROM "+tb), int64(8))
		})
		t.Run("insert_overwrite", func(t *testing.T) {
			exec(t, db, "INSERT OVERWRITE "+tb+" VALUES (7, 'o')")
			same(t, "the overwrite", column(t, db, "SELECT k FROM "+tb), int64(7))
		})
		t.Run("delete", func(t *testing.T) {
			res := exec(t, db, "DELETE FROM "+tb+" WHERE k = ?", int64(7))
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				t.Errorf("the delete affected %d, %v, want 1", n, err)
			}
			same(t, "after the delete", column(t, db, "SELECT count(*) FROM "+tb), int64(0))
		})
	})
}

func TestIntegrationSchema(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table("schema_" + p.name)
		t.Run("table", func(t *testing.T) { exec(t, db, "CREATE OR REPLACE TABLE "+tb+" (k INT, v STRING)") })
		t.Run("primary_key", func(t *testing.T) { refused(t, db, "CREATE OR REPLACE TABLE "+tb+"_pk (k INT PRIMARY KEY)", 1005) })
		t.Run("foreign_key", func(t *testing.T) {
			refused(t, db, "CREATE OR REPLACE TABLE "+tb+"_fk (k INT REFERENCES "+tb+"(k))", 1005)
		})
		t.Run("unique_constraint", func(t *testing.T) { refused(t, db, "CREATE OR REPLACE TABLE "+tb+"_u (k INT UNIQUE)", 1005) })
		t.Run("check_constraint", func(t *testing.T) {
			exec(t, db, "CREATE OR REPLACE TABLE "+tb+"_c (k INT CHECK (k > 0))")
			refused(t, db, "INSERT INTO "+tb+"_c VALUES (-1)", 1133)
		})
		t.Run("default_value", func(t *testing.T) {
			exec(t, db, "CREATE OR REPLACE TABLE "+tb+"_d (k INT DEFAULT 7, v STRING)")
			exec(t, db, "INSERT INTO "+tb+"_d (v) VALUES ('x')")
			same(t, "the default", column(t, db, "SELECT k FROM "+tb+"_d"), int64(7))
		})
		t.Run("view", func(t *testing.T) {
			v := "dbmeta." + prefix + "view"
			exec(t, db, "CREATE OR REPLACE VIEW "+v+" AS SELECT 1 AS one")
			same(t, "the view", column(t, db, "SELECT one FROM "+v), int64(1))
		})
		t.Run("materialized_view", func(t *testing.T) {
			if _, err := db.ExecContext(t.Context(), "CREATE MATERIALIZED VIEW "+tb+"_mv AS SELECT 1 AS one"); err == nil {
				t.Error("a materialized view of no table gave no error")
			}
		})
		t.Run("aggregating_index", func(t *testing.T) {
			stmt := "CREATE OR REPLACE AGGREGATING INDEX " + prefix + "agg_" + p.name + " AS SELECT k, count(*) FROM " + tb + " GROUP BY k"
			if release(t, db) >= "1.2.9" {
				// 1.2.948 has another syntax for it (recorded).
				refused(t, db, stmt, 1005)
				return
			}
			if super(t, p, db, stmt) {
				exec(t, db, "DROP AGGREGATING INDEX "+prefix+"agg_"+p.name)
			}
		})
		t.Run("inverted_index", func(t *testing.T) {
			stmt := "CREATE OR REPLACE INVERTED INDEX " + prefix + "inv ON " + tb + "(v)"
			if release(t, db) >= "1.2.9" {
				exec(t, db, stmt)
				return
			}
			super(t, p, db, stmt)
		})
		t.Run("ngram_index", func(t *testing.T) {
			stmt := "CREATE OR REPLACE NGRAM INDEX " + prefix + "ng ON " + tb + "(v)"
			if release(t, db) >= "1.2.9" {
				exec(t, db, stmt)
				return
			}
			super(t, p, db, stmt)
		})
		t.Run("vector_index", func(t *testing.T) {
			exec(t, db, "CREATE OR REPLACE TABLE "+tb+"_vec (e VECTOR(3), VECTOR INDEX vi (e) distance = 'cosine')")
		})
		t.Run("spatial_index", func(t *testing.T) {
			exec(t, db, "CREATE OR REPLACE TABLE "+tb+"_geo (g GEOMETRY, SPATIAL INDEX si (g))")
		})
		t.Run("sequence", func(t *testing.T) {
			if super(t, p, db, "CREATE SEQUENCE IF NOT EXISTS "+prefix+"seq") {
				same(t, "the sequence", column(t, db, "SELECT nextval("+prefix+"seq)"), int64(1))
			}
		})
		t.Run("stream", func(t *testing.T) {
			stmt := "CREATE OR REPLACE STREAM " + tb + "_st ON TABLE " + tb
			if release(t, db) < "1.2.9" {
				// The license of the Enterprise Edition in 1.2.881 is
				// expired (recorded).
				refused(t, db, stmt, 1404)
				return
			}
			exec(t, db, stmt)
			exec(t, db, "DROP STREAM "+tb+"_st")
		})
		t.Run("stage", func(t *testing.T) { super(t, p, db, "CREATE OR REPLACE STAGE "+prefix+"stage") })
		t.Run("transient_table", func(t *testing.T) { exec(t, db, "CREATE OR REPLACE TRANSIENT TABLE "+tb+"_tr (k INT)") })
		t.Run("temporary_table", func(t *testing.T) {
			// The driver sends no cookie, so the server refuses a temporary
			// table (D122).
			refused(t, db, "CREATE OR REPLACE TEMP TABLE "+prefix+"tmp (k INT)", 1006)
		})
		t.Run("external_table", func(t *testing.T) {
			refused(t, db, "CREATE OR REPLACE TABLE "+tb+"_ext (k INT) 's3://dbimp/x/' CONNECTION = (ACCESS_KEY_ID = 'a' SECRET_ACCESS_KEY = 'b')", 3901)
		})
		t.Run("virtual_column", func(t *testing.T) {
			exec(t, db, "CREATE OR REPLACE TABLE "+tb+"_vc (v VARIANT)")
			refused(t, db, "ALTER TABLE "+tb+"_vc ADD VIRTUAL COLUMN (v['a'] AS va)", 1005)
		})
		t.Run("cluster_by", func(t *testing.T) { exec(t, db, "CREATE OR REPLACE TABLE "+tb+"_cl (k INT, v STRING) CLUSTER BY (k)") })
	})
}

func TestIntegrationFeatures(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table("feat_" + p.name)
		exec(t, db, "CREATE OR REPLACE TABLE "+tb+" (k INT, v VARIANT)")
		exec(t, db, "INSERT INTO "+tb+" VALUES (1, parse_json('{\"k\": [1, null], \"n\": 1.5}'))")
		exec(t, db, "INSERT INTO "+tb+" VALUES (2, parse_json('[]'))")
		t.Run("time_travel_at", func(t *testing.T) {
			snap := column(t, db, "SELECT snapshot_id FROM fuse_snapshot('dbmeta', ?) ORDER BY timestamp LIMIT 1", strings.TrimPrefix(tb, "dbmeta."))
			same(t, "the table at its first snapshot", column(t, db, fmt.Sprintf("SELECT count(*) FROM %s AT (SNAPSHOT => '%s')", tb, snap[0])), int64(1))
		})
		t.Run("flashback_table", func(t *testing.T) {
			snap := column(t, db, "SELECT snapshot_id FROM fuse_snapshot('dbmeta', ?) ORDER BY timestamp LIMIT 1", strings.TrimPrefix(tb, "dbmeta."))
			stmt := fmt.Sprintf("ALTER TABLE %s FLASHBACK TO (SNAPSHOT => '%s')", tb, snap[0])
			if p == ordinary {
				refused(t, db, stmt, 1063)
				return
			}
			exec(t, db, stmt)
			same(t, "the table after the flashback", column(t, db, "SELECT count(*) FROM "+tb), int64(1))
			exec(t, db, "INSERT INTO "+tb+" VALUES (2, parse_json('[]'))")
		})
		t.Run("undrop_table", func(t *testing.T) {
			exec(t, db, "CREATE OR REPLACE TABLE "+tb+"_u (k INT)")
			exec(t, db, "DROP TABLE "+tb+"_u")
			exec(t, db, "UNDROP TABLE "+tb+"_u")
			same(t, "the table after the undrop", column(t, db, "SELECT count(*) FROM "+tb+"_u"), int64(0))
		})
		t.Run("variant_path", func(t *testing.T) {
			rows := column(t, db, "SELECT v:k[0] FROM "+tb+" WHERE k = 1")
			same(t, "the path", rows, int64(1))
		})
		t.Run("qualify", func(t *testing.T) {
			same(t, "qualify", column(t, db, "SELECT number FROM numbers(5) QUALIFY row_number() OVER (ORDER BY number DESC) = 1"), int64(4))
		})
		t.Run("generate_series", func(t *testing.T) {
			same(t, "generate_series", column(t, db, "SELECT * FROM generate_series(1, 3)"), int64(1), int64(2), int64(3))
		})
		t.Run("flatten", func(t *testing.T) {
			same(t, "flatten", column(t, db, "SELECT value FROM FLATTEN(INPUT => parse_json('[1, 2]'))"), int64(1), int64(2))
		})
		t.Run("result_scan", func(t *testing.T) {
			// Each request of HTTP is a session of its own, so it has no last
			// query (recorded).
			refused(t, db, "SELECT * FROM RESULT_SCAN(LAST_QUERY_ID())", 1006)
		})
		t.Run("task", func(t *testing.T) {
			want := 1702
			if p == ordinary {
				want = 1063
			}
			refused(t, db, "CREATE TASK IF NOT EXISTS "+prefix+"task WAREHOUSE = 'x' SCHEDULE = 1 MINUTE AS SELECT 1", want)
		})
		t.Run("attach_table", func(t *testing.T) {
			refused(t, db, "ATTACH TABLE "+tb+"_att 's3://dbimp/x/' CONNECTION = (ACCESS_KEY_ID = 'a' SECRET_ACCESS_KEY = 'b')", 3901)
		})
		t.Run("query_from_a_stage", func(t *testing.T) {
			stage := prefix + "qstage"
			if !super(t, p, db, "CREATE OR REPLACE STAGE "+stage) {
				return
			}
			defer exec(t, db, "DROP STAGE "+stage)
			exec(t, db, "COPY INTO @"+stage+" FROM "+tb)
			same(t, "the rows of the stage", column(t, db, "SELECT count(*) FROM @"+stage), int64(2))
		})
		t.Run("transactions", func(t *testing.T) {
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			exec(t, tx, "INSERT INTO "+tb+" VALUES (3, NULL)")
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			same(t, "the rows after the commit", column(t, db, "SELECT count(*) FROM "+tb), int64(3))
		})
		t.Run("kill_a_query", func(t *testing.T) {
			text := fmt.Sprintf("SELECT number FROM numbers(1000000000000) WHERE number > %d", len(p.name)+int(time.Now().UnixNano()%1e6))
			closeEarly(t, db, text)
			waitGone(t, db, text)
		})
	})
}

// roundTrip returns the round trip of the type typ, as the column type sqlType
// of the table tb.
func roundTrip(typ, sqlType, tb string, values []dbimptest.Value) dbimptest.RoundTripCase {
	return dbimptest.RoundTripCase{
		Type:     typ,
		Setup:    []string{"CREATE OR REPLACE TABLE " + tb + " (k STRING, v " + sqlType + " NULL)"},
		Teardown: []string{"DROP TABLE IF EXISTS " + tb},
		Insert:   "INSERT INTO " + tb + " VALUES (?, ?)",
		Literal: func(key string, v any) (string, error) {
			lit, err := literal(v)
			if err != nil {
				return "", err
			}
			// A literal goes in parentheses, because -128::TINYINT is
			// -(128::TINYINT), which overflows (measured).
			return "INSERT INTO " + tb + " VALUES ('" + key + "', (" + lit + ")::" + sqlType + ")", nil
		},
		Select: "SELECT v FROM " + tb + " WHERE k = ?",
		Update: "UPDATE " + tb + " SET v = ? WHERE k = ?",
		Delete: "DELETE FROM " + tb + " WHERE k = ?",
		Values: values,
		Equal:  equal,
	}
}

// literal writes v as a literal of SQL, for the round trip.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64, float64:
		return fmt.Sprint(v), nil
	case string:
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(v) + "'", nil
	case *apd.Decimal:
		return "'" + v.Text('f') + "'", nil
	case time.Time:
		return "'" + v.UTC().Format("2006-01-02 15:04:05.999999 +00:00") + "'", nil
	}
	return "", fmt.Errorf("a literal of %T: %w", v, dbimp.ErrNotSupported)
}

// equal compares a value read back with the value wanted: a decimal by its
// value, and a time by its instant.
func equal(got, want any) bool {
	switch w := want.(type) {
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case time.Time:
		g, ok := got.(time.Time)
		return ok && g.Equal(w)
	}
	return reflect.DeepEqual(got, want)
}

func decimal(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestIntegrationRoundTrip(t *testing.T) {
	long := strings.Repeat("xé", 5000)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	ints := func(lo, hi int64) []dbimptest.Value {
		return []dbimptest.Value{{Name: "null", In: nil}, {Name: "zero", In: int64(0)}, {Name: "smallest", In: lo}, {Name: "largest", In: hi}}
	}
	for _, tt := range []struct {
		typ, sqlType string
		values       func(t *testing.T) []dbimptest.Value
	}{
		{"boolean", "BOOLEAN", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "false", In: false}, {Name: "true", In: true}}
		}},
		{"tinyint", "TINYINT", func(*testing.T) []dbimptest.Value { return ints(math.MinInt8, math.MaxInt8) }},
		{"smallint", "SMALLINT", func(*testing.T) []dbimptest.Value { return ints(math.MinInt16, math.MaxInt16) }},
		{"int", "INT", func(*testing.T) []dbimptest.Value { return ints(math.MinInt32, math.MaxInt32) }},
		{"bigint", "BIGINT", func(*testing.T) []dbimptest.Value { return ints(math.MinInt64, math.MaxInt64) }},
		{"uint8", "UINT8", func(*testing.T) []dbimptest.Value { return ints(0, math.MaxUint8) }},
		{"uint16", "UINT16", func(*testing.T) []dbimptest.Value { return ints(0, math.MaxUint16) }},
		{"uint32", "UINT32", func(*testing.T) []dbimptest.Value { return ints(0, math.MaxUint32) }},
		{"uint64", "UINT64", func(t *testing.T) []dbimptest.Value {
			return []dbimptest.Value{
				{Name: "null", In: nil}, {Name: "zero", In: int64(0)}, {Name: "int64", In: int64(math.MaxInt64)},
				{Name: "largest", In: uint64(math.MaxUint64), Want: decimal(t, "18446744073709551615")},
			}
		}},
		{"float", "FLOAT", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "half", In: 0.5}, {Name: "large", In: -3.4e38}}
		}},
		{"double", "DOUBLE", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{
				{Name: "null", In: nil}, {Name: "half", In: 0.5}, {Name: "largest", In: math.MaxFloat64},
				{Name: "last digit", In: 0.1 + 0.2},
			}
		}},
		{"decimal", "DECIMAL(38, 10)", func(t *testing.T) []dbimptest.Value {
			return []dbimptest.Value{
				{Name: "null", In: nil}, {Name: "zero", In: decimal(t, "0")},
				{Name: "every digit", In: decimal(t, "1234567890123456789012345678.0123456789")},
				{Name: "smallest", In: decimal(t, "-9999999999999999999999999999.9999999999")},
			}
		}},
		{"date", "DATE", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "first", In: day(1000, 1, 1)}, {Name: "last", In: day(9999, 12, 31)}}
		}},
		{"timestamp", "TIMESTAMP", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{
				{Name: "null", In: nil}, {Name: "microseconds", In: time.Date(2026, 9, 29, 12, 34, 56, 123456000, time.UTC)},
				{Name: "offset", In: time.Date(2026, 9, 29, 12, 34, 56, 0, time.FixedZone("", 5*3600+1800))},
			}
		}},
		{"timestamp_tz", "TIMESTAMP_TZ", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{
				{Name: "null", In: nil}, {Name: "utc", In: time.Date(2026, 9, 29, 12, 34, 56, 123456000, time.UTC)},
				{Name: "epoch", In: time.Unix(0, 0).UTC()},
			}
		}},
		{"interval", "INTERVAL", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "day", In: "1 day"}, {Name: "negative", In: "-1 month"}}
		}},
		{"string", "STRING", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{
				{Name: "null", In: nil}, {Name: "empty", In: ""}, {Name: "long", In: long},
				{Name: "unicode", In: "é 日本 🙂"}, {Name: "quotes", In: `say "hi" \ bye`},
			}
		}},
		{"binary", "BINARY", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "bytes", In: "00ff", Want: []byte{0, 0xff}}, {Name: "empty", In: "", Want: []byte{}}}
		}},
		{"array", "ARRAY(INT NULL)", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "empty", In: "[]", Want: []any{}}, {Name: "values", In: "[1, null, 3]", Want: []any{int64(1), nil, int64(3)}}}
		}},
		{"map", "MAP(STRING, INT NULL)", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "empty", In: "{}", Want: map[string]any{}}, {Name: "values", In: `{"a": 1, "b": null}`, Want: map[string]any{"a": int64(1), "b": nil}}}
		}},
		{"tuple", "TUPLE(INT NULL, STRING NULL)", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "values", In: `[1, "x"]`, Want: []any{int64(1), "x"}}, {Name: "nulls", In: `[null, null]`, Want: []any{nil, nil}}}
		}},
		{"variant", "VARIANT", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{
				{Name: "null", In: nil}, {Name: "object", In: `{"k": [1, null, "s"], "n": 1.5}`, Want: map[string]any{"k": []any{int64(1), nil, "s"}, "n": 1.5}},
				{Name: "array", In: `[]`, Want: []any{}},
			}
		}},
		{"bitmap", "BITMAP", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "values", In: "1,3,5", Want: "1,3,5"}, {Name: "empty", In: "0", Want: "0"}}
		}},
		{"vector", "VECTOR(3)", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "values", In: "[1.5, -2, 3]", Want: []float32{1.5, -2, 3}}, {Name: "zero", In: "[0, 0, 0]", Want: []float32{0, 0, 0}}}
		}},
		{"geometry", "GEOMETRY", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "point", In: "POINT(1 2)"}, {Name: "line", In: "LINESTRING(0 0,1 1)"}}
		}},
		{"geography", "GEOGRAPHY", func(*testing.T) []dbimptest.Value {
			return []dbimptest.Value{{Name: "null", In: nil}, {Name: "point", In: "POINT(3 4)"}, {Name: "origin", In: "POINT(0 0)"}}
		}},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				c := roundTrip(tt.typ, tt.sqlType, table("rt_"+tt.typ+"_"+p.name), tt.values(t))
				switch tt.typ {
				case "timestamp_tz":
					// The server fails an UPDATE of a TIMESTAMP_TZ with 1104
					// "internal error: entered unreachable code" (measured on
					// both releases), and an insert works.
					c.SkipUpdate = func(dbimptest.Value, dbimptest.Value) string {
						return "the server fails an UPDATE of a TIMESTAMP_TZ with 1104 (docs/DATABEND.md)"
					}
				case "tuple":
					// A Variant does not cast to a Tuple (measured), so the
					// test builds the tuple from the fields of the JSON text.
					tb := strings.TrimPrefix(c.Teardown[0], "DROP TABLE IF EXISTS ")
					tuple := "IF(:value IS NULL, NULL, (CAST(parse_json(:value)[0] AS INT), CAST(parse_json(:value)[1] AS STRING)))"
					c.Named = true
					c.Insert = "INSERT INTO " + tb + " SELECT :key, " + tuple
					c.Update = "UPDATE " + tb + " SET v = " + tuple + " WHERE k = :key"
					c.Select = "SELECT v FROM " + tb + " WHERE k = :key"
					c.Delete = "DELETE FROM " + tb + " WHERE k = :key"
					c.Literal = noLiteral
				case "binary", "array", "map", "variant", "vector":
					// The value goes as text, which the column casts, because
					// params has no form for these types (D120).
					c.Insert = "INSERT INTO " + strings.TrimPrefix(c.Teardown[0], "DROP TABLE IF EXISTS ") + " SELECT ?, " + cast(tt.typ, "?", tt.sqlType)
					c.Update = "UPDATE " + strings.TrimPrefix(c.Teardown[0], "DROP TABLE IF EXISTS ") + " SET v = " + cast(tt.typ, "?", tt.sqlType) + " WHERE k = ?"
					c.Literal = noLiteral
				case "bitmap":
					// JSON carries no bytes of a Bitmap, so the test reads it
					// with to_string, and the driver refuses the
					// column itself (D119).
					tb := strings.TrimPrefix(c.Teardown[0], "DROP TABLE IF EXISTS ")
					c.Insert = "INSERT INTO " + tb + " SELECT ?, to_bitmap(?)"
					c.Update = "UPDATE " + tb + " SET v = to_bitmap(?) WHERE k = ?"
					c.Select = "SELECT to_string(v) FROM " + tb + " WHERE k = ?"
					c.Literal = noLiteral
				}
				dbimptest.RoundTrip(t, db, c)
				if tt.typ == "bitmap" {
					tb := strings.TrimPrefix(c.Teardown[0], "DROP TABLE IF EXISTS ")
					exec(t, db, "INSERT INTO "+tb+" SELECT 'raw', to_bitmap('1')")
					var v any
					err := db.QueryRowContext(t.Context(), "SELECT v FROM "+tb+" WHERE k = 'raw'").Scan(&v)
					if !errors.Is(err, dbimp.ErrNotSupported) {
						t.Errorf("reading a Bitmap gave %v, %v, want dbimp.ErrNotSupported (D119)", v, err)
					}
				}
			})
		})
	}
}

// noLiteral is the Literal of a round trip whose value the test writes as
// text that the column casts, which the bound form already covers.
func noLiteral(string, any) (string, error) {
	return "", fmt.Errorf("a literal that the column casts from text: %w", dbimp.ErrNotSupported)
}

// cast returns the SQL that turns the text arg into a value of sqlType.
func cast(typ, arg, sqlType string) string {
	switch typ {
	case "binary":
		return "from_hex(" + arg + ")"
	case "variant":
		return "parse_json(" + arg + ")"
	case "vector":
		// 1.2.881 casts no Variant to a Vector, and casts an Array
		// (measured).
		return "CAST(CAST(parse_json(" + arg + ") AS ARRAY(FLOAT)) AS " + sqlType + ")"
	case "array", "map":
		return "CAST(parse_json(" + arg + ") AS " + sqlType + ")"
	}
	return "CAST(" + arg + " AS " + sqlType + ")"
}
