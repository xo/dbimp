package databricks_test

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// quote writes a string literal. Spark reads a backslash in a string as an escape,
// and reads two literals that stand side by side as one, so a doubled quote is no
// quote. The literal escapes the backslash and the quote with a backslash.
func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// rtValue is one value of a round trip: the value that the test writes as an
// argument, the value that a read returns, and the expression that writes it as a
// literal of its type.
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
	// bind is the expression that takes the argument, ? by default, such as
	// from_json(CAST(? AS STRING), 'ARRAY<INT>') for a list, which a Go value cannot
	// bind (the server refuses an ARRAY parameter).
	bind string
	// wrap writes the expression of a value into the form that the column holds, and is
	// %s by default. A type that the test stores as text uses CAST(%s AS STRING).
	wrap string
	// read is the expression that reads the column, and v by default. It casts the
	// text of a stored type back into its type, so that the column of the result has
	// the type that the test covers.
	read string
	// equal compares a value that a read returned with the value wanted, and defaults
	// to same.
	equal  func(got, want any) bool
	values []rtValue
}

// null is the value NULL.
var null = rtValue{name: "null", lit: "NULL"}

// newCase returns the round trip of the case, in a table of its own.
func newCase(name string, c rtCase) dbimptest.RoundTripCase {
	tbl := table("rt_" + name)
	bind, wrap, read := c.bind, c.wrap, c.read
	if bind == "" {
		bind = "?"
	}
	if wrap == "" {
		wrap = "%s"
	}
	if read == "" {
		read = "v"
	}
	values := make([]dbimptest.Value, len(c.values))
	for i, v := range c.values {
		values[i] = dbimptest.Value{Name: v.name, In: v.in, Want: v.want}
	}
	eq := c.equal
	if eq == nil {
		eq = same
	}
	return dbimptest.RoundTripCase{
		Type:     name,
		Setup:    []string{"CREATE TABLE " + tbl + " (k STRING, v " + c.column + ") USING DELTA"},
		Teardown: []string{"DROP TABLE IF EXISTS " + tbl},
		Insert:   "INSERT INTO " + tbl + " (k, v) VALUES (?, " + fmt.Sprintf(wrap, bind) + ")",
		Literal: func(key string, v any) (string, error) {
			for _, cv := range c.values {
				if fmt.Sprint(cv.in) == fmt.Sprint(v) && cv.lit != "" {
					return "INSERT INTO " + tbl + " (k, v) VALUES (" + quote(key) + ", " + fmt.Sprintf(wrap, cv.lit) + ")", nil
				}
			}
			return "", fmt.Errorf("writing %v as a literal: %w", v, dbimp.ErrNotSupported)
		},
		Select: "SELECT " + read + " FROM " + tbl + " WHERE k = ?",
		Update: "UPDATE " + tbl + " SET v = " + fmt.Sprintf(wrap, bind) + " WHERE k = ?",
		Delete: "DELETE FROM " + tbl + " WHERE k = ?",
		Values: values,
		Equal:  eq,
	}
}

// intCase returns the round trip of an integer type, with its smallest and its largest
// value. The literal casts text, because the lexer reads -9223372036854775808 as
// the minus of a number that is too large.
func intCase(column string, lo, hi int64) rtCase {
	lit := func(n int64) string { return "CAST('" + strconv.FormatInt(n, 10) + "' AS " + column + ")" }
	return rtCase{column: column, values: []rtValue{
		{name: "min", in: lo, lit: lit(lo)},
		{name: "zero", in: int64(0), lit: lit(0)},
		{name: "one", in: int64(1), lit: lit(1)},
		{name: "max", in: hi, lit: lit(hi)},
		null,
	}}
}

// local makes a date and a time with no zone.
func local(year int, month time.Month, day, hour, minute, second, nanos int) dbimp.LocalDateTime {
	return dbimp.LocalDateTime{
		Date: dbimp.Date{Year: year, Month: month, Day: day},
		Time: dbimp.LocalTime{Hour: hour, Minute: minute, Second: second, Nanosecond: nanos},
	}
}

