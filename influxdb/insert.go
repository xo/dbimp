package influxdb

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// pathWrite is the endpoint of line protocol that every release has
// (measured).
const pathWrite = "/write"

// insert is one INSERT statement of the influx shell:
// INSERT [INTO <database>[.<retention-policy>]] <line-protocol> (D85).
type insert struct {
	db, rp string
	lines  []lpLine
}

// parseInsert returns the INSERT statement that query holds, or nil when
// query is not one. A statement that starts with INSERT is always one, in
// both dialects, because the server has no INSERT of its own (D85).
func parseInsert(query string) (*insert, error) {
	rest, ok := cutWord(strings.TrimLeft(query, " \t\r\n"), "INSERT")
	if !ok {
		return nil, nil
	}
	ins := &insert{}
	if after, ok := cutWord(rest, "INTO"); ok {
		target, lp, err := readTarget(after)
		if err != nil {
			return nil, err
		}
		if ins.db, ins.rp, err = splitTarget(target); err != nil {
			return nil, err
		}
		rest = lp
	}
	lines, err := parseLines(rest)
	if err != nil {
		return nil, err
	}
	ins.lines = lines
	return ins, nil
}

// cutWord returns s after the word w and the white space that follows it,
// when s starts with w in any case and white space follows it.
func cutWord(s, w string) (string, bool) {
	if len(s) <= len(w) || !strings.EqualFold(s[:len(w)], w) || !isSpace(s[len(w)]) {
		return s, false
	}
	return strings.TrimLeft(s[len(w):], " \t\r\n"), true
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// readTarget reads the target of INTO, up to white space, where an
// identifier in double quotes can hold a space or a dot.
func readTarget(s string) (string, string, error) {
	quoted := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			quoted = !quoted
		case c == '\\' && quoted:
			i++
		case isSpace(c) && !quoted:
			return s[:i], strings.TrimLeft(s[i:], " \t\r\n"), nil
		}
	}
	return "", "", fmt.Errorf("parsing INSERT INTO: no line protocol follows %q: %w", s, dbimp.ErrInvalidValue)
}

// splitTarget splits <database>[.<retention-policy>], and unquotes each part.
func splitTarget(t string) (string, string, error) {
	var parts []string
	var b strings.Builder
	quoted := false
	for i := 0; i < len(t); i++ {
		switch c := t[i]; {
		case c == '"':
			quoted = !quoted
		case c == '\\' && quoted && i+1 < len(t):
			i++
			b.WriteByte(t[i])
		case c == '.' && !quoted:
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	parts = append(parts, b.String())
	switch {
	case quoted || len(parts) > 2 || slices.Contains(parts, ""):
		return "", "", fmt.Errorf("parsing INSERT INTO %q: %w", t, dbimp.ErrInvalidValue)
	case len(parts) == 2:
		return parts[0], parts[1], nil
	}
	return parts[0], "", nil
}

// lpPair is one tag or one field of a line: the key and the value as they
// are written, or the name of the placeholder that holds the value.
type lpPair struct {
	key, val, ph string
}

// lpLine is one line of line protocol. A blank line or a comment holds only
// its text.
type lpLine struct {
	raw          string
	measurement  string
	tags, fields []lpPair
	ts, tsPH     string
}

// parseLines parses the lines of line protocol in s. It reads each part of a
// line with its escapes, so that a placeholder is found only where a value
// stands (D85).
func parseLines(s string) ([]lpLine, error) {
	var lines []lpLine
	for i := 0; i < len(s); {
		j := lineEnd(s, i)
		text := strings.TrimRight(s[i:j], "\r")
		i = j + 1
		if t := strings.TrimSpace(text); t == "" || strings.HasPrefix(t, "#") {
			lines = append(lines, lpLine{raw: text})
			continue
		}
		l, err := parseLine(strings.TrimLeft(text, " \t"))
		if err != nil {
			return nil, fmt.Errorf("parsing the line protocol %q: %w", text, err)
		}
		lines = append(lines, l)
	}
	if !slices.ContainsFunc(lines, func(l lpLine) bool { return l.measurement != "" }) {
		return nil, fmt.Errorf("parsing INSERT: no line protocol: %w", dbimp.ErrInvalidValue)
	}
	return lines, nil
}

// lineEnd returns the index of the new line that ends the line at i, outside
// a string field, or len(s).
func lineEnd(s string, i int) int {
	quoted := false
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			quoted = !quoted
		case '\n':
			if !quoted {
				return i
			}
		}
	}
	return len(s)
}

