package bigquery //nolint:testpackage // The round trip shares the helpers of the integration tests, which are in the package.

import (
	"encoding/base64"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// quote writes a string literal of GoogleSQL, with the escapes that it reads in a
// quoted string: a backslash, the quote, and a line feed.
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
	// expr is the expression that takes the argument @value, such as CAST(@value AS
	// INT64). A parameter has the type of its Go value, and a NULL is a STRING, so
	// the statement casts it.
	expr   string
	values []rtValue
}

// newCase returns the round trip of the case, in a table of its own. Each statement
// names its arguments, so that a statement can use the value more than once.
func newCase(t testing.TB, name string, c rtCase) dbimptest.RoundTripCase {
	t.Helper()
	tbl := table(t, "rt_"+name)
	values := make([]dbimptest.Value, len(c.values))
	lits := map[string]string{}
	for i, v := range c.values {
		values[i] = dbimptest.Value{Name: v.name, In: v.in, Want: v.want}
		lits[fmt.Sprint(v.in)] = v.lit
	}
	return dbimptest.RoundTripCase{
		Type:     name,
		Named:    true,
		Setup:    []string{"CREATE OR REPLACE TABLE " + tbl + " (k STRING, v " + c.column + ")"},
		Teardown: []string{"DROP TABLE IF EXISTS " + tbl},
		Insert:   "INSERT INTO " + tbl + " (k, v) SELECT @key, " + c.expr,
		Literal: func(key string, v any) (string, error) {
			lit, ok := lits[fmt.Sprint(v)]
			if !ok || lit == "" {
				return "", fmt.Errorf("writing %v as a literal: %w", v, dbimp.ErrNotSupported)
			}
			return "INSERT INTO " + tbl + " (k, v) VALUES (" + quote(key) + ", " + lit + ")", nil
		},
		Select: "SELECT v FROM " + tbl + " WHERE k = @key",
		Update: "UPDATE " + tbl + " SET v = " + c.expr + " WHERE k = @key",
		Delete: "DELETE FROM " + tbl + " WHERE k = @key",
		Values: values,
		Equal:  same,
	}
}

// instant makes a time in UTC.
func instant(year int, month time.Month, day, hour, minute, second, micro int) time.Time {
	return time.Date(year, month, day, hour, minute, second, micro*1000, time.UTC)
}

