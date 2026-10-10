package databricks

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// The types of a parameter that the driver sends. The server kept the type of
// each one (measured). It refuses BINARY, ARRAY, MAP and STRUCT with the error
// INVALID_PARAMETER_MARKER_VALUE.INVALID_DATA_TYPE (measured), so the driver
// sends none of them (D193 item 9).
const (
	typeBigint       = "BIGINT"
	typeDouble       = "DOUBLE"
	typeString       = "STRING"
	typeBoolean      = "BOOLEAN"
	typeDate         = "DATE"
	typeTimestamp    = "TIMESTAMP"
	typeTimestampNTZ = "TIMESTAMP_NTZ"
	typeYearToMonth  = "INTERVAL YEAR TO MONTH"
	typeDayToSecond  = "INTERVAL DAY TO SECOND"
	// typeNull is the type of a parameter that is nil, which has no Go type
	// to name one. The server takes a NULL of the type VOID for a column of
	// any type, where a NULL of the type STRING fails for a column of a number
	// under ANSI mode. The server has no recording for it, so the live run
	// holds the fact (D193).
	typeNull = "VOID"
)

// maxPrecision is the most digits of a DECIMAL (measured).
const maxPrecision = 38

// param is one parameter of the body. A named marker :name has the member
// name, and a positional marker ? has none. The value is text, or null for a
// NULL (measured).
type param struct {
	Name  string  `json:"name,omitzero"`
	Value *string `json:"value"`
	Type  string  `json:"type,omitzero"`
}

// bindArgs returns the arguments as the parameters of the server, in the
// order of the arguments (D193). The driver writes no marker of its own, so
// the server binds the ? and the :name of the statement.
func bindArgs(args []driver.NamedValue) ([]param, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]param, 0, len(args))
	for _, arg := range args {
		p, err := bind(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %d: %w", arg.Ordinal, err)
		}
		p.Name = arg.Name
		out = append(out, p)
	}
	return out, nil
}

// bind returns v as a typed parameter. A list and a map have no type that the
// server takes, and neither has a byte slice, so each fails with
// dbimp.ErrArguments.
func bind(v any) (param, error) {
	switch v := v.(type) {
	case nil:
		return param{Type: typeNull}, nil
	case int64:
		return param{Value: new(strconv.FormatInt(v, 10)), Type: typeBigint}, nil
	case float64:
		return param{Value: new(formatFloat(v)), Type: typeDouble}, nil
	case string:
		return param{Value: new(v), Type: typeString}, nil
	case bool:
		return param{Value: new(strconv.FormatBool(v)), Type: typeBoolean}, nil
	case []byte:
		return param{}, fmt.Errorf("writing an argument of []byte: the server refuses a BINARY parameter, so bind the hexadecimal text and write unhex(:name) in the statement: %w", dbimp.ErrArguments)
	case dbimp.Date:
		if !v.IsValid() || v.Year < 1 || v.Year > 9999 {
			return param{}, fmt.Errorf("writing the date %s: %w", v, dbimp.ErrInvalidValue)
		}
		return param{Value: new(v.String()), Type: typeDate}, nil
	case dbimp.LocalDateTime:
		if !v.IsValid() || v.Date.Year < 1 || v.Date.Year > 9999 {
			return param{}, fmt.Errorf("writing the date and time %s: %w", v, dbimp.ErrInvalidValue)
		}
		return param{Value: new(formatLocal(v)), Type: typeTimestampNTZ}, nil
	case time.Time:
		u := v.UTC()
		if u.Year() < 1 || u.Year() > 9999 {
			return param{}, fmt.Errorf("writing the time %s: the year is out of range: %w", u.Format(time.RFC3339Nano), dbimp.ErrInvalidValue)
		}
		return param{Value: new(u.Format("2006-01-02 15:04:05.999999")), Type: typeTimestamp}, nil
	case *apd.Decimal:
		return bindDecimal(v)
	case dbimp.Interval:
		return bindInterval(v)
	case []any, map[string]any:
		return param{}, fmt.Errorf("writing an argument of %T: the server refuses an ARRAY, a MAP and a STRUCT parameter, so bind the JSON text and parse it in the statement: %w", v, dbimp.ErrArguments)
	}
	return param{}, fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrArguments)
}

