package databend //nolint:testpackage // The tests read the decoder of the driver, which is not exported.

import (
	"encoding/json/jsontext"
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
		{"UInt64", "18446744073709551615", uint64(18446744073709551615)},
		{"UInt64", "7", uint64(7)},
		{"Float32", "-3.4e+38", -3.4e38},
		{"Float64", "Infinity", nil},
		{"Decimal(76, 20)", "-99999999999999999999999999999999999999999999999999999999.99999999999999999999", d("-99999999999999999999999999999999999999999999999999999999.99999999999999999999")},
		{"Boolean", "1", true},
		{"Binary", "00FF", []byte{0, 0xff}},
		{"Date", "9999-12-31", dbimp.Date{Year: 9999, Month: 12, Day: 31}},
		{"Timestamp", "2026-09-29 12:34:56.123456", time.Date(2026, 9, 29, 12, 34, 56, 123456000, time.UTC)},
		{"Interval", "1 day 2:00:00.000003", dbimp.Interval{Days: 1, Nanoseconds: 2*3600e9 + 3000}},
		{"Interval", "1 year 2 months 3 days 4:05:06.5", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: 14706e9 + 5e8}},
		{"Interval", "-1 month -2 days", dbimp.Interval{Months: -1, Days: -2}},
		{"Interval", "-0:00:00.000001", dbimp.Interval{Nanoseconds: -1000}},
		{"Interval", "00:00:00", dbimp.Interval{}},
		{"Interval", "100 days 25:00:00", dbimp.Interval{Days: 100, Nanoseconds: 25 * 3600e9}},
		{"Interval", "178956970 years 7 months 2562047:47:16.854775", dbimp.Interval{Months: 2147483647, Nanoseconds: 9223372036854775000}},
		{"Variant", `{"k":[1,null,"s"],"n":1.5}`, map[string]any{"k": []any{int64(1), nil, "s"}, "n": 1.5}},
		{"Array(Int32 NULL)", "[1,NULL,3]", []any{int64(1), nil, int64(3)}},
		{"Array(Array(UInt8))", "[[1,2],[],[3]]", []any{[]any{int64(1), int64(2)}, []any{}, []any{int64(3)}}},
		{"Array(String NULL)", `["a\"b","c\d","x,y","","NULL",NULL,"q'r"]`, []any{`a"b`, `c\d`, "x,y", "", "NULL", nil, "q'r"}},
		{"Array(Binary)", "[00FF]", []any{[]byte{0, 0xff}}},
		{"Array(Date)", `["2026-09-29"]`, []any{dbimp.Date{Year: 2026, Month: 9, Day: 29}}},
		{"Array(Variant)", `[{"a":1}]`, []any{map[string]any{"a": int64(1)}}},
		{"Array(Geometry)", `["POINT(1 2)","LINESTRING(0 0,1 1)"]`, []any{"POINT(1 2)", "LINESTRING(0 0,1 1)"}},
		{"Array(Nothing)", "[]", []any{}},
		{"Map(String, String NULL)", `{"k\"1":"v,1","k2":NULL}`, map[string]any{`k"1`: "v,1", "k2": nil}},
		{"Map(UInt8, String)", `{1:"a",2:"b"}`, map[any]any{int64(1): "a", int64(2): "b"}},
		{"Map(String, Array(UInt8))", `{"a":[1,2]}`, map[string]any{"a": []any{int64(1), int64(2)}}},
		{"Map(Nothing)", "{}", map[string]any{}},
		{"Tuple(UInt8, String, NULL, Array(UInt8))", `(1,"x",NULL,[1])`, []any{int64(1), "x", nil, []any{int64(1)}}},
		{"Array(Tuple(UInt8, String))", `[(1,"a")]`, []any{[]any{int64(1), "a"}}},
		{"Vector(3)", "[1.5,-2.0,3.0]", dbimp.Vector[float32]{1.5, -2, 3}},
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
	ct, _ := parseType("Date")
	if got, err := driverMode.decode(ct, "2932896"); err != nil || got != (dbimp.Date{Year: 9999, Month: 12, Day: 31}) {
		t.Errorf("decoding a Date in the driver mode = %v, %v, want 9999-12-31", got, err)
	}
	for _, tt := range []struct {
		typ, text string
		want      time.Time
	}{
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
	ct, _ = parseType("Nullable(Bitmap)")
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

// TestDecodeGeometry holds D135 for each geometry_output_format, with the
// texts that the server wrote for a Geometry, an Array(Geometry) and a
// Tuple(UInt8, Geometry) (measured on 1.2.948).
func TestDecodeGeometry(t *testing.T) {
	t.Parallel()
	point := map[string]any{"type": "Point", "coordinates": []any{int64(1), int64(2)}}
	wkb := []byte{0x01, 0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0, 0, 0, 0, 0, 0, 0, 0x40}
	for _, tt := range []struct {
		format            string
		top, array, tuple string
		want              any
		scan              reflect.Type
	}{
		{"WKT", "POINT(1 2)", `["POINT(1 2)"]`, `(1,"POINT(1 2)")`, "POINT(1 2)", reflect.TypeFor[string]()},
		{"EWKT", "POINT(1 2)", `["POINT(1 2)"]`, `(1,"POINT(1 2)")`, "POINT(1 2)", reflect.TypeFor[string]()},
		{"GeoJSON", `{"type": "Point", "coordinates": [1,2]}`, `[{"type": "Point", "coordinates": [1,2]}]`, `(1,{"type": "Point", "coordinates": [1,2]})`, point, reflect.TypeFor[any]()},
		{"WKB", "0101000000000000000000F03F0000000000000040", `["0101000000000000000000F03F0000000000000040"]`, `(1,"0101000000000000000000F03F0000000000000040")`, wkb, reflect.TypeFor[[]byte]()},
	} {
		f, err := session{"settings": jsontext.Value(`{"geometry_output_format":"` + tt.format + `"}`)}.format(time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			typ, text string
			want      any
		}{
			{"Geometry", tt.top, tt.want},
			{"Geography", tt.top, tt.want},
			{"Array(Geometry)", tt.array, []any{tt.want}},
			{"Tuple(UInt8, Geometry)", tt.tuple, []any{int64(1), tt.want}},
		} {
			ct, err := parseType(c.typ)
			if err != nil {
				t.Fatal(err)
			}
			got, err := f.decode(ct, c.text)
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("%s: %s %s gave %#v, %v, want %#v", tt.format, c.typ, c.text, got, err, c.want)
			}
		}
		if got := f.geoType(); got != tt.scan {
			t.Errorf("%s: the scan type is %v, want %v", tt.format, got, tt.scan)
		}
	}
}

