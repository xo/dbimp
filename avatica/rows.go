package avatica

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"

	"github.com/xo/dbimp"
)

// rows reads the rows of one statement in frames, one token at a time (D25
// and D157). The answer to prepareAndExecute or execute holds the signature
// and the first frame, and the answer to each fetch holds the next frame.
// After the last row, or at Close, rows sends closeStatement.
type rows struct {
	c *conn
	// ctx is the context of the query, which each fetch takes (D157 and
	// hard rule 4).
	ctx       context.Context //nolint:containedctx // A fetch of the next frame starts in Next, which takes no context (D157).
	stmtID    int64
	frameSize int

	st  *dbimp.Stream
	dec *jsontext.Decoder
	// tail reads the rest of the answer after the rows of its frame, from
	// the innermost object to the outermost.
	tail []func() error

	columns     []column
	cols        []string
	cur         []driver.Value
	updateCount int64
	// offset is the offset of the frame, n the count of its rows that were
	// read, and done is true for the last frame.
	offset  int64
	n       int64
	done    bool
	missing bool
	inRows  bool
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and closed once the statement is
	// closed.
	read   bool
	closed bool
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

// errMissingStatement is the error of an answer that says that the server
// does not know the statement, which it sends with no error (measured).
const errMissingStatement dbimp.Error = "the server does not know the statement"

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the name of the type that the server gives, in upper case, such as
// CHARACTER, INTERVAL DAY TO SECOND or INTEGER ARRAY.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return strings.ToUpper(r.columns[i].Type.Name)
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D155).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return scanType(r.columns[i].Type)
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable, from nullable
// of the signature, which is 2 when the server does not know.
func (r *rows) ColumnTypeNullable(i int) (bool, bool) {
	switch r.columns[i].Nullable {
	case 0:
		return false, true
	case 1:
		return true, true
	}
	return false, false
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength, from precision of
// the signature, for a text or a binary type. Phoenix gives 0 for a length
// that has no bound (measured).
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	c := r.columns[i]
	switch c.Type.kind() {
	case typeChar, typeVarchar, typeBinary, typeVarbinary:
		if c.Precision <= 0 {
			return math.MaxInt64, true
		}
		return c.Precision, true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale, from
// precision and scale of the signature, for a DECIMAL.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	c := r.columns[i]
	if c.Type.kind() != typeDecimal {
		return 0, 0, false
	}
	return c.Precision, c.Scale, true
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. At the end of a frame that is not the last, it fetches
// the next frame. After the last row, it closes the statement.
func (r *rows) NextRow() error {
	for {
		if r.closed || !r.inRows {
			return io.EOF
		}
		if r.dec.PeekKind() == '[' {
			if err := r.row(); err != nil {
				return r.fail(err)
			}
			r.read = true
			return nil
		}
		if err := expect(r.dec, ']'); err != nil {
			return r.fail(fmt.Errorf("reading the rows of a frame: %w", err))
		}
		r.inRows = false
		if err := r.end(); err != nil {
			return r.fail(err)
		}
		if r.done {
			if err := r.release(); err != nil {
				return r.fail(err)
			}
			return io.EOF
		}
		if r.n == 0 {
			return r.fail(fmt.Errorf("reading a frame at %d: a frame that is not the last has no rows: %w", r.offset, dbimp.ErrInvalidValue))
		}
		next := r.offset + r.n
		err := r.send(map[string]any{
			"request":          "fetch",
			"connectionId":     r.c.id,
			"statementId":      r.stmtID,
			"offset":           next,
			"fetchMaxRowCount": r.frameSize,
		}, r.topFetch)
		if err == nil && !r.inRows {
			if err = r.end(); err == nil {
				err = fmt.Errorf("reading the frame at %d: the answer has no rows: %w", next, dbimp.ErrInvalidValue)
			}
		}
		if err != nil {
			return r.fail(err)
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
// nothing more (D36). It then closes the statement (D159).
func (r *rows) Close() error {
	if r.closed {
		return nil
	}
	var err error
	if r.st != nil {
		err = r.st.Close()
	}
	return errors.Join(err, r.release())
}

// open sends req, which runs the statement, and reads its answer up to its
// first row, or to its end. An error before any row is the error of the
// statement, and closes the statement.
func (r *rows) open(req map[string]any) error {
	r.cols = []string{}
	r.updateCount = -1
	if err := r.send(req, r.topExecute); err != nil {
		return r.fail(err)
	}
	if !r.inRows {
		if err := r.end(); err != nil {
			return r.fail(err)
		}
	}
	return nil
}

// send posts req, and reads its answer with top, which reads each member of
// the object of the answer, up to the rows of its frame.
func (r *rows) send(req map[string]any, top func(string) (bool, error)) error {
	res, err := r.c.c.post(r.ctx, req)
	if err != nil {
		return r.c.check(err)
	}
	r.st = dbimp.NewStream(res.Body)
	r.dec = r.st.Decoder()
	r.tail, r.inRows, r.n = nil, false, 0
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer to %v: %w", req["request"], err)
	}
	stopped, err := members(r.dec, top)
	if err != nil {
		return fmt.Errorf("reading the answer to %v: %w", req["request"], err)
	}
	if stopped {
		r.tail = append(r.tail, func() error {
			_, err := members(r.dec, top)
			return err
		})
	}
	return nil
}

// topExecute reads one member of the answer to prepareAndExecute or execute.
func (r *rows) topExecute(name string) (bool, error) {
	switch name {
	case "missingStatement":
		return false, r.readMissing()
	case "results":
		if r.dec.PeekKind() == 'n' {
			return false, r.dec.SkipValue()
		}
		if err := expect(r.dec, '['); err != nil {
			return false, err
		}
		if r.dec.PeekKind() != ']' {
			if err := expect(r.dec, '{'); err != nil {
				return false, err
			}
			stopped, err := members(r.dec, r.result)
			if err != nil {
				return false, err
			}
			if stopped {
				r.tail = append(r.tail, func() error {
					if _, err := members(r.dec, r.result); err != nil {
						return err
					}
					return skipRest(r.dec)
				})
				return true, nil
			}
		}
		return false, skipRest(r.dec)
	}
	return false, r.dec.SkipValue()
}

// topFetch reads one member of the answer to fetch.
func (r *rows) topFetch(name string) (bool, error) {
	switch name {
	case "missingStatement":
		return false, r.readMissing()
	case "frame":
		return r.frame()
	}
	return false, r.dec.SkipValue()
}

// readMissing reads missingStatement, which the server sends as true, with
// no error, for a statement that it does not know (measured).
func (r *rows) readMissing() error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return err
	}
	r.missing = !dbimp.IsNull(v) && string(v) == "true"
	return nil
}

// result reads one member of a result of an answer.
func (r *rows) result(name string) (bool, error) {
	switch name {
	case "signature":
		v, err := r.dec.ReadValue()
		if err != nil {
			return false, err
		}
		if dbimp.IsNull(v) {
			return false, nil
		}
		var sig struct {
			Columns []column `json:"columns"`
		}
		if err := json.Unmarshal(v, &sig); err != nil {
			return false, fmt.Errorf("reading the signature: %w", err)
		}
		r.columns = sig.Columns
		r.cols = make([]string, len(sig.Columns))
		for i, c := range sig.Columns {
			r.cols[i] = c.Label
		}
		r.cur = make([]driver.Value, len(sig.Columns))
		return false, nil
	case "firstFrame":
		return r.frame()
	case "updateCount":
		v, err := r.dec.ReadValue()
		if err != nil {
			return false, err
		}
		if !dbimp.IsNull(v) {
			if r.updateCount, err = dbimp.Int64(v); err != nil {
				return false, fmt.Errorf("reading updateCount: %w", err)
			}
		}
		return false, nil
	}
	return false, r.dec.SkipValue()
}

// frame reads a frame up to its rows, and returns true when it stops there.
func (r *rows) frame() (bool, error) {
	if r.dec.PeekKind() == 'n' {
		return false, r.dec.SkipValue()
	}
	if err := expect(r.dec, '{'); err != nil {
		return false, err
	}
	stopped, err := members(r.dec, r.frameMember)
	if err != nil || !stopped {
		return false, err
	}
	r.tail = append(r.tail, func() error {
		_, err := members(r.dec, r.frameMember)
		return err
	})
	return true, nil
}

// frameMember reads one member of a frame, and stops inside its rows.
func (r *rows) frameMember(name string) (bool, error) {
	switch name {
	case "rows":
		if r.dec.PeekKind() == 'n' {
			return false, r.dec.SkipValue()
		}
		if r.columns == nil {
			return false, fmt.Errorf("reading the rows of a frame before the signature: %w", dbimp.ErrInvalidValue)
		}
		if err := expect(r.dec, '['); err != nil {
			return false, err
		}
		r.inRows = true
		return true, nil
	case "offset":
		v, err := r.dec.ReadValue()
		if err != nil {
			return false, err
		}
		r.offset, err = dbimp.Int64(v)
		return false, err
	case "done":
		v, err := r.dec.ReadValue()
		if err != nil {
			return false, err
		}
		r.done, err = dbimp.Bool(v)
		return false, err
	}
	return false, r.dec.SkipValue()
}

// end reads the answer to its end after the rows of its frame, and returns
// the error of a statement that the server does not know.
func (r *rows) end() error {
	for _, f := range r.tail {
		if err := f(); err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
	}
	r.tail = nil
	if err := r.st.End(); err != nil {
		return err
	}
	if r.missing {
		return fmt.Errorf("reading the statement %d: %w", r.stmtID, errMissingStatement)
	}
	return nil
}

// row reads one row of a frame into cur.
func (r *rows) row() error {
	if err := expect(r.dec, '['); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	for i := range r.cur {
		if r.dec.PeekKind() == ']' {
			return fmt.Errorf("reading a row of %d values for %d columns: %w", i, len(r.cur), dbimp.ErrColumnCount)
		}
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		if r.cur[i], err = decode(r.columns[i].Type, v); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i], err)
		}
	}
	if r.dec.PeekKind() != ']' {
		return fmt.Errorf("reading a row of more values than its %d columns: %w", len(r.cur), dbimp.ErrColumnCount)
	}
	r.n++
	return expect(r.dec, ']')
}

