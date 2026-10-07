package dynamodb

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

// rows reads the answer of one statement, one item at a time (D25). An answer
// is a JSON object with the array Items, and NextToken when more follows
// (recorded: "a statement"). A page is one request. The rows follow NextToken
// to the last page, and read it only when Next asks for an item that the
// page before it did not hold, so that the driver holds no more than one
// item (D169). A page can hold no item and still carry a token (recorded:
// "a limit with a filter").
type rows struct {
	c *Connector
	// ctx is the context of QueryContext. Each page after the first is a
	// request of its own, and database/sql gives Next no context.
	ctx    context.Context //nolint:containedctx // D169 keeps the context of QueryContext for the request of each page after the first.
	query  string
	params []any
	extra  map[string]any
	shape  shape
	index  map[string]int

	s   *dbimp.Stream
	dec *jsontext.Decoder
	// inItems is true while the decoder is inside the array Items.
	inItems bool
	// token is the NextToken of the current page, and "" when it has none.
	token string

	// cur is the current row.
	cur []driver.Value
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
)

// newRows returns the rows of a statement, whose first page is res. It reads
// the start of the page, and its end if the page holds no item, so that an
// error that comes before any row does not wrap dbimp.ErrIncomplete (D107).
func newRows(ctx context.Context, c *Connector, query string, params []any, extra map[string]any, res *http.Response) (*rows, error) {
	sh := shapeOf(query)
	r := &rows{c: c, ctx: ctx, query: query, params: params, extra: extra, shape: sh, index: map[string]int{}}
	for i, name := range sh.columns {
		r.index[name] = i
	}
	r.cur = make([]driver.Value, len(sh.columns))
	if err := r.startPage(res); err != nil {
		_ = r.s.Close()
		return nil, err
	}
	// A page with no item has no more to say, and the first answer to a
	// statement that has no item must reach the caller before the first row.
	if _, err := r.advance(); err != nil {
		_ = r.s.Close()
		return nil, err
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.shape.columns
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// A column has no type in DynamoDB, because each value names its own (D169),
// so the name is empty.
func (r *rows) ColumnTypeDatabaseTypeName(int) string {
	return ""
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType. A column has no
// type, and two items can hold two types in one attribute (recorded: "a type
// that a value can change"), so the scan type is any (D169).
func (r *rows) ColumnTypeScanType(int) reflect.Type {
	return reflect.TypeFor[any]()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. An item can lack
// any attribute, and a missing attribute is nil (D169).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36).
func (r *rows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next item, which
// ScanColumn assigns.
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	more, err := r.advance()
	if err != nil {
		return r.fail(err)
	}
	if !more {
		r.done = true
		return io.EOF
	}
	if err := r.readItem(); err != nil {
		return r.fail(err)
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

// startPage reads the start of the page res, up to the first item: the
// object, and each member before Items. A page with no Items is read to its
// end.
func (r *rows) startPage(res *http.Response) error {
	r.s = dbimp.NewStream(res.Body)
	r.dec = r.s.Decoder()
	r.inItems, r.token = false, ""
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading the answer: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the answer: %w", err)
		}
		if tok.Kind() != '"' {
			return fmt.Errorf("reading the answer: %v where a name was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
		}
		switch tok.String() {
		case "Items":
			if r.dec.PeekKind() == 'n' {
				if err := r.dec.SkipValue(); err != nil {
					return fmt.Errorf("reading the answer: %w", err)
				}
				continue
			}
			if err := expect(r.dec, '['); err != nil {
				return fmt.Errorf("reading Items: %w", err)
			}
			r.inItems = true
			return nil
		default:
			if err := r.member(tok.String()); err != nil {
				return err
			}
		}
	}
	return r.endPage()
}

// member reads the value of the member name of a page. It keeps NextToken,
// and skips every other member, such as ConsumedCapacity.
func (r *rows) member(name string) error {
	if name != "NextToken" {
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		return nil
	}
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading NextToken: %w", err)
	}
	if dbimp.IsNull(v) {
		return nil
	}
	if r.token, err = dbimp.String(v); err != nil {
		return fmt.Errorf("reading NextToken: %w", err)
	}
	return nil
}

// endPage reads the members of a page after Items, the end of its object and
// the end of its body, and closes the body, so that the connection goes back
// to the pool (D36).
func (r *rows) endPage() error {
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
		if tok.Kind() != '"' {
			return fmt.Errorf("reading the end of the answer: %v where a name was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
		}
		if err := r.member(tok.String()); err != nil {
			return err
		}
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the answer: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	return r.s.Close()
}

// advance moves to the start of the next item, across the end of a page and
// the request for the next one. It reports false at the end of the last page.
func (r *rows) advance() (bool, error) {
	for {
		if r.inItems {
			if r.dec.PeekKind() != ']' {
				return true, nil
			}
			if _, err := r.dec.ReadToken(); err != nil {
				return false, fmt.Errorf("reading the end of Items: %w", err)
			}
			r.inItems = false
			if err := r.endPage(); err != nil {
				return false, err
			}
		}
		// The page is read to its end, so its body is closed.
		if r.token == "" {
			return false, nil
		}
		if err := r.nextPage(); err != nil {
			return false, err
		}
	}
}

// nextPage sends the statement again with the NextToken of the page that the
// rows read to its end, and reads the start of the answer.
func (r *rows) nextPage() error {
	if err := r.ctx.Err(); err != nil {
		return fmt.Errorf("sending the request for the next page: %w", err)
	}
	body, err := statementBody(r.query, r.params, r.token, r.extra)
	if err != nil {
		return err
	}
	res, err := r.c.call(r.ctx, "ExecuteStatement", body)
	if err != nil {
		return fmt.Errorf("reading the next page: %w", err)
	}
	if err := r.startPage(res); err != nil {
		_ = r.s.Close()
		return err
	}
	return nil
}

// readItem reads one item into the current row. An attribute that the item
// lacks is nil, because a missing attribute and NULL are one value (D169).
// An attribute that is not a column is an error, and never a new column
// (D18).
func (r *rows) readItem() error {
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading an item: %w", err)
	}
	clear(r.cur)
	var whole map[string]any
	if r.shape.whole {
		whole = map[string]any{}
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading an item: %w", err)
		}
		if tok.Kind() != '"' {
			return fmt.Errorf("reading an item: %v where a name was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
		}
		name := tok.String()
		i, ok := r.index[name]
		if !ok && !r.shape.whole {
			return fmt.Errorf("reading an item: the attribute %q: %w", name, dbimp.ErrExtraColumn)
		}
		v, err := readAttribute(r.dec)
		if err != nil {
			return fmt.Errorf("reading the attribute %q: %w", name, err)
		}
		if r.shape.whole {
			whole[name] = v
			continue
		}
		r.cur[i] = v
	}
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of an item: %w", err)
	}
	if r.shape.whole {
		r.cur[0] = whole
	}
	return nil
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row
// reached the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before
	// the read sees the end, and the read then fails with "use of closed
	// network connection". The caller must see that its context ended (D36).
	if cerr := r.ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
		err = fmt.Errorf("%w: %w", cerr, err)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}
