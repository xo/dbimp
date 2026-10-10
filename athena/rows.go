package athena

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

	"github.com/xo/dbimp"
)

// rows reads the result of one query, which is a series of pages (D21 and
// D192). A page is the answer of one GetQueryResults. It is a JSON object with
// the members NextToken, Output, ResultSet and UpdateCount, in that order
// (recorded: "the second page"). ResultSet holds the columns in ColumnInfos, the
// rows in ResultRows, and the same two again in ResultSetMetadata and in Rows,
// the form that the SDK names, which the driver skips (recorded: "a select").
// Each row is {"Data": ["1", null]}. The driver reads each page one token at a
// time, so a page is never held in memory (D25).
//
// The rows keep the context of the statement, because the call for each page
// after the first runs in Next, which has no context of its own. This is an
// exception to hard rule 4 of AGENTS.md, as for the rows of Trino and of
// DynamoDB (D192 item 5). The query has ended before the first page, so the
// rows have nothing to stop on the server.
type rows struct {
	c   *Connector
	ctx context.Context //nolint:containedctx // D192 item 5 keeps the context of the statement for the call of each page after the first.
	id  string

	stream *dbimp.Stream
	dec    *jsontext.Decoder

	cols []column
	cur  []driver.Value
	// haveCols is true once a page named the columns, even none.
	haveCols bool
	// next is the NextToken of the page that the rows are in, and "" for the
	// last page.
	next string
	// header is true while the rows still have to skip the header row, which
	// is the first row of the first page of a DML statement (recorded: "a
	// select").
	header bool
	// skip is true when the rows decode no value, for Exec.
	skip bool
	// count is UpdateCount, and hasCount is false when the answer has none
	// (recorded: "describe the table").
	count    int64
	hasCount bool
	// page is the index of the page that the rows are in.
	page int
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the result is read to its
	// end.
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

// newRows returns the rows of the answer res to the first GetQueryResults of
// the query id. It reads the first page up to its first row, and drops the
// header row when header is true. A failure here comes before any row, so it
// does not wrap dbimp.ErrIncomplete (D107).
func newRows(ctx context.Context, c *Connector, id string, res *http.Response, header, skip bool) (*rows, error) {
	r := &rows{c: c, ctx: ctx, id: id, header: header, skip: skip}
	if err := r.begin(res.Body); err != nil {
		_ = r.stream.Close()
		return nil, r.cause(err)
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	names := make([]string, len(r.cols))
	for i, c := range r.cols {
		names[i] = c.Name
	}
	return names
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// It is the name of the type in upper case, such as BIGINT or TIMESTAMP WITH
// TIME ZONE.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D192).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return r.cols[i].scanType()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The member
// Nullable is always UNKNOWN (recorded), and every type can be NULL (D192).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. It is the length of a
// char and of a varchar that names one. A varchar with no length, a string and a
// varbinary have the largest length (recorded: "the type of CAST('héllo' AS
// varchar)").
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	c := r.cols[i]
	switch c.wire() {
	case typeChar, typeVarchar:
		if c.Precision > 0 && c.Precision < 1<<31-1 {
			return c.Precision, true
		}
		return maxLength, true
	case typeString, typeVarbinary:
		return maxLength, true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. It is
// the precision and the scale of a decimal.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if c := r.cols[i]; c.wire() == typeDecimal {
		return c.Precision, c.Scale, true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body of the page, and reads
// nothing more (D36). The query has ended, so there is nothing to stop.
func (r *rows) Close() error {
	return r.stream.Close()
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row of a page, it reads the end of the
// page and fetches the next one. After the last row of the last page, it reads
// the end of the answer (D21 and D36).
func (r *rows) NextRow() error {
	for {
		if r.done {
			return io.EOF
		}
		if r.dec.PeekKind() == ']' {
			if err := r.endPage(); err != nil {
				return r.fail(err)
			}
			continue
		}
		if err := r.readRow(r.skip); err != nil {
			return r.fail(err)
		}
		r.read = true
		return nil
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

// result returns the result of a statement that ran with Exec, whose rows the
// driver read to the end.
func (r *rows) result() result {
	return result{affected: r.count, known: r.hasCount}
}

// begin starts the page whose body is body, and reads it up to its first row.
// The header row is read as well, when the rows have to drop it.
func (r *rows) begin(body io.ReadCloser) error {
	r.stream = dbimp.NewStream(body)
	r.dec = r.stream.Decoder()
	r.next = ""
	if err := r.readHead(); err != nil {
		return err
	}
	if r.header {
		r.header = false
		if r.dec.PeekKind() != ']' {
			if err := r.readRow(true); err != nil {
				return err
			}
		}
	}
	return nil
}

// readHead reads the page up to the opening of ResultRows, and leaves the
// decoder inside the array of the rows.
func (r *rows) readHead() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		name, err := r.member()
		if err != nil {
			return err
		}
		if name != "ResultSet" {
			if err := r.readOther(name); err != nil {
				return err
			}
			continue
		}
		return r.readResultSet()
	}
	return fmt.Errorf("reading the answer: it has no ResultSet: %w", dbimp.ErrInvalidValue)
}

// member reads the name of the next member of an object.
func (r *rows) member() (string, error) {
	tok, err := r.dec.ReadToken()
	if err != nil {
		return "", fmt.Errorf("reading the answer: %w", err)
	}
	return tok.String(), nil
}

// readResultSet reads ResultSet up to the opening of ResultRows.
func (r *rows) readResultSet() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the result set: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		name, err := r.member()
		if err != nil {
			return err
		}
		switch name {
		case "ColumnInfos":
			if err := r.readColumns(); err != nil {
				return err
			}
		case "ResultSetMetadata":
			if err := r.readMetadata(); err != nil {
				return err
			}
		case "ResultRows":
			if !r.haveCols {
				return fmt.Errorf("reading the rows: they come before the columns: %w", dbimp.ErrInvalidValue)
			}
			if err := expect(r.dec, '['); err != nil {
				return fmt.Errorf("reading the rows: %w", err)
			}
			return nil
		default:
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading the member %q: %w", name, err)
			}
		}
	}
	return fmt.Errorf("reading the result set: it has no ResultRows: %w", dbimp.ErrInvalidValue)
}

// readColumns reads ColumnInfos. A page after the first names the columns
// again, and the driver keeps the first.
func (r *rows) readColumns() error {
	if r.haveCols {
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the columns: %w", err)
		}
		return nil
	}
	var cols []column
	if err := json.UnmarshalDecode(r.dec, &cols); err != nil {
		return fmt.Errorf("reading the columns: %w", err)
	}
	r.setColumns(cols)
	return nil
}

// readMetadata reads ResultSetMetadata, which holds the columns again in the
// form that the SDK names. The driver reads them from it only when ColumnInfos
// did not come first.
func (r *rows) readMetadata() error {
	if r.haveCols {
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the metadata: %w", err)
		}
		return nil
	}
	var m struct {
		ColumnInfo []column `json:"ColumnInfo"`
	}
	if err := json.UnmarshalDecode(r.dec, &m); err != nil {
		return fmt.Errorf("reading the metadata: %w", err)
	}
	r.setColumns(m.ColumnInfo)
	return nil
}

