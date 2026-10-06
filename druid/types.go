package druid

import (
	"bytes"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The SQL types of a column, as the third row of the header names them
// (measured). OTHER is a type that SQL has no name for, and the native type
// of the second row names it, such as COMPLEX<json>.
const (
	typeBigint    = "BIGINT"
	typeInteger   = "INTEGER"
	typeSmallint  = "SMALLINT"
	typeTinyint   = "TINYINT"
	typeFloat     = "FLOAT"
	typeReal      = "REAL"
	typeDouble    = "DOUBLE"
	typeDecimal   = "DECIMAL"
	typeBoolean   = "BOOLEAN"
	typeVarchar   = "VARCHAR"
	typeChar      = "CHAR"
	typeTimestamp = "TIMESTAMP"
	typeDate      = "DATE"
	typeArray     = "ARRAY"
	typeNull      = "NULL"
	typeOther     = "OTHER"
)

// The native types that the driver reads by name (measured). An array is
// ARRAY<T> of the native type T of its elements.
const (
	nativeString  = "STRING"
	nativeLong    = "LONG"
	nativeFloat   = "FLOAT"
	nativeDouble  = "DOUBLE"
	nativeJSON    = "COMPLEX<json>"
	nativeHLL     = "COMPLEX<HLLSketch>"
	complexPrefix = "COMPLEX<"
	arrayPrefix   = "ARRAY<"
)

// column is the type of one column: its SQL type and its native type.
type column struct {
	sql    string
	native string
}

// wire returns the type that gives the Go type of the column: the SQL type,
// or the native type for OTHER.
func (c column) wire() string {
	if c.sql == typeOther {
		return c.native
	}
	return c.sql
}

// decode returns the value v of a column of the type c as its Go value
// (D135 and D164). A NULL is nil for every type.
func decode(c column, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch w := c.wire(); w {
	case typeBigint, typeInteger, typeSmallint, typeTinyint:
		return dbimp.Int64(v)
	case typeFloat, typeReal, typeDouble, typeDecimal:
		// A DECIMAL is a double on the server, whose native type is DOUBLE
		// (D164).
		return decodeFloat(v)
	case typeBoolean:
		return dbimp.Bool(v)
	case typeVarchar:
		return decodeVarchar(v)
	case typeChar:
		return dbimp.String(v)
	case typeTimestamp:
		return decodeTime(v)
	case typeDate:
		t, err := decodeTime(v)
		if err != nil {
			return nil, err
		}
		return dbimp.DateOf(t), nil
	case typeArray:
		return decodeArray(c.native, v)
	case typeNull:
		// Only NULL has the type NULL, such as the literal NULL (measured).
		return dbimp.Any(v)
	case nativeJSON:
		return decodeJSON(v)
	case nativeHLL:
		return decodeSketch(v)
	default:
		if strings.HasPrefix(w, complexPrefix) {
			return decodeComplex(v)
		}
	}
	// A type that the driver does not know reads as the decoded JSON value.
	return dbimp.Any(v)
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

// decodeVarchar returns a VARCHAR as a string. A multi-value string with
// more than one value arrives as the JSON text of an array of strings, such
// as ["x","y"], and is a []any of its strings (D164). The server names both
// with the same types, so a string whose text is such an array of two or
// more strings is read as a multi-value string too.
func decodeVarchar(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	if values, ok := multiValue(s); ok {
		return values, nil
	}
	return s, nil
}

// multiValue returns the values of s, the JSON text of an array of two or
// more strings or nulls, and false for any other text.
func multiValue(s string) ([]any, bool) {
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, false
	}
	dec := jsontext.NewDecoder(strings.NewReader(s))
	if _, err := dec.ReadToken(); err != nil {
		return nil, false
	}
	var out []any
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, false
		}
		switch ev.Kind() {
		case 'n':
			out = append(out, nil)
		case '"':
			e, err := dbimp.String(ev)
			if err != nil {
				return nil, false
			}
			out = append(out, e)
		default:
			return nil, false
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, false
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) || len(out) < 2 {
		return nil, false
	}
	return out, true
}

