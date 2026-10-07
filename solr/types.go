package solr

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"time"
	"uuid"

	"github.com/xo/dbimp"
)

// The SQL types that metadata.COLUMNS names for a column, by their JDBC code
// in dataType (measured). A column that is not in metadata.COLUMNS, such as
// an aggregate, has none, and its value reads by its JSON token (D166).
const (
	typeVarchar   = "VARCHAR"
	typeBigint    = "BIGINT"
	typeDouble    = "DOUBLE"
	typeTimestamp = "TIMESTAMP"
	typeAny       = "ANY"
)

// sqlTypes maps the dataType of metadata.COLUMNS to its SQL type.
var sqlTypes = map[int64]string{
	12:   typeVarchar,
	-5:   typeBigint,
	8:    typeDouble,
	93:   typeTimestamp,
	2000: typeAny,
}

// The classes of the field types of the schema that change the Go type of a
// VARCHAR column (D166). The luke handler names each one.
const (
	classBool   = "BoolField"
	classBinary = "BinaryField"
	classUUID   = "UUIDField"
)

// column is the type of one column of a result.
type column struct {
	// sql is the SQL type of the column from metadata.COLUMNS, or "" when it
	// names none.
	sql string
	// class is the class of the field type in the schema, such as BoolField,
	// or "" when the luke handler named none.
	class string
}

// kind is the way that the driver decodes the values of a column.
type kind int

const (
	kindAny kind = iota
	kindString
	kindInt
	kindFloat
	kindBool
	kindTime
	kindBytes
	kindUUID
	kindArray
)

// kind returns the kind of the column.
func (c column) kind() kind {
	switch c.sql {
	case typeAny:
		return kindArray
	case typeBigint:
		return kindInt
	case typeDouble:
		return kindFloat
	case typeTimestamp:
		return kindTime
	case typeVarchar:
		switch c.class {
		case classBool:
			return kindBool
		case classBinary:
			return kindBytes
		case classUUID:
			return kindUUID
		}
		return kindString
	}
	return kindAny
}

// scanType returns the Go type that a value of the column has (D135 and
// D166).
func scanType(c column) reflect.Type {
	switch c.kind() {
	case kindString:
		return reflect.TypeFor[string]()
	case kindInt:
		return reflect.TypeFor[int64]()
	case kindFloat:
		return reflect.TypeFor[float64]()
	case kindBool:
		return reflect.TypeFor[bool]()
	case kindTime:
		return reflect.TypeFor[time.Time]()
	case kindBytes:
		return reflect.TypeFor[[]byte]()
	case kindUUID:
		return reflect.TypeFor[uuid.UUID]()
	case kindArray:
		return reflect.TypeFor[[]any]()
	}
	return reflect.TypeFor[any]()
}

// decode returns the value v of a column as its Go value (D135 and D166). A
// NULL is nil for every type. A value whose JSON token does not fit the type
// of its column reads by its token, so the driver never fails on a value that
// the SQL layer typed in a way that the schema does not say.
func decode(c column, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch c.kind() {
	case kindInt:
		if v.Kind() == '0' {
			return dbimp.Int64(v)
		}
	case kindFloat:
		if v.Kind() == '0' {
			return dbimp.Float64(v)
		}
	case kindString:
		if v.Kind() == '"' {
			return dbimp.String(v)
		}
	case kindBool:
		return decodeBool(v)
	case kindTime:
		if v.Kind() == '"' {
			return decodeTime(v)
		}
	case kindBytes:
		if v.Kind() == '"' {
			return decodeBytes(v)
		}
	case kindUUID:
		if v.Kind() == '"' {
			return decodeUUID(v)
		}
	case kindArray:
		if v.Kind() == '[' {
			return dbimp.Any(v)
		}
	}
	return dbimp.Any(v)
}

// decodeBool returns a single-valued BoolField, which the server writes as
// the string "true" or "false" (measured). A JSON boolean reads as it is.
func decodeBool(v jsontext.Value) (any, error) {
	if v.Kind() != '"' {
		return dbimp.Any(v)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return nil, fmt.Errorf("reading %q as a boolean: %w", s, dbimp.ErrInvalidValue)
}

// decodeTime returns a DatePointField, which the server writes as ISO 8601
// text in UTC with milliseconds (measured).
func decodeTime(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil, fmt.Errorf("reading %q as a time: %w", s, dbimp.ErrInvalidValue)
	}
	return t.UTC(), nil
}

// decodeBytes returns a BinaryField, which the server writes as base64
// (measured).
func decodeBytes(v jsontext.Value) (any, error) {
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

// decodeUUID returns a UUIDField, which the server writes as its text
// (measured).
func decodeUUID(v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("reading %q as a uuid: %w", s, dbimp.ErrInvalidValue)
	}
	return u, nil
}
