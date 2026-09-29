package arangodb

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
	"net/url"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the requests that stop a query on the server after its
// context ended, as D67 bounds them for Neo4j (D90).
const stopTimeout = 5 * time.Second

// finishLimit is the most of the first batch that Close reads to learn the
// id of its cursor (D90). The server sent the whole batch, with its Content-Length, so
// this reads no more of the result than the one batch.
const finishLimit = 1 << 20

// The shapes of a result (D89). A result with no rows has no columns, and no
// shape.
const (
	// shapeValue is one column that holds each value: a document, a scalar,
	// an array, or values of mixed shapes.
	shapeValue = iota + 1
	// shapeObject is an object whose keys are the columns.
	shapeObject
)

// cursorBody is the body of POST /_api/cursor (measured).
type cursorBody struct {
	Query     string                    `json:"query"`
	BindVars  map[string]jsontext.Value `json:"bindVars,omitzero"`
	BatchSize int                       `json:"batchSize"`
	Options   cursorOptions             `json:"options"`
}

// cursorOptions are the options of a cursor. The driver always streams
// (D90).
type cursorOptions struct {
	Stream bool `json:"stream"`
}

// rows reads a cursor one batch at a time, and each batch one token at a
// time (D25 and D90).
type rows struct {
	c *conn
	// ctx is the context of QueryContext, which each fetch of a batch uses
	// (D90). database/sql closes the rows when it ends.
	ctx context.Context //nolint:containedctx // D90 keeps the context of QueryContext for the fetch of each batch.
	trx string
	// tag names the query in its comment, or is "" for cancel=none (D90 and
	// D99).
	tag string

	s   *dbimp.Stream
	dec *jsontext.Decoder
	// length is the Content-Length of the batch, or -1.
	length   int64
	inResult bool
	id       string
	more     bool
	ended    bool

	shape   int
	cols    []string
	index   map[string]int
	first   []any
	pending bool
	cur     []any
	// writes is writesExecuted of the query, from its last batch.
	writes int64
}

// ensure the interfaces.
var _ driver.RowsColumnScanner = (*rows)(nil)

// cursor sends a query, and reads the result up to its first row, which
// names the columns (D89).
func (c *conn) cursor(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	vars, err := bindVars(query, args)
	if err != nil {
		return nil, err
	}
	b := cursorBody{Query: query, BindVars: vars, BatchSize: c.c.cfg.Batch, Options: cursorOptions{Stream: true}}
	var tag string
	if c.c.cfg.Cancel == CancelTag {
		tag = c.tag()
		b.Query = tagged(query, tag)
	}
	w := c.watch(ctx, tag)
	res, err := c.c.do(ctx, http.MethodPost, "cursor", b, c.trx())
	if _, serr := w.end(); err != nil {
		return nil, errors.Join(err, serr)
	}
	r := &rows{c: c, ctx: ctx, trx: c.trx(), tag: tag}
	if err := r.start(res); err != nil {
		return nil, errors.Join(err, r.close(ctx))
	}
	return r, nil
}

// Columns satisfies driver.Rows.
func (r *rows) Columns() []string {
	return r.cols
}

// Close satisfies driver.Rows. If at most 1 MiB of the first batch is left,
// it reads the rest to learn the id of the cursor. Otherwise it closes the
// body and reads nothing more (D36). A cursor that has more batches is then
// deleted, or its query killed by its tag, which stops the query (D90).
func (r *rows) Close() error {
	return r.close(r.ctx)
}

// NextRow satisfies driver.RowsColumnScanner.
func (r *rows) NextRow() error {
	if r.pending {
		r.cur, r.pending = r.first, false
		return nil
	}
	v, ok, err := r.next()
	switch {
	case err != nil:
		return err
	case !ok:
		return io.EOF
	}
	r.cur, err = r.values(v)
	return err
}

// Next satisfies driver.Rows, for a caller that does not use
// RowsColumnScanner.
func (r *rows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	for i := range dest {
		dest[i] = r.cur[i]
	}
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *rows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.cur[i])
}

