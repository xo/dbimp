package elasticsearch

import (
	"bytes"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The types of a column, as the type of an entry of columns names them
// (measured). The server writes a mapping type or an SQL type in lower case.
// The driver reads a constant_keyword and a wildcard column as keyword, a
// match_only_text column as text, and a date_nanos column as datetime,
// because the server names them so (measured).
const (
	typeNull          = "null"
	typeBoolean       = "boolean"
	typeByte          = "byte"
	typeShort         = "short"
	typeInteger       = "integer"
	typeLong          = "long"
	typeUnsignedLong  = "unsigned_long"
	typeHalfFloat     = "half_float"
	typeFloat         = "float"
	typeDouble        = "double"
	typeScaledFloat   = "scaled_float"
	typeKeyword       = "keyword"
	typeText          = "text"
	typeBinary        = "binary"
	typeIP            = "ip"
	typeVersion       = "version"
	typeDatetime      = "datetime"
	typeDate          = "date"
	typeTime          = "time"
	typeGeoPoint      = "geo_point"
	typeGeoShape      = "geo_shape"
	typeShape         = "shape"
	typeIntervalYear  = "interval_year"
	typeIntervalMonth = "interval_month"
	typeIntervalYM    = "interval_year_to_month"
	intervalPrefix    = "interval_"
)

// isMonths reports whether the interval type t counts months, which a
// dbimp.Interval holds. Every other interval counts hours and less, which a
// time.Duration holds (D167).
func isMonths(t string) bool {
	return t == typeIntervalYear || t == typeIntervalMonth || t == typeIntervalYM
}

// decode returns the value v of a column of the type typ as its Go value
// (D135 and D167). A NULL is nil for every type, and a field that a document
// lacks is NULL (measured). A type that the driver does not know is the
// decoded JSON value.
func decode(typ string, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch typ {
	case typeBoolean:
		return dbimp.Bool(v)
	case typeByte, typeShort, typeInteger, typeLong:
		return decodeInteger(v)
	case typeUnsignedLong:
		return decodeUnsigned(v)
	case typeHalfFloat, typeFloat, typeDouble, typeScaledFloat:
		return decodeFloat(v)
	case typeKeyword, typeText, typeIP, typeVersion, typeGeoPoint, typeGeoShape, typeShape:
		return dbimp.String(v)
	case typeBinary:
		return decodeBinary(v)
	case typeDatetime:
		return decodeDatetime(v)
	case typeDate:
		return decodeDate(v)
	case typeTime:
		return decodeTime(v)
	case typeNull:
		return dbimp.Any(v)
	}
	if strings.HasPrefix(typ, intervalPrefix) {
		return decodeInterval(typ, v)
	}
	return dbimp.Any(v)
}

// decodeInteger returns a byte, a short, an integer or a long. A number with
// a fraction of zero, such as the 100.0 that HISTOGRAM gives in a column of
// the type integer, is its integer (measured). Any other fraction fails.
func decodeInteger(v jsontext.Value) (int64, error) {
	if v.Kind() != '0' {
		return dbimp.Int64(v)
	}
	text := string(bytes.TrimSpace(v))
	whole, frac, ok := strings.Cut(text, ".")
	if ok && strings.Trim(frac, "0") == "" {
		return dbimp.Int64(jsontext.Value(whole))
	}
	return dbimp.Int64(v)
}

// decodeUnsigned returns an unsigned_long, which keeps every digit to
// 18446744073709551615 in a JSON number (measured).
func decodeUnsigned(v jsontext.Value) (uint64, error) {
	if v.Kind() != '0' {
		return 0, fmt.Errorf("reading %s as an unsigned_long: %w", v.Kind(), dbimp.ErrInvalidValue)
	}
	text := string(bytes.TrimSpace(v))
	if whole, frac, ok := strings.Cut(text, "."); ok && strings.Trim(frac, "0") == "" {
		text = whole
	}
	u, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %s as an unsigned_long: %w", text, dbimp.ErrInvalidValue)
	}
	return u, nil
}

// decodeFloat returns a number, or the string "NaN", "Infinity" or
// "-Infinity" (measured).
func decodeFloat(v jsontext.Value) (float64, error) {
	if v.Kind() != '"' {
		return dbimp.Float64(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return 0, err
	}
	switch s {
	case "NaN":
		return math.NaN(), nil
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	}
	return 0, fmt.Errorf("reading %q as a float: %w", s, dbimp.ErrInvalidValue)
}

// decodeBinary returns a binary value, which the server writes as a string
// of base64 (measured).
func decodeBinary(v jsontext.Value) ([]byte, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("reading %q as base64: %w", s, dbimp.ErrInvalidValue)
	}
	return b, nil
}

// decodeDatetime returns a datetime, which the server writes in ISO 8601
// with every digit of the second and the offset of time_zone, such as
// 2026-10-01T12:34:56.123456789+05:30 (measured).
func decodeDatetime(v jsontext.Value) (time.Time, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading %q as a datetime: %w", s, dbimp.ErrInvalidValue)
	}
	// time.Parse gives the local zone of the host when the offset is its
	// offset, so the zone is set from the offset alone.
	if _, off := t.Zone(); off != 0 {
		return t.In(time.FixedZone("", off)), nil
	}
	return t.UTC(), nil
}

