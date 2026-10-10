package spanner

import (
	"bytes"
	"context"
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

// metadata is the member metadata of a message. Only the first message of a
// stream has it (recorded: "a stream resumed with the token" is the one that
// has none, and the driver never resumes, D191 item 4).
type metadata struct {
	RowType struct {
		Fields []field `json:"fields"`
	} `json:"rowType"`
}

// wireStats is the member stats of the last message of a statement that
// changes rows (recorded: "an insert in the transaction").
type wireStats struct {
	Exact      *int64 `json:"rowCountExact,string"`
	LowerBound *int64 `json:"rowCountLowerBound,string"`
}

// rows reads the result of executeStreamingSql, one token at a time (D25 and
// D191). The body is a JSON array of PartialResultSet messages. A message has
// values, which is a flat list of the values of many rows in the order of the
// columns, and the first message has metadata. The members can come in any
// order (recorded: "a statement on the stream" writes values before metadata).
// The driver cuts the list of values by the number of columns.
//
// The server splits a value of more than about 1 MiB across messages, and
// marks the message that holds the start with chunkedValue (D191 item 11). The
// member can come after the values, so the driver holds back the last value of
// a message until the message ends, then keeps it as the start of the next
// value when the message is chunked, and joins it to the first value of the
// next message as text before it decodes the value.
type rows struct {
	s   *dbimp.Stream
	dec *jsontext.Decoder
	// ctxErr returns the error of the context of the statement, or nil. The
	// rows hold no context (rule 4 of AGENTS.md).
	ctxErr func() error
	// tx is the transaction of the statement, which keeps the precommit token
	// that a message carries, or nil.
	tx *txn
	// finish ends the transaction that the driver began for the statement, and
	// is nil for any other statement. It commits when ok is true and rolls back
	// when it is false (D191 item 6).
	finish   func(ok bool) error
	finished bool
	// skip is true when the rows decode no value, for Exec.
	skip bool

	cols  []string
	types []wireType

	// The state of the read of the array of messages.
	started  bool
	inElem   bool
	inValues bool
	// chunked is true when the message that is open has chunkedValue.
	chunked bool
	// early holds the values that came before the metadata, in raw text.
	early []jsontext.Value
	// held is the last value that came, which the driver processes when the next
	// one comes or the message ends. partial is the start of a value that the
	// next message continues.
	held       jsontext.Value
	hasHeld    bool
	partial    jsontext.Value
	hasPartial bool

	// cur is the row that the driver builds, n the number of its values, and
	// queue the rows that are complete and not yet returned.
	cur   []driver.Value
	n     int
	queue [][]driver.Value
	row   []driver.Value

	stats *wireStats
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the result is read to its
	// end. failed is true after an error.
	read   bool
	done   bool
	failed bool
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner              = (*rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*rows)(nil)
)

// newRows returns the rows of the response res, read up to the metadata. It
// reads the first message, so an error of the statement comes before any row,
// and does not wrap dbimp.ErrIncomplete (D107). The error of the response is the
// element that holds error, whatever the HTTP status was.
func newRows(ctx context.Context, res *http.Response, tx *txn, skip bool, finish func(ok bool) error) (*rows, error) {
	s := dbimp.NewStream(res.Body)
	r := &rows{s: s, dec: s.Decoder(), ctxErr: ctx.Err, tx: tx, skip: skip, finish: finish}
	for r.types == nil && !r.done {
		if err := r.advance(); err != nil {
			_ = s.Close()
			if isEOF(err) {
				err = fmt.Errorf("reading the answer: it has no metadata: %w", dbimp.ErrInvalidValue)
			}
			return nil, r.cause(err)
		}
	}
	return r, nil
}

// isEOF reports whether err is the end of a result.
func isEOF(err error) bool {
	return errors.Is(err, io.EOF)
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the code of the type, such as INT64. An array is ARRAY.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.types[i].Code
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D191).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return r.types[i].scanType()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The metadata
// names no nullability, so every column can be NULL (D18 and D135).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. A
// NUMERIC column has a precision of 38 and a scale of 9 (recorded: "NUMERIC
// values"). No other type has either.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if r.types[i].Code == wireNumeric {
		return numericPrecision, numericScale, true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36). When the driver began a transaction for the statement, Close ends it:
// it commits, unless the statement failed, and then it rolls back (D191 item
// 6). A caller that reads one row of a result that holds a row, as QueryRow
// does, still commits.
func (r *rows) Close() error {
	err := r.s.Close()
	if ferr := r.end(!r.failed); ferr != nil && err == nil {
		err = ferr
	}
	return err
}

