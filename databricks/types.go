package databricks

import (
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

// kind is the family of a type, which gives its Go type (D135 and D193).
type kind uint8

const (
	kindString kind = iota
	kindInteger
	kindFloat
	kindDecimal
	kindBoolean
	kindBinary
	kindDate
	kindTimestamp
	kindLocalTimestamp
	kindYearMonth
	kindDaySecond
	kindArray
	kindMap
	kindStruct
	kindVariant
	kindVoid
)

// typ is a type that the driver reads from type_text of the manifest, such
// as ARRAY<STRUCT<a: MAP<STRING, ARRAY<INT>> NOT NULL>> (measured). The text
// of a value holds its leaves as text, so the driver needs the whole type to
// decode them.
type typ struct {
	kind kind
	// name is the name of the type in upper case, such as DECIMAL, with no
	// parameters.
	name string
	// precision and scale are those of a DECIMAL, and -1 when the type has none.
	precision, scale int
	// elem is the type of the elements of an ARRAY and the values of a MAP.
	elem *typ
	// fields are the fields of a STRUCT, in the order of the type.
	fields []field
}

// field is one field of a STRUCT.
type field struct {
	name string
	typ  *typ
}

// The names of the types that the manifest gives in type_text (measured). The
// names type_name gives for four of them differ: BYTE, SHORT, LONG and NULL
// stand for TINYINT, SMALLINT, BIGINT and VOID.
const (
	nameTinyint      = "TINYINT"
	nameSmallint     = "SMALLINT"
	nameInt          = "INT"
	nameBigint       = "BIGINT"
	nameFloat        = "FLOAT"
	nameDouble       = "DOUBLE"
	nameDecimal      = "DECIMAL"
	nameBoolean      = "BOOLEAN"
	nameString       = "STRING"
	nameBinary       = "BINARY"
	nameDate         = "DATE"
	nameTimestamp    = "TIMESTAMP"
	nameTimestampNTZ = "TIMESTAMP_NTZ"
	nameInterval     = "INTERVAL"
	nameArray        = "ARRAY"
	nameMap          = "MAP"
	nameStruct       = "STRUCT"
	nameVariant      = "VARIANT"
	nameVoid         = "VOID"
)

// column is one column of the manifest.
type column struct {
	name string
	typ  *typ
}

// manifestColumn is one member of schema.columns in the manifest (measured).
type manifestColumn struct {
	Name      string `json:"name"`
	TypeName  string `json:"type_name"`
	TypeText  string `json:"type_text"`
	Precision *int   `json:"type_precision"`
	Scale     *int   `json:"type_scale"`
}

// newColumn returns the column of an entry of the manifest.
func newColumn(m manifestColumn) (column, error) {
	text := m.TypeText
	if text == "" {
		text = typeNameText(m.TypeName)
	}
	t, err := parseType(text)
	if err != nil {
		return column{}, fmt.Errorf("reading the type %q of the column %q: %w", text, m.Name, err)
	}
	if t.kind == kindDecimal && t.precision < 0 && m.Precision != nil && m.Scale != nil {
		t.precision, t.scale = *m.Precision, *m.Scale
	}
	return column{name: m.Name, typ: t}, nil
}

// typeNameText returns the type of type_name for a column whose manifest has no
// type_text.
func typeNameText(name string) string {
	switch strings.ToUpper(name) {
	case "BYTE":
		return nameTinyint
	case "SHORT":
		return nameSmallint
	case "LONG":
		return nameBigint
	case "NULL":
		return nameVoid
	}
	return name
}

// databaseType returns the name of the type in upper case.
func (c column) databaseType() string {
	return c.typ.name
}

// typeParser reads the text of a type.
type typeParser struct {
	s   string
	pos int
}

// parseType reads type_text, the type as Spark writes it (measured). A type
// that the driver does not know by name is a string, so a value of it is the
// text that the server sent. A type of a list, a map or a struct that does not
// parse is an error.
func parseType(text string) (*typ, error) {
	p := &typeParser{s: text}
	t, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.s) {
		return nil, fmt.Errorf("reading the type at %d: text after the type: %w", p.pos, dbimp.ErrInvalidValue)
	}
	return t, nil
}

func (p *typeParser) skipSpace() {
	for p.pos < len(p.s) && p.s[p.pos] == ' ' {
		p.pos++
	}
}

