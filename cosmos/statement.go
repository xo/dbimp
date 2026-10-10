package cosmos

import "strings"

// stripSemicolon returns the statement with one final semicolon removed. The
// server answers a trailing semicolon with HTTP 400 and the code SC1010, on
// the hosted service and on the emulator (recorded: "a trailing semicolon"),
// so the driver strips it, as D190 says. A semicolon that is not the last
// token stays, and the server answers it (recorded: "two statements in one
// request"). The scan reads strings and comments, because a semicolon in a
// string or in a comment is no token. A string is in single or double
// quotes, with a backslash that escapes the next character. A comment starts
// with two hyphens and runs to the end of the line (recorded: "a line
// comment"). A comment of the form /* */ is no comment to the server
// (recorded: "a block comment"), so the scan does not read one.
func stripSemicolon(query string) string {
	last := -1
	var quote byte
	for i := 0; i < len(query); i++ {
		ch := query[i]
		switch {
		case quote != 0:
			switch ch {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			last = i
		case ch == '\'' || ch == '"':
			quote = ch
			last = i
		case ch == '-' && strings.HasPrefix(query[i:], "--"):
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				i = len(query)
			} else {
				i += end
			}
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
		default:
			last = i
		}
	}
	if last >= 0 && last < len(query) && query[last] == ';' && quote == 0 {
		return query[:last] + query[last+1:]
	}
	return query
}
