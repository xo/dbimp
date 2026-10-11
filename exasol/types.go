package exasol

import (
	"encoding/hex"
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// kind is how the driver reads the values of one column (D206).
type kind int

const (
	kindOther kind = iota
	kindBoolean
	// kindInteger is a DECIMAL with scale 0 and a precision of 18 or less,
	// which the server sends as a JSON number that fits an int64.
	kindInteger
	kindDecimal
	kindDouble
	kindChar
	kindVarchar
	kindDate
	kindTimestamp
	kindTimestampLTZ
	kindIntervalYM
	kindIntervalDS
	kindGeometry
	kindHashtype
)

// The names of the types on the wire, in upper case, as the server writes them
// (recorded: "select every row of types").
const (
	typeBoolean     = "BOOLEAN"
	typeDecimal     = "DECIMAL"
	typeDouble      = "DOUBLE"
	typeChar        = "CHAR"
	typeVarchar     = "VARCHAR"
	typeDate        = "DATE"
	typeTimestamp   = "TIMESTAMP"
	typeTimestampTZ = "TIMESTAMP WITH LOCAL TIME ZONE"
	typeIntervalYM  = "INTERVAL YEAR TO MONTH"
	typeIntervalDS  = "INTERVAL DAY TO SECOND"
	typeGeometry    = "GEOMETRY"
	typeHashtype    = "HASHTYPE"
)

// maxIntegerPrecision is the largest precision of a DECIMAL with scale 0 that
// the server sends as a number, and that fits an int64 (recorded: "select
// decimals by precision").
const maxIntegerPrecision = 18

// column is a column of a result or of the parameters of a statement.
type column struct {
	name string
	typ  dataType
	kind kind
}

// columnOf returns the column for the wire column w.
func columnOf(w wireColumn) column {
	c := column{name: w.Name, typ: w.DataType}
	switch strings.ToUpper(w.DataType.Type) {
	case typeBoolean:
		c.kind = kindBoolean
	case typeDecimal:
		c.kind = kindDecimal
		if w.DataType.Scale == 0 && w.DataType.Precision <= maxIntegerPrecision {
			c.kind = kindInteger
		}
	case typeDouble:
		c.kind = kindDouble
	case typeChar:
		c.kind = kindChar
	case typeVarchar:
		c.kind = kindVarchar
	case typeDate:
		c.kind = kindDate
	case typeTimestamp:
		c.kind = kindTimestamp
	case typeTimestampTZ:
		c.kind = kindTimestampLTZ
	case typeIntervalYM:
		c.kind = kindIntervalYM
	case typeIntervalDS:
		c.kind = kindIntervalDS
	case typeGeometry:
		c.kind = kindGeometry
	case typeHashtype:
		c.kind = kindHashtype
	}
	return c
}

// columnsOf returns the columns for the wire columns.
func columnsOf(ws []wireColumn) []column {
	cols := make([]column, len(ws))
	for i, w := range ws {
		cols[i] = columnOf(w)
	}
	return cols
}

// databaseType returns the name of the type, in upper case. A DECIMAL is a
// DECIMAL whatever its precision and scale.
func (c column) databaseType() string {
	return strings.ToUpper(c.typ.Type)
}

// The scan types of the columns (D206).
var (
	typeOfBool          = reflect.TypeFor[bool]()
	typeOfInt64         = reflect.TypeFor[int64]()
	typeOfDecimal       = reflect.TypeFor[*apd.Decimal]()
	typeOfFloat64       = reflect.TypeFor[float64]()
	typeOfString        = reflect.TypeFor[string]()
	typeOfDate          = reflect.TypeFor[dbimp.Date]()
	typeOfLocalDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfTime          = reflect.TypeFor[time.Time]()
	typeOfInterval      = reflect.TypeFor[dbimp.Interval]()
	typeOfBytes         = reflect.TypeFor[[]byte]()
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the column (D206).
func (c column) scanType() reflect.Type {
	switch c.kind {
	case kindBoolean:
		return typeOfBool
	case kindInteger:
		return typeOfInt64
	case kindDecimal:
		return typeOfDecimal
	case kindDouble:
		return typeOfFloat64
	case kindChar, kindVarchar, kindGeometry:
		return typeOfString
	case kindDate:
		return typeOfDate
	case kindTimestamp:
		return typeOfLocalDateTime
	case kindTimestampLTZ:
		return typeOfTime
	case kindIntervalYM, kindIntervalDS:
		return typeOfInterval
	case kindHashtype:
		return typeOfBytes
	}
	return typeOfAny
}

// decode returns the value v of the column as its Go value (D206). A NULL is
// nil, and so is an empty string, which the server stores as NULL (D207). loc
// is the time zone of the session, for a TIMESTAMP WITH LOCAL TIME ZONE.
func (c column) decode(v jsontext.Value, loc func() (*time.Location, error)) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch c.kind {
	case kindBoolean:
		return dbimp.Bool(v)
	case kindInteger:
		if v.Kind() == '"' {
			// A client that asked for a string gets one, and the text is the
			// digits of the number.
			d, err := dbimp.Decimal(v)
			if err != nil {
				return nil, err
			}
			i, err := d.Int64()
			if err != nil {
				return nil, fmt.Errorf("reading %s as int64: %w", d, dbimp.ErrInvalidValue)
			}
			return i, nil
		}
		return dbimp.Int64(v)
	case kindDecimal:
		return dbimp.Decimal(v)
	case kindDouble:
		return dbimp.Float64(v)
	case kindHashtype:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("reading the hash %q: %w", s, dbimp.ErrInvalidValue)
		}
		return b, nil
	}
	s, err := dbimp.String(v)
	if err != nil {
		if c.kind == kindOther {
			return dbimp.Any(v)
		}
		return nil, err
	}
	switch c.kind {
	case kindDate:
		return dbimp.ParseDate(s)
	case kindTimestamp:
		return dbimp.ParseLocalDateTime(s)
	case kindTimestampLTZ:
		dt, err := dbimp.ParseLocalDateTime(s)
		if err != nil {
			return nil, err
		}
		l, err := loc()
		if err != nil {
			return nil, err
		}
		return dt.In(l), nil
	case kindIntervalYM:
		return parseIntervalYM(s)
	case kindIntervalDS:
		return parseIntervalDS(s)
	}
	return s, nil
}
