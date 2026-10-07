package clickhouse

import (
	"database/sql/driver"
	"fmt"
	"maps"
	"math"
	"math/big"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// syntax is how ClickHouse writes literals, quoted names and comments, so
// that the parser for placeholders skips them (D34). A single quote, a double
// quote and a backtick each open a quote, and a backslash escapes in a quote. A
// comment is -- or # to the end of the line, or /* to */.
var syntax = dbimp.Syntax{
	Quotes:        "'\"`",
	Backslash:     true,
	DashComments:  true,
	HashComments:  true,
	BlockComments: true,
}

// bind writes a typed parameter in the place of each ? and each @name of the
// statement, and returns the statement and the query keys that hold the
// values (D176). The parameter of the first argument is {p1:Type} and its key
// is param_p1. A statement with no arguments goes as it is, because ClickHouse
// has a ? of its own in the operator ?:. A ? takes the next argument that has no
// name, and an @name takes the argument with that name. A missing argument and
// an argument left over are both dbimp.ErrArguments.
func bind(query string, args []driver.NamedValue) (string, url.Values, error) {
	if len(args) == 0 {
		return query, nil, nil
	}
	ps, err := syntax.Placeholders(query)
	if err != nil {
		return "", nil, fmt.Errorf("binding the arguments: %w", err)
	}
	var positional []driver.NamedValue
	named := map[string]driver.NamedValue{}
	for _, a := range args {
		if a.Name == "" {
			positional = append(positional, a)
			continue
		}
		named[a.Name] = a
	}
	var (
		b      strings.Builder
		params = url.Values{}
		used   = map[string]string{}
		last   int
		next   int
	)
	for _, p := range ps {
		var arg driver.NamedValue
		switch {
		case p.Name != "":
			a, ok := named[p.Name]
			if !ok {
				return "", nil, fmt.Errorf("binding @%s: no argument: %w", p.Name, dbimp.ErrArguments)
			}
			arg = a
		case next < len(positional):
			arg = positional[next]
			next++
		default:
			return "", nil, fmt.Errorf("binding ? at offset %d: no argument: %w", p.Offset, dbimp.ErrArguments)
		}
		b.WriteString(query[last:p.Offset])
		last = p.Offset + p.Len
		if name, ok := used[p.Name]; ok && p.Name != "" {
			b.WriteString(name)
			continue
		}
		key := "p" + strconv.Itoa(len(params)+1)
		kind, text, err := parameter(arg.Value)
		if err != nil {
			return "", nil, fmt.Errorf("binding argument %d: %w", arg.Ordinal, err)
		}
		params.Set("param_"+key, text)
		ref := "{" + key + ":" + kind + "}"
		b.WriteString(ref)
		if p.Name != "" {
			used[p.Name] = ref
		}
	}
	b.WriteString(query[last:])
	if next < len(positional) {
		return "", nil, fmt.Errorf("binding %d arguments to %d placeholders: %w", len(positional), next, dbimp.ErrArguments)
	}
	for name := range named {
		if _, ok := used[name]; !ok {
			return "", nil, fmt.Errorf("binding @%s: no placeholder: %w", name, dbimp.ErrArguments)
		}
	}
	return b.String(), params, nil
}

// value is an argument as ClickHouse takes it: its type, its text as the value of
// a parameter, and its text inside an array, a tuple or a map, where a string is
// in quotes.
type value struct {
	kind   string
	plain  string
	quoted string
}

// parameter returns the type of the parameter for v, and its text (D176). The
// type comes from the Go type of v.
func parameter(v any) (string, string, error) {
	val, err := literal(v)
	if err != nil {
		return "", "", err
	}
	return val.kind, val.plain, nil
}

// literal returns the value for v.
func literal(v any) (value, error) {
	switch v := v.(type) {
	case nil:
		return value{kind: "Nullable(Nothing)", plain: `\N`, quoted: "NULL"}, nil
	case bool:
		return plainValue("Bool", strconv.FormatBool(v)), nil
	case int64:
		return plainValue("Int64", strconv.FormatInt(v, 10)), nil
	case uint64:
		return plainValue("UInt64", strconv.FormatUint(v, 10)), nil
	case float64:
		return plainValue("Float64", formatFloat(v)), nil
	case string:
		return stringValue("String", v), nil
	case []byte:
		return stringValue("String", string(v)), nil
	case time.Time:
		return stringValue("DateTime64(9, 'UTC')", v.UTC().Format("2006-01-02 15:04:05.000000000")), nil
	case dbimp.Date:
		return stringValue("Date32", v.String()), nil
	case uuid.UUID:
		return stringValue("UUID", v.String()), nil
	case netip.Addr:
		kind := "IPv6"
		if v.Is4() {
			kind = "IPv4"
		}
		return stringValue(kind, v.String()), nil
	case *big.Int:
		switch {
		case v == nil:
			return literal(nil)
		case v.BitLen() <= 255 || v.Sign() < 0 && v.BitLen() == 256 && v.Cmp(minInt256) == 0:
			return plainValue("Int256", v.String()), nil
		case v.Sign() > 0 && v.BitLen() == 256:
			return plainValue("UInt256", v.String()), nil
		}
		return value{}, fmt.Errorf("writing %d bits as an integer: the server holds 256: %w", v.BitLen(), dbimp.ErrInvalidValue)
	case *apd.Decimal:
		if v == nil {
			return literal(nil)
		}
		return decimalValue(v)
	case []any:
		return listValue(v)
	case []string:
		return listOf(v)
	case []int64:
		return listOf(v)
	case []float64:
		return listOf(v)
	case []bool:
		return listOf(v)
	case map[string]any:
		return mapValue(v)
	}
	return value{}, fmt.Errorf("writing %T as a parameter: %w", v, dbimp.ErrNotSupported)
}

// minInt256 is the smallest value of an Int256, which is -2^255.
var minInt256 = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255))

