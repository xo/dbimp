package tablestore

import (
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"

	"github.com/xo/dbimp"
)

// rows reads the answer to one statement. The answer is a protobuf message with
// the rows in one field, as a FlatBuffers message, and the driver reads the rows
// from the body when a row asks for a value (D21 and D36). The rows keep no
// context, because the statement has ended on the server and each read comes
// from the body, which the context of the request already bounds.
type rows struct {
	body io.Closer
	resp *response
	cols []column
	cur  []driver.Value
	// next is the index of the next row.
	next int
	// read is true once a row reached the caller, so that an error after it wraps
	// dbimp.ErrIncomplete (D107).
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

// newRows returns the rows of resp, which holds the answer up to its first row.
func newRows(body io.Closer, resp *response) *rows {
	r := &rows{body: body, resp: resp}
	if resp.rs != nil {
		r.cols = resp.rs.cols
		r.cur = make([]driver.Value, len(r.cols))
	}
	return r
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	names := make([]string, len(r.cols))
	for i := range r.cols {
		names[i] = r.cols[i].name
	}
	return names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D223).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return r.cols[i].scanType()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The rows mark a
// NULL for any column, and the server says nothing of the constraint, so every
// column can be NULL (D223).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). The statement has ended on the server, so there is nothing to stop.
func (r *rows) Close() error {
	return r.body.Close()
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row it reads the rest of the answer (D21
// and D36).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	if r.resp.rs == nil || r.next >= r.resp.rs.count {
		if err := r.resp.finish(); err != nil {
			return r.fail(err)
		}
		r.done = true
		return io.EOF
	}
	for i := range r.cols {
		v, err := r.cols[i].decode(r.next)
		if err != nil {
			return r.fail(err)
		}
		r.cur[i] = v
	}
	r.next++
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

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// result returns the result of a statement that ran with Exec, whose answer the
// driver read to the end.
func (r *rows) result() result {
	return result{affected: r.resp.affected, known: r.resp.hasAffected}
}
