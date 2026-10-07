package trino

import (
	"bytes"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// signature is the type of a column, from the member typeSignature of the
// page that names the columns. Trino and Presto write it in two forms, and
// both read into this one (measured).
type signature struct {
	// raw is the name of the type in lower case, without its arguments,
	// such as "bigint", "timestamp with time zone" or "array".
	raw string
	// nums are the numbers in the arguments: the length of a varchar, the
	// precision of a time, the precision and the scale of a decimal.
	nums []int64
	// elems are the types in the arguments: the element of an array, the key
	// and the value of a map, and the fields of a row.
	elems []*signature
	// names are the names of the fields of a row, and "" for a field with no
	// name.
	names []string
}

// sigJSON is the member typeSignature of a column. Trino writes each
// argument as a kind and a value, with the kinds LONG, TYPE and NAMED_TYPE.
// Presto writes the types again in typeArguments, with the names of the
// fields of a row in literalArguments, and its arguments have other kinds
// (measured).
type sigJSON struct {
	RawType    string           `json:"rawType"`
	TypeArgs   []sigJSON        `json:"typeArguments"`
	LiteralArg []jsontext.Value `json:"literalArguments"`
	Args       []argJSON        `json:"arguments"`
}

// argJSON is one of the arguments of a type.
type argJSON struct {
	Kind  string         `json:"kind"`
	Value jsontext.Value `json:"value"`
}

// namedJSON is the value of an argument of the kind NAMED_TYPE, a field of
// a row on Trino.
type namedJSON struct {
	Field struct {
		Name string `json:"name"`
	} `json:"fieldName"`
	Type sigJSON `json:"typeSignature"`
}

// signature returns the signature that s holds. text is the type as the
// column writes it, for a column with no signature.
func (s sigJSON) signature(text string) (*signature, error) {
	sig := &signature{raw: strings.ToLower(s.RawType)}
	if sig.raw == "" {
		sig.raw, _, _ = strings.Cut(strings.ToLower(text), "(")
		sig.raw = strings.TrimSpace(sig.raw)
	}
	if len(s.TypeArgs) > 0 {
		for i, ta := range s.TypeArgs {
			e, err := ta.signature("")
			if err != nil {
				return nil, err
			}
			sig.elems = append(sig.elems, e)
			name := ""
			if len(s.LiteralArg) == len(s.TypeArgs) {
				name, _ = dbimp.String(s.LiteralArg[i])
			}
			sig.names = append(sig.names, name)
		}
	}
	for _, a := range s.Args {
		switch a.Kind {
		case "LONG", "LONG_LITERAL":
			n, err := dbimp.Int64(a.Value)
			if err != nil {
				return nil, fmt.Errorf("reading a number of a type: %w", err)
			}
			sig.nums = append(sig.nums, n)
		case "TYPE":
			if len(s.TypeArgs) > 0 {
				continue
			}
			var inner sigJSON
			if err := json.Unmarshal(a.Value, &inner); err != nil {
				return nil, fmt.Errorf("reading an argument of a type: %w", err)
			}
			e, err := inner.signature("")
			if err != nil {
				return nil, err
			}
			sig.elems = append(sig.elems, e)
			sig.names = append(sig.names, "")
		case "NAMED_TYPE":
			var inner namedJSON
			if err := json.Unmarshal(a.Value, &inner); err != nil {
				return nil, fmt.Errorf("reading a field of a type: %w", err)
			}
			e, err := inner.Type.signature("")
			if err != nil {
				return nil, err
			}
			sig.elems = append(sig.elems, e)
			sig.names = append(sig.names, inner.Field.Name)
		}
	}
	return sig, nil
}

// columnJSON is one element of the member columns of a page.
type columnJSON struct {
	Name string  `json:"name"`
	Type string  `json:"type"`
	Sig  sigJSON `json:"typeSignature"`
}

// column is one column of a result.
type column struct {
	name string
	// text is the type as the server writes it, such as decimal(38, 0).
	text string
	sig  *signature
}

// readColumns reads the value of the member columns.
func readColumns(v jsontext.Value) ([]column, error) {
	var raw []columnJSON
	if err := json.Unmarshal(v, &raw); err != nil {
		return nil, fmt.Errorf("reading the columns: %w", err)
	}
	cols := make([]column, len(raw))
	for i, c := range raw {
		sig, err := c.Sig.signature(c.Type)
		if err != nil {
			return nil, fmt.Errorf("reading the type of the column %s: %w", c.Name, err)
		}
		cols[i] = column{name: c.Name, text: c.Type, sig: sig}
	}
	return cols, nil
}

// The names of the types that the driver reads by name (measured). A name
// is the rawType of the signature, in lower case.
const (
	typeBoolean   = "boolean"
	typeTinyint   = "tinyint"
	typeSmallint  = "smallint"
	typeInteger   = "integer"
	typeBigint    = "bigint"
	typeReal      = "real"
	typeDouble    = "double"
	typeDecimal   = "decimal"
	typeNumber    = "number"
	typeChar      = "char"
	typeVarchar   = "varchar"
	typeVarbinary = "varbinary"
	typeJSON      = "json"
	typeVariant   = "variant"
	typeDate      = "date"
	typeTime      = "time"
	typeTimeTZ    = "time with time zone"
	typeTimestamp = "timestamp"
	typeTimestTZ  = "timestamp with time zone"
	typeIntervalY = "interval year to month"
	typeIntervalD = "interval day to second"
	typeArray     = "array"
	typeMap       = "map"
	typeRow       = "row"
	typeUUID      = "uuid"
	typeIPAddress = "ipaddress"
	typeGeometry  = "geometry"
	typeSphere    = "sphericalgeography"
	typeUnknown   = "unknown"
)

// isBinary reports whether the type holds bytes, which the server writes as
// base64: the type varbinary and the sketches and the digests (measured).
func isBinary(raw string) bool {
	switch raw {
	case typeVarbinary, "hyperloglog", "p4hyperloglog", "setdigest", "qdigest", "tdigest":
		return true
	}
	return false
}

// decode returns the value v of a column of the type s as its Go value
// (D135 and D175). A NULL is nil for every type. A value that has no Go type
// in the table is the decoded JSON value.
func (s *signature) decode(v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	switch s.raw {
	case typeBoolean:
		return dbimp.Bool(v)
	case typeTinyint, typeSmallint, typeInteger, typeBigint:
		return dbimp.Int64(v)
	case typeReal, typeDouble:
		return decodeFloat(v)
	case typeDecimal, typeNumber:
		return dbimp.Decimal(v)
	case typeChar, typeVarchar, typeIPAddress, typeGeometry, typeSphere:
		return dbimp.String(v)
	case typeJSON:
		return decodeJSON(v)
	case typeVariant:
		return dbimp.Any(v)
	case typeDate:
		return decodeDate(v)
	case typeTime, typeTimeTZ, typeTimestamp, typeTimestTZ:
		return decodeTime(s.raw, v)
	case typeIntervalY, typeIntervalD:
		return decodeInterval(s.raw, v)
	case typeArray:
		return s.decodeArray(v)
	case typeMap:
		return s.decodeMap(v)
	case typeRow:
		return s.decodeRow(v)
	case typeUUID:
		text, err := dbimp.String(v)
		if err != nil {
			return nil, err
		}
		u, err := uuid.Parse(text)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a UUID: %w: %w", text, dbimp.ErrInvalidValue, err)
		}
		return u, nil
	case typeUnknown:
		return nil, nil
	}
	if isBinary(s.raw) {
		return decodeBytes(v)
	}
	return dbimp.Any(v)
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

// decodeBytes returns a value of base64.
func decodeBytes(v jsontext.Value) ([]byte, error) {
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

// decodeJSON returns a value of the type json, which the server sends as the
// JSON text in a string (measured), as the decoded JSON value.
func decodeJSON(v jsontext.Value) (any, error) {
	if v.Kind() != '"' {
		return dbimp.Any(v)
	}
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

// decodeDate returns a date, such as 2026-10-01 or -0001-01-01.
func decodeDate(v jsontext.Value) (dbimp.Date, error) {
	s, err := dbimp.String(v)
	if err != nil {
		return dbimp.Date{}, err
	}
	return dbimp.ParseDate(s)
}

// trimFraction cuts the fraction of a second of a time to nine digits. A time
// has up to twelve digits of fraction, which are picoseconds, and the Go
// types hold nanoseconds. The digits beyond the ninth must be zero, or the
// value cannot be held, and the function returns an error for it (D175).
func trimFraction(s string) (string, error) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return s, nil
	}
	dot := strings.IndexByte(s[colon:], '.')
	if dot < 0 {
		return s, nil
	}
	start := colon + dot + 1
	end := start
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end-start <= 9 {
		return s, nil
	}
	if strings.Trim(s[start+9:end], "0") != "" {
		return "", fmt.Errorf("reading %q: the fraction has a digit beyond the nanoseconds, which the Go type cannot hold: %w", s, dbimp.ErrInvalidValue)
	}
	return s[:start+9] + s[end:], nil
}

// decodeTime returns a time, a time with a zone, a timestamp or a timestamp
// with a zone, as the type s names. Trino writes the zone of a time with no
// space, and Presto writes it after a space (measured).
func decodeTime(raw string, v jsontext.Value) (any, error) {
	text, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	s, err := trimFraction(text)
	if err != nil {
		return nil, err
	}
	switch raw {
	case typeTime:
		return dbimp.ParseLocalTime(s)
	case typeTimeTZ:
		// Presto writes an offset of zero as UTC, with no space before it
		// (measured).
		if utc, ok := strings.CutSuffix(strings.TrimSuffix(s, " UTC"), "UTC"); ok || strings.HasSuffix(s, " UTC") {
			return dbimp.ParseOffsetTime(strings.TrimSpace(utc) + "Z")
		}
		return dbimp.ParseOffsetTime(strings.Replace(s, " ", "", 1))
	case typeTimestamp:
		return dbimp.ParseLocalDateTime(s)
	}
	stamp, zone, ok := strings.CutLast(s, " ")
	if !ok {
		return nil, fmt.Errorf("reading %q as a timestamp with a time zone: no zone: %w", text, dbimp.ErrInvalidValue)
	}
	ldt, err := dbimp.ParseLocalDateTime(stamp)
	if err != nil {
		return nil, err
	}
	loc, err := location(zone)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", text, err)
	}
	return ldt.In(loc), nil
}

