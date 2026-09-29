package influxdb

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"

	"github.com/xo/dbimp"
)

// pathInfluxQL is the endpoint of InfluxQL, which every release has.
const pathInfluxQL = "/query"

// queryInfluxQL sends a statement of InfluxQL, and reads the answer up to the
// first result set. It asks InfluxDB 1 for chunks with chunked=prefer, and
// every other release for one document (D83).
func (c *conn) queryInfluxQL(ctx context.Context, query string, p jsontext.Value) (*qlRows, error) {
	q := url.Values{}
	if db := c.c.cfg.Database; db != "" {
		q.Set("db", db)
	}
	if rp := c.c.cfg.RetentionPolicy; rp != "" {
		q.Set("rp", rp)
	}
	if c.c.cfg.Chunked == ChunkedPrefer && c.major == 1 {
		q.Set("chunked", "true")
	}
	form := url.Values{"q": {query}}
	if p != nil {
		form.Set("params", string(p))
	}
	path := pathInfluxQL
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	res, err := c.c.send(ctx, c.major, path, "application/x-www-form-urlencoded", []byte(form.Encode()))
	if err != nil {
		return nil, err
	}
	return readInfluxQL(res)
}

// The levels of the answer of InfluxQL. The answer is one document, or one
// document for each chunk. A document holds results, a result holds series,
// and a series holds rows.
const (
	levelDocs = iota
	levelResults
	levelSeries
	levelRows
)

// qlSet is one result set: one series of a statement, or a statement with no
// series, or the error of a statement (D81).
type qlSet struct {
	// id is the statement_id of the statement.
	id int
	// err is the error of the statement, or nil.
	err error
	// name is the name of the series, or nil for a series with no member
	// name. A name that is null reads as "".
	name any
	// tagKeys and tagVals are the tags of the series, in the order of the
	// object.
	tagKeys []string
	tagVals []any
	// cols are the columns of the series, or nil for a statement with no
	// series.
	cols []string
	// arr reads the rows of the series, or is nil for a series with no rows.
	arr *dbimp.ArrayRows
}

// columns returns the columns of the set: measurement, which holds the name
// of the series, each tag, and the columns of the series (D81 and D96). A
// statement with no series has none.
func (s *qlSet) columns() []string {
	if s.cols == nil {
		return []string{}
	}
	return slices.Concat([]string{"measurement"}, s.tagKeys, s.cols)
}

// continues reports whether next is the rest of the series s, which the
// server split into chunks.
func (s *qlSet) continues(next *qlSet) bool {
	return next.err == nil && next.cols != nil && next.id == s.id && next.name == s.name &&
		slices.Equal(next.tagKeys, s.tagKeys) && slices.Equal(next.tagVals, s.tagVals) && slices.Equal(next.cols, s.cols)
}

// qlRows reads the answer of InfluxQL one token at a time (D25). Each series
// is a result set, and Rows.NextResultSet moves to the next one (D81).
type qlRows struct {
	s     *dbimp.Stream
	dec   *jsontext.Decoder
	level int
	// resID is the statement_id of the result that the decoder is in.
	resID int
	// lastID is the largest statement_id of a set so far, or -1.
	lastID int
	// held is a set that follows a gap in statement_id, which waits for the
	// empty sets of the gap (D83).
	held *qlSet

	cur     *qlSet
	cols    []string
	vals    []jsontext.Value
	next    *qlSet
	nextErr error
	ended   bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner = (*qlRows)(nil)
	_ driver.RowsNextResultSet = (*qlRows)(nil)
)