// TestIntegrationRoundTrip stores every type that features.json marks yes in a column of
// that type, as dbimptest.RoundTrip does (step 14a): as a bound argument and as a
// literal, and it updates, reads and deletes each value. A value that has no Go type to
// bind, such as a JSON document, a STRUCT, an ARRAY, a RANGE and a GEOGRAPHY, goes in as
// text that the statement parses.
func TestIntegrationRoundTrip(t *testing.T) {
	db := connect(t)
	cfg := itConfig(t)
	roundTrip := func(t *testing.T, name string, c rtCase) {
		t.Helper()
		if isEmulator(cfg) && !slices.Contains(emulatorTypes, name) {
			t.Skip("the emulator writes this type in another form than the service (docs/BIGQUERY.md, Types)")
		}
		dbimptest.RoundTrip(t, db, newCase(t, strings.ToLower(name), c))
	}
	big := strings.Repeat("x", 100000)
	maxNumeric := "99999999999999999999999999999.999999999"
	maxBig := "578960446186580977117854925043439539266.34992332820282019728792003956564819967"
	minBig := "-578960446186580977117854925043439539266.34992332820282019728792003956564819968"
	date := func(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
	clock := func(h, m, s, micro int) dbimp.LocalTime {
		return dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: micro * 1000}
	}
	dtime := func(d dbimp.Date, c dbimp.LocalTime) dbimp.LocalDateTime {
		return dbimp.LocalDateTime{Date: d, Time: c}
	}
	cases := map[string]rtCase{
		"INTEGER": {column: "INT64", expr: "CAST(@value AS INT64)", values: []rtValue{
			{name: "min", in: int64(math.MinInt64), lit: "-9223372036854775808"},
			{name: "zero", in: int64(0), lit: "0"},
			{name: "max", in: int64(math.MaxInt64), lit: "9223372036854775807"},
			{name: "null", in: nil, lit: "CAST(NULL AS INT64)"},
		}},
		"FLOAT": {column: "FLOAT64", expr: "CAST(@value AS FLOAT64)", values: []rtValue{
			{name: "min", in: -math.MaxFloat64, lit: "-1.7976931348623157e308"},
			{name: "zero", in: 0.0, lit: "0.0"},
			{name: "smallest", in: 5e-324, lit: "4.9e-324"},
			{name: "max", in: math.MaxFloat64, lit: "1.7976931348623157e308"},
			{name: "fraction", in: 0.1, lit: "0.1"},
			{name: "NaN", in: math.NaN(), lit: "CAST('NaN' AS FLOAT64)"},
			{name: "infinity", in: math.Inf(1), lit: "CAST('inf' AS FLOAT64)"},
			{name: "negative infinity", in: math.Inf(-1), lit: "CAST('-inf' AS FLOAT64)"},
			{name: "null", in: nil, lit: "CAST(NULL AS FLOAT64)"},
		}},
		"NUMERIC": {column: "NUMERIC", expr: "CAST(@value AS NUMERIC)", values: []rtValue{
			{name: "min", in: dec(t, "-"+maxNumeric), lit: "NUMERIC '-" + maxNumeric + "'"},
			{name: "zero", in: dec(t, "0"), lit: "NUMERIC '0'"},
			{name: "last digit", in: dec(t, "0.000000001"), lit: "NUMERIC '0.000000001'"},
			{name: "max", in: dec(t, maxNumeric), lit: "NUMERIC '" + maxNumeric + "'"},
			{name: "null", in: nil, lit: "CAST(NULL AS NUMERIC)"},
		}},
		"BIGNUMERIC": {column: "BIGNUMERIC", expr: "CAST(@value AS BIGNUMERIC)", values: []rtValue{
			{name: "min", in: dec(t, minBig), lit: "BIGNUMERIC '" + minBig + "'"},
			{name: "last digit", in: dec(t, "0.00000000000000000000000000000000000001"), lit: "BIGNUMERIC '0.00000000000000000000000000000000000001'"},
			{name: "max", in: dec(t, maxBig), lit: "BIGNUMERIC '" + maxBig + "'"},
			{name: "null", in: nil, lit: "CAST(NULL AS BIGNUMERIC)"},
		}},
		"BOOLEAN": {column: "BOOL", expr: "CAST(@value AS BOOL)", values: []rtValue{
			{name: "true", in: true, lit: "TRUE"},
			{name: "false", in: false, lit: "FALSE"},
			{name: "null", in: nil, lit: "CAST(NULL AS BOOL)"},
		}},
		"STRING": {column: "STRING", expr: "CAST(@value AS STRING)", values: []rtValue{
			{name: "empty", in: "", lit: "''"},
			{name: "unicode", in: "héllo wörld ✓ 日本語 🎉", lit: quote("héllo wörld ✓ 日本語 🎉")},
			{name: "quotes", in: "it's \"q\" \\ back\nline", lit: quote("it's \"q\" \\ back\nline")},
			{name: "long", in: big, lit: quote(big)},
			{name: "null", in: nil, lit: "CAST(NULL AS STRING)"},
		}},
		"BYTES": {column: "BYTES", expr: "CAST(@value AS BYTES)", values: []rtValue{
			{name: "empty", in: []byte{}, lit: "b''"},
			{name: "zero byte", in: []byte{0}, lit: "FROM_BASE64('AA==')"},
			{name: "every byte", in: everyByte(), lit: "FROM_BASE64('" + base64Of(everyByte()) + "')"},
			{name: "long", in: []byte(big), lit: "FROM_BASE64('" + base64Of([]byte(big)) + "')"},
			{name: "null", in: nil, lit: "CAST(NULL AS BYTES)"},
		}},
		"DATE": {column: "DATE", expr: "CAST(@value AS DATE)", values: []rtValue{
			{name: "first", in: date(1, time.January, 1), lit: "DATE '0001-01-01'"},
			{name: "epoch", in: date(1970, time.January, 1), lit: "DATE '1970-01-01'"},
			{name: "leap day", in: date(2024, time.February, 29), lit: "DATE '2024-02-29'"},
			{name: "last", in: date(9999, time.December, 31), lit: "DATE '9999-12-31'"},
			{name: "null", in: nil, lit: "CAST(NULL AS DATE)"},
		}},
		"TIME": {column: "TIME", expr: "CAST(@value AS TIME)", values: []rtValue{
			{name: "midnight", in: clock(0, 0, 0, 0), lit: "TIME '00:00:00'"},
			{name: "step", in: clock(0, 0, 0, 1), lit: "TIME '00:00:00.000001'"},
			{name: "noon", in: clock(12, 34, 56, 789000), lit: "TIME '12:34:56.789'"},
			{name: "last", in: clock(23, 59, 59, 999999), lit: "TIME '23:59:59.999999'"},
			{name: "null", in: nil, lit: "CAST(NULL AS TIME)"},
		}},
		"DATETIME": {column: "DATETIME", expr: "CAST(@value AS DATETIME)", values: []rtValue{
			{name: "first", in: dtime(date(1, time.January, 1), clock(0, 0, 0, 0)), lit: "DATETIME '0001-01-01 00:00:00'"},
			{name: "step", in: dtime(date(1970, time.January, 1), clock(0, 0, 0, 1)), lit: "DATETIME '1970-01-01 00:00:00.000001'"},
			{name: "last", in: dtime(date(9999, time.December, 31), clock(23, 59, 59, 999999)), lit: "DATETIME '9999-12-31 23:59:59.999999'"},
			{name: "null", in: nil, lit: "CAST(NULL AS DATETIME)"},
		}},
		"TIMESTAMP": {column: "TIMESTAMP", expr: "CAST(@value AS TIMESTAMP)", values: []rtValue{
			{name: "first", in: instant(1, time.January, 1, 0, 0, 0, 0), lit: "TIMESTAMP '0001-01-01 00:00:00+00'"},
			{name: "before 1970", in: instant(1969, time.December, 31, 23, 59, 59, 999999), lit: "TIMESTAMP '1969-12-31 23:59:59.999999+00'"},
			{name: "step", in: instant(1970, time.January, 1, 0, 0, 0, 1), lit: "TIMESTAMP '1970-01-01 00:00:00.000001+00'"},
			{name: "from another zone", in: time.Date(2026, time.October, 9, 12, 34, 56, 123456000, time.FixedZone("", 7*3600)), want: instant(2026, time.October, 9, 5, 34, 56, 123456), lit: "TIMESTAMP '2026-10-09 12:34:56.123456+07'"},
			{name: "last", in: instant(9999, time.December, 31, 23, 59, 59, 999999), lit: "TIMESTAMP '9999-12-31 23:59:59.999999+00'"},
			{name: "null", in: nil, lit: "CAST(NULL AS TIMESTAMP)"},
		}},
		// A JSON column reads as the decoded value, so the test writes JSON text and parses
		// it in the statement. A JSON null and a SQL NULL both read as nil (D189 item 7).
		"JSON": {column: "JSON", expr: "PARSE_JSON(@value)", values: []rtValue{
			{name: "object", in: `{"a":[1,2,{"b":null}],"s":"x"}`, want: map[string]any{"a": []any{int64(1), int64(2), map[string]any{"b": nil}}, "s": "x"}, lit: `PARSE_JSON('{"a":[1,2,{"b":null}],"s":"x"}')`},
			{name: "array", in: `[1.5,true,"é"]`, want: []any{1.5, true, "é"}, lit: `PARSE_JSON('[1.5,true,"é"]')`},
			{name: "string", in: `"text"`, want: "text", lit: `PARSE_JSON('"text"')`},
			{name: "big integer", in: `12345678901234567890`, want: dec(t, "12345678901234567890"), lit: `PARSE_JSON('12345678901234567890')`},
			{name: "empty object", in: `{}`, want: map[string]any{}, lit: `PARSE_JSON('{}')`},
			{name: "null", in: nil, lit: "CAST(NULL AS JSON)"},
		}},
		// The text of a point is what the service writes back, with no space after its name.
		"GEOGRAPHY": {column: "GEOGRAPHY", expr: "ST_GEOGFROMTEXT(@value)", values: []rtValue{
			{name: "point", in: "POINT(1 2)", lit: "ST_GEOGFROMTEXT('POINT(1 2)')"},
			{name: "line", in: "LINESTRING(0 0, 1 1)", lit: "ST_GEOGFROMTEXT('LINESTRING(0 0, 1 1)')"},
			{name: "null", in: nil, lit: "CAST(NULL AS GEOGRAPHY)"},
		}},
		"INTERVAL": {column: "INTERVAL", expr: "CAST(@value AS INTERVAL)", values: []rtValue{
			{name: "zero", in: dbimp.Interval{}, lit: "CAST('0-0 0 0:0:0' AS INTERVAL)"},
			{name: "a day", in: dbimp.Interval{Days: 1}, lit: "CAST('0-0 1 0:0:0' AS INTERVAL)"},
			{name: "every part", in: dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60+6)*int64(time.Second) + 789000000}, lit: "CAST('1-2 3 4:5:6.789' AS INTERVAL)"},
			{name: "negative", in: dbimp.Interval{Months: -5, Days: -1, Nanoseconds: -((1*60+2)*60 + 3) * int64(time.Second)}, lit: "CAST('-0-5 -1 -1:2:3' AS INTERVAL)"},
			{name: "step", in: dbimp.Interval{Nanoseconds: 1000}, lit: "CAST('0-0 0 0:0:0.000001' AS INTERVAL)"},
			{name: "null", in: nil, lit: "CAST(NULL AS INTERVAL)"},
		}},
		// An ARRAY is never NULL: the service stores a NULL array as an empty one
		// (recorded: bigquery-070).
		"ARRAY": {column: "ARRAY<INT64>", expr: "ARRAY(SELECT INT64(x) FROM UNNEST(JSON_QUERY_ARRAY(PARSE_JSON(@value))) AS x)", values: []rtValue{
			{name: "empty", in: `[]`, want: []any{}, lit: "ARRAY<INT64>[]"},
			{name: "three", in: `[1,2,3]`, want: []any{int64(1), int64(2), int64(3)}, lit: "[1, 2, 3]"},
			{name: "limits", in: `[-9223372036854775808,9223372036854775807]`, want: []any{int64(math.MinInt64), int64(math.MaxInt64)}, lit: "[-9223372036854775808, 9223372036854775807]"},
			{name: "null", in: nil, want: []any{}, lit: "CAST(NULL AS ARRAY<INT64>)"},
		}},
		"RECORD": {column: "STRUCT<a INT64, b STRING>", expr: "IF(@value IS NULL, NULL, STRUCT(INT64(PARSE_JSON(@value).a) AS a, STRING(PARSE_JSON(@value).b) AS b))", values: []rtValue{
			{name: "plain", in: `{"a":1,"b":"x"}`, want: map[string]any{"a": int64(1), "b": "x"}, lit: "STRUCT(1 AS a, 'x' AS b)"},
			{name: "negative", in: `{"a":-5,"b":"é"}`, want: map[string]any{"a": int64(-5), "b": "é"}, lit: "STRUCT(-5 AS a, 'é' AS b)"},
			{name: "zero", in: `{"a":0,"b":""}`, want: map[string]any{"a": int64(0), "b": ""}, lit: "STRUCT(0 AS a, '' AS b)"},
			{name: "null", in: nil, lit: "CAST(NULL AS STRUCT<a INT64, b STRING>)"},
		}},
		"RANGE": {column: "RANGE<DATE>", expr: "CAST(@value AS RANGE<DATE>)", values: []rtValue{
			{name: "closed", in: "[2024-01-01, 2024-02-01)", lit: "RANGE<DATE> '[2024-01-01, 2024-02-01)'"},
			{name: "open end", in: "[2024-01-01, UNBOUNDED)", lit: "RANGE<DATE> '[2024-01-01, UNBOUNDED)'"},
			{name: "open start", in: "[UNBOUNDED, 2024-02-01)", lit: "RANGE<DATE> '[UNBOUNDED, 2024-02-01)'"},
			{name: "null", in: nil, lit: "CAST(NULL AS RANGE<DATE>)"},
		}},
	}
	for _, name := range []string{"INTEGER", "FLOAT", "NUMERIC", "BIGNUMERIC", "BOOLEAN", "STRING", "BYTES", "DATE", "TIME", "DATETIME", "TIMESTAMP", "JSON", "GEOGRAPHY", "INTERVAL", "RECORD", "RANGE", "ARRAY"} {
		t.Run(name, func(t *testing.T) {
			roundTrip(t, name, cases[name])
		})
	}
}

// emulatorTypes are the types whose round trip the emulator 0.8.1 passes, as far as
// the recordings show. It writes a TIMESTAMP, a TIME, a GEOGRAPHY and a NaN in other
// forms, refuses a NULL element and a RANGE type, wraps an INT64, and stores the base64
// text of a BYTES parameter as the bytes (docs/BIGQUERY.md).
var emulatorTypes = []string{"INTEGER", "BOOLEAN", "STRING", "DATE"}

// everyByte returns the 256 values of a byte, in order.
func everyByte() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// base64Of returns the standard base64 text of b.
func base64Of(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