// start reads the first batch up to its first value, which gives the shape
// and the columns (D89).
func (r *rows) start(res *http.Response) error {
	if err := r.open(res); err != nil {
		return err
	}
	v, ok, err := r.next()
	switch {
	case err != nil:
		return err
	case !ok:
		r.cols = []string{}
		return nil
	}
	if err := r.learnShape(v); err != nil {
		return err
	}
	if r.first, err = r.values(v); err != nil {
		return err
	}
	r.pending = true
	return nil
}

// close closes the body, and stops a cursor that has more batches, with ctx
// without its end, because ctx can have ended. The id of the cursor and
// hasMore follow the result of a batch (measured), so in the middle of the
// first batch the driver does not know the id. If at most 1 MiB of that
// batch is left, it reads the rest to learn the id. Otherwise it kills the
// query by its tag, and the cursor holds its collections until its ttl ends
// (D90). With cancel=none, it sends nothing then. In a later batch, it knows
// the id from the batch before, and deletes the cursor.
func (r *rows) close(ctx context.Context) error {
	var err error
	if !r.ended && r.inResult && r.id == "" && r.length >= 0 && r.length-r.dec.InputOffset() <= finishLimit {
		// The rest of this batch is short, so the driver reads it to learn
		// the id of the cursor, and deletes the cursor.
		if ferr := r.skipBatch(); ferr != nil {
			err = ferr
		}
	}
	if r.s != nil {
		err = errors.Join(err, r.s.Close())
	}
	if r.ended {
		return err
	}
	r.ended = true
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	switch {
	case r.inResult && r.id == "" && r.tag != "":
		if kerr := r.c.c.kill(ctx, r.tag); kerr != nil {
			err = errors.Join(err, kerr)
		}
	case r.id != "" && (r.inResult || r.more):
		derr := r.c.c.call(ctx, http.MethodDelete, "cursor/"+url.PathEscape(r.id), nil, nil, r.trx)
		if derr != nil && !is(derr, numCursorNotFound) {
			err = errors.Join(err, fmt.Errorf("deleting the cursor: %w", derr))
		}
	}
	return err
}

// learnShape sets the shape and the columns from the first value (D89).
func (r *rows) learnShape(v jsontext.Value) error {
	if v.Kind() != '{' {
		r.shape, r.cols = shapeValue, []string{""}
		return nil
	}
	keys, err := objectKeys(v)
	if err != nil {
		return err
	}
	if stored(keys) {
		r.shape, r.cols = shapeValue, []string{""}
		return nil
	}
	r.shape, r.cols = shapeObject, keys
	r.index = make(map[string]int, len(keys))
	for i, k := range keys {
		r.index[k] = i
	}
	return nil
}

// stored reports whether keys are the keys of a stored document, which
// starts with _key, _id and _rev, or of a stored edge, which starts with
// _key, _id, _from, _to and _rev (measured on 3.12.12, D89).
func stored(keys []string) bool {
	switch {
	case len(keys) < 3 || keys[0] != "_key" || keys[1] != "_id":
		return false
	case keys[2] == "_rev":
		return true
	}
	return len(keys) >= 5 && keys[2] == "_from" && keys[3] == "_to" && keys[4] == "_rev"
}

// values returns the values of the columns of one row.
func (r *rows) values(v jsontext.Value) ([]any, error) {
	if r.shape == shapeValue {
		d, err := decode(v)
		if err != nil {
			return nil, fmt.Errorf("reading a row: %w", err)
		}
		return []any{d}, nil
	}
	if v.Kind() != '{' {
		return nil, fmt.Errorf("reading a row: %v in a result of objects: %w", v.Kind(), dbimp.ErrColumnCount)
	}
	out := make([]any, len(r.cols))
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading a row: %w", err)
		}
		name := tok.String()
		i, ok := r.index[name]
		if !ok {
			return nil, fmt.Errorf("reading a row: %q: %w", name, dbimp.ErrExtraColumn)
		}
		if out[i], err = decodeNext(dec); err != nil {
			return nil, fmt.Errorf("reading a row: %q: %w", name, err)
		}
	}
	return out, nil
}

