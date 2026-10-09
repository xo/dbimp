package snowflake

import (
	"database/sql/driver"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The types of a binding that the server takes (measured). VARIANT is not one
// of them.
const (
	bindFixed        = "FIXED"
	bindReal         = "REAL"
	bindText         = "TEXT"
	bindBoolean      = "BOOLEAN"
	bindBinary       = "BINARY"
	bindDate         = "DATE"
	bindTime         = "TIME"
	bindTimestampNTZ = "TIMESTAMP_NTZ"
	bindTimestampTZ  = "TIMESTAMP_TZ"
)

// binding is one typed binding. The value is always text, or null for a NULL
// (measured).
type binding struct {
	Type  string  `json:"type"`
	Value *string `json:"value"`
}

// namedBinding is a binding with its key in bindings: the number of its
// placeholder, or its name.
type namedBinding struct {
	binding

	key string
}

// bindings is the member bindings of the body. It writes its keys in the
// order of the arguments, so that a body never depends on the order of a map.
type bindings []namedBinding

// MarshalJSONTo satisfies json.MarshalerTo.
func (b bindings) MarshalJSONTo(enc *jsontext.Encoder) error {
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for _, nb := range b {
		if err := enc.WriteToken(jsontext.String(nb.key)); err != nil {
			return err
		}
		if err := json.MarshalEncode(enc, nb.binding); err != nil {
			return err
		}
	}
	return enc.WriteToken(jsontext.EndObject)
}

// bindArgs returns the arguments as the bindings of the server, in the order
// of the arguments (D183). A numbered argument takes its number as its key,
// and a named one its name, as the placeholders :1 and :a do. The driver
// writes no placeholder of its own, so the server binds the ? of the
// statement.
func bindArgs(args []driver.NamedValue) (bindings, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make(bindings, 0, len(args))
	for _, arg := range args {
		b, err := bind(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %d: %w", arg.Ordinal, err)
		}
		key := arg.Name
		if key == "" {
			key = strconv.Itoa(arg.Ordinal)
		}
		out = append(out, namedBinding{binding: b, key: key})
	}
	return out, nil
}

// bind returns v as a typed binding. A NULL is a TEXT of null. A VARIANT is
// not a type of a binding, so a list or a map fails with dbimp.ErrArguments.
func bind(v any) (binding, error) {
	switch v := v.(type) {
	case nil:
		return binding{Type: bindText}, nil
	case int64:
		return binding{Type: bindFixed, Value: new(strconv.FormatInt(v, 10))}, nil
	case float64:
		return binding{Type: bindReal, Value: new(formatFloat(v))}, nil
	case string:
		return binding{Type: bindText, Value: new(v)}, nil
	case bool:
		return binding{Type: bindBoolean, Value: new(strconv.FormatBool(v))}, nil
	case []byte:
		return binding{Type: bindBinary, Value: new(strings.ToUpper(hex.EncodeToString(v)))}, nil
	case dbimp.Date:
		if !v.IsValid() {
			return binding{}, fmt.Errorf("writing the date %s: %w", v, dbimp.ErrInvalidValue)
		}
		return binding{Type: bindDate, Value: new(strconv.FormatInt(v.In(time.UTC).UnixMilli(), 10))}, nil
	case dbimp.LocalTime:
		if !v.IsValid() {
			return binding{}, fmt.Errorf("writing the time %s: %w", v, dbimp.ErrInvalidValue)
		}
		nanos := ((int64(v.Hour)*60+int64(v.Minute))*60+int64(v.Second))*int64(time.Second) + int64(v.Nanosecond)
		return binding{Type: bindTime, Value: new(strconv.FormatInt(nanos, 10))}, nil
	case dbimp.LocalDateTime:
		if !v.IsValid() {
			return binding{}, fmt.Errorf("writing the date and time %s: %w", v, dbimp.ErrInvalidValue)
		}
		return binding{Type: bindTimestampNTZ, Value: new(epochNanos(v.In(time.UTC)))}, nil
	case time.Time:
		_, offset := v.Zone()
		return binding{Type: bindTimestampTZ, Value: new(epochNanos(v) + " " + strconv.Itoa(offset/60+1440))}, nil
	case *apd.Decimal:
		if v.Form != apd.Finite {
			return binding{}, fmt.Errorf("writing %s as FIXED: it is not a finite number: %w", v, dbimp.ErrInvalidValue)
		}
		return binding{Type: bindFixed, Value: new(v.Text('f'))}, nil
	case []any, map[string]any:
		return binding{}, fmt.Errorf("writing an argument of %T: VARIANT is not a type of a binding, so bind the JSON text and parse it in the statement: %w", v, dbimp.ErrArguments)
	}
	return binding{}, fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrArguments)
}

// formatFloat writes a float as the text of a REAL: the digits that read
// back to v, and NaN, Infinity and -Infinity as the server takes them as a
// binding: it refuses inf and -inf there (measured, 2026-10-10).
func formatFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	case math.IsInf(v, -1):
		return "-Infinity"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// epochNanos returns the nanoseconds from the epoch to t as text. A
// timestamp of the server runs from the year 1 to the year 9999, and that
// range does not fit an int64 of nanoseconds, so the sum is exact.
func epochNanos(t time.Time) string {
	n := new(big.Int).Mul(big.NewInt(t.Unix()), big.NewInt(int64(time.Second)))
	return n.Add(n, big.NewInt(int64(t.Nanosecond()))).String()
}
