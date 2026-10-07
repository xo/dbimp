package drill //nolint:testpackage // The decoder and the literals are not exported.

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestParseColumn(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want column
	}{
		{"INT", column{name: "INT"}},
		{"VARCHAR(1)", column{name: "VARCHAR", args: []int64{1}}},
		{"VARCHAR(65535)", column{name: "VARCHAR", args: []int64{65535}}},
		{"VARDECIMAL(38, 18)", column{name: "VARDECIMAL", args: []int64{38, 18}}},
		{"BIGINT(0, 0)", column{name: "BIGINT", args: []int64{0, 0}}},
		{"bigint", column{name: "BIGINT"}},
		{"STRUCT(a)", column{name: "STRUCT(A)"}},
	} {
		if got := parseColumn(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseColumn(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

// TestDecode holds D165 for the forms that the recordings hold, and for the
// values that the driver refuses.
func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		typ, in string
		want    any
	}{
		{"INT", `-2147483648`, int64(math.MinInt32)},
		{"BIGINT", `-9223372036854775808`, int64(math.MinInt64)},
		{"BIGINT", `9223372036854775807`, int64(math.MaxInt64)},
		{"BIGINT(0, 0)", `1`, int64(1)},
		{"FLOAT4", `-3.4028234663852886E38`, -3.4028234663852886e38},
		{"FLOAT4", `1.401298464324817E-45`, 1.401298464324817e-45},
		{"FLOAT8", `-1.7976931348623157E308`, -math.MaxFloat64},
		{"FLOAT8", `"Infinity"`, math.Inf(1)},
		{"FLOAT8", `"-Infinity"`, math.Inf(-1)},
		{"VARDECIMAL(38, 18)", `-12345678901234567890.123456789012345678`, dec(t, "-12345678901234567890.123456789012345678")},
		{"VARDECIMAL(38, 0)", `12345678901234567890123456789012345678`, dec(t, "12345678901234567890123456789012345678")},
		{"VARDECIMAL(38, 18)", `1E-18`, dec(t, "1E-18")},
		{"VARDECIMAL(38, 38)", `-1E-38`, dec(t, "-1E-38")},
		{"BIT", `true`, true},
		{"BIT", `false`, false},
		{"VARCHAR(1)", `""`, ""},
		{"VARCHAR", `"é\"\\"`, "é\"\\"},
		{"VARBINARY", `"YWI="`, []byte("ab")},
		{"VARBINARY", `""`, []byte{}},
		{"DATE", `1790812800000`, dbimp.Date{Year: 2026, Month: 10, Day: 1}},
		{"DATE", `-62135596800000`, dbimp.Date{Year: 1, Month: 1, Day: 1}},
		{"DATE", `253402214400000`, dbimp.Date{Year: 9999, Month: 12, Day: 31}},
		{"TIME", `45296789`, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}},
		{"TIME", `86399999`, dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6}},
		{"TIME", `0`, dbimp.LocalTime{}},
		{"TIMESTAMP", `1790858096789`, dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 10, Day: 1}, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}}},
		{"TIMESTAMP", `-1`, dbimp.LocalDateTime{Date: dbimp.Date{Year: 1969, Month: 12, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6}}},
		{"TIMESTAMP", `253402300799999`, dbimp.LocalDateTime{Date: dbimp.Date{Year: 9999, Month: 12, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6}}},
		{"INTERVALYEAR", `"P1Y2M"`, dbimp.Interval{Months: 14}},
		{"INTERVALYEAR", `"P-1Y-2M"`, dbimp.Interval{Months: -14}},
		{"INTERVALDAY", `"P1DT7384.500S"`, dbimp.Interval{Days: 1, Nanoseconds: 7384500 * 1e6}},
		{"INTERVALDAY", `"P-1DT-7384.500S"`, dbimp.Interval{Days: -1, Nanoseconds: -7384500 * 1e6}},
		{"INTERVAL", `"PT0S"`, dbimp.Interval{}},
		{"INTERVAL", `"P-14M"`, dbimp.Interval{Months: -14}},
		{"MAP", `{"k":[1,2,3],"o":{"s":"x"}}`, map[string]any{"k": []any{int64(1), int64(2), int64(3)}, "o": map[string]any{"s": "x"}}},
		{"MAP", `{}`, map[string]any{}},
		{"LIST", `[[1,2],[3]]`, []any{[]any{int64(1), int64(2)}, []any{int64(3)}}},
		// A list of a scalar type arrives under the name of its element
		// (D165).
		{"BIGINT", `[1,2,3]`, []any{int64(1), int64(2), int64(3)}},
		{"VARCHAR", `["a","b"]`, []any{"a", "b"}},
		{"MAP", `[{"a":1}]`, []any{map[string]any{"a": int64(1)}}},
		{"DATE", `[0]`, []any{dbimp.Date{Year: 1970, Month: 1, Day: 1}}},
		{"VARCHAR", `[]`, []any{}},
		// NULL is nil for every type.
		{"INT", `null`, nil},
		{"VARCHAR", `null`, nil},
		{"VARDECIMAL", `null`, nil},
		{"DATE", `null`, nil},
		{"MAP", `null`, nil},
		{"LIST", `null`, nil},
		// A type that the driver does not know reads as its JSON value.
		{"ANY", `{"x":1.5}`, map[string]any{"x": 1.5}},
		{"UNION", `"u"`, "u"},
	} {
		got, err := decode(parseColumn(tt.typ), jsontext.Value(tt.in))
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%s, %s) = %#v, %v, want %#v", tt.typ, tt.in, got, err, tt.want)
		}
	}
	nan, err := decode(column{name: "FLOAT8"}, jsontext.Value(`"NaN"`))
	if f, ok := nan.(float64); err != nil || !ok || !math.IsNaN(f) {
		t.Errorf("decode of \"NaN\" = %v, %v, want NaN", nan, err)
	}
	for _, tt := range []struct{ typ, in string }{
		{"INT", `1.5`},
		{"INT", `"1"`},
		{"BIGINT", `9223372036854775808`},
		{"FLOAT8", `"infinity"`},
		{"FLOAT8", `true`},
		{"VARDECIMAL", `true`},
		{"BIT", `1`},
		{"VARCHAR", `1`},
		{"VARBINARY", `"!!"`},
		{"VARBINARY", `1`},
		{"DATE", `"2026-10-01"`},
		{"DATE", `1.5`},
		{"TIME", `-1`},
		{"TIME", `86400000`},
		{"TIMESTAMP", `"2026-10-01 12:34:56"`},
		{"INTERVAL", `"1 day"`},
		{"INTERVAL", `1`},
		{"BIGINT", `[1,"x"]`},
	} {
		if got, err := decode(parseColumn(tt.typ), jsontext.Value(tt.in)); err == nil {
			t.Errorf("decode(%s, %s) = %#v, want an error", tt.typ, tt.in, got)
		}
	}
}

