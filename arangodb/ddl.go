package arangodb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// ddl is one statement of the DDL of the driver (D92).
type ddl struct {
	verb        string
	drop, index bool
	ifExists    bool
	name, coll  string
	edge        bool
	kind        string
	unique      bool
	sparse      bool
	fields      []string
	expire      int
}

// token is one word of a statement of the DDL, or a name in backticks.
type token struct {
	text   string
	quoted bool
}

// is reports whether t is the keyword w, in any case.
func (t token) is(w string) bool {
	return !t.quoted && strings.EqualFold(t.text, w)
}

// parseDDL returns the statement of the DDL that query holds, or nil when
// query is AQL. AQL has no CREATE and no DROP, so a statement that starts
// with either is DDL of the driver (D92).
// It reads the first word before it splits the rest, so that AQL, whose
// strings can hold a backtick, never meets the rules of the DDL.
func parseDDL(query string) (*ddl, error) {
	first := strings.TrimLeft(query, " \t\r\n")
	if i := strings.IndexAny(first, " \t\r\n(`"); i >= 0 {
		first = first[:i]
	}
	if !strings.EqualFold(first, "CREATE") && !strings.EqualFold(first, "DROP") {
		return nil, nil
	}
	toks, err := tokenize(query)
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %w", strings.TrimSpace(query), err)
	}
	p := &ddlParser{toks: toks, query: query}
	d, err := p.statement()
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %w", strings.TrimSpace(query), err)
	}
	return d, nil
}