// location returns the zone that a timestamp names: an offset such as +05:30,
// or the name of a zone such as Asia/Jakarta. A name that Go cannot load is an
// error, because the value cannot be held without it (measured).
func location(zone string) (*time.Location, error) {
	if zone != "" && (zone[0] == '+' || zone[0] == '-') {
		ot, err := dbimp.ParseOffsetTime("00:00" + zone)
		if err != nil {
			return nil, fmt.Errorf("the zone %q: %w", zone, dbimp.ErrInvalidValue)
		}
		if ot.Offset == 0 {
			return time.UTC, nil
		}
		return time.FixedZone("", ot.Offset), nil
	}
	if zone == "" {
		return nil, fmt.Errorf("the zone is empty: %w", dbimp.ErrInvalidValue)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("the zone %q: %w: %w", zone, dbimp.ErrInvalidValue, err)
	}
	return loc, nil
}

// decodeInterval returns an interval. A year to month interval is 1-2 and a
// day to second interval is 3 04:05:06.789, each with an optional sign that
// turns every part around (measured).
func decodeInterval(raw string, v jsontext.Value) (dbimp.Interval, error) {
	text, err := dbimp.String(v)
	if err != nil {
		return dbimp.Interval{}, err
	}
	bad := func() (dbimp.Interval, error) {
		return dbimp.Interval{}, fmt.Errorf("reading %q as an interval: %w", text, dbimp.ErrInvalidValue)
	}
	s, sign := text, int64(1)
	switch {
	case strings.HasPrefix(s, "-"):
		s, sign = s[1:], -1
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	if raw == typeIntervalY {
		ys, ms, ok := strings.Cut(s, "-")
		years, err1 := strconv.ParseInt(ys, 10, 32)
		months, err2 := strconv.ParseInt(ms, 10, 32)
		total := sign * (years*12 + months)
		if !ok || err1 != nil || err2 != nil || total < math.MinInt32 || total > math.MaxInt32 {
			return bad()
		}
		return dbimp.Interval{Months: int32(total)}, nil
	}
	ds, clock, ok := strings.Cut(s, " ")
	days, err := strconv.ParseInt(ds, 10, 32)
	if !ok || err != nil {
		return bad()
	}
	lt, err := dbimp.ParseLocalTime(clock)
	if err != nil {
		return bad()
	}
	nanos := ((int64(lt.Hour)*60+int64(lt.Minute))*60+int64(lt.Second))*1e9 + int64(lt.Nanosecond)
	// days was parsed as an int32 and is not negative, so the product fits.
	return dbimp.Interval{Days: int32(sign * days), Nanoseconds: sign * nanos}, nil //nolint:gosec // G115: see above.
}

// container returns the JSON value that a container holds. Trino sends an
// array, a map and a row as JSON values. Presto sends each as a string with
// the JSON text, and the same inside a container (measured), so a string where
// a container belongs is its text.
func container(v jsontext.Value) (jsontext.Value, error) {
	if v.Kind() != '"' {
		return v, nil
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	val := jsontext.Value(s)
	if !val.IsValid() {
		return nil, fmt.Errorf("reading %q as JSON: %w", s, dbimp.ErrInvalidValue)
	}
	return val, nil
}

// elem returns the type of the element at i, or an error when the column
// does not name it.
func (s *signature) elem(i int) (*signature, error) {
	if i >= len(s.elems) {
		return nil, fmt.Errorf("reading a %s: the type has no argument %d: %w", s.raw, i, dbimp.ErrInvalidValue)
	}
	return s.elems[i], nil
}

// decodeArray returns an array as a []any, with each element decoded by the
// type of the elements.
func (s *signature) decodeArray(v jsontext.Value) ([]any, error) {
	et, err := s.elem(0)
	if err != nil {
		return nil, err
	}
	v, err = container(v)
	if err != nil {
		return nil, err
	}
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if err := expectKind(dec, '['); err != nil {
		return nil, fmt.Errorf("reading an array: %w", err)
	}
	out := []any{}
	for dec.PeekKind() != ']' {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading an array: %w", err)
		}
		e, err := et.decode(ev)
		if err != nil {
			return nil, fmt.Errorf("reading an element of an array: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}

// decodeMap returns a map as a map[string]any. The keys stay the strings of
// the JSON object, even for an integer or a date (measured).
func (s *signature) decodeMap(v jsontext.Value) (map[string]any, error) {
	vt, err := s.elem(1)
	if err != nil {
		return nil, err
	}
	v, err = container(v)
	if err != nil {
		return nil, err
	}
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if err := expectKind(dec, '{'); err != nil {
		return nil, fmt.Errorf("reading a map: %w", err)
	}
	out := map[string]any{}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading a key of a map: %w", err)
		}
		// The token is void after the next call, so the key is copied first.
		key := tok.String()
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a value of a map: %w", err)
		}
		e, err := vt.decode(ev)
		if err != nil {
			return nil, fmt.Errorf("reading the value of the key %q of a map: %w", key, err)
		}
		out[key] = e
	}
	return out, nil
}

// decodeRow returns a row as a []any, in the order of its fields, with each
// value decoded by the type of its field.
func (s *signature) decodeRow(v jsontext.Value) ([]any, error) {
	v, err := container(v)
	if err != nil {
		return nil, err
	}
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if err := expectKind(dec, '['); err != nil {
		return nil, fmt.Errorf("reading a row: %w", err)
	}
	out := []any{}
	for i := 0; dec.PeekKind() != ']'; i++ {
		ev, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading a field of a row: %w", err)
		}
		ft, err := s.elem(i)
		if err != nil {
			return nil, err
		}
		f, err := ft.decode(ev)
		if err != nil {
			return nil, fmt.Errorf("reading the field %d of a row: %w", i, err)
		}
		out = append(out, f)
	}
	if len(out) != len(s.elems) {
		return nil, fmt.Errorf("reading a row: %d values for %d fields: %w", len(out), len(s.elems), dbimp.ErrColumnCount)
	}
	return out, nil
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

// The scan types of the columns.
var (
	typeOfBool     = reflect.TypeFor[bool]()
	typeOfInt64    = reflect.TypeFor[int64]()
	typeOfFloat64  = reflect.TypeFor[float64]()
	typeOfDecimal  = reflect.TypeFor[*apd.Decimal]()
	typeOfString   = reflect.TypeFor[string]()
	typeOfBytes    = reflect.TypeFor[[]byte]()
	typeOfDate     = reflect.TypeFor[dbimp.Date]()
	typeOfTime     = reflect.TypeFor[dbimp.LocalTime]()
	typeOfTimeTZ   = reflect.TypeFor[dbimp.OffsetTime]()
	typeOfDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfInstant  = reflect.TypeFor[time.Time]()
	typeOfInterval = reflect.TypeFor[dbimp.Interval]()
	typeOfList     = reflect.TypeFor[[]any]()
	typeOfMap      = reflect.TypeFor[map[string]any]()
	typeOfUUID     = reflect.TypeFor[uuid.UUID]()
	typeOfAny      = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the type s (D135 and D175).
func (s *signature) scanType() reflect.Type {
	switch s.raw {
	case typeBoolean:
		return typeOfBool
	case typeTinyint, typeSmallint, typeInteger, typeBigint:
		return typeOfInt64
	case typeReal, typeDouble:
		return typeOfFloat64
	case typeDecimal, typeNumber:
		return typeOfDecimal
	case typeChar, typeVarchar, typeIPAddress, typeGeometry, typeSphere:
		return typeOfString
	case typeDate:
		return typeOfDate
	case typeTime:
		return typeOfTime
	case typeTimeTZ:
		return typeOfTimeTZ
	case typeTimestamp:
		return typeOfDateTime
	case typeTimestTZ:
		return typeOfInstant
	case typeIntervalY, typeIntervalD:
		return typeOfInterval
	case typeArray, typeRow:
		return typeOfList
	case typeMap:
		return typeOfMap
	case typeUUID:
		return typeOfUUID
	}
	if isBinary(s.raw) {
		return typeOfBytes
	}
	return typeOfAny
}

// databaseType returns the name of the type in upper case, such as BIGINT or
// TIMESTAMP WITH TIME ZONE.
func (s *signature) databaseType() string {
	return strings.ToUpper(s.raw)
}