// setColumns keeps the columns of the result.
func (r *rows) setColumns(cols []column) {
	r.cols = cols
	r.haveCols = true
	r.cur = make([]driver.Value, len(cols))
}

// readOther reads a member that is not ResultSet: NextToken, UpdateCount, and
// the members that the driver does not use, such as Output.
func (r *rows) readOther(name string) error {
	switch name {
	case "NextToken":
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading NextToken: %w", err)
		}
		if !dbimp.IsNull(v) {
			if r.next, err = dbimp.String(v); err != nil {
				return fmt.Errorf("reading NextToken: %w", err)
			}
		}
	case "UpdateCount":
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading UpdateCount: %w", err)
		}
		if !dbimp.IsNull(v) {
			if r.count, err = dbimp.Int64(v); err != nil {
				return fmt.Errorf("reading UpdateCount: %w", err)
			}
			r.hasCount = true
		}
	default:
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the member %q: %w", name, err)
		}
	}
	return nil
}

// readRow reads one row, {"Data": [...]}, and decodes its values. A row can
// hold fewer values than the columns, as the row of DESCRIBE does (recorded:
// "describe the table"), and the values that it lacks are NULL. skip reads the
// values and decodes none.
func (r *rows) readRow(skip bool) error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	clear(r.cur)
	for r.dec.PeekKind() != '}' {
		name, err := r.member()
		if err != nil {
			return err
		}
		if name != "Data" {
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading a row: %w", err)
			}
			continue
		}
		if err := r.readData(skip); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a row: %w", err)
	}
	return nil
}

