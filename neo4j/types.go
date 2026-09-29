package neo4j

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The temporal types of Neo4j that a time.Time does not name by itself (D63).
// Each is a defined type of time.Time, so it scans into a *time.Time too. An
// OffsetDateTime and a ZonedDateTime are a time.Time.
type (
	// Date is a date with no time and no zone, such as 2026-09-27. Its
	// time.Time is at midnight in UTC.
	Date time.Time
	// LocalTime is a time of day with no zone, such as 12:50:35.556123456.
	// Its time.Time is on 0000-01-01 in UTC.
	LocalTime time.Time
	// Time is a time of day with an offset, such as 12:50:35.556+01:00. Its
	// time.Time is on 0000-01-01, in a fixed zone of the offset.
	Time time.Time
	// LocalDateTime is a date and a time with no zone, such as
	// 2026-09-27T10:00:00.123456789. Its time.Time is in UTC.
	LocalDateTime time.Time
)

// String returns the date in the form of Neo4j.
func (d Date) String() string {
	return formatDate(time.Time(d))
}

// String returns the time in the form of Neo4j.
func (t LocalTime) String() string {
	return formatClock(time.Time(t))
}

// String returns the time and its offset in the form of Neo4j.
func (t Time) String() string {
	return formatClock(time.Time(t)) + formatOffset(time.Time(t))
}

// String returns the date and the time in the form of Neo4j.
func (t LocalDateTime) String() string {
	return formatDate(time.Time(t)) + "T" + formatClock(time.Time(t))
}

// Duration is a duration of Neo4j (D63). It keeps months and days apart from
// the seconds, because a month and a day have no fixed length, so a
// time.Duration cannot hold it. Nanos is from 0 to 999999999, and Seconds
// holds the sign of the time, as the server keeps them.
type Duration struct {
	Months  int64
	Days    int64
	Seconds int64
	Nanos   int64
}

