package spanner_test

import (
	"bytes"
	"database/sql"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/spanner"
)

// quote writes a string literal of GoogleSQL. A backslash and a quote escape in
// it, and so does a line break.
func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`).Replace(s) + "'"
}

// rtValue is one value of a round trip: the value that the test writes as an
// argument, the value that a read returns, and the expression that writes it as a
// literal.
type rtValue struct {
	name string
	in   any
	// want is the value that a read returns. It is in when it is nil and in is not.
	want any
	// lit is the expression that writes the value in SQL.
	lit string
}

// rtCase is the round trip of one type.
type rtCase struct {
	// column is the type of the column.
	column string
	// store is the expression that takes the argument, ? by default, such as
	// CAST(? AS STRING) for a type that a column cannot have.
	store string
	// load is the expression that reads the column, v by default.
	load string
	// equal compares a value that a read returned with the value wanted, and
	// defaults to same.
	equal  func(got, want any) bool
	values []rtValue
}

// null is the value NULL.
var null = rtValue{name: "null", lit: "NULL"}

// same compares two values by their type and their value. A NaN is a NaN, and a
// decimal is its number.
func same(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case []byte:
		g, ok := got.([]byte)
		return ok && bytes.Equal(g, w)
	case time.Time:
		g, ok := got.(time.Time)
		return ok && g.Equal(w) && g.Location() == time.UTC
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !same(g[i], w[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// sameInput reports whether two values of a case are the same value to write: they
// have one type and print the same, and both are nil or neither is. A NaN is the same
// as a NaN, and a nil slice differs from an empty one.
func sameInput(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb || fmt.Sprint(a) != fmt.Sprint(b) {
		return false
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	switch va.Kind() {
	case reflect.Slice, reflect.Map:
		return va.IsNil() == vb.IsNil()
	}
	return true
}

// newCase returns the round trip of the case, in a table of its own.
func newCase(typ string, c rtCase) dbimptest.RoundTripCase {
	tbl := name("rt_" + strings.ToLower(typ))
	store, load := c.store, c.load
	if store == "" {
		store = "?"
	}
	if load == "" {
		load = "v"
	}
	values := make([]dbimptest.Value, len(c.values))
	lits := map[string]string{}
	for i, v := range c.values {
		values[i] = dbimptest.Value{Name: v.name, In: v.in, Want: v.want}
		lits[v.name] = v.lit
	}
	eq := c.equal
	if eq == nil {
		eq = same
	}
	return dbimptest.RoundTripCase{
		Type:     typ,
		Setup:    []string{"CREATE TABLE " + tbl + " (k STRING(128) NOT NULL, v " + c.column + ") PRIMARY KEY (k)"},
		Teardown: []string{"DROP TABLE " + tbl},
		Insert:   "INSERT INTO " + tbl + " (k, v) VALUES (?, " + store + ")",
		Literal: func(key string, v any) (string, error) {
			for _, cv := range c.values {
				if sameInput(cv.in, v) && cv.lit != "" {
					return "INSERT INTO " + tbl + " (k, v) VALUES (" + quote(key) + ", " + cv.lit + ")", nil
				}
			}
			return "", fmt.Errorf("writing %v as a literal: %w", v, dbimp.ErrNotSupported)
		},
		Select: "SELECT " + load + " FROM " + tbl + " WHERE k = ?",
		Update: "UPDATE " + tbl + " SET v = " + store + " WHERE k = ?",
		Delete: "DELETE FROM " + tbl + " WHERE k = ?",
		Values: values,
		Equal:  eq,
	}
}

// TestIntegrationRoundTrip stores every type that features.json marks yes in a column
// of that type, as dbimptest.RoundTrip does (step 14a): as a bound argument and as a
// literal, and it updates, reads and deletes each value. INTERVAL cannot be the type
// of a column, so its values go in as the text of a STRING column, which the statements
// turn into an INTERVAL. The types that the survey marks no, STRUCT and TOKENLIST, have
// a test of their refusal.
func TestIntegrationRoundTrip(t *testing.T) {
	db := connect(t)
	roundTrip := func(t *testing.T, typ string, c rtCase) {
		t.Helper()
		dbimptest.RoundTrip(t, db, newCase(typ, c))
	}
	long := strings.Repeat("x", 100000)
	longBytes := bytes.Repeat([]byte{0, 1, 254, 255}, 25000)
	maxDec := "99999999999999999999999999999.999999999"
	day := func(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
	at := func(y int, m time.Month, d, h, mi, s, ns int) time.Time {
		return time.Date(y, m, d, h, mi, s, ns, time.UTC)
	}
	obj := map[string]any{"a": int64(1), "b": []any{int64(1), nil, "x"}, "c": map[string]any{"d": true}}
	cases := map[string]rtCase{
		"BOOL": {column: "BOOL", values: []rtValue{
			{name: "true", in: true, lit: "TRUE"},
			{name: "false", in: false, lit: "FALSE"},
			null,
		}},
		"INT64": {column: "INT64", values: []rtValue{
			{name: "min", in: int64(math.MinInt64), lit: "-9223372036854775808"},
			{name: "zero", in: int64(0), lit: "0"},
			{name: "max", in: int64(math.MaxInt64), lit: "9223372036854775807"},
			{name: "one", in: int64(1), lit: "1"},
			null,
		}},
		"FLOAT32": {column: "FLOAT32", values: []rtValue{
			{name: "zero", in: float32(0), want: 0.0, lit: "CAST(0 AS FLOAT32)"},
			{name: "fraction", in: float32(0.1), want: float64(float32(0.1)), lit: "CAST(0.1 AS FLOAT32)"},
			{name: "largest", in: float32(math.MaxFloat32), want: float64(float32(math.MaxFloat32)), lit: "CAST(3.4028234663852886e+38 AS FLOAT32)"},
			{name: "NaN", in: float32(math.NaN()), want: math.NaN(), lit: "CAST('NaN' AS FLOAT32)"},
			null,
		}},
		"FLOAT64": {column: "FLOAT64", values: []rtValue{
			{name: "min", in: -math.MaxFloat64, lit: "-1.7976931348623157e308"},
			{name: "zero", in: 0.0, lit: "0.0"},
			{name: "smallest", in: math.SmallestNonzeroFloat64, lit: "4.9e-324"},
			{name: "max", in: math.MaxFloat64, lit: "1.7976931348623157e308"},
			{name: "fraction", in: 0.1, lit: "0.1"},
			{name: "NaN", in: math.NaN(), lit: "CAST('NaN' AS FLOAT64)"},
			{name: "inf", in: math.Inf(1), lit: "CAST('inf' AS FLOAT64)"},
			{name: "-inf", in: math.Inf(-1), lit: "CAST('-inf' AS FLOAT64)"},
			null,
		}},
		"NUMERIC": {column: "NUMERIC", values: []rtValue{
			{name: "min", in: dec(t, "-"+maxDec), lit: "NUMERIC '-" + maxDec + "'"},
			{name: "zero", in: dec(t, "0"), lit: "NUMERIC '0'"},
			{name: "the last digit", in: dec(t, "0.000000001"), lit: "NUMERIC '0.000000001'"},
			{name: "max", in: dec(t, maxDec), lit: "NUMERIC '" + maxDec + "'"},
			{name: "whole", in: dec(t, "12345"), lit: "NUMERIC '12345'"},
			null,
		}},
		"STRING": {column: "STRING(MAX)", values: []rtValue{
			{name: "empty", in: "", lit: "''"},
			{name: "unicode", in: "héllo 世界 🙂", lit: quote("héllo 世界 🙂")},
			{name: "quotes", in: "it's \"x\" \\ \n", lit: quote("it's \"x\" \\ \n")},
			{name: "long", in: long, lit: quote(long)},
			null,
		}},
		"BYTES": {column: "BYTES(MAX)", values: []rtValue{
			{name: "empty", in: []byte{}, lit: "b''"},
			{name: "binary", in: []byte{0, 255}, lit: `b'\x00\xff'`},
			{name: "long", in: longBytes, lit: ""},
			null,
		}},
		"DATE": {column: "DATE", values: []rtValue{
			{name: "min", in: day(1, 1, 1), lit: "DATE '0001-01-01'"},
			{name: "epoch", in: day(1970, 1, 1), lit: "DATE '1970-01-01'"},
			{name: "leap day", in: day(2024, 2, 29), lit: "DATE '2024-02-29'"},
			{name: "max", in: day(9999, 12, 31), lit: "DATE '9999-12-31'"},
			null,
		}},
		"TIMESTAMP": {column: "TIMESTAMP", values: []rtValue{
			{name: "min", in: at(1, 1, 1, 0, 0, 0, 0), lit: "TIMESTAMP '0001-01-01T00:00:00Z'"},
			{name: "nanoseconds", in: at(2024, 1, 2, 3, 4, 5, 123456789), lit: "TIMESTAMP '2024-01-02T03:04:05.123456789Z'"},
			{name: "before 1970", in: at(1969, 7, 20, 20, 17, 40, 1), lit: "TIMESTAMP '1969-07-20T20:17:40.000000001Z'"},
			{name: "in a zone", in: time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 5*3600+1800)), want: at(2024, 1, 1, 21, 34, 5, 0), lit: "TIMESTAMP '2024-01-02T03:04:05+05:30'"},
			{name: "max", in: at(9999, 12, 31, 23, 59, 59, 999999999), lit: "TIMESTAMP '9999-12-31T23:59:59.999999999Z'"},
			null,
		}},
		"JSON": {column: "JSON", values: []rtValue{
			{name: "object", in: obj, lit: `JSON '{"a":1,"b":[1,null,"x"],"c":{"d":true}}'`},
			{name: "empty object", in: map[string]any{}, lit: "JSON '{}'"},
			{name: "array", in: jsontext.Value(`[1,2,3]`), want: []any{int64(1), int64(2), int64(3)}, lit: "JSON '[1,2,3]'"},
			{name: "string", in: jsontext.Value(`"s"`), want: "s", lit: `JSON '"s"'`},
			null,
		}},
		"UUID": {column: "UUID", values: []rtValue{
			{name: "zero", in: uuid.UUID{}, lit: "CAST('00000000-0000-0000-0000-000000000000' AS UUID)"},
			{name: "a UUID", in: uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"), lit: "CAST('f47ac10b-58cc-4372-a567-0e02b2c3d479' AS UUID)"},
			{name: "max", in: uuid.Max(), lit: "CAST('ffffffff-ffff-ffff-ffff-ffffffffffff' AS UUID)"},
			null,
		}},
		"INTERVAL": {column: "STRING(MAX)", store: "CAST(? AS STRING)", load: "CAST(v AS INTERVAL)", values: []rtValue{
			{name: "a day", in: dbimp.Interval{Days: 1}, lit: "'P1D'"},
			{name: "negative months", in: dbimp.Interval{Months: -5}, lit: "'P-5M'"},
			{name: "every part", in: dbimp.Interval{Months: 14, Days: 3, Nanoseconds: (4*3600 + 5*60 + 6) * 1e9}, lit: "'P1Y2M3DT4H5M6S'"},
			{name: "a fraction", in: dbimp.Interval{Nanoseconds: -1500000000}, lit: "'PT-1.5S'"},
			{name: "zero", in: dbimp.Interval{}, lit: "'P0Y'"},
		}},
		"ARRAY": {column: "ARRAY<INT64>", values: []rtValue{
			{name: "two", in: []int64{1, 2}, want: []any{int64(1), int64(2)}, lit: "[1, 2]"},
			{name: "empty", in: []int64{}, want: []any{}, lit: "ARRAY<INT64>[]"},
			{name: "a NULL element", in: []any{int64(1), nil}, want: []any{int64(1), nil}, lit: "[1, NULL]"},
			{name: "limits", in: []int64{math.MinInt64, math.MaxInt64}, want: []any{int64(math.MinInt64), int64(math.MaxInt64)}, lit: "[-9223372036854775808, 9223372036854775807]"},
			{name: "a NULL array", lit: "CAST(NULL AS ARRAY<INT64>)"},
		}},
	}
	for _, typ := range []string{"BOOL", "INT64", "FLOAT32", "FLOAT64", "NUMERIC", "STRING", "BYTES", "DATE", "TIMESTAMP", "JSON", "UUID", "INTERVAL", "ARRAY"} {
		t.Run(typ, func(t *testing.T) {
			roundTrip(t, typ, cases[typ])
		})
	}
	// A string, a number and a list of STRING as the element of an ARRAY keep their
	// types in an array column too.
	t.Run("ARRAY of every type", func(t *testing.T) {
		tbl := name("rt_arrays")
		ddl(t, db, "CREATE TABLE "+tbl+" (k INT64 NOT NULL, s ARRAY<STRING(MAX)>, f ARRAY<FLOAT64>, b ARRAY<BOOL>, y ARRAY<BYTES(MAX)>, d ARRAY<DATE>, ts ARRAY<TIMESTAMP>, n ARRAY<NUMERIC>, j ARRAY<JSON>, u ARRAY<UUID>) PRIMARY KEY (k)", "DROP TABLE "+tbl)
		stamp := at(2024, 1, 2, 3, 4, 5, 0)
		u := uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")
		exec(t, db, "INSERT INTO "+tbl+" (k, s, f, b, y, d, ts, n, j, u) VALUES (@k, @s, @f, @b, @y, @d, @ts, @n, @j, @u)",
			sql.Named("k", int64(1)), sql.Named("s", []any{"a", nil}), sql.Named("f", []float64{1.5, math.NaN()}), sql.Named("b", []any{true, nil}),
			sql.Named("y", [][]byte{[]byte("abc")}), sql.Named("d", []dbimp.Date{day(2024, 1, 2)}), sql.Named("ts", []time.Time{stamp}),
			sql.Named("n", []*apd.Decimal{dec(t, "1.5")}), sql.Named("j", []jsontext.Value{jsontext.Value(`{"a":1}`)}), sql.Named("u", []uuid.UUID{u}))
		got := rowsOf(t, db, "SELECT s, f, b, y, d, ts, n, j, u FROM "+tbl+" WHERE k = 1")
		want := []any{
			[]any{"a", nil}, []any{1.5, math.NaN()}, []any{true, nil}, []any{[]byte("abc")}, []any{day(2024, 1, 2)},
			[]any{stamp}, []any{dec(t, "1.5")}, []any{map[string]any{"a": int64(1)}}, []any{u},
		}
		if len(got) != 1 {
			t.Fatalf("read %d rows, want 1", len(got))
		}
		for i := range want {
			if !same(got[0][i], want[i]) {
				t.Errorf("column %d is %#v, want %#v", i, got[0][i], want[i])
			}
		}
	})
	t.Run("STRUCT", func(t *testing.T) {
		// A struct is not a column type, and the server refuses it as a column of a
		// result (recorded: "STRUCT as a column").
		err := failure(t, db, "SELECT STRUCT(1 AS a, 'x' AS b) AS s")
		serr, ok := errors.AsType[*spanner.Error](err)
		if !ok || serr.HTTPStatus != 501 || !strings.Contains(serr.Message, "struct") {
			t.Errorf("a STRUCT column gave %v, want HTTP 501", err)
		}
	})
	t.Run("TOKENLIST", func(t *testing.T) {
		// A TOKENLIST column cannot be selected (recorded: "a TOKENLIST column that
		// the statement names").
		tbl := name("rt_tokens")
		ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, body STRING(MAX), body_tokens TOKENLIST AS (TOKENIZE_FULLTEXT(body)) HIDDEN) PRIMARY KEY (id)", "DROP TABLE "+tbl)
		exec(t, db, "INSERT INTO "+tbl+" (id, body) VALUES (1, 'hello')")
		err := failure(t, db, "SELECT body_tokens FROM "+tbl)
		refusedBy(t, err, "TOKENLIST")
	})
}
