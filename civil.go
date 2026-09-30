package dbimp

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// These are the types of D138 and D139, for the kinds of docs/TYPES.md that
// Go has no type for. A Date, a LocalTime and a LocalDateTime have no zone,
// so they name no instant. An OffsetTime has an offset and no date. An
// Interval holds months and days, which have no fixed length. Each type
// writes ISO 8601 with String, reads it back with its Parse function, and is
// a driver.Valuer of that text, so that a driver that does not name the type
// sends its text. A Vector is a slice of numbers.

// Date is a day of the calendar, with no time and no zone, such as
// 2026-09-30.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the date of t, in the location of t.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// ParseDate reads a date as String writes it, such as 2026-09-30, -0044-03-15
// or +10000-01-01.
func ParseDate(s string) (Date, error) {
	p := parser{s: s}
	d, ok := p.date()
	if !ok || p.s != "" || !d.IsValid() {
		return Date{}, formError("date", s)
	}
	return d, nil
}

// String writes the date in ISO 8601: a year of four digits, a sign before a
// year of more than four, and a minus sign before a negative year.
func (d Date) String() string {
	var year string
	switch {
	case d.Year > 9999:
		year = "+" + strconv.Itoa(d.Year)
	case d.Year < 0:
		year = fmt.Sprintf("-%04d", -d.Year)
	default:
		year = fmt.Sprintf("%04d", d.Year)
	}
	return fmt.Sprintf("%s-%02d-%02d", year, int(d.Month), d.Day)
}

// IsValid reports whether the date is a day of the calendar, which February
// 30 is not.
func (d Date) IsValid() bool {
	return DateOf(d.In(time.UTC)) == d
}

// In returns the time.Time of midnight at the start of the date in loc.
func (d Date) In(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

// Value satisfies driver.Valuer. It is the text of String.
func (d Date) Value() (driver.Value, error) {
	return d.String(), nil
}

// LocalTime is a time of day, with no date and no zone, such as 12:30:00.5.
// Hour is from 0 to 23.
type LocalTime struct {
	Hour       int
	Minute     int
	Second     int
	Nanosecond int
}

// LocalTimeOf returns the time of day of t, in the location of t.
func LocalTimeOf(t time.Time) LocalTime {
	h, m, s := t.Clock()
	return LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: t.Nanosecond()}
}

// ParseLocalTime reads a time of day as String writes it, such as 12:30,
// 12:30:00 or 12:30:00.123456789.
func ParseLocalTime(s string) (LocalTime, error) {
	p := parser{s: s}
	t, ok := p.clock()
	if !ok || p.s != "" || !t.IsValid() {
		return LocalTime{}, formError("time of day", s)
	}
	return t, nil
}

// String writes the time of day in ISO 8601, with the fraction of a second
// if there is one, and no trailing zeros.
func (t LocalTime) String() string {
	return fmt.Sprintf("%02d:%02d:%02d%s", t.Hour, t.Minute, t.Second, fraction(int64(t.Nanosecond)))
}

// IsValid reports whether each part of the time is in its range.
func (t LocalTime) IsValid() bool {
	return t.Hour >= 0 && t.Hour < 24 && t.Minute >= 0 && t.Minute < 60 &&
		t.Second >= 0 && t.Second < 60 && t.Nanosecond >= 0 && t.Nanosecond < 1e9
}

// In returns the time.Time of the time of day on 0000-01-01 in loc, as lib/pq
// gives a time of day.
func (t LocalTime) In(loc *time.Location) time.Time {
	return time.Date(0, 1, 1, t.Hour, t.Minute, t.Second, t.Nanosecond, loc)
}

// Value satisfies driver.Valuer. It is the text of String.
func (t LocalTime) Value() (driver.Value, error) {
	return t.String(), nil
}

// OffsetTime is a time of day with an offset from UTC and no date, such as
// 12:30:00.5+01:00, as the TIME of Neo4j and the TIME WITH TIME ZONE of SQL
// hold it (D139). Offset is in seconds east of UTC.
type OffsetTime struct {
	Time   LocalTime
	Offset int
}

// OffsetTimeOf returns the time of day of t and the offset of its zone.
func OffsetTimeOf(t time.Time) OffsetTime {
	_, off := t.Zone()
	return OffsetTime{Time: LocalTimeOf(t), Offset: off}
}

// ParseOffsetTime reads a time of day and an offset as String writes it, such
// as 12:30:00Z, 12:30:00.5+01:00 or 12:30-05:30:15.
func ParseOffsetTime(s string) (OffsetTime, error) {
	p := parser{s: s}
	t, ok := p.clock()
	if !ok {
		return OffsetTime{}, formError("time of day with an offset", s)
	}
	off, ok := p.offset()
	ot := OffsetTime{Time: t, Offset: off}
	if !ok || p.s != "" || !ot.IsValid() {
		return OffsetTime{}, formError("time of day with an offset", s)
	}
	return ot, nil
}

