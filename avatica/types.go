package avatica

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The names of the types that the driver reads, after normalize (D155).
const (
	typeTinyint      = "TINYINT"
	typeSmallint     = "SMALLINT"
	typeInteger      = "INTEGER"
	typeBigint       = "BIGINT"
	typeUnsignedInt  = "UNSIGNED_INT"
	typeUnsignedLong = "UNSIGNED_LONG"
	typeDecimal      = "DECIMAL"
	typeDouble       = "DOUBLE"
	typeFloat        = "FLOAT"
	typeBoolean      = "BOOLEAN"
	typeChar         = "CHAR"
	typeVarchar      = "VARCHAR"
	typeBinary       = "BINARY"
	typeVarbinary    = "VARBINARY"
	typeDate         = "DATE"
	typeTime         = "TIME"
	typeTimestamp    = "TIMESTAMP"
	typeInterval     = "INTERVAL"
	typeArray        = "ARRAY"
	typeUUID         = "UUID"
)

// colType is the type of a column, as the signature of a result names it.
type colType struct {
	// Type is "scalar" or "array".
	Type string `json:"type"`
	// Name is the name of the type, such as INTEGER, CHARACTER, INTERVAL DAY
	// TO SECOND or INTEGER ARRAY (measured).
	Name string `json:"name"`
	// Rep is how the server holds the value. The driver does not read it,
	// because it disagrees with the value (D155).
	Rep string `json:"rep"`
	// Component is the type of the elements of an array.
	Component *colType `json:"component"`
}

// column is one column of the signature of a result.
type column struct {
	Label     string  `json:"label"`
	Type      colType `json:"type"`
	Precision int64   `json:"precision"`
	Scale     int64   `json:"scale"`
	// Nullable is 0 for a column that holds no NULL, 1 for one that can, and
	// 2 when the server does not know, as JDBC names them.
	Nullable int `json:"nullable"`
}

// kind returns the name of the type of t that the driver reads, such as
// CHAR for CHARACTER, INTERVAL for INTERVAL DAY TO SECOND, and ARRAY for an
// array (D155).
func (t colType) kind() string {
	if t.Type == "array" {
		return typeArray
	}
	name := strings.ToUpper(strings.TrimSpace(t.Name))
	switch {
	case name == "CHARACTER":
		return typeChar
	case name == "CHARACTER VARYING":
		return typeVarchar
	case name == "NUMERIC":
		return typeDecimal
	case name == "REAL" || name == "DOUBLE PRECISION":
		return typeDouble
	case strings.HasPrefix(name, typeInterval+" "):
		return typeInterval
	}
	return name
}

// decode returns the value v of a column of type t as its Go value (D155). A
// NULL is nil (D18). A type that the driver does not name keeps the Go type
// of its JSON form.
func decode(t colType, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch t.kind() {
	case typeTinyint, typeSmallint, typeInteger, typeBigint, typeUnsignedInt, typeUnsignedLong:
		return dbimp.Int64(v)
	case typeDecimal:
		return dbimp.Decimal(v)
	case typeDouble, typeFloat:
		return decodeFloat(v)
	case typeBoolean:
		return dbimp.Bool(v)
	case typeChar, typeVarchar:
		return dbimp.String(v)
	case typeBinary, typeVarbinary:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("reading a binary value in base64: %w", dbimp.ErrInvalidValue)
		}
		return b, nil
	case typeDate:
		days, err := dbimp.Int64(v)
		if err != nil {
			return nil, err
		}
		return dbimp.DateOf(time.Unix(days*secondsPerDay, 0).UTC()), nil
	case typeTime:
		ms, err := dbimp.Int64(v)
		if err != nil {
			return nil, err
		}
		return dbimp.LocalTimeOf(time.UnixMilli(ms).UTC()), nil
	case typeTimestamp:
		ms, err := dbimp.Int64(v)
		if err != nil {
			return nil, err
		}
		return dbimp.LocalDateTimeOf(time.UnixMilli(ms).UTC()), nil
	case typeInterval:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		return parseInterval(t.Name, s)
	case typeArray:
		return decodeArray(t, v)
	case typeUUID:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		u, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("reading the UUID %q: %w", s, dbimp.ErrInvalidValue)
		}
		return u, nil
	}
	return dbimp.Any(v)
}

// secondsPerDay is the length of a day of a DATE, which the server sends as
// days since 1970-01-01.
const secondsPerDay = 24 * 60 * 60

