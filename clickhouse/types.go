package clickhouse

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The scan types of the columns.
var (
	typeOfBool     = reflect.TypeFor[bool]()
	typeOfInt64    = reflect.TypeFor[int64]()
	typeOfUint64   = reflect.TypeFor[uint64]()
	typeOfBig      = reflect.TypeFor[*big.Int]()
	typeOfFloat64  = reflect.TypeFor[float64]()
	typeOfDecimal  = reflect.TypeFor[*apd.Decimal]()
	typeOfString   = reflect.TypeFor[string]()
	typeOfBytes    = reflect.TypeFor[[]byte]()
	typeOfDate     = reflect.TypeFor[dbimp.Date]()
	typeOfTime     = reflect.TypeFor[time.Time]()
	typeOfDuration = reflect.TypeFor[time.Duration]()
	typeOfInterval = reflect.TypeFor[dbimp.Interval]()
	typeOfUUID     = reflect.TypeFor[uuid.UUID]()
	typeOfAddr     = reflect.TypeFor[netip.Addr]()
	typeOfList     = reflect.TypeFor[[]any]()
	typeOfMap      = reflect.TypeFor[map[string]any]()
	typeOfVector   = reflect.TypeFor[dbimp.Vector[float32]]()
	typeOfAny      = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the type t (D135 and D177).
func (t *typ) scanType() reflect.Type {
	switch t.family {
	case famBool:
		return typeOfBool
	case famInt8, famInt16, famInt32, famInt64, famUInt8, famUInt16, famUInt32:
		return typeOfInt64
	case famUInt64:
		return typeOfUint64
	case famInt128, famInt256, famUInt128, famUInt256:
		return typeOfBig
	case famFloat32, famFloat64, famBFloat16:
		return typeOfFloat64
	case famDecimal:
		return typeOfDecimal
	case famString, famFixedString, famEnum8, famEnum16:
		return typeOfString
	case famDate, famDate32:
		return typeOfDate
	case famDateTime, famDateTime64:
		return typeOfTime
	case famTime, famTime64:
		return typeOfDuration
	case famInterval:
		return typeOfInterval
	case famUUID:
		return typeOfUUID
	case famIPv4, famIPv6:
		return typeOfAddr
	case famArray, famTuple, famPoint, famRing, famPolygon, famMultiPolygon, famLineString, famMultiLineString, famMultiPoint, famGeometry:
		return typeOfList
	case famMap:
		return typeOfMap
	case famAggregateFunction:
		return typeOfBytes
	case famQBit:
		return typeOfVector
	}
	return typeOfAny
}

// databaseType returns the name of the type in upper case, without its
// arguments, such as INT8 or DATETIME64.
func (t *typ) databaseType() string {
	if t.family == famInterval {
		return "INTERVAL"
	}
	return strings.ToUpper(t.family)
}

// isNullable reports whether a column of the type can hold a NULL: its type is
// Nullable, or it is a type that holds a NULL with no wrapper.
func (t *typ) isNullable() bool {
	switch t.family {
	case famNothing, famVariant, famDynamic, famJSON:
		return true
	}
	return t.nullable
}

// canBeNull reports whether a type of the family can hold a NULL, as the type
// table says: it can be inside Nullable, or it holds a NULL with no wrapper. A
// Nullable cannot wrap an Array, a Map, an AggregateFunction, or a geometry other
// than a Point, and a Variant, a Dynamic and a JSON hold a NULL with no wrapper
// (measured).
func canBeNull(family string) bool {
	switch family {
	case famArray, famMap, famAggregateFunction, famRing, famPolygon, famMultiPolygon, famLineString, famMultiLineString, famMultiPoint, famGeometry:
		return false
	}
	return true
}

// decode returns the value v of a column of the type t as its Go value (D135
// and D177). A NULL is nil for every type. A value whose type has no Go type in
// the table is the decoded JSON value.
func (t *typ) decode(v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch t.family {
	case famNothing:
		return nil, nil
	case famBool:
		return dbimp.Bool(v)
	case famInt8, famInt16, famInt32, famInt64, famUInt8, famUInt16, famUInt32:
		return decodeInt(v)
	case famUInt64:
		return decodeUint(v)
	case famInt128, famInt256, famUInt128, famUInt256:
		return decodeBig(v)
	case famFloat32, famFloat64, famBFloat16:
		return decodeFloat(v)
	case famDecimal:
		return dbimp.Decimal(v)
	case famString, famFixedString, famEnum8, famEnum16:
		return decodeString(v)
	case famDate, famDate32:
		return decodeDate(v)
	case famDateTime, famDateTime64:
		return t.decodeInstant(v)
	case famTime, famTime64:
		return decodeClock(v)
	case famInterval:
		return t.decodeInterval(v)
	case famUUID:
		return decodeUUID(v)
	case famIPv4, famIPv6:
		return decodeAddr(v)
	case famArray:
		return t.decodeArray(v)
	case famTuple:
		return t.decodeTuple(v)
	case famMap:
		return t.decodeMap(v)
	case famPoint, famRing, famPolygon, famMultiPolygon, famLineString, famMultiLineString, famMultiPoint, famGeometry:
		return decodeGeometry(v)
	case famAggregateFunction:
		return decodeBytes(v)
	case famQBit:
		return decodeVector(v)
	}
	return decodeAny(v)
}

// newDecoder returns a decoder of the raw JSON value v. It allows the bytes
// that are not UTF-8, which a String can hold, as the decoder of the rows does.
func newDecoder(v jsontext.Value) *jsontext.Decoder {
	return jsontext.NewDecoder(bytes.NewReader(v), jsontext.AllowInvalidUTF8(true))
}

// numberText returns the text of a JSON number, or of a JSON string that holds
// one. The driver turns the quotes of the 64-bit integers off, and this reads
// either form.
func numberText(v jsontext.Value) (string, error) {
	switch v.Kind() {
	case '0':
		return string(bytes.TrimSpace(v)), nil
	case '"':
		b, err := unquote(v)
		return string(b), err
	}
	return "", fmt.Errorf("reading %s as a number: %w", v.Kind(), dbimp.ErrInvalidValue)
}

// decodeInt returns an integer of 64 bits or fewer as an int64.
func decodeInt(v jsontext.Value) (int64, error) {
	text, err := numberText(v)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %q as an integer: %w", text, dbimp.ErrInvalidValue)
	}
	return n, nil
}

