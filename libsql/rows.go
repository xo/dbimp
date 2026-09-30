package libsql

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

// rows reads the answer of /v3/cursor one entry at a time (D25 and D149).
// The answer is lines of JSON: the baton and the base_url, then step_begin
// with the columns, a row entry for each row, and step_end, or step_error
// after some rows (measured).
type rows struct {
	c *Connector
	s *stream
	// tx is the transaction of the rows, or nil.
	tx *tx
	// ctx is the context of the query, which the request that closes the
	// stream takes without its end (D149).
	ctx    context.Context //nolint:containedctx // The close of the stream after the rows needs it (D149).
	cancel context.CancelFunc

	st         *dbimp.Stream
	dec        *jsontext.Decoder
	httpStatus int

	cols    []string
	decls   []string
	columns []column
	cur     []driver.Value
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the answer is read to
	// its end and the stream is closed.
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

// entry is one line of the answer of a cursor, with its rows read later.
type entry struct {
	Type string `json:"type"`
	Cols []struct {
		Name     string `json:"name"`
		Decltype string `json:"decltype"`
	} `json:"cols"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	Row []jsontext.Value `json:"row"`
}

// readCursor reads the answer of a cursor up to its first row, or to its
// end. An error before any row is the error of the query.
func readCursor(ctx context.Context, r *rows, res *http.Response) error {
	r.ctx = ctx
	r.st = dbimp.NewStream(res.Body)
	r.dec = r.st.Decoder()
	r.httpStatus = res.StatusCode
	r.cols = []string{}
	head, err := r.dec.ReadValue()
	if err != nil {
		return r.fail(fmt.Errorf("reading the head of the cursor: %w", err))
	}
	var h struct {
		Baton   *string `json:"baton"`
		BaseURL *string `json:"base_url"`
	}
	if err := json.Unmarshal(head, &h); err != nil {
		return r.fail(fmt.Errorf("reading the head of the cursor: %w", err))
	}
	if err := r.s.answer(deref(h.Baton), deref(h.BaseURL)); err != nil {
		return r.fail(err)
	}
	for {
		e, err := r.next()
		switch {
		case errors.Is(err, io.EOF):
			return r.finish()
		case err != nil:
			return r.fail(err)
		case e.Type == "step_begin":
			r.cols = make([]string, len(e.Cols))
			r.decls = make([]string, len(e.Cols))
			r.columns = make([]column, len(e.Cols))
			for i, c := range e.Cols {
				r.cols[i], r.decls[i], r.columns[i] = c.Name, c.Decltype, columnOf(c.Decltype)
			}
			r.cur = make([]driver.Value, len(e.Cols))
			return nil
		case e.Type == "step_error" || e.Type == "error":
			return r.fail(r.serverError(e))
		}
	}
}

// deref returns *s, or "" for nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the declared type in upper case, such as INTEGER or F32_BLOB(3), and
// "" for an expression, whose decltype is null (measured).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return strings.ToUpper(r.decls[i])
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D147).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.columns[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. A column of
// SQLite holds NULL unless its table says NOT NULL, which the answer does not
// name, so the driver says that each column can.
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the rest of the answer,
// closes the stream, and returns the error that it holds (D36 and D149).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	for {
		e, err := r.next()
		switch {
		case errors.Is(err, io.EOF):
			if err := r.finish(); err != nil {
				return err
			}
			return io.EOF
		case err != nil:
			return r.fail(err)
		case e.Type == "row":
			if len(e.Row) != len(r.cols) {
				return r.fail(fmt.Errorf("reading a row of %d values for %d columns: %w", len(e.Row), len(r.cols), dbimp.ErrColumnCount))
			}
			for i, v := range e.Row {
				if r.cur[i], err = decode(r.columns[i], v); err != nil {
					return r.fail(fmt.Errorf("reading the column %s: %w", r.cols[i], err))
				}
			}
			r.read = true
			return nil
		case e.Type == "step_error" || e.Type == "error":
			return r.fail(r.serverError(e))
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

// Close satisfies driver.Rows. Before the end, it closes the body and reads
// nothing more (D36). It then closes the stream of a cursor that is not in a
// transaction (D149).
func (r *rows) Close() error {
	if r.done {
		return nil
	}
	err := r.st.Close()
	return errors.Join(err, r.release())
}

// next reads the next entry, or returns io.EOF at the end of the answer.
func (r *rows) next() (entry, error) {
	var e entry
	if r.dec.PeekKind() == 0 {
		if err := r.st.End(); err != nil {
			return e, err
		}
		return e, io.EOF
	}
	v, err := r.dec.ReadValue()
	if errors.Is(err, io.EOF) {
		return e, io.EOF
	}
	if err != nil {
		return e, fmt.Errorf("reading the cursor: %w", err)
	}
	if err := json.Unmarshal(v, &e); err != nil {
		return e, fmt.Errorf("reading an entry of the cursor: %w", err)
	}
	return e, nil
}

// serverError returns the error of a step_error or an error entry.
func (r *rows) serverError(e entry) error {
	if e.Error == nil {
		return &Error{HTTPStatus: r.httpStatus, Message: "the cursor ended with " + e.Type}
	}
	return &Error{HTTPStatus: r.httpStatus, Code: e.Error.Code, Message: e.Error.Message}
}

// fail ends the rows with err, which wraps dbimp.ErrIncomplete after a row
// reached the caller (D107).
func (r *rows) fail(err error) error {
	_ = r.st.Close()
	if rerr := r.release(); rerr != nil {
		err = errors.Join(err, rerr)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// finish ends the rows at the end of the answer.
func (r *rows) finish() error {
	return r.release()
}

// release closes the stream of rows that are not in a transaction, frees the
// transaction of rows that are, and ends the bound of WithTimeout.
func (r *rows) release() error {
	if r.done {
		return nil
	}
	r.done = true
	defer func() {
		if r.cancel != nil {
			r.cancel()
		}
	}()
	if r.tx != nil {
		r.tx.busy = false
		return nil
	}
	return r.c.closeStream(r.ctx, r.s)
}