// NextRow satisfies driver.RowsColumnScanner. It reads the stream up to the
// next complete row, which ScanColumn assigns. After the last message it reads
// the end of the body, and ends the transaction that the driver began, so a
// failed commit is the error of the last call (D21).
func (r *rows) NextRow() error {
	for {
		if len(r.queue) > 0 {
			r.row, r.queue = r.queue[0], r.queue[1:]
			r.read = true
			return nil
		}
		if r.done {
			return io.EOF
		}
		if err := r.advance(); err != nil {
			if isEOF(err) {
				if cerr := r.end(true); cerr != nil {
					return r.fail(cerr)
				}
				continue
			}
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
	copy(dest, r.row)
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.row[i])
}

// end ends the transaction of the statement once.
func (r *rows) end(ok bool) error {
	if r.finish == nil || r.finished {
		return nil
	}
	r.finished = true
	return r.finish(ok)
}

// affected returns the count of the rows that a statement changed, and false
// when the answer holds no count. A partitioned DML statement has a lower bound
// only (recorded: "a partitioned DML statement").
func (r *rows) affected() (int64, bool) {
	switch {
	case r.stats == nil:
		return 0, false
	case r.stats.Exact != nil:
		return *r.stats.Exact, true
	case r.stats.LowerBound != nil:
		return *r.stats.LowerBound, true
	}
	return 0, false
}

// advance reads one step of the array of messages: the start of the array, the
// start or the end of a message, one member, or one value. It returns io.EOF
// when the array ended and the body is at its end.
func (r *rows) advance() error {
	switch {
	case !r.started:
		if err := expect(r.dec, '['); err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		r.started = true
		return nil
	case r.inValues:
		return r.readValue()
	case r.inElem:
		return r.readMember()
	}
	switch r.dec.PeekKind() {
	case ']':
		return r.endArray()
	case 0:
		_, err := r.dec.ReadToken()
		return fmt.Errorf("reading the answer: %w", err)
	}
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading a message: %w", err)
	}
	r.inElem, r.chunked = true, false
	return nil
}

// endArray reads the end of the array and of the body, and makes sure that no
// value waits to be joined.
func (r *rows) endArray() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	if r.hasPartial || r.hasHeld || len(r.early) > 0 {
		return fmt.Errorf("reading the end of the answer: a value is cut: %w", dbimp.ErrInvalidValue)
	}
	if r.n != 0 {
		return fmt.Errorf("reading the end of the answer: a row has %d values of %d: %w", r.n, len(r.cols), dbimp.ErrColumnCount)
	}
	r.done = true
	return io.EOF
}

// readMember reads one member of the open message, or its end.
func (r *rows) readMember() error {
	if r.dec.PeekKind() == '}' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of a message: %w", err)
		}
		r.inElem = false
		return r.endMessage()
	}
	tok, err := r.dec.ReadToken()
	if err != nil {
		return fmt.Errorf("reading a message: %w", err)
	}
	// The token is void after the next call to the decoder, so the driver reads
	// the name first.
	switch name := tok.String(); name {
	case "values":
		if err := expect(r.dec, '['); err != nil {
			return fmt.Errorf("reading the values: %w", err)
		}
		r.inValues = true
	case "metadata":
		var m metadata
		if err := json.UnmarshalDecode(r.dec, &m); err != nil {
			return fmt.Errorf("reading the metadata: %w", err)
		}
		return r.setMetadata(m)
	case "chunkedValue":
		if err := json.UnmarshalDecode(r.dec, &r.chunked); err != nil {
			return fmt.Errorf("reading chunkedValue: %w", err)
		}
	case "stats":
		var st wireStats
		if err := json.UnmarshalDecode(r.dec, &st); err != nil {
			return fmt.Errorf("reading the stats: %w", err)
		}
		r.stats = &st
	case "precommitToken":
		var pt precommit
		if err := json.UnmarshalDecode(r.dec, &pt); err != nil {
			return fmt.Errorf("reading the precommit token: %w", err)
		}
		r.tx.setPrecommit(pt)
	case "error":
		// An element that holds error ends the result with that error,
		// whatever the HTTP status was. The message before it can hold a row
		// that is not complete, and the driver drops it (recorded: "a division
		// by zero in row 3500 of 4000, unsorted, on the stream").
		var w wireError
		if err := json.UnmarshalDecode(r.dec, &w); err != nil {
			return fmt.Errorf("reading the error: %w", err)
		}
		return newErr(w)
	default:
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the member %q: %w", name, err)
		}
	}
	return nil
}

// readValue reads one value of the open list, or its end.
func (r *rows) readValue() error {
	if r.dec.PeekKind() == ']' {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of the values: %w", err)
		}
		r.inValues = false
		return nil
	}
	if r.skip {
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading a value: %w", err)
		}
		return nil
	}
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading a value: %w", err)
	}
	// The decoder reuses its buffer, so the driver keeps a copy.
	return r.push(bytes.Clone(v))
}

// precommit is the member precommitToken of a message or an answer. A commit on
// a multiplexed session sends the one with the highest seqNum (recorded: "commit
// on the multiplexed session with the precommit token").
type precommit struct {
	Token  string `json:"precommitToken"`
	SeqNum int64  `json:"seqNum"`
}

// setMetadata keeps the columns, and processes the values that came before them.
func (r *rows) setMetadata(m metadata) error {
	if r.types != nil {
		return nil
	}
	r.types = make([]wireType, len(m.RowType.Fields))
	r.cols = make([]string, len(m.RowType.Fields))
	for i, f := range m.RowType.Fields {
		if err := f.Type.supported(f.Name); err != nil {
			return err
		}
		r.cols[i], r.types[i] = f.Name, f.Type
	}
	r.cur = make([]driver.Value, len(r.cols))
	early := r.early
	r.early = nil
	for _, v := range early {
		if err := r.cell(v); err != nil {
			return err
		}
	}
	return nil
}

