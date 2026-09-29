package databend //nolint:testpackage // The tests read the decoder of the driver, which is not exported.

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// TestParseType reads the type names that schema wrote (measured).
func TestParseType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		kind     string
		db       string
		nullable bool
		elems    int
		args     []int
	}{
		{"Int32", "int32", "Int32", false, 0, nil},
		{"Nullable(Decimal(76, 20))", kindDecimal, "Decimal(76, 20)", true, 0, []int{76, 20}},
		{"Nullable(Array(Int32 NULL))", kindArray, "Array(Int32 NULL)", true, 1, nil},
		{"Map(String, Int32 NULL)", kindMap, "Map(String, Int32 NULL)", false, 2, nil},
		{"Tuple(UInt8, String, NULL, Array(UInt8))", kindTuple, "Tuple(UInt8, String, NULL, Array(UInt8))", false, 4, nil},
		{"Tuple(a Int32, b String)", kindTuple, "Tuple(a Int32, b String)", false, 2, nil},
		{"Array(TimestampTz)", kindArray, "Array(TimestampTz)", false, 1, nil},
		{"Timestamp_Tz", kindTimeTZ, "Timestamp_Tz", false, 0, nil},
		{"Vector(3)", kindVector, "Vector(3)", false, 0, []int{3}},
		{"Array(Nothing)", kindArray, "Array(Nothing)", false, 1, nil},
		{"NULL", kindNull, "NULL", false, 0, nil},
	} {
		got, err := parseType(tt.name)
		if err != nil {
			t.Errorf("parseType(%q): %v", tt.name, err)
			continue
		}
		if got.kind != tt.kind || got.name != tt.db || got.nullable != tt.nullable || len(got.elems) != tt.elems || !reflect.DeepEqual(got.args, tt.args) {
			t.Errorf("parseType(%q) = %+v, want the kind %s, the name %s, nullable %v, %d elements and %v", tt.name, got, tt.kind, tt.db, tt.nullable, tt.elems, tt.args)
		}
	}
	for _, name := range []string{"", "Array(", "Decimal(38,", "Map(String Int32)", "Int32 x"} {
		if _, err := parseType(name); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseType(%q) gave %v, want dbimp.ErrInvalidValue", name, err)
		}
	}
}

