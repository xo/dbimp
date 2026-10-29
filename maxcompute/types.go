package maxcompute

import (
	"database/sql/driver"
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

// The names of the types that EXPLAIN OUTPUT gives (recorded: "lead: explain
// output of every type"). A type with parameters, or with a type inside it, has
// its name before the first parenthesis or angle bracket.
const (
	nameTinyint       = "TINYINT"
	nameSmallint      = "SMALLINT"
	nameInt           = "INT"
	nameBigint        = "BIGINT"
	nameFloat         = "FLOAT"
	nameDouble        = "DOUBLE"
	nameDecimal       = "DECIMAL"
	nameBoolean       = "BOOLEAN"
	nameString        = "STRING"
	nameVarchar       = "VARCHAR"
	nameChar          = "CHAR"
	nameBinary        = "BINARY"
	nameDate          = "DATE"
	nameDatetime      = "DATETIME"
	nameTimestamp     = "TIMESTAMP"
	nameTimestampNTZ  = "TIMESTAMP_NTZ"
	nameIntervalDay   = "INTERVAL_DAY_TIME"
	nameIntervalMonth = "INTERVAL_YEAR_MONTH"
	nameJSON          = "JSON"
	nameArray         = "ARRAY"
	nameMap           = "MAP"
	nameStruct        = "STRUCT"
	nameVoid          = "VOID"
)

// kind is the family of a type, which decides how the driver decodes a cell.
type kind int

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
	kindIntervalDay
	kindIntervalMonth
	kindJSON
	kindText
	kindVoid
)

// typ is the type of a column.
type typ struct {
	// name is the name of the type in upper case, with no parameters, such as
	// DECIMAL or STRUCT.
	name string
	kind kind
	// length is the length of a VARCHAR or a CHAR, and 0 for any other type.
	length int64
	// precision and scale are the digits of a DECIMAL, and -1 when the text of
	// the type has none.
	precision, scale int64
}

// column is one column of a result.
type column struct {
	name string
	typ  *typ
}

// stringType is the type of a column that has no type, because the statement that
// gives the types failed and the driver read the statement itself (D219).
var stringType = &typ{name: nameString, kind: kindString, precision: -1, scale: -1}

// parseType reads the text of a type, such as DECIMAL(38,18) or ARRAY<INT>. A
// name that the driver does not know is an error, so that a new type of the
// server does not reach the caller as text with no sign.
func parseType(text string) (*typ, error) {
	text = strings.TrimSpace(text)
	name, params, _ := strings.Cut(text, "(")
	if i := strings.IndexByte(name, '<'); i >= 0 {
		name = name[:i]
	}
	t := &typ{name: strings.ToUpper(strings.TrimSpace(name)), precision: -1, scale: -1}
	switch t.name {
	case nameTinyint, nameSmallint, nameInt, nameBigint:
		t.kind = kindInteger
	case nameFloat, nameDouble:
		t.kind = kindFloat
	case nameDecimal:
		t.kind = kindDecimal
		if err := t.digits(params); err != nil {
			return nil, err
		}
	case nameBoolean:
		t.kind = kindBoolean
	case nameString:
		t.kind = kindString
	case nameVarchar, nameChar:
		t.kind = kindString
		if n, ok := strings.CutSuffix(params, ")"); ok {
			v, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("reading the length of the type %q: %w", text, dbimp.ErrInvalidValue)
			}
			t.length = v
		}
	case nameBinary:
		t.kind = kindBinary
	case nameDate:
		t.kind = kindDate
	case nameDatetime, nameTimestamp:
		t.kind = kindTimestamp
	case nameTimestampNTZ:
		t.kind = kindLocalTimestamp
	case nameIntervalDay:
		t.kind = kindIntervalDay
	case nameIntervalMonth:
		t.kind = kindIntervalMonth
	case nameJSON:
		t.kind = kindJSON
	case nameArray, nameMap, nameStruct:
		t.kind = kindText
	case nameVoid:
		t.kind = kindVoid
	default:
		return nil, fmt.Errorf("reading the type %q: the driver does not know it: %w", text, dbimp.ErrNotSupported)
	}
	return t, nil
}