// decodeFloat returns a DOUBLE, which is a JSON number, or the string
// Infinity, -Infinity or NaN (measured).
func decodeFloat(v jsontext.Value) (float64, error) {
	if v.Kind() != '"' {
		return dbimp.Float64(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return 0, err
	}
	switch s {
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	case "NaN":
		return math.NaN(), nil
	}
	return 0, fmt.Errorf("reading the float %q: %w", s, dbimp.ErrInvalidValue)
}

// decodeArray returns an ARRAY, which is a JSON array, as a []any of the Go
// values of its elements (D155).
func decodeArray(t colType, v jsontext.Value) ([]any, error) {
	var elem colType
	if t.Component != nil {
		elem = *t.Component
	}
	dec := jsontext.NewDecoder(strings.NewReader(string(v)))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '[' {
		return nil, fmt.Errorf("reading an array: %s is not an array: %w", v, dbimp.ErrInvalidValue)
	}
	out := []any{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading an array: %w", err)
		}
		e, err := decode(elem, ev)
		if err != nil {
			return nil, fmt.Errorf("reading an element of an array: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}

// The fields of an interval of HSQLDB, in the order that its text holds
// them.
var (
	yearMonthFields = []string{"YEAR", "MONTH"}
	dayTimeFields   = []string{"DAY", "HOUR", "MINUTE", "SECOND"}
)

// parseInterval reads the text s of an interval of HSQLDB whose type is
// name, such as "1 02:03:04.500000" for INTERVAL DAY TO SECOND, or "-1-02"
// for INTERVAL YEAR TO MONTH (measured). The qualifier of name says which
// field each number of s is.
func parseInterval(name, s string) (dbimp.Interval, error) {
	var iv dbimp.Interval
	fields, err := qualifier(name)
	if err != nil {
		return iv, err
	}
	text, neg := strings.CutPrefix(strings.TrimSpace(s), "-")
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ':' || r == '-'
	})
	if len(parts) != len(fields) {
		return iv, fmt.Errorf("reading the interval %q of the type %s: %w", s, name, dbimp.ErrInvalidValue)
	}
	for i, f := range fields {
		if f == "SECOND" {
			d, err := time.ParseDuration(parts[i] + "s")
			if err != nil || strings.HasPrefix(parts[i], "+") {
				return iv, fmt.Errorf("reading the seconds of the interval %q: %w", s, dbimp.ErrInvalidValue)
			}
			iv.Nanoseconds += int64(d)
			continue
		}
		n, err := strconv.ParseInt(parts[i], 10, 32)
		if err != nil || n < 0 {
			return iv, fmt.Errorf("reading the %s of the interval %q: %w", strings.ToLower(f), s, dbimp.ErrInvalidValue)
		}
		switch f {
		case "YEAR":
			iv.Months += int32(n) * 12
		case "MONTH":
			iv.Months += int32(n)
		case "DAY":
			iv.Days += int32(n)
		case "HOUR":
			iv.Nanoseconds += n * int64(time.Hour)
		case "MINUTE":
			iv.Nanoseconds += n * int64(time.Minute)
		}
	}
	if neg {
		iv.Months, iv.Days, iv.Nanoseconds = -iv.Months, -iv.Days, -iv.Nanoseconds
	}
	return iv, nil
}

// qualifier returns the fields of the interval type name, such as DAY, HOUR,
// MINUTE and SECOND for INTERVAL DAY TO SECOND. A precision in the name,
// such as SECOND(6), is dropped.
func qualifier(name string) ([]string, error) {
	q, ok := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(name)), typeInterval+" ")
	if !ok {
		return nil, fmt.Errorf("reading the interval type %q: %w", name, dbimp.ErrInvalidValue)
	}
	start, end, hasEnd := strings.Cut(q, " TO ")
	trim := func(f string) string {
		f, _, _ = strings.Cut(strings.TrimSpace(f), "(")
		return f
	}
	start = trim(start)
	if !hasEnd {
		end = start
	}
	end = trim(end)
	for _, all := range [][]string{yearMonthFields, dayTimeFields} {
		i, j := slices.Index(all, start), slices.Index(all, end)
		if i >= 0 && j >= i {
			return all[i : j+1], nil
		}
	}
	return nil, fmt.Errorf("reading the interval type %q: %w", name, dbimp.ErrInvalidValue)
}

