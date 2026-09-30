package influxdb

import (
	"bytes"
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

// wireType is one type of the type table of step 10 of docs/DRIVER.md. Its
// name is the name of the type in features.json.
type wireType struct {
	// name is the name of the type.
	name string
	// arrow is the data_type that DESCRIBE gives in SQL (measured).
	arrow string
	// goes is the Go type of a value.
	goes reflect.Type
	// influxql says what InfluxQL gives, which names no types (D83).
	influxql string
}

// wireTypes are the types of a field, a tag and the time that a line of line
// protocol writes (measured). SQL can make other types in an expression, and
// columnType reads each of them too.
var wireTypes = []wireType{
	{"null", "Null", nil, "nil"},
	{"float", "Float64", reflect.TypeFor[float64](), "float64, or int64 for a whole number"},
	{"integer", "Int64", reflect.TypeFor[int64](), "int64"},
	{"unsigned integer", "UInt64", reflect.TypeFor[uint64](), "int64, or uint64 above the range of int64"},
	{"string", "Utf8", reflect.TypeFor[string](), "string"},
	{"boolean", "Boolean", reflect.TypeFor[bool](), "bool"},
	{"tag", "Dictionary(Int32, Utf8)", reflect.TypeFor[string](), "string"},
	{"timestamp", "Timestamp(ns)", reflect.TypeFor[time.Time](), "time.Time, in the column time"},
}

// colType is the type of one column of SQL, from its data_type in DESCRIBE
// (D80).
type colType struct {
	// name is the name of the column.
	name string
	// arrow is the data_type, as the server wrote it.
	arrow string
	// nullable is whether the column can hold a NULL.
	nullable bool
	// goes is the Go type of a value, or nil when the values of the column
	// have no single Go type.
	goes reflect.Type
	// decode turns one value of the column into its Go value.
	decode func(jsontext.Value) (any, error)
	// precision and scale are those of a decimal, or -1.
	precision, scale int64
	// float is true for a float, whose value that is not finite arrives as
	// an explicit null (D80).
	float bool
}

// scanType returns the Go type of a value of the column, which Rows.Next
// returns, for a column that can hold a NULL too. ColumnTypeNullable says
// whether it can (D136).
func (t *colType) scanType() reflect.Type {
	if t == nil || t.goes == nil {
		return reflect.TypeFor[any]()
	}
	return t.goes
}

// columnType returns the type of a column whose data_type is arrow, and which
// can hold a NULL when nullable is true. A type that the driver does not name
// decodes as JSON does: a string, a number, a list or an object.
func columnType(arrow string, nullable bool) *colType {
	t := &colType{arrow: arrow, nullable: nullable, precision: -1, scale: -1}
	name, args, _ := strings.Cut(arrow, "(")
	args = strings.TrimSuffix(args, ")")
	switch name {
	case "Int8", "Int16", "Int32", "Int64":
		t.goes, t.decode = reflect.TypeFor[int64](), decodeInt
	case "UInt8", "UInt16", "UInt32", "UInt64":
		t.goes, t.decode = reflect.TypeFor[uint64](), decodeUint
	case "Float16", "Float32", "Float64":
		t.goes, t.decode, t.float = reflect.TypeFor[float64](), decodeFloat, true
	case "Decimal32", "Decimal64", "Decimal128", "Decimal256":
		t.goes, t.decode = reflect.TypeFor[*apd.Decimal](), decodeDecimal
		p, s, _ := strings.Cut(args, ",")
		if pv, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			t.precision = pv
		}
		if sv, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			t.scale = sv
		}
	case "Utf8", "LargeUtf8", "Utf8View":
		t.goes, t.decode = reflect.TypeFor[string](), decodeString
	case "Dictionary":
		// A tag is a dictionary of strings (measured).
		if _, value, _ := strings.Cut(args, ","); strings.Contains(value, "Utf8") {
			t.goes, t.decode = reflect.TypeFor[string](), decodeString
		}
	case "Boolean":
		t.goes, t.decode = reflect.TypeFor[bool](), decodeBool
	case "Timestamp":
		t.goes, t.decode = reflect.TypeFor[time.Time](), decodeTimestamp
	case "Date32", "Date64":
		t.goes, t.decode = reflect.TypeFor[dbimp.Date](), decodeDate
	case "Time32", "Time64":
		t.goes, t.decode = reflect.TypeFor[dbimp.LocalTime](), decodeLocalTime
	case "Interval":
		t.goes, t.decode = reflect.TypeFor[dbimp.Interval](), decodeInterval
	case "Duration":
		t.goes, t.decode = reflect.TypeFor[time.Duration](), decodeDuration
	case "Binary", "LargeBinary", "BinaryView", "FixedSizeBinary":
		// The server writes binary data in hex (measured).
		t.goes, t.decode = reflect.TypeFor[[]byte](), decodeHex
	}
	if t.decode == nil {
		// A parameter is the type Null, whatever its value (measured), so a
		// column of Null decodes as JSON does, as every other type does
		// that the driver does not name.
		t.decode = dbimp.Any
	}
	return t
}

