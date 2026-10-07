package clickhouse_test

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/clickhouse"
	"github.com/xo/dbimp/dbimptest"
)

// optTB is a testing.TB whose context carries the options of the driver, which
// dbimptest.RoundTrip passes to each statement that it runs. It is how a round
// trip waits for a mutation, and turns on a setting that a type needs.
type optTB struct {
	*testing.T

	ctx context.Context //nolint:containedctx // The wrapper hands the context of the test, with the options, to RoundTrip.
}

// Context returns the context of the test with the options.
func (o optTB) Context() context.Context { return o.ctx }

// rtValue is one value of a round trip: the value that the test writes as an
// argument, the value that a read must return, and the literal that writes it in
// SQL, which is an expression that the column takes.
type rtValue struct {
	name string
	in   any
	// want is the value that a read returns. It is in when it is nil and in is not.
	want any
	lit  string
}

// rtCase is the round trip of one type.
type rtCase struct {
	// column is the type of the column.
	column string
	// insert is the statement that inserts a row from its key and its value, as a
	// function of the table. The default is INSERT INTO t (k, v) SELECT ?, ?.
	insert func(tbl string) string
	// set is the expression of the update that takes the new value, which is ? by
	// default.
	set string
	// selectExpr is the expression that the select returns, v by default.
	selectExpr string
	// skipUpdate returns why the update from one value to the next cannot run, or
	// "" when it can. It is nil when every update runs.
	skipUpdate func(from, to dbimptest.Value) string
	// options are the options of each statement, besides the wait for a mutation.
	options []clickhouse.Option
	// engine is the engine of the table, MergeTree by default.
	engine string
	values []rtValue
}

// quote writes a string literal. A string of bytes that are not UTF-8, and a
// string with a control character, is written with unhex, which holds every byte.
func quote(s string) string {
	if !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Sprintf("unhex('%x')", s)
	}
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
}

// d makes a decimal.
func d(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	v, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("reading %q as a decimal: %v", s, err)
	}
	return v
}

// bi makes a big integer.
func bi(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("reading %q as an integer", s)
	}
	return v
}

// null is the value NULL.
var null = rtValue{name: "null", lit: "NULL"}

// ints returns the case of an integer type: its smallest value, zero, its
// largest value, and NULL.
func ints(column string, lo, hi any) rtCase {
	zero := lo
	if _, ok := lo.(int64); ok {
		zero = int64(0)
	}
	if _, ok := lo.(uint64); ok {
		zero = uint64(0)
	}
	return rtCase{column: "Nullable(" + column + ")", values: []rtValue{
		{name: "min", in: lo, lit: fmt.Sprint(lo)},
		{name: "zero", in: zero, lit: "0"},
		{name: "max", in: hi, lit: fmt.Sprint(hi)},
		null,
	}}
}

// bigs returns the case of an integer of 128 or 256 bits.
func bigs(t *testing.T, column, fn, lo, hi string) rtCase {
	t.Helper()
	return rtCase{column: "Nullable(" + column + ")", values: []rtValue{
		{name: "min", in: bi(t, lo), lit: fn + "('" + lo + "')"},
		{name: "zero", in: bi(t, "0"), lit: fn + "('0')"},
		{name: "max", in: bi(t, hi), lit: fn + "('" + hi + "')"},
		{name: "one", in: bi(t, "1"), lit: fn + "('1')"},
		null,
	}}
}

