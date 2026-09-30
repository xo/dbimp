package rqlite

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// sqlSyntax is how SQLite writes a literal, a quoted identifier and a
// comment, for the parser for placeholders (D34 and D143). A quote inside a
// literal is written twice.
var sqlSyntax = dbimp.Syntax{Quotes: "'\"`", DashComments: true, BlockComments: true}

// body returns the body of a request that runs query with args, as the
// server reads it: an array of one statement, which is the text alone, or an
// array of the text, the positional arguments, and an object of the named
// ones (D142 and D143). A string of the form x'...' goes into the text as a
// literal, because the server binds it as a BLOB (measured).
func body(query string, args []driver.NamedValue) ([]byte, error) {
	query, args, err := inline(query, args)
	if err != nil {
		return nil, err
	}
	var e jsontext.Encoder
	var b strings.Builder
	e.Reset(&b)
	if err := e.WriteToken(jsontext.BeginArray); err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	if len(args) == 0 {
		if err := e.WriteToken(jsontext.String(query)); err != nil {
			return nil, fmt.Errorf("writing the request: %w", err)
		}
	} else if err := writeStatement(&e, query, args); err != nil {
		return nil, err
	}
	if err := e.WriteToken(jsontext.EndArray); err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}

// writeStatement writes the statement query with args as an array: the text,
// each positional argument in order, and an object of the named ones.
func writeStatement(e *jsontext.Encoder, query string, args []driver.NamedValue) error {
	if err := e.WriteToken(jsontext.BeginArray); err != nil {
		return fmt.Errorf("writing the request: %w", err)
	}
	if err := e.WriteToken(jsontext.String(query)); err != nil {
		return fmt.Errorf("writing the request: %w", err)
	}
	named := false
	for _, arg := range args {
		if arg.Name != "" {
			named = true
			continue
		}
		if err := writeValue(e, arg.Value); err != nil {
			return fmt.Errorf("writing the argument %d: %w", arg.Ordinal, err)
		}
	}
	if named {
		if err := e.WriteToken(jsontext.BeginObject); err != nil {
			return fmt.Errorf("writing the request: %w", err)
		}
		for _, arg := range args {
			if arg.Name == "" {
				continue
			}
			if err := e.WriteToken(jsontext.String(arg.Name)); err != nil {
				return fmt.Errorf("writing the request: %w", err)
			}
			if err := writeValue(e, arg.Value); err != nil {
				return fmt.Errorf("writing the argument %s: %w", arg.Name, err)
			}
		}
		if err := e.WriteToken(jsontext.EndObject); err != nil {
			return fmt.Errorf("writing the request: %w", err)
		}
	}
	if err := e.WriteToken(jsontext.EndArray); err != nil {
		return fmt.Errorf("writing the request: %w", err)
	}
	return nil
}

// writeValue writes one argument as the JSON value that binds as its type
// (D143).
func writeValue(e *jsontext.Encoder, v any) error {
	var tok jsontext.Token
	switch v := v.(type) {
	case nil:
		tok = jsontext.Null
	case bool:
		tok = jsontext.Bool(v)
	case int64:
		tok = jsontext.Int(v)
	case uint64:
		if v > math.MaxInt64 {
			return fmt.Errorf("writing %d: SQLite has no integer above 9223372036854775807: %w", v, dbimp.ErrInvalidValue)
		}
		tok = jsontext.Uint(v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("writing %v: JSON has no form for it: %w", v, dbimp.ErrInvalidValue)
		}
		return e.WriteValue(jsontext.Value(floatText(v)))
	case string:
		tok = jsontext.String(v)
	case []byte:
		return writeBlob(e, v)
	case time.Time:
		tok = jsontext.String(v.Format(time.RFC3339Nano))
	case dbimp.Date:
		tok = jsontext.String(v.String())
	case dbimp.LocalTime:
		tok = jsontext.String(v.String())
	case dbimp.LocalDateTime:
		tok = jsontext.String(v.String())
	default:
		return fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
	}
	return e.WriteToken(tok)
}