// readInfluxQL reads the answer of res up to its first result set. It returns
// the error of the first statement, when that statement failed.
func readInfluxQL(res *http.Response) (*qlRows, error) {
	s := dbimp.NewStream(res.Body)
	r := &qlRows{s: s, dec: s.Decoder(), lastID: -1}
	first, err := r.pull()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	switch {
	case first == nil:
		first = &qlSet{}
	case first.err != nil:
		_ = s.Close()
		return nil, first.err
	}
	r.activate(first)
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *qlRows) Columns() []string {
	return r.cols
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36).
func (r *qlRows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner. A series that holds
// "partial":true continues in the next chunk, and its rows continue in the
// same result set (D83). An error of the same statement that follows its rows
// is the error of the set.
func (r *qlRows) NextRow() error {
	for {
		if r.cur.arr == nil {
			return io.EOF
		}
		err := r.cur.arr.Next(r.vals)
		switch {
		case err == nil:
			return nil
		case !errors.Is(err, io.EOF):
			r.cur.arr = nil
			return fmt.Errorf("reading a row: %w", err)
		}
		r.cur.arr = nil
		partial, err := r.endSeries()
		switch {
		case err != nil:
			return err
		case !partial:
			// database/sql compares the end of the rows with io.EOF itself.
			return io.EOF
		}
		set, err := r.advance()
		switch {
		case err != nil:
			return err
		case set == nil:
			return io.EOF
		case set.err != nil && set.id == r.cur.id:
			return set.err
		case r.cur.continues(set):
			r.cur.arr = set.arr
		default:
			r.held = set
			return io.EOF
		}
	}
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *qlRows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	for i := range dest {
		v, err := r.value(i)
		if err != nil {
			return err
		}
		dest[i] = v
	}
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *qlRows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	v, err := r.value(i)
	if err != nil {
		return err
	}
	return dbimp.Assign(scanCtx, dest, v)
}

// HasNextResultSet satisfies driver.RowsNextResultSet. It skips the rows of
// the current set that are left, and reads the answer up to the next set.
func (r *qlRows) HasNextResultSet() bool {
	if r.next == nil && r.nextErr == nil {
		r.next, r.nextErr = r.skipToNext()
	}
	return r.next != nil || r.nextErr != nil
}

// NextResultSet satisfies driver.RowsNextResultSet. The error of a statement
// is the error of its set.
func (r *qlRows) NextResultSet() error {
	if !r.HasNextResultSet() {
		return io.EOF
	}
	set, err := r.next, r.nextErr
	r.next, r.nextErr = nil, nil
	switch {
	case err != nil:
		return err
	case set.err != nil:
		r.activate(&qlSet{id: set.id})
		return set.err
	}
	r.activate(set)
	return nil
}

// value returns the value of column i: the name, a tag, or a value of the row
// that decodes by D83.
func (r *qlRows) value(i int) (any, error) {
	if i == 0 {
		return r.cur.name, nil
	}
	i--
	if i < len(r.cur.tagVals) {
		return r.cur.tagVals[i], nil
	}
	i -= len(r.cur.tagVals)
	v, err := decodeInfluxQL(r.vals[i], r.cur.cols[i] == "time")
	if err != nil {
		return nil, fmt.Errorf("reading the column %q: %w", r.cur.cols[i], err)
	}
	return v, nil
}

// activate makes set the current result set.
func (r *qlRows) activate(set *qlSet) {
	r.cur, r.cols = set, set.columns()
	r.vals = make([]jsontext.Value, len(set.cols))
	r.lastID = max(r.lastID, set.id)
}

// skipToNext reads the rows of the current set that are left, and returns
// the next set, or nil at the end of the answer.
func (r *qlRows) skipToNext() (*qlSet, error) {
	for {
		err := r.NextRow()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return r.pull()
}

// pull returns the next set, with an empty set for each statement_id that the
// server left out (D83).
func (r *qlRows) pull() (*qlSet, error) {
	set := r.held
	r.held = nil
	if set == nil {
		var err error
		if set, err = r.advance(); err != nil || set == nil {
			return nil, err
		}
	}
	if set.id > r.lastID+1 {
		r.held = set
		return &qlSet{id: r.lastID + 1}, nil
	}
	return set, nil
}

// advance reads the answer up to the next series, the next statement with no
// series, or the next error. It returns nil at the end of the answer.
func (r *qlRows) advance() (*qlSet, error) {
	for {
		switch r.level {
		case levelDocs:
			if r.ended {
				return nil, nil
			}
			if r.dec.PeekKind() != '{' {
				r.ended = true
				return nil, r.s.End()
			}
			set, err := r.openDoc()
			if err != nil || set != nil {
				return set, err
			}
		case levelResults:
			set, err := r.readResult()
			if err != nil || set != nil {
				return set, err
			}
		case levelSeries:
			set, err := r.readSeries()
			if err != nil || set != nil {
				return set, err
			}
		case levelRows:
			// The current set did not read its rows. Its rows are skipped.
			if err := r.skipRows(); err != nil {
				return nil, err
			}
		}
	}
}

// openDoc reads a document up to its results. A document that holds "error"
// is the error of the request.
func (r *qlRows) openDoc() (*qlSet, error) {
	if err := expect(r.dec, '{'); err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		name, err := readName(r.dec)
		if err != nil {
			return nil, err
		}
		switch name {
		case "results":
			if err := expect(r.dec, '['); err != nil {
				return nil, fmt.Errorf("reading the results: %w", err)
			}
			r.level = levelResults
			return nil, nil
		case "error":
			msg, err := readString(r.dec)
			if err != nil {
				return nil, err
			}
			return &qlSet{id: r.lastID + 1, err: &Error{HTTPStatus: http.StatusOK, Statement: -1, Message: msg}}, nil
		}
		if err := r.dec.SkipValue(); err != nil {
			return nil, fmt.Errorf("reading the answer: %w", err)
		}
	}
	return nil, r.closeObject()
}

// readResult reads the next result up to its series. A result with no series
// is a set with no columns, and a result with an error is the error of its
// statement.
func (r *qlRows) readResult() (*qlSet, error) {
	if r.dec.PeekKind() == ']' {
		if _, err := r.dec.ReadToken(); err != nil {
			return nil, fmt.Errorf("reading the end of the results: %w", err)
		}
		if err := r.skipMembers(); err != nil {
			return nil, err
		}
		r.level = levelDocs
		return nil, nil
	}
	if err := expect(r.dec, '{'); err != nil {
		return nil, fmt.Errorf("reading a result: %w", err)
	}
	r.resID = r.lastID + 1
	var msg string
	for r.dec.PeekKind() != '}' {
		name, err := readName(r.dec)
		if err != nil {
			return nil, err
		}
		switch name {
		case "statement_id":
			v, err := r.dec.ReadValue()
			if err != nil {
				return nil, fmt.Errorf("reading the statement_id: %w", err)
			}
			id, err := dbimp.Int64(v)
			if err != nil {
				return nil, fmt.Errorf("reading the statement_id: %w", err)
			}
			r.resID = int(id)
		case "error":
			if msg, err = readString(r.dec); err != nil {
				return nil, err
			}
		case "series":
			if err := expect(r.dec, '['); err != nil {
				return nil, fmt.Errorf("reading the series: %w", err)
			}
			r.level = levelSeries
			return nil, nil
		default:
			if err := r.dec.SkipValue(); err != nil {
				return nil, fmt.Errorf("reading a result: %w", err)
			}
		}
	}
	if err := r.closeObject(); err != nil {
		return nil, err
	}
	if msg != "" {
		return &qlSet{id: r.resID, err: &Error{HTTPStatus: http.StatusOK, Statement: r.resID, Message: msg}}, nil
	}
	return &qlSet{id: r.resID}, nil
}

// readSeries reads the next series of the result up to its rows.
func (r *qlRows) readSeries() (*qlSet, error) {
	if r.dec.PeekKind() == ']' {
		if _, err := r.dec.ReadToken(); err != nil {
			return nil, fmt.Errorf("reading the end of the series: %w", err)
		}
		msg, err := r.resultTail()
		if err != nil {
			return nil, err
		}
		r.level = levelResults
		if msg != "" {
			return &qlSet{id: r.resID, err: &Error{HTTPStatus: http.StatusOK, Statement: r.resID, Message: msg}}, nil
		}
		return nil, nil
	}
	if err := expect(r.dec, '{'); err != nil {
		return nil, fmt.Errorf("reading a series: %w", err)
	}
	set := &qlSet{id: r.resID, cols: []string{}}
	for r.dec.PeekKind() != '}' {
		name, err := readName(r.dec)
		if err != nil {
			return nil, err
		}
		switch name {
		case "name":
			if set.name, err = readString(r.dec); err != nil {
				return nil, err
			}
		case "tags":
			if set.tagKeys, set.tagVals, err = readTags(r.dec); err != nil {
				return nil, err
			}
		case "columns":
			if set.cols, err = readStrings(r.dec); err != nil {
				return nil, err
			}
		case "values":
			if set.arr, err = dbimp.NewArrayRows(r.dec, len(set.cols)); err != nil {
				return nil, err
			}
			r.level = levelRows
			return set, nil
		default:
			if err := r.dec.SkipValue(); err != nil {
				return nil, fmt.Errorf("reading a series: %w", err)
			}
		}
	}
	// A series with no values has no rows.
	if err := r.closeObject(); err != nil {
		return nil, err
	}
	return set, nil
}

// endSeries reads the members of the series that follow its rows, and
// reports whether the series holds "partial":true, which says that it
// continues in the next chunk.
func (r *qlRows) endSeries() (bool, error) {
	partial := false
	for r.dec.PeekKind() != '}' {
		name, err := readName(r.dec)
		if err != nil {
			return false, err
		}
		v, err := r.dec.ReadValue()
		if err != nil {
			return false, fmt.Errorf("reading %q of a series: %w", name, err)
		}
		if name == "partial" && v.Kind() == 't' {
			partial = true
		}
	}
	if err := r.closeObject(); err != nil {
		return false, err
	}
	r.level = levelSeries
	return partial, nil
}

// skipRows reads the rows that are left of a series, and the rest of it.
func (r *qlRows) skipRows() error {
	for r.dec.PeekKind() != ']' {
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the rows: %w", err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the rows: %w", err)
	}
	_, err := r.endSeries()
	return err
}

