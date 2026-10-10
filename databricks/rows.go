package databricks

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

// formatJSONArray is the format of every result that the driver reads
// (measured, D193 item 1). It is the default with the disposition INLINE.
const formatJSONArray = "JSON_ARRAY"

// status is the member status of an answer (measured).
type status struct {
	State string `json:"state"`
	Error *struct {
		Code    jsontext.Value `json:"error_code"`
		Message string         `json:"message"`
	} `json:"error"`
	SQLState string `json:"sql_state"`
}

// manifest is the member manifest of an answer that succeeded. It comes before
// the member result, so the driver knows the columns and their order before the
// first row (measured).
type manifest struct {
	Format string `json:"format"`
	Schema struct {
		ColumnCount int              `json:"column_count"`
		Columns     []manifestColumn `json:"columns"`
	} `json:"schema"`
	TotalChunkCount int  `json:"total_chunk_count"`
	Truncated       bool `json:"truncated"`
}

// rows reads the result of one statement, one token at a time (D25 and D193).
// The answer is one JSON object. Its members come in the order statement_id,
// status, manifest, result (measured). The member result holds the rows in
// data_array, each row an array of strings or nulls. The driver reads the
// answer of a statement that succeeded, and polls a statement that did not
// end, before it returns the rows, so the rows hold no context and no request
// (rule 4 of AGENTS.md). The one function that they hold is the cancel of the
// context of the timeout, which Close calls.
type rows struct {
	s      *dbimp.Stream
	dec    *jsontext.Decoder
	cancel context.CancelFunc

	cols []column
	// cur is the current row.
	cur []driver.Value
	// want is row_count of the result, and hasWant is false when the answer
	// names none.
	want    int64
	hasWant bool
	// count is the number of rows that the caller has read.
	count int64
	// inArray is true while the decoder is inside data_array.
	inArray bool
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the result is read to
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
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
)

// readAnswer reads the head of an answer with the status 200. It returns the
// rows, positioned before the first row, when the statement succeeded. It
// returns the id of the statement, and no rows, when the statement is still
// pending or running, and an error when it failed, was canceled or is closed. An
// error comes before any row, so it does not wrap dbimp.ErrIncomplete (D107).
// The body is closed unless the function returns rows.
func readAnswer(res *http.Response) (*rows, string, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), cancel: func() {}}
	id, pending, err := r.readHead()
	switch {
	case err != nil:
		_ = s.Close()
		return nil, id, err
	case pending:
		_ = s.Close()
		return nil, id, nil
	case r.done:
		// The body is at its end, so closing it returns the connection to the
		// pool (D36).
		_ = s.Close()
	}
	return r, id, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	names := make([]string, len(r.cols))
	for i, c := range r.cols {
		names[i] = c.name
	}
	return names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the name of the type in upper case, such as DECIMAL, with no
// parameters, and INTERVAL for each interval (D193).
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D193).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.cols[i].typ)
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The manifest
// has no member for NULL, so every column can be NULL and the driver does not
// know (measured).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. It is
// the precision and the scale of a DECIMAL column.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if t := r.cols[i].typ; t.kind == kindDecimal && t.precision >= 0 {
		return int64(t.precision), int64(t.scale), true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). A statement that ended needs no cancel on the server.
