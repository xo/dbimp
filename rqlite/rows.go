package rqlite

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
	"strings"

	"github.com/xo/dbimp"
)

// rows reads the answer of one request one token at a time (D25 and D142).
// The answer is one JSON object with results, an array with one element for
// each statement, and each element holds columns, then types, then values,
// or error, last_insert_id and rows_affected (measured).
type rows struct {
	s          *dbimp.Stream
	dec        *jsontext.Decoder
	httpStatus int

	cols    []string
	types   []string
	classes []class
	arr     *dbimp.ArrayRows
	vals    []jsontext.Value
	// cur is the current row.
	cur []driver.Value
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the answer is read to
	// its end.
	read bool
	done bool

	// errs holds the error of each result that holds one.
	errs []string
	// lastInsertID and rowsAffected are the counts of the last result.
	lastInsertID int64
	rowsAffected int64

	// cancel ends the bound of WithTimeout on the request, or does nothing
	// (D146).
	cancel context.CancelFunc
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
)

// readAnswer reads an answer up to its first row. It returns the error of the
// server for an answer that holds no rows, which is how every statement that
// fails arrives, because the server reads every row before it answers
// (measured). A failure before any row does not wrap dbimp.ErrIncomplete,
// which is for a failure after a row (D107).
func readAnswer(res *http.Response) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), httpStatus: res.StatusCode, cols: []string{}}
	if err := r.readHead(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if r.arr == nil {
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
// It is the declared type in upper case, as the statement wrote it, such as
// BIGINT or VARCHAR(10), or the storage class of the first row for a column
// of an expression, and "" for an expression whose first row is NULL
// (measured).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return strings.ToUpper(r.types[i])
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D140).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.classes[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. A column of
// SQLite holds NULL unless its table says NOT NULL, which the answer does not
// name, so the driver says that each column can.
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). The server sends the answer after the statement ends, so the
// statement needs no cancel.
func (r *rows) Close() error {
	err := r.s.Close()
	if r.cancel != nil {
		r.cancel()
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the rest of the answer,
// and returns the error that it holds (D36).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	if r.arr == nil {
		return r.finish()
	}
	err := r.arr.Next(r.vals)
	if errors.Is(err, io.EOF) {
		r.arr = nil
		return r.finish()
	}
	if err != nil {
		return err
	}
	for i, v := range r.vals {
		if r.cur[i], err = decode(r.classes[i], v); err != nil {
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

// readHead reads the answer up to the rows of its first result, or to its
// end when no result has rows.
func (r *rows) readHead() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		if tok.String() != "results" {
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading %q of the answer: %w", tok.String(), err)
			}
			continue
		}
		if err := expect(r.dec, '['); err != nil {
			return fmt.Errorf("reading the results: %w", err)
		}
		if err := r.readResults(); err != nil || r.arr != nil {
			return err
		}
	}
	return nil
}

// readResults reads the elements of results up to the rows of one, or to
// the end of results. An answer holds one element for the one statement that
// the driver sends, none for an empty statement, and the result of the last
// statement for a text with two (measured).
func (r *rows) readResults() error {
	for r.dec.PeekKind() != ']' {
		if err := expect(r.dec, '{'); err != nil {
			return fmt.Errorf("reading a result: %w", err)
		}
		if err := r.readResult(); err != nil || r.arr != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the results: %w", err)
	}
	return nil
}

// readResult reads the members of one result up to its values, or to its
// end. columns and types come before values (measured).
func (r *rows) readResult() error {
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading a result: %w", err)
		}
		name := tok.String()
		if name == "values" {
			if len(r.types) != len(r.cols) {
				return fmt.Errorf("reading a result: %d types for %d columns: %w", len(r.types), len(r.cols), dbimp.ErrColumnCount)
			}
			if r.arr, err = dbimp.NewArrayRows(r.dec, len(r.cols)); err != nil {
				return fmt.Errorf("reading the values: %w", err)
			}
			r.vals = make([]jsontext.Value, len(r.cols))
			r.cur = make([]driver.Value, len(r.cols))
			return nil
		}
		if err := r.readMember(name); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a result: %w", err)
	}
	return nil
}

// readMember reads a member of a result that is not values.
func (r *rows) readMember(name string) error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading %q of a result: %w", name, err)
	}
	switch name {
	case "columns":
		if err := json.Unmarshal(v, &r.cols); err != nil {
			return fmt.Errorf("reading the columns: %w", err)
		}
		if r.cols == nil {
			r.cols = []string{}
		}
	case "types":
		if err := json.Unmarshal(v, &r.types); err != nil {
			return fmt.Errorf("reading the types: %w", err)
		}
		r.classes = make([]class, len(r.types))
		for i, t := range r.types {
			r.classes[i] = classOf(t)
		}
	case "error":
		msg, err := dbimp.String(v)
		if err != nil {
			return fmt.Errorf("reading the error of a result: %w", err)
		}
		r.errs = append(r.errs, msg)
	case "last_insert_id":
		if r.lastInsertID, err = dbimp.Int64(v); err != nil {
			return fmt.Errorf("reading last_insert_id: %w", err)
		}
	case "rows_affected":
		if r.rowsAffected, err = dbimp.Int64(v); err != nil {
			return fmt.Errorf("reading rows_affected: %w", err)
		}
	}
	return nil
}

// finish reads the rest of the answer after the rows of a result, or after
// its head when no result has rows, and the end of the body. It returns
// io.EOF for an answer with no error, and the error of the server otherwise.
func (r *rows) finish() error {
	r.done = true
	if err := r.finishResults(); err != nil {
		return err
	}
	for r.dec.PeekKind() != '}' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	if len(r.errs) == 0 {
		return io.EOF
	}
	err := &Error{HTTPStatus: r.httpStatus, Message: r.errs[0]}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// finishResults reads the rest of results, when the head stopped at the
// values of a result: the members of that result after its values, and the
// elements after it.
func (r *rows) finishResults() error {
	if r.vals == nil {
		return nil
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading a result: %w", err)
		}
		if err := r.readMember(tok.String()); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a result: %w", err)
	}
	for r.dec.PeekKind() != ']' {
		if err := expect(r.dec, '{'); err != nil {
			return fmt.Errorf("reading a result: %w", err)
		}
		for r.dec.PeekKind() != '}' {
			tok, err := r.dec.ReadToken()
			if err != nil {
				return fmt.Errorf("reading a result: %w", err)
			}
			if err := r.readMember(tok.String()); err != nil {
				return err
			}
		}
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of a result: %w", err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the results: %w", err)
	}
	return nil
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
