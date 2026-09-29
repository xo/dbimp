package surrealdb

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"time"
	"uuid"

	"github.com/xo/dbimp"
)

// setReader reads the result sets of a response in one wire format: cborSets
// for CBOR and jsonSets for JSON (D49 and D108). Each statement of the
// request is a result set (D52).
type setReader interface {
	// next reads the rest of the current result set, and moves to the next
	// one. The first call moves to the first. It returns the columns of the
	// result set, and the error of its statement if it failed, or io.EOF when
	// no result set is left.
	next() ([]string, error)
	// row reads the next row of the current result set into vals, a raw value
	// for each column and nil for a key that the row lacks. It returns io.EOF
	// after the last row, or the error of the statement if it failed after
	// its rows.
	row(vals [][]byte) error
	// decode returns the Go value of a raw value that row read.
	decode(b []byte) (any, error)
}

// rows reads the response to one request, one value at a time (D25).
type rows struct {
	body io.ReadCloser
	sets setReader

	// cols are the columns of the current result set, and vals holds the raw
	// value of each column of the current row.
	cols []string
	vals [][]byte
	// next caches the answer of HasNextResultSet: 0 when it is not known, 1
	// for yes and 2 for no. nextCols and nextErr are the result set that it
	// read ahead, which NextResultSet moves to.
	next     int
	nextCols []string
	nextErr  error
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner = (*rows)(nil)
	_ driver.RowsNextResultSet = (*rows)(nil)
)

// readResponse reads the response in body, through sets, up to the rows of
// its first statement. It closes body if it returns an error.
func readResponse(body io.ReadCloser, sets setReader) (*rows, error) {
	rs := &rows{body: body, sets: sets}
	cols, err := sets.next()
	if err != nil {
		body.Close()
		return nil, err
	}
	rs.setColumns(cols)
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
	return rs.sets.row(rs.vals)
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
// the current result set, which a caller can leave unread, and the start of
// the next one.
func (rs *rows) HasNextResultSet() bool {
	if rs.next != 0 {
		return rs.next == 1
	}
	cols, err := rs.sets.next()
	if errors.Is(err, io.EOF) {
		rs.next = 2
		return false
	}
	rs.next, rs.nextCols, rs.nextErr = 1, cols, err
	return true
}

// NextResultSet satisfies driver.RowsNextResultSet. It moves to the result of
// the next statement, and returns the error of that statement if it failed
// (D55).
func (rs *rows) NextResultSet() error {
	if !rs.HasNextResultSet() {
		return io.EOF
	}
	err := rs.nextErr
	rs.setColumns(rs.nextCols)
	rs.next, rs.nextCols, rs.nextErr = 0, nil, nil
	return err
}

// setColumns moves to a result set with the columns cols.
func (rs *rows) setColumns(cols []string) {
	rs.cols, rs.vals = cols, make([][]byte, len(cols))
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

// value returns the value of column i of the current row.
func (rs *rows) value(i int) (any, error) {
	if i >= len(rs.vals) || rs.vals[i] == nil {
		return nil, nil
	}
	return rs.sets.decode(rs.vals[i])
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

// kind is the kind of the next value of a response.
type kind int

// The kinds of a value that a reader tells apart.
const (
	kindOther kind = iota
	kindObject
	kindArray
)

// setState is the state of the current result set, which cborSets and
// jsonSets share. Each format walks the answer with its own decoder (D108).
type setState struct {
	// started is true once the head of the response is read.
	started bool
	// cols are the columns of the current result set, and index finds the
	// column of a key.
	cols  []string
	index map[string]int
	// first is a row that was read ahead to learn the columns, which row
	// returns first.
	first   [][]byte
	pending bool
	// stream is true while the rows of the current result set arrive as the
	// elements of an array, and objects is true if they are objects.
	stream  bool
	objects bool
	// setDone is true once the entry of the current statement is read to its
	// end, and setErr holds its error, which row returns after its rows.
	// kind is the kind of the error, which 3.x sends before the status.
	setDone bool
	setErr  error
	kind    string
	// done is true once the response is read to its end.
	done bool
}

// reset starts a new result set.
func (s *setState) reset() {
	s.cols, s.index, s.first = nil, nil, nil
	s.pending, s.stream, s.objects, s.setDone, s.setErr, s.kind = false, false, false, false, nil, ""
}

// setColumns sets the columns of the result set.
func (s *setState) setColumns(cols []string) {
	s.cols = cols
	s.index = make(map[string]int, len(cols))
	for i, c := range cols {
		s.index[c] = i
	}
}

// fail marks the statement as failed, with the message msg, or a message
// that names its status if msg is "". The result of a statement that failed
// is its message, and never a row. A statement that fails after its rows,
// which no measured release does, says its status.
func (s *setState) fail(status, msg string) {
	e := Error{Kind: s.kind, Msg: "the statement ended with the status " + status}
	if msg != "" {
		e.Msg = msg
	}
	if s.pending && !s.objects && len(s.first) == 1 {
		s.pending, s.cols = false, []string{}
	}
	s.setErr = &ResponseError{HTTPStatus: http.StatusOK, Status: status, Errs: []Error{e}}
}

// setError returns the error of the statement, once, and forgets it.
func (s *setState) setError() error {
	err := s.setErr
	s.setErr = nil
	return err
}

// rpcError returns the error of the RPC call, from its decoded value v.
func rpcError(v any) error {
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