// TestDecode decodes the text of each kind of value as the server wrote it
// (measured, D118 and D119).
func TestDecode(t *testing.T) {
	t.Parallel()
	f := &format{loc: time.UTC}
	d := func(s string) *apd.Decimal {
		v, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tt := range []struct {
		typ, text string
		want      any
	}{
		{"Int8", "-128", int64(-128)},
		{"UInt64", "18446744073709551615", d("18446744073709551615")},
		{"UInt64", "7", int64(7)},
		{"Float32", "-3.4e+38", -3.4e38},
		{"Float64", "Infinity", nil},
		{"Decimal(76, 20)", "-99999999999999999999999999999999999999999999999999999999.99999999999999999999", d("-99999999999999999999999999999999999999999999999999999999.99999999999999999999")},
		{"Boolean", "1", true},
		{"Binary", "00FF", []byte{0, 0xff}},
		{"Date", "9999-12-31", time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)},
		{"Timestamp", "2026-09-29 12:34:56.123456", time.Date(2026, 9, 29, 12, 34, 56, 123456000, time.UTC)},
		{"Interval", "1 day 2:00:00.000003", "1 day 2:00:00.000003"},
		{"Variant", `{"k":[1,null,"s"],"n":1.5}`, map[string]any{"k": []any{int64(1), nil, "s"}, "n": 1.5}},
		{"Array(Int32 NULL)", "[1,NULL,3]", []any{int64(1), nil, int64(3)}},
		{"Array(Array(UInt8))", "[[1,2],[],[3]]", []any{[]any{int64(1), int64(2)}, []any{}, []any{int64(3)}}},
		{"Array(String NULL)", `["a\"b","c\d","x,y","","NULL",NULL,"q'r"]`, []any{`a"b`, `c\d`, "x,y", "", "NULL", nil, "q'r"}},
		{"Array(Binary)", "[00FF]", []any{[]byte{0, 0xff}}},
		{"Array(Date)", `["2026-09-29"]`, []any{time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)}},
		{"Array(Variant)", `[{"a":1}]`, []any{map[string]any{"a": int64(1)}}},
		{"Array(Geometry)", `["POINT(1 2)","LINESTRING(0 0,1 1)"]`, []any{"POINT(1 2)", "LINESTRING(0 0,1 1)"}},
		{"Array(Nothing)", "[]", []any{}},
		{"Map(String, String NULL)", `{"k\"1":"v,1","k2":NULL}`, map[string]any{`k"1`: "v,1", "k2": nil}},
		{"Map(UInt8, String)", `{1:"a",2:"b"}`, map[any]any{int64(1): "a", int64(2): "b"}},
		{"Map(String, Array(UInt8))", `{"a":[1,2]}`, map[string]any{"a": []any{int64(1), int64(2)}}},
		{"Map(Nothing)", "{}", map[string]any{}},
		{"Tuple(UInt8, String, NULL, Array(UInt8))", `(1,"x",NULL,[1])`, []any{int64(1), "x", nil, []any{int64(1)}}},
		{"Array(Tuple(UInt8, String))", `[(1,"a")]`, []any{[]any{int64(1), "a"}}},
		{"Vector(3)", "[1.5,-2.0,3.0]", []float32{1.5, -2, 3}},
	} {
		ct, err := parseType(tt.typ)
		if err != nil {
			t.Fatal(err)
		}
		got, err := f.decode(ct, tt.text)
		if err != nil {
			t.Errorf("decoding %s %q: %v", tt.typ, tt.text, err)
			continue
		}
		switch w := tt.want.(type) {
		case *apd.Decimal:
			if g, ok := got.(*apd.Decimal); !ok || g.Cmp(w) != 0 {
				t.Errorf("decoding %s %q = %v, want %v", tt.typ, tt.text, got, w)
			}
		case nil:
			if g, ok := got.(float64); !ok || g <= 1e308 {
				t.Errorf("decoding %s %q = %v, want +Inf", tt.typ, tt.text, got)
			}
		default:
			if !reflect.DeepEqual(got, w) {
				t.Errorf("decoding %s %q = %#v, want %#v", tt.typ, tt.text, got, w)
			}
		}
	}
}

// TestDecodeModes decodes the time types in the driver mode, and a Bitmap,
// whose value never arrives (D119).
func TestDecodeModes(t *testing.T) {
	t.Parallel()
	driverMode := &format{driver: true, loc: time.UTC}
	for _, tt := range []struct {
		typ, text string
		want      time.Time
	}{
		{"Date", "2932896", time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)},
		{"Timestamp", "-30610224000000000", time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"Timestamp_Tz", "1790665496123456 19800", time.Date(2026, 9, 29, 7, 4, 56, 123456000, time.UTC)},
	} {
		ct, _ := parseType(tt.typ)
		got, err := driverMode.decode(ct, tt.text)
		g, _ := got.(time.Time)
		if err != nil || !g.Equal(tt.want) {
			t.Errorf("decoding %s %q in the driver mode = %v, %v, want %v", tt.typ, tt.text, got, err, tt.want)
		}
	}
	ct, _ := parseType("Nullable(Bitmap)")
	if _, err := (&format{}).decode(ct, "<bitmap binary>"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("decoding a Bitmap gave %v, want dbimp.ErrNotSupported", err)
	}
	for _, tt := range []struct{ typ, text string }{
		{"Int32", "x"},
		{"Array(Int32)", "[1,2"},
		{"Array(Int32)", "[1] x"},
		{"Tuple(Int32)", "(1,2)"},
		{"Map(String, Int32)", `{"a" 1}`},
		{"Array(String)", `["a`},
	} {
		ct, _ := parseType(tt.typ)
		if _, err := (&format{}).decode(ct, tt.text); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("decoding %s %q gave %v, want dbimp.ErrInvalidValue", tt.typ, tt.text, err)
		}
	}
}
