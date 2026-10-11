package exasol

import (
	"database/sql/driver"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// bind returns the data of executePreparedStatement for args: one column of
// one value for each parameter, in the JSON form that the server reads for the
// declared type of the parameter (D208 and docs/EXASOL.md, Parameters). The
// server binds the values, so no value is written into the text of the
// statement.
func bind(cols []column, args []driver.NamedValue, loc func() (*time.Location, error)) ([]jsontext.Value, error) {
	if len(args) != len(cols) {
		return nil, fmt.Errorf("binding the arguments: %d arguments for %d placeholders: %w", len(args), len(cols), dbimp.ErrArguments)
	}
	data := make([]jsontext.Value, len(cols))
	for i, a := range args {
		if a.Name != "" {
			return nil, fmt.Errorf("binding the argument %s: the server takes positional arguments only: %w", a.Name, dbimp.ErrNotSupported)
		}
		v, err := bindValue(cols[i], a.Value, loc)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %d for %s: %w", a.Ordinal, cols[i].databaseType(), err)
		}
		// The data holds the list of the values of a column.
		data[i] = jsontext.Value("[" + string(v) + "]")
	}
	return data, nil
}

// bindValue returns the JSON of the value v for the parameter c.
func bindValue(c column, v any, loc func() (*time.Location, error)) (jsontext.Value, error) {
	if v == nil {
		return jsontext.Value("null"), nil
	}
	switch c.kind {
	case kindBoolean:
		b, ok := v.(bool)
		if !ok {
			return nil, unsupported(v)
		}
		return json.Marshal(b)
	case kindInteger, kindDecimal:
		s, err := decimalText(v)
		if err != nil {
			return nil, err
		}
		return json.Marshal(s)
	case kindDouble:
		return doubleJSON(v)
	case kindDate:
		return textJSON(dateText(v))
	case kindTimestamp:
		return textJSON(timestampText(v, nil))
	case kindTimestampLTZ:
		return textJSON(timestampText(v, loc))
	case kindIntervalYM:
		return textJSON(intervalText(v, func(iv dbimp.Interval) (string, error) {
			return formatIntervalYM(iv, c.typ.Precision)
		}))
	case kindIntervalDS:
		return textJSON(intervalText(v, func(iv dbimp.Interval) (string, error) {
			return formatIntervalDS(iv, c.typ.Precision, c.typ.Fraction)
		}))
	case kindHashtype:
		switch x := v.(type) {
		case []byte:
			return json.Marshal(hex.EncodeToString(x))
		case string:
			return json.Marshal(x)
		}
		return nil, unsupported(v)
	case kindGeometry:
		return textJSON(stringText(v))
	}
	// A string parameter is the type that the server gives a placeholder in a
	// select list (recorded: "prepare a select with parameters in the select
	// list"), so every value has a text form.
	return textJSON(anyText(v))
}

func unsupported(v any) error {
	return fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}

// textJSON returns the JSON string of s, or err.
func textJSON(s string, err error) (jsontext.Value, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// decimalText returns the digits of a number for a DECIMAL parameter. The
// server takes a number or a string, and a string keeps every digit (recorded:
// "lead: a string for a decimal with a scale").
func decimalText(v any) (string, error) {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10), nil
	case uint64:
		return strconv.FormatUint(x, 10), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", fmt.Errorf("writing %v: a decimal is finite: %w", x, dbimp.ErrInvalidValue)
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case *apd.Decimal:
		if x.Form != apd.Finite {
			return "", fmt.Errorf("writing %s: a decimal is finite: %w", x, dbimp.ErrInvalidValue)
		}
		return x.Text('f'), nil
	case string:
		if _, _, err := apd.NewFromString(x); err != nil {
			return "", fmt.Errorf("writing %q as a decimal: %w", x, dbimp.ErrInvalidValue)
		}
		return x, nil
	case []byte:
		return decimalText(string(x))
	}
	return "", unsupported(v)
}

// doubleJSON returns the JSON number of a DOUBLE parameter. The server refuses
// an integer for a double: the number needs a fraction or an exponent
// (recorded: "lead: an integer for a double"), so a whole number gets ".0".
func doubleJSON(v any) (jsontext.Value, error) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int64:
		f = float64(x)
	case uint64:
		f = float64(x)
	case *apd.Decimal, string, []byte:
		s, err := decimalText(x)
		if err != nil {
			return nil, err
		}
		if f, err = strconv.ParseFloat(s, 64); err != nil {
			return nil, fmt.Errorf("writing %s as a double: %w", s, dbimp.ErrInvalidValue)
		}
	default:
		return nil, unsupported(v)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("writing %v: the server takes no double that is not finite: %w", f, dbimp.ErrInvalidValue)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return jsontext.Value(s), nil
}

// stringText returns v as text for a column of text: a string, or bytes that
// are UTF-8.
func stringText(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case []byte:
		if !utf8.Valid(x) {
			return "", fmt.Errorf("writing bytes that are not UTF-8 as text: %w", dbimp.ErrInvalidValue)
		}
		return string(x), nil
	}
	return "", unsupported(v)
}

// dateText returns v as the text of a DATE.
func dateText(v any) (string, error) {
	switch x := v.(type) {
	case dbimp.Date:
		return x.String(), nil
	case time.Time:
		return dbimp.DateOf(x).String(), nil
	case dbimp.LocalDateTime:
		return x.Date.String(), nil
	case string:
		return x, nil
	}
	return "", unsupported(v)
}

// timestampText returns v as the text of a TIMESTAMP. A time.Time keeps the
// date and the time of day of its own zone, as a TIMESTAMP holds no zone. If
// loc is not nil, the column is TIMESTAMP WITH LOCAL TIME ZONE, and the time is
// in the zone of the session.
func timestampText(v any, loc func() (*time.Location, error)) (string, error) {
	switch x := v.(type) {
	case time.Time:
		if loc != nil {
			l, err := loc()
			if err != nil {
				return "", err
			}
			x = x.In(l)
		}
		return localText(dbimp.LocalDateTimeOf(x)), nil
	case dbimp.LocalDateTime:
		return localText(x), nil
	case dbimp.Date:
		return localText(dbimp.LocalDateTime{Date: x}), nil
	case string:
		return x, nil
	}
	return "", unsupported(v)
}

// localText writes a date and a time as the server does: a space between
// them, and a fraction of up to nine digits if there is one.
func localText(dt dbimp.LocalDateTime) string {
	return dt.Date.String() + " " + dt.Time.String()
}

// intervalText returns v as the text of an interval column, which format
// writes for a dbimp.Interval.
func intervalText(v any, format func(dbimp.Interval) (string, error)) (string, error) {
	switch x := v.(type) {
	case dbimp.Interval:
		return format(x)
	case string:
		return x, nil
	}
	return "", unsupported(v)
}

// anyText returns v as the text that a VARCHAR parameter takes.
func anyText(v any) (string, error) {
	switch x := v.(type) {
	case string, []byte:
		return stringText(x)
	case bool:
		return strconv.FormatBool(x), nil
	case int64, uint64, float64, *apd.Decimal:
		return decimalText(x)
	case time.Time:
		return timestampText(x, nil)
	case dbimp.Date:
		return x.String(), nil
	case dbimp.LocalDateTime:
		return localText(x), nil
	case dbimp.Interval:
		return x.String(), nil
	}
	return "", unsupported(v)
}
