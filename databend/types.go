package databend

import (
	"encoding/base64"
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

// The kinds of a type, as schema names them, in lower case. A kind that the
// driver does not know reads as its text.
const (
	kindBoolean   = "boolean"
	kindString    = "string"
	kindBinary    = "binary"
	kindFloat32   = "float32"
	kindFloat64   = "float64"
	kindDecimal   = "decimal"
	kindDate      = "date"
	kindTimestamp = "timestamp"
	kindTimeTZ    = "timestamp_tz"
	kindInterval  = "interval"
	kindVariant   = "variant"
	kindBitmap    = "bitmap"
	kindVector    = "vector"
	kindGeometry  = "geometry"
	kindGeography = "geography"
	kindArray     = "array"
	kindMap       = "map"
	kindTuple     = "tuple"
	kindNull      = "null"
	kindNothing   = "nothing"
)

// colType is the type of a column or of an element, parsed from a name of
// schema such as Nullable(Array(Int32 NULL)) (D118).
type colType struct {
	// name is the name as schema writes it, and kind the name of its
	// outer type in lower case, such as array or uint64.
	name string
	kind string
	// nullable is true for Nullable(T) and for T NULL.
	nullable bool
	// elems are the types of the elements: one for an array, the key and
	// the value for a map, and one for each field of a tuple.
	elems []*colType
	// args are the numbers in the parentheses, such as the precision and
	// the scale of a decimal, or the dimension of a vector.
	args []int
}

// parseType parses a type name of schema.
func parseType(name string) (*colType, error) {
	p := &typeParser{s: name}
	t, err := p.parse()
	if err != nil {
		return nil, fmt.Errorf("reading the type %q: %w", name, err)
	}
	if p.skip(); p.i != len(p.s) {
		return nil, fmt.Errorf("reading the type %q: text after the type: %w", name, dbimp.ErrInvalidValue)
	}
	return t, nil
}

// typeParser reads a type name, one part at a time.
type typeParser struct {
	s string
	i int
}

func (p *typeParser) skip() {
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
}

// word reads a word of letters, digits and underscores.
func (p *typeParser) word() string {
	p.skip()
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c != '_' && (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			break
		}
		p.i++
	}
	return p.s[start:p.i]
}

func (p *typeParser) accept(c byte) bool {
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}

func (p *typeParser) parse() (*colType, error) {
	p.skip()
	start := p.i
	w := p.word()
	if w == "" {
		return nil, fmt.Errorf("no type at %d: %w", p.i, dbimp.ErrInvalidValue)
	}
	kind := strings.ToLower(w)
	if kind == "nullable" {
		if !p.accept('(') {
			return nil, fmt.Errorf("no ( after Nullable: %w", dbimp.ErrInvalidValue)
		}
		t, err := p.parse()
		if err != nil {
			return nil, err
		}
		if !p.accept(')') {
			return nil, fmt.Errorf("no ) after Nullable: %w", dbimp.ErrInvalidValue)
		}
		// The name stays that of the inner type, which is the database type
		// of the column.
		t.nullable = true
		return t, nil
	}
	if kind == "timestamptz" {
		kind = kindTimeTZ
	}
	t := &colType{kind: kind}
	if p.accept('(') {
		for {
			p.skip()
			if p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
				n, err := strconv.Atoi(p.word())
				if err != nil {
					return nil, fmt.Errorf("reading a number: %w", dbimp.ErrInvalidValue)
				}
				t.args = append(t.args, n)
			} else {
				// A field of a tuple can have a name, as in
				// Tuple(a Int32, b String). A word is a name when a type
				// follows it, and NULL is no type.
				save := p.i
				if kind != kindTuple || p.word() == "" || !p.typeFollows() {
					p.i = save
				}
				e, err := p.parse()
				if err != nil {
					return nil, err
				}
				t.elems = append(t.elems, e)
			}
			if p.accept(')') {
				break
			}
			if !p.accept(',') {
				return nil, fmt.Errorf("no , or ) in the arguments: %w", dbimp.ErrInvalidValue)
			}
		}
	}
	p.skip()
	if strings.HasPrefix(strings.ToUpper(p.s[p.i:]), "NULL") && (p.i+4 == len(p.s) || strings.ContainsRune(",) ", rune(p.s[p.i+4]))) {
		p.i += 4
		t.nullable = true
	}
	t.name = strings.TrimSpace(p.s[start:p.i])
	return t, nil
}

// typeFollows reports whether the name of a type follows, as it does after
// the name of a field of a tuple. NULL is no type.
func (p *typeParser) typeFollows() bool {
	save := p.i
	defer func() { p.i = save }()
	w := p.word()
	return w != "" && !strings.EqualFold(w, "NULL")
}