// push takes the next value of a message. The first value of a message that
// continues a chunked value is joined to its start. The value that came before
// is complete when this one comes, because it is not the last of its message.
func (r *rows) push(v jsontext.Value) error {
	if r.hasPartial {
		joined, err := join(r.partial, v)
		if err != nil {
			return err
		}
		v, r.partial, r.hasPartial = joined, nil, false
	}
	if r.hasHeld {
		if err := r.cell(r.held); err != nil {
			return err
		}
	}
	r.held, r.hasHeld = v, true
	return nil
}

// endMessage handles the last value of a message, which stays held until the
// message ends, because chunkedValue can come after the values. A chunked value
// waits for the next message.
func (r *rows) endMessage() error {
	switch {
	case r.chunked && !r.hasHeld:
		return fmt.Errorf("reading a message: chunkedValue is true and the message has no value: %w", dbimp.ErrInvalidValue)
	case r.chunked:
		r.partial, r.hasPartial = r.held, true
		r.held, r.hasHeld = nil, false
	case r.hasHeld:
		v := r.held
		r.held, r.hasHeld = nil, false
		return r.cell(v)
	}
	return nil
}

// cell decodes one complete value, and stores it in the row that the driver
// builds. A value that came before the metadata waits for it.
func (r *rows) cell(v jsontext.Value) error {
	if r.types == nil {
		r.early = append(r.early, v)
		return nil
	}
	if len(r.cols) == 0 {
		return fmt.Errorf("reading a value: the statement has no columns: %w", dbimp.ErrColumnCount)
	}
	x, err := r.types[r.n].decode(v)
	if err != nil {
		return fmt.Errorf("reading the column %s: %w", r.cols[r.n], err)
	}
	r.cur[r.n] = x
	if r.n++; r.n == len(r.cols) {
		r.queue = append(r.queue, r.cur)
		r.cur, r.n = make([]driver.Value, len(r.cols)), 0
	}
	return nil
}

// join returns the value that the pieces a and b make. The server splits a
// STRING, a BYTES value and a JSON value as text, and the driver joins the
// pieces before it decodes the base64 (docs/SPANNER.md, "Responses"). It joins
// two lists as the protocol says: the last element of the first and the first
// element of the second join too, when both are strings or both are lists.
func join(a, b jsontext.Value) (jsontext.Value, error) {
	switch {
	case a.Kind() == '"' && b.Kind() == '"':
		x, err := dbimp.String(a)
		if err != nil {
			return nil, err
		}
		y, err := dbimp.String(b)
		if err != nil {
			return nil, err
		}
		return jsontext.AppendQuote(nil, x+y)
	case a.Kind() == '[' && b.Kind() == '[':
		var x, y []any
		if err := json.Unmarshal(a, &x); err != nil {
			return nil, fmt.Errorf("joining the pieces of a list: %w", err)
		}
		if err := json.Unmarshal(b, &y); err != nil {
			return nil, fmt.Errorf("joining the pieces of a list: %w", err)
		}
		out, err := json.Marshal(joinLists(x, y))
		if err != nil {
			return nil, fmt.Errorf("joining the pieces of a list: %w", err)
		}
		return out, nil
	}
	return nil, fmt.Errorf("joining the pieces of a value: they are %s and %s, and the driver joins strings and lists: %w", a.Kind(), b.Kind(), dbimp.ErrNotSupported)
}

// joinLists joins two lists. The last element of x and the first element of y
// join when both are strings or both are lists.
func joinLists(x, y []any) []any {
	if len(x) == 0 || len(y) == 0 {
		return append(x, y...)
	}
	last, first := x[len(x)-1], y[0]
	switch l := last.(type) {
	case string:
		if f, ok := first.(string); ok {
			x[len(x)-1] = l + f
			return append(x, y[1:]...)
		}
	case []any:
		if f, ok := first.([]any); ok {
			x[len(x)-1] = joinLists(l, f)
			return append(x, y[1:]...)
		}
	}
	return append(x, y...)
}

// cause adds the error of the context of the statement to err. When the
// context ends, the transport can close the connection before the read sees
// the end, and the read then fails with "use of closed network connection".
// The caller must see that its context ended (D36).
func (r *rows) cause(err error) error {
	if cerr := r.ctxErr(); cerr != nil && !errors.Is(err, cerr) {
		return fmt.Errorf("%w: %w", cerr, err)
	}
	return err
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done, r.failed = true, true
	r.queue = nil
	err = r.cause(err)
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

// emptyRows is the result of a statement that has no rows, such as a DDL
// statement that runs with Query.
type emptyRows struct{}

func (emptyRows) Columns() []string         { return []string{} }
func (emptyRows) Close() error              { return nil }
func (emptyRows) Next([]driver.Value) error { return io.EOF }
