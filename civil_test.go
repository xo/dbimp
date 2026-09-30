package dbimp_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

func TestDate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		d    dbimp.Date
		text string
	}{
		{dbimp.Date{Year: 2026, Month: 9, Day: 30}, "2026-09-30"},
		{dbimp.Date{Year: 1, Month: 1, Day: 1}, "0001-01-01"},
		{dbimp.Date{Year: 0, Month: 2, Day: 29}, "0000-02-29"},
		{dbimp.Date{Year: -44, Month: 3, Day: 15}, "-0044-03-15"},
		{dbimp.Date{Year: 9999, Month: 12, Day: 31}, "9999-12-31"},
		{dbimp.Date{Year: 10000, Month: 1, Day: 1}, "+10000-01-01"},
		{dbimp.Date{Year: -999999999, Month: 1, Day: 1}, "-999999999-01-01"},
	} {
		if got := tt.d.String(); got != tt.text {
			t.Errorf("%#v.String() = %q, want %q", tt.d, got, tt.text)
		}
		got, err := dbimp.ParseDate(tt.text)
		if err != nil || got != tt.d {
			t.Errorf("ParseDate(%q) = %#v, %v, want %#v", tt.text, got, err, tt.d)
		}
		if v, err := tt.d.Value(); err != nil || v != tt.text {
			t.Errorf("%#v.Value() = %v, %v", tt.d, v, err)
		}
	}
	for _, s := range []string{"", "2026-9-30", "2026-02-30", "2026-13-01", "10000-01-01", "+2026-01-01", "2026-09-30T00:00", "2026/09/30", "-44-03-15"} {
		if _, err := dbimp.ParseDate(s); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("ParseDate(%q) = %v, want dbimp.ErrInvalidValue", s, err)
		}
	}
	d := dbimp.Date{Year: 2026, Month: 9, Day: 30}
	loc := time.FixedZone("x", -7*3600)
	if got := d.In(loc); !got.Equal(time.Date(2026, 9, 30, 0, 0, 0, 0, loc)) {
		t.Errorf("In = %v", got)
	}
	if got := dbimp.DateOf(time.Date(2026, 9, 30, 23, 59, 0, 0, loc)); got != d {
		t.Errorf("DateOf = %#v, want the date in the location of the time", got)
	}
}

func TestLocalTime(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		lt    dbimp.LocalTime
		text  string
		parse []string
	}{
		{dbimp.LocalTime{Hour: 12, Minute: 30}, "12:30:00", []string{"12:30"}},
		{dbimp.LocalTime{Hour: 12, Minute: 30, Nanosecond: 5e8}, "12:30:00.5", []string{"12:30:00.500000000"}},
		{dbimp.LocalTime{}, "00:00:00", nil},
		{dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}, "23:59:59.999999999", nil},
		{dbimp.LocalTime{Second: 1, Nanosecond: 1}, "00:00:01.000000001", nil},
	} {
		if got := tt.lt.String(); got != tt.text {
			t.Errorf("%#v.String() = %q, want %q", tt.lt, got, tt.text)
		}
		for _, s := range append([]string{tt.text}, tt.parse...) {
			got, err := dbimp.ParseLocalTime(s)
			if err != nil || got != tt.lt {
				t.Errorf("ParseLocalTime(%q) = %#v, %v, want %#v", s, got, err, tt.lt)
			}
		}
	}
	for _, s := range []string{"", "24:00:00", "12:60", "12:30:60", "12", "12:3", "12:30:00.", "12:30:00.1234567890", "12:30:00Z", "12:30:00+01:00"} {
		if _, err := dbimp.ParseLocalTime(s); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("ParseLocalTime(%q) = %v, want dbimp.ErrInvalidValue", s, err)
		}
	}
	lt := dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3, Nanosecond: 4}
	if got := lt.In(time.UTC); !got.Equal(time.Date(0, 1, 1, 1, 2, 3, 4, time.UTC)) {
		t.Errorf("In = %v, want the time on 0000-01-01", got)
	}
	if got := dbimp.LocalTimeOf(time.Date(2026, 1, 1, 1, 2, 3, 4, time.UTC)); got != lt {
		t.Errorf("LocalTimeOf = %#v", got)
	}
}