// format holds the settings of the session that say how the server writes a
// value (D118).
type format struct {
	// driver is true for http_json_result_mode=driver.
	driver bool
	// binary is the value of binary_output_format, in lower case.
	binary string
	// loc is the timezone of a Timestamp.
	loc *time.Location
}

// decode returns the Go value of the text of a value of type t, which arrived
// as a JSON string.
func (f *format) decode(t *colType, text string) (any, error) {
	switch t.kind {
	case kindArray, kindMap, kindTuple, kindVector:
		s := &valueScanner{s: text}
		v, err := f.nested(s, t)
		if err != nil {
			return nil, err
		}
		if s.skip(); s.i != len(s.s) {
			return nil, fmt.Errorf("reading a %s: text after the value: %w", t.name, dbimp.ErrInvalidValue)
		}
		return v, nil
	case kindVariant:
		return dbimp.Any(jsontext.Value(text))
	}
	return f.scalar(t, text)
}

// scalar returns the Go value of the text of a value of a type that holds no
// elements.
func (f *format) scalar(t *colType, text string) (any, error) {
	switch t.kind {
	case "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32":
		i, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, valueError(t, text)
		}
		return i, nil
	case "uint64":
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i, nil
		}
		if _, err := strconv.ParseUint(text, 10, 64); err != nil {
			return nil, valueError(t, text)
		}
		d, _, err := apd.NewFromString(text)
		if err != nil {
			return nil, valueError(t, text)
		}
		return d, nil
	case kindFloat32, kindFloat64:
		// The server writes the shortest text of the value, and the driver
		// reads it as the float64 of that text, so a Float32 reads as the
		// caller wrote it.
		v, err := strconv.ParseFloat(text, 64)
		if err != nil && !math.IsInf(v, 0) {
			return nil, valueError(t, text)
		}
		return v, nil
	case kindDecimal:
		d, _, err := apd.NewFromString(text)
		if err != nil {
			return nil, valueError(t, text)
		}
		return d, nil
	case kindBoolean:
		switch text {
		case "1", "true":
			return true, nil
		case "0", "false":
			return false, nil
		}
		return nil, valueError(t, text)
	case kindBinary:
		return f.bytes(t, text)
	case kindDate:
		return f.date(t, text)
	case kindTimestamp:
		return f.timestamp(t, text)
	case kindTimeTZ:
		return f.timestampTZ(t, text)
	case kindBitmap:
		// JSON carries no bytes of a bitmap, whatever the settings say
		// (D119).
		return nil, fmt.Errorf("reading a %s: the server writes no bytes of a Bitmap in JSON: %w", t.name, dbimp.ErrNotSupported)
	}
	// A String, an Interval, a Geometry, a Geography and a kind that the
	// driver does not know read as their text.
	return text, nil
}

// bytes decodes a Binary as binary_output_format writes it.
func (f *format) bytes(t *colType, text string) ([]byte, error) {
	switch f.binary {
	case "", "hex":
		b, err := hex.DecodeString(text)
		if err != nil {
			return nil, valueError(t, text)
		}
		return b, nil
	case "base64":
		b, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, valueError(t, text)
		}
		return b, nil
	}
	return []byte(text), nil
}

// date decodes a Date: the text 2026-09-29, or the days since 1970 in the
// driver mode.
func (f *format) date(t *colType, text string) (time.Time, error) {
	if f.driver {
		days, err := strconv.Atoi(text)
		if err != nil {
			return time.Time{}, valueError(t, text)
		}
		return time.Unix(0, 0).UTC().AddDate(0, 0, days), nil
	}
	v, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return time.Time{}, valueError(t, text)
	}
	return v, nil
}

// timestamp decodes a Timestamp: the text 2026-09-29 12:34:56.123456 in the
// timezone of the session, or the microseconds since 1970 in the driver
// mode.
func (f *format) timestamp(t *colType, text string) (time.Time, error) {
	loc := f.loc
	if loc == nil {
		loc = time.UTC
	}
	if f.driver {
		us, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return time.Time{}, valueError(t, text)
		}
		return time.UnixMicro(us).In(loc), nil
	}
	v, err := time.ParseInLocation("2006-01-02 15:04:05.999999", text, loc)
	if err != nil {
		return time.Time{}, valueError(t, text)
	}
	return v, nil
}

// timestampTZ decodes a Timestamp_Tz: the text 2026-09-29 12:34:56.123456
// +0530, or the microseconds and the offset in seconds in the driver mode.
func (f *format) timestampTZ(t *colType, text string) (time.Time, error) {
	if f.driver {
		us, off, ok := strings.Cut(text, " ")
		u, err1 := strconv.ParseInt(us, 10, 64)
		o, err2 := strconv.Atoi(off)
		if !ok || err1 != nil || err2 != nil {
			return time.Time{}, valueError(t, text)
		}
		return time.UnixMicro(u).In(time.FixedZone("", o)), nil
	}
	v, err := time.Parse("2006-01-02 15:04:05.999999 -0700", text)
	if err != nil {
		return time.Time{}, valueError(t, text)
	}
	return v, nil
}

