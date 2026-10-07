package clickhouse //nolint:testpackage // The tests read the types of the driver, which are not exported.

import (
	"encoding/json/jsontext"
	"errors"
	"math"
	"math/big"
	"net/netip"
	"reflect"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// TestParseType holds the reading of the names of types, with the wrappers and
// the arguments of each family (measured).
func TestParseType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		want typ
	}{
		{"Int32", typ{family: famInt32}},
		{"Nullable(Int32)", typ{family: famInt32, nullable: true}},
		{"LowCardinality(String)", typ{family: famString}},
		{"LowCardinality(Nullable(String))", typ{family: famString, nullable: true}},
		{"SimpleAggregateFunction(max, Int32)", typ{family: famInt32}},
		{"SimpleAggregateFunction(anyLast, Nullable(DateTime64(3)))", typ{family: famDateTime64, nullable: true, nums: []int64{3}}},
		{"Decimal(76, 40)", typ{family: famDecimal, nums: []int64{76, 40}}},
		{"Decimal32(4)", typ{family: famDecimal, nums: []int64{9, 4}}},
		{"Decimal256(40)", typ{family: famDecimal, nums: []int64{76, 40}}},
		{"FixedString(4)", typ{family: famFixedString, nums: []int64{4}}},
		{"DateTime", typ{family: famDateTime}},
		{"DateTime('Asia/Jakarta')", typ{family: famDateTime, zone: "Asia/Jakarta"}},
		{"DateTime64(9)", typ{family: famDateTime64, nums: []int64{9}}},
		{"DateTime64(3, 'America/New_York')", typ{family: famDateTime64, nums: []int64{3}, zone: "America/New_York"}},
		{"Time64(3)", typ{family: famTime64, nums: []int64{3}}},
		{"Enum8('a' = 1, 'b, (c)' = 2)", typ{family: famEnum8}},
		{"IntervalDay", typ{family: famInterval, unit: "Day"}},
		{"Variant(Array(Int32), Int32, String)", typ{family: famVariant}},
		{"JSON(max_dynamic_paths=10, a.b Int32)", typ{family: famJSON}},
		{"AggregateFunction(sum, UInt32)", typ{family: famAggregateFunction}},
		{"QBit(Float32, 4)", typ{family: famQBit, nums: []int64{4}, elems: []*typ{{family: famFloat32}}}},
		{"Array(Array(String))", typ{family: famArray, elems: []*typ{{family: famArray, elems: []*typ{{family: famString}}}}}},
		{"Map(Int32, Array(String))", typ{family: famMap, elems: []*typ{{family: famInt32}, {family: famArray, elems: []*typ{{family: famString}}}}}},
		{"Tuple(Int32, String)", typ{family: famTuple, elems: []*typ{{family: famInt32}, {family: famString}}, names: []string{"", ""}}},
		{"Tuple(a Int32, `b c` Array(Nullable(String)))", typ{family: famTuple, elems: []*typ{{family: famInt32}, {family: famArray, elems: []*typ{{family: famString, nullable: true}}}}, names: []string{"a", "b c"}}},
		{"Tuple(Decimal(10, 2), DateTime64(3, 'UTC'))", typ{family: famTuple, elems: []*typ{{family: famDecimal, nums: []int64{10, 2}}, {family: famDateTime64, nums: []int64{3}, zone: "UTC"}}, names: []string{"", ""}}},
		{"Nullable(Nothing)", typ{family: famNothing, nullable: true}},
		{"Point", typ{family: famPoint}},
	} {
		got, err := parseType(tt.name)
		if err != nil {
			t.Errorf("parseType(%q): %v", tt.name, err)
			continue
		}
		if !reflect.DeepEqual(*got, tt.want) {
			t.Errorf("parseType(%q) = %+v, want %+v", tt.name, *got, tt.want)
		}
	}
}

// TestParseTypeRefuses holds that a name that cannot be a type is an error.
func TestParseTypeRefuses(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"Array(Int32",
		"Array()",
		"Array(Int32, Int32)",
		"Map(String)",
		"Decimal(10)",
		"Decimal(x, 2)",
		"FixedString()",
		"Nullable()",
		"DateTime64()",
		"QBit(Float32)",
		"Tuple(a Int32, b Array(",
	} {
		if _, err := parseType(name); err == nil || !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseType(%q) = %v, want an error that wraps dbimp.ErrInvalidValue", name, err)
		}
	}
}

// decodeAs decodes the JSON text of one value as the type named name.
func decodeAs(t *testing.T, name, text string) (any, error) {
	t.Helper()
	ty, err := parseType(name)
	if err != nil {
		t.Fatalf("parseType(%q): %v", name, err)
	}
	return ty.decode(jsontext.Value(text))
}