// formatFloat writes a float as text that the server reads as a DOUBLE: the
// digits that read back to v, and NaN, Infinity and -Infinity, as the server
// writes them (measured).
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

// formatLocal writes a date and a time with no zone, with at most six digits of
// fraction, which the server keeps (measured for a TIMESTAMP).
func formatLocal(v dbimp.LocalDateTime) string {
	t := time.Date(v.Date.Year, v.Date.Month, v.Date.Day, v.Time.Hour, v.Time.Minute, v.Time.Second, v.Time.Nanosecond, time.UTC)
	return t.Format("2006-01-02 15:04:05.999999")
}

// bindDecimal returns a decimal as a DECIMAL whose precision and scale are
// those of its digits. The server takes the precision from the value, and
// keeps 38 digits (measured).
func bindDecimal(d *apd.Decimal) (param, error) {
	if d == nil {
		return param{Type: typeNull}, nil
	}
	if d.Form != apd.Finite {
		return param{}, fmt.Errorf("writing %s as a DECIMAL: it is not a finite number: %w", d, dbimp.ErrInvalidValue)
	}
	digits := int(d.NumDigits())
	scale := 0
	precision := digits
	if d.Exponent >= 0 {
		precision += int(d.Exponent)
	} else {
		scale = int(-d.Exponent)
		precision = max(digits, scale)
	}
	if precision > maxPrecision {
		return param{}, fmt.Errorf("writing %s as a DECIMAL: it needs %d digits, and the most is %d: %w", d, precision, maxPrecision, dbimp.ErrInvalidValue)
	}
	return param{Value: new(d.Text('f')), Type: "DECIMAL(" + strconv.Itoa(precision) + "," + strconv.Itoa(scale) + ")"}, nil
}

// bindInterval returns an interval as the text that the server reads for
// INTERVAL YEAR TO MONTH or INTERVAL DAY TO SECOND (measured). Spark has no
// interval of months and days together, so one that has both fails with
// dbimp.ErrArguments.
func bindInterval(iv dbimp.Interval) (param, error) {
	const day = int64(24 * time.Hour)
	if iv.Months != 0 && (iv.Days != 0 || iv.Nanoseconds != 0) {
		return param{}, fmt.Errorf("writing the interval %s: Databricks has no interval of months and days together: %w", iv, dbimp.ErrArguments)
	}
	if iv.Months != 0 {
		m := int64(iv.Months)
		sign := ""
		if m < 0 {
			sign, m = "-", -m
		}
		return param{Value: new(sign + strconv.FormatInt(m/12, 10) + "-" + strconv.FormatInt(m%12, 10)), Type: typeYearToMonth}, nil
	}
	// A day to second interval is one exact length, so the days and the time
	// carry one sign.
	days := int64(iv.Days) + iv.Nanoseconds/day
	rest := iv.Nanoseconds % day
	switch {
	case days > 0 && rest < 0:
		days, rest = days-1, rest+day
	case days < 0 && rest > 0:
		days, rest = days+1, rest-day
	}
	sign := ""
	if days < 0 || rest < 0 {
		sign, days, rest = "-", -days, -rest
	}
	if rest%1000 != 0 {
		return param{}, fmt.Errorf("writing the interval %s: Databricks keeps microseconds: %w", iv, dbimp.ErrInvalidValue)
	}
	secs := rest / int64(time.Second)
	micros := rest % int64(time.Second) / 1000
	text := sign + strconv.FormatInt(days, 10) + " " +
		pad2(secs/3600) + ":" + pad2(secs/60%60) + ":" + pad2(secs%60)
	if micros != 0 {
		text += "." + strings.TrimRight(fmt.Sprintf("%06d", micros), "0")
	}
	return param{Value: new(text), Type: typeDayToSecond}, nil
}

// pad2 writes n with at least two digits.
func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