// floatText writes a float as the server reads it.
func floatText(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case f == 0 && math.Signbit(f):
		return "-0.0"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// floats returns the case of a float type, with the function that makes a float
// of the text of each value.
func floats(column, fn string, values ...rtValue) rtCase {
	for i := range values {
		if f, ok := values[i].in.(float64); ok {
			values[i].lit = fn + "('" + floatText(f) + "')"
			if column == "Float32" {
				// A bound float is a Float64 that the server rounds to a Float32, and
				// toFloat32 of the text of the largest Float32 is one step below it on
				// 25.3 and 25.8 (measured), so the literal casts a number.
				values[i].lit = "CAST(" + floatText(f) + " AS Float32)"
				if math.IsNaN(f) || math.IsInf(f, 0) {
					values[i].lit = "CAST('" + floatText(f) + "' AS Float32)"
				}
			}
		}
	}
	return rtCase{column: "Nullable(" + column + ")", values: append(values, null)}
}

// instant returns a time of the case of a DateTime or a DateTime64.
func instant(name string, tm time.Time, fn string) rtValue {
	return rtValue{name: name, in: tm, lit: fn}
}

// TestIntegrationRoundTrip is the round trip of each type that features.json marks
// yes (step 14a): it stores values in a column of the type, as a bound argument and
// as a literal, reads each one back with its Go type, updates it to the next value,
// deletes it, and sees that it is gone. It runs as the administrator and as the
// ordinary user.
func TestIntegrationRoundTrip(t *testing.T) { //nolint:maintidx // One table, a case for each type of features.json.
	st := theState(t)
	cases := roundTripCases(t, st.rel)
	for _, name := range roundTripNames {
		t.Run(name, func(t *testing.T) {
			c, ok := cases[name]
			if !ok {
				t.Fatalf("no case for %s", name)
			}
			if why := c.unavailable(st.rel); why != "" {
				t.Skip(why)
			}
			eachPrincipal(t, func(t *testing.T, e env) {
				tbl := e.name("rt_" + strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, " ", "_"), ".", "_")))
				opts := append([]clickhouse.Option{clickhouse.WithParameter("mutations_sync", 2)}, c.cs.options...)
				ctx := clickhouse.WithOptions(t.Context(), opts...)
				engine := c.cs.engine
				if engine == "" {
					engine = "MergeTree ORDER BY k"
				}
				insert := "INSERT INTO " + tbl + " (k, v) SELECT ?, ?"
				if c.cs.insert != nil {
					insert = c.cs.insert(tbl)
				}
				set := "?"
				if c.cs.set != "" {
					set = c.cs.set
				}
				sel := "v"
				if c.cs.selectExpr != "" {
					sel = c.cs.selectExpr
				}
				// The table is dropped when the test ends, and the first thing that
				// RoundTrip does is to make it.
				e.dropAtEnd(t, tbl)
				values := make([]dbimptest.Value, len(c.cs.values))
				for i, v := range c.cs.values {
					values[i] = dbimptest.Value{Name: v.name, In: v.in, Want: v.want}
				}
				dbimptest.RoundTrip(optTB{T: t, ctx: ctx}, e.db, dbimptest.RoundTripCase{
					Type:     name,
					Setup:    []string{"CREATE TABLE " + tbl + " (k String, v " + c.cs.column + ") ENGINE = " + engine},
					Teardown: []string{"DROP TABLE IF EXISTS " + tbl + " SYNC"},
					Insert:   insert,
					Literal: func(key string, v any) (string, error) {
						for _, cv := range c.cs.values {
							if equalValue(v, cv.in) && fmt.Sprintf("%T", v) == fmt.Sprintf("%T", cv.in) {
								if cv.lit == "" {
									break
								}
								return "INSERT INTO " + tbl + " (k, v) SELECT " + quote(key) + ", " + cv.lit, nil
							}
						}
						return "", fmt.Errorf("the case has no literal for %#v: %w", v, dbimp.ErrNotSupported)
					},
					Select:     "SELECT " + sel + " FROM " + tbl + " WHERE k = ?",
					Update:     "ALTER TABLE " + tbl + " UPDATE v = " + set + " WHERE k = ?",
					Delete:     "DELETE FROM " + tbl + " WHERE k = ?",
					SkipUpdate: c.cs.skipUpdate,
					Values:     values,
					Wait:       3 * time.Second,
					Equal:      equalValue,
				})
			})
		})
	}
}

// roundTripNames are the types of features.json that the survey marks yes, as it
// names them, in its order.
var roundTripNames = []string{
	"Int8", "Int16", "Int32", "Int64", "Int128", "Int256",
	"UInt8", "UInt16", "UInt32", "UInt64", "UInt128", "UInt256",
	"Float32", "Float64", "Decimal", "Bool", "String", "FixedString",
	"Date", "Date32", "DateTime", "DateTime64", "Enum8", "Enum16",
	"UUID", "IPv4", "IPv6", "Array", "Tuple", "Map", "BFloat16",
	"Time", "Time64", "Interval", "Nothing", "Variant", "Dynamic", "JSON",
	"Point", "Ring", "Polygon", "MultiPolygon", "LineString", "MultiLineString",
	"26.9 MultiPoint", "26.9 Geometry", "AggregateFunction", "26.9 QBit",
}

// roundTripCase is a case, and the release that it needs.
type roundTripCase struct {
	cs rtCase
	// since is the first release that has the type, or {0, 0} for every release.
	since [2]int
	// why is the reason that the case does not run before since.
	why string
}

// unavailable returns why the case does not run on the release, or "".
func (c roundTripCase) unavailable(r release) string {
	if c.since != [2]int{} && !r.atLeast(c.since[0], c.since[1]) {
		return c.why
	}
	return ""
}

