package drill

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/xo/dbimp"
)

// ErrCut is the error of an answer that ends with no queryState. The server
// writes queryState last, so an answer without it did not end (D165).
const ErrCut dbimp.Error = "the answer was cut short"

// ErrCanceled is the error of a query that the server ended with queryState
// CANCELED, when the context of the query did not end. Someone else
// cancelled it, such as an administrator (measured).
const ErrCanceled dbimp.Error = "the query was cancelled"

// rows reads the answer of one query, which is one JSON object, one token at
// a time (D25 and D165). Its members come in this order: queryId, columns,
// metadata, attemptedAutoLimit, rows and queryState. An error before any row
// has exception, errorMessage and stackTrace in place of the columns, with
// the verbose option. The driver reads queryState at the end, and only it
// says whether the query completed.
type rows struct {
	s   *dbimp.Stream
	dec *jsontext.Decoder
	obj *dbimp.ObjectRows
	w   *watch

	cols  []string
	types []column
	// lists is true for a column that held a JSON array in a row that the
	// caller read (D165).
	lists []bool
	// vals holds the raw values of the current row, and cur its decoded
	// values.
	vals []jsontext.Value
	cur  []driver.Value

	id        string
	state     string
	exception string
	message   string
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the answer is read to
	// its end.
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

// readAnswer reads the head of an answer, up to the first row. It reads the
// end of an answer that holds no rows, and returns its error, which comes
// before any row and so does not wrap dbimp.ErrIncomplete (D107). ctx and
// sess are those of the query, for the watch.
func (c *Connector) readAnswer(ctx context.Context, res *http.Response, sess session) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder()}
	hasRows, err := r.readHead(func(id string) { r.w = c.watch(ctx, id, sess) })
	if err == nil && !hasRows {
		if err = r.finish(); errors.Is(err, io.EOF) {
			err = r.prepare()
		}
	}
	if err != nil {
		_ = s.Close()
		r.w.end()
		return nil, err
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the type of metadata without its numbers, such as VARDECIMAL.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.types[i].name
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and
// D165). It names any for a column that held a list.
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.types[i], r.lists[i])
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The metadata
// holds no mode, and every type can be NULL (D165).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. It is the width of
// a VARCHAR that has one, such as VARCHAR(5). A column of a file has none.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	if c := r.types[i]; c.name == typeVarchar && len(c.args) == 1 {
		return c.args[0], true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. It
// is the precision and the scale of a VARDECIMAL, such as VARDECIMAL(38, 18).
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if c := r.types[i]; c.name == typeVarDecimal && len(c.args) == 2 {
		return c.args[0], c.args[1], true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). It ends the watch of the query. A query that ran to its end needs
