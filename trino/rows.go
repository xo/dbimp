package trino

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// maxIdleBackoff is the longest pause between two polls of pages that hold no
// rows, such as a query that waits in a queue.
const maxIdleBackoff = 100 * time.Millisecond

// rows reads the answer of one statement, which is a series of pages (D21 and
// D175). Each page is a JSON object that the server sends for one request.
// The first comes from the POST, and the others from GET to the nextUri of the
// page before. A page holds the members nextUri, columns, data, stats, error,
// updateType and updateCount, and the driver reads each one token at a time,
// so a page of megabytes is never held in memory (D25).
//
// The rows keep the context of the query, because the polls for the next page
// run in Next, which has no context of its own, and the cancel of the query
// runs after the context ended. This is an exception to hard rule 4 of
// AGENTS.md, as for the rows of Databend (D175).
type rows struct {
	cn  *conn
	ctx context.Context //nolint:containedctx // D175 keeps the context of QueryContext for the poll of each page and for the cancel.

	// stream and dec read the page that the rows are in, and are nil between
	// two pages.
	stream *dbimp.Stream
	dec    *jsontext.Decoder

	cols []column
	cur  []driver.Value

	// next is the nextUri of the page that the rows are in, or "" for the last
	// page. The cancel reads it from another goroutine, so mu guards it.
	mu   sync.Mutex
	next string

	// haveCols is true once a page named the columns, even none. inPage is true
	// between the start and the end of the object of a page, and inData between
	// the brackets of its member data.
	haveCols bool
	inPage   bool
	inData   bool
	// pageErr is the error member of the page that the rows are in.
	pageErr *Error
	// idle counts the pages in a row that held no rows.
	idle int
	// sawRow is true if the page that the rows are in has a row.
	sawRow bool
	// read is true once a row reached the caller, so that an error after it
	// wraps dbimp.ErrIncomplete (D107), and done once the answer is read to its
	// end.
	read bool
	done bool

	count    int64
	hasCount bool

	// stop ends the watch of the context, which sends the cancel when it ends
	// before the answer is read. finished closes when that cancel is done.
	stop      func() bool
	finished  chan struct{}
	closeOnce sync.Once
	closeErr  error
	cancelled sync.Once
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

// newRows returns the rows of the answer to the POST, whose body is body. It
// starts the watch of ctx. The caller calls open.
func newRows(ctx context.Context, cn *conn, body io.ReadCloser) *rows {
	r := &rows{cn: cn, ctx: ctx, finished: make(chan struct{})}
	r.stream = dbimp.NewStream(body)
	r.dec = r.stream.Decoder()
	r.stop = context.AfterFunc(ctx, func() {
		defer close(r.finished)
		r.cancel()
	})
	return r
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
// It is the name of the type in upper case, such as BIGINT or TIMESTAMP WITH
// TIME ZONE.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	return r.cols[i].sig.databaseType()
}

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType (D135 and D175).
func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	return r.cols[i].sig.scanType()
}

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The columns do
// not say whether they can be NULL, and every type can be (D175).
func (r *rows) ColumnTypeNullable(int) (bool, bool) {
	return true, true
}

// ColumnTypeLength satisfies driver.RowsColumnTypeLength. It is the length of
// a char or a varchar, and of the largest value for a varchar with no length
// and a varbinary.
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	sig := r.cols[i].sig
	switch sig.raw {
	case typeChar, typeVarchar:
		if len(sig.nums) > 0 && sig.nums[0] < 1<<31-1 {
			return sig.nums[0], true
		}
		return 1<<63 - 1, true
	case typeVarbinary:
		return 1<<63 - 1, true
	}
	return 0, false
}

// ColumnTypePrecisionScale satisfies driver.RowsColumnTypePrecisionScale. It is
// the precision and the scale of a decimal.
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	sig := r.cols[i].sig
	if sig.raw == typeDecimal && len(sig.nums) == 2 {
		return sig.nums[0], sig.nums[1], true
	}
	return 0, 0, false
}

// Close satisfies driver.Rows. It closes the body of the page, and reads
// nothing more (D36). The server runs a query on when its client leaves, so
// rows that the caller closes before the end send DELETE to the nextUri, and
// so do rows whose context ended and whose cancel did not run (D175).
func (r *rows) Close() error {
	r.closeOnce.Do(func() {
		if r.stream != nil {
			r.closeErr = r.stream.Close()
			r.stream = nil
		}
		if !r.stop() {
			// The context ended, and its function runs or ran the cancel.
			<-r.finished
			return
		}
		if r.cn.st.tx != "" && r.ctx.Err() == nil && r.drain() {
			return
		}
		r.cancel()
	})
	return r.closeErr
}

