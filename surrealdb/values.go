package surrealdb

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The CBOR tags of SurrealDB, which its SDKs read and write (D53).
const (
	tagSpecDatetime = 0
	tagNone         = 6
	tagTable        = 7
	tagRecordID     = 8
	tagStringUUID   = 9
	tagDecimal      = 10
	tagDatetime     = 12
	tagStringDur    = 13
	tagDuration     = 14
	tagFuture       = 15
	tagUUID         = 37
	tagRange        = 49
	tagIncluded     = 50
	tagExcluded     = 51
	tagFile         = 55
	tagSet          = 56
	tagPoint        = 88
	tagLine         = 89
	tagPolygon      = 90
	tagMultiPoint   = 91
	tagMultiLine    = 92
	tagMultiPolygon = 93
	tagCollection   = 94
)

// wireTypes are the types that SurrealDB sends, with the Go type that the
// driver returns for each in CBOR (D53). The type table of
// docs/SURREALDB.md comes from them. A response names no type for a column,
// because each record holds what it holds, so each column scans into any.
var wireTypes = []struct {
	name string
	goes string
}{
	{"none", "nil (tag 6)"},
	{"null", "nil"},
	{"bool", "bool"},
	{"int", "int64"},
	{"float", "float64"},
	{"decimal", "*apd.Decimal (tag 10)"},
	{"string", "string"},
	{"datetime", "time.Time in UTC (tags 0 and 12)"},
	{"duration", "time.Duration (tags 13 and 14)"},
	{"uuid", "uuid.UUID (tags 9 and 37)"},
	{"bytes", "[]byte"},
	{"array", "[]any"},
	{"set", "[]any (tag 56)"},
	{"object", "map[string]any"},
	{"record", "RecordID (tag 8)"},
	{"geometry", "map[string]any of GeoJSON (tags 88 to 94)"},
	{"range", "string, such as 1..5 (tags 49 to 51)"},
	{"table", "string (tag 7)"},
}

// geometryTypes are the GeoJSON names of the tags of a geometry.
var geometryTypes = map[uint64]string{
	tagPoint:        "Point",
	tagLine:         "LineString",
	tagPolygon:      "Polygon",
	tagMultiPoint:   "MultiPoint",
	tagMultiLine:    "MultiLineString",
	tagMultiPolygon: "MultiPolygon",
	tagCollection:   "GeometryCollection",
}

// decodeCBOR returns the value of one CBOR item of SurrealDB as its Go value
// (D53).
func decodeCBOR(raw []byte) (any, error) {
	return readValue(dbimp.NewCBORDecoder(bytes.NewReader(raw)))
}

