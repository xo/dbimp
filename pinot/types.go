package pinot

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

// The types of a column, as columnDataTypes names them (measured). An array
// is the type of its element with the suffix _ARRAY, such as INT_ARRAY.
const (
	typeInt        = "INT"
	typeLong       = "LONG"
	typeFloat      = "FLOAT"
	typeDouble     = "DOUBLE"
	typeBigDecimal = "BIG_DECIMAL"
	typeBoolean    = "BOOLEAN"
	typeTimestamp  = "TIMESTAMP"
	typeString     = "STRING"
	typeJSON       = "JSON"
	typeBytes      = "BYTES"
	typeMap        = "MAP"
	typeUnknown    = "UNKNOWN"
	arraySuffix    = "_ARRAY"
)

// timestampLayout is the form of a TIMESTAMP, which the server writes in
// UTC, such as 2023-11-14 22:13:20.123, with from one to three digits after
// the point (measured). A layout with no fraction takes any fraction.
const timestampLayout = "2006-01-02 15:04:05"

// decode returns the value v of a column of the type typ as its Go value
// (D130). A NULL is nil for every type, because each query sends
// enableNullHandling=true. A type that the driver does not know, such as
// OBJECT, reads as the decoded JSON value.
func decode(typ string, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	if elem, ok := strings.CutSuffix(typ, arraySuffix); ok {
		return decodeArray(typ, elem, v)
	}
	switch typ {
	case typeInt, typeLong:
		return dbimp.Int64(v)
	case typeFloat, typeDouble:
		return decodeFloat(v)
	case typeBigDecimal:
		return dbimp.Decimal(v)
	case typeBoolean:
		return dbimp.Bool(v)
	case typeString:
		return dbimp.String(v)
	case typeJSON:
		// A JSON value is the text of JSON in a string (measured).
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		return dbimp.Any(jsontext.Value(s))
	case typeBytes:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as hex: %w", s, dbimp.ErrInvalidValue)
		}
		return b, nil
	case typeTimestamp:
		s, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		t, err := time.ParseInLocation(timestampLayout, s, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a TIMESTAMP: %w", s, dbimp.ErrInvalidValue)
		}
		return t, nil
	}
	// MAP is a JSON object whose values have no type in the answer, and
	// UNKNOWN is the type of a NULL literal.
	return dbimp.Any(v)
}

// decodeFloat returns a FLOAT or a DOUBLE, which is a JSON number, or the
// string "NaN", "Infinity" or "-Infinity" (measured).
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

// decodeArray returns an array as []any, with each element decoded by the
// type elem.
func decodeArray(typ, elem string, v jsontext.Value) ([]any, error) {
	if v.Kind() != '[' {
		return nil, fmt.Errorf("reading a %s: %s is not an array: %w", typ, v.Kind(), dbimp.ErrInvalidValue)
	}
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading a %s: %w", typ, err)
	}
	out := []any{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a %s: %w", typ, err)
		}
		e, err := decode(elem, ev)
		if err != nil {
			return nil, fmt.Errorf("reading a %s: %w", typ, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// The scan types of the columns.
var (
	typeOfInt64   = reflect.TypeFor[int64]()
	typeOfFloat64 = reflect.TypeFor[float64]()
	typeOfDecimal = reflect.TypeFor[*apd.Decimal]()
	typeOfBool    = reflect.TypeFor[bool]()
	typeOfString  = reflect.TypeFor[string]()
	typeOfBytes   = reflect.TypeFor[[]byte]()
	typeOfTime    = reflect.TypeFor[time.Time]()
	typeOfMap     = reflect.TypeFor[map[string]any]()
	typeOfArray   = reflect.TypeFor[[]any]()
	typeOfAny     = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the type typ (D130).
func scanType(typ string) reflect.Type {
	if strings.HasSuffix(typ, arraySuffix) {
		return typeOfArray
	}
	switch typ {
	case typeInt, typeLong:
		return typeOfInt64
	case typeFloat, typeDouble:
		return typeOfFloat64
	case typeBigDecimal:
		return typeOfDecimal
	case typeBoolean:
		return typeOfBool
	case typeString:
		return typeOfString
	case typeBytes:
		return typeOfBytes
	case typeTimestamp:
		return typeOfTime
	case typeMap:
		return typeOfMap
	}
	return typeOfAny
}

// literal returns the argument v as a literal of Pinot, which the driver
// writes into the text of the query in place of its ? (D132).
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "", fmt.Errorf("writing %v: Pinot has no literal for it: %w", v, dbimp.ErrInvalidValue)
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case string:
		return quote(v), nil
	case *apd.Decimal:
		if v.Form != apd.Finite {
			return "", fmt.Errorf("writing %s: Pinot has no literal for it: %w", v, dbimp.ErrInvalidValue)
		}
		return "CAST(" + quote(v.Text('f')) + " AS BIG_DECIMAL)", nil
	case time.Time:
		// Pinot compares the milliseconds of a LONG with a TIMESTAMP.
		return strconv.FormatInt(v.UnixMilli(), 10), nil
	case []byte:
		return "hexToBytes('" + hex.EncodeToString(v) + "')", nil
	}
	return "", fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}

// quote returns s in single quotes, with each ' doubled.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