// databaseTypeName returns the name of the type of the column in upper case,
// such as INT64 or TIMESTAMP(NS).
func (t *colType) databaseTypeName() string {
	if t == nil {
		return ""
	}
	return strings.ToUpper(t.arrow)
}

func decodeInt(v jsontext.Value) (any, error) {
	return dbimp.Int64(v)
}

func decodeUint(v jsontext.Value) (any, error) {
	if v.Kind() != '0' {
		return nil, fmt.Errorf("reading %s as uint64: %w", v.Kind(), dbimp.ErrInvalidValue)
	}
	u, err := strconv.ParseUint(string(bytes.TrimSpace(v)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("reading %s as uint64: %w", v, dbimp.ErrInvalidValue)
	}
	return u, nil
}

// decodeFloat reads a float. The driver asks for JSON, where a float that is
// not finite is null (D80). It also reads the text NaN, inf and -inf, which
// other forms of the answer write, such as CSV (measured), in case a server
// sends one.
func decodeFloat(v jsontext.Value) (any, error) {
	if v.Kind() == '"' {
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "nan", "+nan", "-nan":
			return math.NaN(), nil
		case "inf", "+inf", "infinity", "+infinity":
			return math.Inf(1), nil
		case "-inf", "-infinity":
			return math.Inf(-1), nil
		}
		return nil, fmt.Errorf("reading %q as float64: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.Float64(v)
}

func decodeDecimal(v jsontext.Value) (any, error) {
	return dbimp.Decimal(v)
}

func decodeString(v jsontext.Value) (any, error) {
	return dbimp.String(v)
}

func decodeBool(v jsontext.Value) (any, error) {
	return dbimp.Bool(v)
}

// localLayout is the form of a timestamp with no zone, such as
// 2024-01-02T03:04:05.123456789 (measured).
const localLayout = "2006-01-02T15:04:05.999999999"

// decodeTimestamp reads a timestamp. One with no zone is in UTC, and one with
// a zone, such as Timestamp(ns, "UTC"), ends with Z or an offset (measured).
func decodeTimestamp(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	return parseTime(s)
}

// parseTime reads a time in RFC 3339, with or without a zone.
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation(localLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading the time %q: %w", s, dbimp.ErrInvalidValue)
	}
	return t, nil
}

// decodeDate reads a Date32, such as 2024-01-02, or a Date64, such as
// 2024-01-02T00:00:00, as a Date (measured on 3.11.5, D138).
func decodeDate(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	day, _, _ := strings.Cut(s, "T")
	return dbimp.ParseDate(day)
}

// decodeLocalTime reads a Time32 or a Time64, such as 12:30:00.500, as a
// LocalTime (measured on 3.11.5, D138).
func decodeLocalTime(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	return dbimp.ParseLocalTime(s)
}

// decodeInterval reads an Interval, which the server writes as parts with
// their units, each with its own sign, such as 14 mons 3 days 4 hours
// 5 mins 6.500000000 secs and -1 mons -2 days, and zero as an empty string
// (measured on 3.11.5, D138).
func decodeInterval(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	iv, ok := parseInterval(s)
	if !ok {
		return nil, fmt.Errorf("reading %q as an interval: %w", s, dbimp.ErrInvalidValue)
	}
	return iv, nil
}

// The nanoseconds of each unit of the time of an interval of the server.
var intervalUnits = map[string]int64{
	"hour": int64(time.Hour), "min": int64(time.Minute), "sec": int64(time.Second),
	"millisecond": int64(time.Millisecond), "microsecond": int64(time.Microsecond), "nanosecond": 1,
}

// parseInterval reads the text of decodeInterval.
func parseInterval(s string) (dbimp.Interval, bool) {
	fields := strings.Fields(s)
	if len(fields)%2 != 0 {
		return dbimp.Interval{}, false
	}
	var months, days, nanos int64
	for i := 0; i < len(fields); i += 2 {
		num, unit := fields[i], strings.TrimSuffix(fields[i+1], "s")
		switch unit {
		case "year", "mon", "day":
			n, err := strconv.ParseInt(num, 10, 64)
			if err != nil {
				return dbimp.Interval{}, false
			}
			switch unit {
			case "year":
				months += n * 12
			case "mon":
				months += n
			default:
				days += n
			}
			continue
		}
		per, ok := intervalUnits[unit]
		if !ok {
			return dbimp.Interval{}, false
		}
		n, ok := parseNanos(num, per)
		if !ok || n > 0 && nanos > math.MaxInt64-n || n < 0 && nanos < math.MinInt64-n {
			return dbimp.Interval{}, false
		}
		nanos += n
	}
	if months < math.MinInt32 || months > math.MaxInt32 || days < math.MinInt32 || days > math.MaxInt32 {
		return dbimp.Interval{}, false
	}
	return dbimp.Interval{Months: int32(months), Days: int32(days), Nanoseconds: nanos}, true
}

// parseNanos reads a number of a unit of per nanoseconds, with a fraction for
// a unit of a second or more, such as -0.500000000, as nanoseconds. A
// negative number is built down from zero, so that the smallest int64 fits.
func parseNanos(num string, per int64) (int64, bool) {
	neg := strings.HasPrefix(num, "-")
	whole, frac, _ := strings.Cut(num, ".")
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || len(frac) > 9 || frac != "" && per < int64(time.Second) || w > math.MaxInt64/per || w < math.MinInt64/per {
		return 0, false
	}
	n := w * per
	if frac == "" {
		return n, true
	}
	f, err := strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
	if err != nil {
		return 0, false
	}
	f *= per / int64(time.Second)
	if neg {
		if n < math.MinInt64+f {
			return 0, false
		}
		return n - f, true
	}
	if n > math.MaxInt64-f {
		return 0, false
	}
	return n + f, true
}

// formatInterval writes iv as the server casts it, such as
// 14 mons -3 days -0.000001000 secs (measured on 3.11.5).
func formatInterval(iv dbimp.Interval) string {
	sign, secs, nanos := "", iv.Nanoseconds/1e9, iv.Nanoseconds%1e9
	if iv.Nanoseconds < 0 {
		// Each part is negated after the division, so that the smallest
		// int64 fits.
		sign, secs, nanos = "-", -secs, -nanos
	}
	return fmt.Sprintf("%d mons %d days %s%d.%09d secs", iv.Months, iv.Days, sign, secs, nanos)
}

// decodeDuration reads a Duration, which the server writes in ISO 8601 as
// seconds, such as PT1.5S, -PT2.5S and PT9223372036.854775807S, and zero as
// P0D (measured on 3.11.5).
func decodeDuration(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	d, ok := parseDuration(s)
	if !ok {
		return nil, fmt.Errorf("reading %q as a duration: %w", s, dbimp.ErrInvalidValue)
	}
	return d, nil
}

// parseDuration parses the ISO 8601 text of decodeDuration.
func parseDuration(s string) (time.Duration, bool) {
	text, neg := strings.CutPrefix(s, "-")
	if text == "P0D" {
		return 0, true
	}
	text, ok := strings.CutPrefix(text, "PT")
	if !ok {
		return 0, false
	}
	if text, ok = strings.CutSuffix(text, "S"); !ok {
		return 0, false
	}
	whole, frac, _ := strings.Cut(text, ".")
	if whole == "" || len(frac) > 9 || strings.ContainsAny(whole+frac, "+-") {
		return 0, false
	}
	sec, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || sec > math.MaxInt64/int64(time.Second) {
		return 0, false
	}
	var ns int64
	if frac != "" {
		if ns, err = strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64); err != nil {
			return 0, false
		}
	}
	d := sec * int64(time.Second)
	if d > math.MaxInt64-ns {
		return 0, false
	}
	d += ns
	if neg {
		d = -d
	}
	return time.Duration(d), true
}

func decodeHex(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("reading %q as hex: %w", s, dbimp.ErrInvalidValue)
	}
	return b, nil
}

// decodeInfluxQL decodes one value of InfluxQL, which names no types (D83). A
// number with no fraction and no exponent is an int64, or a uint64 above the
// range of int64. Any other number is a float64. The value of the column time
// is a time.Time.
func decodeInfluxQL(v jsontext.Value, isTime bool) (any, error) {
	switch {
	case dbimp.IsNull(v):
		return nil, nil
	case v.Kind() == '0':
		text := string(bytes.TrimSpace(v))
		if strings.ContainsAny(text, ".eE") {
			return dbimp.Float64(v)
		}
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i, nil
		}
		if u, err := strconv.ParseUint(text, 10, 64); err == nil {
			return u, nil
		}
		return dbimp.Float64(v)
	case v.Kind() == '"' && isTime:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		return parseTime(s)
	}
	return dbimp.Any(v)
}