// peek returns the byte at the position, and 0 at the end.
func (p *typeParser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

// word reads a run of letters, digits and underscores.
func (p *typeParser) word() string {
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			p.pos++
			continue
		}
		break
	}
	return p.s[start:p.pos]
}

func (p *typeParser) expect(c byte) error {
	p.skipSpace()
	if p.peek() != c {
		return fmt.Errorf("reading the type at %d: want %q: %w", p.pos, c, dbimp.ErrInvalidValue)
	}
	p.pos++
	return nil
}

// parseType reads one type, with the words that follow it up to the next
// delimiter of an enclosing type: NOT NULL, COLLATE <name> and the like.
func (p *typeParser) parseType() (*typ, error) {
	p.skipSpace()
	w := p.word()
	if w == "" {
		return nil, fmt.Errorf("reading the type at %d: no name: %w", p.pos, dbimp.ErrInvalidValue)
	}
	t := &typ{name: strings.ToUpper(w), precision: -1, scale: -1}
	switch t.name {
	case nameArray:
		t.kind = kindArray
		if err := p.expect('<'); err != nil {
			return nil, err
		}
		elem, err := p.parseType()
		if err != nil {
			return nil, err
		}
		t.elem = elem
		if err := p.expect('>'); err != nil {
			return nil, err
		}
	case nameMap:
		t.kind = kindMap
		if err := p.expect('<'); err != nil {
			return nil, err
		}
		if _, err := p.parseType(); err != nil {
			return nil, err
		}
		if err := p.expect(','); err != nil {
			return nil, err
		}
		elem, err := p.parseType()
		if err != nil {
			return nil, err
		}
		t.elem = elem
		if err := p.expect('>'); err != nil {
			return nil, err
		}
	case nameStruct:
		t.kind = kindStruct
		if err := p.parseFields(t); err != nil {
			return nil, err
		}
	case nameInterval:
		t.kind = kindYearMonth
		var units []string
		for {
			p.skipSpace()
			u := strings.ToUpper(p.word())
			if u == "" {
				break
			}
			units = append(units, u)
		}
		if len(units) > 0 && units[0] != "YEAR" && units[0] != "MONTH" {
			t.kind = kindDaySecond
		}
	default:
		t.kind = scalarKind(t.name)
		p.skipSpace()
		if p.peek() == '(' {
			if err := p.parseParameters(t); err != nil {
				return nil, err
			}
		}
	}
	p.skipModifiers()
	return t, nil
}

// scalarKind returns the kind of the scalar type name.
func scalarKind(name string) kind {
	switch name {
	case nameTinyint, nameSmallint, nameInt, nameBigint:
		return kindInteger
	case nameFloat, nameDouble:
		return kindFloat
	case nameDecimal:
		return kindDecimal
	case nameBoolean:
		return kindBoolean
	case nameBinary:
		return kindBinary
	case nameDate:
		return kindDate
	case nameTimestamp:
		return kindTimestamp
	case nameTimestampNTZ:
		return kindLocalTimestamp
	case nameVariant:
		return kindVariant
	case nameVoid:
		return kindVoid
	}
	// STRING, CHAR, VARCHAR, GEOMETRY, GEOGRAPHY and any type that the driver
	// does not know.
	return kindString
}

// parseParameters reads the numbers in parentheses after a type, such as
// (10,2) or (4326).
func (p *typeParser) parseParameters(t *typ) error {
	p.pos++
	var nums []int
	for {
		p.skipSpace()
		start := p.pos
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
		if start == p.pos {
			return fmt.Errorf("reading the type at %d: want a number: %w", p.pos, dbimp.ErrInvalidValue)
		}
		n, err := strconv.Atoi(p.s[start:p.pos])
		if err != nil {
			return fmt.Errorf("reading the type at %d: %w", start, dbimp.ErrInvalidValue)
		}
		nums = append(nums, n)
		p.skipSpace()
		switch p.peek() {
		case ',':
			p.pos++
		case ')':
			p.pos++
			if t.kind == kindDecimal && len(nums) == 2 {
				t.precision, t.scale = nums[0], nums[1]
			}
			return nil
		default:
			return fmt.Errorf("reading the type at %d: %w", p.pos, dbimp.ErrInvalidValue)
		}
	}
}

