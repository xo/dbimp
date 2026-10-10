package bigquery

import (
	"database/sql/driver"
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The parameter modes of the request (recorded: bigquery-101 and bigquery-115).
const (
	modeNamed      = "NAMED"
	modePositional = "POSITIONAL"
)

// The types of a parameter that the driver sends. A value must carry its own
// type, because the service cannot infer it from the text (docs/BIGQUERY.md).
const (
	typeInt64      = "INT64"
	typeFloat64    = "FLOAT64"
	typeNumeric    = "NUMERIC"
	typeBigNumeric = "BIGNUMERIC"
	typeBool       = "BOOL"
	typeString     = "STRING"
	typeBytes      = "BYTES"
	typeDate       = "DATE"
	typeTime       = "TIME"
	typeDatetime   = "DATETIME"
	typeTimestamp  = "TIMESTAMP"
	typeInterval   = "INTERVAL"
)

// maxNumericIntegerDigits is the digits before the point that a NUMERIC holds,
// and maxNumericScale the digits after it. A BIGNUMERIC holds 38 before and 38
// after (docs/BIGQUERY.md).
const (
	maxNumericIntegerDigits = 29
	maxNumericScale         = 9
	maxBigNumericInteger    = 39
	maxBigNumericScale      = 38
)

// paramType is the member parameterType of a parameter.
type paramType struct {
	Type string `json:"type"`
}

// paramValue is the member parameterValue of a parameter. The value is text,
// and a NULL has no value (recorded: bigquery-133).
type paramValue struct {
	Value *string `json:"value,omitzero"`
}

// queryParam is one entry of queryParameters.
type queryParam struct {
	Name  string     `json:"name,omitzero"`
	Type  paramType  `json:"parameterType"`
	Value paramValue `json:"parameterValue"`
}

// bindArgs returns the parameter mode and the parameters of the arguments, in
// the order of the arguments (D189). The arguments are all named, and the
// statement writes @name, or none is named, and the statement writes ?. The
// service refuses a mix (recorded: bigquery-152 and bigquery-153), so the driver
// refuses it first.
func bindArgs(args []driver.NamedValue) (string, []queryParam, error) {
	if len(args) == 0 {
		return "", nil, nil
	}
	named := args[0].Name != ""
	out := make([]queryParam, 0, len(args))
	seen := map[string]bool{}
	for _, arg := range args {
		if (arg.Name != "") != named {
			return "", nil, fmt.Errorf("binding the argument %d: the arguments must be all named or all positional: %w", arg.Ordinal, dbimp.ErrArguments)
		}
		if named {
			if !validParamName(arg.Name) {
				return "", nil, fmt.Errorf("binding the argument %d: %q is not the name of a parameter: %w", arg.Ordinal, arg.Name, dbimp.ErrArguments)
			}
			if seen[arg.Name] {
				return "", nil, fmt.Errorf("binding the argument %d: the name %q is used twice: %w", arg.Ordinal, arg.Name, dbimp.ErrArguments)
			}
			seen[arg.Name] = true
		}
		typ, val, err := bind(arg.Value)
		if err != nil {
			return "", nil, fmt.Errorf("binding the argument %d: %w", arg.Ordinal, err)
		}
		out = append(out, queryParam{Name: arg.Name, Type: paramType{Type: typ}, Value: paramValue{Value: val}})
	}
	if named {
		return modeNamed, out, nil
	}
	return modePositional, out, nil
}

// validParamName reports whether s can follow the @ of a named parameter.
func validParamName(s string) bool {
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return !isNameRune(r) })
}

