package dynamodb

import (
	"database/sql/driver"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// Set is a set of strings, of numbers or of binary values, which a caller
// sends as an argument. A []any is a list, and an argument cannot tell a
// list from a set by its Go type, so a set has a type of its own (D169). The
// type of the first element names the kind of the set: a string gives SS, a
// number gives NS, and a []byte gives BS. The values are the same as those of
// a list, and every element must have the kind of the first one. The server
// refuses an empty set, so the driver does too. A set that the driver reads
// is a []any, as the type table says.
type Set []any

// parameters returns the Parameters of the request: one typed value for each
// argument, in the order of the ? of the statement (recorded: "positional
// parameters"). The server takes too many values with no error, so the driver
// refuses a count that is not the count of the placeholders (D169). The
// server binds no named parameter (recorded: "a named parameter").
func parameters(query string, args []driver.NamedValue) ([]any, error) {
	n, err := countPlaceholders(query)
	if err != nil {
		return nil, fmt.Errorf("reading the statement: %w", err)
	}
	for _, a := range args {
		if a.Name != "" {
			return nil, fmt.Errorf("binding the argument %q: the server binds each ? by its position: %w", a.Name, dbimp.ErrNotSupported)
		}
	}
	if len(args) != n {
		return nil, fmt.Errorf("binding: %d arguments for %d ? placeholders: %w", len(args), n, dbimp.ErrArguments)
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]any, len(args))
	for i, a := range args {
		if out[i], err = attribute(a.Value); err != nil {
			return nil, fmt.Errorf("binding the argument %d: %w", a.Ordinal, err)
		}
	}
	return out, nil
}

// attribute returns the typed value of v, as the server takes it in
// Parameters: an object with one member, whose name is the type, such as
// {"S": "x"} (recorded). It encodes with json/v2.
func attribute(v any) (any, error) {
	switch v := v.(type) {
	case nil:
		return map[string]any{"NULL": true}, nil
	case string:
		return map[string]any{"S": v}, nil
	case bool:
		return map[string]any{"BOOL": v}, nil
	case []byte:
		return map[string]any{"B": base64.StdEncoding.EncodeToString(v)}, nil
	case *apd.Decimal:
		return numberAttribute(v)
	case apd.Decimal:
		return numberAttribute(&v)
	case []any:
		list := make([]any, len(v))
		for i, e := range v {
			a, err := attribute(e)
			if err != nil {
				return nil, fmt.Errorf("the element %d of a list: %w", i, err)
			}
			list[i] = a
		}
		return map[string]any{"L": list}, nil
	case map[string]any:
		m := make(map[string]any, len(v))
		for k, e := range v {
			a, err := attribute(e)
			if err != nil {
				return nil, fmt.Errorf("the key %q of a map: %w", k, err)
			}
			m[k] = a
		}
		return map[string]any{"M": m}, nil
	case Set:
		return setAttribute(v)
	}
	if text, ok := integerText(v); ok {
		return map[string]any{"N": text}, nil
	}
	switch v := v.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("a number %v: DynamoDB has no NaN and no infinity: %w", v, dbimp.ErrInvalidValue)
		}
		return map[string]any{"N": strconv.FormatFloat(v, 'g', -1, 64)}, nil
	case float32:
		return attribute(float64(v))
	}
	return nil, fmt.Errorf("the type %T: DynamoDB has no type for it: %w", v, dbimp.ErrNotSupported)
}

// integerText returns the text of v when it is an integer of any Go type.
func integerText(v any) (string, bool) {
	switch v := v.(type) {
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	}
	return "", false
}

// numberAttribute returns the typed value N of d. The server keeps 38
// digits, and refuses a number that it cannot hold (recorded: "a number
// with 39 digits"), so the driver sends every digit and leaves the refusal
// to the server.
func numberAttribute(d *apd.Decimal) (any, error) {
	if d == nil {
		return map[string]any{"NULL": true}, nil
	}
	if d.Form != apd.Finite {
		return nil, fmt.Errorf("a number %s: DynamoDB has no NaN and no infinity: %w", d.String(), dbimp.ErrInvalidValue)
	}
	return map[string]any{"N": d.Text('f')}, nil
}

// setAttribute returns the typed value SS, NS or BS of s.
func setAttribute(s Set) (any, error) {
	if len(s) == 0 {
		return nil, fmt.Errorf("a set with no element: DynamoDB refuses an empty set: %w", dbimp.ErrInvalidValue)
	}
	var kind string
	out := make([]string, len(s))
	for i, e := range s {
		var k, text string
		switch e := e.(type) {
		case string:
			k, text = "SS", e
		case []byte:
			k, text = "BS", base64.StdEncoding.EncodeToString(e)
		default:
			a, err := attribute(e)
			if err != nil {
				return nil, fmt.Errorf("the element %d of a set: %w", i, err)
			}
			n, ok := a.(map[string]any)["N"].(string)
			if !ok {
				return nil, fmt.Errorf("the element %d of a set is a %T, and a set holds strings, numbers or binary values: %w", i, e, dbimp.ErrInvalidValue)
			}
			k, text = "NS", n
		}
		if kind != "" && k != kind {
			return nil, fmt.Errorf("the element %d of a set has another kind than the first: %w", i, dbimp.ErrInvalidValue)
		}
		kind, out[i] = k, text
	}
	return map[string]any{kind: out}, nil
}