func TestScanType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		typ  string
		list bool
		want reflect.Type
	}{
		{"BIGINT", false, typeOfInt64},
		{"BIGINT", true, typeOfAny},
		{"VARDECIMAL(38, 18)", false, typeOfDecimal},
		{"MAP", false, typeOfMap},
		{"LIST", false, typeOfList},
		{"UNION", false, typeOfAny},
	} {
		if got := scanType(parseColumn(tt.typ), tt.list); got != tt.want {
			t.Errorf("scanType(%s, %v) = %v, want %v", tt.typ, tt.list, got, tt.want)
		}
	}
}

// TestLiteral holds D165 for each Go type of an argument: the literal that
// the driver writes.
func TestLiteral(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   any
		want string
	}{
		{nil, `NULL`},
		{true, `TRUE`},
		{false, `FALSE`},
		{int64(7), `7`},
		{int64(math.MinInt32), `-2147483648`},
		{int64(math.MaxInt32) + 1, `CAST(2147483648 AS BIGINT)`},
		{int64(math.MinInt64), `CAST('-9223372036854775808' AS BIGINT)`},
		{int64(math.MaxInt64), `CAST(9223372036854775807 AS BIGINT)`},
		{0.1, `CAST('0.1' AS DOUBLE)`},
		{math.MaxFloat64, `CAST('1.7976931348623157e+308' AS DOUBLE)`},
		{math.NaN(), `CAST('NaN' AS DOUBLE)`},
		{math.Inf(-1), `CAST('-Infinity' AS DOUBLE)`},
		{"é'x\\", `'é''x\'`},
		{"", `''`},
		{[]byte{0, 'a', 0xff}, `binary_string('\x00\x61\xff')`},
		{[]byte{}, `binary_string('')`},
		{time.Date(2026, 10, 1, 19, 34, 56, 789e6, time.FixedZone("x", 7*3600)), `TIMESTAMP '2026-10-01 12:34:56.789'`},
		{dbimp.Date{Year: 2026, Month: 10, Day: 1}, `DATE '2026-10-01'`},
		{dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3, Nanosecond: 4e6}, `TIME '01:02:03.004'`},
		{dbimp.LocalDateTime{Date: dbimp.Date{Year: 1, Month: 1, Day: 1}}, `TIMESTAMP '0001-01-01 00:00:00.000'`},
		{dec(t, "-12345678901234567890.0123456789"), `CAST('-12345678901234567890.0123456789' AS DECIMAL(30, 10))`},
		{dec(t, "1E+3"), `CAST('1000' AS DECIMAL(4, 0))`},
		{dec(t, "0.00"), `CAST('0.00' AS DECIMAL(2, 2))`},
		{dbimp.Interval{Months: 14}, `INTERVAL '1-2' YEAR TO MONTH`},
		{dbimp.Interval{Months: -14}, `INTERVAL '-1-2' YEAR TO MONTH`},
		{dbimp.Interval{Days: 1, Nanoseconds: 7384500 * 1e6}, `INTERVAL '1 02:03:04.500' DAY TO SECOND`},
		{dbimp.Interval{Days: -1, Nanoseconds: -7384500 * 1e6}, `INTERVAL '-1 02:03:04.500' DAY TO SECOND`},
		{dbimp.Interval{}, `INTERVAL '0 00:00:00.000' DAY TO SECOND`},
	} {
		got, err := literal(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("literal(%#v) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		in   any
		want error
	}{
		{dec(t, "NaN"), dbimp.ErrInvalidValue},
		{dec(t, "1E+38"), dbimp.ErrInvalidValue},
		{dec(t, "1E-39"), dbimp.ErrInvalidValue},
		{dbimp.Date{Year: 0, Month: 1, Day: 1}, dbimp.ErrInvalidValue},
		{dbimp.Date{Year: 10000, Month: 1, Day: 1}, dbimp.ErrInvalidValue},
		{dbimp.LocalTime{Nanosecond: 1}, dbimp.ErrInvalidValue},
		{dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 10, Day: 1}, Time: dbimp.LocalTime{Nanosecond: 1000}}, dbimp.ErrInvalidValue},
		{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), dbimp.ErrInvalidValue},
		{dbimp.Interval{Months: 1, Days: 1}, dbimp.ErrInvalidValue},
		{dbimp.Interval{Nanoseconds: 1}, dbimp.ErrInvalidValue},
		{dbimp.OffsetTime{}, dbimp.ErrNotSupported},
		{[]any{1}, dbimp.ErrNotSupported},
		{int32(1), dbimp.ErrNotSupported},
		{map[string]any{}, dbimp.ErrNotSupported},
	} {
		if got, err := literal(tt.in); !errors.Is(err, tt.want) {
			t.Errorf("literal(%#v) = %q, %v, want %v", tt.in, got, err, tt.want)
		}
	}
}