// parseLine parses one line: measurement[,tag=value...] field=value[,...]
// [timestamp].
func parseLine(s string) (lpLine, error) {
	var l lpLine
	var i int
	l.measurement, i = readRaw(s, 0, ", ")
	if l.measurement == "" {
		return l, fmt.Errorf("no measurement: %w", dbimp.ErrInvalidValue)
	}
	for i < len(s) && s[i] == ',' {
		p, j, err := readPair(s, i+1, false)
		if err != nil {
			return l, err
		}
		l.tags, i = append(l.tags, p), j
	}
	if i >= len(s) || s[i] != ' ' {
		return l, fmt.Errorf("no field: %w", dbimp.ErrInvalidValue)
	}
	for i < len(s) && s[i] == ' ' {
		i++
	}
	for {
		p, j, err := readPair(s, i, true)
		if err != nil {
			return l, err
		}
		l.fields, i = append(l.fields, p), j
		if i >= len(s) || s[i] != ',' {
			break
		}
		i++
	}
	l.ts = strings.TrimSpace(s[i:])
	if strings.ContainsAny(l.ts, " \t") {
		return l, fmt.Errorf("the timestamp %q: %w", l.ts, dbimp.ErrInvalidValue)
	}
	l.tsPH = placeholder(l.ts)
	return l, nil
}

// readPair reads key=value at i. A field value in double quotes is a string.
func readPair(s string, i int, field bool) (lpPair, int, error) {
	var p lpPair
	p.key, i = readRaw(s, i, "=, ")
	if p.key == "" || i >= len(s) || s[i] != '=' {
		return p, i, fmt.Errorf("a key with no value: %w", dbimp.ErrInvalidValue)
	}
	i++
	if field && i < len(s) && s[i] == '"' {
		j := i + 1
		for ; j < len(s) && s[j] != '"'; j++ {
			if s[j] == '\\' {
				j++
			}
		}
		if j >= len(s) {
			return p, j, fmt.Errorf("a string with no end: %w", dbimp.ErrInvalidValue)
		}
		p.val = s[i : j+1]
		return p, j + 1, nil
	}
	p.val, i = readRaw(s, i, ", ")
	if p.val == "" {
		return p, i, fmt.Errorf("the key %q has an empty value: %w", p.key, dbimp.ErrInvalidValue)
	}
	p.ph = placeholder(p.val)
	return p, i, nil
}

// readRaw reads s from i up to a character of stops that no backslash
// escapes, and returns the text as it is written.
func readRaw(s string, i int, stops string) (string, int) {
	j := i
	for ; j < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		if strings.IndexByte(stops, s[j]) >= 0 {
			break
		}
	}
	j = min(j, len(s))
	return s[i:j], j
}

// placeholder returns the name of the placeholder that v is, such as "key"
// for $key or "1" for $1, or "".
func placeholder(v string) string {
	name, ok := strings.CutPrefix(v, "$")
	if !ok || name == "" {
		return ""
	}
	digits := strings.Trim(name, "0123456789") == ""
	for i, c := range name {
		ok := c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || i > 0 && '0' <= c && c <= '9'
		if !ok && !digits {
			return ""
		}
	}
	return name
}

// body returns the lines with each argument written as a literal of line
// protocol. Without arguments, the lines go as they are written, and a value
// such as $x is text. A NULL leaves its tag, its field or its timestamp out
// of the line (D85).
func (ins *insert) body(args []driver.NamedValue) (string, error) {
	vals := map[string]any{}
	for _, a := range args {
		vals[paramName(a)] = a.Value
	}
	used := map[string]bool{}
	bind := func(name string) (any, error) {
		v, ok := vals[name]
		if !ok {
			return nil, fmt.Errorf("the placeholder $%s has no argument: %w", name, dbimp.ErrArguments)
		}
		used[name] = true
		return v, nil
	}
	var b strings.Builder
	for i, l := range ins.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if l.measurement == "" {
			b.WriteString(l.raw)
			continue
		}
		if err := l.write(&b, len(args) > 0, bind); err != nil {
			return "", err
		}
	}
	for _, a := range args {
		if !used[paramName(a)] {
			return "", fmt.Errorf("the argument %s fills no placeholder: %w", paramName(a), dbimp.ErrArguments)
		}
	}
	return b.String(), nil
}