// The scan types of the columns.
var (
	typeOfInt64         = reflect.TypeFor[int64]()
	typeOfDecimal       = reflect.TypeFor[*apd.Decimal]()
	typeOfFloat64       = reflect.TypeFor[float64]()
	typeOfBool          = reflect.TypeFor[bool]()
	typeOfString        = reflect.TypeFor[string]()
	typeOfBytes         = reflect.TypeFor[[]byte]()
	typeOfDate          = reflect.TypeFor[dbimp.Date]()
	typeOfLocalTime     = reflect.TypeFor[dbimp.LocalTime]()
	typeOfLocalDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfInterval      = reflect.TypeFor[dbimp.Interval]()
	typeOfArray         = reflect.TypeFor[[]any]()
	typeOfUUID          = reflect.TypeFor[uuid.UUID]()
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of a column of type t (D155).
func scanType(t colType) reflect.Type {
	switch t.kind() {
	case typeTinyint, typeSmallint, typeInteger, typeBigint, typeUnsignedInt, typeUnsignedLong:
		return typeOfInt64
	case typeDecimal:
		return typeOfDecimal
	case typeDouble, typeFloat:
		return typeOfFloat64
	case typeBoolean:
		return typeOfBool
	case typeChar, typeVarchar:
		return typeOfString
	case typeBinary, typeVarbinary:
		return typeOfBytes
	case typeDate:
		return typeOfDate
	case typeTime:
		return typeOfLocalTime
	case typeTimestamp:
		return typeOfLocalDateTime
	case typeInterval:
		return typeOfInterval
	case typeArray:
		return typeOfArray
	case typeUUID:
		return typeOfUUID
	}
	return typeOfAny
}

// The reps of a TypedValue that the driver sends (docs/AVATICA.md, Parameters).
const (
	repNull       = "NULL"
	repLong       = "LONG"
	repDouble     = "DOUBLE"
	repBoolean    = "BOOLEAN"
	repString     = "STRING"
	repByteString = "BYTE_STRING"
	repDate       = "JAVA_SQL_DATE"
	repTime       = "JAVA_SQL_TIME"
	repTimestamp  = "JAVA_SQL_TIMESTAMP"
)

// typedValue is one TypedValue of Avatica, which the server binds by its rep.
type typedValue struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// arg returns the argument v as a TypedValue (D158). A decimal goes as a
// STRING, because the server reads a NUMBER as a double. A time goes in
// milliseconds, which is all that the server keeps.
func arg(v any) (typedValue, error) {
	switch v := v.(type) {
	case nil:
		return typedValue{Type: repNull}, nil
	case int64:
		return typedValue{Type: repLong, Value: v}, nil
	case uint64:
		if v > math.MaxInt64 {
			return typedValue{}, fmt.Errorf("writing %d: the largest integer is 9223372036854775807: %w", v, dbimp.ErrInvalidValue)
		}
		return typedValue{Type: repLong, Value: int64(v)}, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return typedValue{}, fmt.Errorf("writing %v: JSON has no infinity and no NaN: %w", v, dbimp.ErrInvalidValue)
		}
		return typedValue{Type: repDouble, Value: v}, nil
	case bool:
		return typedValue{Type: repBoolean, Value: v}, nil
	case string:
		return typedValue{Type: repString, Value: v}, nil
	case []byte:
		return typedValue{Type: repByteString, Value: base64.StdEncoding.EncodeToString(v)}, nil
	case *apd.Decimal:
		if v == nil {
			return typedValue{Type: repNull}, nil
		}
		return typedValue{Type: repString, Value: v.Text('f')}, nil
	case uuid.UUID:
		return typedValue{Type: repString, Value: v.String()}, nil
	case dbimp.Date:
		return typedValue{Type: repDate, Value: v.In(time.UTC).Unix() / secondsPerDay}, nil
	case dbimp.LocalTime:
		t := v.In(time.UTC)
		return typedValue{Type: repTime, Value: t.Sub(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)).Milliseconds()}, nil
	case dbimp.LocalDateTime:
		return typedValue{Type: repTimestamp, Value: v.In(time.UTC).UnixMilli()}, nil
	case time.Time:
		return typedValue{Type: repTimestamp, Value: v.UnixMilli()}, nil
	}
	return typedValue{}, fmt.Errorf("writing a value of the type %T: %w", v, dbimp.ErrInvalidValue)
}

// isArg reports whether CheckNamedValue keeps v, because arg writes it
// itself (D158).
func isArg(v any) bool {
	switch v.(type) {
	case uint64, *apd.Decimal, uuid.UUID, dbimp.Date, dbimp.LocalTime, dbimp.LocalDateTime:
		return true
	}
	return false
}
