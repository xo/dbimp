package trino_test

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// rtValue is one value of a round trip: the value that the test writes as an
// argument, the value that a read must return, and the literal that writes it in
// SQL, which is an expression of the type of the column.
type rtValue struct {
	name string
	in   any
	want any
	lit  string
}

// rtCase is the round trip of one type.
type rtCase struct {
	// column is the type of the column.
	column string
	// insert is the SQL that inserts one row, with the key and the value as its
	// arguments, as a function of the table. The default is VALUES (?, ?).
	insert func(tbl string) string
	// selectExpr is the expression that the select returns, v by default.
	selectExpr string
	values     []rtValue
}

// literalInsert returns the statement that inserts the literal of a value.
func literalInsert(tbl, key, lit string) string {
	return "INSERT INTO " + tbl + " SELECT " + quoteSQL(key) + ", " + lit
}

// quoteSQL writes a string literal.
func quoteSQL(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// dec returns the decimal that text writes.
func decOf(t *testing.T, text string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(text)
	if err != nil {
		t.Fatalf("reading %q as a decimal: %v", text, err)
	}
	return d
}

// scalarBytes returns the bytes of the one value of a query.
func scalarBytes(t *testing.T, e env, query string) []byte {
	t.Helper()
	b, ok := e.scalar(t, query).([]byte)
	if !ok || len(b) == 0 {
		t.Fatalf("%q gave %v, want bytes", query, b)
	}
	return b
}

// fraction returns nanoseconds for a time of nine digits on Trino, and of three
// digits on Presto, which holds a millisecond (D175).
func fraction(e env, nanos int) int {
	if e.isPresto() {
		return nanos / 1e6 * 1e6
	}
	return nanos
}

// precision returns the type of a column with a time, with nine digits of fraction on
// Trino and three on Presto.
func precision(e env, name string) string {
	if e.isPresto() {
		return name
	}
	first, rest, _ := strings.Cut(name, " ")
	if rest != "" {
		rest = " " + rest
	}
	return first + "(9)" + rest
}

// sketch returns the case of a type that only a function makes: the argument is the
// number that the function reads, and the value is what the function makes of it,
// which the test reads with a select first.
func sketch(t *testing.T, e env, column, expr string) rtCase {
	t.Helper()
	var values []rtValue
	for _, n := range []int64{1, 42, 1 << 40} {
		query := "SELECT " + strings.ReplaceAll(expr, "?", strconv.FormatInt(n, 10))
		values = append(values, rtValue{
			name: strconv.FormatInt(n, 10),
			in:   n,
			want: scalarBytes(t, e, query),
			lit:  strings.ReplaceAll(expr, "?", strconv.FormatInt(n, 10)),
		})
	}
	return rtCase{
		column:     column,
		insert:     func(tbl string) string { return "INSERT INTO " + tbl + " SELECT ?, " + expr },
		selectExpr: "v",
		values:     values,
	}
}

// geometry returns the case of a type that holds the text WKT of a shape.
func geometry(column, expr string) rtCase {
	shapes := []string{"POINT (1 2)", "POLYGON ((0 0, 1 0, 1 1, 0 0))", "LINESTRING (0 0, 1 1, 2 0)"}
	var values []rtValue
	for i, wkt := range shapes {
		values = append(values, rtValue{name: strconv.Itoa(i), in: wkt, want: wkt, lit: strings.ReplaceAll(expr, "?", quoteSQL(wkt))})
	}
	values = append(values, rtValue{name: "null", lit: "NULL"})
	return rtCase{
		column: column,
		insert: func(tbl string) string { return "INSERT INTO " + tbl + " SELECT ?, " + expr },
		values: values,
	}
}

// roundTripCases returns the case of each type of features.json that is a yes,
// by the name that the survey gives it.
func roundTripCases(t *testing.T, e env) map[string]rtCase { //nolint:maintidx // One table, an entry for each type.
	t.Helper()
	d := func(text string) *apd.Decimal { return decOf(t, text) }
	nul := rtValue{name: "null", lit: "NULL"}
	ints := func(column string, lo, hi int64, loLit string) rtCase {
		if loLit == "" {
			loLit = strconv.FormatInt(lo, 10)
		}
		return rtCase{column: column, values: []rtValue{
			{name: "min", in: lo, want: lo, lit: loLit}, {name: "max", in: hi, want: hi, lit: strconv.FormatInt(hi, 10)},
			{name: "zero", in: int64(0), want: int64(0), lit: "0"}, nul,
		}}
	}
	u := func(s string) uuid.UUID {
		id, err := uuid.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	hour := int64(3600e9)
	// Presto refuses to insert an integer into a narrower column, and a double into a
	// real, with no cast, where Trino coerces them (measured), so the cases of
	// those types cast on Presto.
	cast := func(c rtCase) rtCase {
		if !e.isPresto() {
			return c
		}
		c.insert = func(tbl string) string { return "INSERT INTO " + tbl + " VALUES (?, CAST(? AS " + c.column + "))" }
		for i := range c.values {
			if c.values[i].lit != "NULL" {
				c.values[i].lit = "CAST(" + c.values[i].lit + " AS " + c.column + ")"
			}
		}
		return c
	}
	cases := map[string]rtCase{
		"BOOLEAN":  {column: "boolean", values: []rtValue{{name: "true", in: true, want: true, lit: "TRUE"}, {name: "false", in: false, want: false, lit: "FALSE"}, nul}},
		"TINYINT":  ints("tinyint", -128, 127, ""),
		"SMALLINT": ints("smallint", -32768, 32767, ""),
		"INTEGER":  ints("integer", -2147483648, 2147483647, ""),
		"BIGINT":   ints("bigint", math.MinInt64, math.MaxInt64, "BIGINT '-9223372036854775808'"),
		"REAL": {column: "real", values: []rtValue{
			{name: "fraction", in: 1.5, want: 1.5, lit: "REAL '1.5'"},
			{name: "max", in: 3.4028235e38, want: 3.4028235e38, lit: "REAL '3.4028235E38'"},
			{name: "smallest", in: 1.4e-45, want: 1.4e-45, lit: "REAL '1.4E-45'"},
			{name: "negative zero", in: math.Copysign(0, -1), want: math.Copysign(0, -1), lit: "REAL '-0e0'"},
			{name: "nan", in: math.NaN(), want: math.NaN(), lit: "nan()"}, nul,
		}},
		"DOUBLE": {column: "double", values: []rtValue{
			{name: "max", in: math.MaxFloat64, want: math.MaxFloat64, lit: "DOUBLE '1.7976931348623157E308'"},
			{name: "smallest", in: 5e-324, want: 5e-324, lit: "DOUBLE '4.9E-324'"},
			{name: "negative zero", in: math.Copysign(0, -1), want: math.Copysign(0, -1), lit: "DOUBLE '-0e0'"},
			{name: "nan", in: math.NaN(), want: math.NaN(), lit: "nan()"},
			{name: "infinity", in: math.Inf(1), want: math.Inf(1), lit: "infinity()"},
			{name: "negative infinity", in: math.Inf(-1), want: math.Inf(-1), lit: "-infinity()"}, nul,
		}},
		"DECIMAL": {column: "decimal(38, 2)", values: []rtValue{
			{name: "max", in: d("1234567890123456789012345678901234.56"), want: d("1234567890123456789012345678901234.56"), lit: "DECIMAL '1234567890123456789012345678901234.56'"},
			{name: "negative", in: d("-0.01"), want: d("-0.01"), lit: "DECIMAL '-0.01'"},
			{name: "zero", in: d("0.00"), want: d("0.00"), lit: "DECIMAL '0.00'"}, nul,
		}},
		"CHAR": {column: "char(5)", values: []rtValue{
			{name: "padded", in: "ab", want: "ab   ", lit: "'ab'"}, {name: "empty", in: "", want: "     ", lit: "''"},
			{name: "unicode", in: "日本語", want: "日本語  ", lit: "'日本語'"}, {name: "full", in: "abcde", want: "abcde", lit: "'abcde'"}, nul,
		}},
		"VARCHAR": {column: "varchar", values: []rtValue{
			{name: "empty", in: "", want: "", lit: "''"},
			{name: "quotes", in: "é'\"\\ x", want: "é'\"\\ x", lit: quoteSQL("é'\"\\ x")},
			{name: "long", in: strings.Repeat("long ", 2000), want: strings.Repeat("long ", 2000), lit: quoteSQL(strings.Repeat("long ", 2000))},
			{name: "unicode", in: "日本語 😀 ñ\n\t", want: "日本語 😀 ñ\n\t", lit: quoteSQL("日本語 😀 ñ\n\t")}, nul,
		}},
		"VARBINARY": {column: "varbinary", values: []rtValue{
			{name: "empty", in: []byte{}, want: []byte{}, lit: "X''"}, {name: "bytes", in: []byte{0, 0xff, 1}, want: []byte{0, 0xff, 1}, lit: "X'00ff01'"},
			{name: "long", in: make([]byte, 1000), want: make([]byte, 1000), lit: "X'" + strings.Repeat("00", 1000) + "'"}, nul,
		}},
		"JSON": {column: "json", insert: func(tbl string) string { return "INSERT INTO " + tbl + " VALUES (?, json_parse(?))" }, values: []rtValue{
			{name: "object", in: `{"a":[1,null,"x"]}`, want: map[string]any{"a": []any{int64(1), nil, "x"}}, lit: `JSON '{"a":[1,null,"x"]}'`},
			{name: "array", in: `[]`, want: []any{}, lit: `JSON '[]'`},
			{name: "string", in: `"é"`, want: "é", lit: `JSON '"é"'`},
			{name: "float", in: `1.5`, want: 1.5, lit: `JSON '1.5'`},
			{name: "integer", in: `9223372036854775807`, want: int64(9223372036854775807), lit: `JSON '9223372036854775807'`},
			{name: "true", in: `true`, want: true, lit: `JSON 'true'`}, nul,
		}},
		"INTERVAL YEAR TO MONTH": {column: "interval year to month", values: []rtValue{
			{name: "months", in: dbimp.Interval{Months: 14}, want: dbimp.Interval{Months: 14}, lit: "INTERVAL '1-2' YEAR TO MONTH"},
			{name: "negative", in: dbimp.Interval{Months: -14}, want: dbimp.Interval{Months: -14}, lit: "INTERVAL - '1-2' YEAR TO MONTH"},
			// A zero Interval is written as a day to second interval, because it names
			// no unit, so it is not a value of this type.
			{name: "one month", in: dbimp.Interval{Months: 1}, want: dbimp.Interval{Months: 1}, lit: "INTERVAL '0-1' YEAR TO MONTH"},
			{name: "large", in: dbimp.Interval{Months: 12000000}, want: dbimp.Interval{Months: 12000000}, lit: "INTERVAL '1000000-0' YEAR TO MONTH"}, nul,
		}},
		"INTERVAL DAY TO SECOND": {column: "interval day to second", values: []rtValue{
			{name: "mixed", in: dbimp.Interval{Days: 3, Nanoseconds: 4*hour + 5*60e9 + 6789e6}, want: dbimp.Interval{Days: 3, Nanoseconds: 4*hour + 5*60e9 + 6789e6}, lit: "INTERVAL '3 04:05:06.789' DAY TO SECOND"},
			{name: "negative days", in: dbimp.Interval{Days: -1}, want: dbimp.Interval{Days: -1}, lit: "INTERVAL - '1 00:00:00.000' DAY TO SECOND"},
			{name: "negative time", in: dbimp.Interval{Nanoseconds: -1500e6}, want: dbimp.Interval{Nanoseconds: -1500e6}, lit: "INTERVAL - '0 00:00:01.500' DAY TO SECOND"},
			{name: "zero", in: dbimp.Interval{}, want: dbimp.Interval{}, lit: "INTERVAL '0 00:00:00.000' DAY TO SECOND"},
			{name: "large", in: dbimp.Interval{Days: 1000000}, want: dbimp.Interval{Days: 1000000}, lit: "INTERVAL '1000000 00:00:00.000' DAY TO SECOND"}, nul,
		}},
		"UUID": {column: "uuid", values: []rtValue{
			{name: "some", in: u("12345678-1234-5678-1234-567812345678"), want: u("12345678-1234-5678-1234-567812345678"), lit: "UUID '12345678-1234-5678-1234-567812345678'"},
			{name: "zero", in: uuid.UUID{}, want: uuid.UUID{}, lit: "UUID '00000000-0000-0000-0000-000000000000'"},
			{name: "max", in: u("ffffffff-ffff-ffff-ffff-ffffffffffff"), want: u("ffffffff-ffff-ffff-ffff-ffffffffffff"), lit: "UUID 'ffffffff-ffff-ffff-ffff-ffffffffffff'"}, nul,
		}},
		"IPADDRESS": {column: "ipaddress", insert: func(tbl string) string { return "INSERT INTO " + tbl + " VALUES (?, CAST(? AS ipaddress))" }, values: []rtValue{
			{name: "v4", in: "10.0.0.1", want: "10.0.0.1", lit: "IPADDRESS '10.0.0.1'"}, {name: "v6", in: "2001:db8::ff00:42:8329", want: "2001:db8::ff00:42:8329", lit: "IPADDRESS '2001:db8::ff00:42:8329'"},
			{name: "loopback", in: "::1", want: "::1", lit: "IPADDRESS '::1'"}, {name: "broadcast", in: "255.255.255.255", want: "255.255.255.255", lit: "IPADDRESS '255.255.255.255'"}, nul,
		}},
		"DATE": {column: "date", values: []rtValue{
			{name: "some", in: dbimp.Date{Year: 2026, Month: 10, Day: 1}, want: dbimp.Date{Year: 2026, Month: 10, Day: 1}, lit: "DATE '2026-10-01'"},
			{name: "min", in: dbimp.Date{Year: 1, Month: 1, Day: 1}, want: dbimp.Date{Year: 1, Month: 1, Day: 1}, lit: "DATE '0001-01-01'"},
			{name: "max", in: dbimp.Date{Year: 9999, Month: 12, Day: 31}, want: dbimp.Date{Year: 9999, Month: 12, Day: 31}, lit: "DATE '9999-12-31'"},
			{name: "leap day", in: dbimp.Date{Year: 2024, Month: 2, Day: 29}, want: dbimp.Date{Year: 2024, Month: 2, Day: 29}, lit: "DATE '2024-02-29'"}, nul,
		}},
	}
	tm := func(h, m, s, ns int) dbimp.LocalTime {
		return dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: fraction(e, ns)}
	}
	frac := func(ns int) string {
		if e.isPresto() {
			return fmt.Sprintf("%03d", ns/1e6)
		}
		return fmt.Sprintf("%09d", ns)
	}
	for _, name := range []string{"TINYINT", "SMALLINT", "INTEGER", "REAL"} {
		cases[name] = cast(cases[name])
	}
	cases["TIME"] = rtCase{column: precision(e, "time"), values: []rtValue{
		{name: "midnight", in: tm(0, 0, 0, 0), want: tm(0, 0, 0, 0), lit: "TIME '00:00:00." + frac(0) + "'"},
		{name: "last", in: tm(23, 59, 59, 999999999), want: tm(23, 59, 59, 999999999), lit: "TIME '23:59:59." + frac(999999999) + "'"},
		{name: "some", in: tm(12, 34, 56, 123456789), want: tm(12, 34, 56, 123456789), lit: "TIME '12:34:56." + frac(123456789) + "'"}, nul,
	}}
	ot := func(h, m, s, ns, off int) dbimp.OffsetTime {
		return dbimp.OffsetTime{Time: tm(h, m, s, ns), Offset: off}
	}
	cases["TIME WITH TIME ZONE"] = rtCase{column: precision(e, "time with time zone"), values: []rtValue{
		{name: "utc", in: ot(0, 0, 0, 0, 0), want: ot(0, 0, 0, 0, 0), lit: "TIME '00:00:00." + frac(0) + " +00:00'"},
		{name: "east", in: ot(23, 59, 59, 999999999, 14*3600), want: ot(23, 59, 59, 999999999, 14*3600), lit: "TIME '23:59:59." + frac(999999999) + " +14:00'"},
		{name: "west", in: ot(12, 34, 56, 123456789, -5*3600-30*60), want: ot(12, 34, 56, 123456789, -5*3600-30*60), lit: "TIME '12:34:56." + frac(123456789) + " -05:30'"}, nul,
	}}
	dt := func(y int, mo time.Month, d int, tt dbimp.LocalTime) dbimp.LocalDateTime {
		return dbimp.LocalDateTime{Date: dbimp.Date{Year: y, Month: mo, Day: d}, Time: tt}
	}
	cases["TIMESTAMP"] = rtCase{column: precision(e, "timestamp"), values: []rtValue{
		{name: "min", in: dt(1, 1, 1, tm(0, 0, 0, 0)), want: dt(1, 1, 1, tm(0, 0, 0, 0)), lit: "TIMESTAMP '0001-01-01 00:00:00." + frac(0) + "'"},
		{name: "max", in: dt(9999, 12, 31, tm(23, 59, 59, 999999999)), want: dt(9999, 12, 31, tm(23, 59, 59, 999999999)), lit: "TIMESTAMP '9999-12-31 23:59:59." + frac(999999999) + "'"},
		{name: "some", in: dt(2026, 10, 1, tm(12, 34, 56, 123456789)), want: dt(2026, 10, 1, tm(12, 34, 56, 123456789)), lit: "TIMESTAMP '2026-10-01 12:34:56." + frac(123456789) + "'"}, nul,
	}}
	at := func(y int, mo time.Month, d, h, mi, s, ns, off int) time.Time {
		loc := time.UTC
		if off != 0 {
			loc = time.FixedZone("", off)
		}
		return time.Date(y, mo, d, h, mi, s, fraction(e, ns), loc)
	}
	cases["TIMESTAMP WITH TIME ZONE"] = rtCase{column: precision(e, "timestamp with time zone"), values: []rtValue{
		{name: "min", in: at(1, 1, 1, 0, 0, 0, 0, 0), want: at(1, 1, 1, 0, 0, 0, 0, 0), lit: "TIMESTAMP '0001-01-01 00:00:00." + frac(0) + " +00:00'"},
		{name: "max", in: at(9999, 12, 31, 23, 59, 59, 999999999, 0), want: at(9999, 12, 31, 23, 59, 59, 999999999, 0), lit: "TIMESTAMP '9999-12-31 23:59:59." + frac(999999999) + " +00:00'"},
		{name: "east", in: at(2026, 10, 1, 12, 34, 56, 123456789, 5*3600+30*60), want: at(2026, 10, 1, 12, 34, 56, 123456789, 5*3600+30*60), lit: "TIMESTAMP '2026-10-01 12:34:56." + frac(123456789) + " +05:30'"},
		{name: "west", in: at(2026, 3, 8, 1, 2, 3, 4000000, -8*3600), want: at(2026, 3, 8, 1, 2, 3, 4000000, -8*3600), lit: "TIMESTAMP '2026-03-08 01:02:03." + frac(4000000) + " -08:00'"}, nul,
	}}
	cases["ARRAY"] = rtCase{column: "array(array(integer))", values: []rtValue{
		{name: "nested", in: []any{[]any{int64(1)}, []any{int64(2), int64(3)}}, want: []any{[]any{int64(1)}, []any{int64(2), int64(3)}}, lit: "ARRAY[ARRAY[1], ARRAY[2, 3]]"},
		{name: "empty", in: []any{}, want: []any{}, lit: "CAST(ARRAY[] AS array(array(integer)))"},
		{name: "nulls", in: []any{nil, []any{}, []any{nil, int64(2147483647)}}, want: []any{nil, []any{}, []any{nil, int64(2147483647)}}, lit: "ARRAY[NULL, CAST(ARRAY[] AS array(integer)), ARRAY[NULL, 2147483647]]"}, nul,
	}}
	cases["MAP"] = rtCase{column: "map(varchar, integer)", values: []rtValue{
		{name: "two", in: map[string]any{"a": int64(1), "b": int64(2)}, want: map[string]any{"a": int64(1), "b": int64(2)}, lit: "MAP(ARRAY['a', 'b'], ARRAY[1, 2])"},
		{name: "empty", in: map[string]any{}, want: map[string]any{}, lit: "CAST(MAP() AS map(varchar, integer))"},
		{name: "unicode", in: map[string]any{"é": int64(-1), "日本": nil}, want: map[string]any{"é": int64(-1), "日本": nil}, lit: "MAP(ARRAY['é', '日本'], ARRAY[-1, NULL])"}, nul,
	}}
	// The driver sends no ROW as an argument, because a []any is an ARRAY, so
	// the statement casts the JSON text that the argument holds.
	cases["ROW"] = rtCase{column: "row(a integer, b varchar)", insert: func(tbl string) string {
		return "INSERT INTO " + tbl + " SELECT ?, CAST(JSON_PARSE(?) AS row(a integer, b varchar))"
	}, values: []rtValue{
		{name: "both", in: `[1,"x"]`, want: []any{int64(1), "x"}, lit: "CAST(ROW(1, 'x') AS row(a integer, b varchar))"},
		{name: "nulls", in: `[null,null]`, want: []any{nil, nil}, lit: "CAST(ROW(NULL, NULL) AS row(a integer, b varchar))"},
		{name: "extremes", in: `[2147483647,"é"]`, want: []any{int64(2147483647), "é"}, lit: "CAST(ROW(2147483647, 'é') AS row(a integer, b varchar))"}, nul,
	}}
	cases["HYPERLOGLOG"] = sketch(t, e, "HyperLogLog", "approx_set(CAST(? AS bigint))")
	cases["P4HYPERLOGLOG"] = sketch(t, e, "P4HyperLogLog", "CAST(approx_set(CAST(? AS bigint)) AS P4HyperLogLog)")
	cases["SETDIGEST"] = sketch(t, e, "SetDigest", "make_set_digest(CAST(? AS bigint))")
	cases["QDIGEST"] = sketch(t, e, "qdigest(bigint)", "qdigest_agg(CAST(? AS bigint))")
	cases["TDIGEST"] = sketch(t, e, "tdigest", "tdigest_agg(CAST(? AS double))")
	if e.isPresto() {
		cases["TDIGEST"] = sketch(t, e, "tdigest(double)", "tdigest_agg(CAST(? AS double))")
	}
	cases["GEOMETRY"] = geometry("Geometry", "ST_GeometryFromText(?)")
	cases["SPHERICALGEOGRAPHY"] = geometry("SphericalGeography", "to_spherical_geography(ST_GeometryFromText(?))")
	cases["UNKNOWN"] = rtCase{column: "bigint", selectExpr: "NULL", values: []rtValue{
		{name: "first", lit: "NULL"}, {name: "second", lit: "NULL"},
	}}
	cases["trino 483 NUMBER"] = rtCase{column: "number", insert: func(tbl string) string { return "INSERT INTO " + tbl + " VALUES (?, CAST(? AS NUMBER))" }, values: []rtValue{
		{name: "fraction", in: d("1.5"), want: d("1.5"), lit: "CAST(DECIMAL '1.5' AS NUMBER)"},
		{name: "negative", in: d("-0.001"), want: d("-0.001"), lit: "CAST(DECIMAL '-0.001' AS NUMBER)"},
		{name: "digits", in: d("12345678901234567890123456789012345678"), want: d("12345678901234567890123456789012345678"), lit: "CAST(DECIMAL '12345678901234567890123456789012345678' AS NUMBER)"},
		nul,
	}}
	cases["trino 483 VARIANT"] = rtCase{column: "variant", insert: func(tbl string) string { return "INSERT INTO " + tbl + " VALUES (?, CAST(json_parse(?) AS VARIANT))" }, values: []rtValue{
		{name: "object", in: `{"a":[1,"x"]}`, want: map[string]any{"a": []any{int64(1), "x"}}, lit: `CAST(JSON '{"a":[1,"x"]}' AS VARIANT)`},
		{name: "integer", in: `42`, want: int64(42), lit: `CAST(JSON '42' AS VARIANT)`},
		{name: "string", in: `"é"`, want: "é", lit: `CAST(JSON '"é"' AS VARIANT)`}, nul,
	}}
	return cases
}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type that
// features.json marks yes, and tests the refusal of each type that it marks no,
// per flavor (step 14a). The memory catalog refuses UPDATE and DELETE, so the round
// trip skips the update and the delete, and a table that a test drops holds the
// rows (D175).
func TestIntegrationRoundTrip(t *testing.T) { //nolint:maintidx // One table, an entry for each type.
	e := newEnv(t)
	cases := roundTripCases(t, e)
	roundTrip := func(t *testing.T, e env, name string) {
		t.Helper()
		c, ok := cases[name]
		if !ok {
			t.Fatalf("no round trip for %s", name)
		}
		tbl := e.table(t, "rt_"+name)
		insert := "INSERT INTO " + tbl + " VALUES (?, ?)"
		if c.insert != nil {
			insert = c.insert(tbl)
		}
		selectExpr := c.selectExpr
		if selectExpr == "" {
			selectExpr = "v"
		}
		values := make([]dbimptest.Value, len(c.values))
		for i, v := range c.values {
			values[i] = dbimptest.Value{Name: v.name, In: v.in, Want: v.want}
		}
		dbimptest.RoundTrip(t, e.db, dbimptest.RoundTripCase{
			Type:     name,
			Setup:    []string{"CREATE TABLE " + tbl + " (k varchar, v " + c.column + ")"},
			Teardown: []string{"DROP TABLE IF EXISTS " + tbl},
			Insert:   insert,
			Literal: func(key string, in any) (string, error) {
				for _, v := range c.values {
					if equalValues(in, v.in) && v.lit != "" {
						return literalInsert(tbl, key, v.lit), nil
					}
				}
				return "", fmt.Errorf("the case has no literal for %#v: %w", in, dbimp.ErrNotSupported)
			},
			Select: "SELECT " + selectExpr + " FROM " + tbl + " WHERE k = ?",
			// The memory catalog refuses UPDATE and DELETE, so a value stays and
			// the table drops at the end.
			SkipUpdate: func(dbimptest.Value, dbimptest.Value) string { return "the memory catalog refuses UPDATE" },
			Values:     values,
			Equal:      equalValues,
		})
	}
	refuseType := func(column string) func(*testing.T, env) {
		return func(t *testing.T, e env) {
			tbl := e.table(t, t.Name())
			e.refused(t, "CREATE TABLE "+tbl+" (v "+column+")")
		}
	}
	var features []feature
	for _, name := range []string{
		"BOOLEAN", "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "REAL", "DOUBLE", "DECIMAL", "CHAR", "VARCHAR", "VARBINARY", "JSON",
		"INTERVAL YEAR TO MONTH", "INTERVAL DAY TO SECOND", "UUID", "IPADDRESS", "DATE", "TIME", "TIME WITH TIME ZONE", "TIMESTAMP",
		"TIMESTAMP WITH TIME ZONE", "ARRAY", "MAP", "ROW", "HYPERLOGLOG", "P4HYPERLOGLOG", "SETDIGEST", "QDIGEST", "TDIGEST", "GEOMETRY",
		"SPHERICALGEOGRAPHY", "UNKNOWN",
	} {
		features = append(features, feature{name: name, parallel: true, run: func(t *testing.T, e env) { roundTrip(t, e, name) }})
	}
	features = append(features,
		feature{name: "trino 483 NUMBER", minTrino: 483, parallel: true, run: func(t *testing.T, e env) { roundTrip(t, e, "trino 483 NUMBER") }},
		feature{name: "trino 476 NUMBER", maxTrino: 476, run: refuseType("number")},
		feature{name: "presto NUMBER", run: refuseType("number")},
		feature{name: "trino 483 VARIANT", minTrino: 483, parallel: true, run: func(t *testing.T, e env) { roundTrip(t, e, "trino 483 VARIANT") }},
		feature{name: "trino 476 VARIANT", maxTrino: 476, run: refuseType("variant")},
		feature{name: "presto VARIANT", run: refuseType("variant")},
	)
	runFeaturesOn(t, e, features)
}