// roundTripCases returns the case of each type, by the name of the survey.
func roundTripCases(t *testing.T, rel release) map[string]roundTripCase {
	t.Helper()
	always := func(c rtCase) roundTripCase { return roundTripCase{cs: c} }
	// since marks a case that needs the release major.8 or later, which is every
	// first release that a type has.
	since := func(major int, why string, c rtCase) roundTripCase {
		return roundTripCase{cs: c, since: [2]int{major, 8}, why: why}
	}
	long := strings.Repeat("0123456789", 10000)
	zero := time.Unix(0, 0).UTC()
	// float32max is the largest Float32, which the server reads from a Float64 on
	// every release. Its text is 3.4028235e38, which Go reads as the same Float32.
	float32max := math.MaxFloat32
	float32want := 3.4028235e38
	float64min := 0.0
	if rel.atLeast(26, 8) {
		float64min = math.SmallestNonzeroFloat64
	}
	return map[string]roundTripCase{
		"Int8":    always(ints("Int8", int64(-128), int64(127))),
		"Int16":   always(ints("Int16", int64(-32768), int64(32767))),
		"Int32":   always(ints("Int32", int64(math.MinInt32), int64(math.MaxInt32))),
		"Int64":   always(ints("Int64", int64(math.MinInt64), int64(math.MaxInt64))),
		"UInt8":   always(ints("UInt8", int64(0), int64(255))),
		"UInt16":  always(ints("UInt16", int64(0), int64(65535))),
		"UInt32":  always(ints("UInt32", int64(0), int64(math.MaxUint32))),
		"UInt64":  always(ints("UInt64", uint64(0), uint64(math.MaxUint64))),
		"Int128":  always(bigs(t, "Int128", "toInt128", "-170141183460469231731687303715884105728", "170141183460469231731687303715884105727")),
		"UInt128": always(bigs(t, "UInt128", "toUInt128", "0", "340282366920938463463374607431768211455")),
		"Int256":  always(bigs(t, "Int256", "toInt256", "-57896044618658097711785492504343953926634992332820282019728792003956564819968", "57896044618658097711785492504343953926634992332820282019728792003956564819967")),
		"UInt256": always(bigs(t, "UInt256", "toUInt256", "0", "115792089237316195423570985008687907853269984665640564039457584007913129639935")),
		"Float32": always(floats("Float32", "toFloat32",
			rtValue{name: "zero", in: 0.0},
			rtValue{name: "negative zero", in: math.Copysign(0, -1)},
			rtValue{name: "a decimal", in: 0.1},
			rtValue{name: "negative", in: -1.5},
			rtValue{name: "max", in: float32max, want: float32want},
			rtValue{name: "NaN", in: math.NaN()},
			rtValue{name: "infinity", in: math.Inf(-1)},
		)),
		"Float64": always(floats("Float64", "toFloat64",
			rtValue{name: "zero", in: 0.0},
			rtValue{name: "negative zero", in: math.Copysign(0, -1)},
			rtValue{name: "a decimal", in: 0.1},
			rtValue{name: "max", in: math.MaxFloat64},
			rtValue{name: "min", in: math.SmallestNonzeroFloat64, want: float64min},
			rtValue{name: "NaN", in: math.NaN()},
			rtValue{name: "infinity", in: math.Inf(1)},
		)),
		"BFloat16": always(floats("BFloat16", "toBFloat16",
			rtValue{name: "zero", in: 0.0},
			rtValue{name: "one and a half", in: 1.5},
			rtValue{name: "negative", in: -1.5},
			rtValue{name: "pi", in: 3.14159, want: 3.140625},
		)),
		"Decimal": always(rtCase{column: "Nullable(Decimal(76, 40))", values: []rtValue{
			{name: "zero", in: d(t, "0"), lit: "toDecimal256('0', 40)"},
			{name: "one and a half", in: d(t, "1.5"), lit: "toDecimal256('1.5', 40)"},
			{name: "the last digit", in: d(t, "123456789012345678901234567890123456.1234567890123456789012345678901234567890"), lit: "toDecimal256('123456789012345678901234567890123456.1234567890123456789012345678901234567890', 40)"},
			{name: "negative", in: d(t, "-0.0000000000000000000000000000000000000001"), lit: "toDecimal256('-0.0000000000000000000000000000000000000001', 40)"},
			null,
		}}),
		"Bool": always(rtCase{column: "Nullable(Bool)", values: []rtValue{
			{name: "true", in: true, lit: "true"},
			{name: "false", in: false, lit: "false"},
			null,
		}}),
		"String": always(rtCase{column: "Nullable(String)", values: []rtValue{
			{name: "empty", in: "", lit: "''"},
			{name: "ascii", in: "hello", lit: "'hello'"},
			{name: "unicode", in: "héllo € 日本 😀", lit: quote("héllo € 日本 😀")},
			{name: "escapes", in: "a\tb\nc\\d'e\"f\x00g\r \\N", lit: quote("a\tb\nc\\d'e\"f\x00g\r \\N")},
			{name: "bytes", in: "\xff\xfe\x80 abc", lit: quote("\xff\xfe\x80 abc")},
			{name: "long", in: long, lit: "'" + long + "'"},
			null,
		}}),
		"FixedString": always(rtCase{column: "Nullable(FixedString(8))", values: []rtValue{
			{name: "empty", in: "", want: strings.Repeat("\x00", 8), lit: "toFixedString('', 8)"},
			{name: "short", in: "abc", want: "abc" + strings.Repeat("\x00", 5), lit: "toFixedString('abc', 8)"},
			{name: "full", in: "12345678", lit: "toFixedString('12345678', 8)"},
			{name: "unicode", in: "é€", want: "é€" + strings.Repeat("\x00", 3), lit: "toFixedString('é€', 8)"},
			null,
		}}),
		"Date": always(rtCase{column: "Nullable(Date)", values: []rtValue{
			{name: "min", in: dbimp.Date{Year: 1970, Month: 1, Day: 1}, lit: "toDate('1970-01-01')"},
			{name: "max", in: dbimp.Date{Year: 2149, Month: 6, Day: 6}, lit: "toDate('2149-06-06')"},
			{name: "a day", in: dbimp.Date{Year: 2026, Month: 10, Day: 7}, lit: "toDate('2026-10-07')"},
			null,
		}}),
		"Date32": always(rtCase{column: "Nullable(Date32)", values: []rtValue{
			{name: "min", in: dbimp.Date{Year: 1900, Month: 1, Day: 1}, lit: "toDate32('1900-01-01')"},
			{name: "max", in: dbimp.Date{Year: 2299, Month: 12, Day: 31}, lit: "toDate32('2299-12-31')"},
			{name: "before the epoch", in: dbimp.Date{Year: 1969, Month: 12, Day: 31}, lit: "toDate32('1969-12-31')"},
			{name: "a day", in: dbimp.Date{Year: 2026, Month: 10, Day: 7}, lit: "toDate32('2026-10-07')"},
			null,
		}}),
		"DateTime": always(rtCase{column: "Nullable(DateTime('America/New_York'))", skipUpdate: mutationInstant(rel), values: []rtValue{
			instant("epoch", zero, "toDateTime('1970-01-01 00:00:00', 'UTC')"),
			instant("max", time.Date(2106, 2, 7, 6, 28, 15, 0, time.UTC), "toDateTime('2106-02-07 06:28:15', 'UTC')"),
			instant("a time", time.Date(2026, 10, 7, 12, 34, 56, 0, time.UTC), "toDateTime('2026-10-07 12:34:56', 'UTC')"),
			// 01:30 on 2026-11-01 in New York is two instants, an hour apart.
			instant("first 01:30", time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), "toDateTime('2026-11-01 05:30:00', 'UTC')"),
			instant("second 01:30", time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), "toDateTime('2026-11-01 06:30:00', 'UTC')"),
			null,
		}}),
		"DateTime64": always(rtCase{column: "Nullable(DateTime64(9, 'Asia/Jakarta'))", skipUpdate: mutationInstant(rel), values: []rtValue{
			instant("min", time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), "toDateTime64('1900-01-01 00:00:00', 9, 'UTC')"),
			instant("max", time.Date(2262, 4, 11, 23, 47, 16, 854775807, time.UTC), "toDateTime64('2262-04-11 23:47:16.854775807', 9, 'UTC')"),
			instant("a nanosecond", zero.Add(time.Nanosecond), "toDateTime64('1970-01-01 00:00:00.000000001', 9, 'UTC')"),
			instant("before the epoch", zero.Add(-time.Nanosecond), "toDateTime64('1969-12-31 23:59:59.999999999', 9, 'UTC')"),
			instant("a time", time.Date(2026, 10, 7, 12, 34, 56, 123456789, time.UTC), "toDateTime64('2026-10-07 12:34:56.123456789', 9, 'UTC')"),
			null,
		}}),
		"Enum8": always(rtCase{column: "Nullable(Enum8('a' = 1, 'b' = 2, 'it''s' = -128))", values: []rtValue{
			{name: "a", in: "a", lit: "'a'"},
			{name: "b", in: "b", lit: "'b'"},
			{name: "a quote", in: "it's", lit: `'it\'s'`},
			null,
		}}),
		"Enum16": always(rtCase{column: "Nullable(Enum16('x' = 1000, 'y' = 32767, 'z' = -32768))", values: []rtValue{
			{name: "x", in: "x", lit: "'x'"},
			{name: "y", in: "y", lit: "'y'"},
			{name: "z", in: "z", lit: "'z'"},
			null,
		}}),
		"UUID": always(rtCase{column: "Nullable(UUID)", values: []rtValue{
			{name: "zero", in: uuid.UUID{}, lit: "toUUID('00000000-0000-0000-0000-000000000000')"},
			{name: "max", in: uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff"), lit: "toUUID('ffffffff-ffff-ffff-ffff-ffffffffffff')"},
			{name: "a uuid", in: uuid.MustParse("61f0c404-5cb3-11e7-907b-a6006ad3dba0"), lit: "toUUID('61f0c404-5cb3-11e7-907b-a6006ad3dba0')"},
			null,
		}}),
		"IPv4": always(rtCase{column: "Nullable(IPv4)", values: []rtValue{
			{name: "zero", in: netip.MustParseAddr("0.0.0.0"), lit: "toIPv4('0.0.0.0')"},
			{name: "max", in: netip.MustParseAddr("255.255.255.255"), lit: "toIPv4('255.255.255.255')"},
			{name: "private", in: netip.MustParseAddr("192.168.0.1"), lit: "toIPv4('192.168.0.1')"},
			null,
		}}),
		"IPv6": always(rtCase{column: "Nullable(IPv6)", values: []rtValue{
			{name: "zero", in: netip.MustParseAddr("::"), lit: "toIPv6('::')"},
			{name: "max", in: netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"), lit: "toIPv6('ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff')"},
			{name: "documentation", in: netip.MustParseAddr("2001:db8::1"), lit: "toIPv6('2001:db8::1')"},
			{name: "mapped IPv4", in: netip.MustParseAddr("::ffff:1.2.3.4"), lit: "toIPv6('::ffff:1.2.3.4')"},
			null,
		}}),
		"Array": always(rtCase{column: "Array(Array(Nullable(String)))", values: []rtValue{
			{name: "empty", in: []any{}, want: []any{}, lit: "[]"},
			{name: "empty inside", in: []any{[]any{}}, want: []any{[]any{}}, lit: "[[]]"},
			{name: "values", in: []any{[]any{"a", nil, "é\t'"}, []any{}}, want: []any{[]any{"a", nil, "é\t'"}, []any{}}, lit: `[['a', NULL, 'é\t\''], []]`},
		}}),
		"Tuple": always(rtCase{
			column: "Tuple(Int32, Nullable(String))",
			// A Tuple has no Go type to bind, so the argument is its JSON text, which
			// JSONExtract reads (D176).
			insert: func(tbl string) string {
				return "INSERT INTO " + tbl + " (k, v) SELECT ?, JSONExtract(?, 'Tuple(Int32, Nullable(String))')"
			},
			set: "JSONExtract(?, 'Tuple(Int32, Nullable(String))')",
			values: []rtValue{
				{name: "zero", in: `[0, ""]`, want: []any{int64(0), ""}, lit: "JSONExtract('[0, \"\"]', 'Tuple(Int32, Nullable(String))')"},
				{name: "extremes", in: `[-2147483648, "é€"]`, want: []any{int64(math.MinInt32), "é€"}, lit: "JSONExtract('[-2147483648, \"é€\"]', 'Tuple(Int32, Nullable(String))')"},
				{name: "a NULL inside", in: `[2147483647, null]`, want: []any{int64(math.MaxInt32), nil}, lit: "JSONExtract('[2147483647, null]', 'Tuple(Int32, Nullable(String))')"},
			},
		}),
		"Map": always(rtCase{column: "Map(String, Int32)", values: []rtValue{
			{name: "empty", in: map[string]any{}, want: map[string]any{}, lit: "map()"},
			{name: "extremes", in: map[string]any{"a": int64(math.MinInt32), "b": int64(math.MaxInt32)}, want: map[string]any{"a": int64(math.MinInt32), "b": int64(math.MaxInt32)}, lit: "map('a', -2147483648, 'b', 2147483647)"},
			{name: "keys with escapes", in: map[string]any{"k'\\\"": int64(1), "": int64(0), "é": int64(-1)}, want: map[string]any{"k'\\\"": int64(1), "": int64(0), "é": int64(-1)}, lit: `map('k\'\\"', 1, '', 0, 'é', -1)`},
		}}),
		"Time": since(25, "25.3 has no Time", rtCase{
			column:  "Time",
			options: timeOptions(rel),
			values: []rtValue{
				{name: "zero", in: "00:00:00", want: time.Duration(0), lit: "CAST('00:00:00' AS Time)"},
				{name: "a time", in: "12:34:56", want: 12*time.Hour + 34*time.Minute + 56*time.Second, lit: "CAST('12:34:56' AS Time)"},
				{name: "max", in: "999:59:59", want: 999*time.Hour + 59*time.Minute + 59*time.Second, lit: "CAST('999:59:59' AS Time)"},
				{name: "min", in: "-999:59:59", want: -(999*time.Hour + 59*time.Minute + 59*time.Second), lit: "CAST('-999:59:59' AS Time)"},
			},
		}),
		"Time64": since(25, "25.3 has no Time64", rtCase{
			column:  "Time64(3)",
			options: timeOptions(rel),
			values: []rtValue{
				{name: "zero", in: "00:00:00.000", want: time.Duration(0), lit: "CAST('00:00:00.000' AS Time64(3))"},
				{name: "a time", in: "12:34:56.789", want: 12*time.Hour + 34*time.Minute + 56*time.Second + 789*time.Millisecond, lit: "CAST('12:34:56.789' AS Time64(3))"},
				{name: "max", in: "999:59:59.999", want: 999*time.Hour + 59*time.Minute + 59*time.Second + 999*time.Millisecond, lit: "CAST('999:59:59.999' AS Time64(3))"},
				{name: "min", in: "-999:59:59.999", want: -(999*time.Hour + 59*time.Minute + 59*time.Second + 999*time.Millisecond), lit: "CAST('-999:59:59.999' AS Time64(3))"},
			},
		}),
		"Interval": always(rtCase{
			column: "IntervalDay",
			insert: func(tbl string) string { return "INSERT INTO " + tbl + " (k, v) SELECT ?, toIntervalDay(?)" },
			set:    "toIntervalDay(?)",
			values: []rtValue{
				{name: "zero", in: int64(0), want: dbimp.Interval{}, lit: "toIntervalDay(0)"},
				{name: "days", in: int64(5), want: dbimp.Interval{Days: 5}, lit: "toIntervalDay(5)"},
				{name: "negative", in: int64(-3), want: dbimp.Interval{Days: -3}, lit: "toIntervalDay(-3)"},
				{name: "max", in: int64(math.MaxInt32), want: dbimp.Interval{Days: math.MaxInt32}, lit: "toIntervalDay(2147483647)"},
			},
		}),
		"Nothing": always(rtCase{
			// The server cannot store the type Nothing in a column (the code 370), and
			// the only value of it is NULL, so the table holds a Nullable(Int8) and
			// the select makes a Nullable(Nothing) from the stored NULL, with the
			// only expression that has that type: the NULL literal.
			column:     "Nullable(Int8)",
			selectExpr: "if(isNull(v), NULL, NULL)",
			values: []rtValue{
				{name: "null", lit: "NULL"},
				{name: "null again", lit: "NULL", in: nil},
			},
		}),
		// The server converts a value to a Variant only when its type is a member, so
		// the members are the Go types of the arguments: Int64, String, Array(Int64).
		"Variant": always(rtCase{column: "Variant(Int64, String, Array(Int64))", values: []rtValue{
			null,
			{name: "an integer", in: int64(-5), lit: "toInt64(-5)"},
			{name: "a string", in: "text é", lit: "'text é'"},
			{name: "an array", in: []int64{1, 2}, want: []any{int64(1), int64(2)}, lit: "CAST([1, 2] AS Array(Int64))"},
		}}),
		"Dynamic": always(rtCase{column: "Dynamic", skipUpdate: noDynamicUpdate(rel), values: []rtValue{
			null,
			{name: "an integer", in: int64(math.MinInt64), lit: "-9223372036854775808"},
			{name: "a string", in: "text é", lit: "'text é'"},
			{name: "a float", in: 1.5, lit: "1.5"},
			{name: "a bool", in: true, lit: "true"},
			{name: "an array", in: []int64{1, 2}, want: []any{int64(1), int64(2)}, lit: "[1, 2]"},
		}}),
		"JSON": always(rtCase{column: "JSON", skipUpdate: noDynamicUpdate(rel), values: []rtValue{
			jsonText("empty", `{}`, map[string]any{}),
			jsonText("a string", `{"a":"x"}`, map[string]any{"a": "x"}),
			jsonText("a number", `{"a":1,"b":{"c":[1.5,"x"]}}`, jsonNumbers(rel)),
		}}),
		"Point":           always(geo(rel, "Tuple(Float64, Float64)", [3]string{"[0, 0]", "[1.5, -2.5]", "[1e10, 1e-10]"})),
		"Ring":            always(geo(rel, "Array(Tuple(Float64, Float64))", [3]string{"[]", "[[0, 0], [1, 1]]", "[[0, 0], [1, 0], [1, 1], [0, 0]]"})),
		"Polygon":         always(geo(rel, "Array(Array(Tuple(Float64, Float64)))", [3]string{"[]", "[[[0, 0], [1, 1]]]", "[[[0, 0], [10, 0], [10, 10], [0, 0]], [[1, 1], [2, 1], [2, 2], [1, 1]]]"})),
		"MultiPolygon":    always(geo(rel, "Array(Array(Array(Tuple(Float64, Float64))))", [3]string{"[]", "[[[[0, 0], [1, 1]]]]", "[[[[0, 0], [10, 0], [10, 10], [0, 0]]], [[[20, 20], [21, 20], [21, 21], [20, 20]]]]"})),
		"LineString":      always(geo(rel, "Array(Tuple(Float64, Float64))", [3]string{"[]", "[[0, 0], [1, 1]]", "[[0, 0], [1.5, 2.5], [-3, 4]]"})),
		"MultiLineString": always(geo(rel, "Array(Array(Tuple(Float64, Float64)))", [3]string{"[]", "[[[0, 0], [1, 1]]]", "[[[0, 0], [1, 1]], [[2, 2], [3, 3], [4, 4]]]"})),
		"26.9 MultiPoint": since(26, "only 26.8 and 26.9 have MultiPoint", geo(rel, "Array(Tuple(Float64, Float64))", [3]string{"[]", "[[0, 0], [1, 1]]", "[[0, 0], [1.5, 2.5], [-3, 4]]"})),
		"26.9 Geometry": since(26, "only 26.8 and 26.9 have Geometry", rtCase{
			column: "Geometry",
			// A Geometry converts only the types that it holds by name, so the point is a
			// Point and not a tuple.
			insert: func(tbl string) string {
				return "INSERT INTO " + tbl + " (k, v) SELECT ?, CAST(JSONExtract(?, 'Point') AS Geometry)"
			},
			set: "CAST(JSONExtract(?, 'Point') AS Geometry)",
			values: []rtValue{
				{name: "a point", in: "[1.5, 2.5]", want: []any{1.5, 2.5}, lit: "CAST(JSONExtract('[1.5, 2.5]', 'Point') AS Geometry)"},
				{name: "the origin", in: "[0, 0]", want: []any{0.0, 0.0}, lit: "CAST(JSONExtract('[0, 0]', 'Point') AS Geometry)"},
			},
		}),
		"AggregateFunction": always(rtCase{
			column: "AggregateFunction(sum, UInt32)",
			insert: func(tbl string) string { return "INSERT INTO " + tbl + " (k, v) SELECT ?, sumState(toUInt32(?))" },
			// A mutation refuses an aggregate function in its expression (the code 184),
			// and takes the same function in a subquery.
			set: "(SELECT sumState(toUInt32(?)))",
			values: []rtValue{
				{name: "zero", in: int64(0), want: make([]byte, 8), lit: "sumState(toUInt32(0))"},
				{name: "five", in: int64(5), want: []byte{5, 0, 0, 0, 0, 0, 0, 0}, lit: "sumState(toUInt32(5))"},
				{name: "large", in: int64(math.MaxUint32), want: []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}, lit: "sumState(toUInt32(4294967295))"},
			},
		}),
		"26.9 QBit": since(26, "only 26.8 and 26.9 have QBit", rtCase{
			column: "QBit(Float32, 4)",
			insert: func(tbl string) string {
				return "INSERT INTO " + tbl + " (k, v) SELECT ?, CAST(JSONExtract(?, 'Array(Float32)') AS QBit(Float32, 4))"
			},
			set: "CAST(JSONExtract(?, 'Array(Float32)') AS QBit(Float32, 4))",
			values: []rtValue{
				{name: "ones", in: "[1, 2, 3, 4]", want: dbimp.Vector[float32]{1, 2, 3, 4}, lit: "CAST(JSONExtract('[1, 2, 3, 4]', 'Array(Float32)') AS QBit(Float32, 4))"},
				{name: "fractions", in: "[0.5, -0.5, 10000000000, 3]", want: dbimp.Vector[float32]{0.5, -0.5, 1e10, 3}, lit: "CAST(JSONExtract('[0.5, -0.5, 10000000000, 3]', 'Array(Float32)') AS QBit(Float32, 4))"},
			},
		}),
	}
}