// NextRow satisfies driver.RowsColumnScanner. It decodes the next row, which
// ScanColumn assigns. After the last row, it reads the rest of the answer, and
// returns the error that the last page holds (D21 and D36).
func (r *rows) NextRow() error {
	for {
		if r.done {
			return io.EOF
		}
		ok, err := r.step()
		if err != nil {
			return r.fail(err)
		}
		if ok {
			r.read = true
			return nil
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

// open reads the answer up to the first row, or to its end if it has none. The
// servers send the columns while the query still runs, and an error that the
// query finds comes in a later page, so QueryContext waits for the first row to
// return the error that comes before any row (D175). Such an error does not wrap
// dbimp.ErrIncomplete (D107). A statement that has no columns, such as DDL, gives
// none, and is read to its end.
func (r *rows) open() error {
	if err := r.beginPage(); err != nil {
		return r.fail(err)
	}
	for !r.ready() {
		if _, err := r.step(); err != nil {
			return r.fail(err)
		}
	}
	return nil
}

// ready reports whether the rows are at a row, or at their end.
func (r *rows) ready() bool {
	return r.done || r.haveCols && r.inData && r.dec.PeekKind() != ']'
}

// drain reads the pages that follow, for rows that the caller closes in a
// transaction. A cancel aborts the transaction (measured), and a query whose last
// row the caller read, as QueryRow does, only waits for the client to take its last
// page, which is empty. drain keeps nothing of the pages. It returns true when a
// page had no nextUri, so the query ended and needs no cancel. When a page is
// larger than drainBytes, or the query has more than drainPages pages, it returns
// false, and the caller cancels the query, which aborts the transaction.
func (r *rows) drain() bool {
	for range drainPages {
		next := r.getNext()
		if next == "" {
			return true
		}
		after, err := r.skipPage(next)
		if err != nil {
			return false
		}
		r.setNext(after)
	}
	return r.getNext() == ""
}

// skipPage reads the page at next, and returns its nextUri, or "" for the last
// page.
func (r *rows) skipPage(next string) (string, error) {
	res, err := r.cn.poll(r.ctx, next)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	dec := jsontext.NewDecoder(io.LimitReader(res.Body, drainBytes))
	if err := expectKind(dec, '{'); err != nil {
		return "", fmt.Errorf("reading a page: %w", err)
	}
	after := ""
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return "", fmt.Errorf("reading a page: %w", err)
		}
		if tok.String() != "nextUri" {
			if err := dec.SkipValue(); err != nil {
				return "", fmt.Errorf("reading a page: %w", err)
			}
			continue
		}
		v, err := dec.ReadValue()
		if err != nil {
			return "", fmt.Errorf("reading the nextUri: %w", err)
		}
		if !dbimp.IsNull(v) {
			if after, err = dbimp.String(v); err != nil {
				return "", fmt.Errorf("reading the nextUri: %w", err)
			}
		}
	}
	return after, nil
}

// cancel sends DELETE to the nextUri of the page, once, if the query has a
// next page, because the query still runs on the server. The answer holds
// nothing that the caller needs, and the read of the query fails with the
// error of its context.
func (r *rows) cancel() {
	r.cancelled.Do(func() {
		r.mu.Lock()
		next := r.next
		r.mu.Unlock()
		if next != "" {
			_ = r.cn.stop(r.ctx, next)
		}
	})
}

// setNext sets the nextUri of the page.
func (r *rows) setNext(next string) {
	r.mu.Lock()
	r.next = next
	r.mu.Unlock()
}

// getNext returns the nextUri of the page.
func (r *rows) getNext() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next
}

// result returns the result of the statement, which is read to its end.
func (r *rows) result() result {
	return result{count: r.count, known: r.hasCount}
}

// fail ends the rows with err. It wraps dbimp.ErrIncomplete when a row reached
// the caller before err (D107).
func (r *rows) fail(err error) error {
	r.done = true
	// When the context ends, the transport can close the connection before the
	// read sees the end, and the read then fails with "use of closed network
	// connection". The caller must see that its context ended (D36).
	if cerr := r.ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
		err = fmt.Errorf("%w: %w", cerr, err)
	}
	if r.read {
		return fmt.Errorf("reading the result: %w: %w", dbimp.ErrIncomplete, err)
	}
	return err
}

// step does one thing: it reads one row, one member of a page, the end of a
// page, or the start of the next page. It reports whether a row is ready.
func (r *rows) step() (bool, error) {
	switch {
	case r.inData:
		if r.dec.PeekKind() == ']' {
			r.inData = false
			if _, err := r.dec.ReadToken(); err != nil {
				return false, fmt.Errorf("reading the end of the data: %w", err)
			}
			return false, nil
		}
		if err := r.readRow(); err != nil {
			return false, err
		}
		r.sawRow = true
		return true, nil
	case r.inPage:
		return false, r.member()
	}
	return false, r.beginPage()
}

