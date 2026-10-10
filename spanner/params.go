package spanner

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// syntax is how GoogleSQL writes literals, quoted names and comments, so that
// the parser for placeholders skips them (D34). A single quote, a double quote
// and a backtick each open a quote, and a backslash escapes in a quote. A
// comment is -- or # to the end of the line, or /* to */.
var syntax = dbimp.Syntax{
	Quotes:        "'\"`",
	Backslash:     true,
	DashComments:  true,
	HashComments:  true,
	BlockComments: true,
}

// array is an argument that binds as an ARRAY. CheckNamedValue makes it from
// a slice, so that an empty slice keeps the type of its elements, and a nil
// slice is a NULL of that type. The values
// are canonical: each one is a Go value that bindValue takes.
type array struct {
	elem wireType
	vals []any
	// null is true for a nil slice, which binds as a NULL of the type ARRAY.
	null bool
}

// bound holds the parameters of one statement, in the two members of the
// request.
type bound struct {
	// text is the statement, with each ? written as @pN.
	text string
	// params maps the name of a parameter, with no @, to its value in the
	// JSON form of its type. paramTypes maps it to its type, and holds no entry
	// for a NULL of no type.
	params     map[string]any
	paramTypes map[string]wireType
}

// bindArgs returns the statement and the arguments in the form that the
// server takes (D191 item 7). The server binds named parameters only, as @name
// (recorded: "a positional parameter"). A ? becomes @p1, @p2 and so on, by
// the number of the argument. A caller can write @name with sql.Named. A
// statement that mixes the two forms is an error. A caller that writes @p1
// with a positional argument gets the same binding as one that writes ?.
func bindArgs(query string, args []driver.NamedValue) (bound, error) {
	b := bound{text: query}
	ps, err := syntax.Placeholders(query)
	if err != nil {
		return b, fmt.Errorf("binding the arguments: %w", err)
	}
	var marks, names int
	for _, p := range ps {
		if p.Name == "" {
			marks++
		} else {
			names++
		}
	}
	var positional, named int
	for _, a := range args {
		if a.Name == "" {
			positional++
		} else {
			named++
		}
	}
	switch {
	case marks > 0 && names > 0, positional > 0 && named > 0:
		return b, fmt.Errorf("binding the arguments: the statement mixes ? and @name, or the arguments mix numbered and named ones: %w", dbimp.ErrArguments)
	case marks > 0 && marks != positional:
		return b, fmt.Errorf("binding the arguments: %d ? placeholders and %d arguments: %w", marks, positional, dbimp.ErrArguments)
	}
	if marks > 0 {
		var sb strings.Builder
		last, n := 0, 0
		for _, p := range ps {
			if p.Name != "" {
				continue
			}
			n++
			sb.WriteString(query[last:p.Offset])
			sb.WriteString("@p" + strconv.Itoa(n))
			last = p.Offset + p.Len
		}
		sb.WriteString(query[last:])
		b.text = sb.String()
	}
	if len(args) == 0 {
		return b, nil
	}
	b.params = make(map[string]any, len(args))
	b.paramTypes = make(map[string]wireType, len(args))
	for _, a := range args {
		name := a.Name
		if name == "" {
			name = "p" + strconv.Itoa(a.Ordinal)
		}
		t, v, err := bindValue(a.Value)
		if err != nil {
			return b, fmt.Errorf("binding the argument %d: %w", a.Ordinal, err)
		}
		b.params[name] = v
		if t.Code != "" {
			b.paramTypes[name] = t
		}
	}
	return b, nil
}

// bindValue returns the type of v and its value in the JSON form of that type
// (docs/SPANNER.md, "Types"). A NULL has no type, and the driver leaves its
// type out, so the server takes it from the statement. v is canonical, as
// CheckNamedValue leaves it.
func bindValue(v any) (wireType, any, error) {
	switch v := v.(type) {
	case nil:
		return wireType{}, nil, nil
	case int64:
		// An INT64 is a string, and a JSON number is refused (recorded: "an INT64
		// parameter given as a JSON number").
		return wireType{Code: wireInt64}, strconv.FormatInt(v, 10), nil
	case float64:
		return wireType{Code: wireFloat64}, formatFloat(v), nil
	case float32:
		// A float32 is a FLOAT32 parameter. The server writes the widened value, so
		// a float32 and the float64 of a FLOAT32 column convert exactly.
		return wireType{Code: wireFloat32}, formatFloat(float64(v)), nil
	case bool:
		return wireType{Code: wireBool}, v, nil
	case string:
		return wireType{Code: wireString}, v, nil
	case []byte:
		if v == nil {
			// A nil slice is NULL, as it is for the other drivers of database/sql.
			return wireType{}, nil, nil
		}
		return wireType{Code: wireBytes}, base64.StdEncoding.EncodeToString(v), nil
	case time.Time:
		return wireType{Code: wireTimestamp}, v.UTC().Format(time.RFC3339Nano), nil
	case dbimp.Date:
		if !v.IsValid() {
			return wireType{}, nil, fmt.Errorf("writing the date %s: %w", v, dbimp.ErrInvalidValue)
		}
		return wireType{Code: wireDate}, v.String(), nil
	case *apd.Decimal:
		if v.Form != apd.Finite {
			return wireType{}, nil, fmt.Errorf("writing %s as NUMERIC: it is not a finite number: %w", v, dbimp.ErrInvalidValue)
		}
		return wireType{Code: wireNumeric}, v.Text('f'), nil
	case uuid.UUID:
		return wireType{Code: wireUUID}, v.String(), nil
	case dbimp.Interval:
		text := v.String()
		if v == (dbimp.Interval{}) {
			// The server writes a zero interval as P0Y (recorded: "INTERVAL
			// values with fractions and negatives").
			text = "P0Y"
		}
		return wireType{Code: wireInterval}, text, nil
	case jsontext.Value:
		if !v.IsValid() {
			return wireType{}, nil, fmt.Errorf("writing a JSON argument: the text is not JSON: %w", dbimp.ErrInvalidValue)
		}
		return wireType{Code: wireJSON}, string(v), nil
	case map[string]any:
		text, err := json.Marshal(v, json.Deterministic(true))
		if err != nil {
			return wireType{}, nil, fmt.Errorf("writing a JSON argument: %w", err)
		}
		return wireType{Code: wireJSON}, string(text), nil
	case array:
		return bindArray(v)
	}
	return wireType{}, nil, fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrArguments)
}

