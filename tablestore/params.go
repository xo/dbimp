package tablestore

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// syntax says how the SQL of Tablestore writes literals, quoted names and
// comments, so that the parser for placeholders skips them (D34). The dialect is
// the one of MySQL, whose version the server reports (recorded: "lead: the
// version variable"): a backslash escapes a character in a string, and a quote
// stands for itself when it is doubled.
var syntax = dbimp.Syntax{Quotes: "'\"`", Backslash: true, DashComments: true, HashComments: true, BlockComments: true}

// literal writes v as a literal of Tablestore SQL. The server binds no
// parameter (recorded: "a question mark parameter"), so the driver writes each
// argument into the text (D224). A NULL is the bare NULL.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return strings.ToUpper(strconv.FormatBool(v)), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "", fmt.Errorf("writing the number %v: a table stores no NaN and no Infinity: %w", v, dbimp.ErrNotSupported)
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case string:
		return quote(v), nil
	case []byte:
		return "X'" + hex.EncodeToString(v) + "'", nil
	// The server keeps seconds and drops the fraction (measured: a DATETIME that
	// was written with .123456 reads back without it), and it reads the text in its
	// own zone.
	case time.Time:
		return quote(v.In(serverZone).Format("2006-01-02 15:04:05")), nil
	}
	return "", fmt.Errorf("writing a literal of the type %T: %w", v, dbimp.ErrNotSupported)
}

// quote writes s as a string literal: a backslash and a quote are escaped, and so
// is the character NUL.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `''`, "\x00", `\0`)
	return "'" + r.Replace(s) + "'"
}