func (r *rows) Close() error {
	err := r.s.Close()
	r.cancel()
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the end of the answer, and
// returns its error (D21 and D36).
func (r *rows) NextRow() error {
	if r.done || !r.inArray {
		r.done = true
		return io.EOF
	}
	if r.dec.PeekKind() == ']' {
		if err := r.endRows(); err != nil {
			return r.fail(err)
		}
		return io.EOF
	}
	if err := r.readRow(); err != nil {
		return r.fail(err)
	}
	r.count++
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

// readRow reads and decodes one row.
func (r *rows) readRow() error {
	if err := expect(r.dec, '['); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	i := 0
	for ; r.dec.PeekKind() != ']'; i++ {
		if r.dec.PeekKind() == 0 {
			// The decoder holds an error, such as the end of the body, and
			// ReadToken returns it.
			_, err := r.dec.ReadToken()
			return fmt.Errorf("reading a row: %w", err)
		}
		if i >= len(r.cols) {
			return fmt.Errorf("reading a row: more than %d values: %w", len(r.cols), dbimp.ErrColumnCount)
		}
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		// The value is decoded before the next call to the decoder, which
		// reuses its buffer.
		if r.cur[i], err = decode(r.cols[i].typ, v); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i].name, err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a row: %w", err)
	}
	if i < len(r.cols) {
		return fmt.Errorf("reading a row: %d values for %d columns: %w", i, len(r.cols), dbimp.ErrColumnCount)
	}
	return nil
}

// endRows reads the end of data_array and the members that follow it, makes
// sure that the result held the rows that row_count named, and ends the answer.
func (r *rows) endRows() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the rows: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the result: %w", err)
		}
		if err := r.readOther(tok.String()); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the result: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the member %q: %w", tok.String(), err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	r.done, r.inArray = true, false
	if r.hasWant && r.count != r.want {
		return fmt.Errorf("reading the result: %d rows of %d: %w", r.count, r.want, ErrCut)
	}
	// The body is at its end, so closing it returns the connection to the
	// pool (D36).
	if err := r.s.Close(); err != nil {
		return fmt.Errorf("closing the answer: %w", err)
	}
	return nil
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// result returns the result of a statement that ran with Exec, whose rows the
// driver reads when they hold the count (D193 item 8). An answer that holds no
// column named num_affected_rows has no count, as a CREATE TABLE has none.
func (r *rows) result() (result, error) {
	if len(r.cols) == 0 || r.cols[0].name != columnAffected || r.cols[0].typ.kind != kindInteger {
		return result{}, nil
	}
	var res result
	for {
		err := r.NextRow()
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return result{}, err
		}
		if n, ok := r.cur[0].(int64); ok && !res.known {
			res = result{affected: n, known: true}
		}
	}
}

// readHead reads the members of the answer up to the first row. It returns the
// id of the statement, and pending is true for a statement that is still pending
// or running, whose small body it reads to its end.
func (r *rows) readHead() (string, bool, error) {
	var id string
	if err := expect(r.dec, '{'); err != nil {
		return id, false, fmt.Errorf("reading the answer: %w", err)
	}
	var state string
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return id, false, fmt.Errorf("reading the answer: %w", err)
		}
		switch name := tok.String(); name {
		case "statement_id":
			if err := json.UnmarshalDecode(r.dec, &id); err != nil {
				return id, false, fmt.Errorf("reading the id of the statement: %w", err)
			}
		case "status":
			var st status
			if err := json.UnmarshalDecode(r.dec, &st); err != nil {
				return id, false, fmt.Errorf("reading the status: %w", err)
			}
			state = st.State
			switch state {
			case stateSucceeded:
			case statePending, stateRunning:
				return id, true, r.drain()
			default:
				return id, false, r.stateError(id, &st)
			}
		case "manifest":
			var m manifest
			if err := json.UnmarshalDecode(r.dec, &m); err != nil {
				return id, false, fmt.Errorf("reading the manifest: %w", err)
			}
			if err := r.setManifest(&m); err != nil {
				return id, false, err
			}
		case "result":
			if state != stateSucceeded {
				return id, false, fmt.Errorf("reading the answer: a result comes before the status: %w", dbimp.ErrInvalidValue)
			}
			if err := r.readResult(); err != nil {
				return id, false, err
			}
			if r.inArray {
				return id, false, nil
			}
		default:
			if err := r.dec.SkipValue(); err != nil {
				return id, false, fmt.Errorf("reading the member %q: %w", name, err)
			}
		}
	}
	if state == "" {
		return id, false, fmt.Errorf("reading the answer: it has no status: %w", dbimp.ErrInvalidValue)
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return id, false, fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.s.End(); err != nil {
		return id, false, err
	}
	if r.cols == nil {
		r.cols = []column{}
	}
	r.done = true
	return id, false, nil
}