// tokenize splits a statement into words, names in backticks, and the
// characters ( ) and ,.
func tokenize(s string) ([]token, error) {
	var toks []token
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case ' ', '\t', '\r', '\n':
			i++
		case '(', ')', ',':
			toks = append(toks, token{text: string(c)})
			i++
		case '`':
			j := strings.IndexByte(s[i+1:], '`')
			if j < 0 {
				return nil, fmt.Errorf("a name in backticks with no end: %w", dbimp.ErrUnterminated)
			}
			toks = append(toks, token{text: s[i+1 : i+1+j], quoted: true})
			i += j + 2
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t\r\n(),`", rune(s[j])) {
				j++
			}
			toks = append(toks, token{text: s[i:j]})
			i = j
		}
	}
	return toks, nil
}

// ddlParser reads the tokens of one statement.
type ddlParser struct {
	toks  []token
	i     int
	query string
}

func (p *ddlParser) peek() token {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return token{}
}

// accept reads the keyword w if it comes next.
func (p *ddlParser) accept(w string) bool {
	if p.peek().is(w) {
		p.i++
		return true
	}
	return false
}

// want reads the keywords ws, in order, or returns an error.
func (p *ddlParser) want(ws ...string) error {
	for _, w := range ws {
		if !p.accept(w) {
			return fmt.Errorf("%s where %s was expected: %w", p.found(), w, dbimp.ErrInvalidValue)
		}
	}
	return nil
}

// found names the next token for an error.
func (p *ddlParser) found() string {
	if t := p.peek(); t.text != "" {
		return strconv.Quote(t.text)
	}
	return "the end"
}

// name reads a name.
func (p *ddlParser) name() (string, error) {
	t := p.peek()
	if t.text == "" || !t.quoted && strings.ContainsAny(t.text, "(),") {
		return "", fmt.Errorf("%s where a name was expected: %w", p.found(), dbimp.ErrInvalidValue)
	}
	p.i++
	return t.text, nil
}

// statement reads a whole statement.
func (p *ddlParser) statement() (*ddl, error) {
	d := &ddl{drop: p.peek().is("DROP")}
	p.i++
	if d.drop {
		d.verb = "DROP"
	} else {
		d.verb = "CREATE"
	}
	var err error
	switch {
	case d.drop && p.accept("COLLECTION"):
		if err = p.ifExists(d); err == nil {
			d.name, err = p.name()
		}
	case d.drop && p.accept("INDEX"):
		d.index = true
		if err = p.ifExists(d); err == nil {
			d.name, err = p.name()
		}
		if err == nil {
			if err = p.want("ON"); err == nil {
				d.coll, err = p.name()
			}
		}
	case !d.drop && p.accept("COLLECTION"):
		if err = p.ifNotExists(d); err == nil {
			if d.name, err = p.name(); err == nil {
				d.edge = p.accept("EDGE")
			}
		}
	case !d.drop:
		err = p.createIndex(d)
	default:
		err = fmt.Errorf("%s where COLLECTION or INDEX was expected: %w", p.found(), dbimp.ErrInvalidValue)
	}
	if err != nil {
		return nil, err
	}
	if p.i < len(p.toks) {
		return nil, fmt.Errorf("%s after the end of the statement: %w", p.found(), dbimp.ErrInvalidValue)
	}
	return d, nil
}

// ifExists reads IF EXISTS, if it comes next.
func (p *ddlParser) ifExists(d *ddl) error {
	if !p.accept("IF") {
		return nil
	}
	d.ifExists = true
	return p.want("EXISTS")
}

// ifNotExists reads IF NOT EXISTS, if it comes next.
func (p *ddlParser) ifNotExists(d *ddl) error {
	if !p.accept("IF") {
		return nil
	}
	d.ifExists = true
	return p.want("NOT", "EXISTS")
}

// createIndex reads CREATE [UNIQUE] [SPARSE] [GEO|INVERTED|TTL] INDEX
// [IF NOT EXISTS] <name> ON <collection> (<field>, ...) [EXPIRE AFTER <n>].
func (p *ddlParser) createIndex(d *ddl) error {
	d.index, d.kind = true, "persistent"
	for {
		switch {
		case p.accept("UNIQUE"):
			d.unique = true
			continue
		case p.accept("SPARSE"):
			d.sparse = true
			continue
		case p.accept("GEO"):
			d.kind = "geo"
		case p.accept("INVERTED"):
			d.kind = "inverted"
		case p.accept("TTL"):
			d.kind = "ttl"
		}
		break
	}
	if d.kind != "persistent" && (d.unique || d.sparse) {
		return fmt.Errorf("UNIQUE and SPARSE are for a persistent index: %w", dbimp.ErrInvalidValue)
	}
	if err := p.want("INDEX"); err != nil {
		return err
	}
	if err := p.ifNotExists(d); err != nil {
		return err
	}
	var err error
	if d.name, err = p.name(); err != nil {
		return err
	}
	if err := p.want("ON"); err != nil {
		return err
	}
	if d.coll, err = p.name(); err != nil {
		return err
	}
	if err := p.fieldList(d); err != nil {
		return err
	}
	if d.kind != "ttl" {
		return nil
	}
	if len(d.fields) != 1 {
		return fmt.Errorf("a TTL index has one field: %w", dbimp.ErrInvalidValue)
	}
	if err := p.want("EXPIRE", "AFTER"); err != nil {
		return err
	}
	t := p.peek()
	n, err := strconv.Atoi(t.text)
	if err != nil || n < 0 || t.quoted {
		return fmt.Errorf("%s where a number of seconds was expected: %w", p.found(), dbimp.ErrInvalidValue)
	}
	p.i++
	d.expire = n
	return nil
}

// fieldList reads (<field>, ...).
func (p *ddlParser) fieldList(d *ddl) error {
	if p.peek().text != "(" || p.peek().quoted {
		return fmt.Errorf("%s where ( was expected: %w", p.found(), dbimp.ErrInvalidValue)
	}
	p.i++
	for {
		f, err := p.name()
		if err != nil {
			return err
		}
		d.fields = append(d.fields, f)
		switch t := p.peek(); {
		case t.text == "," && !t.quoted:
			p.i++
		case t.text == ")" && !t.quoted:
			p.i++
			return nil
		default:
			return fmt.Errorf("%s where , or ) was expected: %w", p.found(), dbimp.ErrInvalidValue)
		}
	}
}

// run sends the HTTP call of the statement (D92).
func (d *ddl) run(ctx context.Context, c *Connector) error {
	switch {
	case !d.index && !d.drop:
		typ := 2
		if d.edge {
			typ = 3
		}
		err := c.call(ctx, http.MethodPost, "collection", map[string]any{"name": d.name, "type": typ}, nil, "")
		if d.ifExists && is(err, numDuplicateName) {
			return nil
		}
		return wrapDDL(err, "creating the collection", d.name)
	case !d.index:
		err := c.call(ctx, http.MethodDelete, "collection/"+url.PathEscape(d.name), nil, nil, "")
		if d.ifExists && is(err, numCollectionNotFound) {
			return nil
		}
		return wrapDDL(err, "dropping the collection", d.name)
	case !d.drop:
		if d.ifExists {
			if id, err := indexID(ctx, c, d.coll, d.name); err != nil || id != "" {
				return wrapDDL(err, "finding the index", d.name)
			}
		}
		body := map[string]any{"type": d.kind, "name": d.name, "fields": d.fields}
		if d.unique {
			body["unique"] = true
		}
		if d.sparse {
			body["sparse"] = true
		}
		if d.kind == "ttl" {
			body["expireAfter"] = d.expire
		}
		if d.kind == "geo" {
			// One field holds GeoJSON, or an array in the order of GeoJSON,
			// [longitude, latitude] (D106).
			body["geoJson"] = len(d.fields) == 1
		}
		err := c.call(ctx, http.MethodPost, "index?collection="+url.QueryEscape(d.coll), body, nil, "")
		return wrapDDL(err, "creating the index", d.name)
	}
	id, err := indexID(ctx, c, d.coll, d.name)
	switch {
	case err != nil:
		return wrapDDL(err, "finding the index", d.name)
	case id == "" && d.ifExists:
		return nil
	case id == "":
		return fmt.Errorf("dropping the index %q: %q has no such index: %w", d.name, d.coll, dbimp.ErrInvalidValue)
	}
	return wrapDDL(c.call(ctx, http.MethodDelete, "index/"+id, nil, nil, ""), "dropping the index", d.name)
}

// indexID returns the id of the index name of the collection coll, such as
// types/123, or "" when there is none.
func indexID(ctx context.Context, c *Connector, coll, name string) (string, error) {
	var list struct {
		Indexes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"indexes"`
	}
	if err := c.call(ctx, http.MethodGet, "index?collection="+url.QueryEscape(coll), nil, &list, ""); err != nil {
		return "", err
	}
	for _, ix := range list.Indexes {
		if ix.Name == name {
			return ix.ID, nil
		}
	}
	return "", nil
}

// wrapDDL wraps an error of a statement of the DDL, or returns nil.
func wrapDDL(err error, what, name string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %q: %w", what, name, err)
}
