package snowflake

import (
	"encoding/hex"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The names of the types in the member type of rowType, which are the wire
// types (measured). The server names a geography and a geometry object, so
// the two names after object are never sent today. A driver of another
// version of the server can send them, and they read as object does.
const (
	wireFixed        = "fixed"
	wireReal         = "real"
	wireText         = "text"
	wireBinary       = "binary"
	wireBoolean      = "boolean"
	wireDate         = "date"
	wireTime         = "time"
	wireTimestampNTZ = "timestamp_ntz"
	wireTimestampLTZ = "timestamp_ltz"
	wireTimestampTZ  = "timestamp_tz"
	wireVariant      = "variant"
	wireObject       = "object"
	wireArray        = "array"
	wireGeography    = "geography"
	wireGeometry     = "geometry"
	wireVector       = "vector"
)

// maxIntegerPrecision is the most digits of a fixed value of scale 0 that
// always fit an int64 (D183).
const maxIntegerPrecision = 18

// column is the type of one column, from its entry of rowType.
type column struct {
	name string
	// wire is the type of the entry, in lower case.
	wire string
	// precision, scale, length and nullable are the members of the entry.
	// A member that the entry leaves null is -1.
	precision int64
	scale     int64
	length    int64
	nullable  bool
}

// isInteger reports whether the column is a fixed of scale 0 whose values
// always fit an int64: the precision is 18 or less (D183).
func (c column) isInteger() bool {
	return c.wire == wireFixed && c.scale == 0 && c.precision >= 0 && c.precision <= maxIntegerPrecision
}

// databaseType returns the name of the type in upper case.
func (c column) databaseType() string {
	return strings.ToUpper(c.wire)
}

// decode returns the value v of a column as its Go value (D135 and D183). A
// value of the wire is a string, or null (measured). loc is the zone of a
// timestamp_ltz value.
func decode(c column, v jsontext.Value, loc *time.Location) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	switch c.wire {
	case wireFixed:
		if c.isInteger() {
			return parseInt(s)
		}
		return parseDecimal(s)
	case wireReal:
		return parseFloat(s)
	case wireText:
		return s, nil
	case wireBinary:
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as hex: %w", s, dbimp.ErrInvalidValue)
		}
		return b, nil
	case wireBoolean:
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("reading %q as a boolean: %w", s, dbimp.ErrInvalidValue)
	case wireDate:
		return parseDate(s)
	case wireTime:
		return parseTime(s)
	case wireTimestampNTZ:
		sec, nanos, err := parseEpoch(s)
		if err != nil {
			return nil, err
		}
		return dbimp.LocalDateTimeOf(time.Unix(sec, nanos).UTC()), nil
	case wireTimestampLTZ:
		sec, nanos, err := parseEpoch(s)
		if err != nil {
			return nil, err
		}
		return time.Unix(sec, nanos).In(loc), nil
	case wireTimestampTZ:
		return parseTimestampTZ(s)
	case wireVariant, wireObject, wireArray, wireGeography, wireGeometry:
		return parseJSON(s)
	case wireVector:
		return parseVector(s)
	}
	return nil, fmt.Errorf("reading a value of the type %q: the driver has no Go type for it: %w", c.wire, dbimp.ErrNotSupported)
}

// parseInt reads a fixed value of scale 0 and a precision of 18 or less.
func parseInt(s string) (int64, error) {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %q as an integer: %w", s, dbimp.ErrInvalidValue)
	}
	return i, nil
}

// parseDecimal reads any other fixed value, which is decimal digits (D33).
func parseDecimal(s string) (*apd.Decimal, error) {
	d, _, err := apd.NewFromString(s)
	if err != nil || d.Form != apd.Finite {
		return nil, fmt.Errorf("reading %q as a decimal: %w", s, dbimp.ErrInvalidValue)
	}
	return d, nil
}

// parseFloat reads a real value. A NaN is NaN, and an infinity is inf or
// -inf (measured).
func parseFloat(s string) (float64, error) {
	switch s {
	case "NaN":
		return math.NaN(), nil
	case "inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %q as a float: %w", s, dbimp.ErrInvalidValue)
	}
	return f, nil
}

// The range of the days of a date, and of the seconds of a timestamp, that
// the years 1 to 9999 name.
const (
	minDay    = -719162
	maxDay    = 2932896
	minSecond = -62135596800
	maxSecond = 253402300799
)