// instant makes a time in the zone with the offset in seconds.
func instant(year int, month time.Month, day, hour, minute, second, nanos, offset int) time.Time {
	return time.Date(year, month, day, hour, minute, second, nanos, time.FixedZone("", offset))
}

// utcText writes an instant in UTC as the literal of a TIMESTAMP.
func utcText(t time.Time) string {
	return "TIMESTAMP'" + t.UTC().Format("2006-01-02 15:04:05.999999") + "'"
}

// longList writes a JSON array of n numbers, and returns it with its value.
func longList(n int) (string, []any) {
	var b strings.Builder
	b.WriteByte('[')
	want := make([]any, n)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(i))
		want[i] = int64(i)
	}
	b.WriteByte(']')
	return b.String(), want
}

// everyByte returns the 256 values of a byte, in order.
func everyByte() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

// TestIntegrationRoundTrip stores every type that features.json marks yes in a column
// of that type, as dbimptest.RoundTrip does (step 14a): as a bound argument and as a
// literal, and it updates, reads and deletes each value. A type that the server
// refuses as a parameter, a binary value and a list, goes in as text that the
// statement turns into the type. A type that a table of Delta cannot hold, such as an
// interval or a geometry, and the type VOID, which only a result can have, is stored as
// text, or as a NULL in a column of text, and the select casts it to its type, so that
// the column of the result has the type that the test covers. The three digits of
// fraction of a timestamp are the digits that the server gives (D193 item 3).
func TestIntegrationRoundTrip(t *testing.T) {
	db := connect(t)
	roundTrip := func(t *testing.T, name string, c rtCase) {
		t.Helper()
		dbimptest.RoundTrip(t, db, newCase(name, c))
	}
	long := strings.Repeat("x", 20000)
	list, listWant := longList(1000)
	utc := func(year int, month time.Month, day, hour, minute, second, nanos int) time.Time {
		return time.Date(year, month, day, hour, minute, second, nanos, time.UTC)
	}
	cases := map[string]rtCase{
		"TINYINT":  intCase("TINYINT", math.MinInt8, math.MaxInt8),
		"SMALLINT": intCase("SMALLINT", math.MinInt16, math.MaxInt16),
		"INT":      intCase("INT", math.MinInt32, math.MaxInt32),
		"BIGINT":   intCase("BIGINT", math.MinInt64, math.MaxInt64),
		// A FLOAT keeps 32 bits, and the server writes the shortest text of them, so the
		// float64 that a read returns is the one nearest to that text.
		"FLOAT": {column: "FLOAT", values: []rtValue{
			{name: "min", in: -math.MaxFloat32, want: -3.4028235e38, lit: "CAST('-3.4028235E38' AS FLOAT)"},
			{name: "zero", in: 0.0, lit: "CAST('0' AS FLOAT)"},
			{name: "smallest", in: math.SmallestNonzeroFloat32, want: 1.4e-45, lit: "CAST('1.4E-45' AS FLOAT)"},
			{name: "max", in: math.MaxFloat32, want: 3.4028235e38, lit: "CAST('3.4028235E38' AS FLOAT)"},
			{name: "fraction", in: 0.1, lit: "CAST('0.1' AS FLOAT)"},
			{name: "NaN", in: math.NaN(), lit: "CAST('NaN' AS FLOAT)"},
			{name: "Infinity", in: math.Inf(1), lit: "CAST('Infinity' AS FLOAT)"},
			{name: "-Infinity", in: math.Inf(-1), lit: "CAST('-Infinity' AS FLOAT)"},
			null,
		}},
		"DOUBLE": {column: "DOUBLE", values: []rtValue{
			{name: "min", in: -math.MaxFloat64, lit: "CAST('-1.7976931348623157E308' AS DOUBLE)"},
			{name: "zero", in: 0.0, lit: "CAST('0' AS DOUBLE)"},
			{name: "smallest", in: math.SmallestNonzeroFloat64, lit: "CAST('4.9E-324' AS DOUBLE)"},
			{name: "max", in: math.MaxFloat64, lit: "CAST('1.7976931348623157E308' AS DOUBLE)"},
			{name: "fraction", in: 0.1, lit: "CAST('0.1' AS DOUBLE)"},
			{name: "NaN", in: math.NaN(), lit: "CAST('NaN' AS DOUBLE)"},
			{name: "Infinity", in: math.Inf(1), lit: "CAST('Infinity' AS DOUBLE)"},
			{name: "-Infinity", in: math.Inf(-1), lit: "CAST('-Infinity' AS DOUBLE)"},
			null,
		}},
		"DECIMAL": {column: "DECIMAL(38,18)", values: []rtValue{
			{name: "min", in: dec(t, "-99999999999999999999.999999999999999999"), lit: "CAST('-99999999999999999999.999999999999999999' AS DECIMAL(38,18))"},
			{name: "zero", in: dec(t, "0"), lit: "CAST('0' AS DECIMAL(38,18))"},
			{name: "last digit", in: dec(t, "0.000000000000000001"), lit: "CAST('0.000000000000000001' AS DECIMAL(38,18))"},
			{name: "scale", in: dec(t, "1.50"), lit: "CAST('1.50' AS DECIMAL(38,18))"},
			{name: "max", in: dec(t, "99999999999999999999.999999999999999999"), lit: "CAST('99999999999999999999.999999999999999999' AS DECIMAL(38,18))"},
			null,
		}},
		"BOOLEAN": {column: "BOOLEAN", values: []rtValue{
			{name: "true", in: true, lit: "TRUE"},
			{name: "false", in: false, lit: "FALSE"},
			null,
		}},
		"STRING": {column: "STRING", values: []rtValue{
			{name: "empty", in: "", lit: "''"},
			{name: "unicode", in: "héllo wörld ✓ 日本語 🎉", lit: quote("héllo wörld ✓ 日本語 🎉")},
			{name: "quotes", in: "it's \"q\" \\ back\nline\ttab", lit: quote("it's \"q\" \\ back\nline\ttab")},
			{name: "long", in: long, lit: quote(long)},
			null,
		}},
		// The server refuses a BINARY parameter (measured), so the argument is the
		// hexadecimal text, and the statement writes unhex.
		"BINARY": {column: "BINARY", bind: "unhex(CAST(? AS STRING))", values: []rtValue{
			{name: "zero byte", in: "00", want: []byte{0}, lit: "X'00'"},
			{name: "every byte", in: strings.ToUpper(hex.EncodeToString(everyByte())), want: everyByte(), lit: "X'" + strings.ToUpper(hex.EncodeToString(everyByte())) + "'"},
			{name: "text", in: strings.ToUpper(hex.EncodeToString([]byte("héllo"))), want: []byte("héllo"), lit: "CAST(" + quote("héllo") + " AS BINARY)"},
			{name: "long", in: strings.Repeat("AB", 20000), want: []byte(strings.Repeat("\xab", 20000)), lit: "X'" + strings.Repeat("AB", 20000) + "'"},
			null,
		}},
		"DATE": {column: "DATE", values: []rtValue{
			{name: "first", in: dbimp.Date{Year: 1, Month: time.January, Day: 1}, lit: "DATE'0001-01-01'"},
			{name: "before 1970", in: dbimp.Date{Year: 1969, Month: time.December, Day: 31}, lit: "DATE'1969-12-31'"},
			{name: "epoch", in: dbimp.Date{Year: 1970, Month: time.January, Day: 1}, lit: "DATE'1970-01-01'"},
			{name: "leap day", in: dbimp.Date{Year: 2024, Month: time.February, Day: 29}, lit: "DATE'2024-02-29'"},
			{name: "last", in: dbimp.Date{Year: 9999, Month: time.December, Day: 31}, lit: "DATE'9999-12-31'"},
			null,
		}},
		"TIMESTAMP": {column: "TIMESTAMP", values: []rtValue{
			{name: "first", in: utc(1, time.January, 1, 0, 0, 0, 0), lit: utcText(utc(1, time.January, 1, 0, 0, 0, 0))},
			{name: "before 1970", in: utc(1969, time.December, 31, 23, 59, 59, 999000000), lit: utcText(utc(1969, time.December, 31, 23, 59, 59, 999000000))},
			{name: "step", in: utc(1970, time.January, 1, 0, 0, 0, 1000000), lit: utcText(utc(1970, time.January, 1, 0, 0, 0, 1000000))},
			{name: "from another zone", in: instant(2026, time.October, 9, 12, 34, 56, 123000000, 7*3600), want: utc(2026, time.October, 9, 5, 34, 56, 123000000), lit: utcText(utc(2026, time.October, 9, 5, 34, 56, 123000000))},
			{name: "microseconds are cut", in: utc(2024, time.January, 2, 3, 4, 5, 123456000), want: utc(2024, time.January, 2, 3, 4, 5, 123000000), lit: "TIMESTAMP'2024-01-02 03:04:05.123456'"},
			{name: "last", in: utc(9999, time.December, 31, 23, 59, 59, 999000000), lit: utcText(utc(9999, time.December, 31, 23, 59, 59, 999000000))},
			null,
		}},
		"TIMESTAMP_NTZ": {column: "TIMESTAMP_NTZ", values: []rtValue{
			{name: "first", in: local(1, time.January, 1, 0, 0, 0, 0), lit: "TIMESTAMP_NTZ'0001-01-01 00:00:00'"},
			{name: "before 1970", in: local(1969, time.December, 31, 23, 59, 59, 999000000), lit: "TIMESTAMP_NTZ'1969-12-31 23:59:59.999'"},
			{name: "step", in: local(1970, time.January, 1, 0, 0, 0, 1000000), lit: "TIMESTAMP_NTZ'1970-01-01 00:00:00.001'"},
			{name: "microseconds are cut", in: local(2024, time.January, 2, 3, 4, 5, 123456000), want: local(2024, time.January, 2, 3, 4, 5, 123000000), lit: "TIMESTAMP_NTZ'2024-01-02 03:04:05.123456'"},
			{name: "last", in: local(9999, time.December, 31, 23, 59, 59, 999000000), lit: "TIMESTAMP_NTZ'9999-12-31 23:59:59.999'"},
			null,
		}},
		// The interval is stored as its text, and the select casts it back.
		"INTERVAL YEAR TO MONTH": {column: "STRING", wrap: "CAST(%s AS STRING)", read: "CAST(v AS INTERVAL YEAR TO MONTH)", values: []rtValue{
			{name: "min", in: dbimp.Interval{Months: math.MinInt32}, lit: "INTERVAL '-178956970-8' YEAR TO MONTH"},
			{name: "negative", in: dbimp.Interval{Months: -14}, lit: "INTERVAL '-1-2' YEAR TO MONTH"},
			// A Go zero Interval is sent as a day to second interval, which a year to
			// month column refuses, so this case has no zero.
			{name: "one month", in: dbimp.Interval{Months: 1}, lit: "INTERVAL '0-1' YEAR TO MONTH"},
			{name: "max", in: dbimp.Interval{Months: math.MaxInt32}, lit: "INTERVAL '178956970-7' YEAR TO MONTH"},
			null,
		}},
		"INTERVAL DAY TO SECOND": {column: "STRING", wrap: "CAST(%s AS STRING)", read: "CAST(v AS INTERVAL DAY TO SECOND)", values: []rtValue{
			// The live run read the extremes, 106751991 days and 4 hours, wrong after the cast of
			// their text back to an interval (the server gave 106751 days), while the parser
			// reads the text right (TestParseInterval). So the test keeps to 100000 days.
			{name: "large negative", in: dbimp.Interval{Days: -100000, Nanoseconds: -int64(4*time.Hour + 54*time.Second + 775808*time.Microsecond)}, lit: "INTERVAL '-100000 04:00:54.775808' DAY TO SECOND"},
			{name: "negative", in: dbimp.Interval{Days: -1, Nanoseconds: -int64(2*time.Hour + 3*time.Minute + 4*time.Second + 500*time.Millisecond)}, lit: "INTERVAL '-1 02:03:04.5' DAY TO SECOND"},
			{name: "zero", in: dbimp.Interval{}, lit: "INTERVAL '0 00:00:00' DAY TO SECOND"},
			{name: "one microsecond", in: dbimp.Interval{Nanoseconds: 1000}, lit: "INTERVAL '0 00:00:00.000001' DAY TO SECOND"},
			{name: "days and time", in: dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute + 4*time.Second + 123456*time.Microsecond)}, lit: "INTERVAL '1 02:03:04.123456' DAY TO SECOND"},
			{name: "large", in: dbimp.Interval{Days: 100000, Nanoseconds: int64(4*time.Hour + 54*time.Second + 775807*time.Microsecond)}, lit: "INTERVAL '100000 04:00:54.775807' DAY TO SECOND"},
			null,
		}},
		// The server refuses an ARRAY, a MAP and a STRUCT parameter, so the argument is JSON
		// text that from_json turns into the type.
		"ARRAY": {column: "ARRAY<INT>", bind: "from_json(CAST(? AS STRING), 'ARRAY<INT>')", values: []rtValue{
			{name: "empty", in: `[]`, want: []any{}, lit: "CAST(ARRAY() AS ARRAY<INT>)"},
			{name: "numbers", in: `[1,2,3]`, want: []any{int64(1), int64(2), int64(3)}, lit: "ARRAY(1, 2, 3)"},
			{name: "a NULL element", in: `[null,1]`, want: []any{nil, int64(1)}, lit: "ARRAY(CAST(NULL AS INT), 1)"},
			{name: "long", in: list, want: listWant, lit: "ARRAY(" + strings.Trim(list, "[]") + ")"},
			null,
		}},
		"MAP": {column: "MAP<STRING, INT>", bind: "from_json(CAST(? AS STRING), 'MAP<STRING,INT>')", values: []rtValue{
			{name: "empty", in: `{}`, want: map[string]any{}, lit: "CAST(MAP() AS MAP<STRING, INT>)"},
			{name: "keys", in: `{"a":1,"b":2}`, want: map[string]any{"a": int64(1), "b": int64(2)}, lit: "MAP('a', 1, 'b', 2)"},
			{name: "a NULL value", in: `{"é":null}`, want: map[string]any{"é": nil}, lit: "MAP('é', CAST(NULL AS INT))"},
			null,
		}},
		"STRUCT": {column: "STRUCT<a: INT, b: STRING>", bind: "from_json(CAST(? AS STRING), 'STRUCT<a: INT, b: STRING>')", values: []rtValue{
			{name: "fields", in: `{"a":1,"b":"x"}`, want: map[string]any{"a": int64(1), "b": "x"}, lit: "NAMED_STRUCT('a', 1, 'b', 'x')"},
			{name: "a NULL field", in: `{"a":null,"b":"é"}`, want: map[string]any{"a": nil, "b": "é"}, lit: "NAMED_STRUCT('a', CAST(NULL AS INT), 'b', 'é')"},
			{name: "no field", in: `{}`, want: map[string]any{"a": nil, "b": nil}, lit: "NAMED_STRUCT('a', CAST(NULL AS INT), 'b', CAST(NULL AS STRING))"},
			null,
		}},
		"VARIANT": {column: "VARIANT", bind: "parse_json(CAST(? AS STRING))", equal: sameVariant, values: []rtValue{
			{name: "object", in: `{"a":[1,2,{"b":null}],"s":"x"}`, want: map[string]any{"a": []any{int64(1), int64(2), map[string]any{"b": nil}}, "s": "x"}, lit: "PARSE_JSON('{\"a\":[1,2,{\"b\":null}],\"s\":\"x\"}')"},
			{name: "array", in: `[1.5,true,"é"]`, want: []any{1.5, true, "é"}, lit: "PARSE_JSON('[1.5,true,\"é\"]')"},
			{name: "string", in: `"text"`, want: "text", lit: "PARSE_JSON('\"text\"')"},
			{name: "big integer", in: `9007199254740993`, want: int64(9007199254740993), lit: "PARSE_JSON('9007199254740993')"},
			{name: "empty object", in: `{}`, want: map[string]any{}, lit: "PARSE_JSON('{}')"},
			{name: "boolean", in: `false`, want: false, lit: "PARSE_JSON('false')"},
			// A JSON null and a SQL NULL are both nil (D193 item 7).
			{name: "JSON null", in: `null`, lit: "PARSE_JSON('null')"},
			null,
		}},
		// A geometry is stored as its text, and the select makes it again.
		"GEOMETRY": {column: "STRING", bind: "ST_GEOMFROMTEXT(CAST(? AS STRING))", wrap: "ST_ASTEXT(%s)", read: "ST_GEOMFROMTEXT(v)", values: []rtValue{
			{name: "point", in: "POINT(1 2)", lit: "ST_GEOMFROMTEXT('POINT(1 2)')"},
			{name: "origin", in: "POINT(0 0)", lit: "ST_GEOMFROMTEXT('POINT(0 0)')"},
			{name: "fractions", in: "POINT(-122.4194 37.7749)", lit: "ST_GEOMFROMTEXT('POINT(-122.4194 37.7749)')"},
			null,
		}},
		"GEOGRAPHY": {column: "STRING", bind: "ST_GEOGFROMTEXT(CAST(? AS STRING))", wrap: "ST_ASTEXT(%s)", read: "ST_GEOGFROMTEXT(v)", values: []rtValue{
			{name: "point", in: "POINT(1 2)", want: "SRID=4326;POINT(1 2)", lit: "ST_GEOGFROMTEXT('POINT(1 2)')"},
			{name: "origin", in: "POINT(0 0)", want: "SRID=4326;POINT(0 0)", lit: "ST_GEOGFROMTEXT('POINT(0 0)')"},
			{name: "fractions", in: "POINT(-122.4194 37.7749)", want: "SRID=4326;POINT(-122.4194 37.7749)", lit: "ST_GEOGFROMTEXT('POINT(-122.4194 37.7749)')"},
			null,
		}},
		// A table cannot hold the type VOID, which is the type of a NULL with no other
		// type. The table holds a NULL in a column of text, and the select writes a NULL
		// literal, whose column has the type VOID.
		"VOID": {column: "STRING", read: "NULL AS v", values: []rtValue{
			{name: "null", lit: "NULL"},
			{name: "null again", lit: "NULL"},
		}},
	}
	for _, name := range []string{
		"TINYINT", "SMALLINT", "INT", "BIGINT", "FLOAT", "DOUBLE", "DECIMAL", "BOOLEAN", "STRING", "BINARY", "DATE", "TIMESTAMP", "TIMESTAMP_NTZ",
		"INTERVAL YEAR TO MONTH", "INTERVAL DAY TO SECOND", "ARRAY", "MAP", "STRUCT", "VARIANT", "GEOMETRY", "GEOGRAPHY", "VOID",
	} {
		t.Run(name, func(t *testing.T) {
			roundTrip(t, strings.ToLower(strings.ReplaceAll(name, " ", "_")), cases[name])
		})
	}
}

// sameVariant is same, except that the text null stands for a JSON null, which reads
// back as nil.
func sameVariant(got, want any) bool {
	if s, ok := want.(string); ok && s == "null" {
		return got == nil
	}
	return same(got, want)
}