// write writes the line to b, and binds each placeholder when bound is true.
func (l lpLine) write(b *strings.Builder, bound bool, bind func(string) (any, error)) error {
	b.WriteString(l.measurement)
	for _, p := range l.tags {
		val := p.val
		if bound && p.ph != "" {
			v, err := bind(p.ph)
			if err != nil {
				return err
			}
			if v == nil {
				continue
			}
			if val, err = tagLiteral(v); err != nil {
				return fmt.Errorf("the tag %s: %w", p.key, err)
			}
		}
		b.WriteString("," + p.key + "=" + val)
	}
	n := 0
	for _, p := range l.fields {
		val := p.val
		if bound && p.ph != "" {
			v, err := bind(p.ph)
			if err != nil {
				return err
			}
			if v == nil {
				continue
			}
			if val, err = fieldLiteral(v); err != nil {
				return fmt.Errorf("the field %s: %w", p.key, err)
			}
		}
		if n == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteByte(',')
		}
		b.WriteString(p.key + "=" + val)
		n++
	}
	if n == 0 {
		return fmt.Errorf("a line of %s has no field that is not NULL: %w", l.measurement, dbimp.ErrInvalidValue)
	}
	ts := l.ts
	if bound && l.tsPH != "" {
		v, err := bind(l.tsPH)
		if err != nil {
			return err
		}
		if ts, err = timestampLiteral(v); err != nil {
			return err
		}
	}
	if ts != "" {
		b.WriteString(" " + ts)
	}
	return nil
}

// tagLiteral writes v as the value of a tag, which is text with its commas,
// equal signs and spaces escaped.
func tagLiteral(v any) (string, error) {
	var s string
	switch v := v.(type) {
	case string:
		s = v
	case int64:
		s = strconv.FormatInt(v, 10)
	case uint64:
		s = strconv.FormatUint(v, 10)
	case float64:
		s = strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		s = strconv.FormatBool(v)
	case time.Time:
		s = v.Format(time.RFC3339Nano)
	case *apd.Decimal:
		s = v.Text('f')
	default:
		return "", fmt.Errorf("a tag of %T: %w", v, dbimp.ErrNotSupported)
	}
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return "", fmt.Errorf("a tag value %q: %w", s, dbimp.ErrInvalidValue)
	}
	return strings.NewReplacer(`,`, `\,`, `=`, `\=`, ` `, `\ `).Replace(s), nil
}

// fieldLiteral writes v as the value of a field: 1i for an integer, 1u for
// an unsigned integer, a float, true or false, or a string in double quotes.
func fieldLiteral(v any) (string, error) {
	switch v := v.(type) {
	case int64:
		return strconv.FormatInt(v, 10) + "i", nil
	case uint64:
		return strconv.FormatUint(v, 10) + "u", nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "", fmt.Errorf("line protocol has no %v: %w", v, dbimp.ErrInvalidValue)
		}
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case *apd.Decimal:
		if v.Form != apd.Finite {
			return "", fmt.Errorf("line protocol has no %s: %w", v, dbimp.ErrInvalidValue)
		}
		// A field holds a float64, so a decimal is written as one.
		return v.Text('g'), nil
	case bool:
		return strconv.FormatBool(v), nil
	case string:
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`, nil
	}
	return "", fmt.Errorf("a field of %T: %w", v, dbimp.ErrNotSupported)
}

// timestampLiteral writes v as the timestamp of a line, in nanoseconds. A
// NULL leaves the timestamp out, and the server uses its own time.
func timestampLiteral(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "", nil
	case time.Time:
		return strconv.FormatInt(v.UnixNano(), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	}
	return "", fmt.Errorf("a timestamp of %T: %w", v, dbimp.ErrNotSupported)
}

// write sends an INSERT to /write, into the database and the retention
// policy of INTO, or of the DSN (D85).
func (c *conn) write(ctx context.Context, ins *insert, args []driver.NamedValue) error {
	body, err := ins.body(args)
	if err != nil {
		return fmt.Errorf("binding the INSERT: %w", err)
	}
	db, rp := ins.db, ins.rp
	if db == "" {
		db, rp = c.c.cfg.Database, c.c.cfg.RetentionPolicy
	}
	if db == "" {
		return fmt.Errorf("running INSERT: no database: name it in the DSN or with INTO: %w", dbimp.ErrInvalidValue)
	}
	q := url.Values{"db": {db}}
	if rp != "" {
		q.Set("rp", rp)
	}
	res, err := c.c.send(ctx, c.major, pathWrite+"?"+q.Encode(), "text/plain; charset=utf-8", []byte(body))
	if err != nil {
		return err
	}
	return res.Body.Close()
}

// noRows is the result of an INSERT, which has no columns and no rows.
type noRows struct{}

func (noRows) Columns() []string         { return []string{} }
func (noRows) Close() error              { return nil }
func (noRows) Next([]driver.Value) error { return io.EOF }
