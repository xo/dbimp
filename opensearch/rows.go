package opensearch

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/xo/dbimp"
)

// maxSkip is the most that Close reads of the rest of a page, to find the
// cursor that follows its rows, so that it can close the cursor (D168). A
// page of the default size is far smaller. A page that is larger leaves its
// cursor to the server, which drops it when its keep_alive ends.
const maxSkip = 256 << 10

// rows reads the answer of one statement, one page at a time (D25 and D168).
// A page is a JSON object of the jdbc format. The first page holds schema, an
// array of the name, the alias and the type of each column, and datarows, an
// array of arrays in the order of schema, and cursor if more rows follow. A
// page of the legacy engine after the first holds no schema. The server writes
// the members in no fixed order, and the cursor can come before the rows or
// after them (measured). A page of an error holds error and status, with HTTP
// 200 for some errors (measured).
//
// Each cursor serves once, so the rows never read a page again (measured).
// The rows keep the context of the statement, because the request for each
// next page needs it (D168).
type rows struct {
	ctx context.Context //nolint:containedctx // The request for each next page needs the context of the statement (D168).
	c   *Connector

	s     *dbimp.Stream
	meter *meter
	dec   *jsontext.Decoder
	arr   *dbimp.ArrayRows

	cols  []string
	types []string
	vals  []jsontext.Value
	// cur is the current row.
	cur []driver.Value
	// next is the cursor for the next page. It is set when the page names it,
	// before or after its rows.
	next string
	// status is the status that the body of the page names, for an error.
	status int
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
// bound what it reads (D168).
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
func newRows(ctx context.Context, c *Connector, body io.ReadCloser) *rows {
	r := &rows{ctx: ctx, c: c}
	r.setBody(body)
	return r
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the type of the column in upper case, such as INTEGER or GEO_POINT
// (D168).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return databaseTypeName(r.types[i])
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and
// D168).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.types[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The answer does
// not say whether a column can be NULL, and every type can be (D168).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// of the answer, with one exception. Rows that the caller closes before the
// end read the rest of the current page, at most maxSkip bytes, to learn the
// cursor, and close it on the server (D168). The context of the statement
// ends the read, so a statement whose context ended closes the cursor that it
// knows, and a page that it did not finish leaves its cursor to the server,
// which drops it when its keep_alive ends.
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
// answer (D21 and D168).
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
	return r.beginPage()
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

// beginPage reads a page up to the start of its rows. It reads the schema of
// the first page, and skips every member that it does not know. An error in
// the page is returned.
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
		case "datarows":
			if r.cols == nil {
				return fmt.Errorf("reading the answer: the rows come before the schema: %w", dbimp.ErrInvalidValue)
			}
			if r.arr, err = dbimp.NewArrayRows(r.dec, len(r.cols)); err != nil {
				return err
			}
			return nil
		case "schema":
			err = r.readSchema()
		case "cursor":
			err = r.readCursor()
		case "status":
			err = r.readStatus()
		case "error":
			// The error of the server is returned as it is.
			return r.readError()
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

// readSchema reads the array schema of the first page. A later page that
// holds it again is skipped, because the columns of a statement do not
// change. A column has the label that AS gave it, in alias, and else the
// text of its expression, in name (measured). The legacy engine writes an
// empty alias for an object and a nested field, which is no label.
func (r *rows) readSchema() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return err
	}
	if r.cols != nil {
		return nil
	}
	var cols []struct {
		Name  string `json:"name"`
		Alias string `json:"alias"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(v, &cols); err != nil {
		return fmt.Errorf("reading the schema: %w: %w", dbimp.ErrInvalidValue, err)
	}
	r.cols, r.types = make([]string, len(cols)), make([]string, len(cols))
	for i, c := range cols {
		r.cols[i], r.types[i] = c.Name, c.Type
		if c.Alias != "" {
			r.cols[i] = c.Alias
		}
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

// readStatus reads the member status, which a page of an error needs.
func (r *rows) readStatus() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return err
	}
	if n, err := dbimp.Int64(v); err == nil {
		r.status = int(n)
	}
	return nil
}

// readError reads the member error of a page, and the members after it up to
// the end of the object, for the status, and returns the error. A page with
// HTTP 200 can hold one, and the statement failed all the same (measured).
func (r *rows) readError() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading the error: %w", err)
	}
	detail := v.Clone()
	for r.dec.PeekKind() != '}' && r.dec.PeekKind() != 0 {
		name, err := r.name()
		if err != nil {
			return fmt.Errorf("reading the error: %w", err)
		}
		if name == "status" {
			err = r.readStatus()
		} else {
			err = r.dec.SkipValue()
		}
		if err != nil {
			return fmt.Errorf("reading the error: %q: %w", name, err)
		}
	}
	// Read the end of the object and of the body, so that the connection can
	// serve the next request. The statement failed, and a failure here does
	// not matter.
	if _, err := r.dec.ReadToken(); err == nil {
		_ = r.s.End()
	}
	body := `{"error":` + string(detail)
	if r.status != 0 {
		body += `,"status":` + strconv.Itoa(r.status)
	}
	return newError(http.StatusOK, body+"}", nil)
}

// endPage reads the members after the rows of a page, which hold the cursor,
// and then the end of the body, so that the connection can serve the next
// request (D36). A cursor that came before the rows stays, and a page that
// names none ends the statement.
func (r *rows) endPage() error {
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
		case "status":
			err = r.readStatus()
		case "error":
			return r.readError()
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
// up to its first row. The cursor serves once, so the driver forgets it as it
// sends it. The request wraps no driver.ErrBadConn, because the statement
// reached the server (D8).
func (r *rows) nextPage() error {
	cursor := r.next
	r.next = ""
	body, err := dbimp.MarshalParams(request{Cursor: cursor}, nil)
	if err != nil {
		return fmt.Errorf("writing the request for the next page: %w", err)
	}
	if err := r.s.Close(); err != nil {
		return fmt.Errorf("closing the page: %w", err)
	}
	res, err := r.c.post(r.ctx, sqlPath, body, false)
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
