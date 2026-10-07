package solr

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// syntax is how Solr writes literals, quoted names and comments (D34 and
// D166). The SQL layer reads the lexical rules of MySQL, where a text in
// double quotes is a name, and a name can be quoted with backticks
// (measured).
var syntax = dbimp.Syntax{
	Quotes:        "'\"`",
	DashComments:  true,
	BlockComments: true,
}

// quote returns s as a literal, with each ' written twice (measured).
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// timeLayout is the form of a date in Solr, which a filter compares with the
// text of the date (measured).
const timeLayout = "2006-01-02T15:04:05.000Z"

// literal returns the value v as a literal of Solr SQL. The server binds no
// argument, so the driver writes each one into the statement (D166).
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case string:
		return quote(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "", fmt.Errorf("writing %v as a literal: the server has none: %w", v, dbimp.ErrNotSupported)
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case bool:
		if v {
			return "TRUE", nil
		}
		return "FALSE", nil
	case []byte:
		return quote(base64.StdEncoding.EncodeToString(v)), nil
	case time.Time:
		return quote(v.UTC().Format(timeLayout)), nil
	case dbimp.Date:
		return quote(v.In(time.UTC).Format(timeLayout)), nil
	case dbimp.LocalDateTime:
		return quote(v.In(time.UTC).Format(timeLayout)), nil
	case uuid.UUID:
		return quote(v.String()), nil
	case *apd.Decimal:
		if v == nil {
			return "NULL", nil
		}
		if v.Form != apd.Finite {
			return "", fmt.Errorf("writing %s as a literal: the server has none: %w", v.String(), dbimp.ErrNotSupported)
		}
		return v.Text('f'), nil
	}
	return "", fmt.Errorf("writing a %T as a literal: %w", v, dbimp.ErrNotSupported)
}