// readValue reads the next item of d as its Go value.
func readValue(d *dbimp.CBORDecoder) (any, error) {
	h, err := d.ReadHead()
	if err != nil {
		return nil, err
	}
	switch h.Major {
	case dbimp.CBORUint:
		if h.Arg > math.MaxInt64 {
			return apd.NewWithBigInt(new(apd.BigInt).SetMathBigInt(new(big.Int).SetUint64(h.Arg)), 0), nil
		}
		return int64(h.Arg), nil
	case dbimp.CBORNegInt:
		if h.Arg > math.MaxInt64 {
			n := new(big.Int).SetUint64(h.Arg)
			n.Neg(n.Add(n, big.NewInt(1)))
			return apd.NewWithBigInt(new(apd.BigInt).SetMathBigInt(n), 0), nil
		}
		return -1 - int64(h.Arg), nil
	case dbimp.CBORBytes:
		b, err := d.ReadString(h)
		if b == nil && err == nil {
			b = []byte{}
		}
		return b, err
	case dbimp.CBORText:
		b, err := d.ReadString(h)
		return string(b), err
	case dbimp.CBORArray:
		a := []any{}
		for i := uint64(0); ; i++ {
			more, err := d.More(h, i)
			if err != nil {
				return nil, err
			}
			if !more {
				return a, nil
			}
			v, err := readValue(d)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
	case dbimp.CBORMap:
		m := map[string]any{}
		for i := uint64(0); ; i += 2 {
			more, err := d.More(h, i)
			if err != nil {
				return nil, err
			}
			if !more {
				return m, nil
			}
			k, err := d.ReadText()
			if err != nil {
				return nil, fmt.Errorf("reading the key of an object: %w", err)
			}
			v, err := readValue(d)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
	case dbimp.CBORTag:
		return readTag(d, h.Arg)
	case dbimp.CBORSimple:
		if f, ok := h.Float(); ok {
			return f, nil
		}
		if b, ok := h.Bool(); ok {
			return b, nil
		}
		if h.Null() {
			return nil, nil
		}
	}
	return nil, fmt.Errorf("reading CBOR major type %d, argument %d: %w", h.Major, h.Arg, dbimp.ErrInvalidValue)
}

// readTag reads the item of the tag num.
func readTag(d *dbimp.CBORDecoder, num uint64) (any, error) {
	v, err := readValue(d)
	if err != nil {
		return nil, err
	}
	switch num {
	case tagNone:
		return nil, nil
	case tagTable:
		return asString(num, v)
	case tagSpecDatetime:
		s, err := asString(num, v)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, fmt.Errorf("reading the datetime %q: %w", s, dbimp.ErrInvalidValue)
		}
		return t.UTC(), nil
	case tagDatetime:
		secs, nanos, err := pair(num, v)
		if err != nil {
			return nil, err
		}
		return time.Unix(secs, nanos).UTC(), nil
	case tagStringDur:
		s, err := asString(num, v)
		if err != nil {
			return nil, err
		}
		return parseDuration(s)
	case tagDuration:
		secs, nanos, err := pair(num, v)
		if err != nil {
			return nil, err
		}
		if secs > (math.MaxInt64-nanos)/int64(time.Second) {
			return nil, fmt.Errorf("reading a duration of %d seconds: longer than a time.Duration holds: %w", secs, dbimp.ErrInvalidValue)
		}
		return time.Duration(secs)*time.Second + time.Duration(nanos), nil
	case tagStringUUID:
		s, err := asString(num, v)
		if err != nil {
			return nil, err
		}
		u, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("reading the uuid %q: %w", s, dbimp.ErrInvalidValue)
		}
		return u, nil
	case tagUUID:
		b, ok := v.([]byte)
		if !ok || len(b) != 16 {
			return nil, fmt.Errorf("reading a uuid of %T: %w", v, dbimp.ErrInvalidValue)
		}
		return uuid.UUID(b), nil
	case tagDecimal:
		s, err := asString(num, v)
		if err != nil {
			return nil, err
		}
		dec, _, err := apd.NewFromString(s)
		if err != nil {
			return nil, fmt.Errorf("reading the decimal %q: %w", s, dbimp.ErrInvalidValue)
		}
		return dec, nil
	case tagRecordID:
		return recordID(v)
	case tagSet:
		if a, ok := v.([]any); ok {
			return a, nil
		}
		return nil, fmt.Errorf("reading a set of %T: %w", v, dbimp.ErrInvalidValue)
	case tagRange:
		return rangeString(v)
	case tagIncluded, tagExcluded:
		return bound{num: num, v: v}, nil
	case tagFile:
		a, ok := v.([]any)
		if !ok || len(a) != 2 {
			return nil, fmt.Errorf("reading a file of %T: %w", v, dbimp.ErrInvalidValue)
		}
		return fmt.Sprintf("f'%v:%v'", a[0], a[1]), nil
	case tagFuture:
		return fmt.Sprintf("<future> { %s }", literal(v)), nil
	}
	if name, ok := geometryTypes[num]; ok {
		return geometry(num, name, v)
	}
	return nil, fmt.Errorf("reading the CBOR tag %d: %w", num, dbimp.ErrNotSupported)
}

// bound is the bound of a range, before readTag of the range reads it.
type bound struct {
	num uint64
	v   any
}