func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func bigOf(t *testing.T, s string) *big.Int {
	t.Helper()
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("%q is not an integer", s)
	}
	return n
}

// TestDecode holds D135, D176 and D177 for each family: the Go type of each
// value, and the value that the text of the server gives.
func TestDecode(t *testing.T) {
	t.Parallel()
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skip("the host has no tzdata")
	}
	for _, tt := range []struct {
		name string
		text string
		want any
	}{
		{"Int8", `-128`, int64(-128)},
		{"UInt32", `4294967295`, int64(4294967295)},
		{"Int64", `-9223372036854775808`, int64(math.MinInt64)},
		// 25.3 quotes the integers of 64 bits unless the setting says not to, and
		// the driver reads either form.
		{"Int64", `"-9223372036854775808"`, int64(math.MinInt64)},
		{"UInt64", `18446744073709551615`, uint64(math.MaxUint64)},
		{"UInt64", `"18446744073709551615"`, uint64(math.MaxUint64)},
		{"Int128", `-170141183460469231731687303715884105728`, bigOf(t, "-170141183460469231731687303715884105728")},
		{"UInt256", `115792089237316195423570985008687907853269984665640564039457584007913129639935`, bigOf(t, "115792089237316195423570985008687907853269984665640564039457584007913129639935")},
		{"Float64", `1.7976931348623157e308`, math.MaxFloat64},
		{"Float32", `0.1`, 0.1},
		{"Float64", `-0`, math.Copysign(0, -1)},
		{"Float64", `"inf"`, math.Inf(1)},
		{"Float64", `"-inf"`, math.Inf(-1)},
		{"BFloat16", `3.140625`, 3.140625},
		{"Decimal(76, 40)", `-0.0000000000000000000000000000000000000001`, dec(t, "-1E-40")},
		{"Decimal(9, 4)", `99999.9999`, dec(t, "99999.9999")},
		{"Bool", `true`, true},
		{"String", `"héllo €"`, "héllo €"},
		{"String", "\"\xff\x80\"", "\xff\x80"},
		{"String", `"\u0000\/\"\\"`, "\x00/\"\\"},
		{"String", `"😀"`, "😀"},
		{"FixedString(4)", `"ab\u0000\u0000"`, "ab\x00\x00"},
		{"Enum8('a' = 1, 'b' = 2)", `"b"`, "b"},
		{"Date", `"2149-06-06"`, dbimp.Date{Year: 2149, Month: 6, Day: 6}},
		{"Date32", `"1900-01-01"`, dbimp.Date{Year: 1900, Month: 1, Day: 1}},
		{"DateTime('Asia/Jakarta')", `"2026-10-07T05:34:56Z"`, time.Date(2026, 10, 7, 12, 34, 56, 0, jakarta)},
		{"DateTime", `"2106-02-07T06:28:15Z"`, time.Date(2106, 2, 7, 6, 28, 15, 0, time.UTC)},
		{"DateTime64(9)", `"2262-04-11T23:47:16.854775807Z"`, time.Date(2262, 4, 11, 23, 47, 16, 854775807, time.UTC)},
		{"DateTime64(3, 'UTC')", `"1969-12-31T23:59:59.999Z"`, time.Date(1969, 12, 31, 23, 59, 59, 999e6, time.UTC)},
		{"Time", `"999:59:59"`, 999*time.Hour + 59*time.Minute + 59*time.Second},
		{"Time", `"12:34:56Z"`, 12*time.Hour + 34*time.Minute + 56*time.Second},
		{"Time64(3)", `"-00:00:01.500Z"`, -1500 * time.Millisecond},
		{"Time", `"-999:59:59"`, -(999*time.Hour + 59*time.Minute + 59*time.Second)},
		{"Time64(9)", `"-00:00:00.000000001"`, -time.Nanosecond},
		{"Time64(3)", `"12:34:56.789"`, 12*time.Hour + 34*time.Minute + 56*time.Second + 789*time.Millisecond},
		{"IntervalDay", `-3`, dbimp.Interval{Days: -3}},
		{"IntervalWeek", `2`, dbimp.Interval{Days: 14}},
		{"IntervalMonth", `5`, dbimp.Interval{Months: 5}},
		{"IntervalQuarter", `2`, dbimp.Interval{Months: 6}},
		{"IntervalYear", `1`, dbimp.Interval{Months: 12}},
		{"IntervalHour", `2`, dbimp.Interval{Nanoseconds: 7200e9}},
		{"IntervalMillisecond", `-5`, dbimp.Interval{Nanoseconds: -5e6}},
		{"IntervalNanosecond", `9223372036854775807`, dbimp.Interval{Nanoseconds: math.MaxInt64}},
		{"UUID", `"61f0c404-5cb3-11e7-907b-a6006ad3dba0"`, uuid.MustParse("61f0c404-5cb3-11e7-907b-a6006ad3dba0")},
		{"IPv4", `"192.168.0.1"`, netip.MustParseAddr("192.168.0.1")},
		{"IPv6", `"::ffff:1.2.3.4"`, netip.MustParseAddr("::ffff:1.2.3.4")},
		{"Array(Int32)", `[1, 2,3]`, []any{int64(1), int64(2), int64(3)}},
		{"Array(Nothing)", `[]`, []any{}},
		{"Array(Array(String))", `[["a","b"],[],["c"]]`, []any{[]any{"a", "b"}, []any{}, []any{"c"}}},
		{"Array(Nullable(Int32))", `[1,null,3]`, []any{int64(1), nil, int64(3)}},
		{"Tuple(Int32, String)", `[1,"x"]`, []any{int64(1), "x"}},
		// A caller who turns the setting back gets an object, which is read in the
		// order of its members.
		{"Tuple(a Int32, b String)", `{"a":2,"b":"y"}`, []any{int64(2), "y"}},
		{"Map(String, Int32)", `{"a":2,"b":-1}`, map[string]any{"a": int64(2), "b": int64(-1)}},
		{"Map(Int32, Array(String))", `{"1":["p","q"]}`, map[string]any{"1": []any{"p", "q"}}},
		{"Map(Tuple(Int32, Int32), Int32)", `{"(1,2)":3}`, map[string]any{"(1,2)": int64(3)}},
		{"Variant(Array(Int32), Int32, String)", `[1,2]`, []any{int64(1), int64(2)}},
		{"Variant(Int32, String)", `"text"`, "text"},
		{"Dynamic", `42`, int64(42)},
		{"Dynamic", `1.5`, 1.5},
		{"Dynamic", `true`, true},
		{"JSON", `{"a":1,"b":{"c":[1,2,"x"]}}`, map[string]any{"a": int64(1), "b": map[string]any{"c": []any{int64(1), int64(2), "x"}}}},
		{"JSON", `{"n":18446744073709551615}`, map[string]any{"n": dec(t, "18446744073709551615")}},
		{"Point", `[1,2.5]`, []any{1.0, 2.5}},
		{"Polygon", `[[[0,0],[1,0],[1,1]]]`, []any{[]any{[]any{0.0, 0.0}, []any{1.0, 0.0}, []any{1.0, 1.0}}}},
		{"AggregateFunction(sum, UInt32)", `"\u0005\u0000\u0000\u0000\u0000\u0000\u0000\u0000"`, []byte{5, 0, 0, 0, 0, 0, 0, 0}},
		{"AggregateFunction(sum, UInt32)", "\"\xff\xfe\"", []byte{0xff, 0xfe}},
		{"QBit(Float32, 4)", `[1,2.5,-3,0.1]`, dbimp.Vector[float32]{1, 2.5, -3, 0.1}},
		{"Nullable(Nothing)", `null`, nil},
		{"Nothing", `0`, nil},
		{"Nullable(Int32)", `null`, nil},
		{"Array(Int32)", `null`, nil},
		{"Nullable(String)", `""`, ""},
		{"Int8", ``, nil},
	} {
		got, err := decodeAs(t, tt.name, tt.text)
		if err != nil {
			t.Errorf("decoding %s from %s: %v", tt.name, tt.text, err)
			continue
		}
		if !equal(got, tt.want) {
			t.Errorf("decoding %s from %s gave %#v, want %#v", tt.name, tt.text, got, tt.want)
		}
	}
}