// nested reads the value of an Array, a Map, a Tuple or a Vector, which the
// server writes as text of SQL, such as [1,NULL,3], {"a":1} and (1,"x")
// (D119). s is at the start of the value.
func (f *format) nested(s *valueScanner, t *colType) (any, error) {
	if s.null() {
		return nil, nil
	}
	switch t.kind {
	case kindArray, kindVector:
		var elem *colType
		switch {
		case t.kind == kindVector:
			elem = &colType{name: "Float32", kind: kindFloat32}
		case len(t.elems) == 1:
			elem = t.elems[0]
		default:
			elem = &colType{name: "Nothing", kind: kindNothing}
		}
		var out []any
		var vec []float32
		err := s.list('[', ']', func() error {
			v, err := f.nested(s, elem)
			if err != nil {
				return err
			}
			if t.kind == kindVector {
				x, ok := v.(float64)
				if !ok {
					return fmt.Errorf("reading a %s: an element that is not a number: %w", t.name, dbimp.ErrInvalidValue)
				}
				vec = append(vec, float32(x))
				return nil
			}
			out = append(out, v)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if t.kind == kindVector {
			if vec == nil {
				vec = []float32{}
			}
			return vec, nil
		}
		if out == nil {
			out = []any{}
		}
		return out, nil
	case kindTuple:
		out := []any{}
		i := 0
		err := s.list('(', ')', func() error {
			if i >= len(t.elems) {
				return fmt.Errorf("reading a %s: more fields than its type: %w", t.name, dbimp.ErrInvalidValue)
			}
			v, err := f.nested(s, t.elems[i])
			if err != nil {
				return err
			}
			out = append(out, v)
			i++
			return nil
		})
		return out, err
	case kindMap:
		if len(t.elems) < 2 {
			// Map(Nothing) is the type of an empty map.
			err := s.list('{', '}', func() error {
				return fmt.Errorf("reading a %s: an entry in an empty map: %w", t.name, dbimp.ErrInvalidValue)
			})
			return map[string]any{}, err
		}
		strKeys := t.elems[0].kind == kindString
		ms := map[string]any{}
		ma := map[any]any{}
		err := s.list('{', '}', func() error {
			k, err := f.nested(s, t.elems[0])
			if err != nil {
				return err
			}
			if !s.accept(':') {
				return fmt.Errorf("reading a %s: no : after a key: %w", t.name, dbimp.ErrInvalidValue)
			}
			v, err := f.nested(s, t.elems[1])
			if err != nil {
				return err
			}
			if strKeys {
				ks, _ := k.(string)
				ms[ks] = v
			} else {
				ma[k] = v
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if strKeys {
			return ms, nil
		}
		return ma, nil
	case kindVariant, kindGeometry, kindGeography:
		if s.peek() == '"' {
			str, err := s.quoted()
			if err != nil {
				return nil, err
			}
			if t.kind == kindVariant {
				return dbimp.Any(jsontext.Value(str))
			}
			return str, nil
		}
		raw, err := s.json()
		if err != nil {
			return nil, err
		}
		if t.kind == kindVariant {
			return dbimp.Any(jsontext.Value(raw))
		}
		return raw, nil
	case kindNull, kindNothing:
		return nil, fmt.Errorf("reading a %s: a value that is not NULL: %w", t.name, dbimp.ErrInvalidValue)
	}
	var text string
	if s.peek() == '"' {
		str, err := s.quoted()
		if err != nil {
			return nil, err
		}
		text = str
	} else {
		text = s.bare()
	}
	return f.scalar(t, text)
}

// valueError returns the error of a text that is not a value of t.
func valueError(t *colType, text string) error {
	if len(text) > 40 {
		text = text[:40] + "..."
	}
	return fmt.Errorf("reading %q as a %s: %w", text, t.name, dbimp.ErrInvalidValue)
}

// valueScanner reads the text of a nested value.
type valueScanner struct {
	s string
	i int
}

func (s *valueScanner) skip() {
	for s.i < len(s.s) && (s.s[s.i] == ' ' || s.s[s.i] == '\n' || s.s[s.i] == '\t') {
		s.i++
	}
}

func (s *valueScanner) peek() byte {
	s.skip()
	if s.i == len(s.s) {
		return 0
	}
	return s.s[s.i]
}

func (s *valueScanner) accept(c byte) bool {
	if s.peek() == c {
		s.i++
		return true
	}
	return false
}

// null reads a bare NULL, if one is next.
func (s *valueScanner) null() bool {
	s.skip()
	if !strings.HasPrefix(s.s[s.i:], "NULL") {
		return false
	}
	if end := s.i + 4; end < len(s.s) && !strings.ContainsRune(",]):} ", rune(s.s[end])) {
		return false
	}
	s.i += 4
	return true
}

// list reads a list between open and end, and calls elem for each element,
// which reads it.
func (s *valueScanner) list(open, end byte, elem func() error) error {
	if !s.accept(open) {
		return fmt.Errorf("reading a nested value: no %q at %d: %w", open, s.i, dbimp.ErrInvalidValue)
	}
	if s.accept(end) {
		return nil
	}
	for {
		if err := elem(); err != nil {
			return err
		}
		if s.accept(end) {
			return nil
		}
		if !s.accept(',') {
			return fmt.Errorf("reading a nested value: no , or %q at %d: %w", end, s.i, dbimp.ErrInvalidValue)
		}
	}
}

// quoted reads a string in double quotes. The server escapes a quote with a
// backslash, and writes every other character as it is, a backslash too
// (measured), so a string that ends with a backslash reads wrong
// (docs/DATABEND.md).
func (s *valueScanner) quoted() (string, error) {
	if !s.accept('"') {
		return "", fmt.Errorf("reading a string: no quote at %d: %w", s.i, dbimp.ErrInvalidValue)
	}
	var b strings.Builder
	for s.i < len(s.s) {
		c := s.s[s.i]
		switch {
		case c == '\\' && s.i+1 < len(s.s) && s.s[s.i+1] == '"':
			b.WriteByte('"')
			s.i += 2
		case c == '"':
			s.i++
			return b.String(), nil
		default:
			b.WriteByte(c)
			s.i++
		}
	}
	return "", fmt.Errorf("reading a string: no end quote: %w", dbimp.ErrInvalidValue)
}

// bare reads a value with no quotes, such as a number or the hex of a
// Binary, up to the next delimiter.
func (s *valueScanner) bare() string {
	s.skip()
	start := s.i
	for s.i < len(s.s) && !strings.ContainsRune(",]):}", rune(s.s[s.i])) {
		s.i++
	}
	return strings.TrimSpace(s.s[start:s.i])
}

// json reads one JSON value with no quotes around it, such as a Variant or
// the GeoJSON of a Geometry, inside a nested value.
func (s *valueScanner) json() (string, error) {
	s.skip()
	dec := jsontext.NewDecoder(strings.NewReader(s.s[s.i:]))
	v, err := dec.ReadValue()
	if err != nil {
		return "", fmt.Errorf("reading a JSON value in a nested value: %w", err)
	}
	s.i += int(dec.InputOffset())
	return string(v), nil
}

// scanTypes are the Go types of the kinds, which ColumnTypeScanType returns
// (D118). A UInt64 is an int64 or an *apd.Decimal, and a kind that the driver
// does not know is a string.
var scanTypes = map[string]reflect.Type{
	"int8":        reflect.TypeFor[int64](),
	"int16":       reflect.TypeFor[int64](),
	"int32":       reflect.TypeFor[int64](),
	"int64":       reflect.TypeFor[int64](),
	"uint8":       reflect.TypeFor[int64](),
	"uint16":      reflect.TypeFor[int64](),
	"uint32":      reflect.TypeFor[int64](),
	"uint64":      reflect.TypeFor[any](),
	kindFloat32:   reflect.TypeFor[float64](),
	kindFloat64:   reflect.TypeFor[float64](),
	kindDecimal:   reflect.TypeFor[*apd.Decimal](),
	kindBoolean:   reflect.TypeFor[bool](),
	kindString:    reflect.TypeFor[string](),
	kindBinary:    reflect.TypeFor[[]byte](),
	kindDate:      reflect.TypeFor[time.Time](),
	kindTimestamp: reflect.TypeFor[time.Time](),
	kindTimeTZ:    reflect.TypeFor[time.Time](),
	kindInterval:  reflect.TypeFor[string](),
	kindVariant:   reflect.TypeFor[any](),
	kindBitmap:    reflect.TypeFor[any](),
	kindVector:    reflect.TypeFor[[]float32](),
	kindGeometry:  reflect.TypeFor[string](),
	kindGeography: reflect.TypeFor[string](),
	kindArray:     reflect.TypeFor[[]any](),
	kindTuple:     reflect.TypeFor[[]any](),
	kindNull:      reflect.TypeFor[any](),
	kindNothing:   reflect.TypeFor[any](),
}

// scanType returns the Go type of a value of t.
func (t *colType) scanType() reflect.Type {
	if t.kind == kindMap {
		if len(t.elems) == 2 && t.elems[0].kind != kindString {
			return reflect.TypeFor[map[any]any]()
		}
		return reflect.TypeFor[map[string]any]()
	}
	if st, ok := scanTypes[t.kind]; ok {
		return st
	}
	return reflect.TypeFor[string]()
}
