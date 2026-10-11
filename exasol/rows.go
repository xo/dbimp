package exasol

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/xo/dbimp"
)

// rows reads a result set (D210). The first answer holds the first rows, and a
// result of more rows has a handle, which the fetch command reads from by the
// position of a row. The data of an answer is a list of columns, so one decoder
// for each column reads the values of that column, and a row is one value from
// each of them.
type rows struct {
	c *conn
	// ctx is the context of the query, which a fetch and the close of the
	// handle take, because database/sql calls Next and Close with none (D210
	// and hard rule 4). The close takes it without its end.
	ctx    context.Context //nolint:containedctx // Next and Close of driver.Rows take no context (D210).
	cancel context.CancelFunc

	names   []string
	cols    []column
	handle  int64
	total   int64
	fetched int64
	// page is the page that the decoders read: the rows left in it.
	page []*jsontext.Decoder
	left int64
	loc  func() (*time.Location, error)

	cur []any
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the rows ended.
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
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
)

// newRows returns the rows of the result set rs. It reads the first page.
func (c *conn) newRows(ctx context.Context, cancel context.CancelFunc, rs *resultSet) (*rows, error) {
	r := &rows{
		c:      c,
		ctx:    ctx,
		cancel: cancel,
		cols:   columnsOf(rs.Columns),
		handle: rs.Handle,
		total:  rs.NumRows,
		loc:    c.s.location,
	}
	r.names = make([]string, len(r.cols))
	for i, col := range r.cols {
		r.names[i] = col.name
	}
	r.cur = make([]any, len(r.cols))
	if len(rs.Columns) != rs.NumColumns && rs.NumColumns != 0 {
		return nil, r.fail(fmt.Errorf("reading the columns: %d columns for numColumns %d: %w", len(rs.Columns), rs.NumColumns, dbimp.ErrColumnCount))
	}
	// The first message holds numRowsInMessage rows. A result with no handle
	// holds all of its rows, and numRowsInMessage is then numRows.
	if err := r.setPage(rs.Data, rs.NumRowsInMessage); err != nil {
		return nil, r.fail(err)
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D206).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return r.cols[i].scanType()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The answer does
// not name NOT NULL, so the driver says that each column can hold NULL.
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. It is the number of
// characters of a CHAR and a VARCHAR, and the number of bytes of a HASHTYPE,
// whose size on the wire is the number of hex digits.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	c := r.cols[i]
	switch c.kind {
	case kindChar, kindVarchar:
		return int64(c.typ.Size), true
	case kindHashtype:
		return int64(c.typ.Size / 2), true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. A
// DECIMAL has both.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	c := r.cols[i]
	if c.kind != kindDecimal && c.kind != kindInteger {
		return 0, 0, false
	}
	return int64(c.typ.Precision), int64(c.typ.Scale), true
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. It fetches the next page when the page ends, and after the
// last row it closes the handle (D36 and D210).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	if r.left == 0 {
		switch more, err := r.fetch(); {
		case err != nil:
			return r.fail(err)
		case !more:
			if err := r.finish(); err != nil {
				return err
			}
			return io.EOF
		}
	}
	for i, dec := range r.page {
		if dec.PeekKind() == ']' {
			return r.fail(fmt.Errorf("reading the column %s: fewer values than rows: %w", r.names[i], dbimp.ErrColumnCount))
		}
		v, err := dec.ReadValue()
		if err != nil {
			return r.fail(fmt.Errorf("reading the column %s: %w", r.names[i], err))
		}
		if r.cur[i], err = r.cols[i].decode(v, r.loc); err != nil {
			return r.fail(fmt.Errorf("reading the column %s: %w", r.names[i], err))
		}
	}
	r.left--
	r.read = true
	return nil
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	for i, v := range r.cur {
		dest[i] = v
	}
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.cur[i])
}

// Close satisfies driver.Rows. It closes the handle of the result set, and
// reads no more rows (D36 and D210).
func (r *rows) Close() error {
	if r.closed {
		return nil
	}
	return r.release(r.ctx)
}

// setPage makes the data the page that Next reads, of n rows.
func (r *rows) setPage(data columnData, n int64) error {
	r.page, r.left = nil, 0
	if n == 0 {
		return nil
	}
	if len(data) != len(r.cols) {
		return fmt.Errorf("reading the data: %d columns for %d columns: %w", len(data), len(r.cols), dbimp.ErrColumnCount)
	}
	page := make([]*jsontext.Decoder, len(data))
	for i, col := range data {
		dec := jsontext.NewDecoder(bytes.NewReader(col))
		tok, err := dec.ReadToken()
		if err != nil || tok.Kind() != '[' {
			return fmt.Errorf("reading the data of the column %s: not a list of values: %w", r.names[i], dbimp.ErrInvalidValue)
		}
		page[i] = dec
	}
	r.page, r.left = page, n
	r.fetched += n
	return nil
}

// fetch reads the next page, and reports whether there is a row. A result with
// no handle has no next page.
func (r *rows) fetch() (bool, error) {
	if r.handle == 0 || r.fetched >= r.total {
		return false, nil
	}
	env, err := r.c.s.call(r.ctx, fetchRequest{
		Command:         cmdFetch,
		ResultSetHandle: r.handle,
		StartPosition:   r.fetched,
		NumBytes:        r.c.c.cfg.FetchSize << 10,
	})
	if err != nil {
		return false, fmt.Errorf("fetching the rows from %d: %w", r.fetched, err)
	}
	n := env.ResponseData.NumRows
	if n <= 0 {
		return false, fmt.Errorf("fetching the rows from %d: the answer holds no row: %w", r.fetched, dbimp.ErrIncomplete)
	}
	if err := r.setPage(env.ResponseData.Data, n); err != nil {
		return false, err
	}
	return true, nil
}

// fail ends the rows with err, which wraps dbimp.ErrIncomplete after a row
// reached the caller (D107).
func (r *rows) fail(err error) error {
	r.done = true
	if rerr := r.release(r.ctx); rerr != nil {
		err = errors.Join(err, rerr)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// finish ends the rows at the last row.
func (r *rows) finish() error {
	r.done = true
	return r.release(r.ctx)
}

// release closes the handle, and ends the bound of WithTimeout.
func (r *rows) release(ctx context.Context) error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.cancel != nil {
		defer r.cancel()
	}
	if r.handle == 0 {
		return nil
	}
	ctx, cancel := closeContext(ctx)
	defer cancel()
	if _, err := r.c.s.call(ctx, closeResultRequest{Command: cmdCloseResult, Handles: []int64{r.handle}}); err != nil {
		if r.c.s.failure() != nil {
			// The session ended, and the server freed the handle with it.
			return nil
		}
		return fmt.Errorf("closing the result set %d: %w", r.handle, err)
	}
	return nil
}

// closeContext returns the context for a request that frees something on the
// server. If the context of the caller ended, the request still goes out, with
// a bound of its own, so that the server frees the object (D210).
func closeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
}

// noRows is the result of a statement that returns a count and not rows, run
// with Query: no column and no row.
type noRows struct{}

func (noRows) Columns() []string         { return []string{} }
func (noRows) Close() error              { return nil }
func (noRows) Next([]driver.Value) error { return io.EOF }