// resultTail reads the members of a result that follow its series, and
// returns the text of an error among them.
func (r *qlRows) resultTail() (string, error) {
	var msg string
	for r.dec.PeekKind() != '}' {
		name, err := readName(r.dec)
		if err != nil {
			return "", err
		}
		if name == "error" {
			if msg, err = readString(r.dec); err != nil {
				return "", err
			}
			continue
		}
		if err := r.dec.SkipValue(); err != nil {
			return "", fmt.Errorf("reading a result: %w", err)
		}
	}
	return msg, r.closeObject()
}

// skipMembers reads the members that are left of an object, and its end.
func (r *qlRows) skipMembers() error {
	for r.dec.PeekKind() != '}' {
		if _, err := readName(r.dec); err != nil {
			return err
		}
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
	}
	return r.closeObject()
}

// closeObject reads the end of an object.
func (r *qlRows) closeObject() error {
	if err := expect(r.dec, '}'); err != nil {
		return fmt.Errorf("reading the end of an object: %w", err)
	}
	return nil
}

// readName reads the name of a member.
func readName(dec *jsontext.Decoder) (string, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return "", fmt.Errorf("reading the answer: %w", err)
	}
	return tok.String(), nil
}

// readString reads a string, or nil as "".
func readString(dec *jsontext.Decoder) (string, error) {
	v, err := dec.ReadValue()
	if err != nil {
		return "", fmt.Errorf("reading a string: %w", err)
	}
	if dbimp.IsNull(v) {
		return "", nil
	}
	return dbimp.String(v)
}