// timeOptions returns the options that make a column of the type Time: the
// setting that 25.8 needs, and none for 26.8 and 26.9, which have the type by default.
func timeOptions(rel release) []clickhouse.Option {
	if rel.atLeast(26, 8) {
		return nil
	}
	return []clickhouse.Option{clickhouse.WithParameter("enable_time_time64_type", 1)}
}

// jsonText returns a value of a JSON column, which the argument writes as text
// that the server reads as JSON.
func jsonText(name, text string, want any) rtValue {
	return rtValue{name: name, in: text, want: want, lit: "CAST('" + text + "' AS JSON)"}
}

// mutationInstant returns the check that skips the update to an instant before the
// year 2001 on 26.8 and 26.9, which fails there. They store a DateTime64 parameter of a
// mutation as a string of its number, such as '0' or '-2208988800', and reads it
// back as the text of a time, which it refuses with the code 41, for an instant
// whose number has fewer than ten digits. A caller who binds an instant in an
// ALTER TABLE ... UPDATE on 26.8 and 26.9 meets the same refusal (measured, and see Faults in
// docs/CLICKHOUSE.md). The instants of the round trip after 2001 update fine.
func mutationInstant(rel release) func(from, to dbimptest.Value) string {
	if !rel.atLeast(26, 8) {
		return nil
	}
	return func(_, to dbimptest.Value) string {
		if tm, ok := to.In.(time.Time); ok && tm.Unix() < 1_000_000_000 {
			return "26.8 and 26.9 refuse a DateTime64 parameter in a mutation when the instant has fewer than ten digits (the code 41)"
		}
		return ""
	}
}