// decodeUint returns a UInt64.
func decodeUint(v jsontext.Value) (uint64, error) {
	text, err := numberText(v)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %q as an unsigned integer: %w", text, dbimp.ErrInvalidValue)
	}
	return n, nil
}

// decodeBig returns an integer of 128 or 256 bits.
func decodeBig(v jsontext.Value) (*big.Int, error) {
	text, err := numberText(v)
	if err != nil {
		return nil, err
	}
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, fmt.Errorf("reading %q as an integer: %w", text, dbimp.ErrInvalidValue)
	}
	return n, nil
}

// decodeFloat returns a float. The server writes a NaN and an infinity as the
// strings "nan", "inf" and "-inf" when the driver sets
// output_format_json_quote_denormals (D176).
func decodeFloat(v jsontext.Value) (float64, error) {
	if v.Kind() == '0' {
		return dbimp.Float64(v)
	}
	text, err := numberText(v)
	if err != nil {
		return 0, err
	}
	switch strings.ToLower(text) {
	case "nan":
		return math.NaN(), nil
	case "inf", "+inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %q as a float: %w", text, dbimp.ErrInvalidValue)
	}
	return f, nil
}

// decodeString returns a JSON string with the bytes that it holds, which are
// not always UTF-8 (measured).
func decodeString(v jsontext.Value) (string, error) {
	if v.Kind() != '"' {
		return "", fmt.Errorf("reading %s as a string: %w", v.Kind(), dbimp.ErrInvalidValue)
	}
	b, err := unquote(v)
	return string(b), err
}

// decodeBytes returns a JSON string as the bytes that it holds.
func decodeBytes(v jsontext.Value) ([]byte, error) {
	if v.Kind() != '"' {
		return nil, fmt.Errorf("reading %s as bytes: %w", v.Kind(), dbimp.ErrInvalidValue)
	}
	return unquote(v)
}

