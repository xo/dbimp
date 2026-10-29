package maxcompute_test

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// slots limits how many round trips run at once, because each statement is a job
// of the service, and the service resets a connection now and then.
var slots = make(chan struct{}, 4)

// quote writes a string literal. MaxCompute reads a backslash in a string as an
// escape, so the literal escapes the backslash and the quote with a backslash.
func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// rtCase is the round trip of one type.
type rtCase struct {
	// column is the type of the column.
	column string
	// bind is the expression that takes the argument, ? by default, such as
	// ARRAY(CAST(? AS STRING)) for a list, which a Go value cannot bind.
	bind string
	// equal compares a value that a read returned with the value wanted, and
	// defaults to same.
	equal  func(got, want any) bool
	values []dbimptest.Value
}

// same compares two values of a read: a time as an instant, a decimal by its
// number, and any other value as DeepEqual does.
func same(got, want any) bool {
	switch w := want.(type) {
	case time.Time:
		g, ok := got.(time.Time)
		return ok && g.Equal(w)
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	}
	return reflect.DeepEqual(got, want)
}

// null is the value NULL.
var null = dbimptest.Value{Name: "null"}

// roundTripCase returns the case that RoundTrip runs for a type, in a table of
// its own. MaxCompute changes a row of a transactional table only, so the update
// and the delete of RoundTrip are skipped here, and the tests of CRUD hold them.
func roundTripCase(name string, c rtCase) dbimptest.RoundTripCase {
	tbl := "dbimp_it_rt_" + strings.ToLower(strings.NewReplacer("<", "_", ">", "_", ",", "_", ":", "_", "(", "_", ")", "_").Replace(name))
	bind := c.bind
	if bind == "" {
		bind = "?"
	}
	eq := c.equal
	if eq == nil {
		eq = same
	}
	return dbimptest.RoundTripCase{
		Type:     name,
		Setup:    []string{"DROP TABLE IF EXISTS " + tbl, "CREATE TABLE " + tbl + " (k STRING, v " + c.column + ")"},
		Teardown: []string{"DROP TABLE IF EXISTS " + tbl},
		Insert:   "INSERT INTO " + tbl + " (k, v) VALUES (?, " + bind + ")",
		Literal: func(key string, v any) (string, error) {
			var text string
			switch v := v.(type) {
			case int64, bool:
				text = fmt.Sprint(v)
			case float64:
				text = strconv.FormatFloat(v, 'g', -1, 64)
			case string:
				text = quote(v)
			default:
				return "", fmt.Errorf("writing %v as a literal: %w", v, dbimp.ErrNotSupported)
			}
			return "INSERT INTO " + tbl + " (k, v) VALUES (" + quote(key) + ", " + strings.Replace(bind, "?", text, 1) + ")", nil
		},
		Select: "SELECT v FROM " + tbl + " WHERE k = ?",
		SkipUpdate: func(dbimptest.Value, dbimptest.Value) string {
			return "MaxCompute updates a row of a transactional table only"
		},
		Values: c.values,
		Equal:  eq,
	}
}

// intValues returns the smallest and the largest value of an integer type, and a
// NULL.
func intValues(lo, hi int64) []dbimptest.Value {
	return []dbimptest.Value{{Name: "min", In: lo}, {Name: "max", In: hi}, null}
}

