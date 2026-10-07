package clickhouse

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The families of the types that the driver reads by name (measured). A family
// is the name of a type without its arguments, such as Decimal for Decimal(18,
// 4). The names Decimal32 to Decimal256 are the family Decimal, and the names
// IntervalDay and its kin are the family Interval.
const (
	famNothing           = "Nothing"
	famBool              = "Bool"
	famInt8              = "Int8"
	famInt16             = "Int16"
	famInt32             = "Int32"
	famInt64             = "Int64"
	famInt128            = "Int128"
	famInt256            = "Int256"
	famUInt8             = "UInt8"
	famUInt16            = "UInt16"
	famUInt32            = "UInt32"
	famUInt64            = "UInt64"
	famUInt128           = "UInt128"
	famUInt256           = "UInt256"
	famFloat32           = "Float32"
	famFloat64           = "Float64"
	famBFloat16          = "BFloat16"
	famDecimal           = "Decimal"
	famString            = "String"
	famFixedString       = "FixedString"
	famEnum8             = "Enum8"
	famEnum16            = "Enum16"
	famDate              = "Date"
	famDate32            = "Date32"
	famDateTime          = "DateTime"
	famDateTime64        = "DateTime64"
	famTime              = "Time"
	famTime64            = "Time64"
	famInterval          = "Interval"
	famUUID              = "UUID"
	famIPv4              = "IPv4"
	famIPv6              = "IPv6"
	famArray             = "Array"
	famTuple             = "Tuple"
	famMap               = "Map"
	famVariant           = "Variant"
	famDynamic           = "Dynamic"
	famJSON              = "JSON"
	famPoint             = "Point"
	famRing              = "Ring"
	famPolygon           = "Polygon"
	famMultiPolygon      = "MultiPolygon"
	famLineString        = "LineString"
	famMultiLineString   = "MultiLineString"
	famMultiPoint        = "MultiPoint"
	famGeometry          = "Geometry"
	famAggregateFunction = "AggregateFunction"
	famQBit              = "QBit"
)

// typ is the type of a column, from the second line of the answer, which names
// each type as the server does, such as Nullable(Decimal(18, 4)) or
// Array(Tuple(a Int32, b String)).
type typ struct {
	// family is the name of the type without its arguments, and without the
	// wrappers Nullable, LowCardinality and SimpleAggregateFunction.
	family string
	// nullable is true when the type was inside Nullable.
	nullable bool
	// nums are the numbers of the arguments: the length of a FixedString, the
	// precision and the scale of a Decimal, the precision of a DateTime64, a
	// Time64, and the size of a QBit.
	nums []int64
	// zone is the name of the time zone of a DateTime or a DateTime64, or "".
	zone string
	// unit is the unit of an Interval, such as Day.
	unit string
	// elems are the types in the arguments: the element of an Array, the key
	// and the value of a Map, the fields of a Tuple, and the element of a
	// QBit.
	elems []*typ
	// names are the names of the fields of a Tuple, or "" for a field with no
	// name.
	names []string
}

// parseType returns the type that the server names with s.
func parseType(s string) (*typ, error) {
	s = strings.TrimSpace(s)
	name, inner, hasArgs, err := cutType(s)
	if err != nil {
		return nil, err
	}
	var args []string
	if hasArgs {
		args = splitTop(inner)
	}
	switch name {
	case "Nullable", "LowCardinality":
		if len(args) != 1 {
			return nil, fmt.Errorf("parsing the type %q: %s takes one argument: %w", s, name, dbimp.ErrInvalidValue)
		}
		t, err := parseType(args[0])
		if err != nil {
			return nil, err
		}
		t.nullable = t.nullable || name == "Nullable"
		return t, nil
	case "SimpleAggregateFunction":
		if len(args) < 2 {
			return nil, fmt.Errorf("parsing the type %q: SimpleAggregateFunction takes a function and a type: %w", s, dbimp.ErrInvalidValue)
		}
		return parseType(args[len(args)-1])
	}
	t := &typ{family: name}
	switch {
	case strings.HasPrefix(name, famInterval) && name != famInterval:
		t.family, t.unit = famInterval, strings.TrimPrefix(name, famInterval)
	case name == "Decimal32" || name == "Decimal64" || name == "Decimal128" || name == "Decimal256":
		t.family = famDecimal
		precision := map[string]int64{"Decimal32": 9, "Decimal64": 18, "Decimal128": 38, "Decimal256": 76}[name]
		scale, err := numArgs(s, args, 1)
		if err != nil {
			return nil, err
		}
		t.nums = []int64{precision, scale[0]}
		return t, nil
	}
	if err := t.readArgs(s, args); err != nil {
		return nil, err
	}
	return t, nil
}