// unquote returns the bytes of the JSON string v. A byte that is not UTF-8
// stays as it is, which jsontext.AppendUnquote does not do, and an escape of a
// lone surrogate is U+FFFD.
func unquote(v jsontext.Value) ([]byte, error) {
	s := bytes.TrimSpace(v)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return nil, fmt.Errorf("reading a string: it has no quotes: %w", dbimp.ErrInvalidValue)
	}
	s = s[1 : len(s)-1]
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			out = append(out, c)
			continue
		}
		i++
		if i >= len(s) {
			return nil, fmt.Errorf("reading a string: it ends in a backslash: %w", dbimp.ErrInvalidValue)
		}
		switch s[i] {
		case '"', '\\', '/':
			out = append(out, s[i])
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			r, n, ok := unquoteRune(s[i+1:])
			if !ok {
				return nil, fmt.Errorf("reading a string: a bad escape at %d: %w", i, dbimp.ErrInvalidValue)
			}
			out = utf8.AppendRune(out, r)
			i += n
		default:
			return nil, fmt.Errorf("reading a string: the escape \\%c: %w", s[i], dbimp.ErrInvalidValue)
		}
	}
	return out, nil
}

// unquoteRune reads the four hex digits of a \u escape, and the second escape of
// a surrogate pair. It returns the rune and the count of the bytes that it
// read.
func unquoteRune(s []byte) (rune, int, bool) {
	hex := func(b []byte) (rune, bool) {
		if len(b) < 4 {
			return 0, false
		}
		n, err := strconv.ParseUint(string(b[:4]), 16, 32)
		return rune(n), err == nil //nolint:gosec // G115: four hex digits fit in a rune.
	}
	r, ok := hex(s)
	if !ok {
		return 0, 0, false
	}
	if r < 0xD800 || r >= 0xE000 {
		return r, 4, true
	}
	if r < 0xDC00 && len(s) >= 10 && s[4] == '\\' && s[5] == 'u' {
		if low, ok := hex(s[6:]); ok && low >= 0xDC00 && low < 0xE000 {
			return (r-0xD800)<<10 + (low - 0xDC00) + 0x10000, 10, true
		}
	}
	return utf8.RuneError, 4, true
}

// decodeDate returns a date, such as 2026-10-07.
func decodeDate(v jsontext.Value) (dbimp.Date, error) {
	s, err := decodeString(v)
	if err != nil {
		return dbimp.Date{}, err
	}
	return dbimp.ParseDate(s)
}

// decodeInstant returns a DateTime or a DateTime64 as the instant that the
// text names. The driver sets date_time_output_format to iso, so the text is
// UTC with a Z, such as 2026-11-01T05:30:00.000Z, and no hour of a change of
// the clock is ambiguous (D177). The time is in the zone of the type, when the
// type names one that the host knows, and UTC otherwise.
func (t *typ) decodeInstant(v jsontext.Value) (time.Time, error) {
	s, err := decodeString(v)
	if err != nil {
		return time.Time{}, err
	}
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading %q as a time: %w: %w", s, dbimp.ErrInvalidValue, err)
	}
	if t.zone != "" {
		if loc, err := time.LoadLocation(t.zone); err == nil {
			return at.In(loc), nil
		}
	}
	return at.UTC(), nil
}

// decodeClock returns a Time or a Time64 as a duration. The text is HH:MM:SS
// with a fraction for a Time64, and a sign, and the hours go to 999. With
// date_time_output_format=iso, which the driver sets, 25.8 ends the text with a Z,
// as in 12:34:56Z, which means no zone here and is dropped (measured).
func decodeClock(v jsontext.Value) (time.Duration, error) {
	s, err := decodeString(v)
	if err != nil {
		return 0, err
	}
	s = strings.TrimSuffix(s, "Z")
	bad := func() (time.Duration, error) {
		return 0, fmt.Errorf("reading %q as a time: %w", s, dbimp.ErrInvalidValue)
	}
	text, neg := strings.CutPrefix(s, "-")
	whole, frac, _ := strings.Cut(text, ".")
	parts := strings.Split(whole, ":")
	if len(parts) != 3 || len(frac) > 9 {
		return bad()
	}
	var n [3]int64
	for i, p := range parts {
		if p == "" || len(p) > 3 {
			return bad()
		}
		x, err := strconv.ParseInt(p, 10, 64)
		if err != nil || x < 0 {
			return bad()
		}
		n[i] = x
	}
	var nanos int64
	if frac != "" {
		x, err := strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
		if err != nil {
			return bad()
		}
		nanos = x
	}
	d := time.Duration(((n[0]*60+n[1])*60+n[2])*1e9 + nanos)
	if neg {
		d = -d
	}
	return d, nil
}

