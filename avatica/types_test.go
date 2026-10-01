package avatica //nolint:testpackage // These tests read the decoder and the encoder of values, which are not exported.

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

func TestParseInterval(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, text string
		want       dbimp.Interval
	}{
		{"INTERVAL DAY TO SECOND", "1 02:03:04.500000", dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute + 4500*time.Millisecond)}},
		{"INTERVAL DAY TO SECOND", "-1 02:03:04.5", dbimp.Interval{Days: -1, Nanoseconds: -int64(2*time.Hour + 3*time.Minute + 4500*time.Millisecond)}},
		{"INTERVAL DAY TO SECOND", "0 00:00:00.000001", dbimp.Interval{Nanoseconds: 1000}},
		{"INTERVAL YEAR TO MONTH", "1-02", dbimp.Interval{Months: 14}},
		{"INTERVAL YEAR TO MONTH", "-1-02", dbimp.Interval{Months: -14}},
		{"INTERVAL YEAR", "3", dbimp.Interval{Months: 36}},
		{"INTERVAL MONTH", "7", dbimp.Interval{Months: 7}},
		{"INTERVAL DAY", "10", dbimp.Interval{Days: 10}},
		{"INTERVAL HOUR TO MINUTE", "5:06", dbimp.Interval{Nanoseconds: int64(5*time.Hour + 6*time.Minute)}},
		{"INTERVAL MINUTE TO SECOND(3)", "2:03.250", dbimp.Interval{Nanoseconds: int64(2*time.Minute + 3250*time.Millisecond)}},
		{"INTERVAL SECOND", "1.5", dbimp.Interval{Nanoseconds: int64(1500 * time.Millisecond)}},
	} {
		got, err := parseInterval(tt.name, tt.text)
		if err != nil || got != tt.want {
			t.Errorf("parseInterval(%q, %q) = %+v, %v, want %+v", tt.name, tt.text, got, err, tt.want)
		}
	}
	for _, tt := range []struct{ name, text string }{
		{"INTERVAL DAY TO SECOND", "1 02:03"},
		{"INTERVAL DAY TO SECOND", "x 02:03:04"},
		{"INTERVAL DAY TO SECOND", "1 02:03:+4"},
		{"INTERVAL MONTH TO YEAR", "1-02"},
		{"INTERVAL FORTNIGHT", "1"},
		{"VARCHAR", "1"},
		{"INTERVAL DAY", "1-2"},
	} {
		if _, err := parseInterval(tt.name, tt.text); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseInterval(%q, %q) gave %v, want dbimp.ErrInvalidValue", tt.name, tt.text, err)
		}
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()
	scalar := func(name string) colType { return colType{Type: "scalar", Name: name} }
	for _, tt := range []struct {
		typ  colType
		v    string
		want any
	}{
		{scalar("INTEGER"), `null`, nil},
		{scalar("UNSIGNED_LONG"), `9223372036854775807`, int64(math.MaxInt64)},
		{scalar("DOUBLE"), `"-Infinity"`, math.Inf(-1)},
		{scalar("FLOAT"), `3.3999999521443642E38`, 3.3999999521443642e38},
		{scalar("CHARACTER"), `"ab "`, "ab "},
		{scalar("CHARACTER VARYING"), `"x"`, "x"},
		{scalar("NUMERIC"), `1.50`, apd.New(150, -2)},
		{scalar("DATE"), `-1`, dbimp.Date{Year: 1969, Month: time.December, Day: 31}},
		{scalar("TIME"), `45296789`, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}},
		{scalar("TIMESTAMP"), `-1`, dbimp.LocalDateTime{Date: dbimp.Date{Year: 1969, Month: time.December, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999000000}}},
		{scalar("BINARY"), `"AP8="`, []byte{0, 0xff}},
		{scalar("UUID"), `"6ba7b810-9dad-11d1-80b4-00c04fd430c8"`, uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")},
		{colType{Type: "array", Name: "VARCHAR ARRAY", Component: &colType{Type: "scalar", Name: "VARCHAR"}}, `["a",null]`, []any{"a", nil}},
		{colType{Type: "array", Name: "INTEGER ARRAY"}, `[1]`, []any{int64(1)}},
		{scalar("GEOMETRY"), `"POINT (1 2)"`, "POINT (1 2)"},
		{scalar("OTHER"), `12`, int64(12)},
	} {
		got, err := decode(tt.typ, jsontext.Value(tt.v))
		if d, ok := tt.want.(*apd.Decimal); ok {
			if g, gok := got.(*apd.Decimal); !gok || err != nil || g.Cmp(d) != 0 {
				t.Errorf("decode(%s, %s) = %v, %v, want %v", tt.typ.Name, tt.v, got, err, tt.want)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%s, %s) = %#v, %v, want %#v", tt.typ.Name, tt.v, got, err, tt.want)
		}
	}
	if got, err := decode(scalar("DOUBLE"), jsontext.Value(`"NaN"`)); err != nil || !isNaN(got) {
		t.Errorf(`decode(DOUBLE, "NaN") = %v, %v, want NaN`, got, err)
	}
	for _, tt := range []struct {
		typ colType
		v   string
	}{
		{scalar("DOUBLE"), `"infinity"`},
		{scalar("BINARY"), `"not base64!"`},
		{scalar("UUID"), `"not a uuid"`},
		{colType{Type: "array", Name: "INTEGER ARRAY"}, `{"a":1}`},
		{scalar("INTERVAL DAY"), `"1 02"`},
	} {
		if _, err := decode(tt.typ, jsontext.Value(tt.v)); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("decode(%s, %s) gave %v, want dbimp.ErrInvalidValue", tt.typ.Name, tt.v, err)
		}
	}
}