// bindArray returns the type and the value of an ARRAY.
func bindArray(a array) (wireType, any, error) {
	if a.null {
		elem := a.elem
		return wireType{Code: wireArray, Elem: &elem}, nil, nil
	}
	out := make([]any, len(a.vals))
	for i, e := range a.vals {
		t, v, err := bindValue(e)
		if err != nil {
			return wireType{}, nil, fmt.Errorf("writing element %d of an ARRAY: %w", i, err)
		}
		if t.Code != "" && !reflect.DeepEqual(t, a.elem) {
			return wireType{}, nil, fmt.Errorf("writing element %d of an ARRAY: it is %s, and the elements are %s: %w", i, t.Code, a.elem.Code, dbimp.ErrArguments)
		}
		out[i] = v
	}
	elem := a.elem
	return wireType{Code: wireArray, Elem: &elem}, out, nil
}

// formatFloat writes a FLOAT64 as the server takes it: a JSON number, and the
// strings NaN, Infinity and -Infinity for the three values that JSON lacks
// (recorded: "FLOAT64 parameters that are not finite").
func formatFloat(v float64) any {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	case math.IsInf(v, -1):
		return "-Infinity"
	}
	return v
}

// canon returns v as a Go value that bindValue takes. It is what
// CheckNamedValue and database/sql do to an argument.
func canon(v any) (any, error) {
	switch x := v.(type) {
	case nil, int64, float64, float32, bool, string, []byte, time.Time, dbimp.Date, *apd.Decimal, uuid.UUID, dbimp.Interval, jsontext.Value, map[string]any:
		if d, ok := x.(*apd.Decimal); ok && d == nil {
			return nil, nil
		}
		return v, nil
	case apd.Decimal:
		return &x, nil
	}
	if valuer, ok := v.(driver.Valuer); ok {
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			return nil, nil
		}
		out, err := valuer.Value()
		if err != nil {
			return nil, fmt.Errorf("reading the value of %T: %w", v, err)
		}
		return canon(out)
	}
	out, err := driver.DefaultParameterConverter.ConvertValue(v)
	if err != nil {
		return nil, fmt.Errorf("converting %T: %w: %w", v, dbimp.ErrArguments, err)
	}
	return out, nil
}

// makeArray returns the array that the slice s binds as. A []any takes the type
// of its elements from the first one that is not nil, and fails when it has
// none. Any other slice takes it from its element type, so an empty slice and
// a slice of NULLs keep their type.
func makeArray(s reflect.Value) (array, error) {
	a := array{vals: make([]any, s.Len()), null: s.IsNil()}
	for i := range a.vals {
		e := s.Index(i)
		if e.Kind() == reflect.Interface && e.IsNil() {
			continue
		}
		v, err := canon(e.Interface())
		if err != nil {
			return array{}, fmt.Errorf("writing element %d of an ARRAY: %w", i, err)
		}
		a.vals[i] = v
	}
	if et := s.Type().Elem(); et.Kind() != reflect.Interface {
		t, ok := elemType(et)
		if !ok {
			return array{}, fmt.Errorf("writing an ARRAY of %s: the type has no Spanner type: %w", et, dbimp.ErrArguments)
		}
		a.elem = t
		return a, nil
	}
	for _, v := range a.vals {
		if v == nil {
			continue
		}
		t, _, err := bindValue(v)
		if err != nil {
			return array{}, err
		}
		a.elem = t
		return a, nil
	}
	return array{}, fmt.Errorf("writing an ARRAY with no element that has a value: the type of the elements is unknown, so use a slice of a type, such as []int64: %w", dbimp.ErrArguments)
}

// elemTypes are the Go types whose Spanner type does not follow from their kind.
var elemTypes = map[reflect.Type]string{
	reflect.TypeFor[[]byte]():         wireBytes,
	reflect.TypeFor[time.Time]():      wireTimestamp,
	reflect.TypeFor[dbimp.Date]():     wireDate,
	reflect.TypeFor[*apd.Decimal]():   wireNumeric,
	reflect.TypeFor[apd.Decimal]():    wireNumeric,
	reflect.TypeFor[uuid.UUID]():      wireUUID,
	reflect.TypeFor[dbimp.Interval](): wireInterval,
	reflect.TypeFor[jsontext.Value](): wireJSON,
	reflect.TypeFor[map[string]any](): wireJSON,
}

// elemType returns the Spanner type of the elements of a slice, from the Go type
// of the elements. An empty slice and a slice of NULLs have no element to ask.
func elemType(rt reflect.Type) (wireType, bool) {
	if code, ok := elemTypes[rt]; ok {
		return wireType{Code: code}, true
	}
	switch rt.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return wireType{Code: wireInt64}, true
	case reflect.Float32:
		return wireType{Code: wireFloat32}, true
	case reflect.Float64:
		return wireType{Code: wireFloat64}, true
	case reflect.Bool:
		return wireType{Code: wireBool}, true
	case reflect.String:
		return wireType{Code: wireString}, true
	}
	return wireType{}, false
}
