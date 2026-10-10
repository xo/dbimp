package athena

import (
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// literals returns the arguments as the strings of ExecutionParameters. The
// server reads each string as an SQL expression and binds it to the next ? of
// the statement (recorded: "a parameter of the type integer", "a parameter that
// is a string literal"), so the driver writes each argument as a literal that
// the server parses (D34 and D192 item 4). The text of the statement keeps its
// ?, and the driver does not parse it.
func literals(args []driver.NamedValue) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]string, len(args))
	for i, arg := range args {
		if arg.Name != "" {
			return nil, fmt.Errorf("binding the argument %s: Athena has no named parameter: %w", arg.Name, dbimp.ErrArguments)
		}
		lit, err := literal(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %d: %w", arg.Ordinal, err)
		}
		out[i] = lit
	}
	return out, nil
}

// quote writes s as a string literal, with each quote doubled.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// literal writes v as a literal. A NULL is the bare NULL, whose type is unknown,
// and the server coerces it to the type of the expression (recorded: "a null
// parameter").
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return strings.ToUpper(strconv.FormatBool(v)), nil
	case int64:
		return integer(v), nil
	case float64:
		return float(v), nil
	case string:
		return quote(v), nil
	case []byte:
		return "X'" + hex.EncodeToString(v) + "'", nil
	case time.Time:
		return instant(v)
	case *apd.Decimal:
		return decimal(v)
	case dbimp.Date:
		if !v.IsValid() {
			return "", fmt.Errorf("writing the date %s: %w", v, dbimp.ErrInvalidValue)
		}
		return "DATE " + quote(v.String()), nil
	case dbimp.LocalTime:
		if !v.IsValid() {
			return "", fmt.Errorf("writing the time %s: %w", v, dbimp.ErrInvalidValue)
		}
		return "TIME " + quote(clock(v)), nil
	case dbimp.OffsetTime:
		if !v.IsValid() {
			return "", fmt.Errorf("writing the time %s: %w", v, dbimp.ErrInvalidValue)
		}
		return "TIME " + quote(clock(v.Time)+" "+offset(v.Offset)), nil
	case dbimp.LocalDateTime:
		if !v.IsValid() {
			return "", fmt.Errorf("writing the date and time %s: %w", v, dbimp.ErrInvalidValue)
		}
		return "TIMESTAMP " + quote(v.Date.String()+" "+clock(v.Time)), nil
	case dbimp.Interval:
		return interval(v)
	case uuid.UUID:
		return "UUID " + quote(v.String()), nil
	case netip.Addr:
		if !v.IsValid() {
			return "", fmt.Errorf("writing an address that is not valid: %w", dbimp.ErrInvalidValue)
		}
		return "IPADDRESS " + quote(v.String()), nil
	case []any:
		return list(v)
	case []string:
		return list(toAny(v))
	case []int64:
		return list(toAny(v))
	case []float64:
		return list(toAny(v))
	case []bool:
		return list(toAny(v))
	case map[string]any:
		return mapping(v)
	}
	return "", fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}

// toAny returns the elements of s as a []any.
func toAny[T any](s []T) []any {
	out := make([]any, len(s))
	for i, e := range s {
		out[i] = e
	}
	return out
}

// integer writes an integer. The smallest int64 cannot be written as a negative
// literal, because the server reads the minus sign as an operator on
// 9223372036854775808, which no integer holds, so it is a BIGINT of text.
func integer(n int64) string {
	if n == math.MinInt64 {
		return "BIGINT '-9223372036854775808'"
	}
	return strconv.FormatInt(n, 10)
}

// float writes a double, with every digit that it needs. NaN and the infinities
// have functions (recorded: "the type of nan()").
func float(f float64) string {
	switch {
	case math.IsNaN(f):
		return "nan()"
	case math.IsInf(f, 1):
		return "infinity()"
	case math.IsInf(f, -1):
		return "-infinity()"
	}
	return "DOUBLE '" + strconv.FormatFloat(f, 'g', -1, 64) + "'"
}