// String writes the time of day and the offset in ISO 8601, with Z for an
// offset of zero, and the seconds of an offset only when it has them.
func (t OffsetTime) String() string {
	return t.Time.String() + formatOffset(t.Offset)
}

// IsValid reports whether the time of day is valid, and the offset is less
// than 24 hours.
func (t OffsetTime) IsValid() bool {
	return t.Time.IsValid() && t.Offset > -86400 && t.Offset < 86400
}

// ToTime returns the time.Time of the time of day on 0000-01-01, in a fixed
// zone of the offset, as lib/pq gives a TIMETZ.
func (t OffsetTime) ToTime() time.Time {
	return t.Time.In(fixedZone(t.Offset))
}

// Value satisfies driver.Valuer. It is the text of String.
func (t OffsetTime) Value() (driver.Value, error) {
	return t.String(), nil
}

// formatOffset writes an offset in seconds, such as Z, +01:00 or -05:30:15.
func formatOffset(off int) string {
	if off == 0 {
		return "Z"
	}
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	s := fmt.Sprintf("%s%02d:%02d", sign, off/3600, off%3600/60)
	if sec := off % 60; sec != 0 {
		s += fmt.Sprintf(":%02d", sec)
	}
	return s
}

// fixedZone returns a zone of the offset off, in seconds, with no name. An
// offset of zero is time.UTC.
func fixedZone(off int) *time.Location {
	if off == 0 {
		return time.UTC
	}
	return time.FixedZone("", off)
}

// LocalDateTime is a date and a time of day, with no zone, such as
// 2026-09-30T12:30:00.5. It names no instant until a caller names a
// location.
type LocalDateTime struct {
	Date Date
	Time LocalTime
}

// LocalDateTimeOf returns the date and the time of day of t, in the location
// of t.
func LocalDateTimeOf(t time.Time) LocalDateTime {
	return LocalDateTime{Date: DateOf(t), Time: LocalTimeOf(t)}
}

// ParseLocalDateTime reads a date and a time as String writes it, such as
// 2026-09-30T12:30:00.5. It takes a space in place of the T too, as SQL
// writes a timestamp.
func ParseLocalDateTime(s string) (LocalDateTime, error) {
	p := parser{s: s}
	d, ok := p.date()
	if !ok || (!p.byte('T') && !p.byte(' ')) {
		return LocalDateTime{}, formError("date and time", s)
	}
	t, ok := p.clock()
	dt := LocalDateTime{Date: d, Time: t}
	if !ok || p.s != "" || !dt.IsValid() {
		return LocalDateTime{}, formError("date and time", s)
	}
	return dt, nil
}

// String writes the date and the time in ISO 8601, joined by a T.
func (dt LocalDateTime) String() string {
	return dt.Date.String() + "T" + dt.Time.String()
}

// IsValid reports whether the date and the time are both valid.
func (dt LocalDateTime) IsValid() bool {
	return dt.Date.IsValid() && dt.Time.IsValid()
}

// In returns the time.Time of the date and the time in loc. A time that loc
// skips, as a zone does when its clock moves forward, moves as time.Date
// moves it.
func (dt LocalDateTime) In(loc *time.Location) time.Time {
	return time.Date(dt.Date.Year, dt.Date.Month, dt.Date.Day, dt.Time.Hour, dt.Time.Minute, dt.Time.Second, dt.Time.Nanosecond, loc)
}

// Value satisfies driver.Valuer. It is the text of String.
func (dt LocalDateTime) Value() (driver.Value, error) {
	return dt.String(), nil
}

// Interval is a length of the calendar: months, days, and an exact time, as
// PostgreSQL and the Arrow type Interval(MonthDayNano) keep it. A month and a
// day have no fixed length, so the three parts stay apart. Each part carries
// its own sign.
type Interval struct {
	Months      int32
	Days        int32
	Nanoseconds int64
}

// ParseInterval reads an interval in ISO 8601, such as P1Y2M3DT4H5M6.5S,
// P2W, PT-1.5S or -P1D. Each part can have a sign, and a sign before the P
// turns every part around.
func ParseInterval(s string) (Interval, error) {
	iv, ok := parseInterval(s)
	if !ok {
		return Interval{}, formError("interval", s)
	}
	return iv, nil
}