// decodeInterval returns an Interval. The value is a number of one unit, which
// the type names (measured). A year is 12 months, a quarter is 3 months, a week
// is 7 days, and the units below a day are nanoseconds (D177).
func (t *typ) decodeInterval(v jsontext.Value) (dbimp.Interval, error) {
	n, err := decodeInt(v)
	if err != nil {
		return dbimp.Interval{}, err
	}
	var (
		per   int64
		limit int64 = math.MaxInt32
		part  func(int64) dbimp.Interval
	)
	asMonths := func(x int64) dbimp.Interval { return dbimp.Interval{Months: int32(x)} } //nolint:gosec // G115: the limit is checked.
	asDays := func(x int64) dbimp.Interval { return dbimp.Interval{Days: int32(x)} }     //nolint:gosec // G115: the limit is checked.
	asNanos := func(x int64) dbimp.Interval { return dbimp.Interval{Nanoseconds: x} }
	switch t.unit {
	case "Nanosecond":
		per, limit, part = 1, math.MaxInt64, asNanos
	case "Microsecond":
		per, limit, part = 1e3, math.MaxInt64, asNanos
	case "Millisecond":
		per, limit, part = 1e6, math.MaxInt64, asNanos
	case "Second":
		per, limit, part = 1e9, math.MaxInt64, asNanos
	case "Minute":
		per, limit, part = 60e9, math.MaxInt64, asNanos
	case "Hour":
		per, limit, part = 3600e9, math.MaxInt64, asNanos
	case "Day":
		per, part = 1, asDays
	case "Week":
		per, part = 7, asDays
	case "Month":
		per, part = 1, asMonths
	case "Quarter":
		per, part = 3, asMonths
	case "Year":
		per, part = 12, asMonths
	default:
		return dbimp.Interval{}, fmt.Errorf("reading an interval: the unit %q is not one that the server has: %w", t.unit, dbimp.ErrInvalidValue)
	}
	if n > limit/per || n < -limit/per {
		return dbimp.Interval{}, fmt.Errorf("reading %d %s as an interval: it does not fit: %w", n, t.unit, dbimp.ErrInvalidValue)
	}
	return part(n * per), nil
}

// decodeUUID returns a UUID.
func decodeUUID(v jsontext.Value) (uuid.UUID, error) {
	s, err := decodeString(v)
	if err != nil {
		return uuid.UUID{}, err
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("reading %q as a UUID: %w: %w", s, dbimp.ErrInvalidValue, err)
	}
	return u, nil
}

// decodeAddr returns an IPv4 or an IPv6 address. An IPv4 that the server maps
// into an IPv6 stays an IPv6 address (D177).
func decodeAddr(v jsontext.Value) (netip.Addr, error) {
	s, err := decodeString(v)
	if err != nil {
		return netip.Addr{}, err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("reading %q as an address: %w: %w", s, dbimp.ErrInvalidValue, err)
	}
	return a, nil
}

// decodeArray returns an Array as a []any, with each element decoded by the
// type of the elements.
func (t *typ) decodeArray(v jsontext.Value) ([]any, error) {
	dec := newDecoder(v)
	if err := expectKind(dec, '['); err != nil {
		return nil, fmt.Errorf("reading an array: %w", err)
	}
	out := []any{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading an array: %w", err)
		}
		e, err := t.elems[0].decode(ev)
		if err != nil {
			return nil, fmt.Errorf("reading an element of an array: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}

// decodeTuple returns a Tuple as a []any, in the order of its fields. The driver
// sets output_format_json_named_tuples_as_objects to 0, so every tuple is an
// array, and a caller who sets it back gets an object, which this reads in the
// order of its members (D176).
func (t *typ) decodeTuple(v jsontext.Value) ([]any, error) {
	dec := newDecoder(v)
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, fmt.Errorf("reading a tuple: %w", err)
	}
	var end jsontext.Kind
	switch tok.Kind() {
	case '[':
		end = ']'
	case '{':
		end = '}'
	default:
		return nil, fmt.Errorf("reading a tuple: %v where an array was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
	}
	out := make([]any, 0, len(t.elems))
	for i := 0; dec.PeekKind() != end; i++ {
		if end == '}' {
			// The name of the member is read and dropped.
			if _, err := dec.ReadValue(); err != nil {
				return nil, fmt.Errorf("reading a tuple: %w", err)
			}
		}
		fv, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a field of a tuple: %w", err)
		}
		if i >= len(t.elems) {
			return nil, fmt.Errorf("reading a tuple: more than %d fields: %w", len(t.elems), dbimp.ErrColumnCount)
		}
		f, err := t.elems[i].decode(fv)
		if err != nil {
			return nil, fmt.Errorf("reading the field %d of a tuple: %w", i, err)
		}
		out = append(out, f)
	}
	if len(out) != len(t.elems) {
		return nil, fmt.Errorf("reading a tuple: %d values for %d fields: %w", len(out), len(t.elems), dbimp.ErrColumnCount)
	}
	return out, nil
}

// decodeMap returns a Map as a map[string]any. A key of another type than
// String stays the text of the JSON object, such as 1 or (1,2) (D177).
func (t *typ) decodeMap(v jsontext.Value) (map[string]any, error) {
	dec := newDecoder(v)
	if err := expectKind(dec, '{'); err != nil {
		return nil, fmt.Errorf("reading a map: %w", err)
	}
	out := map[string]any{}
	for dec.PeekKind() != '}' {
		kv, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a key of a map: %w", err)
		}
		key, err := decodeString(kv)
		if err != nil {
			return nil, fmt.Errorf("reading a key of a map: %w", err)
		}
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a value of a map: %w", err)
		}
		e, err := t.elems[1].decode(ev)
		if err != nil {
			return nil, fmt.Errorf("reading the value of the key %q of a map: %w", key, err)
		}
		out[key] = e
	}
	return out, nil
}

