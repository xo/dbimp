package pinot

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/xo/dbimp"
)

// ErrPartial is the error of an answer that the server marked partialResult
// without an exception, as a join does when it stops at maxRowsInJoin with
// joinOverflowMode=BREAK (measured). After a row, it comes wrapped with
// dbimp.ErrIncomplete (D133).
const ErrPartial dbimp.Error = "the server marked the result as partial"

// rows reads the answer of one query one token at a time (D25 and D133). The
// answer is one JSON object, and resultTable comes first in it, with
// dataSchema and then rows, and exceptions and partialResult follow
// (measured).
type rows struct {
	s          *dbimp.Stream
	dec        *jsontext.Decoder
	httpStatus int

	cols  []string
	types []string
	arr   *dbimp.ArrayRows
	vals  []jsontext.Value
	// cur is the current row.
	cur []driver.Value
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the answer is read to
	// its end.
	read bool
	done bool

	exceptions []Exception
	partial    bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
)

// readAnswer reads an answer up to its first row. It returns the error of the
// server for an answer that holds no rows, which is how every failed query
// arrives, and for an answer whose rows are empty and whose exceptions
// follow them. A failure before any row does not wrap dbimp.ErrIncomplete,
// which is for a failure after a row (D107).
func readAnswer(res *http.Response) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), httpStatus: res.StatusCode, cols: []string{}}
	if err := r.readHead(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if r.arr == nil || r.dec.PeekKind() == ']' {
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
// It is the type that dataSchema names, such as LONG or INT_ARRAY.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.types[i]
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D130).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.types[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. Every column
// can be NULL, because each query sends enableNullHandling=true (D130).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). The Broker sends the answer after the query ends, so a query that
// sent its answer needs no cancel.
func (r *rows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the rest of the answer,
// and returns the error that it holds (D36 and D133).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	if r.arr == nil {
		return r.finish(false)
	}
	err := r.arr.Next(r.vals)
	if errors.Is(err, io.EOF) {
		return r.finish(true)
	}
	if err != nil {
		return err
	}
	for i, v := range r.vals {
		if r.cur[i], err = decode(r.types[i], v); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i], err)
		}
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

// readHead reads the answer up to the start of its rows, or to its end when
// it has none.
func (r *rows) readHead() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		if name := tok.String(); name != "resultTable" {
			if err := r.readMember(name); err != nil {
				return err
			}
			continue
		}
		if err := r.readTable(); err != nil {
			return err
		}
		if r.arr != nil {
			return nil
		}
	}
	return nil
}

// readTable reads resultTable up to the start of its rows, or to its end
// when it has none. dataSchema comes before rows (measured).
func (r *rows) readTable() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading resultTable: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading resultTable: %w", err)
		}
		switch name := tok.String(); name {
		case "rows":
			if len(r.types) != len(r.cols) || r.types == nil {
				return fmt.Errorf("reading resultTable: the rows come before dataSchema: %w", dbimp.ErrInvalidValue)
			}
			if r.arr, err = dbimp.NewArrayRows(r.dec, len(r.cols)); err != nil {
				return fmt.Errorf("reading resultTable: %w", err)
			}
			r.vals = make([]jsontext.Value, len(r.cols))
			r.cur = make([]driver.Value, len(r.cols))
			return nil
		case "dataSchema":
			v, err := r.dec.ReadValue()
			if err != nil {
				return fmt.Errorf("reading dataSchema: %w", err)
			}
			var schema struct {
				ColumnNames     []string `json:"columnNames"`
				ColumnDataTypes []string `json:"columnDataTypes"`
			}
			if err := json.Unmarshal(v, &schema); err != nil {
				return fmt.Errorf("reading dataSchema: %w", err)
			}
			if len(schema.ColumnNames) != len(schema.ColumnDataTypes) {
				return fmt.Errorf("reading dataSchema: %d names for %d types: %w", len(schema.ColumnNames), len(schema.ColumnDataTypes), dbimp.ErrColumnCount)
			}
			r.cols, r.types = schema.ColumnNames, schema.ColumnDataTypes
			if r.cols == nil {
				r.cols, r.types = []string{}, []string{}
			}
		default:
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading %q of resultTable: %w", name, err)
			}
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of resultTable: %w", err)
	}
	return nil
}

// readMember reads a member of the answer that is not resultTable.
func (r *rows) readMember(name string) error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading %q of the answer: %w", name, err)
	}
	switch name {
	case "exceptions":
		var ex []Exception
		if err := json.Unmarshal(v, &ex); err != nil {
			return fmt.Errorf("reading the exceptions of the answer: %w", err)
		}
		r.exceptions = append(r.exceptions, ex...)
	case "partialResult":
		if r.partial, err = dbimp.Bool(v); err != nil {
			return fmt.Errorf("reading partialResult: %w", err)
		}
	}
	return nil
}

// finish reads the rest of the answer after its rows, or after its head when
// it has no rows, and the end of the body. It returns io.EOF for a result
// that the server completed, and the error of the server otherwise.
func (r *rows) finish(inTable bool) error {
	r.done = true
	if inTable {
		for r.dec.PeekKind() != '}' {
			if _, err := r.dec.ReadToken(); err != nil {
				return fmt.Errorf("reading the end of resultTable: %w", err)
			}
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading the end of resultTable: %w", err)
			}
		}
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of resultTable: %w", err)
		}
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
		if err := r.readMember(tok.String()); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	var err error
	switch {
	case len(r.exceptions) > 0:
		err = newError(r.httpStatus, r.exceptions)
	case r.partial:
		err = ErrPartial
	default:
		return io.EOF
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