// parseDate reads a date, the days since 1970-01-01 (measured).
func parseDate(s string) (dbimp.Date, error) {
	days, err := strconv.ParseInt(s, 10, 64)
	if err != nil || days < minDay || days > maxDay {
		return dbimp.Date{}, fmt.Errorf("reading %q as a date: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.DateOf(time.Unix(days*86400, 0).UTC()), nil
}

// parseTime reads a time of day, the seconds since midnight with a fraction
// (measured).
func parseTime(s string) (dbimp.LocalTime, error) {
	sec, nanos, err := parseEpoch(s)
	if err != nil || sec < 0 || sec >= 86400 {
		return dbimp.LocalTime{}, fmt.Errorf("reading %q as a time of day: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.LocalTimeOf(time.Unix(sec, nanos).UTC()), nil
}

// parseEpoch reads seconds with a fraction of up to nine digits, such as
// 1791549296.123456789, as the whole seconds and the nanoseconds. A negative
// value is the exact negative number, so the nanoseconds are never negative
// (measured for the positive form only).
func parseEpoch(s string) (int64, int64, error) {
	bad := fmt.Errorf("reading %q as seconds: %w", s, dbimp.ErrInvalidValue)
	body, negative := strings.CutPrefix(s, "-")
	whole, fraction, _ := strings.Cut(body, ".")
	if !digits(whole) || len(fraction) > 9 || fraction != "" && !digits(fraction) {
		return 0, 0, bad
	}
	sec, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || sec > maxSecond+1 {
		return 0, 0, bad
	}
	var nanos int64
	if fraction != "" {
		nanos, err = strconv.ParseInt(fraction+strings.Repeat("0", 9-len(fraction)), 10, 64)
		if err != nil {
			return 0, 0, bad
		}
	}
	if negative {
		sec, nanos = -sec, -nanos
		if nanos < 0 {
			sec, nanos = sec-1, nanos+int64(time.Second)
		}
	}
	if sec < minSecond || sec > maxSecond {
		return 0, 0, bad
	}
	return sec, nanos, nil
}

// digits reports whether s is one or more decimal digits.
func digits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// parseTimestampTZ reads <epoch seconds> <offset>, where the offset is the
// minutes from UTC plus 1440 (measured). The text 1791524096.123456789 1860
// is 420 minutes, which is +07:00.
func parseTimestampTZ(s string) (time.Time, error) {
	epoch, offset, ok := strings.Cut(s, " ")
	if !ok {
		return time.Time{}, fmt.Errorf("reading %q as a timestamp with a zone: %w", s, dbimp.ErrInvalidValue)
	}
	sec, nanos, err := parseEpoch(epoch)
	if err != nil {
		return time.Time{}, err
	}
	n, err := strconv.Atoi(offset)
	if err != nil || n < 0 || n > 2880 {
		return time.Time{}, fmt.Errorf("reading the offset %q of a timestamp with a zone: %w", offset, dbimp.ErrInvalidValue)
	}
	return time.Unix(sec, nanos).In(time.FixedZone("", (n-1440)*60)), nil
}

// parseJSON reads the text of a variant, an object or an array as the
// decoded JSON value. The server writes the text over several lines, and
// writes a NULL inside an array as undefined, which is not JSON (measured),
// so the driver reads it as null.
func parseJSON(s string) (any, error) {
	val := jsontext.Value(undefinedAsNull(s))
	if !val.IsValid() {
		return nil, fmt.Errorf("reading %q as JSON: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.Any(val)
}

// undefinedAsNull returns s with each undefined that stands outside a string
// written as null.
func undefinedAsNull(s string) string {
	const word = "undefined"
	if !strings.Contains(s, word) {
		return s
	}
	var b strings.Builder
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString && c == '\\' && i+1 < len(s):
			b.WriteByte(c)
			i++
			b.WriteByte(s[i])
			continue
		case c == '"':
			inString = !inString
		case !inString && strings.HasPrefix(s[i:], word):
			b.WriteString("null")
			i += len(word) - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// parseVector reads the text of a vector, such as [1.000000,2.000000]. The
// metadata names no type of the elements, so each one is a float64 (D183).
func parseVector(s string) (dbimp.Vector[float64], error) {
	dec := jsontext.NewDecoder(strings.NewReader(s))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '[' {
		return nil, fmt.Errorf("reading %q as a vector: %w", s, dbimp.ErrInvalidValue)
	}
	out := dbimp.Vector[float64]{}
	for dec.PeekKind() != ']' {
		v, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading %q as a vector: %w", s, dbimp.ErrInvalidValue)
		}
		f, err := dbimp.Float64(v)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a vector: %w", s, err)
		}
		out = append(out, f)
	}
	return out, nil
}

// The scan types of the columns.
var (
	typeOfInt64         = reflect.TypeFor[int64]()
	typeOfDecimal       = reflect.TypeFor[*apd.Decimal]()
	typeOfFloat64       = reflect.TypeFor[float64]()
	typeOfString        = reflect.TypeFor[string]()
	typeOfBytes         = reflect.TypeFor[[]byte]()
	typeOfBool          = reflect.TypeFor[bool]()
	typeOfDate          = reflect.TypeFor[dbimp.Date]()
	typeOfTime          = reflect.TypeFor[dbimp.LocalTime]()
	typeOfLocalDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfInstant       = reflect.TypeFor[time.Time]()
	typeOfMap           = reflect.TypeFor[map[string]any]()
	typeOfList          = reflect.TypeFor[[]any]()
	typeOfVector        = reflect.TypeFor[dbimp.Vector[float64]]()
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the column c (D135 and D183). A
// variant is the decoded JSON value, so its type is any.
func scanType(c column) reflect.Type {
	switch c.wire {
	case wireFixed:
		if c.isInteger() {
			return typeOfInt64
		}
		return typeOfDecimal
	case wireReal:
		return typeOfFloat64
	case wireText:
		return typeOfString
	case wireBinary:
		return typeOfBytes
	case wireBoolean:
		return typeOfBool
	case wireDate:
		return typeOfDate
	case wireTime:
		return typeOfTime
	case wireTimestampNTZ:
		return typeOfLocalDateTime
	case wireTimestampLTZ, wireTimestampTZ:
		return typeOfInstant
	case wireObject, wireGeography, wireGeometry:
		return typeOfMap
	case wireArray:
		return typeOfList
	case wireVector:
		return typeOfVector
	}
	return typeOfAny
}