// noDynamicUpdate returns the check that skips each update of a column of the type
// Dynamic or JSON on 25.3, which refuses it (the code 420, measured).
func noDynamicUpdate(rel release) func(from, to dbimptest.Value) string {
	if !rel.is253() {
		return nil
	}
	return func(_, _ dbimptest.Value) string {
		return "25.3 refuses to update a column with dynamic subcolumns (the code 420)"
	}
}

// jsonNumbers is the value that a read gives for a JSON column with numbers: a
// number of an object is a number, and 25.3 writes the numbers inside an array as
// strings (measured).
func jsonNumbers(rel release) any {
	if rel.is253() {
		return map[string]any{"a": int64(1), "b": map[string]any{"c": []any{"1.5", "x"}}}
	}
	return map[string]any{"a": int64(1), "b": map[string]any{"c": []any{1.5, "x"}}}
}

// geo returns the case of a geometry type, whose value is the JSON text of its
// coordinates, which JSONExtract reads as the underlying type of the geometry
// (D176). A read gives nested lists of float64 (D177).
func geo(_ release, underlying string, texts [3]string) rtCase {
	c := rtCase{
		column: underlying,
		insert: func(tbl string) string {
			return "INSERT INTO " + tbl + " (k, v) SELECT ?, JSONExtract(?, '" + underlying + "')"
		},
		set: "JSONExtract(?, '" + underlying + "')",
	}
	for i, text := range texts {
		c.values = append(c.values, rtValue{
			name: "shape " + strconv.Itoa(i),
			in:   text,
			want: floatsOf(text),
			lit:  "JSONExtract('" + text + "', '" + underlying + "')",
		})
	}
	return c
}

