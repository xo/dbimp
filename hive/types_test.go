package hive //nolint:testpackage // The test reads the decoders of the driver, which are not exported.

import (
	"testing"
	"time"

	"github.com/xo/dbimp"
)

func TestDecodeZoned(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want time.Time
	}{
		{"2026-01-02 03:04:05.0 GMT", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{"2026-01-02 03:04:05.0 Etc/UTC", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		{"2026-01-02 03:04:05.123456789 UTC", time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)},
		{"2026-01-02 03:04:05 America/New_York", time.Date(2026, 1, 2, 8, 4, 5, 0, time.UTC)},
	} {
		got, err := decodeZoned(tt.in)
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("decodeZoned(%q) = %v, %v, want %v", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"", "2026-01-02 03:04:05", "x y z", "2026-01-02 03:04:05.0 Nowhere/Land"} {
		if _, err := decodeZoned(in); err == nil {
			t.Errorf("decodeZoned(%q) took it, want an error", in)
		}
	}
}

func TestDecodeIntervals(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		year bool
		want dbimp.Interval
	}{
		{"1-2", true, dbimp.Interval{Months: 14}},
		{"-1-2", true, dbimp.Interval{Months: -14}},
		{"0-0", true, dbimp.Interval{}},
		{"1 02:03:04.500000000", false, dbimp.Interval{Days: 1, Nanoseconds: ((2*60+3)*60+4)*1e9 + 5e8}},
		{"-1 02:03:04.5", false, dbimp.Interval{Days: -1, Nanoseconds: -(((2*60+3)*60+4)*1e9 + 5e8)}},
		{"0 00:00:00", false, dbimp.Interval{}},
	} {
		var got dbimp.Interval
		var err error
		if tt.year {
			got, err = decodeIntervalYM(tt.in)
		} else {
			got, err = decodeIntervalDT(tt.in)
		}
		if err != nil || got != tt.want {
			t.Errorf("decoding %q = %+v, %v, want %+v", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"", "x", "1", "1-x"} {
		if _, err := decodeIntervalYM(in); err == nil {
			t.Errorf("decodeIntervalYM(%q) took it, want an error", in)
		}
	}
	for _, in := range []string{"", "x", "1", "1 2:3", "a b:c:d"} {
		if _, err := decodeIntervalDT(in); err == nil {
			t.Errorf("decodeIntervalDT(%q) took it, want an error", in)
		}
	}
}

func TestDecodeDecimalKeepsTheScale(t *testing.T) {
	t.Parallel()
	d, err := decodeDecimal("1.5", 2)
	if err != nil || d.String() != "1.50" {
		t.Errorf("decodeDecimal(1.5, 2) = %v, %v, want 1.50 (D228)", d, err)
	}
	if _, err := decodeDecimal("x", 2); err == nil {
		t.Error("decodeDecimal(x) took it, want an error")
	}
}