// rangeString writes a range as SurrealQL writes it, such as 1..5, 1..=5,
// 1>..5 or ..5 (D53).
func rangeString(v any) (string, error) {
	a, ok := v.([]any)
	if !ok || len(a) != 2 {
		return "", fmt.Errorf("reading a range of %T: %w", v, dbimp.ErrInvalidValue)
	}
	var s string
	if b, ok := a[0].(bound); ok {
		s = literal(b.v)
		if b.num == tagExcluded {
			s += ">"
		}
	} else if a[0] != nil {
		return "", fmt.Errorf("reading the start of a range: %w", dbimp.ErrInvalidValue)
	}
	s += ".."
	if b, ok := a[1].(bound); ok {
		if b.num == tagIncluded {
			s += "="
		}
		s += literal(b.v)
	} else if a[1] != nil {
		return "", fmt.Errorf("reading the end of a range: %w", dbimp.ErrInvalidValue)
	}
	return s, nil
}

// recordID reads the item of the tag of a record id: [table, key], or the
// text table:key.
func recordID(v any) (RecordID, error) {
	switch v := v.(type) {
	case []any:
		if len(v) != 2 {
			break
		}
		table, ok := v[0].(string)
		if !ok {
			break
		}
		return RecordID{Table: table, ID: v[1]}, nil
	case string:
		for i := range len(v) {
			if v[i] == ':' {
				return RecordID{Table: v[:i], ID: v[i+1:]}, nil
			}
		}
	}
	return RecordID{}, fmt.Errorf("reading a record id of %T: %w", v, dbimp.ErrInvalidValue)
}

// geometry returns the GeoJSON object of the geometry of the tag num (D53).
func geometry(num uint64, name string, v any) (map[string]any, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("reading a geometry of %T: %w", v, dbimp.ErrInvalidValue)
	}
	if num == tagCollection {
		return map[string]any{"type": name, "geometries": a}, nil
	}
	coords, err := coordinates(a)
	if err != nil {
		return nil, err
	}
	return map[string]any{"type": name, "coordinates": coords}, nil
}

// coordinates turns each nested geometry of a into its coordinates, as
// GeoJSON writes them.
func coordinates(a []any) ([]any, error) {
	out := make([]any, len(a))
	for i, e := range a {
		switch e := e.(type) {
		case map[string]any:
			c, ok := e["coordinates"]
			if !ok {
				return nil, fmt.Errorf("reading a geometry inside a geometry: %w", dbimp.ErrInvalidValue)
			}
			out[i] = c
		default:
			out[i] = e
		}
	}
	return out, nil
}

// asString returns v, the item of the tag num, as a string.
func asString(num uint64, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("reading the CBOR tag %d of %T: %w", num, v, dbimp.ErrInvalidValue)
	}
	return s, nil
}

// pair reads the item [seconds, nanoseconds] of a datetime or a duration, in
// which each part can be left out when it is zero.
func pair(num uint64, v any) (int64, int64, error) {
	a, ok := v.([]any)
	if !ok || len(a) > 2 {
		return 0, 0, fmt.Errorf("reading the CBOR tag %d of %T: %w", num, v, dbimp.ErrInvalidValue)
	}
	var parts [2]int64
	for i, e := range a {
		n, ok := e.(int64)
		if !ok {
			return 0, 0, fmt.Errorf("reading the CBOR tag %d: %T is not an integer: %w", num, e, dbimp.ErrInvalidValue)
		}
		parts[i] = n
	}
	if parts[1] < 0 || parts[1] >= int64(time.Second) {
		return 0, 0, fmt.Errorf("reading the CBOR tag %d: %d nanoseconds: %w", num, parts[1], dbimp.ErrInvalidValue)
	}
	return parts[0], parts[1], nil
}