// readStrings reads an array of strings.
func readStrings(dec *jsontext.Decoder) ([]string, error) {
	if err := expect(dec, '['); err != nil {
		return nil, fmt.Errorf("reading the columns: %w", err)
	}
	out := []string{}
	for dec.PeekKind() != ']' {
		s, err := readString(dec)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading the end of the columns: %w", err)
	}
	return out, nil
}

// readTags reads the tags of a series, in the order of the object.
func readTags(dec *jsontext.Decoder) ([]string, []any, error) {
	if err := expect(dec, '{'); err != nil {
		return nil, nil, fmt.Errorf("reading the tags: %w", err)
	}
	var (
		keys []string
		vals []any
	)
	for dec.PeekKind() != '}' {
		name, err := readName(dec)
		if err != nil {
			return nil, nil, err
		}
		v, err := dec.ReadValue()
		if err != nil {
			return nil, nil, fmt.Errorf("reading the tag %q: %w", name, err)
		}
		val, err := decodeInfluxQL(v, false)
		if err != nil {
			return nil, nil, fmt.Errorf("reading the tag %q: %w", name, err)
		}
		keys, vals = append(keys, name), append(vals, val)
	}
	if _, err := dec.ReadToken(); err != nil {
		return nil, nil, fmt.Errorf("reading the end of the tags: %w", err)
	}
	return keys, vals, nil
}

// expect reads one token, and returns an error if it is not kind.
func expect(dec *jsontext.Decoder, kind jsontext.Kind) error {
	tok, err := dec.ReadToken()
	switch {
	case err != nil:
		return err
	case tok.Kind() != kind:
		return fmt.Errorf("%v where %v was expected: %w", tok.Kind(), kind, dbimp.ErrInvalidValue)
	}
	return nil
}