// parseFields reads <name: type, name: type> after STRUCT.
func (p *typeParser) parseFields(t *typ) error {
	if err := p.expect('<'); err != nil {
		return err
	}
	p.skipSpace()
	if p.peek() == '>' {
		p.pos++
		return nil
	}
	for {
		name, err := p.fieldName()
		if err != nil {
			return err
		}
		if err := p.expect(':'); err != nil {
			return err
		}
		ft, err := p.parseType()
		if err != nil {
			return err
		}
		t.fields = append(t.fields, field{name: name, typ: ft})
		p.skipSpace()
		switch p.peek() {
		case ',':
			p.pos++
		case '>':
			p.pos++
			return nil
		default:
			return fmt.Errorf("reading the type at %d: want , or >: %w", p.pos, dbimp.ErrInvalidValue)
		}
	}
}

// fieldName reads the name of a field, which is a word or a name in backquotes.
func (p *typeParser) fieldName() (string, error) {
	p.skipSpace()
	if p.peek() != '`' {
		if w := p.word(); w != "" {
			return w, nil
		}
		return "", fmt.Errorf("reading the type at %d: no name of a field: %w", p.pos, dbimp.ErrInvalidValue)
	}
	p.pos++
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++
		if c == '`' {
			if p.peek() == '`' {
				b.WriteByte('`')
				p.pos++
				continue
			}
			return b.String(), nil
		}
		b.WriteByte(c)
	}
	return "", fmt.Errorf("reading the type: a name in backquotes does not end: %w", dbimp.ErrInvalidValue)
}

// skipModifiers skips the words that follow a type and that the driver does
// not read: NOT NULL, COLLATE <name> and COMMENT '<text>'.
func (p *typeParser) skipModifiers() {
	for {
		p.skipSpace()
		switch c := p.peek(); {
		case c == '\'':
			p.pos++
			for p.pos < len(p.s) {
				q := p.s[p.pos]
				p.pos++
				if q == '\'' {
					if p.peek() == '\'' {
						p.pos++
						continue
					}
					break
				}
			}
		case c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			p.word()
		default:
			return
		}
	}
}

// decode returns the value of a column as its Go value (D135 and D193). A
// value of a row is a JSON string, or null (measured).
func decode(t *typ, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, fmt.Errorf("reading the value of a %s column: %w", t.name, err)
	}
	switch t.kind {
	case kindArray, kindMap, kindStruct:
		return decodeNested(t, s)
	case kindVariant:
		return parseVariant(s)
	}
	return decodeScalar(t, s)
}

// decodeScalar reads the text s of a value of the scalar type t.
func decodeScalar(t *typ, s string) (any, error) {
	switch t.kind {
	case kindInteger:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("reading %q as an integer: %w", s, dbimp.ErrInvalidValue)
		}
		return i, nil
	case kindFloat:
		// ParseFloat takes NaN, Infinity and -Infinity, which the server writes
		// (measured). The text of a FLOAT is the shortest text of a 32 bit
		// value, and the driver reads the float64 nearest to it (D193 notes).
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a float: %w", s, dbimp.ErrInvalidValue)
		}
		return f, nil
	case kindDecimal:
		d, _, err := apd.NewFromString(s)
		if err != nil || d.Form != apd.Finite {
			return nil, fmt.Errorf("reading %q as a decimal: %w", s, dbimp.ErrInvalidValue)
		}
		return d, nil
	case kindBoolean:
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("reading %q as a boolean: %w", s, dbimp.ErrInvalidValue)
	case kindBinary:
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as base64: %w", s, dbimp.ErrInvalidValue)
		}
		return b, nil
	case kindDate:
		d, err := dbimp.ParseDate(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a date: %w", s, dbimp.ErrInvalidValue)
		}
		return d, nil
	case kindTimestamp:
		// The text has three digits of fraction and the letter Z (measured).
		ts, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a timestamp: %w", s, dbimp.ErrInvalidValue)
		}
		return ts.UTC(), nil
	case kindLocalTimestamp:
		ts, err := dbimp.ParseLocalDateTime(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a timestamp with no zone: %w", s, dbimp.ErrInvalidValue)
		}
		return ts, nil
	case kindYearMonth, kindDaySecond:
		return parseInterval(s)
	case kindVoid:
		return nil, nil
	}
	return s, nil
}

