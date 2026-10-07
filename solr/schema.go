package solr

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// schema is what a connection knows of the columns of one table (D166). The
// SQL types come from metadata.COLUMNS, and the classes of the field types
// from the luke handler, because the answer of a statement names neither.
type schema struct {
	// sql maps the name of a column to its SQL type.
	sql map[string]string
	// class maps the name of a field to the class of its field type.
	class map[string]string
	// dynamic holds the patterns of the dynamic fields, and the class of
	// each.
	dynamic []dynamicField
}

// dynamicField is a dynamic field of the schema, such as *_s.
type dynamicField struct {
	pattern string
	class   string
}

// column returns the type of the column named name, and whether the schema
// knows it. The server matches names without regard to case (measured), so
// the lookup does too, after an exact match.
func (s *schema) column(name string) (column, bool) {
	if s == nil {
		return column{}, false
	}
	key, ok := s.sql[name]
	if !ok {
		for k, v := range s.sql {
			if strings.EqualFold(k, name) {
				key, name, ok = v, k, true
				break
			}
		}
	}
	if !ok {
		return column{}, false
	}
	return column{sql: key, class: s.classOf(name)}, true
}

// classOf returns the class of the field type of the field named name, from
// its field or from the dynamic field that it matches, or "" for neither.
func (s *schema) classOf(name string) string {
	if class := s.class[name]; class != "" {
		return class
	}
	for k, v := range s.class {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	for _, d := range s.dynamic {
		if d.matches(name) {
			return d.class
		}
	}
	return ""
}

// matches reports whether name is a field of the dynamic field d. A pattern
// has one * at its start or at its end.
func (d dynamicField) matches(name string) bool {
	switch {
	case strings.HasPrefix(d.pattern, "*"):
		return strings.HasSuffix(name, d.pattern[1:])
	case strings.HasSuffix(d.pattern, "*"):
		return strings.HasPrefix(name, d.pattern[:len(d.pattern)-1])
	}
	return name == d.pattern
}

// luke is the part of the answer of the luke handler that the driver reads.
type luke struct {
	Schema struct {
		Fields map[string]struct {
			Type string `json:"type"`
		} `json:"fields"`
		DynamicFields map[string]struct {
			Type string `json:"type"`
		} `json:"dynamicFields"`
		Types map[string]struct {
			ClassName string `json:"className"`
		} `json:"types"`
	} `json:"schema"`
}

// simpleClass returns the name of a Java class without its package.
func simpleClass(name string) string {
	if _, after, ok := strings.CutLast(name, "."); ok {
		return after
	}
	return name
}

// readLuke reads the answer of the luke handler of the table.
func (c *conn) readLuke(ctx context.Context, table string) (map[string]string, []dynamicField, error) {
	res, err := c.c.luke(ctx, table)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	var l luke
	if err := json.UnmarshalRead(res.Body, &l, jsontext.AllowDuplicateNames(true)); err != nil {
		return nil, nil, fmt.Errorf("reading the schema of %s: %w", table, err)
	}
	class := func(typ string) string {
		return simpleClass(l.Schema.Types[typ].ClassName)
	}
	fields := make(map[string]string, len(l.Schema.Fields))
	for name, f := range l.Schema.Fields {
		fields[name] = class(f.Type)
	}
	var dynamic []dynamicField
	for pattern, f := range l.Schema.DynamicFields {
		dynamic = append(dynamic, dynamicField{pattern: pattern, class: class(f.Type)})
	}
	return fields, dynamic, nil
}

// refused reports whether err is the answer of a server that does not have
// the table, or does not let the user read it. The statement that named the
// table ran, so a table that the schema calls missing is a name that is not a
// collection, such as an alias of a subquery, and its columns read by their
// tokens.
func refused(err error) bool {
	e, ok := errors.AsType[*Error](err)
	if !ok {
		return false
	}
	switch e.HTTPStatus {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	}
	return false
}

// readSchema reads the schema of the table, from metadata.COLUMNS and from
// the luke handler. A part that the server refuses is empty, and any other
// error reaches the caller.
func (c *conn) readSchema(ctx context.Context, o options, table string) (*schema, error) {
	s := &schema{sql: map[string]string{}}
	stmt := "SELECT columnName, dataType FROM metadata.COLUMNS WHERE tableName = " + quote(table)
	r, err := c.run(ctx, o, stmt, false)
	switch {
	case refused(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("reading the columns of %s: %w", table, err)
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("reading the columns of %s: %w", table, err)
		}
		name, ok := r.cur[0].(string)
		if !ok || len(r.cur) < 2 {
			continue
		}
		if code, ok := r.cur[1].(int64); ok {
			s.sql[name] = sqlTypes[code]
		}
	}
	if len(s.sql) == 0 {
		return nil, nil
	}
	s.class, s.dynamic, err = c.readLuke(ctx, table)
	if err != nil && !refused(err) {
		return nil, err
	}
	return s, nil
}

// tables returns the names of the tables that a statement reads, which follow
// FROM and JOIN, in order. It skips literals, comments, subqueries and the
// names of a schema, such as metadata.COLUMNS, which are no collection.
func tables(stmt string) []string {
	toks := tokens(stmt)
	var out []string
	for i := range toks {
		if toks[i].quoted || (!strings.EqualFold(toks[i].text, "FROM") && !strings.EqualFold(toks[i].text, "JOIN")) {
			continue
		}
		for j := i + 1; j < len(toks); {
			name, next, ok := tableName(toks, j)
			if ok {
				out = append(out, name)
			}
			j = next
			// An alias, with or without AS.
			if j < len(toks) && strings.EqualFold(toks[j].text, "AS") && !toks[j].quoted {
				j++
			}
			if j < len(toks) && !isKeyword(toks[j]) && toks[j].text != "," && toks[j].text != ")" {
				j++
			}
			if j < len(toks) && toks[j].text == "," && !toks[j].quoted {
				j++
				continue
			}
			break
		}
	}
	return out
}

// token is one token of a statement.
type token struct {
	text   string
	quoted bool
}

// tableName reads a table at toks[i]. It returns the name, the index after
// it, and false for a subquery or a qualified name.
func tableName(toks []token, i int) (string, int, bool) {
	if i >= len(toks) {
		return "", i, false
	}
	t := toks[i]
	if t.text == "(" && !t.quoted {
		return "", i + 1, false
	}
	if !t.quoted && (isKeyword(t) || len(t.text) == 1 && !isWordByte(t.text[0])) {
		return "", i, false
	}
	if i+1 < len(toks) && toks[i+1].text == "." && !toks[i+1].quoted {
		// A qualified name. Skip each part of it.
		j := i + 1
		for j+1 < len(toks) && toks[j].text == "." && !toks[j].quoted {
			j += 2
		}
		return "", j, false
	}
	return t.text, i + 1, true
}

// isKeyword reports whether t is a word that follows a table and is no alias.
func isKeyword(t token) bool {
	if t.quoted {
		return false
	}
	switch strings.ToUpper(t.text) {
	case "WHERE", "GROUP", "ORDER", "LIMIT", "OFFSET", "FETCH", "HAVING", "UNION", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "ON", "USING", "SELECT", "FROM":
		return true
	}
	return false
}

// isWordByte reports whether c can be part of an unquoted name.
func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// tokens splits a statement into words, quoted names and single characters.
// It drops string literals and comments.
func tokens(stmt string) []token {
	var out []token
	for i := 0; i < len(stmt); {
		c := stmt[i]
		switch {
		case c == '\'':
			i = skipQuoted(stmt, i)
		case c == '"' || c == '`':
			end := skipQuoted(stmt, i)
			name := strings.ReplaceAll(stmt[i+1:max(i+1, end-1)], string(c)+string(c), string(c))
			out = append(out, token{text: name, quoted: true})
			i = end
		case c == '-' && strings.HasPrefix(stmt[i:], "--"):
			if j := strings.IndexByte(stmt[i:], '\n'); j >= 0 {
				i += j + 1
			} else {
				i = len(stmt)
			}
		case c == '/' && strings.HasPrefix(stmt[i:], "/*"):
			if j := strings.Index(stmt[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = len(stmt)
			}
		case isWordByte(c):
			j := i
			for j < len(stmt) && isWordByte(stmt[j]) {
				j++
			}
			out = append(out, token{text: stmt[i:j]})
			i = j
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			out = append(out, token{text: string(c)})
			i++
		}
	}
	return out
}

// skipQuoted returns the index after the quoted text that starts at i. A
// quote written twice stands for itself.
func skipQuoted(stmt string, i int) int {
	q := stmt[i]
	for j := i + 1; j < len(stmt); j++ {
		if stmt[j] != q {
			continue
		}
		if j+1 < len(stmt) && stmt[j+1] == q {
			j++
			continue
		}
		return j + 1
	}
	return len(stmt)
}
