package cosmos

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

// valueColumn is the name of the one column of a result whose rows are not
// objects, such as the rows of SELECT VALUE. The server names an expression
// with no alias $1 (recorded: "an expression with no alias"), and D18 rule 3
// gives such a result one column with the name that the server gives.
const valueColumn = "$1"

// rows reads the answer of one query, one document at a time (D25). A page is
// a JSON object with the array Documents, and the header X-Ms-Continuation
// holds the token of the next page (recorded: "the first page with the
// default size"). The rows follow the token to the last page, and read a page
// only when Next asks for a document that the page before it did not hold, so
// that the driver holds no more than one document.
//
// The columns are the keys of the first document (D18 and D190). If the
// first document is not an object, the result has the one column $1.
type rows struct {
	c *Connector
	// ctx is the context of QueryContext. Each page after the first is a
	// request of its own, and database/sql gives Next no context.
	ctx context.Context //nolint:containedctx // D190 keeps the context of QueryContext for the request of each page after the first, as D45 does for other drivers.
	q   *query

	s   *dbimp.Stream
	dec *jsontext.Decoder
	// inDocs is true while the decoder is inside the array Documents.
	inDocs bool
	// token is the header X-Ms-Continuation of the current page, and "" when
	// it has none.
	token string

	// cols are the columns, and scalar is true for the one column $1. objs
	// reads the objects of the current page when the rows are objects.
	cols   []string
	scalar bool
	objs   *dbimp.ObjectRows
	vals   []jsontext.Value

	// cur is the current row.
	cur []driver.Value
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
)

