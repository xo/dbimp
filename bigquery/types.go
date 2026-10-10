package bigquery

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The names of the types in the member type of a field of the schema, which
// are the wire types (recorded: bigquery-070). The service writes the legacy
// names INTEGER, FLOAT, BOOLEAN and RECORD, and never INT64, FLOAT64, BOOL or
// STRUCT, and the driver reads both forms.
const (
	wireInteger    = "INTEGER"
	wireFloat      = "FLOAT"
	wireNumeric    = "NUMERIC"
	wireBigNumeric = "BIGNUMERIC"
	wireBoolean    = "BOOLEAN"
	wireString     = "STRING"
	wireBytes      = "BYTES"
	wireDate       = "DATE"
	wireTime       = "TIME"
	wireDatetime   = "DATETIME"
	wireTimestamp  = "TIMESTAMP"
	wireJSON       = "JSON"
	wireGeography  = "GEOGRAPHY"
	wireInterval   = "INTERVAL"
	wireRecord     = "RECORD"
	wireRange      = "RANGE"
	// wireArray is the name that ColumnTypeDatabaseTypeName gives a column of the
	// mode REPEATED. The schema has no such type.
	wireArray = "ARRAY"
)

// The modes of a field (recorded: bigquery-017 and bigquery-142).
const (
	modeNullable = "NULLABLE"
	modeRequired = "REQUIRED"
	modeRepeated = "REPEATED"
)

// aliases map the names of GoogleSQL to the legacy names of the schema.
var aliases = map[string]string{
	"INT64":   wireInteger,
	"FLOAT64": wireFloat,
	"BOOL":    wireBoolean,
	"STRUCT":  wireRecord,
}

// schemaField is one field of the member schema of an answer.
type schemaField struct {
	Name   string        `json:"name"`
	Type   string        `json:"type"`
	Mode   string        `json:"mode"`
	Fields []schemaField `json:"fields"`
}

// schema is the member schema of an answer. A statement with no result has none
// (recorded: bigquery-050), and the emulator writes an empty object.
type schema struct {
	Fields []schemaField `json:"fields"`
}

// column is the type of one column, or of one member of a RECORD.
type column struct {
	name string
	// wire is the type of the field, in upper case, with the legacy name.
	wire string
	mode string
	// fields are the members of a RECORD.
	fields []column
}

// newColumns reads the fields of a schema. It returns an error that names the
// column for a RECORD whose members repeat a name or have none (D189 item 7).
func newColumns(fields []schemaField, path string) ([]column, error) {
	out := make([]column, len(fields))
	for i, f := range fields {
		wire := strings.ToUpper(f.Type)
		if a, ok := aliases[wire]; ok {
			wire = a
		}
		name := path + f.Name
		c := column{name: f.Name, wire: wire, mode: strings.ToUpper(f.Mode)}
		if wire == wireRecord {
			var err error
			if c.fields, err = newColumns(f.Fields, name+"."); err != nil {
				return nil, err
			}
			seen := map[string]bool{}
			for _, m := range c.fields {
				if m.name == "" || seen[m.name] {
					return nil, fmt.Errorf("reading the column %s: the struct has a member with no name or a repeated name, so it has no map: cast it in SQL, for example with TO_JSON_STRING: %w", name, dbimp.ErrNotSupported)
				}
				seen[m.name] = true
			}
		}
		out[i] = c
	}
	return out, nil
}

// repeated reports whether the column is an ARRAY.
func (c column) repeated() bool {
	return c.mode == modeRepeated
}

// nullable reports whether a value of the column can be NULL. An ARRAY is never
// NULL, because the service writes a NULL array as an empty one (recorded:
// bigquery-070).
func (c column) nullable() bool {
	return c.mode != modeRepeated && c.mode != modeRequired
}

// databaseType returns the name of the type in upper case.
func (c column) databaseType() string {
	if c.repeated() {
		return wireArray
	}
	return c.wire
}

// cell is one value of a row, which holds the member v (recorded: bigquery-070).
type cell struct {
	V jsontext.Value `json:"v"`
}

// decode returns the value v of a column as its Go value (D135 and D189). A NULL
// is nil, except an ARRAY, which is never NULL.
func decode(c column, v jsontext.Value) (any, error) {
	if !c.repeated() {
		return decodeOne(c, v)
	}
	if dbimp.IsNull(v) {
		return []any{}, nil
	}
	var elems []cell
	if err := json.Unmarshal(v, &elems); err != nil {
		return nil, fmt.Errorf("reading the array of the column %s: %w", c.name, dbimp.ErrInvalidValue)
	}
	out := make([]any, len(elems))
	for i, e := range elems {
		x, err := decodeOne(c, e.V)
		if err != nil {
			return nil, err
		}
		out[i] = x
	}
	return out, nil
}

