package hive

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

// syntax is how HiveQL and the SQL of Impala write literals and comments, so
// that the parser for placeholders skips them (D34). A backslash escapes a
// character inside a quote on both. Neither flavor has an @name placeholder,
// and an @ that the parser reads as one is an argument error.
var syntax = dbimp.Syntax{
	Quotes:        "'\"`",
	Backslash:     true,
	DashComments:  true,
	BlockComments: true,
}

// bind writes each argument into query as a literal, because the server binds
// none (D235).
func bind(query string, args []driver.NamedValue) (string, error) {
	return syntax.Bind(query, args, literal)
}

// literal writes v as a literal of the SQL of Hive and Impala.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float64:
		switch {
		case math.IsNaN(v):
			return "cast('NaN' as double)", nil
		case math.IsInf(v, 1):
			return "cast('Infinity' as double)", nil
		case math.IsInf(v, -1):
			return "cast('-Infinity' as double)", nil
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case string:
		return quote(v), nil
	case []byte:
		return "cast(unhex('" + hex.EncodeToString(v) + "') as binary)", nil
	case time.Time:
		return "cast('" + v.UTC().Format("2006-01-02 15:04:05.999999999") + "' as timestamp)", nil
	case dbimp.Date:
		return "cast('" + v.String() + "' as date)", nil
	case dbimp.LocalDateTime:
		return "cast('" + strings.Replace(v.String(), "T", " ", 1) + "' as timestamp)", nil
	case *apd.Decimal:
		if v == nil {
			return "NULL", nil
		}
		return v.String(), nil
	}
	return "", fmt.Errorf("writing an argument of type %T as a literal: %w", v, dbimp.ErrNotSupported)
}

// quote writes s as a string literal in single quotes, with a backslash before
// each backslash and each single quote.
func quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for i := range len(s) {
		switch s[i] {
		case '\\', '\'':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	b.WriteByte('\'')
	return b.String()
}

// quoteIdent writes name as a quoted identifier, in backticks.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