// floatsOf reads the JSON text of nested lists of numbers as nested []any of
// float64.
func floatsOf(text string) any {
	var parse func(s string) (any, string)
	parse = func(s string) (any, string) {
		s = strings.TrimLeft(s, " ")
		if s[0] != '[' {
			end := strings.IndexAny(s, ",]")
			f, err := strconv.ParseFloat(s[:end], 64)
			if err != nil {
				panic(err)
			}
			return f, s[end:]
		}
		out := []any{}
		s = strings.TrimLeft(s[1:], " ")
		for s[0] != ']' {
			var v any
			v, s = parse(s)
			out = append(out, v)
			s = strings.TrimLeft(s, " ")
			s = strings.TrimPrefix(s, ",")
			s = strings.TrimLeft(s, " ")
		}
		return out, s[1:]
	}
	v, _ := parse(text)
	return v
}

// TestIntegrationRoundTripUnavailable holds the types that a release lacks, which
// features.json marks no: the server refuses a type that it does not know with
// the code 50, or reads the name as another type (measured). Each subtest runs
// where the type is missing and skips where it is there.
func TestIntegrationRoundTripUnavailable(t *testing.T) {
	st := theState(t)
	t.Run("25.3 Time", func(t *testing.T) {
		if !st.rel.is253() {
			t.Skip("the release has the type")
		}
		eachPrincipal(t, func(t *testing.T, e env) {
			// 25.3 reads the name Time as an alias of Int64, so a value of the text of
			// a time is not one, and Time64 is not a type.
			tbl := e.table(t, "missing", "CREATE TABLE %s (v Time) ENGINE = Memory")
			if got := e.scalar(t, "SELECT type FROM system.columns WHERE database = ? AND table = ? AND name = 'v'", e.database, prefix+"_missing"); got != "Int64" {
				t.Errorf("the column of the type Time is %v on 25.3, want Int64", got)
			}
			if cerr := e.refusal(t, "INSERT INTO "+tbl+" VALUES ('12:34:56')"); cerr.Code != 6 && cerr.Code != 27 {
				t.Errorf("a time of text in a column of the type Time gave %+v, want a refusal to read it as an integer", cerr)
			}
			if cerr := e.refusal(t, "CREATE TABLE "+e.name("missing64")+" (v Time64(3)) ENGINE = Memory"); cerr.Code != 50 {
				t.Errorf("a column of Time64 gave %+v, want the code 50", cerr)
			}
		})
	})
	for _, tt := range []struct {
		name   string
		column string
	}{
		{"25.8 MultiPoint", "MultiPoint"},
		{"25.8 QBit", "QBit(Float32, 4)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if st.rel.atLeast(26, 8) {
				t.Skip("the release has the type")
			}
			eachPrincipal(t, func(t *testing.T, e env) {
				tbl := e.name("missing")
				e.dropAtEnd(t, tbl)
				if cerr := e.refusal(t, "CREATE TABLE "+tbl+" (v "+tt.column+") ENGINE = Memory"); cerr.Code != 50 {
					t.Errorf("a column of %s gave %+v, want the code 50", tt.column, cerr)
				}
			})
		})
	}
	t.Run("25.8 Geometry", func(t *testing.T) {
		if st.rel.atLeast(26, 8) {
			t.Skip("the release has the type")
		}
		eachPrincipal(t, func(t *testing.T, e env) {
			// The name Geometry is the name of a String before 26.8 (measured).
			got := e.scalar(t, "SELECT toTypeName(CAST((1.5, 2.5)::Point AS Geometry))")
			if got != "String" {
				t.Errorf("the type Geometry is %v on this release, want String", got)
			}
		})
	})
}
