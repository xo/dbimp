package surrealdb

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"uuid"

	"github.com/xo/dbimp"
)

// rows reads the response to one request, one value at a time (D25). Each
// statement of the request is a result set (D52).
type rows struct {
	body io.ReadCloser
	r    reader

	// cols are the columns of the current result set, and index finds the
	// column of a key.
	cols  []string
	index map[string]int
	// vals holds the raw value of each column of the current row, and nil
	// for a key that the row lacks.
	vals [][]byte
	// first is a row that was read ahead to learn the columns, which NextRow
	// returns first.
	first   [][]byte
	pending bool
	// stream is true while the rows of the current result set arrive as the
	// elements of an array, and objects is true if they are objects.
	stream  bool
	objects bool
	// setDone is true once the entry of the current statement is read to its
	// end, and setErr holds its error, which NextRow returns after its rows.
	// kind is the kind of the error, which 3.x sends before the status.
	setDone bool
	setErr  error
	kind    string
	// next caches the answer of HasNextResultSet: 0 when it is not known, 1
	// for yes and 2 for no. nextErr is an error that NextResultSet returns.
	next    int
	nextErr error
	// done is true once the response is read to its end.
	done bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner = (*rows)(nil)
	_ driver.RowsNextResultSet = (*rows)(nil)
)

// readResponse reads the response in body up to the rows of its first
// statement. It closes body if it returns an error.
func readResponse(body io.ReadCloser, r reader) (*rows, error) {
	rs := &rows{body: body, r: r}
	statements, err := rs.readHead()
	if err == nil {
		if statements {
			err = rs.startSet()
		} else {
			err = rs.readValue()
		}
	}
	if err != nil {
		body.Close()
		return nil, err
	}
	return rs, nil
}

// Columns satisfies driver.Rows.
func (rs *rows) Columns() []string {
	return rs.cols
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36).
func (rs *rows) Close() error {
	return rs.body.Close()
}

// NextRow satisfies driver.RowsColumnScanner.
func (rs *rows) NextRow() error {
	if rs.pending {
		rs.pending = false
		copy(rs.vals, rs.first)
		return nil
	}
	if !rs.stream {
		if err := rs.finishSet(); err != nil {
			return err
		}
		return io.EOF
	}
	more, err := rs.r.more()
	if err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	if !more {
		rs.stream = false
		if err := rs.finishSet(); err != nil {
			return err
		}
		return io.EOF
	}
	return rs.readRow(rs.vals)
}

// Next satisfies driver.Rows, for a caller that does not scan through
// ScanColumn.
func (rs *rows) Next(dest []driver.Value) error {
	if err := rs.NextRow(); err != nil {
		return err
	}
	for i := range dest {
		v, err := rs.value(i)
		if err != nil {
			return err
		}
		dest[i] = v
	}
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner. It decodes the value of the
// column, and hands it to dbimp.Assign. A record id, a UUID and a duration
// scan into a string as SurrealQL writes them.
func (rs *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	v, err := rs.value(i)
	if err != nil {
		return err
	}
	switch dest.(type) {
	case *string, *sql.Null[string], *sql.NullString, *[]byte, *sql.RawBytes:
		switch s := v.(type) {
		case RecordID:
			v = s.String()
		case uuid.UUID:
			v = s.String()
		case time.Duration:
			v = formatDuration(s)
		}
	}
	return dbimp.Assign(scanCtx, dest, v)
}

// HasNextResultSet satisfies driver.RowsNextResultSet. It reads the rest of
// the current result set, which a caller can leave unread.
func (rs *rows) HasNextResultSet() bool {
	if rs.next != 0 {
		return rs.next == 1
	}
	rs.next = 2
	if err := rs.skipSet(); err != nil {
		rs.next, rs.nextErr = 1, err
		return true
	}
	if rs.done {
		return false
	}
	more, err := rs.r.more()
	switch {
	case err != nil:
		rs.next, rs.nextErr = 1, fmt.Errorf("reading the next statement: %w", err)
	case more:
		rs.next = 1
	default:
		if err := rs.finish(); err != nil {
			rs.next, rs.nextErr = 1, err
		}
	}
	return rs.next == 1
}

// NextResultSet satisfies driver.RowsNextResultSet. It moves to the result of
// the next statement, and returns the error of that statement if it failed
// (D55).
func (rs *rows) NextResultSet() error {
	if !rs.HasNextResultSet() {
		return io.EOF
	}
	rs.next = 0
	if err := rs.nextErr; err != nil {
		rs.nextErr = nil
		return err
	}
	return rs.readSet()
}

// drain reads every result set to its end, for ExecContext. It returns the
// error of the first statement that failed (D55).
func (rs *rows) drain() error {
	var first error
	for {
		for {
			err := rs.NextRow()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				first = cmp(first, err)
				if !isServerError(err) {
					return first
				}
				break
			}
		}
		if !rs.HasNextResultSet() {
			return first
		}
		if err := rs.NextResultSet(); err != nil {
			first = cmp(first, err)
			if !isServerError(err) {
				return first
			}
		}
	}
}