// floatText returns v with a fraction or an exponent, so that the server
// binds it as a REAL: 1 is 1.0 (measured and D143).
func floatText(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// writeBlob writes b as an array of numbers, which the server binds as a
// BLOB (measured).
func writeBlob(e *jsontext.Encoder, b []byte) error {
	if err := e.WriteToken(jsontext.BeginArray); err != nil {
		return err
	}
	for _, c := range b {
		if err := e.WriteToken(jsontext.Uint(uint64(c))); err != nil {
			return err
		}
	}
	return e.WriteToken(jsontext.EndArray)
}

// isHexForm reports whether the server can read the string s as a BLOB,
// which it does for x'...' (measured). The driver takes every string that
// starts with x' or X' and ends with ', after any space, so that it never
// misses one. A string that it takes wrongly still goes as text.
func isHexForm(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) >= 3 && (s[0] == 'x' || s[0] == 'X') && s[1] == '\'' && s[len(s)-1] == '\''
}

// inline writes each string argument of the form x'...' into query as a
// literal of SQL, in place of its placeholder, and returns the other
// arguments (D143). It finds a ? and an @name with the parser of the root
// package (D34). A statement with such a string and a placeholder of the
// forms ?NNN, :name or $name, which that parser does not read, is an error.
func inline(query string, args []driver.NamedValue) (string, []driver.NamedValue, error) {
	if !slices.ContainsFunc(args, isHexArg) {
		return query, args, nil
	}
	ps, err := sqlSyntax.Placeholders(query)
	if err != nil {
		return "", nil, fmt.Errorf("reading the placeholders: %w", err)
	}
	var positional []driver.NamedValue
	named := map[string]driver.NamedValue{}
	for _, arg := range args {
		if arg.Name == "" {
			positional = append(positional, arg)
		} else {
			named[arg.Name] = arg
		}
	}
	var b strings.Builder
	last, k := 0, 0
	drop := map[int]bool{}
	for _, p := range ps {
		var arg driver.NamedValue
		switch {
		case p.Name != "":
			a, ok := named[p.Name]
			if !ok {
				return "", nil, fmt.Errorf("binding @%s: no argument has the name: %w", p.Name, dbimp.ErrArguments)
			}
			arg = a
		case p.Offset+p.Len < len(query) && isDigit(query[p.Offset+p.Len]):
			return "", nil, fmt.Errorf("writing an argument of the form x'...' as text: the driver does not read the placeholder ?NNN: %w", dbimp.ErrNotSupported)
		case k < len(positional):
			arg = positional[k]
			k++
		default:
			return "", nil, fmt.Errorf("binding the placeholder at %d: too few arguments: %w", p.Offset, dbimp.ErrArguments)
		}
		if !isHexArg(arg) {
			continue
		}
		b.WriteString(query[last:p.Offset])
		b.WriteString("'" + strings.ReplaceAll(arg.Value.(string), "'", "''") + "'") //nolint:forcetypeassert // isHexArg holds a string.
		last = p.Offset + p.Len
		drop[arg.Ordinal] = true
	}
	var rest []driver.NamedValue
	for _, arg := range args {
		if !isHexArg(arg) {
			rest = append(rest, arg)
			continue
		}
		if !drop[arg.Ordinal] {
			return "", nil, fmt.Errorf("writing the argument %d of the form x'...' as text: the driver finds only a ? and an @name: %w", arg.Ordinal, dbimp.ErrNotSupported)
		}
	}
	b.WriteString(query[last:])
	return b.String(), rest, nil
}

// isHexArg reports whether arg is a string of the form x'...'.
func isHexArg(arg driver.NamedValue) bool {
	s, ok := arg.Value.(string)
	return ok && isHexForm(s)
}

// isDigit reports whether c is a digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
