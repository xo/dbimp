package snowflake_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// quote writes a string literal. Snowflake reads a backslash in a string as an
// escape, so the literal doubles it, and doubles each quote.
func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s) + "'"
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
	// expr is the expression that takes the argument, ? by default, such as
	// PARSE_JSON(?) for a variant, which a Go value cannot bind.
	expr string
	// equal compares a value that a read returned with the value wanted, and
	// defaults to same.
	equal  func(got, want any) bool
	values []rtValue
}

// null is the value NULL.
var null = rtValue{name: "null", lit: "NULL"}

// newCase returns the round trip of the case, in a table of its own.
func newCase(name string, c rtCase) dbimptest.RoundTripCase {
	tbl := table("rt_" + name)
	expr := c.expr
	if expr == "" {
		expr = "?"
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
		Type:     name,
		Setup:    []string{"CREATE OR REPLACE TABLE " + tbl + " (k VARCHAR, v " + c.column + ")"},
		Teardown: []string{"DROP TABLE IF EXISTS " + tbl},
		Insert:   "INSERT INTO " + tbl + " (k, v) SELECT ?, " + expr,
		Literal: func(key string, v any) (string, error) {
			for _, cv := range c.values {
				if fmt.Sprint(cv.in) == fmt.Sprint(v) && cv.lit != "" {
					return "INSERT INTO " + tbl + " (k, v) SELECT " + quote(key) + ", " + cv.lit, nil
				}
			}
			return "", fmt.Errorf("writing %v as a literal: %w", v, dbimp.ErrNotSupported)
		},
		Select: "SELECT v FROM " + tbl + " WHERE k = ?",
		Update: "UPDATE " + tbl + " SET v = " + expr + " WHERE k = ?",
		Delete: "DELETE FROM " + tbl + " WHERE k = ?",
		Values: values,
		Equal:  eq,
	}
}

// instant makes a time in the zone with the offset in seconds.
func instant(year int, month time.Month, day, hour, minute, second, nanos, offset int) time.Time {
	return time.Date(year, month, day, hour, minute, second, nanos, time.FixedZone("", offset))
}

// ts writes the time as a literal of a timestamp with a zone.
func ts(t time.Time, typ string) string {
	return "'" + t.Format("2006-01-02 15:04:05.999999999 -07:00") + "'::" + typ
}

