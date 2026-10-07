package elasticsearch

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// maxSkip is the most that Close reads of the rest of a page, to find the
// cursor that follows its rows, so that it can close the cursor (D167). A
// page of the default size is far smaller. A page that is larger leaves its
// cursor to the server, which drops it when its keep_alive ends.
const maxSkip = 256 << 10

// rows reads the answer of one statement, one page at a time (D25 and D167).
// A page is a JSON object. The first page holds columns, the names and the
// types of the columns, then rows, an array of arrays in the order of
// columns, and cursor if more rows follow. Each later page holds rows and
// cursor, and no columns (measured). A cursor that the last page lacks is
// closed by the server.
//
// The rows keep the context of the statement, because the request for each
// next page needs it (D167).
type rows struct {
	ctx context.Context //nolint:containedctx // The request for each next page needs the context of the statement (D167).
	c   *Connector
	o   options

	s     *dbimp.Stream
	meter *meter
	dec   *jsontext.Decoder
	arr   *dbimp.ArrayRows

	cols  []string
	types []string
	vals  []jsontext.Value
	// cur is the current row.
	cur []driver.Value
	// next is the cursor for the next page, which the end of a page holds.
	next string
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107). done is true once the answer is read
	// to its end or failed, and closed once Close ran.
	read   bool
	done   bool
	closed bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
)

// meter counts the bytes that the driver reads of a body, so that Close can
// bound what it reads (D167).
type meter struct {
	r io.ReadCloser
	n int64
}

// Read satisfies io.Reader.
func (m *meter) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	m.n += int64(n)
	return n, err
}

// Close satisfies io.Closer.
func (m *meter) Close() error {
	return m.r.Close()
}

// newRows returns rows that read body, the first page of a statement.
func newRows(ctx context.Context, c *Connector, o options, body io.ReadCloser) *rows {
	r := &rows{ctx: ctx, c: c, o: o}
	r.setBody(body)
	return r
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the type of the column in upper case, such as INTEGER or
// INTERVAL_DAY_TO_SECOND, as SYS TYPES names it (D167).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return strings.ToUpper(r.types[i])
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and
// D167).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.types[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The answer does
// not say whether a column can be NULL, and every type can be (D167).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// of the answer, with one exception. Rows that the caller closes before the
// end read the rest of the current page, at most maxSkip bytes, to learn the
// cursor, and close it on the server (D167). The context of the statement
// ends the read, so a statement whose context ended closes no cursor, which
// the server drops when its keep_alive ends.
func (r *rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.done {
		return r.s.Close()
	}
	cursor := r.next
	if cursor == "" && r.ctx.Err() == nil {
		cursor = r.skipPage()
	}
	err := r.s.Close()
	if cursor != "" {
		// A close that fails leaves the cursor to the server, which drops it
		// when its keep_alive ends, and the caller has left.
		_ = r.c.stop(r.ctx, cursor)
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. At the end of a page, it reads the next page by the
// cursor, and after the last row of the last page, it reads the end of the
// answer (D21 and D167).
func (r *rows) NextRow() error {
	for {
		if r.done {
			return io.EOF
		}
		err := r.arr.Next(r.vals)
		switch {
		case err == nil:
			if err := r.decodeRow(); err != nil {
				return r.fail(err)
			}
			r.read = true
			return nil
		case !errors.Is(err, io.EOF):
			return r.fail(err)
		}
		if err := r.endPage(); err != nil {
			return r.fail(err)
		}
		if r.next == "" {
			r.done = true
			return io.EOF
		}
		if err := r.nextPage(); err != nil {
			return r.fail(err)
		}
	}
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

// setBody makes body the page that the rows read.
func (r *rows) setBody(body io.ReadCloser) {
	r.meter = &meter{r: body}
	r.s = dbimp.NewStream(r.meter)
	r.dec = r.s.Decoder()
	r.arr = nil
}

// open reads the first page up to its first row. An error here comes before
// any row, so it does not wrap dbimp.ErrIncomplete (D107).
func (r *rows) open() error {
	if err := r.beginPage(); err != nil {
		return err
	}
	if r.cols == nil {
		return fmt.Errorf("reading the answer: no columns: %w", dbimp.ErrInvalidValue)
	}
	return nil
}

// skipPage reads the rest of the current page, and returns its cursor, or ""
// if it has none or the read passed maxSkip or failed. The read ends after
// stopTimeout, which closes the body, so a server that stops sending cannot
// hold Close. When the page ends, it reads the end of the body, so that the
// connection goes back to the pool.
func (r *rows) skipPage() string {
	if r.arr == nil {
		return ""
	}
	timer := time.AfterFunc(stopTimeout, func() { _ = r.s.Close() })
	defer timer.Stop()
	start := r.meter.n
	for r.dec.PeekKind() != ']' {
		if r.dec.PeekKind() == 0 || r.meter.n-start > maxSkip {
			return ""
		}
		if err := r.dec.SkipValue(); err != nil {
			return ""
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return ""
	}
	if r.endObject(maxSkip, start) != nil {
		return ""
	}
	_ = r.s.End()
	return r.next
}

// decodeRow decodes the values of the row that ArrayRows read. The values
// are decoded before the next call to the decoder, which reuses its buffer.
func (r *rows) decodeRow() error {
	for i, v := range r.vals {
		val, err := decode(r.types[i], v)
		if err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i], err)
		}
		r.cur[i] = val
	}
	return nil
}

// beginPage reads a page up to the start of its rows. It reads the columns
// of the first page, and skips every member that it does not know.
func (r *rows) beginPage() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		name, err := r.name()
		if err != nil {
			return err
		}
		switch name {
		case "rows":
			if r.cols == nil {
				return fmt.Errorf("reading the answer: the rows come before the columns: %w", dbimp.ErrInvalidValue)
			}
			if r.arr, err = dbimp.NewArrayRows(r.dec, len(r.cols)); err != nil {
				return err
			}
			return nil
		case "columns":
			err = r.readColumns()
		case "cursor":
			err = r.readCursor()
		case "error":
			err = r.readError()
		default:
			err = r.dec.SkipValue()
		}
		if err != nil {
			return fmt.Errorf("reading %q: %w", name, err)
		}
	}
	return fmt.Errorf("reading the answer: no rows: %w", dbimp.ErrInvalidValue)
}