// isNaN reports whether v is a float64 that is NaN.
func isNaN(v any) bool {
	f, ok := v.(float64)
	return ok && math.IsNaN(f)
}

// TestArg holds D158: each Go type goes as the TypedValue whose rep the
// server binds.
func TestArg(t *testing.T) {
	t.Parallel()
	day := dbimp.Date{Year: 2026, Month: time.October, Day: 1}
	for _, tt := range []struct {
		v    any
		want string
	}{
		{nil, `{"type":"NULL","value":null}`},
		{int64(-1), `{"type":"LONG","value":-1}`},
		{uint64(math.MaxInt64), `{"type":"LONG","value":9223372036854775807}`},
		{1.5, `{"type":"DOUBLE","value":1.5}`},
		{true, `{"type":"BOOLEAN","value":true}`},
		{"é", `{"type":"STRING","value":"é"}`},
		{[]byte{0, 0xff}, `{"type":"BYTE_STRING","value":"AP8="}`},
		{apd.New(12345678901234567, -30), `{"type":"STRING","value":"0.000000000000012345678901234567"}`},
		{(*apd.Decimal)(nil), `{"type":"NULL","value":null}`},
		{uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), `{"type":"STRING","value":"6ba7b810-9dad-11d1-80b4-00c04fd430c8"}`},
		{day, `{"type":"JAVA_SQL_DATE","value":20727}`},
		{dbimp.Date{Year: 1969, Month: time.December, Day: 31}, `{"type":"JAVA_SQL_DATE","value":-1}`},
		{dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789999999}, `{"type":"JAVA_SQL_TIME","value":45296789}`},
		{dbimp.LocalDateTime{Date: day, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123000000}}, `{"type":"JAVA_SQL_TIMESTAMP","value":1790858096123}`},
		{time.Date(2026, time.October, 1, 18, 4, 56, 123000000, time.FixedZone("", 19800)), `{"type":"JAVA_SQL_TIMESTAMP","value":1790858096123}`},
	} {
		tv, err := arg(tt.v)
		if err != nil {
			t.Errorf("arg(%#v): %v", tt.v, err)
			continue
		}
		b, err := json.Marshal(tv)
		if err != nil || string(b) != tt.want {
			t.Errorf("arg(%#v) = %s, %v, want %s", tt.v, b, err, tt.want)
		}
	}
	for _, v := range []any{uint64(math.MaxInt64) + 1, math.NaN(), math.Inf(1), struct{}{}} {
		if _, err := arg(v); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("arg(%#v) gave %v, want dbimp.ErrInvalidValue", v, err)
		}
	}
}

// TestASCII holds D158: each character outside ASCII becomes an escape of
// JSON, and the JSON keeps its meaning.
func TestASCII(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`{"a":"x"}`:       `{"a":"x"}`,
		`{"a":"é"}`:       `{"a":"\u00e9"}`,
		`{"a":"€\u0000"}`: `{"a":"\u20ac\u0000"}`,
		`{"a":"😀"}`:       `{"a":"\ud83d\ude00"}`,
	} {
		got := ascii([]byte(in))
		if string(got) != want {
			t.Errorf("ascii(%s) = %s, want %s", in, got, want)
		}
		var a, b any
		if err := json.Unmarshal([]byte(in), &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(got, &b); err != nil || !reflect.DeepEqual(a, b) {
			t.Errorf("ascii(%s) = %s, which reads as %v, %v, want %v", in, got, b, err, a)
		}
	}
}

// FuzzASCII holds that ascii writes only ASCII, and keeps the meaning of any
// JSON string.
func FuzzASCII(f *testing.F) {
	for _, s := range []string{"x", "é'\"\\ x", "😀€", " "} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		b, err := json.Marshal(s)
		if err != nil {
			return
		}
		got := ascii(b)
		for _, c := range got {
			if c >= 0x80 {
				t.Fatalf("ascii(%s) = %s, which holds a byte outside ASCII", b, got)
			}
		}
		var back string
		if err := json.Unmarshal(got, &back); err != nil || back != s {
			t.Fatalf("ascii(%s) = %s, which reads as %q, %v, want %q", b, got, back, err, s)
		}
	})
}
