package clickhouse

import (
	"bytes"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// marker is the text that the server writes in the stream when a query fails
// after it sent some rows, and the status of the response is already 200
// (D176 and measured). On 25.3 and 25.8 it is followed by a line break and the
// text of the error. On 26.9 it is followed by a line break, a tag, a line break,
// the text, the length of the text, a space, the tag, a line break and the marker
// again. The tag is in the header X-ClickHouse-Exception-Tag of the response.
const marker = "__exception__"

// tagHeader is the header of a response that names the tag of the marker, which
// 26.9 sends on every response (measured).
const tagHeader = "X-Clickhouse-Exception-Tag"

// formatHeader is the header of a response that names its format.
const formatHeader = "X-Clickhouse-Format"

// maxTrailer is the most that the driver reads of the text of an error after
// some rows.
const maxTrailer = 1 << 20

// rows reads the answer of one query in the format
// JSONCompactEachRowWithNamesAndTypes, one line at a time (D25 and D176). The
// first line is a JSON array of the names of the columns, the second is a JSON
// array of their types, and each line after them is a row. A result with no
// column, such as the answer to an INSERT, is an empty body. An error after some
// rows is the marker (D176).
type rows struct {
	body io.ReadCloser
	dec  *jsontext.Decoder
	w    *watch

	names []string
	cols  []*typ
	// cur is the current row.
	cur []driver.Value

	// tag is the header X-ClickHouse-Exception-Tag of the response, and tagged
	// is true when the end of the answer can be the tagged form of the marker.
	tag    string
	tagged bool

	// read is true once a row reached the caller, so that an error after it wraps
	// dbimp.ErrIncomplete (D107), and done once the answer is read to its end.
	read bool
	done bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
)

// readAnswer reads the names and the types of the columns of an answer, and
// then waits for its first row, or for its end. A server that fails the query
// before its first row answers with the marker after the names and the types, so
// QueryContext returns that error. It does not wrap dbimp.ErrIncomplete (D107).
// w is the watch of the query, which the rows end. version is the version that
// the connection read.
func readAnswer(res *http.Response, w *watch, version string) (*rows, error) {
	if f := res.Header.Get(formatHeader); f != "" && f != format {
		_ = res.Body.Close()
		return nil, fmt.Errorf("reading the answer: the format is %s and the driver reads %s, so the statement names a FORMAT: %w", f, format, dbimp.ErrNotSupported)
	}
	r := &rows{
		body: res.Body,
		dec:  jsontext.NewDecoder(res.Body, jsontext.AllowInvalidUTF8(true)),
		w:    w,
		tag:  res.Header.Get(tagHeader),
	}
	r.tagged = r.tag != "" || tagged(version)
	if err := r.readHeader(); err != nil {
		_ = r.body.Close()
		return nil, err
	}
	if !r.done && r.dec.PeekKind() != '[' {
		if err := r.NextRow(); !errors.Is(err, io.EOF) {
			_ = r.body.Close()
			if err == nil {
				err = fmt.Errorf("reading the answer: a row after the end of the rows: %w", dbimp.ErrInvalidValue)
			}
			return nil, err
		}
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName. It
// is the name of the type in upper case, without its arguments or its wrappers,
// such as INT8 or DATETIME64.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D177).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return r.cols[i].scanType()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. A column can hold
// a NULL when its type is Nullable, or is a type that holds a NULL of its own.
func (r *rows) ColumnTypeNullable(i int) (bool, bool) {
	return r.cols[i].isNullable(), true
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. It is the length of a
// FixedString.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	if t := r.cols[i]; t.family == famFixedString && len(t.nums) == 1 {
		return t.nums[0], true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. It is
// the precision and the scale of a Decimal.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if t := r.cols[i]; t.family == famDecimal && len(t.nums) == 2 {
		return t.nums[0], t.nums[1], true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). It ends the watch of the query. A query that ran to its end needs no
// cancel, and one whose context ended was cancelled by the watch. Rows that the
// caller closes before the end cancel the query on the server too (D176).
func (r *rows) Close() error {
	err := r.body.Close()
	if r.done {
		r.w.end()
	} else {
		r.w.abort()
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the end of the answer, and
// returns the error that the marker holds (D21 and D176).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	if r.dec.PeekKind() != '[' {
		return r.finish()
	}
	if err := r.readRow(); err != nil {
		return r.fail(err)
	}
	r.read = true
	return nil
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	copy(dest, r.cur)
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.cur[i])
}

// readHeader reads the line of the names and the line of the types. An empty
// answer has neither, and is a result with no column.
func (r *rows) readHeader() error {
	if r.dec.PeekKind() != '[' {
		if err := r.finish(); !errors.Is(err, io.EOF) {
			return err
		}
		return nil
	}
	names, err := r.readStrings()
	if err != nil {
		return r.headerError(fmt.Errorf("reading the names of the columns: %w", err))
	}
	if r.dec.PeekKind() != '[' {
		return r.headerError(fmt.Errorf("reading the types of the columns: the answer has no line of types: %w", io.ErrUnexpectedEOF))
	}
	texts, err := r.readStrings()
	if err != nil {
		return r.headerError(fmt.Errorf("reading the types of the columns: %w", err))
	}
	if len(texts) != len(names) {
		return fmt.Errorf("reading the header of the answer: %d names and %d types: %w", len(names), len(texts), dbimp.ErrColumnCount)
	}
	r.names = names
	r.cols = make([]*typ, len(names))
	for i, text := range texts {
		if r.cols[i], err = parseType(text); err != nil {
			return fmt.Errorf("reading the type of the column %s: %w", names[i], err)
		}
	}
	r.cur = make([]driver.Value, len(names))
	return nil
}

// headerError returns err, for a failure in the header. The header comes before
// any row, so no ErrIncomplete wraps it.
func (r *rows) headerError(err error) error {
	r.done = true
	return r.fail(err)
}

// readStrings reads one line of the header, an array of strings.
func (r *rows) readStrings() ([]string, error) {
	if err := expectKind(r.dec, '['); err != nil {
		return nil, err
	}
	out := []string{}
	for r.dec.PeekKind() != ']' {
		v, err := r.dec.ReadValue()
		if err != nil {
			return nil, err
		}
		s, err := decodeString(v)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return nil, err
	}
	return out, nil
}

// readRow reads and decodes one row.
func (r *rows) readRow() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	i := 0
	for ; r.dec.PeekKind() != ']'; i++ {
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		if i == len(r.cols) {
			return fmt.Errorf("reading a row: more than %d values: %w", len(r.cols), dbimp.ErrColumnCount)
		}
		// The value is decoded before the next call to the decoder, which reuses
		// its buffer.
		if r.cur[i], err = r.cols[i].decode(v); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.names[i], err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a row: %w", err)
	}
	if i < len(r.cols) {
		return fmt.Errorf("reading a row: %d values for %d columns: %w", i, len(r.cols), dbimp.ErrColumnCount)
	}
	return nil
}

// finish reads what follows the last row. It returns io.EOF for an answer that
// the server completed, the error that the marker holds, and an error for any
// other end. The decoder fails at the first character of the marker, which is
// not JSON, so the text is read from what the decoder holds and from the body
// (D176).
func (r *rows) finish() error {
	r.done = true
	_, err := r.dec.ReadValue()
	switch {
	case errors.Is(err, io.EOF):
		r.w.end()
		return io.EOF
	case err == nil:
		return r.fail(fmt.Errorf("reading the answer: a value that is not a row: %w", dbimp.ErrInvalidValue))
	}
	var serr *jsontext.SyntacticError
	if !errors.As(err, &serr) || errors.Is(err, io.ErrUnexpectedEOF) {
		// The body failed, or it ended inside a value: the answer was cut.
		return r.fail(fmt.Errorf("reading the answer: %w", err))
	}
	var buf bytes.Buffer
	_, rerr := buf.ReadFrom(io.LimitReader(io.MultiReader(bytes.NewReader(bytes.Clone(r.dec.UnreadBuffer())), r.body), maxTrailer))
	rest := bytes.TrimLeft(buf.Bytes(), " \t\r\n")
	// The server closes the connection after it wrote the marker and the text,
	// with no last chunk, so the body ends with io.ErrUnexpectedEOF after the
	// text (measured on 25.3, 25.8 and 26.9). Any other error is a failure.
	switch {
	case rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF):
		return r.fail(fmt.Errorf("reading the error after the rows: %w", rerr))
	case bytes.HasPrefix(rest, []byte(marker)):
		return r.fail(r.trailer(string(rest[len(marker):])))
	case len(rest) < len(marker) && strings.HasPrefix(marker, string(rest)):
		return r.fail(fmt.Errorf("reading the answer: it ends inside the marker of an error: %w", io.ErrUnexpectedEOF))
	}
	return r.fail(fmt.Errorf("reading the answer: %w", err))
}

