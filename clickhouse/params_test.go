package clickhouse //nolint:testpackage // The tests bind arguments with the function of the driver, which is not exported.

import (
	"database/sql/driver"
	"errors"
	"math"
	"math/big"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// args numbers the values as database/sql does.
func args(values ...any) []driver.NamedValue {
	out := make([]driver.NamedValue, len(values))
	for i, v := range values {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

// TestBindEachType holds D176: each Go type is a typed parameter, {pN:Type} in the
// statement and param_pN in the query string, with the text that the server reads
// as the format TabSeparated (measured).
func TestBindEachType(t *testing.T) {
	t.Parallel()
	d, _, err := apd.NewFromString("-12.340")
	if err != nil {
		t.Fatal(err)
	}
	huge, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
	for _, tt := range []struct {
		name  string
		value any
		kind  string
		text  string
	}{
		{"nil", nil, "Nullable(Nothing)", `\N`},
		{"bool", true, "Bool", "true"},
		{"int64", int64(-5), "Int64", "-5"},
		{"uint64", uint64(math.MaxUint64), "UInt64", "18446744073709551615"},
		{"float64", 1.5, "Float64", "1.5"},
		{"nan", math.NaN(), "Float64", "nan"},
		{"inf", math.Inf(-1), "Float64", "-inf"},
		{"string", "a'b", "String", "a'b"},
		{"string with tab, newline and backslash", "a\tb\nc\\d\re\x00", "String", `a\tb\nc\\d\re\0`},
		{"the text of a NULL", `\N`, "String", `\\N`},
		{"bytes", []byte{0xff, 'a'}, "String", "\xffa"},
		{"time", time.Date(2026, 10, 7, 12, 34, 56, 789, time.FixedZone("x", 7*3600)), "DateTime64(9, 'UTC')", "2026-10-07 05:34:56.000000789"},
		{"date", dbimp.Date{Year: 2026, Month: 10, Day: 7}, "Date32", "2026-10-07"},
		{"uuid", uuid.MustParse("61f0c404-5cb3-11e7-907b-a6006ad3dba0"), "UUID", "61f0c404-5cb3-11e7-907b-a6006ad3dba0"},
		{"IPv4", netip.MustParseAddr("1.2.3.4"), "IPv4", "1.2.3.4"},
		{"IPv6", netip.MustParseAddr("2001:db8::1"), "IPv6", "2001:db8::1"},
		{"IPv4 in IPv6", netip.MustParseAddr("::ffff:1.2.3.4"), "IPv6", "::ffff:1.2.3.4"},
		{"decimal", d, "Decimal(76, 3)", "-12.340"},
		{"big", big.NewInt(-5), "Int256", "-5"},
		{"unsigned big", huge, "UInt256", huge.String()},
		{"strings", []string{"a", "b'c", "d\te"}, "Array(String)", `['a','b\'c','d\te']`},
		{"ints", []int64{1, -2}, "Array(Int64)", "[1,-2]"},
		{"floats", []float64{1.5, math.Inf(1)}, "Array(Float64)", "[1.5,inf]"},
		{"bools", []bool{true, false}, "Array(Bool)", "[true,false]"},
		{"empty list", []any{}, "Array(Nothing)", "[]"},
		{"list with NULL", []any{int64(1), nil}, "Array(Nullable(Int64))", "[1,NULL]"},
		{"list of NULL", []any{nil}, "Array(Nullable(Nothing))", "[NULL]"},
		{"list of lists", []any{[]string{"a"}, []string{}}, "Array(Array(String))", "[['a'],[]]"},
		{"map", map[string]any{"b": int64(2), "a'": int64(1)}, "Map(String, Int64)", `{'a\'':1,'b':2}`},
		{"empty map", map[string]any{}, "Map(String, Nothing)", "{}"},
	} {
		stmt, params, err := bind("SELECT ?", args(tt.value))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if want := "SELECT {p1:" + tt.kind + "}"; stmt != want {
			t.Errorf("%s: the statement is %q, want %q", tt.name, stmt, want)
		}
		if got := params.Get("param_p1"); got != tt.text || len(params) != 1 {
			t.Errorf("%s: the parameters are %v, want param_p1=%q", tt.name, params, tt.text)
		}
	}
}

// TestBindPlaceholders holds the placeholders: ? and @name, where they are and
// where they are not.
func TestBindPlaceholders(t *testing.T) {
	t.Parallel()
	named := func(name string, v any) driver.NamedValue { return driver.NamedValue{Name: name, Value: v} }
	for _, tt := range []struct {
		name  string
		query string
		args  []driver.NamedValue
		want  string
		keys  map[string]string
	}{
		{"no arguments, no change", "SELECT a ? b : c, '?'", nil, "SELECT a ? b : c, '?'", nil},
		{"two", "SELECT ?, ?", args(int64(1), "x"), "SELECT {p1:Int64}, {p2:String}", map[string]string{"param_p1": "1", "param_p2": "x"}},
		{"in a string", "SELECT '?', ?, \"?\", `?`, 'it''s ?', 'a\\' ?'", args(int64(1)), "SELECT '?', {p1:Int64}, \"?\", `?`, 'it''s ?', 'a\\' ?'", map[string]string{"param_p1": "1"}},
		{"in comments", "SELECT ? -- ?\n, /* ? */ ? # ?\n, ?", args(int64(1), int64(2), int64(3)), "SELECT {p1:Int64} -- ?\n, /* ? */ {p2:Int64} # ?\n, {p3:Int64}", map[string]string{"param_p1": "1", "param_p2": "2", "param_p3": "3"}},
		{"named", "SELECT @b, @a, @b", []driver.NamedValue{named("a", int64(1)), named("b", "x")}, "SELECT {p1:String}, {p2:Int64}, {p1:String}", map[string]string{"param_p1": "x", "param_p2": "1"}},
		{"named and positional", "SELECT ?, @a, ?", []driver.NamedValue{named("a", int64(1)), {Value: "x"}, {Value: "y"}}, "SELECT {p1:String}, {p2:Int64}, {p3:String}", map[string]string{"param_p1": "x", "param_p2": "1", "param_p3": "y"}},
		{"the version variable", "SELECT @@x, ?", args(int64(1)), "SELECT @@x, {p1:Int64}", map[string]string{"param_p1": "1"}},
	} {
		got, params, err := bind(tt.query, tt.args)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: the statement is %q, want %q", tt.name, got, tt.want)
		}
		keys := map[string]string{}
		for k, v := range params {
			keys[k] = v[0]
		}
		if len(tt.keys) > 0 && !reflect.DeepEqual(keys, tt.keys) {
			t.Errorf("%s: the parameters are %v, want %v", tt.name, keys, tt.keys)
		}
	}
}

// TestBindRefuses holds that a missing argument, an argument left over, a
// statement that ends in a comment that never closes, and a Go type that the
// server has no parameter for are each an error.
func TestBindRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		args  []driver.NamedValue
		want  error
	}{
		{"missing", "SELECT ?, ?", args(int64(1)), dbimp.ErrArguments},
		{"left over", "SELECT ?", args(int64(1), int64(2)), dbimp.ErrArguments},
		{"no placeholder", "SELECT 1", args(int64(1)), dbimp.ErrArguments},
		{"missing name", "SELECT @a", []driver.NamedValue{{Name: "b", Value: int64(1)}}, dbimp.ErrArguments},
		{"name left over", "SELECT 1", []driver.NamedValue{{Name: "b", Value: int64(1)}}, dbimp.ErrArguments},
		{"comment", "SELECT ? /* ?", args(int64(1)), dbimp.ErrUnterminated},
		{"a struct", "SELECT ?", args(struct{}{}), dbimp.ErrNotSupported},
		{"mixed list", "SELECT ?", args([]any{int64(1), "a"}), dbimp.ErrInvalidValue},
		{"mixed map", "SELECT ?", args(map[string]any{"a": int64(1), "b": "a"}), dbimp.ErrInvalidValue},
		{"a decimal with no value", "SELECT ?", args(apd.New(0, 0).Set(&apd.Decimal{Form: apd.NaN})), dbimp.ErrInvalidValue},
		{"too many bits", "SELECT ?", args(new(big.Int).Lsh(big.NewInt(1), 256)), dbimp.ErrInvalidValue},
		{"a decimal of 77 digits", "SELECT ?", args(mustDecimal(t, "1"+strings.Repeat("0", 76))), dbimp.ErrInvalidValue},
	} {
		_, _, err := bind(tt.query, tt.args)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", tt.name, err, tt.want)
		}
	}
}

