package drill

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

// syntax is how Drill writes literals, quoted names and comments, so that the
// parser for placeholders skips them (D34). A single quote, a double quote
// and a backtick each open a quote. A backslash escapes nothing in a quote
// (measured). A comment is -- to the end of the line, or /* to */.
var syntax = dbimp.Syntax{
	Quotes:        "'\"`",
	DashComments:  true,
	BlockComments: true,
}

// bindQuery writes each argument into the statement as a literal, because
// the server binds no argument (D34 and D165). A statement with no argument
// goes as it is.
func bindQuery(query string, args []driver.NamedValue) (string, error) {
	if len(args) == 0 {
		return query, nil
	}
	return syntax.Bind(query, args, literal)
}

// quote writes s as a string literal, with each quote doubled.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// literal writes v as a literal. A NULL is the bare NULL, whose type is INT
// (measured), which the server coerces to the type of the column in a
// comparison.
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
		return binary(v), nil
	case time.Time:
		return instant(v)
	case *apd.Decimal:
		return decimal(v)
	case dbimp.Date:
		if y := v.Year; y < 1 || y > 9999 {
			return "", fmt.Errorf("writing %s: the year is outside 1 to 9999: %w", v, dbimp.ErrInvalidValue)
		}
		return "DATE " + quote(v.String()), nil
	case dbimp.LocalTime:
		s, err := clock(v)
		return "TIME " + quote(s), err
	case dbimp.LocalDateTime:
		return localDateTime(v)
	case dbimp.Interval:
		return interval(v)
	case dbimp.OffsetTime:
		return "", fmt.Errorf("writing %s: Drill has no time with an offset: %w", v, dbimp.ErrNotSupported)
	}
	return "", fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}

// integer writes an integer. The smallest int64 is a BIGINT of text, because
// the server reads the minus sign as an operator on 9223372036854775808,
// which no BIGINT holds. A number inside the range of an INT is an INT, as
// the server reads a literal, and the others are BIGINT (measured).
func integer(n int64) string {
	switch {
	case n == math.MinInt64:
		return "CAST('-9223372036854775808' AS BIGINT)"
	case n < math.MinInt32 || n > math.MaxInt32:
		return "CAST(" + strconv.FormatInt(n, 10) + " AS BIGINT)"
	}
	return strconv.FormatInt(n, 10)
}

// float writes a double, with every digit that it needs. The server reads
// the text of NaN and of the infinities in a cast (measured).
func float(f float64) string {
	switch {
	case math.IsNaN(f):
		return "CAST('NaN' AS DOUBLE)"
	case math.IsInf(f, 1):
		return "CAST('Infinity' AS DOUBLE)"
	case math.IsInf(f, -1):
		return "CAST('-Infinity' AS DOUBLE)"
	}
	return "CAST('" + strconv.FormatFloat(f, 'g', -1, 64) + "' AS DOUBLE)"
}

// decimal writes a decimal with every digit, as a cast of its text. The type
// has the precision and the scale of the digits, and the server holds up to
// 38 digits.
func decimal(d *apd.Decimal) (string, error) {
	if d == nil {
		return "NULL", nil
	}
	if d.Form != apd.Finite {
		return "", fmt.Errorf("writing %s: a DECIMAL has no such value: %w", d, dbimp.ErrInvalidValue)
	}
	text := d.Text('f')
	scale := max(-int(d.Exponent), 0)
	digits := max(int(d.NumDigits())+max(int(d.Exponent), 0), scale, 1)
	if digits > 38 {
		return "", fmt.Errorf("writing %s: the server holds 38 digits: %w", d, dbimp.ErrInvalidValue)
	}
	return fmt.Sprintf("CAST(%s AS DECIMAL(%d, %d))", quote(text), digits, scale), nil
}

// binary writes bytes with binary_string, which reads the escape \xNN of
// each byte (measured).
func binary(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		fmt.Fprintf(&s, `\x%02x`, c)
	}
	return "binary_string(" + quote(s.String()) + ")"
}

// clock writes a time of day. The server holds a time to the millisecond
// (measured), so a finer one is an error, because a cut would change the
// value with no sign.
func clock(t dbimp.LocalTime) (string, error) {
	if t.Nanosecond%1e6 != 0 {
		return "", fmt.Errorf("writing %s: Drill holds a time to the millisecond: %w", t, dbimp.ErrInvalidValue)
	}
	return fmt.Sprintf("%02d:%02d:%02d.%03d", t.Hour, t.Minute, t.Second, t.Nanosecond/1e6), nil
}

// localDateTime writes a date and a time of day as a TIMESTAMP, which has no
// zone.
func localDateTime(v dbimp.LocalDateTime) (string, error) {
	if y := v.Date.Year; y < 1 || y > 9999 {
		return "", fmt.Errorf("writing %s: the year is outside 1 to 9999: %w", v, dbimp.ErrInvalidValue)
	}
	s, err := clock(v.Time)
	if err != nil {
		return "", err
	}
	return "TIMESTAMP " + quote(v.Date.String()+" "+s), nil
}

// instant writes a time.Time as the TIMESTAMP of its time in UTC, because
// Drill has no zone and reads a TIMESTAMP as UTC (measured).
func instant(t time.Time) (string, error) {
	return localDateTime(dbimp.LocalDateTimeOf(t.UTC()))
}

// interval writes an interval. The server has two intervals, one of years
// and months, and one of days and time, and neither holds both.
func interval(iv dbimp.Interval) (string, error) {
	if iv.Months != 0 && (iv.Days != 0 || iv.Nanoseconds != 0) {
		return "", fmt.Errorf("writing %s: an interval holds months, or days and time, and not both: %w", iv, dbimp.ErrInvalidValue)
	}
	if iv.Months != 0 {
		m := int64(iv.Months)
		sign := ""
		if m < 0 {
			sign, m = "-", -m
		}
		return fmt.Sprintf("INTERVAL '%s%d-%d' YEAR TO MONTH", sign, m/12, m%12), nil
	}
	// The server counts milliseconds.
	if iv.Nanoseconds%1e6 != 0 {
		return "", fmt.Errorf("writing %s: Drill holds an interval to the millisecond: %w", iv, dbimp.ErrInvalidValue)
	}
	ms := int64(iv.Days)*86400000 + iv.Nanoseconds/1e6
	sign := ""
	if ms < 0 {
		sign, ms = "-", -ms
	}
	return fmt.Sprintf("INTERVAL '%s%d %02d:%02d:%02d.%03d' DAY TO SECOND", sign,
		ms/86400000, ms%86400000/3600000, ms%3600000/60000, ms%60000/1000, ms%1000), nil
}
