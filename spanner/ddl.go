package spanner

import (
	"fmt"
	"strings"

	"github.com/xo/dbimp"
)

// splitStatements cuts text at each semicolon that is outside a literal, a
// quoted name and a comment (D198). It returns each statement without the
// white space around it. It drops a statement that holds nothing but white space
// and comments, so a trailing semicolon and a comment after the last one add no
// statement. It returns an error when the text ends inside a literal, a quoted
// name or a comment.
func splitStatements(text string) ([]string, error) {
	var out []string
	start := 0
	add := func(end int) {
		s := strings.TrimSpace(text[start:end])
		if skipLeading(s) != "" {
			out = append(out, s)
		}
	}
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == ';':
			add(i)
			i++
			start = i
		case c == '\'' || c == '"' || c == '`':
			end, err := skipLiteral(text, i)
			if err != nil {
				return nil, err
			}
			i = end
		case c == '#' || strings.HasPrefix(text[i:], "--"):
			if j := strings.IndexByte(text[i:], '\n'); j >= 0 {
				i += j + 1
			} else {
				i = len(text)
			}
		case strings.HasPrefix(text[i:], "/*"):
			j := strings.Index(text[i+2:], "*/")
			if j < 0 {
				return nil, fmt.Errorf("parsing a comment at offset %d: %w", i, dbimp.ErrUnterminated)
			}
			i += 2 + j + 2
		default:
			i++
		}
	}
	add(len(text))
	return out, nil
}

// skipLiteral returns the offset after the literal or the quoted name that
// starts at i. A quote written three times opens a literal that ends at three
// of them. A backslash escapes the next character, unless a raw prefix (r or R)
// comes before the quote. A quote that is written twice needs no rule, because
// the two literals that it makes sit side by side.
func skipLiteral(text string, i int) (int, error) {
	q := text[i]
	raw := i > 0 && (text[i-1] == 'r' || text[i-1] == 'R')
	closing := string(q)
	j := i + 1
	if q != '`' && strings.HasPrefix(text[i:], strings.Repeat(string(q), 3)) {
		closing = strings.Repeat(string(q), 3)
		j = i + 3
	}
	for ; j < len(text); j++ {
		switch {
		case text[j] == '\\' && !raw:
			j++
		case strings.HasPrefix(text[j:], closing):
			return j + len(closing), nil
		}
	}
	return 0, fmt.Errorf("parsing a quote at offset %d: %w", i, dbimp.ErrUnterminated)
}

// scan decides the kind of text, and for a DDL text it returns the statements
// of the batch. A text with several statements that are all DDL is a batch. A
// text that mixes DDL with other statements is an error. Several statements
// that are not DDL keep the kind of the first one, and the server refuses them,
// as D191 says. A text that cannot be split keeps the kind of its first word.
func scan(text string) (kind, []string, error) {
	k := classify(text)
	if !strings.Contains(text, ";") {
		return k, nil, nil
	}
	stmts, err := splitStatements(text)
	if err != nil || len(stmts) == 0 {
		// The server reports a text that does not parse, with its own position.
		return k, nil, nil //nolint:nilerr // See the comment above.
	}
	ddl := 0
	for _, s := range stmts {
		if classify(s) == kindDDL {
			ddl++
		}
	}
	switch {
	case ddl == len(stmts):
		return kindDDL, stmts, nil
	case ddl > 0:
		return kindDDL, nil, fmt.Errorf("running the statements: %d of %d are DDL, and DDL cannot run with other statements in one call: %w", ddl, len(stmts), dbimp.ErrNotSupported)
	}
	return classify(stmts[0]), nil, nil
}
