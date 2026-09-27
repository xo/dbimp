package dbimptest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// Contract is the part of the contract of D8 and D18 to D21 that every
// driver keeps in the same way. RunContract tests it against fake servers,
// which answer with bodies in the form of the product. The driver supplies
// the bodies, because each product has its own form.
type Contract struct {
	// Open returns a database for the fake server at url, which is an
	// http:// URL. The driver builds its own DSN from url.
	Open func(t *testing.T, url string) *sql.DB
	// Query is any statement. A fake server ignores it, and answers each case
	// with the body of that case.
	Query string
	// Columns is a result with at least two columns.
	Columns ColumnsCase
	// Null is a result whose first row holds a NULL.
	Null NullCase
	// ErrorAfterRows is a result that holds some rows and then an error.
	ErrorAfterRows ErrorCase
	// Stream is the form of a result with many rows.
	Stream StreamCase
	// Transactions is true if the driver supports transactions. If it is
	// false, BeginTx must return dbimp.ErrNotSupported (D20).
	Transactions bool
	// ContentType is the content type of the bodies of the cases, such as
	// "application/cbor". It is "application/json" when it is empty.
	ContentType string
}

// contentType returns the content type of the bodies of c.
func (c Contract) contentType() string {
	if c.ContentType == "" {
		return "application/json"
	}
	return c.ContentType
}

// ColumnsCase is a result and the names of its columns, in order.
type ColumnsCase struct {
	Body string
	Want []string
}

// NullCase is a result whose first row holds a NULL in column Column.
type NullCase struct {
	Body   string
	Column int
}

// ErrorCase is a result that holds Rows rows and then an error.
type ErrorCase struct {
	Body string
	Rows int
}

// StreamCase is the form of a result with many rows of one column: Head,
// then Row repeated with Sep between each two, then Tail.
type StreamCase struct {
	Head string
	Row  string
	Sep  string
	Tail string
}

// streamBytes is the size of the result that the stream cases send.
const streamBytes = 64 << 20

// RunContract tests the driver against c. Each subtest checks that it leaves
// no goroutine behind, so the test that calls RunContract must not run in
// parallel with another test.
func RunContract(t *testing.T, c Contract) {
	t.Helper()
	t.Run("columns keep their order", func(t *testing.T) {
		db := open(t, c, serve(c.Columns.Body, c.contentType()))
		rows := query(t, db, c.Query)
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("reading the columns: %v", err)
		}
		if !slices.Equal(cols, c.Columns.Want) {
			t.Errorf("columns are %q, want %q, in that order (D18)", cols, c.Columns.Want)
		}
	})
	t.Run("a NULL is nil", func(t *testing.T) {
		db := open(t, c, serve(c.Null.Body, c.contentType()))
		checkNull(t, db, c)
	})
	t.Run("an error after rows reaches the caller", func(t *testing.T) {
		db := open(t, c, serve(c.ErrorAfterRows.Body, c.contentType()))
		rows := query(t, db, c.Query)
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err == nil {
			t.Errorf("read %d rows and no error, want the error that follows %d rows (D21)", n, c.ErrorAfterRows.Rows)
		}
		if n != c.ErrorAfterRows.Rows {
			t.Errorf("read %d rows before the error, want %d", n, c.ErrorAfterRows.Rows)
		}
	})
	t.Run("close before the end reads nothing more", func(t *testing.T) {
		h := newStreamHandler(c.Stream, c.contentType(), nil)
		db := open(t, c, h)
		rows := query(t, db, c.Query)
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("closing the rows: %v", err)
		}
		select {
		case <-h.done:
		case <-time.After(30 * time.Second):
			t.Fatal("the fake server still sends the result 30 seconds after the rows closed")
		}
		if written := h.written.Load(); written > h.total/2 {
			t.Errorf("the server sent %d of %d bytes after the rows closed, so Close read the rest (D36)", written, h.total)
		}
	})
	t.Run("a cancelled context stops the read", func(t *testing.T) {
		wait := make(chan struct{})
		h := newStreamHandler(c.Stream, c.contentType(), wait)
		db := open(t, c, h)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		rows, err := db.QueryContext(ctx, c.Query)
		if err != nil {
			t.Fatalf("sending the query: %v", err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		cancel()
		close(wait)
		for rows.Next() {
		}
		if err := rows.Err(); !errors.Is(err, context.Canceled) {
			t.Errorf("the error is %v, want context.Canceled (D36)", err)
		}
	})
	t.Run("a request is sent once", func(t *testing.T) {
		var hits atomic.Int32
		db := open(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("the fake server cannot hijack the connection")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijacking the connection: %v", err)
				return
			}
			_ = conn.Close()
		}))
		rows, err := db.QueryContext(t.Context(), c.Query)
		if err == nil {
			rows.Close()
			t.Fatal("a query whose connection closed returned no error")
		}
		if n := hits.Load(); n != 1 {
			t.Errorf("the server received the request %d times, want 1 (D8)", n)
		}
	})
	t.Run("an unreachable server is a bad connection", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		CheckGoroutines(t)
		db := c.Open(t, url)
		t.Cleanup(func() { db.Close() })
		rows, err := db.QueryContext(t.Context(), c.Query)
		if err == nil {
			rows.Close()
			t.Fatal("a query to a closed server returned no error")
		}
		if !errors.Is(err, driver.ErrBadConn) {
			t.Errorf("the error is %v, want driver.ErrBadConn, because the request never reached the server (D8)", err)
		}
	})
	t.Run("a transaction is never faked", func(t *testing.T) {
		if c.Transactions {
			t.Skip("the driver supports transactions")
		}
		db := open(t, c, serve(c.Columns.Body, c.contentType()))
		tx, err := db.BeginTx(t.Context(), nil)
		if err == nil {
			_ = tx.Rollback()
			t.Fatal("BeginTx returned a transaction for a driver that has none")
		}
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("the error is %v, want dbimp.ErrNotSupported (D20)", err)
		}
	})
}