// readArgs reads the arguments of the type that each family takes.
func (t *typ) readArgs(s string, args []string) error {
	var err error
	switch t.family {
	case famDecimal:
		t.nums, err = numArgs(s, args, 2)
	case famFixedString, famTime64:
		t.nums, err = numArgs(s, args, 1)
	case famDateTime64:
		if t.nums, err = numArgs(s, args[:min(len(args), 1)], 1); err == nil && len(args) > 1 {
			t.zone = unquoteName(args[1])
		}
	case famDateTime:
		if len(args) > 0 {
			t.zone = unquoteName(args[0])
		}
	case famQBit:
		// QBit(Float32, N) names the type of its elements, and then its size.
		if len(args) != 2 {
			return fmt.Errorf("parsing the type %q: QBit takes a type and a size: %w", s, dbimp.ErrInvalidValue)
		}
		if t.elems, err = parseTypes(s, args[:1], 1); err == nil {
			t.nums, err = numArgs(s, args[1:], 1)
		}
	case famArray:
		t.elems, err = parseTypes(s, args, 1)
	case famMap:
		t.elems, err = parseTypes(s, args, 2)
	case famTuple:
		for _, a := range args {
			name, typeText := splitField(a)
			e, perr := parseType(typeText)
			if perr != nil {
				return perr
			}
			t.elems, t.names = append(t.elems, e), append(t.names, name)
		}
	}
	return err
}

// parseTypes parses the n arguments that name types.
func parseTypes(s string, args []string, n int) ([]*typ, error) {
	if len(args) != n {
		return nil, fmt.Errorf("parsing the type %q: %d arguments, want %d: %w", s, len(args), n, dbimp.ErrInvalidValue)
	}
	out := make([]*typ, n)
	for i, a := range args {
		e, err := parseType(a)
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

// numArgs returns the first n arguments as numbers.
func numArgs(s string, args []string, n int) ([]int64, error) {
	if len(args) < n {
		return nil, fmt.Errorf("parsing the type %q: %d arguments, want %d: %w", s, len(args), n, dbimp.ErrInvalidValue)
	}
	out := make([]int64, n)
	for i := range n {
		v, err := strconv.ParseInt(strings.TrimSpace(args[i]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing the type %q: the argument %q is not a number: %w", s, args[i], dbimp.ErrInvalidValue)
		}
		out[i] = v
	}
	return out, nil
}

// unquoteName returns the text of a name in single quotes, such as a time
// zone, or the text that it is when it has no quotes.
func unquoteName(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `\'`, "'")
	}
	return s
}

// cutType splits a type into its name and the text between the outer
// parentheses of its arguments.
func cutType(s string) (string, string, bool, error) {
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return s, "", false, nil
	}
	if !strings.HasSuffix(s, ")") {
		return "", "", false, fmt.Errorf("parsing the type %q: no closing parenthesis: %w", s, dbimp.ErrInvalidValue)
	}
	return strings.TrimSpace(s[:i]), s[i+1 : len(s)-1], true, nil
}

// splitTop splits the arguments of a type at each comma that is outside
// parentheses and quotes.
func splitTop(s string) []string {
	var (
		out   []string
		depth int
		quote byte
		start int
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case c == '\'' || c == '`' || c == '"':
			quote = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" || len(out) > 0 {
		out = append(out, rest)
	}
	return out
}

// splitField splits the argument of a Tuple into the name of its field and its
// type. A field with no name is its type alone, which has no space outside
// parentheses and quotes.
func splitField(s string) (string, string) {
	var (
		depth int
		quote byte
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case c == '\'' || c == '`' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case (c == ' ' || c == '\t') && depth == 0:
			field := strings.Trim(s[:i], "`\"")
			return field, strings.TrimSpace(s[i+1:])
		}
	}
	return "", s
}
