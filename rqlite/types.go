package rqlite

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// class is how the driver reads the values of a column, from its declared
// type (D140).
type class int

// The classes of a column. The first five are the affinities of SQLite. The
// server writes a date, a time and a boolean in forms of its own for the
// four declared types that follow them (measured).
const (
	// classNone is the class of a column with no type that the driver
	// supports, such as ANY or the "" of an expression. Each value reads by
	// its JSON form (D140).
	classNone class = iota
	classInteger
	classReal
	classText
	classBlob
	classNumeric
	classBoolean
	classDate
	classDateTime
	classTimestamp
)

// The canonical names of the classes, as ColumnTypeDatabaseTypeName names a
// column whose declared type is the name alone.
const (
	typeInteger   = "INTEGER"
	typeReal      = "REAL"
	typeText      = "TEXT"
	typeBlob      = "BLOB"
	typeNumeric   = "NUMERIC"
	typeBoolean   = "BOOLEAN"
	typeDate      = "DATE"
	typeDateTime  = "DATETIME"
	typeTimestamp = "TIMESTAMP"
	typeAny       = "ANY"
)

// classOf returns the class of the declared type typ, as the server names it
// in types, such as "bigint" or "varchar(10)" (D140). BOOLEAN, DATE,
// DATETIME and TIMESTAMP are their own classes, because the server writes
// them in forms of their own (measured). Every other name follows the rules
// of affinity of SQLite, in their order: a name with INT, then CHAR, CLOB or
// TEXT, then BLOB, then REAL, FLOA or DOUB, and NUMERIC for any other. ANY
// and the "" of an expression have no class.
func classOf(typ string) class {
	t := strings.ToUpper(strings.TrimSpace(typ))
	switch t {
	case "", typeAny:
		return classNone
	case typeBoolean:
		return classBoolean
	case typeDate:
		return classDate
	case typeDateTime:
		return classDateTime
	case typeTimestamp:
		return classTimestamp
	}
	switch {
	case strings.Contains(t, "INT"):
		return classInteger
	case strings.Contains(t, "CHAR"), strings.Contains(t, "CLOB"), strings.Contains(t, "TEXT"):
		return classText
	case strings.Contains(t, "BLOB"):
		return classBlob
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return classReal
	}
	return classNumeric
}

// decode returns the value v of a column of the class c as its Go value
// (D140). A NULL is nil. A value whose JSON form cannot have the Go type of
// its column, such as 'abc' in an INTEGER column, has the Go type of its
// JSON form, because SQLite keeps the storage class of each value.
func decode(c class, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch c {
	case classReal:
		if v.Kind() == '0' {
			return dbimp.Float64(v)
		}
	case classDate, classDateTime, classTimestamp:
		if v.Kind() == '"' {
			return decodeTime(c, v)
		}
	}
	return decodeJSON(v)
}

// decodeJSON returns v by its JSON form: a number is an int64 if it has no
// fraction and no exponent and fits, and a float64 otherwise, because SQLite
// has no larger integer (measured). A string is a string, an array of bytes
// is a []byte, which every request asks for with blob_array (D142), and true
// and false are a bool.
func decodeJSON(v jsontext.Value) (any, error) {
	switch v.Kind() {
	case '0':
		if bytes.ContainsAny(v, ".eE") {
			return dbimp.Float64(v)
		}
		if i, err := dbimp.Int64(v); err == nil {
			return i, nil
		}
		return dbimp.Float64(v)
	case '"':
		return dbimp.String(v)
	case 't', 'f':
		return dbimp.Bool(v)
	case '[':
		return decodeBlob(v)
	}
	return nil, fmt.Errorf("reading a value of the kind %v: %w", v.Kind(), dbimp.ErrInvalidValue)
}

// decodeBlob returns a BLOB, which is an array of numbers from 0 to 255 with
// blob_array (measured).
func decodeBlob(v jsontext.Value) ([]byte, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading a BLOB: %w", err)
	}
	b := []byte{}
	for dec.PeekKind() != ']' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading a BLOB: %w", err)
		}
		if tok.Kind() != '0' {
			return nil, fmt.Errorf("reading a BLOB: a byte of the kind %v: %w", tok.Kind(), dbimp.ErrInvalidValue)
		}
		n, err := tok.Int()
		if err != nil {
			return nil, fmt.Errorf("reading a BLOB: %w", err)
		}
		if n < 0 || n > math.MaxUint8 {
			return nil, fmt.Errorf("reading a BLOB: the byte %d: %w", n, dbimp.ErrInvalidValue)
		}
		b = append(b, byte(n))
	}
	return b, nil
}

// decodeTime returns a DATE as a dbimp.Date, a DATETIME as a
// dbimp.LocalDateTime, and a TIMESTAMP as a time.Time, from the RFC 3339 text
// that the server writes for each (D140). The server adds Z to a date and to
// a time that named no zone, so a DATE and a DATETIME keep the fields as the
// server wrote them, and drop the zone. Text that the server did not parse
// stays text.
func decodeTime(c class, v jsontext.Value) (any, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s, nil //nolint:nilerr // A value that is not a time keeps its JSON form (D140).
	}
	switch c {
	case classDate:
		return dbimp.DateOf(t), nil
	case classDateTime:
		return dbimp.LocalDateTimeOf(t), nil
	}
	return t, nil
}

// The scan types of the columns.
var (
	typeOfInt64         = reflect.TypeFor[int64]()
	typeOfFloat64       = reflect.TypeFor[float64]()
	typeOfString        = reflect.TypeFor[string]()
	typeOfBytes         = reflect.TypeFor[[]byte]()
	typeOfBool          = reflect.TypeFor[bool]()
	typeOfDate          = reflect.TypeFor[dbimp.Date]()
	typeOfLocalDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfTime          = reflect.TypeFor[time.Time]()
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of a column of the class c
// (D140). A NUMERIC holds an integer or a REAL, and a column with no class
// holds any value, so each has the scan type any.
func scanType(c class) reflect.Type {
	switch c {
	case classInteger:
		return typeOfInt64
	case classReal:
		return typeOfFloat64
	case classText:
		return typeOfString
	case classBlob:
		return typeOfBytes
	case classBoolean:
		return typeOfBool
	case classDate:
		return typeOfDate
	case classDateTime:
		return typeOfLocalDateTime
	case classTimestamp:
		return typeOfTime
	}
	return typeOfAny
}
