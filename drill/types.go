package drill

import (
	"bytes"
	"encoding/base64"
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

// The wire types of a column, as metadata names them, without the width, the
// precision and the scale (measured).
const (
	typeInt          = "INT"
	typeBigint       = "BIGINT"
	typeFloat4       = "FLOAT4"
	typeFloat8       = "FLOAT8"
	typeVarDecimal   = "VARDECIMAL"
	typeBit          = "BIT"
	typeVarchar      = "VARCHAR"
	typeVarbinary    = "VARBINARY"
	typeDate         = "DATE"
	typeTime         = "TIME"
	typeTimestamp    = "TIMESTAMP"
	typeInterval     = "INTERVAL"
	typeIntervalDay  = "INTERVALDAY"
	typeIntervalYear = "INTERVALYEAR"
	typeMap          = "MAP"
	typeList         = "LIST"
)

// column is the type of one column, from one entry of metadata, such as
// VARDECIMAL(38, 18) or VARCHAR(1).
type column struct {
	// name is the type, such as VARDECIMAL.
	name string
	// args are the numbers in the parentheses of the entry: the width of a
	// VARCHAR, or the precision and the scale of a VARDECIMAL.
	args []int64
}

// parseColumn returns the column of an entry of metadata. An entry that
// does not read as a type with numbers is the whole entry as a name.
func parseColumn(meta string) column {
	name, rest, ok := strings.Cut(meta, "(")
	if !ok {
		return column{name: strings.ToUpper(strings.TrimSpace(meta))}
	}
	c := column{name: strings.ToUpper(strings.TrimSpace(name))}
	for part := range strings.SplitSeq(strings.TrimSuffix(strings.TrimSpace(rest), ")"), ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return column{name: strings.ToUpper(strings.TrimSpace(meta))}
		}
		c.args = append(c.args, n)
	}
	return c
}

// decode returns the value v of a column of the type c as its Go value
// (D135 and D165). A NULL is nil for every type. A list of a scalar type
// arrives under the name of its element, so a JSON array is a []any of the
// values of its elements, whatever the type says (D165).
func decode(c column, v jsontext.Value) (any, error) {
	switch {
	case dbimp.IsNull(v):
		return nil, nil
	case v.Kind() == '[':
		return decodeList(c, v)
	}
	switch c.name {
	case typeInt, typeBigint:
		return dbimp.Int64(v)
	case typeFloat4, typeFloat8:
		return decodeFloat(v)
	case typeVarDecimal:
		return dbimp.Decimal(v)
	case typeBit:
		return dbimp.Bool(v)
	case typeVarchar:
		return dbimp.String(v)
	case typeVarbinary:
		return decodeBinary(v)
	case typeDate:
		t, err := decodeMillis(v)
		if err != nil {
			return nil, err
		}
		return dbimp.DateOf(t), nil
	case typeTime:
		return decodeTime(v)
	case typeTimestamp:
		t, err := decodeMillis(v)
		if err != nil {
			return nil, err
		}
		return dbimp.LocalDateTimeOf(t), nil
	case typeInterval, typeIntervalDay, typeIntervalYear:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		return dbimp.ParseInterval(s)
	}
	// MAP, and a type that the driver does not know, read as the decoded JSON
	// value.
	return dbimp.Any(v)
}

// decodeList returns a JSON array as a []any, with each element decoded as
// a value of the type c.
func decodeList(c column, v jsontext.Value) ([]any, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading a list: %w", err)
	}
	out := []any{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a list: %w", err)
		}
		e, err := decode(c, ev)
		if err != nil {
			return nil, fmt.Errorf("reading an element of a list: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
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

// decodeBinary returns the bytes of a VARBINARY, which the server writes as a
// string of base64 (measured). An empty value is an empty slice and not nil.
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

// decodeMillis returns the time of a DATE or a TIMESTAMP, which is a number
// of milliseconds since 1970-01-01 of the time read as UTC (measured).
func decodeMillis(v jsontext.Value) (time.Time, error) {
	ms, err := dbimp.Int64(v)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms).UTC(), nil
}

// decodeTime returns a TIME, which is a number of milliseconds since
// midnight (measured).
func decodeTime(v jsontext.Value) (dbimp.LocalTime, error) {
	ms, err := dbimp.Int64(v)
	if err != nil {
		return dbimp.LocalTime{}, err
	}
	if ms < 0 || ms >= 86400000 {
		return dbimp.LocalTime{}, fmt.Errorf("reading %d as a time of day: %w", ms, dbimp.ErrInvalidValue)
	}
	return dbimp.LocalTime{
		Hour:       int(ms / 3600000),
		Minute:     int(ms % 3600000 / 60000),
		Second:     int(ms % 60000 / 1000),
		Nanosecond: int(ms%1000) * 1e6,
	}, nil
}

// The scan types of the columns.
var (
	typeOfInt64     = reflect.TypeFor[int64]()
	typeOfFloat64   = reflect.TypeFor[float64]()
	typeOfDecimal   = reflect.TypeFor[*apd.Decimal]()
	typeOfBool      = reflect.TypeFor[bool]()
	typeOfString    = reflect.TypeFor[string]()
	typeOfBytes     = reflect.TypeFor[[]byte]()
	typeOfDate      = reflect.TypeFor[dbimp.Date]()
	typeOfTime      = reflect.TypeFor[dbimp.LocalTime]()
	typeOfTimestamp = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfInterval  = reflect.TypeFor[dbimp.Interval]()
	typeOfMap       = reflect.TypeFor[map[string]any]()
	typeOfList      = reflect.TypeFor[[]any]()
	typeOfAny       = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the column c (D135 and D165). A
// column that held a list gets any, because the type names the element and
// not the list.
func scanType(c column, list bool) reflect.Type {
	if list {
		return typeOfAny
	}
	switch c.name {
	case typeInt, typeBigint:
		return typeOfInt64
	case typeFloat4, typeFloat8:
		return typeOfFloat64
	case typeVarDecimal:
		return typeOfDecimal
	case typeBit:
		return typeOfBool
	case typeVarchar:
		return typeOfString
	case typeVarbinary:
		return typeOfBytes
	case typeDate:
		return typeOfDate
	case typeTime:
		return typeOfTime
	case typeTimestamp:
		return typeOfTimestamp
	case typeInterval, typeIntervalDay, typeIntervalYear:
		return typeOfInterval
	case typeMap:
		return typeOfMap
	case typeList:
		return typeOfList
	}
	return typeOfAny
}