// fail ends the rows with err, which wraps dbimp.ErrIncomplete after a row
// reached the caller (D107).
func (r *rows) fail(err error) error {
	if r.st != nil {
		_ = r.st.Close()
	}
	if rerr := r.release(); rerr != nil {
		err = errors.Join(err, rerr)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// release closes the statement once.
func (r *rows) release() error {
	if r.closed {
		return nil
	}
	r.closed, r.inRows = true, false
	if r.c.bad {
		return nil
	}
	return r.c.closeStatement(r.ctx, r.stmtID)
}

// expect reads the next token, which must be the delimiter kind.
func expect(dec *jsontext.Decoder, kind jsontext.Kind) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != kind {
		return fmt.Errorf("reading %v where %v belongs: %w", tok.Kind(), kind, dbimp.ErrInvalidValue)
	}
	return nil
}

// members reads the members of an object whose start is read, and calls fn
// with the name of each, which reads its value. It reads the end of the
// object. If fn returns true, members returns true at once, and a later call
// reads the rest of the object.
func members(dec *jsontext.Decoder, fn func(string) (bool, error)) (bool, error) {
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return false, err
		}
		stop, err := fn(tok.String())
		if err != nil || stop {
			return stop, err
		}
	}
	return false, expect(dec, '}')
}

// skipRest reads the rest of an array whose start is read, and its end.
func skipRest(dec *jsontext.Decoder) error {
	for dec.PeekKind() != ']' {
		if err := dec.SkipValue(); err != nil {
			return err
		}
	}
	return expect(dec, ']')
}
