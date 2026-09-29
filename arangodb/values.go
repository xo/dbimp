package arangodb

import (
	"bytes"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// paramName returns the name of the bind parameter of an argument: its name,
// or its ordinal, so that the ordinal n fills @n (measured).
func paramName(nv driver.NamedValue) string {
	if nv.Name != "" {
		return nv.Name
	}
	return strconv.Itoa(nv.Ordinal)
}

// aql is the syntax of AQL, for the parser of placeholders: strings in ' or
// ", names in backticks, a backslash that escapes, // and /* */ comments,
// @@name for a collection, and @1 for a positional argument (D104).
var aql = dbimp.Syntax{Quotes: "'\"`", Backslash: true, BlockComments: true, SlashComments: true, DoubleAt: true, DigitNames: true}

// bindVars returns the arguments as the bindVars of a query. A name that the
// query uses as @@name names a collection, and its key is @name (measured).
// The parser of D34 finds the placeholders, so a string or a comment that
// holds @@name does not count (D104).
func bindVars(query string, args []driver.NamedValue) (map[string]jsontext.Value, error) {
	if len(args) == 0 {
		return nil, nil
	}
	values, collections := uses(query)
	vars := make(map[string]jsontext.Value, len(args))
	for _, a := range args {
		name := paramName(a)
		v, err := value(a.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %s: %w", name, err)
		}
		if collections[name] {
			vars["@"+name] = v
		}
		if values[name] || !collections[name] {
			vars[name] = v
		}
	}
	return vars, nil
}

// uses returns the names that query uses as @name and as @@name. A query
// that the parser cannot read, such as one with a string that has no end,
// uses no @@name, and the server reports its fault.
func uses(query string) (map[string]bool, map[string]bool) {
	values, collections := map[string]bool{}, map[string]bool{}
	ps, err := aql.Placeholders(query)
	if err != nil {
		return values, collections
	}
	for _, p := range ps {
		if p.Double {
			collections[p.Name] = true
		} else {
			values[p.Name] = true
		}
	}
	return values, collections
}

// value returns the JSON of one argument. A decimal keeps every digit of its
// text, and a time is a string in RFC 3339, which is how AQL writes a date
// (measured). A slice or a map is its JSON, as AQL takes an array or an
// object.
func value(v any) (jsontext.Value, error) {
	switch v := v.(type) {
	case nil:
		return jsontext.Value("null"), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("JSON has no %v: %w", v, dbimp.ErrInvalidValue)
		}
	case time.Time:
		v = v.UTC()
		return json.Marshal(v.Format(time.RFC3339Nano))
	case *apd.Decimal:
		if v.Form != apd.Finite {
			return nil, fmt.Errorf("JSON has no %s: %w", v, dbimp.ErrInvalidValue)
		}
		return jsontext.Value(v.Text('f')), nil
	case []byte:
		return nil, fmt.Errorf("AQL has no binary type: %w", dbimp.ErrNotSupported)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("writing a parameter of %T: %w: %w", v, dbimp.ErrNotSupported, err)
	}
	return b, nil
}

// checkValue reports whether the driver takes v as it is, without the
// default converter of database/sql: a uint64, a decimal, and a slice or a
// map, which AQL takes as an array or an object.
func checkValue(v any) bool {
	switch v.(type) {
	case uint64, *apd.Decimal, []byte:
		return true
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Slice, reflect.Array:
		return true
	case reflect.Map:
		return reflect.TypeOf(v).Key().Kind() == reflect.String
	}
	return false
}

// decode returns the Go value of one JSON value (D89). A number with no
// fraction and no exponent that fits is an int64, and any other number is a
// float64, because AQL keeps a number as an int64 or a double (measured). An
// array is []any, and an object is map[string]any.
func decode(v jsontext.Value) (any, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	return decodeNext(dec)
}

func decodeNext(dec *jsontext.Decoder) (any, error) {
	switch dec.PeekKind() {
	case '{':
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for dec.PeekKind() != '}' {
			tok, err := dec.ReadToken()
			if err != nil {
				return nil, err
			}
			name := tok.String()
			val, err := decodeNext(dec)
			if err != nil {
				return nil, err
			}
			m[name] = val
		}
		_, err := dec.ReadToken()
		return m, err
	case '[':
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		a := []any{}
		for dec.PeekKind() != ']' {
			val, err := decodeNext(dec)
			if err != nil {
				return nil, err
			}
			a = append(a, val)
		}
		_, err := dec.ReadToken()
		return a, err
	}
	v, err := dec.ReadValue()
	if err != nil {
		return nil, fmt.Errorf("reading a value: %w", err)
	}
	switch v.Kind() {
	case 'n':
		return nil, nil
	case 't', 'f':
		return dbimp.Bool(v)
	case '"':
		return dbimp.String(v)
	}
	text := string(bytes.TrimSpace(v))
	if !strings.ContainsAny(text, ".eE") {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i, nil
		}
	}
	return dbimp.Float64(v)
}