// encodeCBOR appends v to e, with the tags of SurrealDB, so that the server
// keeps its type (D53). A value of a type that it does not know goes through
// json/v2, and then becomes CBOR, as an object, an array, a string, a number,
// a boolean or null.
func encodeCBOR(e *dbimp.CBOREncoder, v any) error {
	switch v := v.(type) {
	case nil:
		e.Null()
	case bool:
		e.Bool(v)
	case string:
		e.Text(v)
	case []byte:
		e.ByteString(v)
	case int:
		e.Int(int64(v))
	case int8:
		e.Int(int64(v))
	case int16:
		e.Int(int64(v))
	case int32:
		e.Int(int64(v))
	case int64:
		e.Int(v)
	case uint:
		e.Uint(uint64(v))
	case uint8:
		e.Uint(uint64(v))
	case uint16:
		e.Uint(uint64(v))
	case uint32:
		e.Uint(uint64(v))
	case uint64:
		e.Uint(v)
	case float32:
		e.Float(float64(v))
	case float64:
		e.Float(v)
	case time.Time:
		e.Tag(tagDatetime)
		e.Array(2)
		e.Int(v.Unix())
		e.Int(int64(v.Nanosecond()))
	case time.Duration:
		if v < 0 {
			return fmt.Errorf("writing the negative duration %s: %w", v, dbimp.ErrInvalidValue)
		}
		e.Tag(tagDuration)
		e.Array(2)
		e.Int(int64(v / time.Second))
		e.Int(int64(v % time.Second))
	case uuid.UUID:
		e.Tag(tagUUID)
		e.ByteString(v[:])
	case RecordID:
		e.Tag(tagRecordID)
		e.Array(2)
		e.Text(v.Table)
		return encodeCBOR(e, v.ID)
	case *apd.Decimal:
		if v == nil {
			e.Null()
			return nil
		}
		e.Tag(tagDecimal)
		e.Text(v.String())
	case apd.Decimal:
		e.Tag(tagDecimal)
		e.Text(v.String())
	case []any:
		e.Array(len(v))
		for _, x := range v {
			if err := encodeCBOR(e, x); err != nil {
				return err
			}
		}
	case map[string]any:
		// The driver sorts the keys, so that the same value is always the
		// same bytes. The server sorts the keys of an object too.
		e.Map(len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			e.Text(k)
			if err := encodeCBOR(e, v[k]); err != nil {
				return err
			}
		}
	default:
		return encodeReflect(e, v)
	}
	return nil
}

// encodeReflect appends a slice, an array or a map with string keys by its
// elements, and any other value through json/v2.
func encodeReflect(e *dbimp.CBOREncoder, v any) error {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			e.Null()
			return nil
		}
		return encodeCBOR(e, rv.Elem().Interface())
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			e.Null()
			return nil
		}
		e.Array(rv.Len())
		for i := range rv.Len() {
			if err := encodeCBOR(e, rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			keys := rv.MapKeys()
			slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(a.String(), b.String()) })
			e.Map(len(keys))
			for _, k := range keys {
				e.Text(k.String())
				if err := encodeCBOR(e, rv.MapIndex(k).Interface()); err != nil {
					return err
				}
			}
			return nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("writing an argument of %T: %w", v, err)
	}
	return jsonToCBOR(e, jsontext.NewDecoder(bytes.NewReader(b)))
}

// jsonToCBOR appends the next JSON value of dec to e.
func jsonToCBOR(e *dbimp.CBOREncoder, dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case '{', '[':
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		object := tok.Kind() == '{'
		var inner dbimp.CBOREncoder
		n := 0
		for k := dec.PeekKind(); k != '}' && k != ']'; k = dec.PeekKind() {
			if err := jsonToCBOR(&inner, dec); err != nil {
				return err
			}
			n++
		}
		if _, err := dec.ReadToken(); err != nil {
			return err
		}
		if object {
			e.Map(n / 2)
		} else {
			e.Array(n)
		}
		e.Raw(inner.Bytes())
		return nil
	}
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	switch val.Kind() {
	case 'n':
		e.Null()
	case 't', 'f':
		e.Bool(val.Kind() == 't')
	case '"':
		s, err := dbimp.String(val)
		if err != nil {
			return err
		}
		e.Text(s)
	case '0':
		n, err := dbimp.Number(val)
		if err != nil {
			return err
		}
		return encodeCBOR(e, n)
	}
	return nil
}