// equal compares two decoded values, with the same Go type.
func equal(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case time.Time:
		g, ok := got.(time.Time)
		return ok && g.Equal(w) && g.Location().String() == w.Location().String()
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case *big.Int:
		g, ok := got.(*big.Int)
		return ok && g.Cmp(w) == 0
	}
	return reflect.DeepEqual(got, want)
}

// TestDecodeRefuses holds that a value that does not fit its type is an error
// and never a guess (D135).
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, text string }{
		{"Int8", `"x"`},
		{"Int8", `1.5`},
		{"Int8", `true`},
		{"UInt64", `-1`},
		{"Int128", `1e5`},
		{"Float64", `"abc"`},
		{"Bool", `1`},
		{"String", `1`},
		{"Date", `"2026-13-40"`},
		{"Date", `20261007`},
		{"DateTime", `"2026-10-07 12:00:00"`},
		{"DateTime", `1790000000`},
		{"Time", `"12:34"`},
		{"Time", `"1234:00:00"`},
		{"Time64(3)", `"12:34:56.1234567890"`},
		{"UUID", `"not a uuid"`},
		{"IPv4", `"1.2.3"`},
		{"Array(Int32)", `{"a":1}`},
		{"Array(Int32)", `[1,"x"]`},
		{"Tuple(Int32, String)", `[1]`},
		{"Tuple(Int32, String)", `[1,"a","b"]`},
		{"Map(String, Int32)", `[1]`},
		{"IntervalDay", `2147483648`},
		{"IntervalYear", `178956971`},
		{"IntervalNanosecond", `"x"`},
		{"IntervalFortnight", `1`},
		{"Point", `["x"]`},
		{"QBit(Float32, 2)", `["x"]`},
		{"AggregateFunction(sum, UInt32)", `1`},
		{"String", `"\x"`},
		{"String", `"\u12"`},
	} {
		got, err := decodeAs(t, tt.name, tt.text)
		if err == nil {
			t.Errorf("decoding %s from %s gave %#v, want an error", tt.name, tt.text, got)
		}
	}
}

