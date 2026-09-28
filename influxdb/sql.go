package influxdb

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"

	"github.com/xo/dbimp"
)

// pathSQL is the endpoint of SQL, which only InfluxDB 3 and later have.
const pathSQL = "/api/v3/query_sql"

// sqlBody is the body of a request of SQL (measured).
type sqlBody struct {
	DB     string         `json:"db,omitzero"`
	Q      string         `json:"q"`
	Format string         `json:"format"`
	Params jsontext.Value `json:"params,omitzero"`
}

// describeRow is one row of the answer to DESCRIBE (measured).
type describeRow struct {
	Name     string `json:"column_name"`
	Type     string `json:"data_type"`
	Nullable string `json:"is_nullable"`
}

// querySQL sends a statement of SQL. With describe=always, it sends DESCRIBE
// first, and reads the columns and their types from it (D80).
func (c *conn) querySQL(ctx context.Context, query string, p jsontext.Value) (*sqlRows, error) {
	var types []*colType
	if c.c.cfg.Describe == DescribeAlways {
		var err error
		if types, err = c.describe(ctx, query, p); err != nil {
			return nil, err
		}
	}
	res, err := c.post(ctx, query, p)
	if err != nil {
		return nil, err
	}
	s := dbimp.NewStream(res.Body)
	var cols []string
	if types != nil {
		cols = make([]string, 0, len(types))
	}
	for _, t := range types {
		cols = append(cols, t.name)
	}
	obj, err := dbimp.NewObjectRows(s.Decoder(), cols)
	if err != nil {
		_ = s.Close()
		return nil, incomplete(err)
	}
	return &sqlRows{s: s, obj: obj, types: types, vals: make([]jsontext.Value, len(obj.Columns()))}, nil
}

// post sends one statement of SQL.
func (c *conn) post(ctx context.Context, query string, p jsontext.Value) (*http.Response, error) {
	b, err := json.Marshal(sqlBody{DB: c.c.cfg.Database, Q: query, Format: "json", Params: p})
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	return c.c.send(ctx, pathSQL, "application/json", b)
}

// describe sends DESCRIBE and the statement, and returns the type of each
// column. It returns nil types when DESCRIBE fails for any reason but the end
// of the context, because the server refuses DESCRIBE for a statement that is
// not a SELECT, with a status that differs by release (D80). The statement
// then runs alone, and a statement that is wrong fails with its own error.
func (c *conn) describe(ctx context.Context, query string, p jsontext.Value) ([]*colType, error) {
	res, err := c.post(ctx, "DESCRIBE "+query, p)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nil
	}
	s := dbimp.NewStream(res.Body)
	defer s.Close()
	var desc []describeRow
	if err := json.UnmarshalDecode(s.Decoder(), &desc); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("reading the answer to DESCRIBE: %w", err)
		}
		return nil, nil
	}
	types := make([]*colType, len(desc))
	for i, d := range desc {
		types[i] = columnType(d.Type, d.Nullable != "NO")
		types[i].name = d.Name
	}
	return types, nil
}

// incomplete wraps an error of the read of a result. The server closes the
// body before its end when a statement fails after some rows, and sends no
// text of the error (measured), so the end of the body is dbimp.ErrIncomplete.
func incomplete(err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("reading the result: the server closed it before its end: %w: %w", dbimp.ErrIncomplete, err)
	}
	return fmt.Errorf("reading the result: %w", err)
}

// sqlRows reads the answer of SQL, an array of objects, one token at a time
// (D25). The types are nil with describe=disable, or when DESCRIBE failed.
type sqlRows struct {
	s     *dbimp.Stream
	obj   *dbimp.ObjectRows
	types []*colType
	vals  []jsontext.Value
	done  bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*sqlRows)(nil)
	_ driver.RowsColumnTypeScanType         = (*sqlRows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*sqlRows)(nil)
	_ driver.RowsColumnTypeNullable         = (*sqlRows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*sqlRows)(nil)
)

// Columns satisfies driver.Rows.
func (r *sqlRows) Columns() []string {
	return r.obj.Columns()
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36).
func (r *sqlRows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner.
func (r *sqlRows) NextRow() error {
	if r.done {
		return io.EOF
	}
	err := r.obj.Next(r.vals)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF):
		r.done = true
		if err := r.s.End(); err != nil {
			return err
		}
		return io.EOF
	}
	r.done = true
	return incomplete(err)
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *sqlRows) Next(dest []driver.Value) error {
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
func (r *sqlRows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	v, err := r.value(i)
	if err != nil {
		return err
	}
	return dbimp.Assign(scanCtx, dest, v)
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType.
func (r *sqlRows) ColumnTypeScanType(i int) reflect.Type {
	return r.typ(i).scanType()
}

// ColumnTypeDatabaseTypeName satisfies
// driver.RowsColumnTypeDatabaseTypeName. It is the data_type of DESCRIBE in
// upper case, or "" without DESCRIBE.
func (r *sqlRows) ColumnTypeDatabaseTypeName(i int) string {
	return r.typ(i).databaseTypeName()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable, from
// is_nullable of DESCRIBE.
func (r *sqlRows) ColumnTypeNullable(i int) (bool, bool) {
	if t := r.typ(i); t != nil {
		return t.nullable, true
	}
	return false, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale,
// for a decimal.
func (r *sqlRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if t := r.typ(i); t != nil && t.precision >= 0 {
		return t.precision, t.scale, true
	}
	return 0, 0, false
}

// value decodes the value of column i. A key that the row does not hold is a
// NULL (D80). A float that is not finite arrives as an explicit null, which
// is a NaN, because the JSON writes NaN, +Inf and -Inf alike (measured).
func (r *sqlRows) value(i int) (any, error) {
	v := r.vals[i]
	if t := r.typ(i); t != nil && t.float && len(v) > 0 && v.Kind() == 'n' {
		return math.NaN(), nil
	}
	if dbimp.IsNull(v) {
		return nil, nil
	}
	if t := r.typ(i); t != nil {
		d, err := t.decode(v)
		if err != nil {
			return nil, fmt.Errorf("reading the column %q: %w", r.Columns()[i], err)
		}
		return d, nil
	}
	return dbimp.Any(v)
}

// typ returns the type of column i, or nil.
func (r *sqlRows) typ(i int) *colType {
	if i < len(r.types) {
		return r.types[i]
	}
	return nil
}