// plainValue returns a value whose text is the same in every place.
func plainValue(kind, text string) value {
	return value{kind: kind, plain: text, quoted: text}
}

// stringValue returns a value whose text is in single quotes inside an array,
// a tuple or a map. A backslash, a quote and the control characters of a tab
// and a new line are escaped, because the text of a parameter is the text of the
// format TabSeparated, in which each of them has a meaning (measured).
func stringValue(kind, s string) value {
	return value{kind: kind, plain: escape(s), quoted: "'" + strings.ReplaceAll(escape(s), "'", `\'`) + "'"}
}

// escape writes s as the format TabSeparated writes a string.
func escape(s string) string {
	var b strings.Builder
	for i := range len(s) {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case 0:
			b.WriteString(`\0`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// formatFloat writes a float with the text that reads back as the same float,
// and the NaN and the infinities as the server reads them.
func formatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// decimalValue returns the value of a decimal, with a scale that holds each of
// its digits. The type holds up to 76 digits (measured).
func decimalValue(d *apd.Decimal) (value, error) {
	if d.Form != apd.Finite {
		return value{}, fmt.Errorf("writing the decimal %s: the server has no NaN and no infinity of a decimal: %w", d, dbimp.ErrInvalidValue)
	}
	scale := max(-int(d.Exponent), 0)
	digits := d.NumDigits()
	if int(digits)+max(int(d.Exponent), 0) > 76 || scale > 76 {
		return value{}, fmt.Errorf("writing the decimal %s: the server holds 76 digits: %w", d, dbimp.ErrInvalidValue)
	}
	return plainValue("Decimal(76, "+strconv.Itoa(scale)+")", d.Text('f')), nil
}

// listValue returns an Array of the elements of v, which must have one type,
// and NULL.
func listValue(v []any) (value, error) {
	elems := make([]value, len(v))
	for i, e := range v {
		var err error
		if elems[i], err = literal(e); err != nil {
			return value{}, fmt.Errorf("writing the element %d of an array: %w", i, err)
		}
	}
	return arrayOf(elems)
}

// listOf returns an Array of the elements of a slice.
func listOf[T any](v []T) (value, error) {
	elems := make([]any, len(v))
	for i, e := range v {
		elems[i] = e
	}
	return listValue(elems)
}

// arrayOf returns the Array of elems. Its type is the type of the elements that
// are not NULL, and Nullable of it when one is NULL. An empty array has the type
// that the server gives [], which is Array(Nothing).
func arrayOf(elems []value) (value, error) {
	var u unifier
	for i, e := range elems {
		if err := u.add(e.kind); err != nil {
			return value{}, fmt.Errorf("writing the element %d of an array: %w", i, err)
		}
	}
	kind := u.kind()
	parts := make([]string, len(elems))
	for i, e := range elems {
		parts[i] = e.quoted
	}
	text := "[" + strings.Join(parts, ",") + "]"
	return value{kind: "Array(" + kind + ")", plain: text, quoted: text}, nil
}

// mapValue returns a Map from String to the type of the values.
func mapValue(v map[string]any) (value, error) {
	keys := slices.Sorted(maps.Keys(v))
	var u unifier
	parts := make([]string, len(keys))
	for i, k := range keys {
		e, err := literal(v[k])
		if err != nil {
			return value{}, fmt.Errorf("writing the value of the key %q of a map: %w", k, err)
		}
		if err := u.add(e.kind); err != nil {
			return value{}, fmt.Errorf("writing the value of the key %q of a map: %w", k, err)
		}
		parts[i] = stringValue("String", k).quoted + ":" + e.quoted
	}
	kind := u.kind()
	text := "{" + strings.Join(parts, ",") + "}"
	return value{kind: "Map(String, " + kind + ")", plain: text, quoted: text}, nil
}

// unifier finds the one type of the elements of an array or the values of a map.
// A NULL has no type of its own, and makes the type Nullable. An empty array and
// an empty map take the type of the others of their kind.
type unifier struct {
	typed   string
	empty   string
	hasNull bool
}

// add takes the type of one element.
func (u *unifier) add(kind string) error {
	switch kind {
	case "Nullable(Nothing)":
		u.hasNull = true
		return nil
	case "Array(Nothing)", "Map(String, Nothing)":
		u.empty = kind
		return nil
	}
	if u.typed != "" && kind != u.typed {
		return fmt.Errorf("its type %s differs from %s: %w", kind, u.typed, dbimp.ErrInvalidValue)
	}
	u.typed = kind
	return nil
}

// kind returns the type of the elements. A list of empty lists has the type of
// an empty list, and a list with no element has the type Nothing.
func (u *unifier) kind() string {
	kind := u.typed
	if kind == "" {
		kind = u.empty
	}
	switch {
	case kind == "":
		kind = "Nothing"
		if u.hasNull {
			kind = "Nullable(Nothing)"
		}
	case u.hasNull:
		kind = "Nullable(" + kind + ")"
	}
	return kind
}