// name reads the name of a member of the answer.
func (r *rows) name() (string, error) {
	tok, err := r.dec.ReadToken()
	if err != nil {
		return "", fmt.Errorf("reading the answer: %w", err)
	}
	if tok.Kind() != '"' {
		return "", fmt.Errorf("reading the answer: %v where a name was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
	}
	return tok.String(), nil
}

// readColumns reads the array columns of the first page. A later page that
// holds it again is skipped, because the columns of a statement do not
// change.
func (r *rows) readColumns() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return err
	}
	if r.cols != nil {
		return nil
	}
	var cols []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(v, &cols); err != nil {
		return fmt.Errorf("reading the columns: %w: %w", dbimp.ErrInvalidValue, err)
	}
	r.cols, r.types = make([]string, len(cols)), make([]string, len(cols))
	for i, c := range cols {
		r.cols[i], r.types[i] = c.Name, c.Type
	}
	r.vals = make([]jsontext.Value, len(cols))
	r.cur = make([]driver.Value, len(cols))
	return nil
}

// readCursor reads the member cursor.
func (r *rows) readCursor() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return err
	}
	if dbimp.IsNull(v) {
		r.next = ""
		return nil
	}
	r.next, err = dbimp.String(v)
	return err
}

// readError reads a member error of a page of HTTP 200, and returns it. The
// server sent none in the measurements, and a page that holds one is an
// error all the same.
func (r *rows) readError() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return err
	}
	body := `{"error":` + string(v) + `}`
	return newError(&dbimp.StatusError{Code: 200, Body: body})
}

// endPage reads the members after the rows of a page, which hold the cursor,
// and then the end of the body, so that the connection can serve the next
// request (D36).
func (r *rows) endPage() error {
	r.next = ""
	if err := r.endObject(-1, 0); err != nil {
		return err
	}
	return r.s.End()
}

// endObject reads the members of the page after its rows, up to the end of
// the object. If limit is not negative, it fails once the body passed limit
// bytes after start.
func (r *rows) endObject(limit, start int64) error {
	for r.dec.PeekKind() != '}' {
		if limit >= 0 && r.meter.n-start > limit {
			return fmt.Errorf("reading the end of the page: more than %d bytes: %w", limit, dbimp.ErrInvalidValue)
		}
		name, err := r.name()
		if err != nil {
			return err
		}
		switch name {
		case "cursor":
			err = r.readCursor()
		case "error":
			err = r.readError()
		default:
			err = r.dec.SkipValue()
		}
		if err != nil {
			return fmt.Errorf("reading %q: %w", name, err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the page: %w", err)
	}
	return nil
}

// nextPage sends the cursor of the page that ended, and reads the next page
// up to its first row. The request wraps no driver.ErrBadConn, because the
// statement reached the server (D8).
func (r *rows) nextPage() error {
	cursor := r.next
	r.next = ""
	body, err := dbimp.MarshalParams(r.o.next(cursor), nil)
	if err != nil {
		return fmt.Errorf("writing the request for the next page: %w", err)
	}
	if err := r.s.Close(); err != nil {
		return fmt.Errorf("closing the page: %w", err)
	}
	res, err := r.c.post(r.ctx, "/_sql", body, false)
	if err != nil {
		return fmt.Errorf("reading the next page: %w", err)
	}
	r.setBody(res.Body)
	if err := r.beginPage(); err != nil {
		return fmt.Errorf("reading the next page: %w", err)
	}
	return nil
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row
// reached the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before
	// the read sees the end, and the read then fails with "use of closed
	// network connection". The caller must see that its context ended (D36).
	if cerr := r.ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
		err = fmt.Errorf("%w: %w", cerr, err)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
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