// digits reads the precision and the scale of a DECIMAL from the text after its
// parenthesis, such as 38,18). A DECIMAL with no parameters has none.
func (t *typ) digits(params string) error {
	body, ok := strings.CutSuffix(params, ")")
	if !ok {
		return nil
	}
	p, s, hasScale := strings.Cut(body, ",")
	var err error
	if t.precision, err = strconv.ParseInt(strings.TrimSpace(p), 10, 64); err != nil {
		return fmt.Errorf("reading the precision of %q: %w", t.name, dbimp.ErrInvalidValue)
	}
	t.scale = 0
	if hasScale {
		if t.scale, err = strconv.ParseInt(strings.TrimSpace(s), 10, 64); err != nil {
			return fmt.Errorf("reading the scale of %q: %w", t.name, dbimp.ErrInvalidValue)
		}
	}
	return nil
}

// errNoTypes is the error of the answer of EXPLAIN OUTPUT that is no table of the
// names and the types of columns, such as the answer for SHOW PARTITIONS. The
// statement is then no query, and the driver sends it as it is (D219 item 4).
const errNoTypes dbimp.Error = "the answer holds no types"

// parseTypes reads the types of the answer of EXPLAIN OUTPUT, which is a text of
// lines: a line of the two names name and type, a line of dashes, and a line for
// each column, with the name and the type apart by a tab (recorded). It returns
// the type of each column, in order.
func parseTypes(text string) ([]*typ, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "name\ttype" {
		return nil, fmt.Errorf("reading the types of a result: the first line is not the names of the columns: %w", errNoTypes)
	}
	var types []*typ
	for _, line := range lines[2:] {
		if line == "" {
			continue
		}
		_, text, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("reading the types of a result: the line %q has no type: %w", line, dbimp.ErrInvalidValue)
		}
		t, err := parseType(text)
		if err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, nil
}

// The Go types of a value, one for each type (D135).
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
	typeOfAny           = reflect.TypeFor[any]()
)

// scanType returns the Go type of a value of the type t (D135 and D219). A JSON
// value is the decoded value, and a VOID is nil, so each has the type any.
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
	case kindBinary:
		return typeOfBytes
	case kindDate:
		return typeOfDate
	case kindTimestamp:
		return typeOfInstant
	case kindLocalTimestamp:
		return typeOfLocalDateTime
	case kindIntervalDay, kindIntervalMonth:
		return typeOfInterval
	case kindString, kindText:
		return typeOfString
	}
	return typeOfAny
}

// The layouts of a DATETIME and of a TIMESTAMP (recorded).
const (
	layoutDatetime  = "2006-01-02 15:04:05"
	layoutTimestamp = "2006-01-02 15:04:05.999999999"
)

// decode returns the value of the cell c of a column of the type t. A DATETIME
// and a TIMESTAMP are read in loc, the zone of the project (D219 item 2).
func decode(t *typ, c cell, loc *time.Location) (driver.Value, error) {
	if c.null {
		return nil, nil //nolint:nilnil // A NULL is nil (hard rule 3).
	}
	v, err := decodeText(t, c.text, loc)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", t.name, err)
	}
	return v, nil
}

// decodeText returns the value that the text s holds, for the type t.
func decodeText(t *typ, s string, loc *time.Location) (driver.Value, error) {
	switch t.kind {
	case kindInteger:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer: %w", s, dbimp.ErrInvalidValue)
		}
		return v, nil
	case kindFloat:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number: %w", s, dbimp.ErrInvalidValue)
		}
		return v, nil
	case kindDecimal:
		d, _, err := apd.NewFromString(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a decimal: %w", s, dbimp.ErrInvalidValue)
		}
		return d, nil
	case kindBoolean:
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not a boolean: %w", s, dbimp.ErrInvalidValue)
	case kindBinary:
		return unescapeBinary(s)
	case kindDate:
		return dbimp.ParseDate(s) //nolint:wrapcheck // The error names the form.
	case kindTimestamp:
		layout := layoutTimestamp
		if t.name == nameDatetime {
			layout = layoutDatetime
		}
		v, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			return nil, fmt.Errorf("%q is not a time: %w", s, dbimp.ErrInvalidValue)
		}
		return v, nil
	case kindLocalTimestamp:
		return dbimp.ParseLocalDateTime(s) //nolint:wrapcheck // The error names the form.
	case kindIntervalDay:
		return parseDayTime(s)
	case kindIntervalMonth:
		return parseYearMonth(s)
	case kindJSON:
		if strings.TrimSpace(s) == "" {
			return nil, nil //nolint:nilnil // An empty text holds no value.
		}
		return dbimp.Any(jsontext.Value(s)) //nolint:wrapcheck // The error names the form.
	}
	return s, nil
}