// decodeOne returns the value v of a column that is not an ARRAY, or of one
// element of an ARRAY.
func decodeOne(c column, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	if c.wire == wireRecord {
		return decodeRecord(c, v)
	}
	s, err := text(v)
	if err != nil {
		return nil, fmt.Errorf("reading the column %s: %w", c.name, err)
	}
	x, err := decodeText(c.wire, s)
	if err != nil {
		return nil, fmt.Errorf("reading the column %s: %w", c.name, err)
	}
	return x, nil
}

// decodeRecord reads a RECORD, which is {"f":[{"v":...},...]} in the order of
// the members (recorded: bigquery-097), as a map by the names of the members.
func decodeRecord(c column, v jsontext.Value) (any, error) {
	var rec struct {
		F []cell `json:"f"`
	}
	if err := json.Unmarshal(v, &rec); err != nil {
		return nil, fmt.Errorf("reading the struct of the column %s: %w", c.name, dbimp.ErrInvalidValue)
	}
	if len(rec.F) != len(c.fields) {
		return nil, fmt.Errorf("reading the struct of the column %s: %d values for %d members: %w", c.name, len(rec.F), len(c.fields), dbimp.ErrColumnCount)
	}
	out := make(map[string]any, len(c.fields))
	for i, m := range c.fields {
		x, err := decode(m, rec.F[i].V)
		if err != nil {
			return nil, err
		}
		out[m.name] = x
	}
	return out, nil
}

// text returns the text of a scalar value. The service writes every scalar as a
// JSON string (recorded: bigquery-070), and the emulator can write a number or a
// boolean.
func text(v jsontext.Value) (string, error) {
	switch v.Kind() {
	case '"':
		return dbimp.String(v)
	case '0':
		return strings.TrimSpace(string(v)), nil
	case 't':
		return "true", nil
	case 'f':
		return "false", nil
	}
	return "", fmt.Errorf("reading %v as a scalar: %w", v.Kind(), dbimp.ErrInvalidValue)
}

// decodeText returns the Go value of the text s of a scalar of the wire type.
func decodeText(wire, s string) (any, error) {
	switch wire {
	case wireInteger:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("reading %q as an integer: %w", s, dbimp.ErrInvalidValue)
		}
		return i, nil
	case wireFloat:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("reading %q as a float: %w", s, dbimp.ErrInvalidValue)
		}
		return f, nil
	case wireNumeric, wireBigNumeric:
		d, _, err := apd.NewFromString(s)
		if err != nil || d.Form != apd.Finite {
			return nil, fmt.Errorf("reading %q as a decimal: %w", s, dbimp.ErrInvalidValue)
		}
		return d, nil
	case wireBoolean:
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("reading %q as a boolean: %w", s, dbimp.ErrInvalidValue)
	case wireString, wireGeography, wireRange:
		return s, nil
	case wireBytes:
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("reading %q as base64: %w", s, dbimp.ErrInvalidValue)
		}
		return b, nil
	case wireDate:
		return dbimp.ParseDate(s)
	case wireTime:
		return dbimp.ParseLocalTime(s)
	case wireDatetime:
		return dbimp.ParseLocalDateTime(s)
	case wireTimestamp:
		return parseTimestamp(s)
	case wireJSON:
		val := jsontext.Value(s)
		if !val.IsValid() {
			return nil, fmt.Errorf("reading %q as JSON: %w", s, dbimp.ErrInvalidValue)
		}
		return dbimp.Any(val)
	case wireInterval:
		return parseInterval(s)
	}
	// A type that the driver does not know, such as a new type of the service, or
	// a field with no type, as the emulator writes for a RANGE, is the text of
	// the service.
	return s, nil
}

// The range of the microseconds from the epoch that the years 1 to 9999 name.
const (
	minMicro = -62135596800 * 1e6
	maxMicro = 253402300799*1e6 + 999999
)

// parseTimestamp reads a timestamp. The driver asks for ISO8601_STRING, such as
// 2024-01-02T03:04:05.123456Z (recorded: bigquery-342). The emulator ignores
// that and sends seconds with a fraction, such as 1704164645.123456, and the
// service sends a double with an exponent by default, such as
// 1.704164645123456E9 (recorded: bigquery-070). The driver reads that text as a
// decimal, never as a float64, because a float64 loses the last digit.
func parseTimestamp(s string) (time.Time, error) {
	if strings.Contains(s, "T") {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("reading %q as a timestamp: %w", s, dbimp.ErrInvalidValue)
		}
		return t.UTC(), nil
	}
	bad := fmt.Errorf("reading %q as a timestamp: %w", s, dbimp.ErrInvalidValue)
	d, _, err := apd.NewFromString(s)
	if err != nil || d.Form != apd.Finite || d.Exponent > 20 || d.Exponent < -40 || d.NumDigits() > 40 {
		return time.Time{}, bad
	}
	var micros apd.Decimal
	ctx := apd.BaseContext.WithPrecision(80)
	if _, err := ctx.Mul(&micros, d, apd.New(1, 6)); err != nil {
		return time.Time{}, bad
	}
	if _, err := ctx.Quantize(&micros, &micros, 0); err != nil {
		return time.Time{}, bad
	}
	n, err := micros.Int64()
	if err != nil || n < minMicro || n > maxMicro {
		return time.Time{}, bad
	}
	return time.UnixMicro(n).UTC(), nil
}