// cmp returns first if it is not nil, and err otherwise.
func cmp(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

// isServerError reports whether err is the failure of one statement, after
// which the other statements of the response can still be read.
func isServerError(err error) bool {
	var e *ResponseError
	return errors.As(err, &e) && e.Status != ""
}

// readHead reads the response up to its result. It reports true for the
// array of the statements of the method query, which it opens, and false
// for the result of another method, such as ping. An error of the RPC call
// itself is the error of the response.
func (rs *rows) readHead() (bool, error) {
	if err := rs.r.open(); err != nil {
		return false, fmt.Errorf("reading the response: %w", err)
	}
	for {
		more, err := rs.r.more()
		if err != nil {
			return false, fmt.Errorf("reading the response: %w", err)
		}
		if !more {
			return false, fmt.Errorf("reading the response: no result and no error: %w", dbimp.ErrIncomplete)
		}
		name, err := rs.r.key()
		if err != nil {
			return false, fmt.Errorf("reading the response: %w", err)
		}
		switch name {
		case "error":
			b, err := rs.r.raw()
			if err != nil {
				return false, fmt.Errorf("reading the error of the response: %w", err)
			}
			return false, rpcError(rs.r, b)
		case "result":
			k, err := rs.r.peek()
			if err != nil {
				return false, fmt.Errorf("reading the result of the response: %w", err)
			}
			if k != kindArray {
				return false, nil
			}
			return true, rs.r.open()
		default:
			if err := rs.r.skip(); err != nil {
				return false, fmt.Errorf("reading %q of the response: %w", name, err)
			}
		}
	}
}

// readValue reads the result of a method other than query, which is one
// value, as one result set of one row.
func (rs *rows) readValue() error {
	b, err := rs.r.raw()
	if err != nil {
		return fmt.Errorf("reading the result of the response: %w", err)
	}
	rs.setColumns([]string{""})
	rs.first, rs.pending, rs.setDone = [][]byte{b}, true, true
	return rs.finish()
}

// startSet reads the entry of the first statement up to its rows. A
// response that holds no statement, as the text BEGIN alone does on 2.7,
// has one result set with no columns and no rows.
func (rs *rows) startSet() error {
	more, err := rs.r.more()
	if err != nil {
		return fmt.Errorf("reading the first statement: %w", err)
	}
	if !more {
		rs.cols, rs.setDone = []string{}, true
		return rs.finish()
	}
	return rs.readSet()
}

// readSet reads the entry of the next statement up to its rows, which the
// caller has found by more.
func (rs *rows) readSet() error {
	rs.cols, rs.index, rs.vals, rs.first = nil, nil, nil, nil
	rs.pending, rs.stream, rs.objects, rs.setDone, rs.setErr, rs.kind = false, false, false, false, nil, ""
	if err := rs.r.open(); err != nil {
		return fmt.Errorf("reading a statement: %w", err)
	}
	for {
		more, err := rs.r.more()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if !more {
			rs.setDone = true
			if rs.cols == nil {
				return fmt.Errorf("reading a statement: no result: %w", dbimp.ErrIncomplete)
			}
			err := rs.setErr
			rs.setErr = nil
			return err
		}
		name, err := rs.r.key()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if name != "result" {
			if err := rs.readMember(name); err != nil {
				return err
			}
			continue
		}
		if err := rs.readResult(); err != nil {
			return err
		}
		if rs.stream {
			return nil
		}
	}
}

// readResult reads the result of a statement: the start of its rows if it is
// an array, and the one row that it is otherwise (D52).
func (rs *rows) readResult() error {
	k, err := rs.r.peek()
	if err != nil {
		return fmt.Errorf("reading the result of a statement: %w", err)
	}
	switch k {
	case kindArray:
		if err := rs.r.open(); err != nil {
			return err
		}
		more, err := rs.r.more()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		if !more {
			rs.cols = []string{}
			return nil
		}
		first, err := rs.r.peek()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		rs.stream = true
		rs.objects = first == kindObject
		return rs.readFirst()
	case kindObject:
		rs.objects = true
		return rs.readFirst()
	}
	b, err := rs.r.raw()
	if err != nil {
		return fmt.Errorf("reading the result of a statement: %w", err)
	}
	rs.setColumns([]string{""})
	rs.first, rs.pending = [][]byte{b}, true
	return nil
}

// readFirst reads the first row, which gives the columns of the result set.
func (rs *rows) readFirst() error {
	if !rs.objects {
		b, err := rs.r.raw()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		rs.setColumns([]string{""})
		rs.first, rs.pending = [][]byte{b}, true
		return nil
	}
	if err := rs.r.open(); err != nil {
		return fmt.Errorf("reading the first row: %w", err)
	}
	var cols []string
	var vals [][]byte
	for {
		more, err := rs.r.more()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		if !more {
			break
		}
		name, err := rs.r.key()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		b, err := rs.r.raw()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		cols, vals = append(cols, name), append(vals, b)
	}
	if cols == nil {
		cols = []string{}
	}
	rs.setColumns(cols)
	rs.first, rs.pending = vals, true
	return nil
}

// readRow reads the next row of the rows of an array into vals.
func (rs *rows) readRow(vals [][]byte) error {
	clear(vals)
	if !rs.objects {
		b, err := rs.r.raw()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		vals[0] = b
		return nil
	}
	k, err := rs.r.peek()
	if err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	if k != kindObject {
		return fmt.Errorf("reading a row that is not an object, after a row that is: %w", dbimp.ErrColumnCount)
	}
	if err := rs.r.open(); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	for {
		more, err := rs.r.more()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		if !more {
			return nil
		}
		name, err := rs.r.key()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		i, ok := rs.index[name]
		if !ok {
			return fmt.Errorf("reading a row: %q: %w", name, dbimp.ErrExtraColumn)
		}
		if vals[i], err = rs.r.raw(); err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
	}
}

// setColumns sets the columns of the result set.
func (rs *rows) setColumns(cols []string) {
	rs.cols = cols
	rs.index = make(map[string]int, len(cols))
	for i, c := range cols {
		rs.index[c] = i
	}
	rs.vals = make([][]byte, len(cols))
}

// readMember reads a member of the entry of a statement that is not its
// result: its status, the kind of its error, or a member that the driver
// does not use, such as time.
func (rs *rows) readMember(name string) error {
	switch name {
	case "status", "kind":
		b, err := rs.r.raw()
		if err != nil {
			return fmt.Errorf("reading %q of a statement: %w", name, err)
		}
		v, err := rs.r.decode(b)
		if err != nil {
			return fmt.Errorf("reading %q of a statement: %w", name, err)
		}
		s, _ := v.(string)
		if name == "kind" {
			rs.kind = s
		} else if s != "OK" {
			rs.setFailed(s)
		}
		return nil
	}
	if err := rs.r.skip(); err != nil {
		return fmt.Errorf("reading %q of a statement: %w", name, err)
	}
	return nil
}

// setFailed marks the statement as failed. The result of a statement that
// failed is its message, and never a row. A statement that fails after its
// rows, which no measured release does, says its status.
func (rs *rows) setFailed(status string) {
	e := Error{Kind: rs.kind, Msg: "the statement ended with the status " + status}
	if rs.pending && !rs.objects && len(rs.first) == 1 {
		if v, err := rs.r.decode(rs.first[0]); err == nil {
			if s, ok := v.(string); ok {
				e.Msg = s
			}
		}
		rs.pending, rs.cols = false, []string{}
	}
	rs.setErr = &ResponseError{HTTPStatus: http.StatusOK, Status: status, Errs: []Error{e}}
}

// finishSet reads the rest of the entry of the current statement after its
// rows, and returns its error.
func (rs *rows) finishSet() error {
	for !rs.setDone {
		more, err := rs.r.more()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if !more {
			rs.setDone = true
			break
		}
		name, err := rs.r.key()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if err := rs.readMember(name); err != nil {
			return err
		}
	}
	err := rs.setErr
	rs.setErr = nil
	return err
}

// skipSet reads the rest of the current result set, and returns an error
// only if the response cannot be read.
func (rs *rows) skipSet() error {
	rs.pending = false
	for rs.stream {
		more, err := rs.r.more()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		if !more {
			rs.stream = false
			break
		}
		if err := rs.r.skip(); err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
	}
	if err := rs.finishSet(); err != nil && !isServerError(err) {
		return err
	}
	return nil
}

// finish reads the rest of the response after the entry of its last
// statement.
func (rs *rows) finish() error {
	if rs.done {
		return nil
	}
	for {
		more, err := rs.r.more()
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		if !more {
			break
		}
		if _, err := rs.r.key(); err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		if err := rs.r.skip(); err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
	}
	rs.done = true
	return rs.r.end()
}

// value returns the value of column i of the current row.
func (rs *rows) value(i int) (any, error) {
	if i >= len(rs.vals) || rs.vals[i] == nil {
		return nil, nil
	}
	return rs.r.decode(rs.vals[i])
}

// rpcError returns the error of the RPC call, which b holds.
func rpcError(r reader, b []byte) error {
	v, err := r.decode(b)
	if err != nil {
		return fmt.Errorf("reading the error of the response: %w", err)
	}
	m, _ := v.(map[string]any)
	e := Error{Code: -1}
	switch c := m["code"].(type) {
	case int64:
		e.Code = int(c)
	case float64:
		e.Code = int(c)
	}
	e.Msg, _ = m["message"].(string)
	e.Kind, _ = m["kind"].(string)
	return &ResponseError{HTTPStatus: http.StatusOK, Errs: []Error{e}}
}
