package opensearch

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The types of a column, as the type of an entry of schema names them
// (measured). The server writes a mapping type or an SQL type in lower case.
// The driver reads a match_only_text column as text, a constant_keyword
// column as keyword, and a date_nanos column as timestamp, because the
// server names them so (measured). A type that the driver does not know is
// the decoded JSON value.
const (
	typeUndefined    = "undefined"
	typeBoolean      = "boolean"
	typeByte         = "byte"
	typeShort        = "short"
	typeInteger      = "integer"
	typeLong         = "long"
	typeFloat        = "float"
	typeDouble       = "double"
	typeHalfFloat    = "half_float"
	typeScaledFloat  = "scaled_float"
	typeKeyword      = "keyword"
	typeText         = "text"
	typeIP           = "ip"
	typeBinary       = "binary"
	typeDate         = "date"
	typeTime         = "time"
	typeTimestamp    = "timestamp"
	typeDatetime     = "datetime"
	typeGeoPoint     = "geo_point"
	typeObject       = "object"
	typeNested       = "nested"
	typeIntegerRange = "integer_range"
)

// timestampLayout is the form of a timestamp: the date, a space and the time
// in UTC, with no zone, and up to nine digits of the second, such as
// 2026-10-01 07:04:56.123 (measured). The fraction is optional on input.
const timestampLayout = "2006-01-02 15:04:05.999999999"

// whole reports whether the values of the type typ are decoded as one value
// of their own, with no decoder for a scalar. A JSON array in a column of
// another type is a field with several values, which the driver gives as a
// []any (D168).
func whole(typ string) bool {
	switch typ {
	case typeUndefined, typeGeoPoint, typeObject, typeNested, typeIntegerRange:
		return true
	}
	return !known(typ)
}

// known reports whether typ is a type that the driver decodes by its name.
func known(typ string) bool {
	switch typ {
	case typeBoolean, typeByte, typeShort, typeInteger, typeLong, typeFloat, typeDouble, typeHalfFloat, typeScaledFloat,
		typeKeyword, typeText, typeIP, typeBinary, typeDate, typeTime, typeTimestamp, typeDatetime,
		typeUndefined, typeGeoPoint, typeObject, typeNested, typeIntegerRange:
		return true
	}
	return false
}

// decode returns the value v of a column of the type typ as its Go value
// (D135 and D168). A NULL is nil for every type, and a field that a document
// lacks is NULL (measured). A field with several values is a JSON array under
// the type of its mapping, and is a []any of the elements (D168).
func decode(typ string, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	if v.Kind() == '[' && !whole(typ) {
		return decodeList(typ, v)
	}
	return decodeValue(typ, v)
}

// decodeList returns the elements of the JSON array v, each decoded as a value
// of the type typ.
func decodeList(typ string, v jsontext.Value) ([]any, error) {
	var elems []jsontext.Value
	if err := json.Unmarshal(v, &elems); err != nil {
		return nil, fmt.Errorf("reading a field with several values: %w: %w", dbimp.ErrInvalidValue, err)
	}
	out := make([]any, len(elems))
	for i, e := range elems {
		if dbimp.IsNull(e) {
			continue
		}
		val, err := decodeValue(typ, e)
		if err != nil {
			return nil, fmt.Errorf("reading value %d of a field with several values: %w", i, err)
		}
		out[i] = val
	}
	return out, nil
}

// decodeValue returns the one value v of the type typ.
func decodeValue(typ string, v jsontext.Value) (any, error) {
	switch typ {
	case typeBoolean:
		return dbimp.Bool(v)
	case typeByte, typeShort, typeInteger, typeLong:
		return dbimp.Int64(v)
	case typeFloat, typeDouble, typeHalfFloat, typeScaledFloat:
		return dbimp.Float64(v)
	case typeKeyword, typeText, typeIP:
		return dbimp.String(v)
	case typeBinary:
		return decodeBinary(v)
	case typeDate:
		return decodeDate(v)
	case typeTime:
		return decodeTime(v)
	case typeTimestamp, typeDatetime:
		return decodeTimestamp(v)
	}
	return dbimp.Any(v)
}

// decodeBinary returns a binary value, which the server writes as a string
// of base64, and the empty string for an empty value (measured).
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

// decodeDate returns a date, which the server writes as the text of a day,
// such as 2026-10-01, for a field whose format is a day (measured). A date
// that the legacy engine writes as the text of a timestamp is an error,
// because no decision names its form (docs/OPENSEARCH.md, open questions).
func decodeDate(v jsontext.Value) (dbimp.Date, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return dbimp.Date{}, err
	}
	d, err := dbimp.ParseDate(s)
	if err != nil {
		return dbimp.Date{}, fmt.Errorf("reading %q as a date: %w", s, dbimp.ErrInvalidValue)
	}
	return d, nil
}

// decodeTime returns a time of day, which the server writes with no zone,
// such as 12:34:56 (measured).
func decodeTime(v jsontext.Value) (dbimp.LocalTime, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return dbimp.LocalTime{}, err
	}
	t, err := dbimp.ParseLocalTime(s)
	if err != nil {
		return dbimp.LocalTime{}, fmt.Errorf("reading %q as a time of day: %w", s, dbimp.ErrInvalidValue)
	}
	return t, nil
}

// decodeTimestamp returns a timestamp, which the server writes in UTC with no
// zone, such as 2026-10-01 07:04:56.123 (measured). It reads the form of ISO
// 8601 with an offset too, which the legacy engine can send, and gives the
// instant in UTC.
func decodeTimestamp(v jsontext.Value) (time.Time, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return time.Time{}, err
	}
	for _, layout := range []string{timestampLayout, time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("reading %q as a timestamp: %w", s, dbimp.ErrInvalidValue)
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
	typeOfClock   = reflect.TypeFor[dbimp.LocalTime]()
	typeOfMap     = reflect.TypeFor[map[string]any]()
	typeOfList    = reflect.TypeFor[[]any]()
	typeOfAny     = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of a column of the type typ
// (D135 and D168). A type that the driver does not know is any.
func scanType(typ string) reflect.Type {
	switch typ {
	case typeBoolean:
		return typeOfBool
	case typeByte, typeShort, typeInteger, typeLong:
		return typeOfInt64
	case typeFloat, typeDouble, typeHalfFloat, typeScaledFloat:
		return typeOfFloat64
	case typeKeyword, typeText, typeIP:
		return typeOfString
	case typeBinary:
		return typeOfBytes
	case typeDate:
		return typeOfDate
	case typeTime:
		return typeOfClock
	case typeTimestamp, typeDatetime:
		return typeOfTime
	case typeGeoPoint, typeObject, typeIntegerRange:
		return typeOfMap
	case typeNested:
		return typeOfList
	}
	return typeOfAny
}

// databaseTypeName returns the type of a column as ColumnTypeDatabaseTypeName
// gives it, the name on the wire in upper case.
func databaseTypeName(typ string) string {
	return strings.ToUpper(typ)
}