// decodeTime returns a TIMESTAMP or the midnight of a DATE, which the server
// writes in ISO 8601 with milliseconds and the offset of sqlTimeZone, such
// as 2026-10-01T12:34:56.789Z (measured).
func decodeTime(v jsontext.Value) (time.Time, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading %q as a time: %w", s, dbimp.ErrInvalidValue)
	}
	// time.Parse gives the local zone of the host when the offset is its
	// offset, so the zone is set from the offset alone.
	if _, off := t.Zone(); off != 0 {
		return t.In(time.FixedZone("", off)), nil
	}
	return t.UTC(), nil
}

// decodeArray returns an ARRAY as a []any, with each element decoded by the
// native type of the elements that native names, such as ARRAY<LONG>. The
// server sends the JSON text of the array in a string, unless the context
// sets sqlStringifyArrays to false, when it sends the array (measured).
func decodeArray(native string, v jsontext.Value) ([]any, error) {
	if v.Kind() == '"' {
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		v = jsontext.Value(s)
	}
	if v.Kind() != '[' {
		return nil, fmt.Errorf("reading an ARRAY: %s is not an array: %w", v.Kind(), dbimp.ErrInvalidValue)
	}
	elem := ""
	if inner, ok := strings.CutPrefix(native, arrayPrefix); ok {
		elem, _ = strings.CutSuffix(inner, ">")
	}
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading an ARRAY: %w", err)
	}
	out := []any{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading an ARRAY: %w", err)
		}
		e, err := decodeElement(elem, ev)
		if err != nil {
			return nil, fmt.Errorf("reading an ARRAY: %w", err)
		}
		out = append(out, e)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading an ARRAY: %w", err)
	}
	return out, nil
}

// decodeElement returns one element of an array of the native type elem.
func decodeElement(elem string, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch {
	case elem == nativeString:
		return dbimp.String(v)
	case elem == nativeLong:
		return dbimp.Int64(v)
	case elem == nativeFloat, elem == nativeDouble:
		return decodeFloat(v)
	case strings.HasPrefix(elem, arrayPrefix):
		return decodeArray(elem, v)
	}
	return dbimp.Any(v)
}