func TestLocalDateTime(t *testing.T) {
	t.Parallel()
	dt := dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 9, Day: 30}, Time: dbimp.LocalTime{Hour: 12, Minute: 30, Nanosecond: 5e8}}
	if got := dt.String(); got != "2026-09-30T12:30:00.5" {
		t.Errorf("String = %q", got)
	}
	for _, s := range []string{"2026-09-30T12:30:00.5", "2026-09-30 12:30:00.500"} {
		if got, err := dbimp.ParseLocalDateTime(s); err != nil || got != dt {
			t.Errorf("ParseLocalDateTime(%q) = %#v, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "2026-09-30", "2026-09-30T", "2026-09-30T12:30:00Z", "2026-02-30T00:00:00", "2026-09-30T24:00:00", "2026-09-30t12:30"} {
		if _, err := dbimp.ParseLocalDateTime(s); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("ParseLocalDateTime(%q) = %v, want dbimp.ErrInvalidValue", s, err)
		}
	}
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skip(err)
	}
	if got := dt.In(oslo); got.Hour() != 12 || got.Location() != oslo {
		t.Errorf("In = %v, want 12:30 in Europe/Oslo", got)
	}
	if got := dbimp.LocalDateTimeOf(dt.In(oslo)); got != dt {
		t.Errorf("LocalDateTimeOf = %#v", got)
	}
}

func TestInterval(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		iv    dbimp.Interval
		text  string
		parse []string
	}{
		{dbimp.Interval{}, "PT0S", []string{"P0D", "PT0.0S"}},
		{dbimp.Interval{Months: 1, Days: 2, Nanoseconds: 3500 * int64(time.Millisecond)}, "P1M2DT3.5S", nil},
		{dbimp.Interval{Months: 14, Days: 3, Nanoseconds: int64(4*time.Hour + 5*time.Minute + 6*time.Second)}, "P1Y2M3DT4H5M6S", nil},
		{dbimp.Interval{Days: 14}, "P14D", []string{"P2W"}},
		{dbimp.Interval{Months: -14}, "P-1Y-2M", []string{"-P1Y2M"}},
		{dbimp.Interval{Nanoseconds: -1500 * int64(time.Millisecond)}, "PT-1.5S", []string{"-PT1.5S"}},
		{dbimp.Interval{Nanoseconds: -int64(time.Hour + time.Nanosecond)}, "PT-1H-0.000000001S", nil},
		{dbimp.Interval{Nanoseconds: 1}, "PT0.000000001S", nil},
		{dbimp.Interval{Months: math.MaxInt32, Days: math.MinInt32, Nanoseconds: math.MaxInt64}, "P178956970Y7M-2147483648DT2562047H47M16.854775807S", nil},
		{dbimp.Interval{Nanoseconds: math.MinInt64}, "PT-2562047H-47M-16.854775808S", nil},
	} {
		if got := tt.iv.String(); got != tt.text {
			t.Errorf("%#v.String() = %q, want %q", tt.iv, got, tt.text)
		}
		for _, s := range append([]string{tt.text}, tt.parse...) {
			got, err := dbimp.ParseInterval(s)
			if err != nil || got != tt.iv {
				t.Errorf("ParseInterval(%q) = %#v, %v, want %#v", s, got, err, tt.iv)
			}
		}
	}
	for _, s := range []string{"", "P", "PT", "P1", "1D", "P1S", "P1D1Y", "PT1S1H", "P1M1M", "PT1D", "P1.5D", "PT1.S", "PT1.0000000001S", "P1DT", "PT1H2", "P2147483648M", "P2147483648D", "PT2562048H", "P1TT1S", "-P-2147483648M"} {
		if _, err := dbimp.ParseInterval(s); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("ParseInterval(%q) = %v, want dbimp.ErrInvalidValue", s, err)
		}
	}
}