// unescapeBinary returns the bytes of the text of a BINARY cell. The server
// writes a byte that is not a plain character as an equals sign and two
// hexadecimal digits, such as =00=FF=10 (recorded).
func unescapeBinary(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '=' {
			out = append(out, s[i])
			continue
		}
		if i+3 > len(s) {
			return nil, fmt.Errorf("%q ends inside an escape: %w", s, dbimp.ErrInvalidValue)
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("%q holds the escape %q: %w", s, s[i:i+3], dbimp.ErrInvalidValue)
		}
		out = append(out, byte(v))
		i += 2
	}
	return out, nil
}

// parseYearMonth reads the text of an INTERVAL_YEAR_MONTH, such as 1-2, which is
// one year and two months (recorded). A minus sign turns both parts around.
func parseYearMonth(s string) (dbimp.Interval, error) {
	neg := strings.HasPrefix(s, "-")
	y, m, ok := strings.Cut(strings.TrimPrefix(s, "-"), "-")
	years, errY := strconv.ParseInt(y, 10, 32)
	months, errM := strconv.ParseInt(m, 10, 32)
	if !ok || errY != nil || errM != nil || years < 0 || months < 0 {
		return dbimp.Interval{}, fmt.Errorf("%q is not an interval of years and months: %w", s, dbimp.ErrInvalidValue)
	}
	total := years*12 + months
	if total > math.MaxInt32 {
		return dbimp.Interval{}, fmt.Errorf("%q has more months than an interval holds: %w", s, dbimp.ErrInvalidValue)
	}
	if neg {
		total = -total
	}
	return dbimp.Interval{Months: int32(total)}, nil
}

// parseDayTime reads the text of an INTERVAL_DAY_TIME, such as
// 1 02:03:04.000000000, which is one day and a time (recorded). A minus sign
// turns both parts around.
func parseDayTime(s string) (dbimp.Interval, error) {
	bad := fmt.Errorf("%q is not an interval of days and a time: %w", s, dbimp.ErrInvalidValue)
	neg := strings.HasPrefix(s, "-")
	d, clock, ok := strings.Cut(strings.TrimPrefix(s, "-"), " ")
	days, err := strconv.ParseInt(d, 10, 32)
	if !ok || err != nil || days < 0 {
		return dbimp.Interval{}, bad
	}
	hms, frac, _ := strings.Cut(clock, ".")
	parts := strings.Split(hms, ":")
	if len(parts) != 3 {
		return dbimp.Interval{}, bad
	}
	var secs [3]int64
	for i, p := range parts {
		if secs[i], err = strconv.ParseInt(p, 10, 64); err != nil || secs[i] < 0 {
			return dbimp.Interval{}, bad
		}
	}
	var nanos int64
	if frac != "" {
		if len(frac) > 9 {
			frac = frac[:9]
		}
		if nanos, err = strconv.ParseInt(frac+strings.Repeat("0", 9-len(frac)), 10, 64); err != nil {
			return dbimp.Interval{}, bad
		}
	}
	total := ((secs[0]*60+secs[1])*60+secs[2])*int64(time.Second) + nanos
	iv := dbimp.Interval{Days: int32(days), Nanoseconds: total}
	if neg {
		iv.Days, iv.Nanoseconds = -iv.Days, -iv.Nanoseconds
	}
	return iv, nil
}