func TestBindQuery(t *testing.T) {
	t.Parallel()
	args := func(vals ...any) []driver.NamedValue {
		out := make([]driver.NamedValue, len(vals))
		for i, v := range vals {
			out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
		}
		return out
	}
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  string
	}{
		{"SELECT 1", nil, "SELECT 1"},
		// With no argument, the statement goes as it is.
		{"SELECT '?', ?", nil, "SELECT '?', ?"},
		{"SELECT ?, ?", args(int64(1), "a'b"), "SELECT 1, 'a''b'"},
		{"SELECT '?', \"?\", `?`, ? -- ?\n, /* ? */ ?", args(int64(1), int64(2)), "SELECT '?', \"?\", `?`, 1 -- ?\n, /* ? */ 2"},
		{"SELECT @a, @b, @a", []driver.NamedValue{{Name: "a", Value: int64(1)}, {Name: "b", Value: "x"}}, "SELECT 1, 'x', 1"},
	} {
		got, err := bindQuery(tt.query, tt.args)
		if err != nil || got != tt.want {
			t.Errorf("bindQuery(%q) = %q, %v, want %q", tt.query, got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  error
	}{
		{"SELECT ?, ?", args(int64(1)), dbimp.ErrArguments},
		{"SELECT ?", args(int64(1), int64(2)), dbimp.ErrArguments},
		{"SELECT @a", args(int64(1)), dbimp.ErrArguments},
		{"SELECT 'x", args(int64(1)), dbimp.ErrUnterminated},
		{"SELECT ?", args([]any{}), dbimp.ErrNotSupported},
	} {
		if got, err := bindQuery(tt.query, tt.args); !errors.Is(err, tt.want) {
			t.Errorf("bindQuery(%q) = %q, %v, want %v", tt.query, got, err, tt.want)
		}
	}
}
