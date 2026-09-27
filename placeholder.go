package dbimp

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// Syntax says how a product writes literals, quoted identifiers and comments,
// so that the parser for placeholders can skip them (D34). The zero value
// knows no quote and no comment.
type Syntax struct {
	// Quotes holds each character that opens and closes a literal or a quoted
	// identifier, such as `'"` or "'\"`". Inside one, the character written
	// twice stands for itself.
	Quotes string
	// Backslash is true if a backslash inside a quote escapes the character
	// after it.
	Backslash bool
	// DashComments is true if -- starts a comment that ends at a new line.
	DashComments bool
	// HashComments is true if # starts a comment that ends at a new line.
	HashComments bool
	// BlockComments is true if /* starts a comment that ends at */.
	BlockComments bool
}

// Placeholder is one placeholder in a statement.
type Placeholder struct {
	// Offset is the byte offset of the placeholder in the statement.
	Offset int
	// Len is the length of the placeholder in bytes.
	Len int
	// Name is the name of an @name placeholder without the @, and "" for a ?.
	Name string
}

// Placeholders returns each ? and each @name in query, in order. It skips
// literals, quoted identifiers and comments. An @ that another @ follows, as
// in @@version, is not a placeholder. It returns an error if query ends
// inside a literal, a quoted identifier or a comment.
func (s Syntax) Placeholders(query string) ([]Placeholder, error) {
	var ps []Placeholder
	for i := 0; i < len(query); {
		c := query[i]
		switch {
		case strings.IndexByte(s.Quotes, c) >= 0:
			end, err := s.skipQuote(query, i)
			if err != nil {
				return nil, err
			}
			i = end
		case s.DashComments && strings.HasPrefix(query[i:], "--"),
			s.HashComments && c == '#':
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				return ps, nil
			}
			i += end + 1
		case s.BlockComments && strings.HasPrefix(query[i:], "/*"):
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("parsing a comment at offset %d: %w", i, ErrUnterminated)
			}
			i += 2 + end + 2
		case c == '?':
			ps = append(ps, Placeholder{Offset: i, Len: 1})
			i++
		case c == '@':
			n := nameLen(query[i+1:])
			switch {
			case strings.HasPrefix(query[i+1:], "@"):
				i += 2 + nameLen(query[i+2:])
			case n == 0:
				i++
			default:
				ps = append(ps, Placeholder{Offset: i, Len: 1 + n, Name: query[i+1 : i+1+n]})
				i += 1 + n
			}
		default:
			i++
		}
	}
	return ps, nil
}

// Bind writes each argument into query as a literal, for a server that binds
// no arguments (D34). A ? takes the next argument that has no name, in order,
// and an @name takes the argument with that name. lit writes one value as a
// literal of the product, and each driver supplies its own, because each
// product quotes in its own way. Bind returns an error if an argument is
// missing, or if an argument is left over.
func (s Syntax) Bind(query string, args []driver.NamedValue, lit func(v any) (string, error)) (string, error) {
	ps, err := s.Placeholders(query)
	if err != nil {
		return "", err
	}
	var positional []driver.NamedValue
	named := make(map[string]driver.NamedValue)
	for _, arg := range args {
		if arg.Name == "" {
			positional = append(positional, arg)
			continue
		}
		named[arg.Name] = arg
	}
	used := make(map[string]bool)
	var b strings.Builder
	last, next := 0, 0
	for _, p := range ps {
		var arg driver.NamedValue
		switch {
		case p.Name == "" && next < len(positional):
			arg = positional[next]
			next++
		case p.Name == "":
			return "", fmt.Errorf("binding ? at offset %d: no argument: %w", p.Offset, ErrArguments)
		default:
			a, ok := named[p.Name]
			if !ok {
				return "", fmt.Errorf("binding @%s: no argument: %w", p.Name, ErrArguments)
			}
			arg, used[p.Name] = a, true
		}
		text, err := lit(arg.Value)
		if err != nil {
			return "", fmt.Errorf("binding the argument at offset %d: %w", p.Offset, err)
		}
		b.WriteString(query[last:p.Offset])
		b.WriteString(text)
		last = p.Offset + p.Len
	}
	switch {
	case next < len(positional):
		return "", fmt.Errorf("binding: %d arguments for %d ? placeholders: %w", len(positional), next, ErrArguments)
	case len(used) < len(named):
		for name := range named {
			if !used[name] {
				return "", fmt.Errorf("binding: argument %q has no placeholder: %w", name, ErrArguments)
			}
		}
	}
	b.WriteString(query[last:])
	return b.String(), nil
}

// skipQuote returns the offset after the quote that starts at i.
func (s Syntax) skipQuote(query string, i int) (int, error) {
	q := query[i]
	for j := i + 1; j < len(query); j++ {
		switch c := query[j]; {
		case s.Backslash && c == '\\':
			j++
		case c == q && j+1 < len(query) && query[j+1] == q:
			j++
		case c == q:
			return j + 1, nil
		}
	}
	return 0, fmt.Errorf("parsing a quote at offset %d: %w", i, ErrUnterminated)
}

// nameLen returns the length of the name at the start of s. A name starts
// with a letter or an underscore, and goes on with letters, digits and
// underscores.
func nameLen(s string) int {
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '_', 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		case '0' <= c && c <= '9' && i > 0:
		default:
			return i
		}
	}
	return len(s)
}