// decodeNested reads the text of an ARRAY, a MAP or a STRUCT, which is JSON
// with every leaf as text, and converts each leaf by its type.
func decodeNested(t *typ, s string) (any, error) {
	v, err := dbimp.Any(jsontext.Value(s))
	if err != nil {
		return nil, fmt.Errorf("reading the value of a %s column as JSON: %w", t.name, dbimp.ErrInvalidValue)
	}
	return convert(t, v)
}

// convert returns the leaves of the JSON value v, which dbimp.Any decoded, as
// the Go values of the type t.
func convert(t *typ, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch t.kind {
	case kindArray:
		list, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("reading a value of %s: want a JSON array, got %T: %w", t.name, v, dbimp.ErrInvalidValue)
		}
		out := make([]any, len(list))
		for i, e := range list {
			var err error
			if out[i], err = convert(t.elem, e); err != nil {
				return nil, err
			}
		}
		return out, nil
	case kindMap:
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reading a value of %s: want a JSON object, got %T: %w", t.name, v, dbimp.ErrInvalidValue)
		}
		out := make(map[string]any, len(m))
		for k, e := range m {
			var err error
			if out[k], err = convert(t.elem, e); err != nil {
				return nil, err
			}
		}
		return out, nil
	case kindStruct:
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reading a value of %s: want a JSON object, got %T: %w", t.name, v, dbimp.ErrInvalidValue)
		}
		out := make(map[string]any, len(m))
		types := make(map[string]*typ, len(t.fields))
		for _, f := range t.fields {
			types[f.name] = f.typ
		}
		for k, e := range m {
			ft, ok := types[k]
			if !ok {
				return nil, fmt.Errorf("reading a value of %s: the field %q is not in the type: %w", t.name, k, dbimp.ErrInvalidValue)
			}
			var err error
			if out[k], err = convert(ft, e); err != nil {
				return nil, err
			}
		}
		return out, nil
	case kindVariant:
		return v, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("reading a value of %s: want text, got %T: %w", t.name, v, dbimp.ErrInvalidValue)
	}
	return decodeScalar(t, s)
}

// parseVariant reads the text of a VARIANT, which is JSON with real types. A
// JSON null and a SQL NULL are both nil (D193 item 7 and D18).
func parseVariant(s string) (any, error) {
	val := jsontext.Value(s)
	if !val.IsValid() {
		return nil, fmt.Errorf("reading %q as JSON: %w", s, dbimp.ErrInvalidValue)
	}
	return dbimp.Any(val)
}

// parseInterval reads the text of an interval, an SQL literal such as
// INTERVAL '1-2' YEAR TO MONTH or INTERVAL '-1 02:03:04.5' DAY TO SECOND
// (measured). A year to month interval has months, and a day to second interval
// has days and nanoseconds, where a negative interval is negative in each part
// (D193 item 6).
func parseInterval(text string) (dbimp.Interval, error) {
	bad := fmt.Errorf("reading %q as an interval: %w", text, dbimp.ErrInvalidValue)
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), "INTERVAL")
	if !ok {
		return dbimp.Interval{}, bad
	}
	rest = strings.TrimSpace(rest)
	negative := false
	// Spark can write the sign before the quote.
	if r, ok := strings.CutPrefix(rest, "-"); ok {
		negative, rest = true, strings.TrimSpace(r)
	} else if r, ok := strings.CutPrefix(rest, "+"); ok {
		rest = strings.TrimSpace(r)
	}
	rest, ok = strings.CutPrefix(rest, "'")
	if !ok {
		return dbimp.Interval{}, bad
	}
	body, fields, ok := strings.Cut(rest, "'")
	if !ok {
		return dbimp.Interval{}, bad
	}
	fields = strings.ToUpper(strings.TrimSpace(fields))
	if r, ok := strings.CutPrefix(body, "-"); ok {
		negative, body = !negative, r
	} else if r, ok := strings.CutPrefix(body, "+"); ok {
		body = r
	}
	body = strings.TrimSpace(body)
	start, _, _ := strings.Cut(fields, " ")
	var iv dbimp.Interval
	var err error
	switch start {
	case "YEAR", "MONTH":
		var months int64
		if months, err = parseMonths(body, start); err == nil {
			if negative {
				months = -months
			}
			if months < math.MinInt32 || months > math.MaxInt32 {
				return dbimp.Interval{}, bad
			}
			iv.Months = int32(months)
		}
	case "DAY", "HOUR", "MINUTE", "SECOND":
		iv.Days, iv.Nanoseconds, err = parseDayTime(body, start)
		if negative {
			iv.Days, iv.Nanoseconds = -iv.Days, -iv.Nanoseconds
		}
	default:
		return dbimp.Interval{}, bad
	}
	if err != nil {
		return dbimp.Interval{}, bad
	}
	return iv, nil
}