// TestInterval holds that an Interval argument is the text that the server
// reads back as the same interval, and that a fraction of a microsecond is
// refused, because the server drops it (measured on 1.2.948).
func TestIntervalArgument(t *testing.T) {
	t.Parallel()
	for _, iv := range []dbimp.Interval{
		{},
		{Months: -14, Days: -3, Nanoseconds: -1000},
		{Months: 1, Days: 2, Nanoseconds: 3500e6},
		{Nanoseconds: -100 * 3600e9},
		{Months: 2147483647, Nanoseconds: 9223372036854775000},
	} {
		text, err := formatInterval(iv)
		if err != nil {
			t.Errorf("formatInterval(%v): %v", iv, err)
			continue
		}
		if back, err := parseInterval(text); err != nil || back != iv {
			t.Errorf("%v writes %q, which reads back as %v, %v", iv, text, back, err)
		}
	}
	if _, err := formatInterval(dbimp.Interval{Nanoseconds: 1}); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("a fraction of a microsecond gave %v, want dbimp.ErrInvalidValue", err)
	}
	for _, s := range []string{"", "1", "1 week", "1 day 1:00", "1:00:00 1 day", "0:60:00", "9999999999999:00:00"} {
		if _, err := parseInterval(s); err == nil {
			t.Errorf("parseInterval(%q) gave no error", s)
		}
	}
}
