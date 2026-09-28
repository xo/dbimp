package influxdb

import (
	"bytes"
	"database/sql"
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

// scanType returns the Go type that a value of the column scans into best:
// sql.Null of the type when the column can hold a NULL.
func (t *colType) scanType() reflect.Type {
	if t == nil || t.goes == nil {
		return reflect.TypeFor[any]()
	}
	if !t.nullable {
		return t.goes
	}
	switch t.goes {
	case reflect.TypeFor[int64]():
		return reflect.TypeFor[sql.Null[int64]]()
	case reflect.TypeFor[uint64]():
		return reflect.TypeFor[sql.Null[uint64]]()
	case reflect.TypeFor[float64]():
		return reflect.TypeFor[sql.Null[float64]]()
	case reflect.TypeFor[string]():
		return reflect.TypeFor[sql.Null[string]]()
	case reflect.TypeFor[bool]():
		return reflect.TypeFor[sql.Null[bool]]()
	case reflect.TypeFor[time.Time]():
		return reflect.TypeFor[sql.Null[time.Time]]()
	}
	// A pointer or a slice holds its NULL as nil.
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
		t.goes, t.decode = reflect.TypeFor[time.Time](), decodeDate
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

// decodeFloat reads a float. A float that is not finite is text in some
// forms of the answer, such as NaN, inf and -inf in CSV (measured), and the
// text reads as its value (D80).
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

// decodeDate reads a date, such as 2024-01-02, at midnight in UTC. A date
// with a time reads as a timestamp.
func decodeDate(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	if t, err := time.ParseInLocation(time.DateOnly, s, time.UTC); err == nil {
		return t, nil
	}
	return parseTime(s)
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