// String returns the duration in the form of Neo4j, such as
// P1Y2M3DT4H5M6.007S, or PT0S for no time.
func (d Duration) String() string {
	var b strings.Builder
	b.WriteString("P")
	if y := d.Months / 12; y != 0 {
		fmt.Fprintf(&b, "%dY", y)
	}
	if m := d.Months % 12; m != 0 {
		fmt.Fprintf(&b, "%dM", m)
	}
	if d.Days != 0 {
		fmt.Fprintf(&b, "%dD", d.Days)
	}
	secs, nanos, neg := d.Seconds, d.Nanos, false
	if secs < 0 {
		// The server writes the time with one sign for each part, so a
		// negative time is written from its magnitude.
		neg = true
		secs = -secs
		if nanos > 0 {
			secs, nanos = secs-1, 1e9-nanos
		}
	}
	if secs == 0 && nanos == 0 {
		if b.Len() == 1 {
			return "PT0S"
		}
		return b.String()
	}
	sign := ""
	if neg {
		sign = "-"
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

// Point is a point of Neo4j (D63), in the coordinate system that SRID names,
// such as 7203 for a cartesian point or 4326 for WGS 84. Dims is 2 or 3, and
// Z is zero for a point of two dimensions.
type Point struct {
	SRID    int
	X, Y, Z float64
	Dims    int
}

// String returns the point in the form of Neo4j, such as
// SRID=7203;POINT (1.5 2.5).
func (p Point) String() string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	if p.Dims == 3 {
		return fmt.Sprintf("SRID=%d;POINT Z (%s %s %s)", p.SRID, f(p.X), f(p.Y), f(p.Z))
	}
	return fmt.Sprintf("SRID=%d;POINT (%s %s)", p.SRID, f(p.X), f(p.Y))
}

// Node is a node of the graph (D63).
type Node struct {
	// ElementID is the element id of the node.
	ElementID string
	// Labels are the labels of the node.
	Labels []string
	// Props are the properties of the node, each a Go value of D63.
	Props map[string]any
}

// Relationship is a relationship of the graph (D63).
type Relationship struct {
	// ElementID is the element id of the relationship.
	ElementID string
	// StartElementID and EndElementID are the element ids of its nodes.
	StartElementID string
	EndElementID   string
	// Type is the type of the relationship.
	Type string
	// Props are the properties of the relationship, each a Go value of D63.
	Props map[string]any
}

// Path is a path of the graph (D63): its nodes, and the relationship between
// each node and the next.
type Path struct {
	Nodes         []Node
	Relationships []Relationship
}

// Vector is a vector of Neo4j (D63). Coordinates is a []int8, an []int16, an
// []int32, an []int64, a []float32 or a []float64, by the type of the
// coordinates on the server. As an argument, a Vector is a vector, and a
// slice by itself is a list.
type Vector struct {
	Coordinates any
}

// The Go types of the coordinates of a vector, by the name that the server
// gives them.
const (
	coordsInt8    = "INT8"
	coordsInt16   = "INT16"
	coordsInt32   = "INT32"
	coordsInt64   = "INT64"
	coordsFloat32 = "FLOAT32"
	coordsFloat64 = "FLOAT64"
)

// formatDate writes the date of t as Neo4j does: a year of four digits, a
// sign before a year of more, and a minus sign before a negative year.
func formatDate(t time.Time) string {
	y, m, d := t.Date()
	var year string
	switch {
	case y > 9999:
		year = "+" + strconv.Itoa(y)
	case y < 0:
		year = fmt.Sprintf("-%04d", -y)
	default:
		year = fmt.Sprintf("%04d", y)
	}
	return fmt.Sprintf("%s-%02d-%02d", year, int(m), d)
}

// formatClock writes the time of day of t, with the nanoseconds if there are
// any, and no trailing zeros.
func formatClock(t time.Time) string {
	return fmt.Sprintf("%02d:%02d:%02d%s", t.Hour(), t.Minute(), t.Second(), fraction(int64(t.Nanosecond())))
}

// fraction writes nanos as a fraction of a second, such as ".007", or "" for
// none.
func fraction(nanos int64) string {
	if nanos == 0 {
		return ""
	}
	return "." + strings.TrimRight(fmt.Sprintf("%09d", nanos), "0")
}

// formatOffset writes the offset of t, such as +01:00, +01:30:15 or Z.
func formatOffset(t time.Time) string {
	_, off := t.Zone()
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

// formatDateTime writes t as a date, a time and an offset. If zone is true,
// it adds the name of the location of t, such as [Europe/Oslo].
func formatDateTime(t time.Time, zone bool) string {
	s := formatDate(t) + "T" + formatClock(t) + formatOffset(t)
	if zone {
		s += "[" + t.Location().String() + "]"
	}
	return s
}

// scanner reads the text of a temporal value from its start.
type scanner struct {
	s string
}

// errForm is the error for a value that does not have the form of its type.
func errForm(typ, s string) error {
	return fmt.Errorf("reading %q as a %s: %w", s, typ, dbimp.ErrInvalidValue)
}

// digits reads a run of at least least digits, and returns its value.
func (sc *scanner) digits(least int) (int, bool) {
	i := 0
	for i < len(sc.s) && sc.s[i] >= '0' && sc.s[i] <= '9' {
		i++
	}
	if i < least || i > 18 {
		return 0, false
	}
	n, err := strconv.Atoi(sc.s[:i])
	if err != nil {
		return 0, false
	}
	sc.s = sc.s[i:]
	return n, true
}

// two reads exactly two digits, as a month, a day, an hour, a minute and a
// second have.
func (sc *scanner) two() (int, bool) {
	if len(sc.s) < 2 || sc.s[0] < '0' || sc.s[0] > '9' || sc.s[1] < '0' || sc.s[1] > '9' {
		return 0, false
	}
	v := int(sc.s[0]-'0')*10 + int(sc.s[1]-'0')
	sc.s = sc.s[2:]
	return v, true
}

// byte reads the byte c.
func (sc *scanner) byte(c byte) bool {
	if len(sc.s) == 0 || sc.s[0] != c {
		return false
	}
	sc.s = sc.s[1:]
	return true
}

// parts are the parts of a date and a time of day.
type parts struct {
	y, mo, d     int
	h, mi, s, ns int
}

// date reads a date, such as 2026-09-27, +999999999-12-31 or -0001-06-01,
// into p.
func (sc *scanner) date(p *parts) bool {
	sign := 1
	switch {
	case sc.byte('-'):
		sign = -1
	case sc.byte('+'):
	}
	y, ok := sc.digits(4)
	if !ok || !sc.byte('-') {
		return false
	}
	if p.mo, ok = sc.two(); !ok || !sc.byte('-') {
		return false
	}
	if p.d, ok = sc.two(); !ok {
		return false
	}
	p.y = sign * y
	return true
}

// clock reads a time of day, such as 12:00, 12:50:35 or 12:50:35.556123456,
// into p.
func (sc *scanner) clock(p *parts) bool {
	var ok bool
	if p.h, ok = sc.two(); !ok || !sc.byte(':') {
		return false
	}
	if p.mi, ok = sc.two(); !ok {
		return false
	}
	if !sc.byte(':') {
		return true
	}
	if p.s, ok = sc.two(); !ok {
		return false
	}
	if sc.byte('.') {
		start := sc.s
		n, ok := sc.digits(1)
		width := len(start) - len(sc.s)
		if !ok || width > 9 {
			return false
		}
		for range 9 - width {
			n *= 10
		}
		p.ns = n
	}
	return true
}

// offset reads an offset, such as Z, +01:00 or -05:30:15, in seconds.
func (sc *scanner) offset() (int, bool) {
	if sc.byte('Z') {
		return 0, true
	}
	sign := 1
	switch {
	case sc.byte('-'):
		sign = -1
	case sc.byte('+'):
	default:
		return 0, false
	}
	h, ok := sc.two()
	if !ok || !sc.byte(':') {
		return 0, false
	}
	m, ok := sc.two()
	if !ok {
		return 0, false
	}
	s := 0
	if sc.byte(':') {
		if s, ok = sc.two(); !ok {
			return 0, false
		}
	}
	return sign * (h*3600 + m*60 + s), true
}

// makeTime returns the time of p in loc, and false if a part is out of its
// range, such as the month 13.
func makeTime(p parts, loc *time.Location) (time.Time, bool) {
	t := time.Date(p.y, time.Month(p.mo), p.d, p.h, p.mi, p.s, p.ns, loc)
	ty, tm, td := t.Date()
	if ty != p.y || int(tm) != p.mo || td != p.d || t.Hour() != p.h || t.Minute() != p.mi || t.Second() != p.s {
		return time.Time{}, false
	}
	return t, true
}

// parseDate reads a Date.
func parseDate(s string) (Date, error) {
	sc, p := scanner{s}, parts{}
	if !sc.date(&p) || sc.s != "" {
		return Date{}, errForm("Date", s)
	}
	t, ok := makeTime(p, time.UTC)
	if !ok {
		return Date{}, errForm("Date", s)
	}
	return Date(t), nil
}

// parseLocalTime reads a LocalTime.
func parseLocalTime(s string) (LocalTime, error) {
	sc, p := scanner{s}, parts{mo: 1, d: 1}
	if !sc.clock(&p) || sc.s != "" {
		return LocalTime{}, errForm("LocalTime", s)
	}
	t, ok := makeTime(p, time.UTC)
	if !ok {
		return LocalTime{}, errForm("LocalTime", s)
	}
	return LocalTime(t), nil
}

// parseTime reads a Time, a time of day with an offset.
func parseTime(s string) (Time, error) {
	sc, p := scanner{s}, parts{mo: 1, d: 1}
	if !sc.clock(&p) {
		return Time{}, errForm("Time", s)
	}
	off, ok := sc.offset()
	if !ok || sc.s != "" {
		return Time{}, errForm("Time", s)
	}
	t, ok := makeTime(p, fixedZone(off))
	if !ok {
		return Time{}, errForm("Time", s)
	}
	return Time(t), nil
}

// parseDateTime reads a date and a time, then what follows them: nothing for
// a LocalDateTime, an offset for an OffsetDateTime, and an offset and a zone
// for a ZonedDateTime.
func parseDateTime(typ, s string, withOffset, withZone bool) (time.Time, error) {
	sc, p := scanner{s}, parts{}
	if !sc.date(&p) || !sc.byte('T') || !sc.clock(&p) {
		return time.Time{}, errForm(typ, s)
	}
	off, loc := 0, time.UTC
	if withOffset {
		var ok bool
		if off, ok = sc.offset(); !ok {
			return time.Time{}, errForm(typ, s)
		}
		loc = fixedZone(off)
	}
	if withZone {
		name, open := strings.CutPrefix(sc.s, "[")
		name, closed := strings.CutSuffix(name, "]")
		if !open || !closed || name == "" {
			return time.Time{}, errForm(typ, s)
		}
		sc.s = ""
		// The offset fixes the instant, and the zone names it, so the
		// instant is made in the offset first.
		t, ok := makeTime(p, loc)
		if !ok {
			return time.Time{}, errForm(typ, s)
		}
		return t.In(zone(name, off)), nil
	}
	if sc.s != "" {
		return time.Time{}, errForm(typ, s)
	}
	t, ok := makeTime(p, loc)
	if !ok {
		return time.Time{}, errForm(typ, s)
	}
	return t, nil
}

// fixedZone returns a zone of the offset off, in seconds, with no name, so
// that the zone of an OffsetDateTime is never taken for a named zone. An
// offset of zero is time.UTC.
func fixedZone(off int) *time.Location {
	if off == 0 {
		return time.UTC
	}
	return time.FixedZone("", off)
}

// zone returns the location that name names. If the system has no such
// zone, it is a fixed zone with the name and the offset that the server sent
// (D63).
func zone(name string, off int) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil && name != "Local" {
		return loc
	}
	return time.FixedZone(name, off)
}

// parseDuration reads a Duration, such as P1Y2M3DT4H5M6.007S or
// P-1Y-2M-3DT-5.000000007S. Each part can have a sign, and only the seconds
// can have a fraction.
func parseDuration(s string) (Duration, error) {
	rest, ok := strings.CutPrefix(s, "P")
	if !ok || rest == "" {
		return Duration{}, errForm("Duration", s)
	}
	var (
		d      Duration
		inTime bool
		nanos  int64
		units  = "YMWD"
	)
	for rest != "" {
		if rest[0] == 'T' {
			if inTime {
				return Duration{}, errForm("Duration", s)
			}
			inTime, units, rest = true, "HMS", rest[1:]
			continue
		}
		i := 0
		if i < len(rest) && (rest[i] == '-' || rest[i] == '+') {
			i++
		}
		for i < len(rest) && (rest[i] >= '0' && rest[i] <= '9' || rest[i] == '.') {
			i++
		}
		if i == len(rest) || i == 0 {
			return Duration{}, errForm("Duration", s)
		}
		num, unit := rest[:i], rest[i]
		rest = rest[i+1:]
		u := strings.IndexByte(units, unit)
		if u < 0 {
			return Duration{}, errForm("Duration", s)
		}
		// Each unit can appear once, in its order.
		units = units[u+1:]
		whole, frac, hasFrac := strings.Cut(num, ".")
		if hasFrac && (unit != 'S' || !inTime || len(frac) == 0 || len(frac) > 9) {
			return Duration{}, errForm("Duration", s)
		}
		n, err := strconv.ParseInt(whole, 10, 64)
		if err != nil {
			return Duration{}, errForm("Duration", s)
		}
		neg := strings.HasPrefix(whole, "-")
		switch {
		case !inTime && unit == 'Y':
			d.Months += n * 12
		case !inTime && unit == 'M':
			d.Months += n
		case unit == 'W':
			d.Days += n * 7
		case unit == 'D':
			d.Days += n
		case unit == 'H':
			d.Seconds += n * 3600
		case unit == 'M':
			d.Seconds += n * 60
		case unit == 'S':
			d.Seconds += n
			if hasFrac {
				f, _ := strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
				if neg {
					f = -f
				}
				nanos = f
			}
		}
	}
	// The server keeps the nanoseconds from 0 to 999999999, and the seconds
	// hold the sign.
	d.Seconds += nanos / 1e9
	d.Nanos = nanos % 1e9
	if d.Nanos < 0 {
		d.Seconds, d.Nanos = d.Seconds-1, d.Nanos+1e9
	}
	return d, nil
}

// parsePoint reads a Point, such as SRID=7203;POINT (1.5 2.5) or
// SRID=4979;POINT Z (10.7 59.9 3.0).
func parsePoint(s string) (Point, error) {
	head, coords, ok := strings.Cut(s, ";")
	srid, found := strings.CutPrefix(head, "SRID=")
	if !ok || !found {
		return Point{}, errForm("Point", s)
	}
	var p Point
	var err error
	if p.SRID, err = strconv.Atoi(srid); err != nil {
		return Point{}, errForm("Point", s)
	}
	p.Dims = 2
	switch {
	case strings.HasPrefix(coords, "POINT Z ("):
		p.Dims, coords = 3, coords[len("POINT Z ("):]
	case strings.HasPrefix(coords, "POINT ("):
		coords = coords[len("POINT ("):]
	default:
		return Point{}, errForm("Point", s)
	}
	coords, ok = strings.CutSuffix(coords, ")")
	parts := strings.Fields(coords)
	if !ok || len(parts) != p.Dims {
		return Point{}, errForm("Point", s)
	}
	vals := make([]float64, 3)
	for i, part := range parts {
		if vals[i], err = strconv.ParseFloat(part, 64); err != nil {
			return Point{}, errForm("Point", s)
		}
	}
	p.X, p.Y, p.Z = vals[0], vals[1], vals[2]
	return p, nil
}

// parseFloat reads a float as typed JSON writes it, such as 0.1, 1.5E300,
// NaN, Infinity or -Infinity.
func parseFloat(s string) (float64, error) {
	switch s {
	case "NaN":
		return math.NaN(), nil
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || strings.ContainsAny(s, "iInN") {
		return 0, errForm("Float", s)
	}
	return f, nil
}

// formatFloat writes f as typed JSON reads it.
func formatFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return strconv.FormatFloat(f, 'g', -1, bits)
}
