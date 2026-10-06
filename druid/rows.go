package druid

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/xo/dbimp"
)

// headerRows is the count of the rows of the header of an answer: the names,
// the native types and the SQL types of the columns (D164).
const headerRows = 3

// rows reads the answer of one query as arrayLines, one line at a time (D25
// and D164). Each line is a JSON array. The first three hold the names, the
// native types and the SQL types of the columns, and each line after them is
// a row. An answer that the server completed ends with an empty line. An
// error after some rows ends the answer with no empty line and no other sign
// (measured).
type rows struct {
	s    *dbimp.Stream
	dec  *jsontext.Decoder
	tail *tailReader
	w    *watch

	cols  []string
	types []column
	// cur is the current row.
	cur []driver.Value
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the answer is read to
	// its end.
	read bool
	done bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
)

// readAnswer reads the header of an answer. It reads the end of an answer
// that holds no rows, and returns its error, which comes before any row and
// so does not wrap dbimp.ErrIncomplete (D107). w is the watch of the query,
// which the rows end.
func readAnswer(res *http.Response, w *watch) (*rows, error) {
	tail := &tailReader{r: res.Body}
	s := dbimp.NewStream(tail)
	r := &rows{s: s, dec: s.Decoder(), tail: tail, w: w}
	if err := r.readHeader(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if r.dec.PeekKind() != '[' {
		if err := r.NextRow(); !errors.Is(err, io.EOF) {
			_ = s.Close()
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
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the SQL type of the header, such as BIGINT, or OTHER for a type that
// SQL has no name for, such as COMPLEX<json>.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.types[i].sql
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and
// D164).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.types[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The header
// does not say whether a column can be NULL, and every type can be (D164).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). It ends the watch of the query. A query that ran to its end needs
// no cancel, and one whose context ended was cancelled by the watch. Rows
// that the caller closes before the end cancel the query on the server too
// (D164).
func (r *rows) Close() error {
	err := r.s.Close()
	if r.done {
		r.w.end()
	} else {
		r.w.abort()
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the end of the answer,
// and returns an error if the answer was cut short (D21 and D164).
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

// readHeader reads the three rows of the header, and makes sure that they
// name the same count of columns.
func (r *rows) readHeader() error {
	var header [headerRows][]string
	for i := range header {
		row, err := r.readStrings()
		if errors.Is(err, io.EOF) {
			// An answer of HTTP 200 that ends before its header is cut short.
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			return fmt.Errorf("reading the header of the answer: %w", err)
		}
		header[i] = row
	}
	names, native, sqlTypes := header[0], header[1], header[2]
	if len(native) != len(names) || len(sqlTypes) != len(names) {
		return fmt.Errorf("reading the header of the answer: %d names, %d native types and %d SQL types: %w", len(names), len(native), len(sqlTypes), dbimp.ErrColumnCount)
	}
	r.cols = names
	r.types = make([]column, len(names))
	for i := range names {
		r.types[i] = column{sql: sqlTypes[i], native: native[i]}
	}
	r.cur = make([]driver.Value, len(names))
	return nil
}

// readStrings reads one line of the header, an array of strings.
func (r *rows) readStrings() ([]string, error) {
	if err := expect(r.dec, '['); err != nil {
		return nil, err
	}
	out := []string{}
	for r.dec.PeekKind() != ']' {
		v, err := r.dec.ReadValue()
		if err != nil {
			return nil, err
		}
		s, err := dbimp.String(v)
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
		// The value is decoded before the next call to the decoder, which
		// reuses its buffer.
		if r.cur[i], err = decode(r.types[i], v); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i], err)
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

// finish reads the end of the answer. It returns io.EOF for an answer that
// ends with an empty line, which the server completed, and an error for any
// other end.
func (r *rows) finish() error {
	r.done = true
	_, err := r.dec.ReadToken()
	switch {
	case err == nil:
		return r.fail(fmt.Errorf("reading the answer: a line that is not a row: %w", dbimp.ErrInvalidValue))
	case !errors.Is(err, io.EOF):
		return r.fail(fmt.Errorf("reading the answer: %w", err))
	case !r.tail.endsWithEmptyLine():
		return r.fail(fmt.Errorf("reading the answer: the answer ends with no empty line, so the server stopped the query: %w", ErrCut))
	}
	r.w.end()
	return io.EOF
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row
// reached the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before
	// the read sees the end, and the read then fails with "use of closed
	// network connection". The caller must see that its context ended (D36).
	if cerr := r.w.cause(); cerr != nil && !errors.Is(err, cerr) {
		err = fmt.Errorf("%w: %w", cerr, err)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// ErrCut is the error of an answer that ends with no empty line. The server
// ends an answer so when the query fails after its first rows, with HTTP 200
// and no other sign (measured). After a row, it comes wrapped with
// dbimp.ErrIncomplete (D107 and D164).
const ErrCut dbimp.Error = "the answer was cut short"

// tailReader reads a body, and keeps its last two bytes, so that the rows
// can tell whether the answer ended with an empty line. The decoder skips
// white space, so it cannot tell.
type tailReader struct {
	r    io.ReadCloser
	last [2]byte
	n    int64
}

// Read satisfies io.Reader.
func (t *tailReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	switch {
	case n >= 2:
		t.last = [2]byte{p[n-2], p[n-1]}
	case n == 1:
		t.last = [2]byte{t.last[1], p[0]}
	}
	t.n += int64(n)
	return n, err
}

// Close satisfies io.Closer.
func (t *tailReader) Close() error {
	return t.r.Close()
}

// endsWithEmptyLine reports whether what the reader read ends with an empty
// line: the end of the last line, and then an empty line.
func (t *tailReader) endsWithEmptyLine() bool {
	return t.n >= 2 && t.last == [2]byte{'\n', '\n'}
}

// expect reads one token, and returns an error if it is not the delimiter
// kind.
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