// readData reads the array Data of a row.
func (r *rows) readData(skip bool) error {
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
		// A statement with no column can still send a row, such as DROP TABLE
		// (recorded in the live run of 2026-10-10), and the driver drops it.
		if i >= len(r.cols) && len(r.cols) > 0 && !skip {
			return fmt.Errorf("reading a row: more than %d values: %w", len(r.cols), dbimp.ErrColumnCount)
		}
		if skip || len(r.cols) == 0 {
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading a row: %w", err)
			}
			continue
		}
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		// The value is decoded before the next call to the decoder, which
		// reuses its buffer.
		if r.cur[i], err = r.decode(r.cols[i], v); err != nil {
			return fmt.Errorf("reading the column %s: %w", r.cols[i].Name, err)
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a row: %w", err)
	}
	return nil
}

// decode returns the value v of a column: nil for a NULL, which has no text in
// the answer, and otherwise the Go value of the text.
func (r *rows) decode(c column, v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	s, err := dbimp.String(v)
	if err != nil {
		return nil, err
	}
	return c.decode(s)
}

// endPage reads the end of the rows of the page and the members that follow
// them, and opens the next page when the page named a token. After the last
// page, it ends the result.
func (r *rows) endPage() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the rows: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		name, err := r.member()
		if err != nil {
			return err
		}
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the member %q: %w", name, err)
		}
	}
	// The end of ResultSet.
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the result set: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		name, err := r.member()
		if err != nil {
			return err
		}
		if err := r.readOther(name); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.stream.End(); err != nil {
		return err
	}
	// The body is at its end, so closing it returns the connection to the pool
	// (D36).
	if err := r.stream.Close(); err != nil {
		return fmt.Errorf("closing the page %d: %w", r.page, err)
	}
	if r.next == "" {
		r.done = true
		return nil
	}
	return r.open(r.next)
}

// open fetches the page after token and reads it up to its first row.
func (r *rows) open(token string) error {
	res, err := r.c.results(r.ctx, r.id, token)
	if err != nil {
		return fmt.Errorf("fetching the page %d: %w", r.page+1, err)
	}
	r.page++
	if err := r.begin(res.Body); err != nil {
		return fmt.Errorf("reading the page %d: %w", r.page, err)
	}
	return nil
}

// cause adds the error of the context to err when the context ended. When the
// context ends, the transport can close the connection before the read sees the
// end, and the read then fails with "use of closed network connection". The
// caller must see that its context ended (D36).
func (r *rows) cause(err error) error {
	if cerr := r.ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
		return fmt.Errorf("%w: %w", cerr, err)
	}
	return err
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107), which holds for every error in a page after the
// first.
func (r *rows) fail(err error) error {
	r.done = true
	err = r.cause(err)
	if r.read || r.page > 0 {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// expect reads one token, and returns an error if it is not the delimiter kind.
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