// String writes the interval in ISO 8601, with years and months, days, and
// hours, minutes and seconds, such as P1M2DT3.5S, and PT0S for an interval of
// zero. A negative time is written with a sign before each of its parts.
func (iv Interval) String() string {
	if iv == (Interval{}) {
		return "PT0S"
	}
	var b strings.Builder
	b.WriteString("P")
	if y := iv.Months / 12; y != 0 {
		fmt.Fprintf(&b, "%dY", y)
	}
	if m := iv.Months % 12; m != 0 {
		fmt.Fprintf(&b, "%dM", m)
	}
	if iv.Days != 0 {
		fmt.Fprintf(&b, "%dD", iv.Days)
	}
	if iv.Nanoseconds == 0 {
		return b.String()
	}
	sign, secs, nanos := "", iv.Nanoseconds/1e9, iv.Nanoseconds%1e9
	if iv.Nanoseconds < 0 {
		// Each part is negated after the division, so that the smallest
		// int64 fits.
		sign, secs, nanos = "-", -secs, -nanos
	}
	b.WriteString("T")
	if h := secs / 3600; h != 0 {
		fmt.Fprintf(&b, "%s%dH", sign, h)
	}
	if m := secs % 3600 / 60; m != 0 {
		fmt.Fprintf(&b, "%s%dM", sign, m)
	}
	if s := secs % 60; s != 0 || nanos != 0 {
		fmt.Fprintf(&b, "%s%d%sS", sign, s, fraction(nanos))
	}
	return b.String()
}

// Value satisfies driver.Valuer. It is the text of String.
func (iv Interval) Value() (driver.Value, error) {
	return iv.String(), nil
}

// VectorElement is the type of an element of a Vector.
type VectorElement interface {
	int8 | int16 | int32 | int64 | float32 | float64
}

// Vector is a vector: numbers of one type, for a search by similarity, such
// as the VECTOR of Neo4j and of Databend (D139). It is a slice, so it scans
// into a slice of its elements too. A driver sends a Vector as a vector, and
// a plain slice as a list.
type Vector[T VectorElement] []T

// String writes the vector as a JSON array, such as [1.5,2].
func (v Vector[T]) String() string {
	var b strings.Builder
	b.WriteByte('[')
	for i, e := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		switch x := any(e).(type) {
		case float32:
			b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
		case float64:
			b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		default:
			fmt.Fprint(&b, x)
		}
	}
	b.WriteByte(']')
	return b.String()
}

// Value satisfies driver.Valuer. It is the text of String.
func (v Vector[T]) Value() (driver.Value, error) {
	return v.String(), nil
}

// fraction writes nanos as a fraction of a second, such as ".007", or "" for
// none.
func fraction(nanos int64) string {
	if nanos == 0 {
		return ""
	}
	return "." + strings.TrimRight(fmt.Sprintf("%09d", nanos), "0")
}

// formError is the error of a text that is not a value of its kind.
func formError(kind, s string) error {
	return fmt.Errorf("reading %q as a %s: %w", s, kind, ErrInvalidValue)
}

// parser reads the text of a civil value from its start.
type parser struct {
	s string
}

// byte reads c, and reports whether it was there.
func (p *parser) byte(c byte) bool {
	if p.s != "" && p.s[0] == c {
		p.s = p.s[1:]
		return true
	}
	return false
}

// digits reads a run of from least to most digits, and returns its value and
// its width.
func (p *parser) digits(least, most int) (int, int, bool) {
	i := 0
	for i < len(p.s) && p.s[i] >= '0' && p.s[i] <= '9' {
		i++
	}
	if i < least || i > most {
		return 0, 0, false
	}
	n, err := strconv.Atoi(p.s[:i])
	if err != nil {
		return 0, 0, false
	}
	p.s = p.s[i:]
	return n, i, true
}

// two reads exactly two digits.
func (p *parser) two() (int, bool) {
	n, _, ok := p.digits(2, 2)
	return n, ok
}

// date reads a year, a month and a day. A year of more than four digits has a
// plus sign, and a negative year a minus sign.
func (p *parser) date() (Date, bool) {
	neg, plus := p.byte('-'), false
	if !neg {
		plus = p.byte('+')
	}
	y, width, ok := p.digits(4, 9)
	if !ok || width > 4 && !neg && !plus || plus && width == 4 {
		return Date{}, false
	}
	if neg {
		y = -y
	}
	if !p.byte('-') {
		return Date{}, false
	}
	m, ok := p.two()
	if !ok || !p.byte('-') {
		return Date{}, false
	}
	d, ok := p.two()
	if !ok {
		return Date{}, false
	}
	return Date{Year: y, Month: time.Month(m), Day: d}, true
}

// offset reads an offset, such as Z, +01:00 or -05:30:15, in seconds.
func (p *parser) offset() (int, bool) {
	if p.byte('Z') {
		return 0, true
	}
	sign := 1
	switch {
	case p.byte('-'):
		sign = -1
	case p.byte('+'):
	default:
		return 0, false
	}
	h, ok := p.two()
	if !ok || !p.byte(':') {
		return 0, false
	}
	m, ok := p.two()
	if !ok || m > 59 {
		return 0, false
	}
	s := 0
	if p.byte(':') {
		if s, ok = p.two(); !ok || s > 59 {
			return 0, false
		}
	}
	return sign * (h*3600 + m*60 + s), true
}