// decodeGeometry returns a geometry as nested []any of float64, which is the
// JSON that the server writes (D177).
func decodeGeometry(v jsontext.Value) (any, error) {
	if v.Kind() != '[' {
		f, err := decodeFloat(v)
		if err != nil {
			return nil, fmt.Errorf("reading a geometry: %w", err)
		}
		return f, nil
	}
	dec := newDecoder(v)
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading a geometry: %w", err)
	}
	out := []any{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a geometry: %w", err)
		}
		e, err := decodeGeometry(ev)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// decodeVector returns a QBit as a dbimp.Vector[float32].
func decodeVector(v jsontext.Value) (dbimp.Vector[float32], error) {
	dec := newDecoder(v)
	if err := expectKind(dec, '['); err != nil {
		return nil, fmt.Errorf("reading a vector: %w", err)
	}
	out := dbimp.Vector[float32]{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a vector: %w", err)
		}
		text, err := numberText(ev)
		if err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(text, 32)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a float: %w", text, dbimp.ErrInvalidValue)
		}
		out = append(out, float32(f))
	}
	return out, nil
}

// decodeAny returns the JSON value v as nil, bool, string, a number as
// dbimp.Number reads it, []any or map[string]any. It is the value of a Variant,
// a Dynamic and a JSON, whose member types the format does not say (D176).
func decodeAny(v jsontext.Value) (any, error) {
	switch v.Kind() {
	case 'n':
		return nil, nil
	case 't', 'f':
		return dbimp.Bool(v)
	case '"':
		return decodeString(v)
	case '0':
		return dbimp.Number(v)
	case '[':
		dec := newDecoder(v)
		if _, err := dec.ReadToken(); err != nil {
			return nil, fmt.Errorf("reading an array: %w", err)
		}
		out := []any{}
		for dec.PeekKind() != ']' {
			ev, err := dec.ReadValue()
			if err != nil {
				return nil, fmt.Errorf("reading an array: %w", err)
			}
			e, err := decodeAny(ev)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, nil
	case '{':
		dec := newDecoder(v)
		if _, err := dec.ReadToken(); err != nil {
			return nil, fmt.Errorf("reading an object: %w", err)
		}
		out := map[string]any{}
		for dec.PeekKind() != '}' {
			kv, err := dec.ReadValue()
			if err != nil {
				return nil, fmt.Errorf("reading an object: %w", err)
			}
			key, err := decodeString(kv)
			if err != nil {
				return nil, err
			}
			ev, err := dec.ReadValue()
			if err != nil {
				return nil, fmt.Errorf("reading an object: %w", err)
			}
			if out[key], err = decodeAny(ev); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("reading a value: %w", dbimp.ErrInvalidValue)
}

// expectKind reads one token, and returns an error if it is not of kind.
func expectKind(dec *jsontext.Decoder, kind jsontext.Kind) error {
	tok, err := dec.ReadToken()
	switch {
	case errors.Is(err, io.EOF):
		return fmt.Errorf("the value is empty: %w", dbimp.ErrInvalidValue)
	case err != nil:
		return err
	case tok.Kind() != kind:
		return fmt.Errorf("%v where %v was expected: %w", tok.Kind(), kind, dbimp.ErrInvalidValue)
	}
	return nil
}
