package influxdb //nolint:testpackage // These tests read the decoders and the encoder of arguments, which are not exported.

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

func TestDecodeInfluxQL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in     string
		isTime bool
		want   any
	}{
		{`null`, false, nil},
		{`0`, false, int64(0)},
		{`-9223372036854775808`, false, int64(math.MinInt64)},
		{`9223372036854775808`, false, uint64(1 << 63)},
		{`18446744073709551615`, false, uint64(math.MaxUint64)},
		{`1.5`, false, 1.5},
		{`1e3`, false, 1000.0},
		{`"text"`, false, "text"},
		{`true`, false, true},
		{`"2023-11-14T22:13:20Z"`, true, time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)},
		{`"2023-11-14T22:13:20Z"`, false, "2023-11-14T22:13:20Z"},
		{`1700000000000`, true, int64(1700000000000)},
	} {
		got, err := decodeInfluxQL(jsontext.Value(tt.in), tt.isTime)
		if err != nil {
			t.Errorf("decodeInfluxQL(%s): %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decodeInfluxQL(%s) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
	if _, err := decodeInfluxQL(jsontext.Value(`"yesterday"`), true); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("a time that does not parse gave %v, want dbimp.ErrInvalidValue", err)
	}
}

func TestColumnType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		arrow string
		in    string
		want  any
	}{
		{"Int64", `-1`, int64(-1)},
		{"UInt64", `18446744073709551615`, uint64(math.MaxUint64)},
		{"Float32", `1.5`, 1.5},
		{"Float64", `"inf"`, math.Inf(1)},
		{"Float64", `"Infinity"`, math.Inf(1)},
		{"Float64", `"-inf"`, math.Inf(-1)},
		{"Float64", `"-Infinity"`, math.Inf(-1)},
		{"Utf8", `"s"`, "s"},
		{"Dictionary(Int32, Utf8)", `"a"`, "a"},
		{"Boolean", `false`, false},
		{"Timestamp(ns)", `"2024-01-02T03:04:05.1"`, time.Date(2024, 1, 2, 3, 4, 5, 1e8, time.UTC)},
		{"Timestamp(ns, \"+09:00\")", `"2024-01-02T03:04:05+09:00"`, time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 9*3600))},
		{"Date32", `"2024-01-02"`, dbimp.Date{Year: 2024, Month: 1, Day: 2}},
		{"Date64", `"2024-01-02T00:00:00"`, dbimp.Date{Year: 2024, Month: 1, Day: 2}},
		{"Binary", `"6162"`, []byte("ab")},
		{"Null", `1`, int64(1)},
		{"Time64(ns)", `"12:30:00"`, dbimp.LocalTime{Hour: 12, Minute: 30}},
		{"Time32(ms)", `"12:30:00.500"`, dbimp.LocalTime{Hour: 12, Minute: 30, Nanosecond: 5e8}},
		{"Interval(MonthDayNano)", `"14 mons 3 days 4 hours 5 mins 6.500000000 secs"`, dbimp.Interval{Months: 14, Days: 3, Nanoseconds: 14706e9 + 5e8}},
		{"Interval(MonthDayNano)", `"-1 mons -2 days"`, dbimp.Interval{Months: -1, Days: -2}},
		{"Interval(MonthDayNano)", `"0.000000001 secs"`, dbimp.Interval{Nanoseconds: 1}},
		{"Interval(MonthDayNano)", `""`, dbimp.Interval{}},
		{"Interval(MonthDayNano)", `"-3 hours"`, dbimp.Interval{Nanoseconds: -3 * 3600e9}},
		{"List(Int64)", `[1]`, []any{int64(1)}},
	} {
		got, err := columnType(tt.arrow, true).decode(jsontext.Value(tt.in))
		if err != nil {
			t.Errorf("%s: %s: %v", tt.arrow, tt.in, err)
			continue
		}
		if tm, ok := got.(time.Time); ok {
			if want, ok := tt.want.(time.Time); !ok || !tm.Equal(want) {
				t.Errorf("%s: %s = %v, want %v", tt.arrow, tt.in, got, tt.want)
			}
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %s = %#v, want %#v", tt.arrow, tt.in, got, tt.want)
		}
	}
	for _, in := range []string{`"NaN"`, `"nan"`} {
		got, err := columnType("Float64", false).decode(jsontext.Value(in))
		if f, ok := got.(float64); err != nil || !ok || !math.IsNaN(f) {
			t.Errorf("Float64: %s = %v, %v, want NaN", in, got, err)
		}
	}
	ct := columnType("Decimal128(38, 9)", false)
	if ct.precision != 38 || ct.scale != 9 || ct.scanType() != reflect.TypeFor[*apd.Decimal]() {
		t.Errorf("a decimal has %d, %d and %v, want 38, 9 and *apd.Decimal", ct.precision, ct.scale, ct.scanType())
	}
}