func mustDecimal(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestParameterValuesEncode holds that the query string carries every byte of a
// parameter, including the ones that are not UTF-8.
func TestParameterValuesEncode(t *testing.T) {
	t.Parallel()
	_, params, err := bind("SELECT ?", args("a&b=c d\xff\n"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := url.ParseQuery(params.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := back.Get("param_p1"), `a&b=c d`+"\xff"+`\n`; got != want {
		t.Errorf("the parameter came back as %q, want %q", got, want)
	}
}

// TestCheckNamedValue holds the arguments that the driver keeps for itself and
// the ones that it hands to database/sql (W4).
func TestCheckNamedValue(t *testing.T) {
	t.Parallel()
	cn := &conn{}
	for _, v := range []any{
		uint64(math.MaxUint64), apd.New(1, 0), big.NewInt(1), dbimp.Date{Year: 2026, Month: 1, Day: 1},
		uuid.New(), netip.MustParseAddr("::1"), []any{1}, []string{"a"}, []int64{1}, []float64{1}, []bool{true}, map[string]any{},
		WithTimeout(time.Second),
	} {
		nv := driver.NamedValue{Value: v}
		if err := cn.CheckNamedValue(&nv); err != nil {
			t.Errorf("CheckNamedValue(%T) = %v, want nil", v, err)
		}
	}
	for _, v := range []any{int64(1), "a", 1.5, true, []byte("a"), time.Now(), nil, int32(1), uint8(1)} {
		nv := driver.NamedValue{Value: v}
		if err := cn.CheckNamedValue(&nv); !errors.Is(err, driver.ErrSkip) {
			t.Errorf("CheckNamedValue(%T) = %v, want driver.ErrSkip, so that database/sql converts it (W4)", v, err)
		}
	}
	// A decimal that is a value, and a nil pointer, are changed.
	nv := driver.NamedValue{Value: *apd.New(1, 0)}
	if err := cn.CheckNamedValue(&nv); err != nil {
		t.Errorf("CheckNamedValue of an apd.Decimal = %v", err)
	}
	if _, ok := nv.Value.(*apd.Decimal); !ok {
		t.Errorf("an apd.Decimal became %T, want *apd.Decimal", nv.Value)
	}
	var nilDec *apd.Decimal
	nv = driver.NamedValue{Value: nilDec}
	if err := cn.CheckNamedValue(&nv); err != nil || nv.Value != nil {
		t.Errorf("CheckNamedValue of a nil *apd.Decimal = %v, %v, want nil and nil", nv.Value, err)
	}
}
