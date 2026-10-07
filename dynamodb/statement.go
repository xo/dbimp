package dynamodb

import (
	"strings"

	"github.com/xo/dbimp"
)

// syntax says how PartiQL writes literals, quoted names and comments, so that
// the parser for placeholders skips them (D34 and D169). Single quotes write
// a literal, and double quotes write a name (recorded).
var syntax = dbimp.Syntax{Quotes: `'"`, DashComments: true, BlockComments: true}

// shape is the shape of the result of a statement. The server sends no
// columns, and an item holds only the attributes that it has (recorded), so
// the driver reads the columns from the text of the statement (D163).
type shape struct {
	// columns are the columns of the result, in the order of the statement.
	// They are one column named "" when whole is true, and none for a
	// statement that returns no item.
	columns []string
	// whole is true when each item is the one value of the only column, as a
	// map[string]any, which SELECT * and RETURNING give (D18 and D163).
	whole bool
}

// token kinds of the scanner of a statement.
const (
	kindWord   = 'w' // a word or a number
	kindName   = 'n' // a name in double quotes
	kindString = 's' // a literal in single quotes
	kindPunct  = 'p' // one other character
)

// token is one token of a statement.
type token struct {
	kind byte
	// text is the word, the name without its quotes, the literal without its
	// quotes, or the character.
	text string
	// start and end are the offsets of the token in the statement.
	start, end int
}

// scan splits query into tokens. It skips white space and comments. The text
// of a statement that the server refuses can end inside a quote, and scan
// then ends the token at the end of the text, because the server reports
// that error.
func scan(query string) []token {
	var toks []token
	for i := 0; i < len(query); {
		c := query[i]
		switch {
		case c == ' ', c == '\t', c == '\n', c == '\r':
			i++
		case strings.HasPrefix(query[i:], "--"):
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				return toks
			}
			i += end + 1
		case strings.HasPrefix(query[i:], "/*"):
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				return toks
			}
			i += 2 + end + 2
		case c == '\'' || c == '"':
			text, end := quoted(query, i)
			kind := byte(kindString)
			if c == '"' {
				kind = kindName
			}
			toks = append(toks, token{kind: kind, text: text, start: i, end: end})
			i = end
		case strings.HasPrefix(query[i:], "<<") || strings.HasPrefix(query[i:], ">>"):
			toks = append(toks, token{kind: kindPunct, text: query[i : i+2], start: i, end: i + 2})
			i += 2
		case isWordByte(c):
			end := i
			for end < len(query) && isWordByte(query[end]) {
				end++
			}
			toks = append(toks, token{kind: kindWord, text: query[i:end], start: i, end: end})
			i = end
		default:
			toks = append(toks, token{kind: kindPunct, text: query[i : i+1], start: i, end: i + 1})
			i++
		}
	}
	return toks
}

// quoted reads the quoted text that starts at i, where the quote written
// twice stands for itself. It returns the text and the offset after the
// closing quote.
func quoted(query string, i int) (string, int) {
	q := query[i]
	var b strings.Builder
	for j := i + 1; j < len(query); j++ {
		switch {
		case query[j] == q && j+1 < len(query) && query[j+1] == q:
			b.WriteByte(q)
			j++
		case query[j] == q:
			return b.String(), j + 1
		default:
			b.WriteByte(query[j])
		}
	}
	return b.String(), len(query)
}

// opens reports whether t opens a bracket: a parenthesis, a square bracket, a
// brace, or << for a set.
func opens(t token) bool {
	return t.kind == kindPunct && (t.text == "(" || t.text == "[" || t.text == "{" || t.text == "<<")
}

// closes reports whether t closes a bracket.
func closes(t token) bool {
	return t.kind == kindPunct && (t.text == ")" || t.text == "]" || t.text == "}" || t.text == ">>")
}

// isWordByte reports whether c can be in a word or a number.
func isWordByte(c byte) bool {
	return c == '_' || c >= 0x80 || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// shapeOf returns the shape of the result of query (D163). A SELECT with a
// list of paths gives one column for each, named by the last part of its
// path, as the server names it: m.k[1] gives k[1] (recorded: "a projection
// of nested paths"). SELECT * gives one column that holds each item. An
// INSERT, an UPDATE or a DELETE gives no item, unless it has RETURNING,
// which gives the item as one column. Any other statement gives one column.
func shapeOf(query string) shape {
	toks := scan(query)
	whole := shape{columns: []string{""}, whole: true}
	if len(toks) == 0 || toks[0].kind != kindWord {
		return whole
	}
	switch strings.ToUpper(toks[0].text) {
	case "SELECT":
		return selectShape(query, toks[1:])
	case "INSERT", "UPDATE", "DELETE":
		if hasWord(toks, "RETURNING") {
			return whole
		}
		return shape{columns: []string{}}
	}
	return whole
}

// hasWord reports whether toks hold the word w outside any bracket.
func hasWord(toks []token, w string) bool {
	depth := 0
	for _, t := range toks {
		switch {
		case opens(t):
			depth++
		case closes(t):
			depth--
		case depth == 0 && t.kind == kindWord && strings.EqualFold(t.text, w):
			return true
		}
	}
	return false
}

// selectShape returns the shape of a SELECT whose tokens after the keyword
// are toks.
func selectShape(query string, toks []token) shape {
	// The list ends at FROM, which is not inside a bracket.
	depth := 0
	end := len(toks)
	for i, t := range toks {
		switch {
		case opens(t):
			depth++
		case closes(t):
			depth--
		case depth == 0 && t.kind == kindWord && strings.EqualFold(t.text, "FROM"):
			end = i
		}
		if end != len(toks) {
			break
		}
	}
	toks = toks[:end]
	if len(toks) == 1 && toks[0].kind == kindPunct && toks[0].text == "*" {
		return shape{columns: []string{""}, whole: true}
	}
	var (
		columns []string
		item    []token
	)
	flush := func() {
		if len(item) > 0 {
			columns = append(columns, columnName(query, item))
		}
		item = nil
	}
	depth = 0
	for _, t := range toks {
		switch {
		case opens(t):
			depth++
		case closes(t):
			depth--
		case depth == 0 && t.kind == kindPunct && t.text == ",":
			flush()
			continue
		}
		item = append(item, t)
	}
	flush()
	if columns == nil {
		columns = []string{}
	}
	return shape{columns: columns}
}

// columnName returns the name of the column of one item of a list of paths,
// which is the last part of the path (recorded). A name in double quotes is
// the name without its quotes.
func columnName(query string, item []token) string {
	last := -1
	depth := 0
	for i, t := range item {
		switch {
		case t.kind == kindPunct && t.text == "[":
			depth++
		case t.kind == kindPunct && t.text == "]":
			depth--
		case depth == 0 && t.kind == kindPunct && t.text == ".":
			last = i
		}
	}
	part := item[last+1:]
	if len(part) == 1 && (part[0].kind == kindName || part[0].kind == kindWord) {
		return part[0].text
	}
	if len(part) == 0 {
		return strings.TrimSpace(query[item[0].start:item[len(item)-1].end])
	}
	return query[part[0].start:part[len(part)-1].end]
}

// countPlaceholders returns the count of the ? in query, which the server
// binds with its Parameters. It returns an error that wraps
// dbimp.ErrUnterminated for a statement that ends inside a literal, a name or
// a comment.
func countPlaceholders(query string) (int, error) {
	ps, err := syntax.Placeholders(query)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range ps {
		if p.Name == "" {
			n++
		}
	}
	return n, nil
}
