package maxcompute

import (
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// syntax is how MaxCompute SQL writes literals, quoted names and comments, so
// that the parser for placeholders leaves them as they are (D34 and D220 item 2).
// A string has single or double quotes, and a backslash escapes inside it. A name
// has backticks.
var syntax = dbimp.Syntax{
	Quotes:        "'\"`",
	Backslash:     true,
	DashComments:  true,
	BlockComments: true,
}

// bindArgs writes each argument into the text of the statement as a literal,
// because the server binds no parameter (recorded: "a question mark parameter",
// "a named parameter"). A statement with no argument goes unchanged. zone gives
// the zone of the project, which a time needs, and the driver asks for it only
// then.
func bindArgs(query string, args []driver.NamedValue, zone func() (*time.Location, error)) (string, error) {
	if len(args) == 0 {
		return query, nil
	}
	text, err := syntax.Bind(query, args, func(v any) (string, error) {
		return literal(v, zone)
	})
	if err != nil {
		return "", fmt.Errorf("binding the arguments: %w", err)
	}
	return text, nil
}

// literal returns v as a literal of MaxCompute SQL.
func literal(v any, zone func() (*time.Location, error)) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return floatLiteral(v)
	case bool:
		return strconv.FormatBool(v), nil
	case string:
		return quote(v), nil
	case []byte:
		return "unhex('" + strings.ToUpper(hex.EncodeToString(v)) + "')", nil
	case time.Time:
		loc, err := zone()
		if err != nil {
			return "", err
		}
		return "TIMESTAMP'" + v.In(loc).Format("2006-01-02 15:04:05.000000000") + "'", nil
	case dbimp.Date:
		if !v.IsValid() || v.Year < 1 || v.Year > 9999 {
			return "", fmt.Errorf("writing the date %s: %w", v, dbimp.ErrInvalidValue)
		}
		return "DATE'" + v.String() + "'", nil
	case dbimp.LocalDateTime:
		if !v.IsValid() || v.Date.Year < 1 || v.Date.Year > 9999 {
			return "", fmt.Errorf("writing the date and time %s: %w", v, dbimp.ErrInvalidValue)
		}
		return "TIMESTAMP_NTZ'" + strings.Replace(v.String(), "T", " ", 1) + "'", nil
	case *apd.Decimal:
		if v == nil {
			return "NULL", nil
		}
		if v.Form != apd.Finite {
			return "", fmt.Errorf("writing the decimal %s: it is not a finite number: %w", v, dbimp.ErrInvalidValue)
		}
		return v.Text('f') + "BD", nil
	case dbimp.Interval:
		return intervalLiteral(v)
	}
	return "", fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrArguments)
}

// floatLiteral writes a float as a DOUBLE literal. The server has no literal for
// NaN or an infinity (recorded: the cast of the text NaN is a NULL), so the
// driver refuses them.
func floatLiteral(v float64) (string, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "", fmt.Errorf("writing the number %v: MaxCompute has no literal for it: %w", v, dbimp.ErrInvalidValue)
	}
	return strings.Replace(strconv.FormatFloat(v, 'g', -1, 64), "e+", "e", 1) + "D", nil
}

// quote writes s as a string with single quotes. A backslash and a quote have a
// backslash before them.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\\', '\'':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// intervalLiteral writes an interval of months, or an interval of days and a
// time. An interval that has both has no literal, because MaxCompute has two
// types, one for each.
func intervalLiteral(iv dbimp.Interval) (string, error) {
	switch {
	case iv.Months != 0 && (iv.Days != 0 || iv.Nanoseconds != 0):
		return "", fmt.Errorf("writing an interval of months and of days: MaxCompute has one type for each: %w", dbimp.ErrArguments)
	case iv.Months != 0:
		months := int64(iv.Months)
		sign := ""
		if months < 0 {
			sign, months = "-", -months
		}
		return fmt.Sprintf("INTERVAL '%s%d-%d' YEAR TO MONTH", sign, months/12, months%12), nil
	}
	sign, days, nanos := "", int64(iv.Days), iv.Nanoseconds
	if days < 0 || nanos < 0 {
		sign, days, nanos = "-", -days, -nanos
	}
	secs := nanos / int64(time.Second)
	return fmt.Sprintf("INTERVAL '%s%d %02d:%02d:%02d.%09d' DAY TO SECOND", sign, days, secs/3600, secs%3600/60, secs%60, nanos%int64(time.Second)), nil
}