func TestParams(t *testing.T) {
	t.Parallel()
	d, _, err := apd.NewFromString("12345678901234567890.123456789")
	if err != nil {
		t.Fatal(err)
	}
	got, err := params([]driver.NamedValue{
		{Name: "a", Value: int64(1)},
		{Ordinal: 2, Value: uint64(math.MaxUint64)},
		{Name: "c", Value: d},
		{Name: "t", Value: time.Date(2024, 1, 2, 3, 4, 5, 6, time.UTC)},
		{Name: "n", Value: nil},
		{Name: "s", Value: `quote " here`},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":1,"2":18446744073709551615,"c":12345678901234567890.123456789,"t":"2024-01-02T03:04:05.000000006Z","n":null,"s":"quote \" here"}`
	if string(got) != want {
		t.Errorf("params = %s, want %s", got, want)
	}
	for _, v := range []any{[]byte("x"), math.NaN(), math.Inf(1), struct{}{}} {
		if _, err := params([]driver.NamedValue{{Name: "x", Value: v}}); err == nil {
			t.Errorf("params took %#v, which JSON or InfluxDB cannot hold", v)
		}
	}
}

func TestCheckNamedValue(t *testing.T) {
	t.Parallel()
	c := &conn{}
	d := apd.New(15, -1)
	for _, tt := range []struct {
		in, want any
		err      error
	}{
		{uint64(math.MaxUint64), uint64(math.MaxUint64), nil},
		{d, d, nil},
		{*d, d, nil},
		{(*apd.Decimal)(nil), nil, nil},
		{"s", "s", driver.ErrSkip},
	} {
		nv := driver.NamedValue{Value: tt.in}
		err := c.CheckNamedValue(&nv)
		if !errors.Is(err, tt.err) {
			t.Errorf("CheckNamedValue(%#v) = %v, want %v", tt.in, err, tt.err)
			continue
		}
		if !reflect.DeepEqual(nv.Value, tt.want) {
			t.Errorf("CheckNamedValue(%#v) left %#v, want %#v", tt.in, nv.Value, tt.want)
		}
	}
}

// TestParseDuration holds D135 for a Duration, with the texts that the
// server wrote (measured on 3.11.5).
func TestParseDuration(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want time.Duration
	}{
		{"PT1.5S", 1500 * time.Millisecond},
		{"PT90S", 90 * time.Second},
		{"-PT2.5S", -2500 * time.Millisecond},
		{"PT0.000007S", 7 * time.Microsecond},
		{"PT864000S", 240 * time.Hour},
		{"P0D", 0},
		{"PT9223372036.854775807S", math.MaxInt64},
		{"-PT9223372036.854775807S", -math.MaxInt64},
	} {
		got, ok := parseDuration(tt.in)
		if !ok || got != tt.want {
			t.Errorf("parseDuration(%q) = %v, %v, want %v", tt.in, got, ok, tt.want)
		}
	}
	for _, in := range []string{"", "PT", "PTS", "PT1", "P1D", "PT1.0000000001S", "PT9223372037S", "1s", "PT-1S"} {
		if got, ok := parseDuration(in); ok {
			t.Errorf("parseDuration(%q) = %v, want a failure", in, got)
		}
	}
	d, err := columnType("Duration(ns)", true).decode(jsontext.Value(`"PT1.5S"`))
	if err != nil || d != 1500*time.Millisecond {
		t.Errorf("a Duration(ns) decoded as %#v, %v", d, err)
	}
	if st := columnType("Duration(s)", true).scanType(); st != reflect.TypeFor[time.Duration]() {
		t.Errorf("the scan type of a Duration is %v", st)
	}
}

// TestIntervalArgument holds that an Interval argument is the text that the
// server casts, which the driver reads back as the same interval (measured
// on 3.11.5).
func TestIntervalArgument(t *testing.T) {
	t.Parallel()
	for _, iv := range []dbimp.Interval{
		{}, {Months: 14, Days: -3, Nanoseconds: -1000}, {Months: 1, Days: 2, Nanoseconds: 3500e6},
		{Nanoseconds: math.MaxInt64}, {Nanoseconds: math.MinInt64},
	} {
		text := formatInterval(iv)
		if back, ok := parseInterval(text); !ok || back != iv {
			t.Errorf("%v writes %q, which reads back as %v, %v", iv, text, back, ok)
		}
	}
	for _, s := range []string{"1", "1 week", "1.5 days", "1.5 nanoseconds", "x secs"} {
		if iv, ok := parseInterval(s); ok {
			t.Errorf("parseInterval(%q) = %v, want a failure", s, iv)
		}
	}
}
