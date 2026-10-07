package elasticsearch //nolint:testpackage // The decoder is not exported.

import (
	"encoding/json/jsontext"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestDecode holds D167 for the forms that the recordings hold, and for the
// values that the driver refuses.
func TestDecode(t *testing.T) {
	t.Parallel()
	kolkata := time.FixedZone("", 5*3600+1800)
	for _, tt := range []struct {
		typ, in string
		want    any
	}{
		{"boolean", `true`, true},
		{"byte", `-128`, int64(-128)},
		{"short", `32767`, int64(32767)},
		{"integer", `-2147483648`, int64(math.MinInt64 >> 32)},
		{"integer", `100.0`, int64(100)},
		{"integer", `0.0`, int64(0)},
		{"long", `9223372036854775807`, int64(math.MaxInt64)},
		{"long", `-9223372036854775808`, int64(math.MinInt64)},
		{"unsigned_long", `18446744073709551615`, uint64(math.MaxUint64)},
		{"unsigned_long", `0`, uint64(0)},
		{"unsigned_long", `7`, uint64(7)},
		{"half_float", `65504.0`, 65504.0},
		{"float", `3.4028235E38`, 3.4028235e38},
		{"float", `-1.4E-45`, -1.4e-45},
		{"double", `1.7976931348623157E308`, math.MaxFloat64},
		{"double", `4.9E-324`, math.SmallestNonzeroFloat64},
		{"double", `"Infinity"`, math.Inf(1)},
		{"double", `"-Infinity"`, math.Inf(-1)},
		{"scaled_float", `1234.57`, 1234.57},
		{"keyword", `"é'\"\\ x"`, "é'\"\\ x"},
		{"keyword", `""`, ""},
		{"text", `"Some text"`, "Some text"},
		{"binary", `"AP8="`, []byte{0, 255}},
		{"binary", `""`, []byte{}},
		{"ip", `"2001:db8::1"`, "2001:db8::1"},
		{"version", `"1.2.3-beta"`, "1.2.3-beta"},
		{"datetime", `"2026-10-01T07:04:56.789Z"`, time.Date(2026, 10, 1, 7, 4, 56, 789e6, time.UTC)},
		{"datetime", `"2026-10-01T12:34:56.123456789Z"`, time.Date(2026, 10, 1, 12, 34, 56, 123456789, time.UTC)},
		{"datetime", `"2026-10-01T12:34:56.789+05:30"`, time.Date(2026, 10, 1, 12, 34, 56, 789e6, kolkata)},
		{"datetime", `"9999-12-31T23:59:59.999Z"`, time.Date(9999, 12, 31, 23, 59, 59, 999e6, time.UTC)},
		{"date", `"2026-10-01T00:00:00.000Z"`, dbimp.Date{Year: 2026, Month: 10, Day: 1}},
		{"date", `"2026-10-01T00:00:00.000+05:30"`, dbimp.Date{Year: 2026, Month: 10, Day: 1}},
		{"date", `"0001-01-01T00:00:00.000Z"`, dbimp.Date{Year: 1, Month: 1, Day: 1}},
		{"date", `"-0001-01-01T00:00:00.000Z"`, dbimp.Date{Year: -1, Month: 1, Day: 1}},
		{"time", `"12:34:56.789Z"`, dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}}},
		{"time", `"12:34:56.789+05:30"`, dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}, Offset: 19800}},
		{"geo_point", `"POINT (-71.34 41.12)"`, "POINT (-71.34 41.12)"},
		{"geo_shape", `"POLYGON ((0 0, 1 0, 1 1, 0 0))"`, "POLYGON ((0 0, 1 0, 1 1, 0 0))"},
		{"shape", `"POINT (1 2)"`, "POINT (1 2)"},
		{"interval_year", `"P1Y"`, dbimp.Interval{Months: 12}},
		{"interval_month", `"P1M"`, dbimp.Interval{Months: 1}},
		{"interval_year_to_month", `"P1Y2M"`, dbimp.Interval{Months: 14}},
		{"interval_year_to_month", `"P-1Y-2M"`, dbimp.Interval{Months: -14}},
		{"interval_day", `"PT48H"`, 48 * time.Hour},
		{"interval_hour", `"PT3H"`, 3 * time.Hour},
		{"interval_minute", `"PT3M"`, 3 * time.Minute},
		{"interval_second", `"PT4.5S"`, 4500 * time.Millisecond},
		{"interval_day_to_hour", `"PT26H"`, 26 * time.Hour},
		{"interval_day_to_minute", `"PT26H3M"`, 26*time.Hour + 3*time.Minute},
		{"interval_day_to_second", `"PT26H3M4.5S"`, 26*time.Hour + 3*time.Minute + 4500*time.Millisecond},
		{"interval_day_to_second", `"PT-24H"`, -24 * time.Hour},
		{"interval_hour_to_minute", `"PT2H3M"`, 2*time.Hour + 3*time.Minute},
		{"interval_hour_to_second", `"PT2H3M4S"`, 2*time.Hour + 3*time.Minute + 4*time.Second},
		{"interval_minute_to_second", `"PT3M4S"`, 3*time.Minute + 4*time.Second},
		{"null", `null`, nil},
		{"long", `null`, nil},
		{"geo_point", `null`, nil},
		// A type that the driver does not know is the decoded JSON value.
		{"unknown_type", `{"x":[1,"a",null]}`, map[string]any{"x": []any{int64(1), "a", nil}}},
	} {
		got, err := decode(tt.typ, jsontext.Value(tt.in))
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%s, %s) = %#v, %v, want %#v", tt.typ, tt.in, got, err, tt.want)
		}
	}
	nan, err := decode("double", jsontext.Value(`"NaN"`))
	if f, ok := nan.(float64); err != nil || !ok || !math.IsNaN(f) {
		t.Errorf("decode of \"NaN\" = %v, %v, want NaN", nan, err)
	}
	// The time zone of a datetime keeps its offset, and no time.Local.
	got, err := decode("datetime", jsontext.Value(`"2026-10-01T12:34:56.789+05:30"`))
	if tm, ok := got.(time.Time); err != nil || !ok || tm.Location() == time.Local {
		t.Errorf("decode of a datetime with an offset = %v, %v, want a fixed zone", got, err)
	}
	for _, tt := range []struct{ typ, in string }{
		{"integer", `100.5`},
		{"integer", `1e3`},
		{"integer", `"1"`},
		{"long", `9223372036854775808`},
		{"unsigned_long", `18446744073709551616`},
		{"unsigned_long", `-1`},
		{"unsigned_long", `"1"`},
		{"double", `"infinity"`},
		{"double", `"1"`},
		{"boolean", `1`},
		{"keyword", `1`},
		{"binary", `"!!"`},
		{"binary", `1`},
		{"datetime", `"2026-10-01 12:34:56"`},
		{"datetime", `1790858096789`},
		{"date", `"2026-10-01"`},
		{"date", `"x-10-01T00:00:00.000Z"`},
		{"time", `"12:34:56"`},
		{"interval_year", `"P1D"`},
		{"interval_year", `1`},
		{"interval_day", `"P2D"`},
		{"interval_day", `"P1M"`},
		{"interval_day", `"PT1X"`},
	} {
		if got, err := decode(tt.typ, jsontext.Value(tt.in)); err == nil {
			t.Errorf("decode(%s, %s) = %#v, want an error", tt.typ, tt.in, got)
		}
	}
}

// TestScanType holds D135: the scan type of a column is the Go type of its
// values, and any for a type that the driver does not know.
func TestScanType(t *testing.T) {
	t.Parallel()
	for typ, want := range map[string]reflect.Type{
		"null":                      typeOfAny,
		"boolean":                   typeOfBool,
		"long":                      typeOfInt64,
		"unsigned_long":             typeOfUint64,
		"scaled_float":              typeOfFloat64,
		"ip":                        typeOfString,
		"binary":                    typeOfBytes,
		"datetime":                  typeOfTime,
		"date":                      typeOfDate,
		"time":                      typeOfClock,
		"interval_year":             typeOfInterval,
		"interval_year_to_month":    typeOfInterval,
		"interval_day":              typeOfDuration,
		"interval_minute_to_second": typeOfDuration,
		"unknown_type":              typeOfAny,
	} {
		if got := scanType(typ); got != want {
			t.Errorf("scanType(%s) = %v, want %v", typ, got, want)
		}
	}
}
