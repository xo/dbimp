package dbimp

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

// These functions turn one raw JSON value into a Go value. Each one reads
// the text of the token, so a number never passes through float64 unless
// the driver asks for a float64 (D19).

// IsNull reports whether v is a missing value or a JSON null.
func IsNull(v jsontext.Value) bool {
	return len(v) == 0 || v.Kind() == 'n'
}

// Int64 returns the JSON number v as an int64. It returns an error for a
// number with a fraction or an exponent, and for a number out of range.
func Int64(v jsontext.Value) (int64, error) {
	if v.Kind() != '0' {
		return 0, kindError(v, "number")
	}
	i, err := strconv.ParseInt(string(bytes.TrimSpace(v)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("reading %s as int64: %w", v, ErrInvalidValue)
	}
	return i, nil
}

// Float64 returns the JSON number v as a float64.
func Float64(v jsontext.Value) (float64, error) {
	if v.Kind() != '0' {
		return 0, kindError(v, "number")
	}
	f, err := strconv.ParseFloat(string(bytes.TrimSpace(v)), 64)
	if err != nil {
		return 0, fmt.Errorf("reading %s as float64: %w", v, ErrInvalidValue)
	}
	return f, nil
}

// Bool returns the JSON literal v as a bool.
func Bool(v jsontext.Value) (bool, error) {
	switch v.Kind() {
	case 't':
		return true, nil
	case 'f':
		return false, nil
	}
	return false, kindError(v, "true or false")
}

// String returns the JSON string v, unquoted.
func String(v jsontext.Value) (string, error) {
	if v.Kind() != '"' {
		return "", kindError(v, "string")
	}
	b, err := jsontext.AppendUnquote(nil, bytes.TrimSpace(v))
	if err != nil {
		return "", fmt.Errorf("reading a string: %w", err)
	}
	return string(b), nil
}

// Decimal returns v as a new *apd.Decimal (D33). It takes a JSON number, and
// a JSON string that holds a number, because some servers send a decimal as
// a string to keep its digits.
func Decimal(v jsontext.Value) (*apd.Decimal, error) {
	var text string
	switch v.Kind() {
	case '0':
		text = string(bytes.TrimSpace(v))
	case '"':
		s, err := String(v)
		if err != nil {
			return nil, err
		}
		text = s
	default:
		return nil, kindError(v, "number or string")
	}
	d, _, err := apd.NewFromString(text)
	if err != nil {
		return nil, fmt.Errorf("reading %q as a decimal: %w", text, ErrInvalidValue)
	}
	return d, nil
}

// Number returns the JSON number v as the Go value that holds it exactly,
// for a server that sends no type with a number. An integer that fits is an
// int64. A larger integer is an *apd.Decimal. A number with a fraction or an
// exponent is a float64, because a JSON number that is not an integer is a
// double in every product that sends no type.
func Number(v jsontext.Value) (any, error) {
	if v.Kind() != '0' {
		return nil, kindError(v, "number")
	}
	text := string(bytes.TrimSpace(v))
	if !strings.ContainsAny(text, ".eE") {
		if i, err := strconv.ParseInt(text, 10, 64); err == nil {
			return i, nil
		}
		return Decimal(v)
	}
	return Float64(v)
}

// Any returns v as a Go value: nil, bool, string, a number as Number returns
// it, []any, or map[string]any. It is for a whole document, by rule 3 of
// D18. A map has no order, so a driver never returns the columns of a row
// through Any.
func Any(v jsontext.Value) (any, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	return decodeAny(dec)
}

func decodeAny(dec *jsontext.Decoder) (any, error) {
	switch dec.PeekKind() {
	case '{':
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		m := make(map[string]any)
		for dec.PeekKind() != '}' {
			tok, err := dec.ReadToken()
			if err != nil {
				return nil, err
			}
			// A token is void after the next call to the decoder, so the
			// name is read first.
			name := tok.String()
			val, err := decodeAny(dec)
			if err != nil {
				return nil, err
			}
			m[name] = val
		}
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		return m, nil
	case '[':
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		a := []any{}
		for dec.PeekKind() != ']' {
			val, err := decodeAny(dec)
			if err != nil {
				return nil, err
			}
			a = append(a, val)
		}
		if _, err := dec.ReadToken(); err != nil {
			return nil, err
		}
		return a, nil
	}
	val, err := dec.ReadValue()
	if err != nil {
		return nil, fmt.Errorf("reading a value: %w", err)
	}
	switch val.Kind() {
	case 'n':
		return nil, nil
	case 't', 'f':
		return Bool(val)
	case '"':
		return String(val)
	}
	return Number(val)
}

// Assign stores src in dest, for the ScanColumn method of a driver that
// implements driver.RowsColumnScanner. It stores src itself in a *any, and
// an *apd.Decimal in an *apd.Decimal. It hands every other pair to
// sql.ConvertAssign, and it hands a decimal to it as text, which is the form
// that a *string, a *float64 and the Scan method of apd.Decimal take.
func Assign(scanCtx driver.ScanContext, dest, src any) error {
	switch d := dest.(type) {
	case *any:
		*d = src
		return nil
	case *apd.Decimal:
		if s, ok := src.(*apd.Decimal); ok {
			d.Set(s)
			return nil
		}
	}
	if d, ok := src.(*apd.Decimal); ok {
		src = d.String()
	}
	if err := sql.ConvertAssign(scanCtx, dest, src); err != nil {
		return fmt.Errorf("assigning %T to %T: %w", src, dest, err)
	}
	return nil
}

func kindError(v jsontext.Value, want string) error {
	if len(v) == 0 {
		return fmt.Errorf("reading a missing value as a %s: %w", want, ErrInvalidValue)
	}
	return fmt.Errorf("reading %s as a %s: %w", v.Kind(), want, ErrInvalidValue)
}