// no cancel, and one whose context ended was cancelled by the watch. Rows
// that the caller closes before the end cancel the query on the server too,
// because the server runs it on when the client leaves (D165).
func (r *rows) Close() error {
	err := r.s.Close()
	if r.done {
		r.w.end()
	} else {
		r.w.abort()
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads queryState, and returns an
// error unless it is COMPLETED (D21 and D165).
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	if r.obj == nil {
		return r.finish()
	}
	if err := r.obj.Next(r.vals); err != nil {
		if errors.Is(err, io.EOF) {
			return r.finish()
		}
		return r.fail(err)
	}
	for i, v := range r.vals {
		var err error
		if r.cur[i], err = decode(r.types[i], v); err != nil {
			return r.fail(fmt.Errorf("reading the column %s: %w", r.cols[i], err))
		}
		if v.Kind() == '[' {
			r.lists[i] = true
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

// written returns the count of records of the row that CREATE TABLE AS
// answers, with the columns Fragment and Number of records written
// (measured).
func (r *rows) written() (int64, bool) {
	if len(r.cols) != 2 || r.cols[0] != "Fragment" || r.cols[1] != "Number of records written" {
		return 0, false
	}
	n, ok := r.cur[1].(int64)
	return n, ok
}

// readHead reads the members of the answer up to the start of the rows. It
// returns true when the next token is the first of the rows, and false when
// the object has no rows. The caller then reads the rest with finish.
// started is called with the queryId, as soon as it is read.
func (r *rows) readHead(started func(id string)) (bool, error) {
	if err := expect(r.dec, '{'); err != nil {
		return false, fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() == '"' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return false, fmt.Errorf("reading the answer: %w", err)
		}
		switch name := tok.String(); name {
		case "queryId":
			if r.id, err = r.readString(name); err != nil {
				return false, err
			}
			started(r.id)
		case "columns":
			if r.cols, err = r.readStrings(name); err != nil {
				return false, err
			}
		case "metadata":
			meta, err := r.readStrings(name)
			if err != nil {
				return false, err
			}
			r.types = make([]column, len(meta))
			for i, m := range meta {
				r.types[i] = parseColumn(m)
			}
		case "exception":
			if r.exception, err = r.readString(name); err != nil {
				return false, err
			}
		case "errorMessage":
			if r.message, err = r.readString(name); err != nil {
				return false, err
			}
		case "queryState":
			if r.state, err = r.readString(name); err != nil {
				return false, err
			}
		case "rows":
			return true, r.startRows()
		default:
			// attemptedAutoLimit, stackTrace and any member that a later
			// release adds.
			if _, err := r.dec.ReadValue(); err != nil {
				return false, fmt.Errorf("reading %q: %w", name, err)
			}
		}
	}
	return false, nil
}

// prepare checks the columns and the types, and makes the buffers of a row.
func (r *rows) prepare() error {
	if r.cols == nil {
		r.cols = []string{}
	}
	if r.types == nil {
		r.types = []column{}
	}
	if len(r.types) != len(r.cols) {
		return fmt.Errorf("reading the answer: %d columns and %d types: %w", len(r.cols), len(r.types), dbimp.ErrColumnCount)
	}
	r.lists = make([]bool, len(r.cols))
	r.vals = make([]jsontext.Value, len(r.cols))
	r.cur = make([]driver.Value, len(r.cols))
	return nil
}

// startRows prepares the buffers, and starts the rows.
func (r *rows) startRows() error {
	if err := r.prepare(); err != nil {
		return err
	}
	obj, err := dbimp.NewObjectRows(r.dec, r.cols)
	if err != nil {
		return err
	}
	r.obj = obj
	return nil
}

// readString reads the string value of the member name.
func (r *rows) readString(name string) (string, error) {
	v, err := r.dec.ReadValue()
	if err != nil {
		return "", fmt.Errorf("reading %q: %w", name, err)
	}
	s, err := dbimp.String(v)
	if err != nil {
		return "", fmt.Errorf("reading %q: %w", name, err)
	}
	return s, nil
}

// readStrings reads the array of strings that is the value of the member
// name.
func (r *rows) readStrings(name string) ([]string, error) {
	if err := expect(r.dec, '['); err != nil {
		return nil, fmt.Errorf("reading %q: %w", name, err)
	}
	out := []string{}
	for r.dec.PeekKind() != ']' {
		s, err := r.readString(name)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading %q: %w", name, err)
	}
	return out, nil
}

// finish reads the members that follow the rows, which end the object, and
// then the end of the body. It returns io.EOF for a query that ended
// COMPLETED, and an error for any other end.
func (r *rows) finish() error {
	r.done = true
	for r.dec.PeekKind() == '"' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return r.fail(fmt.Errorf("reading the answer: %w", err))
		}
		switch name := tok.String(); name {
		case "queryState":
			if r.state, err = r.readString(name); err != nil {
				return r.fail(err)
			}
		case "errorMessage":
			if r.message, err = r.readString(name); err != nil {
				return r.fail(err)
			}
		case "exception":
			if r.exception, err = r.readString(name); err != nil {
				return r.fail(err)
			}
		default:
			if _, err := r.dec.ReadValue(); err != nil {
				return r.fail(fmt.Errorf("reading %q: %w", name, err))
			}
		}
	}
	if err := expect(r.dec, '}'); err != nil {
		return r.fail(fmt.Errorf("reading the end of the answer: %w", err))
	}
	if err := r.s.End(); err != nil {
		return r.fail(err)
	}
	if err := r.outcome(); err != nil {
		return r.fail(err)
	}
	r.w.end()
	return io.EOF
}

// outcome returns the error that queryState names, or nil for COMPLETED.
func (r *rows) outcome() error {
	switch r.state {
	case "COMPLETED":
		return nil
	case "FAILED":
		return r.failed()
	case "CANCELED":
		if cerr := r.w.cause(); cerr != nil {
			return fmt.Errorf("reading the answer: %w", cerr)
		}
		return fmt.Errorf("reading the answer of the query %s: %w", r.id, ErrCanceled)
	case "":
		return fmt.Errorf("reading the answer: no queryState: %w", ErrCut)
	}
	return fmt.Errorf("reading the answer: the queryState %q: %w", r.state, dbimp.ErrInvalidValue)
}

// failed returns the *Error of a query that ended FAILED. With the verbose
// option, an error before any row has its message in the answer. An error
// after some rows has none, so the driver reads the profile of the query,
// which holds it (D165).
func (r *rows) failed() *Error {
	msg, class := r.message, r.exception

	if msg == "" && r.w != nil {
		p, err := r.w.profile()
		if err != nil {
			return failure(r.id, class, "the query failed, and the server gave no reason: "+err.Error())
		}
		var cause string
		msg, cause = p.message()
		if class == "" {
			class = cause
		}
	}
	return failure(r.id, class, msg)
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row
// reached the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before
	// the read sees the end, and the read then fails with "use of closed
	// network connection". The caller must see that its context ended (D36).
	if cerr := r.w.cause(); cerr != nil && !errors.Is(err, cerr) {
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
