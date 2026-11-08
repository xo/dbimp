package hive

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/xo/dbimp"
)

// rows reads the result of one operation page by page (D21 and D235). A page
// holds the columns of up to fetchSize rows, and the driver holds one page.
type rows struct {
	c         *conn
	op        *opHandle
	cols      []colDesc
	names     []string
	fetchSize int

	// ctx is the context of the statement, which each FetchResults uses, because
	// the rows read after QueryContext returns (hard rule 4).
	ctx context.Context //nolint:containedctx // The rows read lazily, after QueryContext returns (D235).
	// cancel ends the bound of WithTimeout on the statement.
	cancel context.CancelFunc

	pg  *page
	pos int
	cur []driver.Value
	// read is true once a row reached the caller, so that an error after it wraps
	// dbimp.ErrIncomplete (D107).
	read bool
	done bool

	once sync.Once
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
	_ driver.RowsColumnTypeLength           = (*rows)(nil)
)

// openRows reads the metadata of the operation, and its first page, so that an
// error of the statement reaches the caller before any row does. A statement with
// no result set has no column and no row (docs/HIVE.md, Responses).
func (c *conn) openRows(ctx context.Context, op *opHandle, fetchSize int) (*rows, error) {
	r := &rows{c: c, op: op, ctx: ctx, fetchSize: fetchSize, names: []string{}}
	if !op.hasResultSet {
		r.done = true
		return r, nil
	}
	cols, err := c.c.resultSetMetadata(ctx, op)
	if err != nil {
		return nil, err
	}
	r.cols = cols
	r.names = make([]string, len(cols))
	for i, col := range cols {
		r.names[i] = col.name
	}
	r.cur = make([]driver.Value, len(cols))
	if err := r.fetch(); err != nil {
		return nil, err
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return typeName(r.cols[i].typeID)
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.cols[i].typeID)
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The metadata does
// not name NOT NULL, and a column holds NULL unless its table forbids it, so the
// driver says that each column can.
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. A
// DECIMAL names both in its type qualifiers.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if r.cols[i].typeID != typeDecimal {
		return 0, 0, false
	}
	return int64(r.cols[i].precision), int64(r.cols[i].scale), true
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. A VARCHAR and a CHAR
// name their length in the type qualifiers.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	switch r.cols[i].typeID {
	case typeVarchar, typeChar:
		return int64(r.cols[i].length), true
	}
	return 0, false
}

// Close satisfies driver.Rows. It closes the operation, which stops the work of
// the server, and reads nothing more (D36 and D235).
func (r *rows) Close() error {
	r.once.Do(func() {
		if r.cancel != nil {
			defer r.cancel()
		}
		r.done = true
		r.c.release(context.WithoutCancel(r.ctx), r.op)
	})
	return nil
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. It reads the next page when the page ends, and returns
// io.EOF at the first empty page.
func (r *rows) NextRow() error {
	if r.done && (r.pg == nil || r.pos >= r.pg.rows) {
		return io.EOF
	}
	for r.pg == nil || r.pos >= r.pg.rows {
		if r.pg != nil && r.pg.rows == 0 {
			r.done = true
			return io.EOF
		}
		if r.pg != nil && !r.pg.hasMore && r.c.flavor == flavorImpala {
			// Impala says when no more rows are left, and Hive never does.
			r.done = true
			return io.EOF
		}
		if err := r.fetch(); err != nil {
			if r.read {
				return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
			}
			return err
		}
	}
	for i := range r.cols {
		v, err := decode(&r.cols[i], &r.pg.cols[i], r.pos)
		if err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i].name, err)
		}
		r.cur[i] = v
	}
	r.pos++
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

// fetch reads the next page. An empty page is the end of the rows, because Hive
// reports no more rows on any page (docs/HIVE.md, Responses).
func (r *rows) fetch() error {
	pg, err := r.c.c.fetchResults(r.ctx, r.op, r.fetchSize)
	if err != nil {
		return r.c.fail(err)
	}
	if len(pg.cols) != len(r.cols) && pg.rows > 0 {
		return fmt.Errorf("reading a page: %d columns for %d: %w", len(pg.cols), len(r.cols), dbimp.ErrColumnCount)
	}
	r.pg, r.pos = pg, 0
	return nil
}
