package trino

import (
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// escape writes s as a URL does, with %20 for a space and %2B for a plus sign,
// as the headers of the session and of the prepared statements hold their
// values (measured). Presto reads a plus sign as a space.
func escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// literals returns the arguments as the literals of EXECUTE name USING. Neither
// server binds a parameter that the client sends apart from the text, so the
// driver writes each argument as a literal that the server parses (D34 and
// D175). Presto holds a time to the millisecond only (measured), so it takes
// no argument with a finer fraction.
func literals(args []driver.NamedValue, presto bool) (string, error) {
	out := make([]string, len(args))
	w := writer{presto: presto}
	for i, arg := range args {
		if arg.Name != "" {
			return "", fmt.Errorf("binding the argument %s: the servers have no named parameter: %w", arg.Name, dbimp.ErrArguments)
		}
		lit, err := w.literal(arg.Value)
		if err != nil {
			return "", fmt.Errorf("binding the argument %d: %w", arg.Ordinal, err)
		}
		out[i] = lit
	}
	return strings.Join(out, ", "), nil
}

// writer writes the literals of one flavor.
type writer struct {
	presto bool
}

// quote writes s as a string literal, with each quote doubled.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// literal writes v as a literal. A NULL is the bare NULL, whose type is
// unknown, and the server coerces it to the type of the column or the
// expression (measured).
func (w writer) literal(v any) (string, error) {
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
		return w.instant(v)
	case *apd.Decimal:
		return decimal(v)
	case dbimp.Date:
		return "DATE " + quote(v.String()), nil
	case dbimp.LocalTime:
		s, err := w.clock(v)
		return "TIME " + quote(s), err
	case dbimp.OffsetTime:
		s, err := w.clock(v.Time)
		if err != nil {
			return "", err
		}
		return "TIME " + quote(s+" "+offset(v.Offset)), nil
	case dbimp.LocalDateTime:
		s, err := w.clock(v.Time)
		return "TIMESTAMP " + quote(v.Date.String()+" "+s), err
	case dbimp.Interval:
		return w.interval(v)
	case uuid.UUID:
		return "UUID " + quote(v.String()), nil
	case []any:
		return w.list(v)
	case []string:
		return w.list(toAny(v))
	case []int64:
		return w.list(toAny(v))
	case []float64:
		return w.list(toAny(v))
	case []bool:
		return w.list(toAny(v))
	case map[string]any:
		return w.mapping(v)
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

// integer writes an integer. The smallest int64 cannot be written as a
// negative literal, because the server reads the minus sign as an operator on
// 9223372036854775808, which no integer holds, so it is a BIGINT of text.
func integer(n int64) string {
	if n == math.MinInt64 {
		return "BIGINT '-9223372036854775808'"
	}
	return strconv.FormatInt(n, 10)
}

// float writes a double, with every digit that it needs. NaN and the
// infinities have functions.
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

// clock writes a time of day. Presto holds three digits of fraction, and
// refuses a literal with more (measured), so it gets three, and a finer one is an
// error for it, as a cut would change the value without a sign (D175).
func (w writer) clock(t dbimp.LocalTime) (string, error) {
	if !w.presto {
		return fmt.Sprintf("%02d:%02d:%02d.%09d", t.Hour, t.Minute, t.Second, t.Nanosecond), nil
	}
	if t.Nanosecond%1e6 != 0 {
		return "", fmt.Errorf("writing %s: Presto holds a time to the millisecond: %w", t, dbimp.ErrInvalidValue)
	}
	return fmt.Sprintf("%02d:%02d:%02d.%03d", t.Hour, t.Minute, t.Second, t.Nanosecond/1e6), nil
}

// instant writes a time.Time as a timestamp with a time zone, with its
// offset, because a literal can name an offset and the server keeps the
// instant. An offset with seconds has no such form.
func (w writer) instant(t time.Time) (string, error) {
	if y := t.Year(); y < 1 || y > 9999 {
		return "", fmt.Errorf("writing %s: the year is outside 1 to 9999: %w", t.Format(time.RFC3339Nano), dbimp.ErrInvalidValue)
	}
	_, off := t.Zone()
	if off%60 != 0 {
		return "", fmt.Errorf("writing %s: the offset has seconds: %w", t.Format(time.RFC3339Nano), dbimp.ErrInvalidValue)
	}
	s, err := w.clock(dbimp.LocalTimeOf(t))
	if err != nil {
		return "", err
	}
	return "TIMESTAMP " + quote(dbimp.DateOf(t).String()+" "+s+" "+offset(off)), nil
}

// interval writes an interval. The servers have two intervals, one of years
// and months, and one of days and time, and neither holds both.
func (w writer) interval(iv dbimp.Interval) (string, error) {
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
	// The servers count milliseconds.
	if iv.Nanoseconds%1e6 != 0 {
		return "", fmt.Errorf("writing %s: the servers hold an interval to the millisecond: %w", iv, dbimp.ErrInvalidValue)
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
func (w writer) list(elems []any) (string, error) {
	parts := make([]string, len(elems))
	for i, e := range elems {
		lit, err := w.literal(e)
		if err != nil {
			return "", err
		}
		parts[i] = lit
	}
	return "ARRAY[" + strings.Join(parts, ", ") + "]", nil
}

// mapping writes a map as MAP(ARRAY[keys], ARRAY[values]), with the keys in
// order, so that the same map is the same text.
func (w writer) mapping(m map[string]any) (string, error) {
	keys := slices.Sorted(maps.Keys(m))
	vals := make([]any, len(keys))
	for i, k := range keys {
		vals[i] = m[k]
	}
	ks, err := w.list(toAny(keys))
	if err != nil {
		return "", err
	}
	vs, err := w.list(vals)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "MAP()", nil
	}
	return "MAP(" + ks + ", " + vs + ")", nil
}