// drain reads the rest of the small body of a statement that is not at its
// end, or that ended in an error, so that the connection goes back to the pool.
func (r *rows) drain() error {
	for r.dec.PeekKind() != '}' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the status of a statement that runs: %w", err)
		}
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the status of a statement that runs: %w", err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the status of a statement that runs: %w", err)
	}
	return r.s.End()
}

// stateError returns the error of a statement that did not succeed.
func (r *rows) stateError(id string, st *status) error {
	e := &Error{HTTPStatus: http.StatusOK, StatementID: id, SQLState: st.SQLState, state: st.State}
	switch st.State {
	case stateFailed:
		if st.Error != nil {
			e.Code = codeText(st.Error.Code)
			e.Message = strings.TrimSpace(st.Error.Message)
		}
		if e.Message == "" {
			e.Message = "the statement failed"
		}
	case stateCanceled:
		e.Message = "the statement was canceled"
	case stateClosed:
		e.Message = "the result of the statement is closed"
	default:
		e.Message = fmt.Sprintf("the state of the statement is %q, which the driver does not know", st.State)
		return fmt.Errorf("reading the status: %w", e)
	}
	// A statement that ends in an error has no result, and the rest of the
	// body is small (measured).
	_ = r.drain()
	return e
}

// setManifest keeps the columns of the manifest, and refuses a result that the
// driver cannot read to its end (D193 item 2).
func (r *rows) setManifest(m *manifest) error {
	if m.Format != "" && m.Format != formatJSONArray {
		return fmt.Errorf("reading the manifest: the format is %q, and the driver reads %q: %w", m.Format, formatJSONArray, dbimp.ErrNotSupported)
	}
	if m.Truncated {
		return fmt.Errorf("reading the manifest: the server cut the result at a limit of rows or of bytes, and the driver sends no limit: %w", ErrTruncated)
	}
	if m.TotalChunkCount > 1 {
		return fmt.Errorf("reading the manifest: the result has %d chunks, and the driver reads the first only, with the disposition INLINE: %w", m.TotalChunkCount, dbimp.ErrNotSupported)
	}
	cols := m.Schema.Columns
	if m.Schema.ColumnCount != len(cols) {
		return fmt.Errorf("reading the manifest: %d columns for a column_count of %d: %w", len(cols), m.Schema.ColumnCount, dbimp.ErrColumnCount)
	}
	r.cols = make([]column, len(cols))
	for i, mc := range cols {
		c, err := newColumn(mc)
		if err != nil {
			return err
		}
		r.cols[i] = c
	}
	r.cur = make([]driver.Value, len(cols))
	return nil
}

// readResult reads the members of the member result up to data_array, and
// leaves the decoder inside the array of rows. A result with no rows, such as
// {}, has no data_array, and readResult reads its end.
func (r *rows) readResult() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the result: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the result: %w", err)
		}
		switch name := tok.String(); name {
		case "data_array":
			if r.cols == nil {
				return fmt.Errorf("reading the result: the rows come before the manifest: %w", dbimp.ErrInvalidValue)
			}
			if err := expect(r.dec, '['); err != nil {
				return fmt.Errorf("reading the rows: %w", err)
			}
			r.inArray = true
			return nil
		case "row_count":
			v, err := r.dec.ReadValue()
			if err != nil {
				return fmt.Errorf("reading row_count: %w", err)
			}
			if r.want, err = dbimp.Int64(v); err != nil {
				return fmt.Errorf("reading row_count: %w", err)
			}
			r.hasWant = true
		default:
			if err := r.readOther(name); err != nil {
				return err
			}
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the result: %w", err)
	}
	return nil
}

// readOther reads a member of the result that is not data_array. A link to
// another chunk, or an external link, is a result that the driver does not
// read (D193 item 2).
func (r *rows) readOther(name string) error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading the member %q: %w", name, err)
	}
	switch name {
	case "next_chunk_index", "next_chunk_internal_link", "external_links":
		if !dbimp.IsNull(v) {
			return fmt.Errorf("reading the result: the member %q names more of the result, and the driver reads the first chunk with the disposition INLINE only: %w", name, dbimp.ErrNotSupported)
		}
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