// trailer returns the error that follows the marker. On 25.3 and 25.8 it is the
// text after a line break. On 26.9 it is a tag, the text, its length, the tag and
// the marker, as the response names its tag (measured).
func (r *rows) trailer(s string) error {
	s, ok := strings.CutPrefix(s, "\r\n")
	if !ok {
		return fmt.Errorf("reading the error after the rows: no line break after the marker: %w", dbimp.ErrInvalidValue)
	}
	if !r.tagged {
		e := newError(s, "")
		e.HTTPStatus = 200
		return e
	}
	tag, s, ok := strings.Cut(s, "\r\n")
	if !ok || tag == "" || r.tag != "" && tag != r.tag {
		return fmt.Errorf("reading the error after the rows: the tag %q is not the tag of the response: %w", tag, dbimp.ErrInvalidValue)
	}
	body, ok := strings.CutSuffix(s, " "+tag+"\r\n"+marker+"\r\n")
	if !ok {
		return fmt.Errorf("reading the error after the rows: it is cut short: %w", io.ErrUnexpectedEOF)
	}
	nl := strings.LastIndexByte(body, '\n')
	if n, err := strconv.Atoi(body[nl+1:]); nl < 0 || err != nil || n != nl+1 {
		return fmt.Errorf("reading the error after the rows: its length is not the one that it names: %w", dbimp.ErrInvalidValue)
	}
	e := newError(body[:nl+1], "")
	e.HTTPStatus = 200
	return e
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before the
	// read sees the end, and the read then fails with "use of closed network
	// connection". The caller must see that its context ended (D36).
	if cerr := r.w.cause(); cerr != nil && !errors.Is(err, cerr) {
		err = fmt.Errorf("%w: %w", cerr, err)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}
