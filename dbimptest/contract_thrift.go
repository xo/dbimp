package dbimptest

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// ThriftContract is the part of the contract of D8 and D18 to D21 for a driver
// whose product takes its calls as Thrift messages over HTTP, such as Hive and
// Impala (D232). Each call is one HTTP request, and a result arrives in pages,
// so the cases of the other contracts do not fit. The driver supplies the steps
// of a script for each case, because each product has its own calls. A step
// answers the next call of its name, and a call that no step answers fails the
// test.
type ThriftContract struct {
	// Open returns a database for the fake server at url, which is an http:// URL.
	// The driver builds its own DSN from url.
	Open func(t *testing.T, url string) *sql.DB
	// Query is any statement. The fake server ignores it.
	Query string
	// Common holds the steps that answer a call each time it comes, in any order,
	// such as OpenSession and CloseOperation. A step of a case answers first.
	Common []ThriftStep
	// Columns is a result with at least two columns.
	Columns ThriftColumns
	// Null is a result whose first row holds a NULL. The test runs the steps three
	// times in a row, one for each query that it makes.
	Null ThriftNull
	// ErrorAfterRows is a result that holds some rows, and then an error when the
	// driver reads the next page.
	ErrorAfterRows ThriftErrorCase
	// Close is a result of which the first page holds some rows, and no step for a
	// call that reads another page. A driver that reads more after Close fails the
	// test.
	Close []ThriftStep
	// Cancel is a result of which the first page holds one row, then a step with
	// Hold for the call that reads the next page. The test cancels the context
	// while the server holds that call.
	Cancel []ThriftStep
	// Drop is the name of the call that the statement sends first after the
	// session opens, such as ExecuteStatement. The server closes the connection
	// when it reads it.
	Drop string
	// Transactions is true if the driver supports transactions. If it is false,
	// BeginTx must return dbimp.ErrNotSupported (D20).
	Transactions bool
}

// ThriftStep is one answer of a fake server of Thrift.
type ThriftStep struct {
	// Call is the name of the call that the step answers, such as
	// "FetchResults".
	Call string
	// Reply is the whole message that the server writes.
	Reply []byte
	// Hold makes the server wait for the client to leave, and write nothing.
	Hold bool
}

// ThriftColumns is a result and the names of its columns, in order.
type ThriftColumns struct {
	Steps []ThriftStep
	Want  []string
}

// ThriftNull is a result whose first row holds a NULL in column Column.
type ThriftNull struct {
	Steps  []ThriftStep
	Column int
}

// ThriftErrorCase is a result that holds Rows rows and then an error.
type ThriftErrorCase struct {
	Steps []ThriftStep
	Rows  int
}

// ThriftReply returns the message that answers the call name, with a result
// struct that result writes.
func ThriftReply(name string, result func(*dbimp.ThriftEncoder)) []byte {
	var e dbimp.ThriftEncoder
	e.Message(name, dbimp.ThriftReply, 1)
	e.Field(dbimp.ThriftStruct, 0)
	result(&e)
	e.Stop()
	e.Stop()
	return e.Bytes()
}

// thriftServer is a fake server that answers the calls of a script.
type thriftServer struct {
	tb     testing.TB
	steps  []ThriftStep
	common []ThriftStep
	drop   string

	mu    sync.Mutex
	hits  map[string]int
	srv   *httptest.Server
	held  chan struct{}
	onceH sync.Once
}

// serveThrift starts the server with the steps of a case. It closes with the
// test.
func serveThrift(tb testing.TB, steps, common []ThriftStep, drop string) *thriftServer {
	tb.Helper()
	s := &thriftServer{tb: tb, steps: slices.Clone(steps), common: common, drop: drop, hits: map[string]int{}, held: make(chan struct{})}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	tb.Cleanup(s.srv.Close)
	return s
}

// callName reads the name of the call in a request.
func callName(body []byte) (string, error) {
	name, _, _, err := dbimp.NewThriftDecoder(bytes.NewReader(body)).ReadMessage()
	return name, err
}

func (s *thriftServer) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.tb.Errorf("reading a request to the fake server: %v", err)
		return
	}
	name, err := callName(body)
	if err != nil {
		s.tb.Errorf("reading the name of a call in the fake server: %v", err)
		http.Error(w, "not a call", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.hits[name]++
	if name == s.drop && s.hits[name] == 1 {
		s.mu.Unlock()
		hj, ok := w.(http.Hijacker)
		if !ok {
			s.tb.Error("the fake server cannot hijack the connection")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			s.tb.Errorf("hijacking the connection: %v", err)
			return
		}
		_ = conn.Close()
		return
	}
	var step *ThriftStep
	if len(s.steps) > 0 && s.steps[0].Call == name {
		step = &s.steps[0]
		s.steps = s.steps[1:]
	} else if i := slices.IndexFunc(s.common, func(c ThriftStep) bool { return c.Call == name }); i >= 0 {
		step = &s.common[i]
	}
	s.mu.Unlock()
	if step == nil {
		s.tb.Errorf("the fake server has no step for the call %s (D36 and D8)", name)
		http.Error(w, "no step", http.StatusTeapot)
		return
	}
	if step.Hold {
		time.Sleep(heldSettle)
		s.onceH.Do(func() { close(s.held) })
		<-r.Context().Done()
		return
	}
	w.Header().Set("Content-Type", "application/x-thrift")
	_, _ = w.Write(step.Reply)
}

// count returns how many calls of the name the server read.
func (s *thriftServer) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[name]
}

// runThriftContract runs the cases of c. Each case leaves no goroutine behind,
// so the test that calls RunContract must not run in parallel with another
// test.
func runThriftContract(t *testing.T, c *ThriftContract) {
	t.Helper()
	t.Run("columns keep their order", func(t *testing.T) {
		db := c.open(t, c.Columns.Steps)
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
		db := c.open(t, slices.Repeat(c.Null.Steps, 3))
		checkNull(t, db, Contract{Query: c.Query, Null: NullCase{Column: c.Null.Column}})
	})
	t.Run("an error after rows reaches the caller", func(t *testing.T) {
		db := c.open(t, c.ErrorAfterRows.Steps)
		rows := query(t, db, c.Query)
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
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		// The script holds no step for another call that reads rows, so the
		// server fails the test if Close sends one (D36).
		if err := rows.Close(); err != nil {
			t.Fatalf("closing the rows: %v", err)
		}
	})
	t.Run("a cancelled context stops the read", func(t *testing.T) {
		CheckGoroutines(t)
		srv := serveThrift(t, c.Cancel, c.Common, "")
		db := c.Open(t, srv.srv.URL)
		t.Cleanup(func() { db.Close() })
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() {
			select {
			case <-srv.held:
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
		srv := serveThrift(t, nil, c.Common, c.Drop)
		db := c.Open(t, srv.srv.URL)
		t.Cleanup(func() { db.Close() })
		rows, err := db.QueryContext(t.Context(), c.Query)
		if err == nil {
			rows.Close()
			t.Fatal("a query whose connection closed returned no error")
		}
		if n := srv.count(c.Drop); n != 1 {
			t.Errorf("the server received the call %s %d times, want 1 (D8)", c.Drop, n)
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

// open starts a fake server with the steps and opens the driver against it. It
// checks the goroutines first, so the check runs after the database and the
// server close.
func (c *ThriftContract) open(t *testing.T, steps []ThriftStep) *sql.DB {
	t.Helper()
	CheckGoroutines(t)
	srv := serveThrift(t, steps, c.Common, "")
	db := c.Open(t, srv.srv.URL)
	t.Cleanup(func() { db.Close() })
	return db
}