// parseMonths reads the body of a year to month interval: Y-M, Y or M.
// It returns the count of months without its sign, so that the smallest interval,
// whose count is 2147483648 before the sign, reads.
func parseMonths(body, start string) (int64, error) {
	if start == "MONTH" {
		return strconv.ParseInt(body, 10, 64)
	}
	y, m, hasMonths := strings.Cut(body, "-")
	years, err := strconv.ParseInt(y, 10, 32)
	if err != nil {
		return 0, err
	}
	var months int64
	if hasMonths {
		if months, err = strconv.ParseInt(m, 10, 32); err != nil {
			return 0, err
		}
	}
	return years*12 + months, nil
}

// parseDayTime reads the body of a day to second interval, whose first field
// is start: D, D HH, D HH:MM, D HH:MM:SS.f, HH, HH:MM, HH:MM:SS.f, MM, MM:SS.f
// or SS.f.
func parseDayTime(body, start string) (int32, int64, error) {
	var days int64
	var err error
	if start == "DAY" {
		d, t, hasTime := strings.Cut(body, " ")
		if days, err = strconv.ParseInt(d, 10, 32); err != nil {
			return 0, 0, err
		}
		if !hasTime {
			return int32(days), 0, nil
		}
		body = strings.TrimSpace(t)
		start = "HOUR"
	}
	units := []time.Duration{time.Hour, time.Minute, time.Second}
	switch start {
	case "MINUTE":
		units = units[1:]
	case "SECOND":
		units = units[2:]
	}
	parts := strings.Split(body, ":")
	if len(parts) > len(units) {
		return 0, 0, dbimp.ErrInvalidValue
	}
	var nanos int64
	for i, part := range parts {
		whole, frac, hasFrac := strings.Cut(part, ".")
		n, err := strconv.ParseInt(whole, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, dbimp.ErrInvalidValue
		}
		nanos += n * int64(units[i])
		if hasFrac {
			if i != len(parts)-1 || len(frac) > 9 || frac == "" || units[i] != time.Second {
				return 0, 0, dbimp.ErrInvalidValue
			}
			f, err := strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
			if err != nil {
				return 0, 0, dbimp.ErrInvalidValue
			}
			nanos += f
		}
	}
	return int32(days), nanos, nil
}

// The scan types of the columns (D135 and D193).
var (
	typeOfInt64         = reflect.TypeFor[int64]()
	typeOfFloat64       = reflect.TypeFor[float64]()
	typeOfDecimal       = reflect.TypeFor[*apd.Decimal]()
	typeOfBool          = reflect.TypeFor[bool]()
	typeOfString        = reflect.TypeFor[string]()
	typeOfBytes         = reflect.TypeFor[[]byte]()
	typeOfDate          = reflect.TypeFor[dbimp.Date]()
	typeOfInstant       = reflect.TypeFor[time.Time]()
	typeOfLocalDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfInterval      = reflect.TypeFor[dbimp.Interval]()
	typeOfList          = reflect.TypeFor[[]any]()
	typeOfMap           = reflect.TypeFor[map[string]any]()
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the type t (D135 and D193). A
// VARIANT is the decoded JSON value, and a VOID is nil, so each has the type
// any.
func scanType(t *typ) reflect.Type {
	switch t.kind {
	case kindInteger:
		return typeOfInt64
	case kindFloat:
		return typeOfFloat64
	case kindDecimal:
		return typeOfDecimal
	case kindBoolean:
		return typeOfBool
	case kindString:
		return typeOfString
	case kindBinary:
		return typeOfBytes
	case kindDate:
		return typeOfDate
	case kindTimestamp:
		return typeOfInstant
	case kindLocalTimestamp:
		return typeOfLocalDateTime
	case kindYearMonth, kindDaySecond:
		return typeOfInterval
	case kindArray:
		return typeOfList
	case kindMap, kindStruct:
		return typeOfMap
	}
	return typeOfAny
}
