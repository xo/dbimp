package neo4j

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/xo/dbimp"
)

// The states of the reading of a response. The zero state reads the members
// of the response before its rows.
const (
	// stateRows reads the rows.
	stateRows = iota + 1
	// stateDone has read the whole response.
	stateDone
)

// rows reads a response of the Query API one token at a time (D66). The
// response is one object: data holds fields and then values, one array for
// each row, and errors follow the data when the statement failed after some
// rows.
type rows struct {
	s     *dbimp.Stream
	dec   *jsontext.Decoder
	state int

	cols []string
	arr  *dbimp.ArrayRows
	vals []jsontext.Value

	httpStatus int
	errs       []Error
	txID       string

	// w watches the context of the statement, and stops the statement on
	// the server when the context ends (D67). It is nil for cancel=none.
	w *watch
	// t is the transaction of the statement, or nil. An error of the server
	// ends it (D65).
	t *tx
}

// ensure the interfaces.
var _ driver.RowsColumnScanner = (*rows)(nil)

// readResponse reads a response up to its rows. It returns the error of the
// server when the server sent one before any row, or when the status of the
// response is not 2xx, and then it reads the whole response, which is short.
func readResponse(res *http.Response, t *tx) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), httpStatus: res.StatusCode, t: t}
	if err := r.readHead(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if r.httpStatus >= http.StatusMultipleChoices || len(r.errs) > 0 {
		err := r.drain()
		_ = s.Close()
		if err == nil {
			err = r.error()
		}
		r.endTx(err)
		return nil, err
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// Close satisfies driver.Rows. It waits for a stop of the statement that
// began, and closes the body, and reads nothing more (D36).
func (r *rows) Close() error {
	_, _ = r.w.end()
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner.
func (r *rows) NextRow() error {
	if r.state == stateDone {
		return io.EOF
	}
	err := r.arr.Next(r.vals)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF):
		return r.end(r.finish())
	case errors.Is(err, dbimp.ErrColumnCount):
		// The server sends a row with fewer values than fields before an
		// error, such as a vector in typed JSON v1.0 (measured). The error of
		// the server says more than the count.
		if ferr := r.skipRows(); ferr == nil {
			if ferr = r.finish(); ferr != nil && !errors.Is(ferr, io.EOF) {
				return r.end(ferr)
			}
		}
	}
	return r.end(fmt.Errorf("reading a row: %w", err))
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	for i := range dest {
		v, err := decode(r.vals[i])
		if err != nil {
			return err
		}
		dest[i] = v
	}
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner. It decodes the value of
// the column as it is scanned (D63).
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	v, err := decode(r.vals[i])
	if err != nil {
		return err
	}
	return dbimp.Assign(scanCtx, dest, v)
}

// readHead reads the members of the response before its rows.
func (r *rows) readHead() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the response: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the response: %w", err)
		}
		if name := tok.String(); name == "data" {
			started, err := r.readData()
			if err != nil || started {
				return err
			}
		} else if err := r.readMember(name); err != nil {
			return err
		}
	}
	// A response with no data, such as an error, a begin or a commit.
	r.cols = []string{}
	return r.readEnd()
}

// readData reads data up to its rows, and reports whether the rows started.
func (r *rows) readData() (bool, error) {
	if err := expect(r.dec, '{'); err != nil {
		return false, fmt.Errorf("reading the data: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return false, fmt.Errorf("reading the data: %w", err)
		}
		switch tok.String() {
		case "fields":
			v, err := r.dec.ReadValue()
			if err != nil {
				return false, fmt.Errorf("reading the fields: %w", err)
			}
			if err := json.Unmarshal(v, &r.cols); err != nil {
				return false, fmt.Errorf("reading the fields: %w", err)
			}
		case "values":
			if r.cols == nil {
				return false, fmt.Errorf("reading the values before the fields: %w", dbimp.ErrInvalidValue)
			}
			if r.arr, err = dbimp.NewArrayRows(r.dec, len(r.cols)); err != nil {
				return false, err
			}
			r.vals = make([]jsontext.Value, len(r.cols))
			r.state = stateRows
			return true, nil
		default:
			if err := r.dec.SkipValue(); err != nil {
				return false, fmt.Errorf("reading the data: %w", err)
			}
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return false, fmt.Errorf("reading the end of the data: %w", err)
	}
	if r.cols == nil {
		r.cols = []string{}
	}
	return false, nil
}

// readMember reads a member of the response that is not the data.
func (r *rows) readMember(name string) error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading %q of the response: %w", name, err)
	}
	switch name {
	case "errors":
		var errs []Error
		if err := json.Unmarshal(v, &errs); err != nil {
			return fmt.Errorf("reading the errors of the response: %w", err)
		}
		r.errs = append(r.errs, errs...)
	case "transaction":
		var t struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(v, &t); err != nil {
			return fmt.Errorf("reading the transaction of the response: %w", err)
		}
		r.txID = t.ID
	}
	return nil
}

// skipRows reads every row that is left, and the end of the values.
func (r *rows) skipRows() error {
	for r.dec.PeekKind() != ']' {
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the rows: %w", err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the rows: %w", err)
	}
	return nil
}

// finish reads the rest of the response after its rows: the rest of data,
// the members that follow it, and the end of the body. It returns io.EOF for
// a result that the server completed, and the error of the server otherwise
// (D21 and D66).
func (r *rows) finish() error {
	for r.dec.PeekKind() != '}' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of the data: %w", err)
		}
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the end of the data: %w", err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the data: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		if err := r.readMember(tok.String()); err != nil {
			return err
		}
	}
	if err := r.readEnd(); err != nil {
		return err
	}
	if len(r.errs) > 0 {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, r.error())
	}
	return io.EOF
}

// readEnd reads the end of the response and of the body.
func (r *rows) readEnd() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the response: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	r.state = stateDone
	return nil
}

// end ends the reading of the rows with err, which is io.EOF for a result
// that the server completed. It waits for a stop of the statement that
// began, and adds its error. An error of the server ends the transaction.
func (r *rows) end(err error) error {
	r.state = stateDone
	ran, cerr := r.w.end()
	if ran && r.t != nil {
		r.t.ended = fmt.Errorf("stopping the statement ended the transaction: %w", err)
	}
	r.endTx(err)
	if cerr != nil && !errors.Is(err, io.EOF) {
		return errors.Join(err, cerr)
	}
	return err
}

// endTx ends the transaction of the statement if err holds an error of the
// server, which ends it on the server (D65).
func (r *rows) endTx(err error) {
	if _, ok := errors.AsType[*ResponseError](err); ok && r.t != nil && r.t.ended == nil {
		r.t.ended = err
	}
}

// drain reads every row that is left, and the rest of the response.
func (r *rows) drain() error {
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (r *rows) error() error {
	return &ResponseError{HTTPStatus: r.httpStatus, Errs: r.errs}
}

// expect reads one token, and returns an error if it is not kind.
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