// decimal parses a decimal for a test.
func decimal(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// local makes a date and a time with no zone.
func local(year int, month time.Month, day, hour, minute, second, nanos int) dbimp.LocalDateTime {
	return dbimp.LocalDateTime{
		Date: dbimp.Date{Year: year, Month: month, Day: day},
		Time: dbimp.LocalTime{Hour: hour, Minute: minute, Second: second, Nanosecond: nanos},
	}
}

// TestIntegrationRoundTrip stores a few values of each type that the survey marks
// yes, as an argument and as a literal, and reads each one back with its Go type
// (step 14a and D135).
func TestIntegrationRoundTrip(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	jakarta := time.FixedZone("", 7*3600)
	for _, tt := range []struct {
		name string
		c    rtCase
	}{
		{"TINYINT", rtCase{column: "TINYINT", bind: "CAST(? AS TINYINT)", values: intValues(-128, 127)}},
		{"SMALLINT", rtCase{column: "SMALLINT", bind: "CAST(? AS SMALLINT)", values: intValues(-32768, 32767)}},
		{"INT", rtCase{column: "INT", bind: "CAST(? AS INT)", values: intValues(-2147483648, 2147483647)}},
		{"BIGINT", rtCase{column: "BIGINT", values: intValues(-9223372036854775807, 9223372036854775807)}},
		{"FLOAT", rtCase{column: "FLOAT", bind: "CAST(? AS FLOAT)", values: []dbimptest.Value{{Name: "half", In: 1.5}, {Name: "negative", In: -0.5}, null}}},
		{"DOUBLE", rtCase{column: "DOUBLE", values: []dbimptest.Value{{Name: "tenth", In: 0.1}, {Name: "large", In: 1.7976931348623157e308}, {Name: "small", In: -1.5e-7}, null}}},
		{"DECIMAL", rtCase{column: "DECIMAL(38,18)", values: []dbimptest.Value{
			{Name: "fraction", In: decimal("123.456"), Want: decimal("123.456")},
			{Name: "negative", In: decimal("-99999999999999999999.999999999999999999"), Want: decimal("-99999999999999999999.999999999999999999")},
			null,
		}}},
		{"BOOLEAN", rtCase{column: "BOOLEAN", values: []dbimptest.Value{{Name: "true", In: true}, {Name: "false", In: false}, null}}},
		{"STRING", rtCase{column: "STRING", values: []dbimptest.Value{
			{Name: "ascii", In: "hello"},
			{Name: "unicode", In: "a,\"b\"\nc é 日本 😀"},
			{Name: "empty", In: ""},
			{Name: "backslash and N", In: `\N`},
			null,
		}}},
		{"VARCHAR", rtCase{column: "VARCHAR(10)", bind: "CAST(? AS VARCHAR(10))", values: []dbimptest.Value{{Name: "short", In: "abc"}, {Name: "full", In: "abcdefghij"}, null}}},
		{"CHAR", rtCase{column: "CHAR(5)", bind: "CAST(? AS CHAR(5))", values: []dbimptest.Value{{Name: "padded", In: "ab", Want: "ab   "}, {Name: "full", In: "abcde"}, null}}},
		{"BINARY", rtCase{column: "BINARY", values: []dbimptest.Value{{Name: "bytes", In: []byte{0, 255, 16}}, {Name: "text", In: []byte("abc")}, null}}},
		{"DATE", rtCase{column: "DATE", values: []dbimptest.Value{
			// A date of the year 1 is read back two days later (recorded in
			// docs/MAXCOMPUTE.md, Faults), so the smallest date here is of 1900.
			{Name: "old", In: dbimp.Date{Year: 1900, Month: 1, Day: 1}},
			{Name: "max", In: dbimp.Date{Year: 9999, Month: 12, Day: 31}},
			null,
		}}},
		{"DATETIME", rtCase{column: "DATETIME", bind: "CAST(? AS DATETIME)", values: []dbimptest.Value{
			{Name: "instant", In: time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC), Want: time.Date(2026, 10, 29, 1, 2, 3, 0, time.UTC)},
			{Name: "other zone", In: time.Date(2026, 1, 1, 0, 0, 0, 0, jakarta), Want: time.Date(2025, 12, 31, 17, 0, 0, 0, time.UTC)},
			null,
		}}},
		{"TIMESTAMP", rtCase{column: "TIMESTAMP", bind: "CAST(? AS TIMESTAMP)", values: []dbimptest.Value{
			{Name: "instant", In: time.Date(2026, 10, 29, 1, 2, 3, 123456000, time.UTC), Want: time.Date(2026, 10, 29, 1, 2, 3, 123456000, time.UTC)},
			{Name: "epoch", In: time.Date(1970, 1, 1, 0, 0, 0, 1000, time.UTC), Want: time.Date(1970, 1, 1, 0, 0, 0, 1000, time.UTC)},
			null,
		}}},
		{"TIMESTAMP_NTZ", rtCase{column: "TIMESTAMP_NTZ", bind: "CAST(? AS TIMESTAMP_NTZ)", values: []dbimptest.Value{
			{Name: "wall clock", In: local(2026, 10, 29, 1, 2, 3, 123456000)},
			{Name: "min", In: local(1, 1, 1, 0, 0, 0, 0)},
			null,
		}}},
		{"INTERVAL_DAY_TIME", rtCase{column: "INTERVAL_DAY_TIME", values: []dbimptest.Value{
			{Name: "day and time", In: dbimp.Interval{Days: 1, Nanoseconds: ((2*60+3)*60 + 4) * int64(time.Second)}},
			{Name: "fraction", In: dbimp.Interval{Nanoseconds: 500 * int64(time.Millisecond)}},
			null,
		}}},
		{"INTERVAL_YEAR_MONTH", rtCase{column: "INTERVAL_YEAR_MONTH", values: []dbimptest.Value{
			{Name: "year and month", In: dbimp.Interval{Months: 14}},
			{Name: "months", In: dbimp.Interval{Months: 3}},
			null,
		}}},
		{"JSON", rtCase{column: "JSON", bind: "JSON_PARSE(?)", values: []dbimptest.Value{
			{Name: "object", In: `{"a":[1,2,{"b":null}]}`, Want: map[string]any{"a": []any{int64(1), int64(2), map[string]any{"b": nil}}}},
			{Name: "number", In: `1.5`, Want: 1.5},
			null,
		}}},
		{"ARRAY", rtCase{column: "ARRAY<STRING>", bind: "ARRAY(CAST(? AS STRING), 'b')", values: []dbimptest.Value{
			{Name: "one", In: "a", Want: "[a, b]"},
			{Name: "another", In: "c", Want: "[c, b]"},
		}}},
		{"MAP", rtCase{column: "MAP<STRING,BIGINT>", bind: "MAP(CAST(? AS STRING), 1L)", values: []dbimptest.Value{
			{Name: "one", In: "a", Want: "{a:1}"},
			{Name: "another", In: "c", Want: "{c:1}"},
		}}},
		{"STRUCT", rtCase{column: "STRUCT<x:STRING>", bind: "NAMED_STRUCT('x', CAST(? AS STRING))", values: []dbimptest.Value{
			{Name: "one", In: "a", Want: "{x:a}"},
			{Name: "another", In: "c", Want: "{x:c}"},
		}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			dbimptest.RoundTrip(t, db, roundTripCase(tt.name, tt.c))
		})
	}
}