// beginPage starts a page. The first page is the body of the POST, which the
// rows hold already. The others come from GET to the nextUri.
func (r *rows) beginPage() error {
	if r.stream == nil {
		next := r.getNext()
		if next == "" {
			return fmt.Errorf("reading the next page: the page before it names none: %w", dbimp.ErrInvalidValue)
		}
		if err := r.pause(); err != nil {
			return err
		}
		res, err := r.cn.poll(r.ctx, next)
		if err != nil {
			return err
		}
		r.stream = dbimp.NewStream(res.Body)
		r.dec = r.stream.Decoder()
	}
	r.setNext("")
	r.sawRow = false
	if err := expectKind(r.dec, '{'); err != nil {
		return fmt.Errorf("reading a page: %w", err)
	}
	r.inPage = true
	return nil
}

// pause waits before a poll that follows pages with no rows, so that a query
// in a queue does not make the client send requests as fast as it can. The
// servers answer a poll of a running query only when they have something to
// say, so the pause starts after the third page in a row (measured).
func (r *rows) pause() error {
	if r.idle < 3 {
		return nil
	}
	d := min(time.Duration(r.idle-2)*10*time.Millisecond, maxIdleBackoff)
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-r.ctx.Done():
		return fmt.Errorf("waiting for the next page: %w", r.ctx.Err())
	}
}

// member reads one member of the page, or the end of its object.
func (r *rows) member() error {
	if r.dec.PeekKind() == '}' {
		return r.endPage()
	}
	tok, err := r.dec.ReadToken()
	if err != nil {
		return fmt.Errorf("reading a page: %w", err)
	}
	// The token is void after the next call, so the name is copied first.
	name := tok.String()
	switch name {
	case "nextUri":
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading the nextUri: %w", err)
		}
		if dbimp.IsNull(v) {
			return nil
		}
		next, err := dbimp.String(v)
		if err != nil {
			return fmt.Errorf("reading the nextUri: %w", err)
		}
		r.setNext(next)
	case "columns":
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading the columns: %w", err)
		}
		if r.haveCols || dbimp.IsNull(v) {
			return nil
		}
		cols, err := readColumns(v)
		if err != nil {
			return err
		}
		r.cols, r.haveCols = cols, true
		r.cur = make([]driver.Value, len(cols))
	case "data":
		if r.dec.PeekKind() == 'n' {
			return r.dec.SkipValue()
		}
		if !r.haveCols {
			return fmt.Errorf("reading the data: it comes before the columns: %w", dbimp.ErrInvalidValue)
		}
		if err := expectKind(r.dec, '['); err != nil {
			return fmt.Errorf("reading the data: %w", err)
		}
		r.inData = true
	case "error":
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading the error: %w", err)
		}
		var f failure
		if err := json.Unmarshal(v, &f); err != nil {
			return fmt.Errorf("reading the error: %w", err)
		}
		r.pageErr = newFailure(f)
	case "updateCount":
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading the count of the update: %w", err)
		}
		if dbimp.IsNull(v) {
			return nil
		}
		n, err := dbimp.Int64(v)
		if err != nil {
			return fmt.Errorf("reading the count of the update: %w", err)
		}
		r.count, r.hasCount = n, true
	default:
		if err := r.dec.SkipValue(); err != nil {
			return fmt.Errorf("reading the member %s: %w", name, err)
		}
	}
	return nil
}

// endPage reads the end of the object of a page, and the end of its body, so
// that the connection goes back to the pool. It returns the error that the page
// holds. A page with no nextUri is the last, and ends the rows.
func (r *rows) endPage() error {
	r.inPage = false
	if _, err := r.dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of a page: %w", err)
	}
	if err := r.stream.End(); err != nil {
		return err
	}
	err := r.stream.Close()
	r.stream = nil
	if err != nil {
		return fmt.Errorf("closing a page: %w", err)
	}
	if r.sawRow {
		r.idle = 0
	} else {
		r.idle++
	}
	switch {
	case r.pageErr != nil:
		// The query failed, and the server has no next page for it.
		return r.pageErr
	case r.getNext() == "":
		r.done = true
	}
	return nil
}

// readRow reads and decodes one row, which is a JSON array of values in the
// order of the columns.
func (r *rows) readRow() error {
	if err := expectKind(r.dec, '['); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	i := 0
	for ; r.dec.PeekKind() != ']'; i++ {
		v, err := r.dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		if i == len(r.cols) {
			return fmt.Errorf("reading a row: more than %d values: %w", len(r.cols), dbimp.ErrColumnCount)
		}
		// The value is decoded before the next call to the decoder, which
		// reuses its buffer.
		if r.cur[i], err = r.cols[i].sig.decode(v); err != nil {
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

// The drain of rows that close in a transaction reads at most drainPages pages,
// each of at most drainBytes bytes.
const (
	drainPages = 4
	drainBytes = 64 << 10
)