// TestAssignCivil holds D138: a scan into the type itself, or its sql.Null,
// keeps the value, a scan into a time.Time gets it in UTC, and a scan into a
// string gets the text of String.
func TestAssignCivil(t *testing.T) {
	t.Parallel()
	d := dbimp.Date{Year: 2026, Month: 9, Day: 30}
	lt := dbimp.LocalTime{Hour: 12, Minute: 30}
	dt := dbimp.LocalDateTime{Date: d, Time: lt}
	iv := dbimp.Interval{Months: 1, Days: 2}

	var gd dbimp.Date
	var nd sql.Null[dbimp.Date]
	var gt time.Time
	var nt sql.Null[time.Time]
	var s string
	var ns sql.Null[string]
	var b []byte
	var giv dbimp.Interval
	for _, tt := range []struct {
		dest, src any
		check     func() bool
	}{
		{&gd, d, func() bool { return gd == d }},
		{&nd, d, func() bool { return nd.Valid && nd.V == d }},
		{&gt, d, func() bool { return gt.Equal(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)) }},
		{&nt, dt, func() bool { return nt.Valid && nt.V.Equal(time.Date(2026, 9, 30, 12, 30, 0, 0, time.UTC)) }},
		{&gt, lt, func() bool { return gt.Equal(time.Date(0, 1, 1, 12, 30, 0, 0, time.UTC)) }},
		{&s, dt, func() bool { return s == "2026-09-30T12:30:00" }},
		{&ns, lt, func() bool { return ns.Valid && ns.V == "12:30:00" }},
		{&b, d, func() bool { return string(b) == "2026-09-30" }},
		{&s, iv, func() bool { return s == "P1M2D" }},
		{&giv, iv, func() bool { return giv == iv }},
	} {
		if err := dbimp.Assign(driver.ScanContext{}, tt.dest, tt.src); err != nil || !tt.check() {
			t.Errorf("Assign(%T, %#v) gave %v", tt.dest, tt.src, err)
		}
	}
	var f float64
	if err := dbimp.Assign(driver.ScanContext{}, &f, iv); err == nil {
		t.Errorf("Assign of an Interval into a float64 gave no error")
	}
}

func TestOffsetTime(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		ot    dbimp.OffsetTime
		text  string
		parse []string
	}{
		{dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 30}}, "12:30:00Z", []string{"12:30Z", "12:30:00+00:00"}},
		{dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 30, Nanosecond: 5e8}, Offset: 3600}, "12:30:00.5+01:00", nil},
		{dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59}, Offset: -(5*3600 + 30*60 + 15)}, "23:59:59-05:30:15", nil},
	} {
		if got := tt.ot.String(); got != tt.text {
			t.Errorf("%#v.String() = %q, want %q", tt.ot, got, tt.text)
		}
		for _, s := range append([]string{tt.text}, tt.parse...) {
			if got, err := dbimp.ParseOffsetTime(s); err != nil || got != tt.ot {
				t.Errorf("ParseOffsetTime(%q) = %#v, %v, want %#v", s, got, err, tt.ot)
			}
		}
	}
	for _, s := range []string{"", "12:30", "12:30:00", "12:30:00+1:00", "12:30:00+01", "12:30:00+24:00", "12:30:00+01:60", "24:00:00Z", "12:30:00z"} {
		if _, err := dbimp.ParseOffsetTime(s); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("ParseOffsetTime(%q) = %v, want dbimp.ErrInvalidValue", s, err)
		}
	}
	ot := dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3}, Offset: 3600}
	got := ot.ToTime()
	if _, off := got.Zone(); off != 3600 || got.Hour() != 1 || got.Year() != 0 {
		t.Errorf("ToTime = %v, want 01:02:03 on 0000-01-01 in +01:00", got)
	}
	if back := dbimp.OffsetTimeOf(got); back != ot {
		t.Errorf("OffsetTimeOf = %#v, want %#v", back, ot)
	}
	var gt time.Time
	if err := dbimp.Assign(driver.ScanContext{}, &gt, ot); err != nil || !gt.Equal(got) {
		t.Errorf("Assign of an OffsetTime into a time.Time gave %v, %v", gt, err)
	}
	var s string
	if err := dbimp.Assign(driver.ScanContext{}, &s, ot); err != nil || s != "01:02:03+01:00" {
		t.Errorf("Assign of an OffsetTime into a string gave %q, %v", s, err)
	}
}

func TestVector(t *testing.T) {
	t.Parallel()
	f := dbimp.Vector[float32]{1.5, -2, 0.1}
	if got := f.String(); got != "[1.5,-2,0.1]" {
		t.Errorf("String = %q", got)
	}
	if v, err := (dbimp.Vector[int8]{1, -128}).Value(); err != nil || v != "[1,-128]" {
		t.Errorf("Value = %v, %v", v, err)
	}
	var slice []float32
	if err := dbimp.Assign(driver.ScanContext{}, &slice, f); err != nil || len(slice) != 3 || slice[2] != 0.1 {
		t.Errorf("Assign of a Vector into a []float32 gave %v, %v", slice, err)
	}
	var back dbimp.Vector[float32]
	if err := dbimp.Assign(driver.ScanContext{}, &back, f); err != nil || len(back) != 3 {
		t.Errorf("Assign of a Vector into a Vector gave %v, %v", back, err)
	}
}
