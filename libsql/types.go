package libsql

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// column is how the driver reads the values of one column: the affinity of
// its declared type, and whether it holds a vector (D147).
type column struct {
	affinity dbimp.Affinity
	vector   bool
}

// columnOf returns the column of the declared type decl, which is "" for an
// expression (D140 and D147). F32_BLOB(n) is a vector of float32.
func columnOf(decl string) column {
	return column{
		affinity: dbimp.AffinityOf(decl),
		vector:   strings.HasPrefix(strings.ToUpper(strings.TrimSpace(decl)), typeF32Blob),
	}
}

// typeF32Blob starts the declared type of a vector of float32, such as
// F32_BLOB(3).
const typeF32Blob = "F32_BLOB"

// value is one value of Hrana, with its storage class in Type (measured).
type value struct {
	Type   string         `json:"type"`
	Value  jsontext.Value `json:"value,omitzero"`
	Base64 string         `json:"base64,omitzero"`
}

// decode returns the value v of a column c as its Go value (D140 and D147).
// A NULL is nil. A value whose storage class cannot have the Go type of its
// column keeps the Go type of its storage class.
func decode(c column, raw jsontext.Value) (any, error) {
	var v value
	if err := unmarshalValue(raw, &v); err != nil {
		return nil, err
	}
	switch v.Type {
	case "null":
		return nil, nil
	case "integer":
		s, err := dbimp.String(v.Value)
		if err != nil {
			return nil, fmt.Errorf("reading an integer: %w", err)
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("reading the integer %q: %w", s, dbimp.ErrInvalidValue)
		}
		switch c.affinity {
		case dbimp.AffinityBoolean:
			return i != 0, nil
		case dbimp.AffinityReal:
			return float64(i), nil
		}
		return i, nil
	case "float":
		// The server writes an infinity as null, and loses its sign
		// (measured), so it is +Inf (D147).
		if len(v.Value) == 0 || dbimp.IsNull(v.Value) {
			return math.Inf(1), nil
		}
		return dbimp.Float64(v.Value)
	case "text":
		s, err := dbimp.String(v.Value)
		if err != nil {
			return nil, fmt.Errorf("reading a text: %w", err)
		}
		return decodeText(c.affinity, s), nil
	case "blob":
		b, err := decodeBase64(v.Base64)
		if err != nil {
			return nil, err
		}
		if c.vector && len(b)%4 == 0 {
			vec := make(dbimp.Vector[float32], len(b)/4)
			for i := range vec {
				vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
			}
			return vec, nil
		}
		return b, nil
	}
	return nil, fmt.Errorf("reading a value of the type %q: %w", v.Type, dbimp.ErrInvalidValue)
}

// unmarshalValue reads one value of Hrana. The key value holds a number, a
// string or null, so it is read as raw JSON.
func unmarshalValue(raw jsontext.Value, v *value) error {
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.ReadToken()
	if err != nil || tok.Kind() != '{' {
		return fmt.Errorf("reading a value: %s is not an object: %w", raw, dbimp.ErrInvalidValue)
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading a value: %w", err)
		}
		name := tok.String()
		val, err := dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a value: %w", err)
		}
		switch name {
		case "type":
			if v.Type, err = dbimp.String(val); err != nil {
				return fmt.Errorf("reading the type of a value: %w", err)
			}
		case "value":
			v.Value = val.Clone()
		case "base64":
			if v.Base64, err = dbimp.String(val); err != nil {
				return fmt.Errorf("reading a BLOB: %w", err)
			}
		}
	}
	return nil
}

// decodeBase64 returns the bytes of a BLOB, which the server writes in
// base64 with no padding (measured), and reads with it or without it.
func decodeBase64(s string) ([]byte, error) {
	b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return nil, fmt.Errorf("reading a BLOB in base64: %w", dbimp.ErrInvalidValue)
	}
	return b, nil
}

// The layouts of a date and a time in text, as mattn/go-sqlite3 reads them
// (D147). The zoned layouts come first.
var (
	zonedLayouts = []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00",
	}
	localLayouts = []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02",
	}
)

// decodeText returns a text of a column of the affinity a: a DATE as a
// dbimp.Date, a DATETIME as a dbimp.LocalDateTime, a TIMESTAMP as a time.Time,
// and any other, or a text that does not parse, as a string (D147).
func decodeText(a dbimp.Affinity, s string) any {
	switch a {
	case dbimp.AffinityDate:
		if t, err := time.Parse(time.DateOnly, s); err == nil {
			return dbimp.DateOf(t)
		}
	case dbimp.AffinityDateTime:
		for _, l := range localLayouts {
			if t, err := time.Parse(l, s); err == nil {
				return dbimp.LocalDateTimeOf(t)
			}
		}
	case dbimp.AffinityTimestamp:
		for _, l := range zonedLayouts {
			if t, err := time.Parse(l, s); err == nil {
				return t
			}
		}
		// Text with no zone is a time in UTC, as mattn/go-sqlite3 reads it
		// (D147).
		for _, l := range localLayouts {
			if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
				return t
			}
		}
	}
	return s
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
	typeOfVector        = reflect.TypeFor[dbimp.Vector[float32]]()
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the column c (D140 and D147).
func scanType(c column) reflect.Type {
	if c.vector {
		return typeOfVector
	}
	switch c.affinity {
	case dbimp.AffinityInteger:
		return typeOfInt64
	case dbimp.AffinityReal:
		return typeOfFloat64
	case dbimp.AffinityText:
		return typeOfString
	case dbimp.AffinityBlob:
		return typeOfBytes
	case dbimp.AffinityBoolean:
		return typeOfBool
	case dbimp.AffinityDate:
		return typeOfDate
	case dbimp.AffinityDateTime:
		return typeOfLocalDateTime
	case dbimp.AffinityTimestamp:
		return typeOfTime
	}
	return typeOfAny
}

// arg returns the argument v as a value of Hrana, which the server binds as
// its storage class (D152).
func arg(v any) (map[string]any, error) {
	switch v := v.(type) {
	case nil:
		return map[string]any{"type": "null"}, nil
	case bool:
		i := "0"
		if v {
			i = "1"
		}
		return map[string]any{"type": "integer", "value": i}, nil
	case int64:
		return map[string]any{"type": "integer", "value": strconv.FormatInt(v, 10)}, nil
	case uint64:
		if v > math.MaxInt64 {
			return nil, fmt.Errorf("writing %d: SQLite has no integer above 9223372036854775807: %w", v, dbimp.ErrInvalidValue)
		}
		return map[string]any{"type": "integer", "value": strconv.FormatUint(v, 10)}, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("writing %v: the server takes no float that is not finite: %w", v, dbimp.ErrInvalidValue)
		}
		return map[string]any{"type": "float", "value": v}, nil
	case string:
		return map[string]any{"type": "text", "value": v}, nil
	case []byte:
		// The server writes base64 with no padding (measured), and reads
		// either.
		return map[string]any{"type": "blob", "base64": base64.RawStdEncoding.EncodeToString(v)}, nil
	case time.Time:
		return map[string]any{"type": "text", "value": v.Format(time.RFC3339Nano)}, nil
	case dbimp.Date:
		return map[string]any{"type": "text", "value": v.String()}, nil
	case dbimp.LocalTime:
		return map[string]any{"type": "text", "value": v.String()}, nil
	case dbimp.LocalDateTime:
		return map[string]any{"type": "text", "value": v.String()}, nil
	}
	return nil, fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}
