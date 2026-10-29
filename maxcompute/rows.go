package maxcompute

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/xo/dbimp"
)

// formatCSV and formatText are the values of the attribute Format of a result.
const (
	formatCSV  = "csv"
	formatText = "text"
)

// maxDrain is the most that the driver reads after the end of the rows, to reach
// the end of the answer and give the connection back to the pool (D36). What
// follows the rows is a few closing tags.
const maxDrain = 64 << 10

// rows reads the result of one statement, one cell at a time (D25). The answer is
// XML, and the rows are the character data of one element, in CSV. The driver
// reads the answer of a statement that ended before it returns the rows, so the
// rows hold no context and no request (rule 4 of AGENTS.md). The one function
// that they hold is the cancel of the context of the timeout, which Close calls.
type rows struct {
	cr     *csvReader
	body   io.ReadCloser
	rest   io.Reader
	cancel context.CancelFunc

	cols []column
	loc  *time.Location
	// cur is the current row.
	cur []driver.Value
	// read is true once a row reached the caller, so that an error after it wraps
	// dbimp.ErrIncomplete (D107), and done once the result is read to its end.
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

// newRows returns the rows of the result that t holds, positioned before the first
// row. The types are those of EXPLAIN OUTPUT, and nil when the statement had none,
// so each column is a string. A result of the form text is one column.
func (c *Connector) newRows(ctx context.Context, o options, t *task, types []*typ, cancel context.CancelFunc) (driver.Rows, error) {
	if t.format != formatCSV {
		text, err := t.readText()
		if err != nil {
			return nil, err
		}
		if err := t.finish(); err != nil {
			return nil, err
		}
		return &textRows{text: text, cancel: cancel}, nil
	}
	cr := newCSVReader(t.decoded())
	head, err := cr.record()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("reading the names of the columns: the result is empty: %w", ErrCut)
		}
		return nil, fmt.Errorf("reading the names of the columns: %w", err)
	}
	if types == nil {
		types = make([]*typ, len(head))
		for i := range types {
			types[i] = stringType
		}
	}
	if len(types) != len(head) {
		return nil, fmt.Errorf("reading the result: %d columns, and EXPLAIN OUTPUT gave %d types: %w", len(head), len(types), dbimp.ErrColumnCount)
	}
	r := &rows{cr: cr, body: t.body, rest: t.body, cancel: cancel}
	for i, h := range head {
		r.cols = append(r.cols, column{name: h.text, typ: types[i]})
		if types[i].kind == kindTimestamp && r.loc == nil {
			if r.loc, err = c.zone(ctx, o.project, o.settings); err != nil {
				return nil, err
			}
		}
	}
	r.cur = make([]driver.Value, len(r.cols))
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	names := make([]string, len(r.cols))
	for i, c := range r.cols {
		names[i] = c.name
	}
	return names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName. It
// is the name of the type in upper case, with no parameters, such as DECIMAL
// (D219).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].typ.name
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D219).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.cols[i].typ)
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The answer of
// EXPLAIN OUTPUT has no nullability, so every column can be NULL and the driver
// does not know (recorded).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, false
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. It is the length of a
// VARCHAR or a CHAR column, and the other types have none.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	if l := r.cols[i].typ.length; l > 0 {
		return l, true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. It is
// the precision and the scale of a DECIMAL column.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	t := r.cols[i].typ
	if t.kind == kindDecimal && t.precision >= 0 {
		return t.precision, t.scale, true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more (D36).
func (r *rows) Close() error {
	r.cancel()
	if r.body == nil {
		return nil
	}
	err := r.body.Close()
	r.body = nil
	if err != nil {
		return fmt.Errorf("closing the rows: %w", err)
	}
	return nil
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the end of the answer, and
// returns its error (D21 and D36).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	rec, err := r.cr.record()
	switch {
	case errors.Is(err, io.EOF):
		return r.end()
	case err != nil:
		return r.fail(err)
	case len(rec) != len(r.cols):
		return r.fail(fmt.Errorf("reading a row: %d values for %d columns: %w", len(rec), len(r.cols), dbimp.ErrColumnCount))
	}
	for i, c := range rec {
		v, err := decode(r.cols[i].typ, c, r.loc)
		if err != nil {
			return r.fail(fmt.Errorf("reading the column %s: %w", r.cols[i].name, err))
		}
		r.cur[i] = v
	}
	r.read = true
	return nil
}

// Next satisfies driver.Rows, for a caller that does not use RowsColumnScanner.
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

// end reads what follows the rows, which is a few closing tags, so that the
// connection goes back to the pool (D36), and returns io.EOF.
func (r *rows) end() error {
	r.done = true
	if _, err := io.Copy(io.Discard, io.LimitReader(r.rest, maxDrain)); err != nil {
		return r.fail(fmt.Errorf("reading the end of the answer: %w", err))
	}
	if err := r.Close(); err != nil {
		return err
	}
	return io.EOF
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107), and ErrCut when the answer ended inside the
// result.
func (r *rows) fail(err error) error {
	r.done = true
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = fmt.Errorf("%w: %w", ErrCut, err)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// textRows is a result of the form text, which is the whole text as one column of
// one row (D219 item 4).
type textRows struct {
	text   string
	cancel context.CancelFunc
	done   bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnTypeDatabaseTypeName = (*textRows)(nil)
	_ driver.RowsColumnTypeScanType         = (*textRows)(nil)
	_ driver.RowsColumnTypeNullable         = (*textRows)(nil)
)

// Columns satisfies driver.Rows.
func (r *textRows) Columns() []string { return []string{"text"} }

// Close satisfies driver.Rows.
func (r *textRows) Close() error {
	r.cancel()
	return nil
}

// Next satisfies driver.Rows.
func (r *textRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.text
	return nil
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
func (r *textRows) ColumnTypeDatabaseTypeName(int) string { return nameString }

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType.
func (r *textRows) ColumnTypeScanType(int) reflect.Type { return typeOfString }

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable.
func (r *textRows) ColumnTypeNullable(int) (bool, bool) { return false, true }
