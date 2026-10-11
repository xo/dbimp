package dbimptest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"slices"
	"testing"

	"github.com/xo/dbimp"
)

// WebSocketContract is the part of the contract of D8 and D18 to D21 for a
// driver whose product takes its requests as frames of a WebSocket. The driver
// supplies the steps of a script for each case, because each product has its own
// frames. A step answers the request frame of the same position, so the script
// lists the frames that a correct driver sends, in order, and a frame that the
// script does not hold fails the test.
type WebSocketContract struct {
	// Open returns a database for the server at addr, which is host:port of a
	// TLS server with a certificate that nothing trusts. The driver builds its
	// own DSN from addr, and turns the check of the certificate off.
	Open func(t *testing.T, addr string) *sql.DB
	// Query is any statement. The fake server ignores it, and answers each case
	// with the steps of that case.
	Query string
	// Login holds the steps of a login, which every case starts with.
	Login []WebSocketStep
	// Columns is a result with at least two columns.
	Columns WebSocketColumns
	// Null is a result whose first row holds a NULL.
	Null WebSocketNull
	// ErrorAfterRows is a result that holds some rows, and then an error when
	// the driver reads more.
	ErrorAfterRows WebSocketErrorCase
	// Close is a result of many rows, of which the first message holds some,
	// then the step that answers the request that frees the result. A driver
	// that reads more rows after Close fails the test.
	Close []WebSocketStep
	// Cancel is a result of many rows, of which the first message holds some,
	// then a step with Hold whose Reply is the answer of the server to a query
	// that the client aborted, then the step for the request that frees the
	// result. The test cancels the context while the server holds the request
	// that reads more rows.
	Cancel []WebSocketStep
	// Transactions is true if the driver supports transactions. If it is false,
	// BeginTx must return dbimp.ErrNotSupported (D20).
	Transactions bool
}

// WebSocketColumns is a result and the names of its columns, in order.
type WebSocketColumns struct {
	Steps []WebSocketStep
	Want  []string
}

// WebSocketNull is a result whose first row holds a NULL in column Column. The
// test runs the steps three times in a row, one for each query that it makes.
type WebSocketNull struct {
	Steps  []WebSocketStep
	Column int
}

// WebSocketErrorCase is a result that holds Rows rows and then an error.
type WebSocketErrorCase struct {
	Steps []WebSocketStep
	Rows  int
}

// runWebSocketContract runs the cases of c. Each case leaves no goroutine
// behind, so the test that calls RunContract must not run in parallel with
// another test.
func runWebSocketContract(t *testing.T, c *WebSocketContract) {
	t.Helper()
	t.Run("columns keep their order", func(t *testing.T) {
		db := c.open(t, c.Columns.Steps)
		rows := query(t, db, c.Query)
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("reading the columns: %v", err)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("reading the columns: %v", err)
		}
		if !slices.Equal(cols, c.Columns.Want) {
			t.Errorf("columns are %q, want %q, in that order (D18)", cols, c.Columns.Want)
		}
	})
	t.Run("a NULL is nil", func(t *testing.T) {
		// The check scans the NULL into three destinations, and each scan runs
		// the query again on the same connection.
		db := c.open(t, slices.Repeat(c.Null.Steps, 3))
		checkNull(t, db, Contract{Query: c.Query, Null: NullCase{Column: c.Null.Column}})
	})
	t.Run("an error after rows reaches the caller", func(t *testing.T) {
		db := c.open(t, c.ErrorAfterRows.Steps)
		rows := query(t, db, c.Query)
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err == nil {
			t.Errorf("read %d rows and no error, want the error that follows %d rows (D21)", n, c.ErrorAfterRows.Rows)
		} else if !errors.Is(err, dbimp.ErrIncomplete) && n > 0 {
			t.Errorf("the error is %v, want one that wraps dbimp.ErrIncomplete, because %d rows reached the caller (D107)", err, n)
		}
		if n != c.ErrorAfterRows.Rows {
			t.Errorf("read %d rows before the error, want %d", n, c.ErrorAfterRows.Rows)
		}
	})
	t.Run("close before the end reads no more rows", func(t *testing.T) {
		db := c.open(t, c.Close)
		rows := query(t, db, c.Query)
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		// The script holds no step for another request that reads rows, so the
		// server fails the test if Close sends one (D36).
		if err := rows.Close(); err != nil {
			t.Fatalf("closing the rows: %v", err)
		}
	})
	t.Run("a cancelled context stops the read", func(t *testing.T) {
		CheckGoroutines(t)
		srv := ServeWebSocketScript(t, slices.Concat(c.Login, c.Cancel))
		db := c.Open(t, srv.Addr())
		t.Cleanup(func() { db.Close() })
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			select {
			case <-srv.Held():
				cancel()
			case <-ctx.Done():
			}
		}()
		rows, err := db.QueryContext(ctx, c.Query)
		if err != nil {
			t.Fatalf("sending the query: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
		}
		if err := rows.Err(); !errors.Is(err, context.Canceled) {
			t.Errorf("the error is %v, want context.Canceled (D36)", err)
		}
	})
	t.Run("a request is sent once", func(t *testing.T) {
		CheckGoroutines(t)
		srv := ServeWebSocketScript(t, slices.Concat(c.Login, []WebSocketStep{{Drop: true}}))
		db := c.Open(t, srv.Addr())
		t.Cleanup(func() { db.Close() })
		_ = failingQuery(t, db, c.Query, "a query whose connection closed returned no error")
		if n := len(srv.Frames()) - len(c.Login); n != 1 {
			t.Errorf("the server received the request %d times, want 1 (D8)", n)
		}
	})
	t.Run("an unreachable server is a bad connection", func(t *testing.T) {
		l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		CheckGoroutines(t)
		db := c.Open(t, addr)
		t.Cleanup(func() { db.Close() })
		err = failingQuery(t, db, c.Query, "a query to a closed server returned no error")
		if !errors.Is(err, driver.ErrBadConn) {
			t.Errorf("the error is %v, want driver.ErrBadConn, because the request never reached the server (D8)", err)
		}
	})
	t.Run("a transaction is never faked", func(t *testing.T) {
		if c.Transactions {
			t.Skip("the driver supports transactions")
		}
		db := c.open(t, c.Columns.Steps)
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

// open starts a scripted server with the login and steps, and opens the
// driver against it. It checks the goroutines first, so the check runs after
// the database and the server close.
func (c *WebSocketContract) open(t *testing.T, steps []WebSocketStep) *sql.DB {
	t.Helper()
	CheckGoroutines(t)
	srv := ServeWebSocketScript(t, slices.Concat(c.Login, steps))
	db := c.Open(t, srv.Addr())
	t.Cleanup(func() { db.Close() })
	return db
}

// failingQuery runs q, which must fail, and returns its error. It fails the test
// with msg if the query gave rows.
func failingQuery(t *testing.T, db *sql.DB, q, msg string) error {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err != nil {
		return err
	}
	defer rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	t.Fatal(msg)
	return nil
}