// clock reads an hour and a minute, and a second with a fraction of up to
// nine digits if they are there.
func (p *parser) clock() (LocalTime, bool) {
	var t LocalTime
	var ok bool
	if t.Hour, ok = p.two(); !ok || !p.byte(':') {
		return LocalTime{}, false
	}
	if t.Minute, ok = p.two(); !ok {
		return LocalTime{}, false
	}
	if !p.byte(':') {
		return t, true
	}
	if t.Second, ok = p.two(); !ok {
		return LocalTime{}, false
	}
	if !p.byte('.') {
		return t, true
	}
	n, width, ok := p.digits(1, 9)
	if !ok {
		return LocalTime{}, false
	}
	for range 9 - width {
		n *= 10
	}
	t.Nanosecond = n
	return t, true
}

// The nanoseconds of each unit of the time of an interval.
var timeUnits = map[byte]int64{'H': int64(time.Hour), 'M': int64(time.Minute), 'S': int64(time.Second)}

// parseInterval reads the text of ParseInterval.
func parseInterval(s string) (Interval, bool) {
	text, negAll := strings.CutPrefix(s, "-")
	text, ok := strings.CutPrefix(text, "P")
	if !ok || text == "" || text == "T" {
		return Interval{}, false
	}
	var months, days, nanos int64
	inTime, seen := false, false
	// Each unit can appear once, in its order.
	units := "YMWD"
	for text != "" {
		if text[0] == 'T' {
			if inTime {
				return Interval{}, false
			}
			inTime, units, text = true, "HMS", text[1:]
			if text == "" {
				return Interval{}, false
			}
			continue
		}
		neg := text[0] == '-'
		if neg || text[0] == '+' {
			text = text[1:]
		}
		i := 0
		for i < len(text) && (text[i] >= '0' && text[i] <= '9' || text[i] == '.') {
			i++
		}
		if i == 0 || i == len(text) {
			return Interval{}, false
		}
		num, unit := text[:i], text[i]
		text = text[i+1:]
		u := strings.IndexByte(units, unit)
		if u < 0 {
			return Interval{}, false
		}
		units = units[u+1:]
		whole, frac, hasFrac := strings.Cut(num, ".")
		if whole == "" || hasFrac && (unit != 'S' || !inTime || frac == "" || len(frac) > 9) {
			return Interval{}, false
		}
		n, err := strconv.ParseInt(whole, 10, 64)
		if err != nil {
			return Interval{}, false
		}
		if neg {
			n = -n
		}
		seen = true
		switch {
		case !inTime && (unit == 'Y' || unit == 'M'):
			factor := int64(1)
			if unit == 'Y' {
				factor = 12
			}
			if months, ok = addMul(months, n, factor); !ok {
				return Interval{}, false
			}
		case !inTime && (unit == 'W' || unit == 'D'):
			factor := int64(1)
			if unit == 'W' {
				factor = 7
			}
			if days, ok = addMul(days, n, factor); !ok {
				return Interval{}, false
			}
		case inTime && timeUnits[unit] != 0:
			if nanos, ok = addMul(nanos, n, timeUnits[unit]); !ok {
				return Interval{}, false
			}
			if hasFrac {
				f, err := strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
				if err != nil {
					return Interval{}, false
				}
				if neg {
					f = -f
				}
				if nanos, ok = addMul(nanos, f, 1); !ok {
					return Interval{}, false
				}
			}
		default:
			return Interval{}, false
		}
	}
	if !seen || months < math.MinInt32 || months > math.MaxInt32 || days < math.MinInt32 || days > math.MaxInt32 {
		return Interval{}, false
	}
	iv := Interval{Months: int32(months), Days: int32(days), Nanoseconds: nanos}
	if negAll {
		if iv.Months == math.MinInt32 || iv.Days == math.MinInt32 || iv.Nanoseconds == math.MinInt64 {
			return Interval{}, false
		}
		iv = Interval{Months: -iv.Months, Days: -iv.Days, Nanoseconds: -iv.Nanoseconds}
	}
	return iv, true
}

// addMul returns sum + n*factor, and false when it passes the range of an
// int64.
func addMul(sum, n, factor int64) (int64, bool) {
	if n != 0 && (n > math.MaxInt64/factor || n < math.MinInt64/factor) {
		return 0, false
	}
	p := n * factor
	if p > 0 && sum > math.MaxInt64-p || p < 0 && sum < math.MinInt64-p {
		return 0, false
	}
	return sum + p, true
}