// decimal writes a decimal with every digit. The server reads up to 38 digits,
// and returns its own error for more.
func decimal(d *apd.Decimal) (string, error) {
	if d == nil {
		return "NULL", nil
	}
	if d.Form != apd.Finite {
		return "", fmt.Errorf("writing %s: a DECIMAL has no such value: %w", d, dbimp.ErrInvalidValue)
	}
	return "DECIMAL " + quote(d.Text('f')), nil
}

// offset writes an offset in seconds as +05:30. A literal holds hours and
// minutes only.
func offset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%02d:%02d", sign, sec/3600, sec%3600/60)
}

// clock writes a time of day, with the fraction of a second only when there is
// one, and with no trailing zero, as the recorded literal of a timestamp has no
// fraction (recorded: "a timestamp parameter").
func clock(t dbimp.LocalTime) string {
	s := fmt.Sprintf("%02d:%02d:%02d", t.Hour, t.Minute, t.Second)
	if t.Nanosecond == 0 {
		return s
	}
	return s + "." + strings.TrimRight(fmt.Sprintf("%09d", t.Nanosecond), "0")
}

// instant writes a time.Time as a timestamp with a time zone, with its offset,
// because a literal can name an offset and the server keeps the instant. An
// offset with seconds has no such form.
func instant(t time.Time) (string, error) {
	if y := t.Year(); y < 1 || y > 9999 {
		return "", fmt.Errorf("writing %s: the year is outside 1 to 9999: %w", t.Format(time.RFC3339Nano), dbimp.ErrInvalidValue)
	}
	_, off := t.Zone()
	if off%60 != 0 {
		return "", fmt.Errorf("writing %s: the offset has seconds: %w", t.Format(time.RFC3339Nano), dbimp.ErrInvalidValue)
	}
	return "TIMESTAMP " + quote(dbimp.DateOf(t).String()+" "+clock(dbimp.LocalTimeOf(t))+" "+offset(off)), nil
}

// interval writes an interval. The server has two intervals, one of years and
// months, and one of days and time, and neither holds both.
func interval(iv dbimp.Interval) (string, error) {
	if iv.Months != 0 && (iv.Days != 0 || iv.Nanoseconds != 0) {
		return "", fmt.Errorf("writing %s: an interval holds months, or days and time, and not both: %w", iv, dbimp.ErrInvalidValue)
	}
	if iv.Months != 0 {
		m := int64(iv.Months)
		sign := ""
		if m < 0 {
			sign, m = "- ", -m
		}
		return fmt.Sprintf("INTERVAL %s'%d-%d' YEAR TO MONTH", sign, m/12, m%12), nil
	}
	// The server counts milliseconds.
	if iv.Nanoseconds%1e6 != 0 {
		return "", fmt.Errorf("writing %s: the server holds an interval to the millisecond: %w", iv, dbimp.ErrInvalidValue)
	}
	ms := int64(iv.Days)*86400000 + iv.Nanoseconds/1e6
	sign := ""
	if ms < 0 {
		sign, ms = "- ", -ms
	}
	return fmt.Sprintf("INTERVAL %s'%d %02d:%02d:%02d.%03d' DAY TO SECOND", sign,
		ms/86400000, ms%86400000/3600000, ms%3600000/60000, ms%60000/1000, ms%1000), nil
}

// list writes the elements of a list as an ARRAY.
func list(elems []any) (string, error) {
	parts := make([]string, len(elems))
	for i, e := range elems {
		lit, err := literal(e)
		if err != nil {
			return "", err
		}
		parts[i] = lit
	}
	return "ARRAY[" + strings.Join(parts, ", ") + "]", nil
}

// mapping writes a map as MAP(ARRAY[keys], ARRAY[values]), with the keys in
// order, so that the same map is the same text.
func mapping(m map[string]any) (string, error) {
	keys := slices.Sorted(maps.Keys(m))
	vals := make([]any, len(keys))
	for i, k := range keys {
		vals[i] = m[k]
	}
	ks, err := list(toAny(keys))
	if err != nil {
		return "", err
	}
	vs, err := list(vals)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "MAP()", nil
	}
	return "MAP(" + ks + ", " + vs + ")", nil
}
