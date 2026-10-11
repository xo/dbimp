package exasol

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The text of an interval is the one that the server writes (D206 and
// docs/EXASOL.md, Types). INTERVAL YEAR TO MONTH is a sign, the years, a
// hyphen and two digits of months, such as +000000005-03. INTERVAL DAY TO
// SECOND is a sign, the days, a space, and the time with a fraction, such as
// +000000002 03:04:05.678000000. The number of digits of the years and of the
// days follows the precision of the column.

// nanosPerDay is the length of a day in nanoseconds, which an interval of the
// server holds without a leap second.
const nanosPerDay = int64(24 * 60 * 60 * 1e9)

// parseIntervalYM reads an INTERVAL YEAR TO MONTH. A value of more than
// 2147483647 months does not fit dbimp.Interval, and its error says so (D206).
func parseIntervalYM(s string) (dbimp.Interval, error) {
	neg, body, ok := cutSign(s)
	years, months, found := strings.Cut(body, "-")
	if !ok || !found {
		return dbimp.Interval{}, intervalError(s)
	}
	y, err := strconv.ParseInt(years, 10, 64)
	m, err2 := strconv.ParseInt(months, 10, 64)
	if err != nil || err2 != nil || y < 0 || m < 0 || m > 11 {
		return dbimp.Interval{}, intervalError(s)
	}
	total := y*12 + m
	if neg {
		total = -total
	}
	if total > math.MaxInt32 || total < math.MinInt32 {
		return dbimp.Interval{}, fmt.Errorf("reading the interval %q: %d months do not fit the Months of dbimp.Interval, an int32: %w", s, total, dbimp.ErrInvalidValue)
	}
	return dbimp.Interval{Months: int32(total)}, nil
}

// parseIntervalDS reads an INTERVAL DAY TO SECOND.
func parseIntervalDS(s string) (dbimp.Interval, error) {
	neg, body, ok := cutSign(s)
	days, clock, found := strings.Cut(body, " ")
	if !ok || !found {
		return dbimp.Interval{}, intervalError(s)
	}
	d, err := strconv.ParseInt(days, 10, 64)
	if err != nil || d < 0 || d > math.MaxInt32 {
		return dbimp.Interval{}, intervalError(s)
	}
	t, err := dbimp.ParseLocalTime(clock)
	if err != nil {
		return dbimp.Interval{}, intervalError(s)
	}
	nanos := ((int64(t.Hour)*60+int64(t.Minute))*60+int64(t.Second))*1e9 + int64(t.Nanosecond)
	if neg {
		d, nanos = -d, -nanos
	}
	return dbimp.Interval{Days: int32(d), Nanoseconds: nanos}, nil
}

// cutSign cuts the sign of an interval, which the server always writes.
func cutSign(s string) (bool, string, bool) {
	switch {
	case strings.HasPrefix(s, "+"):
		return false, s[1:], true
	case strings.HasPrefix(s, "-"):
		return true, s[1:], true
	}
	return false, s, false
}

func intervalError(s string) error {
	return fmt.Errorf("reading the interval %q: %w", s, dbimp.ErrInvalidValue)
}

// formatIntervalYM writes iv as an INTERVAL YEAR(precision) TO MONTH. It is an
// error for an interval with days or time, which the type cannot hold.
func formatIntervalYM(iv dbimp.Interval, precision int) (string, error) {
	if iv.Days != 0 || iv.Nanoseconds != 0 {
		return "", fmt.Errorf("writing the interval %s: INTERVAL YEAR TO MONTH holds months only: %w", iv, dbimp.ErrInvalidValue)
	}
	months := int64(iv.Months)
	sign := "+"
	if months < 0 {
		sign, months = "-", -months
	}
	return fmt.Sprintf("%s%0*d-%02d", sign, max(precision, 1), months/12, months%12), nil
}

// formatIntervalDS writes iv as an INTERVAL DAY(precision) TO SECOND(fraction).
// The days and the time can have different signs in iv, and the text has one
// sign, so the function carries a day into the time first. It is an error for
// an interval with months, which the type cannot hold.
func formatIntervalDS(iv dbimp.Interval, precision, fraction int) (string, error) {
	if iv.Months != 0 {
		return "", fmt.Errorf("writing the interval %s: INTERVAL DAY TO SECOND holds days and time only: %w", iv, dbimp.ErrInvalidValue)
	}
	days, nanos := int64(iv.Days)+iv.Nanoseconds/nanosPerDay, iv.Nanoseconds%nanosPerDay
	switch {
	case days > 0 && nanos < 0:
		days, nanos = days-1, nanos+nanosPerDay
	case days < 0 && nanos > 0:
		days, nanos = days+1, nanos-nanosPerDay
	}
	sign := "+"
	if days < 0 || nanos < 0 {
		sign, days, nanos = "-", -days, -nanos
	}
	fraction = min(fraction, 9)
	secs := nanos / 1e9
	out := fmt.Sprintf("%s%0*d %02d:%02d:%02d", sign, max(precision, 1), days, secs/3600, secs%3600/60, secs%60)
	if fraction > 0 {
		out += fmt.Sprintf(".%0*d", fraction, nanos%1e9/pow10(9-fraction))
	}
	return out, nil
}

// pow10 returns 10 to the power n, for n from 0 to 9.
func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}