// skipBatch reads the rest of the result of a batch, and the members that
// follow it.
func (r *rows) skipBatch() error {
	for r.dec.PeekKind() != ']' {
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the rest of a batch: %w", err)
		}
	}
	return r.endResult()
}

// open starts to read a batch, up to the first value of its result.
func (r *rows) open(res *http.Response) error {
	r.length = res.ContentLength
	r.s = dbimp.NewStream(res.Body)
	r.dec = r.s.Decoder()
	if err := expect(r.dec, '{'); err != nil {
		return fmt.Errorf("reading a batch: %w", err)
	}
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading a batch: %w", err)
		}
		if name := tok.String(); name == "result" {
			if err := expect(r.dec, '['); err != nil {
				return fmt.Errorf("reading the result: %w", err)
			}
			r.inResult = true
			return nil
		} else if err := r.member(name); err != nil {
			return err
		}
	}
	return r.closeBatch()
}

// member reads one member of a batch that is not its result.
func (r *rows) member(name string) error {
	v, err := r.dec.ReadValue()
	if err != nil {
		return fmt.Errorf("reading %q of a batch: %w", name, err)
	}
	switch name {
	case "hasMore":
		r.more = v.Kind() == 't'
	case "id":
		if r.id, err = dbimp.String(v); err != nil {
			return fmt.Errorf("reading the id of the cursor: %w", err)
		}
	case "extra":
		var extra struct {
			Stats struct {
				Writes int64 `json:"writesExecuted"`
			} `json:"stats"`
		}
		if err := json.Unmarshal(v, &extra); err != nil {
			return fmt.Errorf("reading the statistics of the query: %w", err)
		}
		r.writes = extra.Stats.Writes
	}
	return nil
}

// closeBatch reads the end of a batch, and of its body.
func (r *rows) closeBatch() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a batch: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	return r.s.Close()
}

// next returns the next value of the result, and fetches the next batch
// when a batch ends and hasMore is true (D90). It returns false at the end.
func (r *rows) next() (jsontext.Value, bool, error) {
	for {
		if r.inResult {
			if r.dec.PeekKind() != ']' {
				v, err := r.dec.ReadValue()
				if err != nil {
					return nil, false, fmt.Errorf("reading a row: %w", err)
				}
				return v.Clone(), true, nil
			}
			if err := r.endResult(); err != nil {
				return nil, false, err
			}
		}
		if !r.more || r.ended {
			r.ended = true
			return nil, false, nil
		}
		res, err := r.c.c.do(r.ctx, http.MethodPost, "cursor/"+url.PathEscape(r.id), nil, r.trx)
		if err != nil {
			// A failed batch ends the cursor on the server (measured).
			r.ended = true
			return nil, false, fmt.Errorf("reading a batch: %w", err)
		}
		if err := r.open(res); err != nil {
			return nil, false, err
		}
	}
}

// endResult reads the end of the result of a batch, and the members that
// follow it, such as hasMore and id.
func (r *rows) endResult() error {
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the result: %w", err)
	}
	r.inResult = false
	for r.dec.PeekKind() != '}' {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading a batch: %w", err)
		}
		if err := r.member(tok.String()); err != nil {
			return err
		}
	}
	return r.closeBatch()
}

// objectKeys returns the keys of the object v, in order.
func objectKeys(v jsontext.Value) ([]string, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(v))
	if _, err := dec.ReadToken(); err != nil {
		return nil, err
	}
	keys := []string{}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading a row: %w", err)
		}
		keys = append(keys, tok.String())
		if err := dec.SkipValue(); err != nil {
			return nil, fmt.Errorf("reading a row: %w", err)
		}
	}
	return keys, nil
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
