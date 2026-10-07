package opensearch

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// syntax says how OpenSearch SQL writes literals, quoted identifiers and
// comments, so that the parser for placeholders skips them (D34). A quote
// written twice stands for itself, and a backslash has no meaning in a
// literal (measured).
var syntax = dbimp.Syntax{Quotes: "'\"`", DashComments: true, BlockComments: true}

// bind writes each argument into query as a literal (D34 and D168). The
// server writes each value of its parameters into the text of the statement
// itself, and 2.19.6 changes a string with a quote or a backslash, so the
// driver sends no parameters (measured). A statement with no argument goes to
// the server as it is.
func bind(query string, args []driver.NamedValue) (string, error) {
	if len(args) == 0 {
		return query, nil
	}
	return syntax.Bind(query, args, literal)
}

// quote writes s as a string literal, with each quote doubled. The server
// reads a backslash as itself, and the characters outside ASCII as they are
// (measured).
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// literal writes v as a literal of OpenSearch SQL. A NULL is the bare NULL,
// whose type is undefined (measured). SQL has no binary literal and no decimal
// type, so those values fail.
func literal(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return strconv.FormatBool(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return float(v)
	case string:
		return quote(v), nil
	case time.Time:
		return instant(v)
	case dbimp.Date:
		if !v.IsValid() {
			return "", fmt.Errorf("writing the date %v: %w", v, dbimp.ErrInvalidValue)
		}
		return "DATE " + quote(v.String()), nil
	case dbimp.LocalTime:
		if !v.IsValid() {
			return "", fmt.Errorf("writing the time %v: %w", v, dbimp.ErrInvalidValue)
		}
		return "TIME " + quote(v.String()), nil
	case dbimp.LocalDateTime:
		if !v.IsValid() || v.Date.Year < 1 || v.Date.Year > 9999 {
			return "", fmt.Errorf("writing the timestamp %v: %w", v, dbimp.ErrInvalidValue)
		}
		return "TIMESTAMP " + quote(v.Date.String()+" "+v.Time.String()), nil
	case []byte:
		return "", fmt.Errorf("writing a []byte: OpenSearch SQL has no binary literal: %w", dbimp.ErrNotSupported)
	}
	return "", fmt.Errorf("writing an argument of %T: %w", v, dbimp.ErrNotSupported)
}

// float writes f as a number with a fraction or an exponent, so that the
// server reads a double and not an integer. JSON has no NaN and no infinity,
// and neither does SQL.
func float(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("writing %v: SQL has no literal for it: %w", f, dbimp.ErrInvalidValue)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s, nil
}

// instant writes t as a TIMESTAMP literal in UTC, with every digit of the
// second (measured).
func instant(t time.Time) (string, error) {
	t = t.UTC()
	if y := t.Year(); y < 1 || y > 9999 {
		return "", fmt.Errorf("writing the time %v: the year %d is outside 1 to 9999: %w", t, y, dbimp.ErrInvalidValue)
	}
	return "TIMESTAMP " + quote(t.Format("2006-01-02 15:04:05.999999999")), nil
}

// plainSelect reports whether query is a plain SELECT, to which the driver
// sends a page size (D168). A plain SELECT starts with SELECT, has a FROM, and
// has none of the words that send the statement to the legacy engine when it
// has a page size. The legacy engine gives a cursor of another form, gives 200
// groups for a GROUP BY and no cursor, and names the type double for a count
// and for a keyword in a GROUP BY (measured). The words are LIMIT, GROUP,
// DISTINCT, JOIN, UNION, MINUS, INTERSECT, HAVING, OVER and a second SELECT,
// and a call of COUNT, SUM, AVG, MIN, MAX or NESTED. The scan skips literals,
// quoted identifiers and comments. A statement in doubt is not plain, and then
// the server cuts its result at the size limit, which the documentation names.
func plainSelect(query string) bool {
	var first, from bool
	n := 0
	for w := range words(query) {
		n++
		up := strings.ToUpper(w.text)
		switch {
		case n == 1:
			if up != "SELECT" {
				return false
			}
			first = true
		case up == "SELECT":
			return false
		case up == "FROM":
			from = true
		case legacyWords[up]:
			return false
		case legacyCalls[up] && w.call:
			return false
		}
	}
	return first && from
}

// legacyWords are the words of a statement that the server runs in the legacy
// engine when it has a page size.
var legacyWords = map[string]bool{
	"LIMIT": true, "GROUP": true, "DISTINCT": true, "JOIN": true, "UNION": true,
	"MINUS": true, "INTERSECT": true, "HAVING": true, "OVER": true,
}

// legacyCalls are the functions whose call sends a statement to the legacy
// engine when it has a page size.
var legacyCalls = map[string]bool{
	"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true, "NESTED": true,
}

// word is a word of a statement.
type word struct {
	text string
	// call is true if a ( follows the word, with only white space between.
	call bool
}

// words returns the words of query in order: each run of letters, digits and
// underscores that starts with a letter or an underscore. It skips literals,
// quoted identifiers and comments, and reads to the end of an unterminated one.
func words(query string) func(yield func(word) bool) {
	return func(yield func(word) bool) {
		for i := 0; i < len(query); {
			c := query[i]
			switch {
			case c == '\'' || c == '"' || c == '`':
				i = skipQuoted(query, i)
			case c == '-' && strings.HasPrefix(query[i:], "--"):
				end := strings.IndexByte(query[i:], '\n')
				if end < 0 {
					return
				}
				i += end + 1
			case c == '/' && strings.HasPrefix(query[i:], "/*"):
				end := strings.Index(query[i+2:], "*/")
				if end < 0 {
					return
				}
				i += 2 + end + 2
			case isWordStart(c):
				j := i + 1
				for j < len(query) && (isWordStart(query[j]) || '0' <= query[j] && query[j] <= '9') {
					j++
				}
				k := j
				for k < len(query) && (query[k] == ' ' || query[k] == '\t' || query[k] == '\n' || query[k] == '\r') {
					k++
				}
				if !yield(word{text: query[i:j], call: k < len(query) && query[k] == '('}) {
					return
				}
				i = j
			case '0' <= c && c <= '9':
				// A number such as 1e5 is no word.
				for i < len(query) && (isWordStart(query[i]) || '0' <= query[i] && query[i] <= '9' || query[i] == '.') {
					i++
				}
			default:
				i++
			}
		}
	}
}

// isWordStart reports whether c starts a word.
func isWordStart(c byte) bool {
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// skipQuoted returns the offset after the quote that starts at i, where a quote
// written twice stands for itself. It returns len(query) for an unterminated
// quote.
func skipQuoted(query string, i int) int {
	q := query[i]
	for j := i + 1; j < len(query); j++ {
		if query[j] != q {
			continue
		}
		if j+1 < len(query) && query[j+1] == q {
			j++
			continue
		}
		return j + 1
	}
	return len(query)
}