// decodeJSON returns a value of COMPLEX<json>, which the server sends as the
// JSON text in a string (measured), as the decoded JSON value.
func decodeJSON(v jsontext.Value) (any, error) {
	if v.Kind() != '"' {
		return dbimp.Any(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	val := jsontext.Value(s)
	if !val.IsValid() {
		return nil, fmt.Errorf("reading %q as JSON: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.Any(val)
}

// decodeSketch returns a value of COMPLEX<HLLSketch>, which the server sends
// as the JSON text of a string of base64 (measured), as its bytes.
func decodeSketch(v jsontext.Value) ([]byte, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	inner, err := dbimp.String(jsontext.Value(s))
	if err != nil {
		return nil, fmt.Errorf("reading a sketch: %w", err)
	}
	b, err := base64.StdEncoding.DecodeString(inner)
	if err != nil {
		return nil, fmt.Errorf("reading %q as base64: %w", inner, dbimp.ErrInvalidValue)
	}
	return b, nil
}

// decodeComplex returns a value of another complex type, such as a theta
// sketch. A string whose text is a JSON string is the base64 of its bytes,
// as a sketch is. A string whose text is other JSON is the decoded JSON
// value, and a string that holds no JSON is that string.
func decodeComplex(v jsontext.Value) (any, error) {
	if v.Kind() != '"' {
		return dbimp.Any(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	val := jsontext.Value(s)
	switch {
	case !val.IsValid():
		return s, nil
	case val.Kind() == '"':
		return decodeSketch(v)
	}
	return dbimp.Any(val)
}

// The scan types of the columns.
var (
	typeOfInt64   = reflect.TypeFor[int64]()
	typeOfFloat64 = reflect.TypeFor[float64]()
	typeOfBool    = reflect.TypeFor[bool]()
	typeOfString  = reflect.TypeFor[string]()
	typeOfBytes   = reflect.TypeFor[[]byte]()
	typeOfTime    = reflect.TypeFor[time.Time]()
	typeOfDate    = reflect.TypeFor[dbimp.Date]()
	typeOfArray   = reflect.TypeFor[[]any]()
	typeOfAny     = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the column c (D135 and D164).
// A VARCHAR is a string, and a multi-value string with more than one value
// is a []any, which a *any takes too.
func scanType(c column) reflect.Type {
	switch c.wire() {
	case typeBigint, typeInteger, typeSmallint, typeTinyint:
		return typeOfInt64
	case typeFloat, typeReal, typeDouble, typeDecimal:
		return typeOfFloat64
	case typeBoolean:
		return typeOfBool
	case typeVarchar, typeChar:
		return typeOfString
	case typeTimestamp:
		return typeOfTime
	case typeDate:
		return typeOfDate
	case typeArray:
		return typeOfArray
	case nativeHLL:
		return typeOfBytes
	}
	return typeOfAny
}

// parameter is one typed parameter of a query (measured).
type parameter struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// parameters returns the arguments as typed parameters, in the order of the
// ? of the statement, which the server binds (D164). Druid has no named
// parameter, so a named argument is refused.
func parameters(args []driver.NamedValue) ([]parameter, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]parameter, 0, len(args))
	for _, arg := range args {
		if arg.Name != "" {
			return nil, fmt.Errorf("binding the argument %s: Druid has no named parameter: %w", arg.Name, dbimp.ErrArguments)
		}
		p, err := bind(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %d: %w", arg.Ordinal, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// bind returns v as a typed parameter. A NULL is a VARCHAR of null, and a
// time.Time is a TIMESTAMP in milliseconds since 1970, which needs no time
// zone (measured).
func bind(v any) (parameter, error) {
	switch v := v.(type) {
	case nil:
		return parameter{Type: typeVarchar}, nil
	case bool:
		return parameter{Type: typeBoolean, Value: v}, nil
	case int64:
		return parameter{Type: typeBigint, Value: v}, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return parameter{}, fmt.Errorf("writing %v: JSON has no number for it: %w", v, dbimp.ErrInvalidValue)
		}
		return parameter{Type: typeDouble, Value: v}, nil
	case string:
		return parameter{Type: typeVarchar, Value: v}, nil
	case time.Time:
		return parameter{Type: typeTimestamp, Value: v.UnixMilli()}, nil
	case dbimp.Date:
		return parameter{Type: typeDate, Value: v.String()}, nil
	case dbimp.LocalDateTime:
		// The server reads the text in the time zone of the query.
		return parameter{Type: typeTimestamp, Value: v.Date.String() + " " + v.Time.String()}, nil
	case *apd.Decimal:
		// The server binds a DECIMAL as the type of its JSON number
		// (measured), so the driver writes every digit.
		if v.Form != apd.Finite {
			return parameter{}, fmt.Errorf("writing %s: JSON has no number for it: %w", v, dbimp.ErrInvalidValue)
		}
		num := jsontext.Value(v.Text('G'))
		if !num.IsValid() || num.Kind() != '0' {
			return parameter{}, fmt.Errorf("writing %s as a JSON number: %w", v, dbimp.ErrInvalidValue)
		}
		return parameter{Type: typeDecimal, Value: num}, nil
	case []any, []string, []int64, []float64, []bool:
		return parameter{Type: typeArray, Value: v}, nil
	case []byte:
		return parameter{}, fmt.Errorf("writing a []byte: Druid has no binary parameter: %w", dbimp.ErrNotSupported)
	}
	return parameter{}, fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}