// TestIntegrationRoundTrip stores every type that features.json marks yes in a
// column of that type, as dbimptest.RoundTrip does (step 14a): as a bound argument
// and as a literal, and it updates, reads and deletes each value. A value that has
// no Go type to bind, a variant and the types after it, goes in as JSON text that
// the statement parses.
func TestIntegrationRoundTrip(t *testing.T) {
	db := connect(t)
	roundTrip := func(t *testing.T, name string, c rtCase) {
		t.Helper()
		dbimptest.RoundTrip(t, db, newCase(name, c))
	}
	big := strings.Repeat("x", 100000)
	maxDec := "9999999999999999999999999999.9999999999"
	cases := map[string]rtCase{
		"REAL": {column: "FLOAT", values: []rtValue{
			{name: "min", in: -math.MaxFloat64, lit: "-1.7976931348623157e308::FLOAT"},
			{name: "zero", in: 0.0, lit: "0::FLOAT"},
			{name: "smallest normal", in: 2.2250738585072014e-308, lit: "2.2250738585072014e-308::FLOAT"},
			{name: "max", in: math.MaxFloat64, lit: "1.7976931348623157e308::FLOAT"},
			{name: "fraction", in: 0.1, lit: "0.1::FLOAT"},
			{name: "NaN", in: math.NaN(), lit: "'NaN'::FLOAT"},
			{name: "inf", in: math.Inf(1), lit: "'inf'::FLOAT"},
			{name: "-inf", in: math.Inf(-1), lit: "'-inf'::FLOAT"},
			null,
		}},
		"TEXT": {column: "VARCHAR", values: []rtValue{
			{name: "empty", in: "", lit: "''"},
			{name: "unicode", in: "héllo wörld ✓ 日本語 🎉", lit: quote("héllo wörld ✓ 日本語 🎉")},
			{name: "quotes", in: "it's \"q\" \\ back\nline", lit: quote("it's \"q\" \\ back\nline")},
			{name: "long", in: big, lit: quote(big)},
			null,
		}},
		"BOOLEAN": {column: "BOOLEAN", values: []rtValue{
			{name: "true", in: true, lit: "TRUE"},
			{name: "false", in: false, lit: "FALSE"},
			null,
		}},
		"DATE": {column: "DATE", values: []rtValue{
			{name: "first", in: dbimp.Date{Year: 1, Month: time.January, Day: 1}, lit: "'0001-01-01'::DATE"},
			{name: "before 1970", in: dbimp.Date{Year: 1969, Month: time.December, Day: 31}, lit: "'1969-12-31'::DATE"},
			{name: "epoch", in: dbimp.Date{Year: 1970, Month: time.January, Day: 1}, lit: "'1970-01-01'::DATE"},
			{name: "leap day", in: dbimp.Date{Year: 2024, Month: time.February, Day: 29}, lit: "'2024-02-29'::DATE"},
			{name: "last", in: dbimp.Date{Year: 9999, Month: time.December, Day: 31}, lit: "'9999-12-31'::DATE"},
			null,
		}},
		"TIME": {column: "TIME(9)", values: []rtValue{
			{name: "midnight", in: dbimp.LocalTime{}, lit: "'00:00:00'::TIME(9)"},
			{name: "step", in: dbimp.LocalTime{Nanosecond: 1}, lit: "'00:00:00.000000001'::TIME(9)"},
			{name: "noon", in: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}, lit: "'12:34:56.123456789'::TIME(9)"},
			{name: "last", in: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}, lit: "'23:59:59.999999999'::TIME(9)"},
			null,
		}},
		"TIMESTAMP_NTZ": {column: "TIMESTAMP_NTZ(9)", values: []rtValue{
			{name: "first", in: ntz(1, time.January, 1, 0, 0, 0, 0), lit: "'0001-01-01 00:00:00'::TIMESTAMP_NTZ(9)"},
			{name: "before 1970", in: ntz(1969, time.December, 31, 23, 59, 59, 999999999), lit: "'1969-12-31 23:59:59.999999999'::TIMESTAMP_NTZ(9)"},
			{name: "step", in: ntz(1970, time.January, 1, 0, 0, 0, 1), lit: "'1970-01-01 00:00:00.000000001'::TIMESTAMP_NTZ(9)"},
			{name: "last", in: ntz(9999, time.December, 31, 23, 59, 59, 999999999), lit: "'9999-12-31 23:59:59.999999999'::TIMESTAMP_NTZ(9)"},
			null,
		}},
		// A TIMESTAMP_TZ binding does not convert to a TIMESTAMP_LTZ column by itself in
		// a list of values, so the statement casts it (measured, 2026-10-10).
		"TIMESTAMP_LTZ": {column: "TIMESTAMP_LTZ(9)", expr: "CAST(? AS TIMESTAMP_LTZ(9))", values: []rtValue{
			{name: "early", in: instant(1900, time.January, 1, 0, 0, 0, 0, 0), lit: ts(instant(1900, time.January, 1, 0, 0, 0, 0, 0), "TIMESTAMP_LTZ(9)")},
			{name: "before 1970", in: instant(1969, time.December, 31, 23, 59, 59, 999999999, 0), lit: ts(instant(1969, time.December, 31, 23, 59, 59, 999999999, 0), "TIMESTAMP_LTZ(9)")},
			{name: "step", in: instant(1970, time.January, 1, 0, 0, 0, 1, 0), lit: ts(instant(1970, time.January, 1, 0, 0, 0, 1, 0), "TIMESTAMP_LTZ(9)")},
			{name: "from another zone", in: instant(2026, time.October, 9, 12, 34, 56, 123456789, 7*3600), want: instant(2026, time.October, 9, 5, 34, 56, 123456789, 0), lit: ts(instant(2026, time.October, 9, 12, 34, 56, 123456789, 7*3600), "TIMESTAMP_LTZ(9)")},
			{name: "last", in: instant(9999, time.December, 31, 23, 59, 59, 999999999, 0), lit: ts(instant(9999, time.December, 31, 23, 59, 59, 999999999, 0), "TIMESTAMP_LTZ(9)")},
			null,
		}},
		"TIMESTAMP_TZ": {column: "TIMESTAMP_TZ(9)", values: []rtValue{
			{name: "east", in: instant(2026, time.October, 9, 12, 34, 56, 123456789, 7*3600), lit: ts(instant(2026, time.October, 9, 12, 34, 56, 123456789, 7*3600), "TIMESTAMP_TZ(9)")},
			{name: "west", in: instant(2026, time.October, 9, 12, 34, 56, 123456789, -(3*3600 + 1800)), lit: ts(instant(2026, time.October, 9, 12, 34, 56, 123456789, -(3*3600+1800)), "TIMESTAMP_TZ(9)")},
			{name: "UTC", in: instant(1970, time.January, 1, 0, 0, 0, 1, 0), lit: ts(instant(1970, time.January, 1, 0, 0, 0, 1, 0), "TIMESTAMP_TZ(9)")},
			{name: "early", in: instant(1900, time.January, 1, 0, 0, 0, 0, 7*3600), lit: ts(instant(1900, time.January, 1, 0, 0, 0, 0, 7*3600), "TIMESTAMP_TZ(9)")},
			null,
		}},
		"BINARY": {column: "BINARY", values: []rtValue{
			{name: "empty", in: []byte{}, lit: "TO_BINARY('', 'HEX')"},
			{name: "zero byte", in: []byte{0}, lit: "TO_BINARY('00', 'HEX')"},
			{name: "every byte", in: everyByte(), lit: "TO_BINARY('" + fmt.Sprintf("%X", everyByte()) + "', 'HEX')"},
			{name: "long", in: []byte(big), lit: "TO_BINARY('" + fmt.Sprintf("%X", []byte(big)) + "', 'HEX')"},
			null,
		}},
		"VARIANT": {column: "VARIANT", expr: "PARSE_JSON(?)", values: []rtValue{
			{name: "object", in: `{"a":[1,2,{"b":null}],"s":"x"}`, want: map[string]any{"a": []any{int64(1), int64(2), map[string]any{"b": nil}}, "s": "x"}, lit: "PARSE_JSON('{\"a\":[1,2,{\"b\":null}],\"s\":\"x\"}')"},
			{name: "array", in: `[1.5,true,"é"]`, want: []any{1.5, true, "é"}, lit: "PARSE_JSON('[1.5,true,\"é\"]')"},
			{name: "string", in: `"text"`, want: "text", lit: "PARSE_JSON('\"text\"')"},
			{name: "big integer", in: `9007199254740993`, want: int64(9007199254740993), lit: "PARSE_JSON('9007199254740993')"},
			{name: "empty object", in: `{}`, want: map[string]any{}, lit: "PARSE_JSON('{}')"},
			{name: "boolean", in: `false`, want: false, lit: "PARSE_JSON('false')"},
			null,
		}},
		"OBJECT": {column: "OBJECT", expr: "PARSE_JSON(?)::OBJECT", values: []rtValue{
			{name: "empty", in: `{}`, want: map[string]any{}, lit: "PARSE_JSON('{}')::OBJECT"},
			{name: "flat", in: `{"k":"v"}`, want: map[string]any{"k": "v"}, lit: "PARSE_JSON('{\"k\":\"v\"}')::OBJECT"},
			{name: "nested", in: `{"a":{"b":[1,"two",null]},"é":1.5}`, want: map[string]any{"a": map[string]any{"b": []any{int64(1), "two", nil}}, "é": 1.5}, lit: "PARSE_JSON('{\"a\":{\"b\":[1,\"two\",null]},\"é\":1.5}')::OBJECT"},
			null,
		}},
		"ARRAY": {column: "ARRAY", expr: "PARSE_JSON(?)::ARRAY", values: []rtValue{
			{name: "empty", in: `[]`, want: []any{}, lit: "PARSE_JSON('[]')::ARRAY"},
			{name: "mixed", in: `[1,"x",null,[2]]`, want: []any{int64(1), "x", nil, []any{int64(2)}}, lit: "PARSE_JSON('[1,\"x\",null,[2]]')::ARRAY"},
			null,
		}},
		"GEOGRAPHY": {column: "GEOGRAPHY", expr: "TO_GEOGRAPHY(?)", values: []rtValue{
			{name: "point", in: `{"type":"Point","coordinates":[1.5,2.5]}`, want: map[string]any{"type": "Point", "coordinates": []any{1.5, 2.5}}, lit: `TO_GEOGRAPHY('{"type":"Point","coordinates":[1.5,2.5]}')`},
			{name: "line", in: `{"type":"LineString","coordinates":[[-122.35,37.55],[-122.25,37.65]]}`, want: map[string]any{"type": "LineString", "coordinates": []any{[]any{-122.35, 37.55}, []any{-122.25, 37.65}}}, lit: `TO_GEOGRAPHY('{"type":"LineString","coordinates":[[-122.35,37.55],[-122.25,37.65]]}')`},
			null,
		}},
		"GEOMETRY": {column: "GEOMETRY", expr: "TO_GEOMETRY(?)", values: []rtValue{
			{name: "point", in: `{"type":"Point","coordinates":[1.5,2.5]}`, want: map[string]any{"type": "Point", "coordinates": []any{1.5, 2.5}}, lit: `TO_GEOMETRY('{"type":"Point","coordinates":[1.5,2.5]}')`},
			{name: "whole numbers", in: `{"type":"Point","coordinates":[3,4]}`, want: map[string]any{"type": "Point", "coordinates": []any{3.0, 4.0}}, lit: `TO_GEOMETRY('{"type":"Point","coordinates":[3,4]}')`},
			null,
		}},
		"VECTOR": {column: "VECTOR(FLOAT, 3)", expr: "PARSE_JSON(?)::ARRAY::VECTOR(FLOAT, 3)", values: []rtValue{
			{name: "ones", in: dbimp.Vector[float64]{1, 2, 3}, lit: "[1,2,3]::VECTOR(FLOAT, 3)"},
			{name: "fractions", in: dbimp.Vector[float64]{0.5, -1.5, 1024.25}, lit: "[0.5,-1.5,1024.25]::VECTOR(FLOAT, 3)"},
			{name: "zeros", in: dbimp.Vector[float64]{0, 0, 0}, lit: "[0,0,0]::VECTOR(FLOAT, 3)"},
			null,
		}},
	}
	// The Go type of a fixed column depends on its precision and its scale
	// (D183): an int64 for a scale of 0 and 18 digits or less, which is the
	// entry FIXED INTEGER, and a decimal for any other, which is the entry
	// FIXED DECIMAL.
	t.Run("FIXED INTEGER", func(t *testing.T) {
		roundTrip(t, "fixed_int", rtCase{column: "NUMBER(18,0)", values: []rtValue{
			{name: "min", in: int64(-999999999999999999), lit: "-999999999999999999::NUMBER(18,0)"},
			{name: "zero", in: int64(0), lit: "0::NUMBER(18,0)"},
			{name: "max", in: int64(999999999999999999), lit: "999999999999999999::NUMBER(18,0)"},
			null,
		}})
	})
	t.Run("FIXED DECIMAL", func(t *testing.T) {
		t.Run("decimal", func(t *testing.T) {
			roundTrip(t, "fixed_dec", rtCase{column: "NUMBER(38,10)", values: []rtValue{
				{name: "min", in: dec(t, "-"+maxDec), lit: "-" + maxDec + "::NUMBER(38,10)"},
				{name: "zero", in: dec(t, "0"), lit: "0::NUMBER(38,10)"},
				{name: "last digit", in: dec(t, "0.0000000001"), lit: "0.0000000001::NUMBER(38,10)"},
				{name: "max", in: dec(t, maxDec), lit: maxDec + "::NUMBER(38,10)"},
				null,
			}})
		})
		t.Run("wide integer", func(t *testing.T) {
			roundTrip(t, "fixed_wide", rtCase{column: "NUMBER(38,0)", values: []rtValue{
				{name: "beyond int64", in: dec(t, "9223372036854775808"), lit: "9223372036854775808::NUMBER(38,0)"},
				{name: "38 digits", in: dec(t, "12345678901234567890123456789012345678"), lit: "12345678901234567890123456789012345678::NUMBER(38,0)"},
				{name: "negative", in: dec(t, "-1"), lit: "-1::NUMBER(38,0)"},
				null,
			}})
		})
	})
	for _, name := range []string{"REAL", "TEXT", "BOOLEAN", "DATE", "TIME", "TIMESTAMP_NTZ", "TIMESTAMP_LTZ", "TIMESTAMP_TZ", "BINARY", "VARIANT", "OBJECT", "ARRAY", "GEOGRAPHY", "GEOMETRY", "VECTOR"} {
		t.Run(name, func(t *testing.T) {
			roundTrip(t, strings.ToLower(name), cases[name])
		})
	}
}

// ntz makes a local date and time.
func ntz(year int, month time.Month, day, hour, minute, second, nanos int) dbimp.LocalDateTime {
	return dbimp.LocalDateTime{
		Date: dbimp.Date{Year: year, Month: month, Day: day},
		Time: dbimp.LocalTime{Hour: hour, Minute: minute, Second: second, Nanosecond: nanos},
	}
}

// everyByte returns the 256 values of a byte, in order.
func everyByte() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}