// TestScanTypesMatchDecode holds D135: the Go type that decode returns is the
// scan type of the column, for each value that is not NULL.
func TestScanTypesMatchDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, text string }{
		{"Bool", `true`}, {"Int8", `1`}, {"UInt16", `1`}, {"UInt64", `1`}, {"Int128", `1`}, {"UInt256", `1`},
		{"Float32", `1.5`}, {"BFloat16", `1.5`}, {"Decimal(10, 2)", `1.5`}, {"String", `"x"`}, {"Enum16('a' = 1)", `"a"`},
		{"FixedString(1)", `"a"`}, {"Date", `"2026-10-07"`}, {"DateTime", `"2026-10-07T00:00:00Z"`},
		{"Time", `"01:02:03"`}, {"IntervalSecond", `3`}, {"UUID", `"61f0c404-5cb3-11e7-907b-a6006ad3dba0"`},
		{"IPv6", `"::1"`}, {"Array(Int8)", `[]`}, {"Tuple(Int8)", `[1]`}, {"Map(String, Int8)", `{}`},
		{"Point", `[1,2]`}, {"Ring", `[[1,2]]`}, {"AggregateFunction(sum, UInt8)", `"\u0001"`}, {"QBit(Float32, 1)", `[1]`},
		{"LowCardinality(String)", `"x"`}, {"Nullable(Int64)", `5`},
	} {
		ty, err := parseType(tt.name)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ty.decode(jsontext.Value(tt.text))
		if err != nil {
			t.Errorf("decoding %s from %s: %v", tt.name, tt.text, err)
			continue
		}
		if g, w := reflect.TypeOf(got), ty.scanType(); g != w {
			t.Errorf("%s decodes to %v, and its scan type is %v (D135)", tt.name, g, w)
		}
	}
}

// TestUnquote holds the reading of a JSON string with bytes that are not UTF-8,
// which jsontext.AppendUnquote cannot give (measured).
func TestUnquote(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want string }{
		{`""`, ""},
		{`"abc"`, "abc"},
		{"\"a\xffb\"", "a\xffb"},
		{`"é"`, "é"},
		{`"\ud800"`, "�"},
		{`"\ud800x"`, "�x"},
		{`"😀"`, "😀"},
		{`"\b\f\n\r\t"`, "\b\f\n\r\t"},
	} {
		got, err := unquote(jsontext.Value(tt.in))
		if err != nil || string(got) != tt.want {
			t.Errorf("unquote(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{``, `"`, `abc`, `"abc`, `"\"`, `"\q"`, `"\u00"`, `"\u00zz"`} {
		if got, err := unquote(jsontext.Value(in)); err == nil {
			t.Errorf("unquote(%q) = %q, want an error", in, got)
		}
	}
}

// TestNullable holds the Can be NULL column of the type table (step 10).
func TestNullable(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"Int8":                           false,
		"Nullable(Int8)":                 true,
		"LowCardinality(Nullable(Int8))": true,
		"Array(Int8)":                    false,
		"Variant(Int8)":                  true,
		"Dynamic":                        true,
		"JSON":                           true,
		"Nullable(Nothing)":              true,
	} {
		ty, err := parseType(name)
		if err != nil {
			t.Fatal(err)
		}
		r := &rows{cols: []*typ{ty}}
		got, ok := r.ColumnTypeNullable(0)
		if !ok || got != want {
			t.Errorf("ColumnTypeNullable of %s = %v, %v, want %v", name, got, ok, want)
		}
	}
}