// bind returns v as the type and the text of a parameter. A NULL has no type,
// so it is a STRING with no value, which only a STRING column or a CAST takes
// (docs/BIGQUERY.md). A list and a map fail with dbimp.ErrArguments.
func bind(v any) (string, *string, error) {
	switch v := v.(type) {
	case nil:
		return typeString, nil, nil
	case int64:
		return typeInt64, new(strconv.FormatInt(v, 10)), nil
	case float64:
		return typeFloat64, new(formatFloat(v)), nil
	case string:
		return typeString, new(v), nil
	case bool:
		return typeBool, new(strconv.FormatBool(v)), nil
	case []byte:
		return typeBytes, new(base64.StdEncoding.EncodeToString(v)), nil
	case dbimp.Date:
		if !v.IsValid() || v.Year < 1 || v.Year > 9999 {
			return "", nil, fmt.Errorf("writing the date %s: %w", v, dbimp.ErrInvalidValue)
		}
		return typeDate, new(v.String()), nil
	case dbimp.LocalTime:
		if !v.IsValid() || v.Nanosecond%1000 != 0 {
			return "", nil, fmt.Errorf("writing the time %s: a TIME has microseconds at most: %w", v, dbimp.ErrInvalidValue)
		}
		return typeTime, new(v.String()), nil
	case dbimp.LocalDateTime:
		if !v.IsValid() || v.Date.Year < 1 || v.Date.Year > 9999 || v.Time.Nanosecond%1000 != 0 {
			return "", nil, fmt.Errorf("writing the date and time %s: a DATETIME is in the years 1 to 9999, with microseconds at most: %w", v, dbimp.ErrInvalidValue)
		}
		return typeDatetime, new(v.String()), nil
	case time.Time:
		u := v.UTC()
		if u.Year() < 1 || u.Year() > 9999 || u.Nanosecond()%1000 != 0 {
			return "", nil, fmt.Errorf("writing the timestamp %s: a TIMESTAMP is in the years 1 to 9999, with microseconds at most: %w", v.Format(time.RFC3339Nano), dbimp.ErrInvalidValue)
		}
		return typeTimestamp, new(u.Format("2006-01-02 15:04:05.999999") + "+00:00"), nil
	case *apd.Decimal:
		return bindDecimal(v)
	case dbimp.Interval:
		if v.Nanoseconds%1000 != 0 {
			return "", nil, fmt.Errorf("writing the interval %s: an INTERVAL has microseconds at most: %w", v, dbimp.ErrInvalidValue)
		}
		return typeInterval, new(formatInterval(v)), nil
	case []any, map[string]any:
		return "", nil, fmt.Errorf("writing an argument of %T: the driver binds no ARRAY or STRUCT, so bind the JSON text and parse it in the statement: %w", v, dbimp.ErrArguments)
	}
	return "", nil, fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrArguments)
}

// bindDecimal returns a decimal as a NUMERIC when it fits one, and as a
// BIGNUMERIC when it does not.
func bindDecimal(d *apd.Decimal) (string, *string, error) {
	if d.Form != apd.Finite {
		return "", nil, fmt.Errorf("writing %s as a decimal: it is not a finite number: %w", d, dbimp.ErrInvalidValue)
	}
	scale := max(0, int(-d.Exponent))
	// A coefficient that ends in zeros has fewer digits than its exponent says,
	// so the driver counts the digits of the number as the text writes it.
	text := d.Text('f')
	integer, _, _ := strings.Cut(strings.TrimPrefix(text, "-"), ".")
	digits := len(strings.TrimLeft(integer, "0"))
	switch {
	case digits <= maxNumericIntegerDigits && scale <= maxNumericScale:
		return typeNumeric, &text, nil
	case digits <= maxBigNumericInteger && scale <= maxBigNumericScale:
		return typeBigNumeric, &text, nil
	}
	return "", nil, fmt.Errorf("writing %s as a decimal: it does not fit a BIGNUMERIC: %w", text, dbimp.ErrInvalidValue)
}

// formatFloat writes a float as the text of a FLOAT64: the digits that read
// back to v, and NaN, Infinity and -Infinity as the service takes them
// (recorded: bigquery-136 and bigquery-137).
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

// formatInterval writes an interval as BigQuery reads it:
// [sign]years-months [sign]days [sign]hours:minutes:seconds[.fraction]. Each
// part carries its own sign.
func formatInterval(iv dbimp.Interval) string {
	ym := "0-0"
	if iv.Months != 0 {
		months := int64(iv.Months)
		sign := ""
		if months < 0 {
			sign, months = "-", -months
		}
		ym = fmt.Sprintf("%s%d-%d", sign, months/12, months%12)
	}
	nanos := iv.Nanoseconds
	sign := ""
	secs, frac := nanos/int64(time.Second), nanos%int64(time.Second)
	if nanos < 0 {
		// Each part is negated after the division, so that the smallest int64
		// fits.
		sign, secs, frac = "-", -secs, -frac
	}
	clock := fmt.Sprintf("%s%d:%d:%d", sign, secs/3600, secs%3600/60, secs%60)
	if frac != 0 {
		clock += "." + strings.TrimRight(fmt.Sprintf("%09d", frac), "0")
	}
	return fmt.Sprintf("%s %d %s", ym, iv.Days, clock)
}