// newRows returns the rows of a query, whose first page is res. It reads the
// start of the page up to the first document, so that an error that comes
// before any row does not wrap dbimp.ErrIncomplete (D107), and so that the
// columns are known. A page with no document and a token is skipped, because
// the columns come from the first document.
func newRows(ctx context.Context, c *Connector, q *query, res *http.Response) (*rows, error) {
	r := &rows{c: c, ctx: ctx, q: q}
	if err := r.startPage(res); err != nil {
		_ = r.s.Close()
		return nil, err
	}
	for {
		if r.inDocs {
			kind := r.dec.PeekKind()
			if kind == 0 {
				// The body ends or breaks before the first document.
				_, err := r.dec.ReadToken()
				_ = r.s.Close()
				return nil, fmt.Errorf("reading the first document: %w", err)
			}
			if kind != ']' {
				break
			}
			if err := r.endDocs(); err != nil {
				_ = r.s.Close()
				return nil, err
			}
		}
		if r.token == "" {
			// The result holds no document, so it has no column.
			r.cols = []string{}
			r.done = true
			return r, nil
		}
		if err := r.nextPage(); err != nil {
			return nil, err
		}
	}
	if err := r.chooseColumns(); err != nil {
		_ = r.s.Close()
		return nil, err
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// ColumnTypeDatabaseTypeName satisfies driver.RowsColumnTypeDatabaseTypeName.
// A column has no type in Cosmos DB, because each document holds its own
// values (recorded), so the name is empty.
func (r *rows) ColumnTypeDatabaseTypeName(int) string {
	return ""
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType. A column has no
// type, and the value of a key can change its type from one document to the
// next, so the scan type is any (D190).
func (r *rows) ColumnTypeScanType(int) reflect.Type {
	return reflect.TypeFor[any]()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. A document can
// lack any key, and a missing key is nil (D190).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// Close satisfies driver.Rows. It closes the body, and reads nothing more
// (D36).
func (r *rows) Close() error {
	return r.s.Close()
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next document,
// which ScanColumn assigns.
func (r *rows) NextRow() error {
	if r.done {
		return io.EOF
	}
	for {
		if r.inDocs {
			ok, err := r.readDoc()
			if err != nil {
				return r.fail(err)
			}
			if ok {
				r.read = true
				return nil
			}
		}
		if r.token == "" {
			r.done = true
			return io.EOF
		}
		if err := r.nextPage(); err != nil {
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

// chooseColumns reads the kind of the first document, and sets the columns.
// An object gives its keys, and anything else gives the column $1.
func (r *rows) chooseColumns() error {
	if r.dec.PeekKind() != '{' {
		r.scalar, r.cols = true, []string{valueColumn}
		r.cur = make([]driver.Value, 1)
		return nil
	}
	objs, err := dbimp.ContinueObjectRows(r.dec, nil)
	if err != nil {
		return fmt.Errorf("reading the first document: %w", err)
	}
	r.objs, r.cols = objs, objs.Columns()
	r.vals = make([]jsontext.Value, len(r.cols))
	r.cur = make([]driver.Value, len(r.cols))
	return nil
}

// readDoc reads the next document of the current page into the current row.
// It reports false at the end of the page, after it read the rest of the
// page. A key that the document lacks is nil, because a missing key, an
// undefined value and null are one value (D190). A key that only a later
// document has is an error, and never a new column (D18).
func (r *rows) readDoc() (bool, error) {
	if r.scalar {
		if r.dec.PeekKind() == ']' {
			return false, r.endDocs()
		}
		v, err := r.dec.ReadValue()
		if err != nil {
			return false, fmt.Errorf("reading a document: %w", err)
		}
		r.cur[0], err = value(v)
		return err == nil, err
	}
	err := r.objs.Next(r.vals)
	switch {
	case errors.Is(err, io.EOF):
		r.inDocs = false
		return false, r.endPage()
	case err != nil:
		return false, err
	}
	for i, v := range r.vals {
		var err error
		if r.cur[i], err = value(v); err != nil {
			return false, fmt.Errorf("reading the key %q: %w", r.cols[i], err)
		}
	}
	return true, nil
}

// value returns the Go value of one JSON value of a document: nil for a
// missing key and for null, and else the value that dbimp.Any returns (D190).
func value(v jsontext.Value) (any, error) {
	if dbimp.IsNull(v) {
		return nil, nil
	}
	return dbimp.Any(v)
}

// startPage reads the start of the page res, up to the first document: the
// object, and each member before Documents. A page with no Documents is read
// to its end.
func (r *rows) startPage(res *http.Response) error {
	r.s = dbimp.NewStream(res.Body)
	r.dec = r.s.Decoder()
	r.inDocs, r.token = false, res.Header.Get("X-Ms-Continuation")
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
		name := tok.String()
		if name != "Documents" {
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading %s: %w", name, err)
			}
			continue
		}
		if r.dec.PeekKind() == 'n' {
			if err := r.dec.SkipValue(); err != nil {
				return fmt.Errorf("reading Documents: %w", err)
			}
			continue
		}
		if err := expect(r.dec, '['); err != nil {
			return fmt.Errorf("reading Documents: %w", err)
		}
		r.inDocs = true
		return nil
	}
	return r.endPage()
}

// endDocs reads the end of the array Documents, if the decoder is in it, and
// the rest of the page.
func (r *rows) endDocs() error {
	if r.inDocs {
		if _, err := r.dec.ReadToken(); err != nil {
			return fmt.Errorf("reading the end of Documents: %w", err)
		}
		r.inDocs = false
	}
	return r.endPage()
}

// endPage reads the members of a page after Documents, such as _count, the
// end of its object and the end of its body, and closes the body, so that the
// connection goes back to the pool (D36). A page with no Documents is read to
// its end by startPage.
func (r *rows) endPage() error {
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
		}
		if tok.Kind() != '"' {
			return fmt.Errorf("reading the end of the answer: %v where a name was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
		}
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the end of the answer: %w", err)
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

// nextPage sends the query again with the token of the page that the rows
// read to its end, and reads the start of the answer. The first page that
// holds a document sets nothing new, because the columns are known.
func (r *rows) nextPage() error {
	if err := r.ctx.Err(); err != nil {
		return fmt.Errorf("sending the request for the next page: %w", err)
	}
	res, err := r.c.docs(r.ctx, r.q, r.token)
	if err != nil {
		return fmt.Errorf("reading the next page: %w", err)
	}
	if err := r.startPage(res); err != nil {
		_ = r.s.Close()
		return err
	}
	if r.inDocs && r.cols != nil && !r.scalar {
		// The objects of a page after the first have the columns of the first.
		if r.objs, err = dbimp.ContinueObjectRows(r.dec, r.cols); err != nil {
			return fmt.Errorf("reading the next page: %w", err)
		}
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