// open starts a fake server with h and opens the driver against it. It
// checks the goroutines first, so the check runs after the database and the
// server close.
func open(t *testing.T, c Contract, h http.Handler) *sql.DB {
	t.Helper()
	CheckGoroutines(t)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	db := c.Open(t, srv.URL)
	t.Cleanup(func() { db.Close() })
	return db
}

func query(t *testing.T, db *sql.DB, q string) *sql.Rows {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err != nil {
		t.Fatalf("sending the query: %v", err)
	}
	t.Cleanup(func() { rows.Close() })
	return rows
}

func checkNull(t *testing.T, db *sql.DB, c Contract) {
	t.Helper()
	for _, dest := range []struct {
		name string
		new  func() any
		ok   func(v any) bool
		err  bool
	}{
		{"*any", func() any { return new(any) }, func(v any) bool {
			p, ok := v.(*any)
			return ok && *p == nil
		}, false},
		{"*sql.Null[string]", func() any { return new(sql.Null[string]) }, func(v any) bool {
			p, ok := v.(*sql.Null[string])
			return ok && !p.Valid
		}, false},
		{"*string", func() any { return new(string) }, nil, true},
	} {
		rows := query(t, db, c.Query)
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("reading the columns: %v", err)
		}
		dests := make([]any, len(cols))
		for i := range dests {
			dests[i] = new(any)
		}
		v := dest.new()
		dests[c.Null.Column] = v
		err = rows.Scan(dests...)
		switch {
		case dest.err && err == nil:
			t.Errorf("scanning a NULL into %s returned no error, want the error of database/sql (D8)", dest.name)
		case !dest.err && err != nil:
			t.Errorf("scanning a NULL into %s: %v", dest.name, err)
		case !dest.err && !dest.ok(v):
			t.Errorf("scanning a NULL into %s gave %v, want NULL (D8)", dest.name, v)
		}
		rows.Close()
	}
}

// serve returns a handler that answers every request with body, of the
// content type contentType.
func serve(body, contentType string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	})
}

// streamHandler sends a result of about 64 MiB, and counts what it wrote.
type streamHandler struct {
	c       StreamCase
	ctype   string
	wait    chan struct{}
	n       int
	total   int64
	written atomic.Int64
	done    chan struct{}
}

// newStreamHandler returns a streamHandler. If wait is not nil, the handler
// sends the first row, then waits until wait closes or the request ends.
func newStreamHandler(c StreamCase, contentType string, wait chan struct{}) *streamHandler {
	n := streamBytes / (len(c.Row) + len(c.Sep))
	return &streamHandler{
		c:     c,
		ctype: contentType,
		wait:  wait,
		n:     n,
		total: int64(len(c.Head) + n*len(c.Row) + (n-1)*len(c.Sep) + len(c.Tail)),
		done:  make(chan struct{}),
	}
}

func (h *streamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer close(h.done)
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", h.ctype)
	flusher, _ := w.(http.Flusher)
	write := func(s string) bool {
		n, err := io.WriteString(w, s)
		h.written.Add(int64(n))
		return err == nil
	}
	if !write(h.c.Head) {
		return
	}
	for i := range h.n {
		if i > 0 && !write(h.c.Sep) {
			return
		}
		if !write(h.c.Row) {
			return
		}
		if i == 0 && flusher != nil {
			flusher.Flush()
		}
		if i == 0 && h.wait != nil {
			select {
			case <-h.wait:
			case <-r.Context().Done():
				return
			}
		}
	}
	write(h.c.Tail)
}