// decodeDate returns a date, which the server writes as the text of its
// midnight in time_zone, such as 2026-10-01T00:00:00.000+05:30. The driver
// reads the day from the text before the T, because the offset of time_zone
// can move the day in a time.Time in UTC (D167).
func decodeDate(v jsontext.Value) (dbimp.Date, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return dbimp.Date{}, err
	}
	day, _, ok := strings.Cut(s, "T")
	if !ok {
		return dbimp.Date{}, fmt.Errorf("reading %q as a date: %w", s, dbimp.ErrInvalidValue)
	}
	d, err := dbimp.ParseDate(day)
	if err != nil {
		return dbimp.Date{}, fmt.Errorf("reading %q as a date: %w", s, dbimp.ErrInvalidValue)
	}
	return d, nil
}

// decodeTime returns a time, which the server writes with milliseconds and
// an offset, such as 12:34:56.789Z (measured).
func decodeTime(v jsontext.Value) (dbimp.OffsetTime, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return dbimp.OffsetTime{}, err
	}
	t, err := dbimp.ParseOffsetTime(s)
	if err != nil {
		return dbimp.OffsetTime{}, fmt.Errorf("reading %q as a time: %w", s, dbimp.ErrInvalidValue)
	}
	return t, nil
}

// decodeInterval returns an interval. The server writes an interval of
// years and months as an ISO 8601 period, such as P1Y2M, and an interval of
// days and time as a duration in hours, such as PT48H (measured). The first
// is a dbimp.Interval, and the second a time.Duration (D167).
func decodeInterval(typ string, v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	iv, err := dbimp.ParseInterval(s)
	if err != nil {
		return nil, fmt.Errorf("reading %q as an interval: %w", s, dbimp.ErrInvalidValue)
	}
	if isMonths(typ) {
		if iv.Days != 0 || iv.Nanoseconds != 0 {
			return nil, fmt.Errorf("reading %q as an interval of months: the server writes years and months only: %w", s, dbimp.ErrInvalidValue)
		}
		return iv, nil
	}
	if iv.Months != 0 || iv.Days != 0 {
		return nil, fmt.Errorf("reading %q as a duration: the server writes hours and no days or months: %w", s, dbimp.ErrInvalidValue)
	}
	return time.Duration(iv.Nanoseconds), nil
}

// The scan types of the columns.
var (
	typeOfInt64    = reflect.TypeFor[int64]()
	typeOfUint64   = reflect.TypeFor[uint64]()
	typeOfFloat64  = reflect.TypeFor[float64]()
	typeOfBool     = reflect.TypeFor[bool]()
	typeOfString   = reflect.TypeFor[string]()
	typeOfBytes    = reflect.TypeFor[[]byte]()
	typeOfTime     = reflect.TypeFor[time.Time]()
	typeOfDate     = reflect.TypeFor[dbimp.Date]()
	typeOfClock    = reflect.TypeFor[dbimp.OffsetTime]()
	typeOfInterval = reflect.TypeFor[dbimp.Interval]()
	typeOfDuration = reflect.TypeFor[time.Duration]()
	typeOfAny      = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of a column of the type typ
// (D135 and D167). A type that the driver does not know is any.
func scanType(typ string) reflect.Type {
	switch typ {
	case typeBoolean:
		return typeOfBool
	case typeByte, typeShort, typeInteger, typeLong:
		return typeOfInt64
	case typeUnsignedLong:
		return typeOfUint64
	case typeHalfFloat, typeFloat, typeDouble, typeScaledFloat:
		return typeOfFloat64
	case typeKeyword, typeText, typeIP, typeVersion, typeGeoPoint, typeGeoShape, typeShape:
		return typeOfString
	case typeBinary:
		return typeOfBytes
	case typeDatetime:
		return typeOfTime
	case typeDate:
		return typeOfDate
	case typeTime:
		return typeOfClock
	}
	if strings.HasPrefix(typ, intervalPrefix) {
		if isMonths(typ) {
			return typeOfInterval
		}
		return typeOfDuration
	}
	return typeOfAny
}

// parameters returns the arguments as the array params, in the order of the
// ? of the statement, which the server binds (D167). The server gives each
// value a type from its JSON kind (measured). Elasticsearch has no named
// parameter, so a named argument is refused.
func parameters(args []driver.NamedValue) ([]any, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]any, 0, len(args))
	for _, arg := range args {
		if arg.Name != "" {
			return nil, fmt.Errorf("binding the argument %s: Elasticsearch has no named parameter: %w", arg.Name, dbimp.ErrArguments)
		}
		p, err := bind(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %d: %w", arg.Ordinal, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// bind returns v as an entry of params. The server takes a plain JSON value
// and no type, so a time.Time is a string in ISO 8601, which the server
// compares with a datetime column and keeps to the nanosecond (measured).
func bind(v any) (any, error) {
	switch v := v.(type) {
	case nil, bool, int64, string:
		return v, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("writing %v: JSON has no number for it: %w", v, dbimp.ErrInvalidValue)
		}
		return v, nil
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano), nil
	case []byte:
		return nil, fmt.Errorf("writing a []byte: Elasticsearch has no binary parameter: %w", dbimp.ErrNotSupported)
	}
	return nil, fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}