// parseInterval reads the text of an INTERVAL, which is
// [sign]years-months [sign]days [sign]hours:minutes:seconds[.fraction], such
// as 1-2 3 4:5:6.789 and -0-5 0 0:0:0 (recorded: bigquery-092). Each part
// carries its own sign.
func parseInterval(s string) (dbimp.Interval, error) {
	bad := fmt.Errorf("reading %q as an interval: %w", s, dbimp.ErrInvalidValue)
	fields := strings.Fields(s)
	if len(fields) != 3 {
		return dbimp.Interval{}, bad
	}
	ymSign, ym := cutSign(fields[0])
	ys, ms, ok := strings.Cut(ym, "-")
	years, ok1 := number(ys, 10000)
	months, ok2 := number(ms, 11)
	if !ok || !ok1 || !ok2 {
		return dbimp.Interval{}, bad
	}
	dSign, ds := cutSign(fields[1])
	days, ok := number(ds, 3660000)
	if !ok {
		return dbimp.Interval{}, bad
	}
	tSign, clock := cutSign(fields[2])
	parts := strings.Split(clock, ":")
	if len(parts) != 3 {
		return dbimp.Interval{}, bad
	}
	secText, frac, _ := strings.Cut(parts[2], ".")
	hours, ok1 := number(parts[0], 87840000)
	minutes, ok2 := number(parts[1], 59)
	secs, ok3 := number(secText, 59)
	if !ok1 || !ok2 || !ok3 || len(frac) > 9 || frac != "" && !isDigits(frac) {
		return dbimp.Interval{}, bad
	}
	var nanos int64
	if frac != "" {
		n, err := strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64)
		if err != nil {
			return dbimp.Interval{}, bad
		}
		nanos = n
	}
	total := ((hours*60+minutes)*60+secs)*int64(time.Second) + nanos
	return dbimp.Interval{
		// The limits of number keep both in the range of an int32.
		Months:      int32(ymSign * (years*12 + months)), //nolint:gosec // At most 120011 in size.
		Days:        int32(dSign * days),                 //nolint:gosec // At most 3660000 in size.
		Nanoseconds: tSign * total,
	}, nil
}

// cutSign returns the sign of s as 1 or -1, and s without it.
func cutSign(s string) (int64, string) {
	switch {
	case strings.HasPrefix(s, "-"):
		return -1, s[1:]
	case strings.HasPrefix(s, "+"):
		return 1, s[1:]
	}
	return 1, s
}

// number reads decimal digits as a number that is at most limit.
func number(s string, limit int64) (int64, bool) {
	if !isDigits(s) || len(s) > 10 {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n <= limit
}

// isDigits reports whether s is one or more decimal digits.
func isDigits(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// The scan types of the columns.
var (
	typeOfInt64         = reflect.TypeFor[int64]()
	typeOfDecimal       = reflect.TypeFor[*apd.Decimal]()
	typeOfFloat64       = reflect.TypeFor[float64]()
	typeOfString        = reflect.TypeFor[string]()
	typeOfBytes         = reflect.TypeFor[[]byte]()
	typeOfBool          = reflect.TypeFor[bool]()
	typeOfDate          = reflect.TypeFor[dbimp.Date]()
	typeOfTime          = reflect.TypeFor[dbimp.LocalTime]()
	typeOfLocalDateTime = reflect.TypeFor[dbimp.LocalDateTime]()
	typeOfInstant       = reflect.TypeFor[time.Time]()
	typeOfInterval      = reflect.TypeFor[dbimp.Interval]()
	typeOfMap           = reflect.TypeFor[map[string]any]()
	typeOfList          = reflect.TypeFor[[]any]()
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the column c (D135 and D189). A
// JSON column is the decoded JSON value, so its type is any. A type that the
// driver does not know is its text.
func scanType(c column) reflect.Type {
	if c.repeated() {
		return typeOfList
	}
	switch c.wire {
	case wireInteger:
		return typeOfInt64
	case wireFloat:
		return typeOfFloat64
	case wireNumeric, wireBigNumeric:
		return typeOfDecimal
	case wireBoolean:
		return typeOfBool
	case wireString, wireGeography, wireRange:
		return typeOfString
	case wireBytes:
		return typeOfBytes
	case wireDate:
		return typeOfDate
	case wireTime:
		return typeOfTime
	case wireDatetime:
		return typeOfLocalDateTime
	case wireTimestamp:
		return typeOfInstant
	case wireInterval:
		return typeOfInterval
	case wireRecord:
		return typeOfMap
	case wireJSON:
		return typeOfAny
	}
	return typeOfString
}
