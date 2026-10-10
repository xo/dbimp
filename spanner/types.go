package spanner

import (
	"bytes"
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The codes of the types in the member type of a field, which are the wire
// types (recorded). The code STRUCT is the type of an element of an array
// only, because the server refuses a struct as a column (recorded: "STRUCT as
// a column").
const (
	wireBool      = "BOOL"
	wireInt64     = "INT64"
	wireFloat32   = "FLOAT32"
	wireFloat64   = "FLOAT64"
	wireNumeric   = "NUMERIC"
	wireString    = "STRING"
	wireBytes     = "BYTES"
	wireDate      = "DATE"
	wireTimestamp = "TIMESTAMP"
	wireJSON      = "JSON"
	wireUUID      = "UUID"
	wireInterval  = "INTERVAL"
	wireArray     = "ARRAY"
	wireStruct    = "STRUCT"
)

// The precision and the scale of NUMERIC: 29 digits before the point and 9
// after it (recorded: "NUMERIC values").
const (
	numericPrecision = 38
	numericScale     = 9
)

// wireType is the member type of a field of the metadata (recorded). An array
// has arrayElementType, and a struct has structType.
type wireType struct {
	Code       string      `json:"code"`
	Elem       *wireType   `json:"arrayElementType,omitzero"`
	Struct     *structType `json:"structType,omitzero"`
	Annotation string      `json:"typeAnnotation,omitzero"`
	Proto      string      `json:"protoTypeFqn,omitzero"`
}

// structType holds the fields of a struct.
type structType struct {
	Fields []field `json:"fields"`
}

// field is one column of a result, or one field of a struct.
type field struct {
	Name string   `json:"name"`
	Type wireType `json:"type"`
}

// supported returns an error that names the column when the type is one that
// the first version does not cover (D191 item 10): ENUM and PROTO, a type of
// the PostgreSQL dialect, and a code that the driver does not know.
func (t *wireType) supported(column string) error {
	if t.Annotation != "" {
		return fmt.Errorf("reading the column %q: the type %s %s is of the PostgreSQL dialect, which the driver does not cover: %w", column, t.Code, t.Annotation, dbimp.ErrNotSupported)
	}
	switch t.Code {
	case wireBool, wireInt64, wireFloat32, wireFloat64, wireNumeric, wireString,
		wireBytes, wireDate, wireTimestamp, wireJSON, wireUUID, wireInterval:
		return nil
	case wireArray:
		if t.Elem == nil {
			return fmt.Errorf("reading the column %q: an ARRAY with no element type: %w", column, dbimp.ErrInvalidValue)
		}
		return t.Elem.supported(column)
	case wireStruct:
		if t.Struct == nil {
			return fmt.Errorf("reading the column %q: a STRUCT with no fields: %w", column, dbimp.ErrInvalidValue)
		}
		for _, f := range t.Struct.Fields {
			if err := f.Type.supported(column); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("reading the column %q: the driver has no Go type for %s: %w", column, t.Code, dbimp.ErrNotSupported)
}

// decode returns the value v of a column of the type t as its Go value (D135
// and D191). A NULL is nil in every type (recorded: "a NULL of each type").
func (t *wireType) decode(v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch t.Code {
	case wireBool:
		return dbimp.Bool(v)
	case wireInt64:
		return decodeInt64(v)
	case wireFloat32, wireFloat64:
		return decodeFloat(v)
	case wireNumeric:
		return decodeNumeric(v)
	case wireString:
		return dbimp.String(v)
	case wireBytes:
		return decodeBytes(v)
	case wireDate:
		return decodeText(v, "a date", dbimp.ParseDate)
	case wireTimestamp:
		return decodeText(v, "a timestamp", parseTimestamp)
	case wireJSON:
		return decodeJSON(v)
	case wireUUID:
		return decodeText(v, "a UUID", uuid.Parse)
	case wireInterval:
		return decodeText(v, "an interval", dbimp.ParseInterval)
	case wireArray:
		return t.decodeArray(v)
	case wireStruct:
		return t.decodeStruct(v)
	}
	return nil, fmt.Errorf("reading a value of the type %s: the driver has no Go type for it: %w", t.Code, dbimp.ErrNotSupported)
}

// decodeText reads the string v and parses it.
func decodeText[T any](v jsontext.Value, what string, parse func(string) (T, error)) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	out, err := parse(s)
	if err != nil {
		return nil, fmt.Errorf("reading %q as %s: %w: %w", s, what, dbimp.ErrInvalidValue, err)
	}
	return out, nil
}

// parseTimestamp reads RFC 3339 text, which the server writes in UTC with a Z.
// A value in another zone is the same instant in UTC.
func parseTimestamp(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t.UTC(), err
}

// decodeInt64 reads an INT64, which is a decimal string (recorded: "INT64
// limits"). A JSON number is accepted too.
func decodeInt64(v jsontext.Value) (any, error) {
	if v.Kind() == '0' {
		return dbimp.Int64(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("reading %q as an integer: %w", s, dbimp.ErrInvalidValue)
	}
	return i, nil
}

// decodeFloat reads a FLOAT64 or a FLOAT32, which is a JSON number, or the
// string NaN, Infinity or -Infinity (recorded: "FLOAT64 NaN and infinity").
// A FLOAT32 is the widened value, so it reads as a float64 that converts back
// to the float32 exactly (recorded: "FLOAT32 of 0.1 and FLOAT64 of 0.1").
func decodeFloat(v jsontext.Value) (any, error) {
	if v.Kind() == '0' {
		return dbimp.Float64(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	switch s {
	case "NaN":
		return math.NaN(), nil
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	}
	return nil, fmt.Errorf("reading %q as a float: %w", s, dbimp.ErrInvalidValue)
}

// decodeNumeric reads a NUMERIC, which is a string of decimal digits (D33).
func decodeNumeric(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	d, _, err := apd.NewFromString(s)
	if err != nil || d.Form != apd.Finite {
		return nil, fmt.Errorf("reading %q as a decimal: %w", s, dbimp.ErrInvalidValue)
	}
	return d, nil
}

// decodeBytes reads a BYTES, which is base64 text with padding (recorded:
// "BYTES values"). The pieces of a value that the server splits are joined
// before this call, so the text is whole.
func decodeBytes(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("reading the base64 text of a BYTES value: %w", dbimp.ErrInvalidValue)
	}
	return b, nil
}

// decodeJSON reads a JSON column, which is a string that holds JSON text. A
// JSON null is the text null, and decodes to nil (D191 item 9).
func decodeJSON(v jsontext.Value) (any, error) {
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

// decodeArray reads an ARRAY, a JSON array of the values of its element type.
func (t *wireType) decodeArray(v jsontext.Value) (any, error) {
	if t.Elem == nil {
		return nil, fmt.Errorf("reading an ARRAY with no element type: %w", dbimp.ErrInvalidValue)
	}
	return readList(v, func(int) *wireType { return t.Elem })
}

// decodeStruct reads a STRUCT, which is a JSON array of the values of its
// fields in their order (recorded: "ARRAY of STRUCT"). The names of the fields
// are in the type of the column only.
func (t *wireType) decodeStruct(v jsontext.Value) (any, error) {
	if t.Struct == nil {
		return nil, fmt.Errorf("reading a STRUCT with no fields: %w", dbimp.ErrInvalidValue)
	}
	out, err := readList(v, func(i int) *wireType {
		if i >= len(t.Struct.Fields) {
			return nil
		}
		return &t.Struct.Fields[i].Type
	})
	if err != nil {
		return nil, err
	}
	if n := len(out); n != len(t.Struct.Fields) {
		return nil, fmt.Errorf("reading a STRUCT: %d values for %d fields: %w", n, len(t.Struct.Fields), dbimp.ErrColumnCount)
	}
	return out, nil
}

// readList reads the JSON array v, and decodes element i with the type that
// typeOf returns for it.
func readList(v jsontext.Value, typeOf func(i int) *wireType) ([]any, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '[' {
		return nil, fmt.Errorf("reading %s as a list: %w", v.Kind(), dbimp.ErrInvalidValue)
	}
	out := []any{}
	for i := 0; dec.PeekKind() != ']'; i++ {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading element %d of a list: %w", i, err)
		}
		et := typeOf(i)
		if et == nil {
			return nil, fmt.Errorf("reading element %d of a list: more elements than the type has: %w", i, dbimp.ErrColumnCount)
		}
		x, err := et.decode(ev)
		if err != nil {
			return nil, fmt.Errorf("reading element %d of a list: %w", i, err)
		}
		out = append(out, x)
	}
	return out, nil
}

// The scan types of the columns (D135 and D191).
var (
	typeOfBool      = reflect.TypeFor[bool]()
	typeOfInt64     = reflect.TypeFor[int64]()
	typeOfFloat64   = reflect.TypeFor[float64]()
	typeOfDecimal   = reflect.TypeFor[*apd.Decimal]()
	typeOfString    = reflect.TypeFor[string]()
	typeOfBytes     = reflect.TypeFor[[]byte]()
	typeOfDate      = reflect.TypeFor[dbimp.Date]()
	typeOfTimestamp = reflect.TypeFor[time.Time]()
	typeOfJSON      = reflect.TypeFor[any]()
	typeOfUUID      = reflect.TypeFor[uuid.UUID]()
	typeOfInterval  = reflect.TypeFor[dbimp.Interval]()
	typeOfList      = reflect.TypeFor[[]any]()
)

// scanType returns the Go type of a value of the type t (D135 and D191). A
// JSON column is the decoded JSON value, so its type is any.
func (t *wireType) scanType() reflect.Type {
	switch t.Code {
	case wireBool:
		return typeOfBool
	case wireInt64:
		return typeOfInt64
	case wireFloat32, wireFloat64:
		return typeOfFloat64
	case wireNumeric:
		return typeOfDecimal
	case wireString:
		return typeOfString
	case wireBytes:
		return typeOfBytes
	case wireDate:
		return typeOfDate
	case wireTimestamp:
		return typeOfTimestamp
	case wireJSON:
		return typeOfJSON
	case wireUUID:
		return typeOfUUID
	case wireInterval:
		return typeOfInterval
	case wireArray, wireStruct:
		return typeOfList
	}
	return typeOfJSON
}
