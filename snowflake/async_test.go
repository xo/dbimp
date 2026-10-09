package snowflake //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
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

// asyncServer is a fake server for a statement that runs. The statement
// answers HTTP 202 with its handle, and each poll answers HTTP 202 until the
// server has answered running polls, and then answers final.
type asyncServer struct {
	t *testing.T
	// running is the count of polls that answer HTTP 202. A negative count
	// answers HTTP 202 for ever.
	running int
	// final is the status and the body of the first poll after them.
	status int
	final  string

	mu      sync.Mutex
	times   []time.Time
	queries []string
	polls   int
	cancels int
	// seen is closed when the server has answered its first poll.
	seen chan struct{}
	once sync.Once
}

const (
	asyncHandle = "01c79a10-0001-ac43-0000-d07900024c7a"
	oneRow      = `{"resultSetMetaData":{"format":"jsonv2","rowType":[{"name":"A","type":"fixed","precision":1,"scale":0,"nullable":false}]},"data":[["1"]],"statementHandle":"` + asyncHandle + `"}`
	running     = `{"code":"333334","message":"Asynchronous execution in progress.","statementHandle":"` + asyncHandle + `","statementStatusUrl":"/api/v2/statements/` + asyncHandle + `"}`
)

func (a *asyncServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	a.mu.Lock()
	a.times = append(a.times, time.Now())
	a.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == pathStatements:
		a.mu.Lock()
		a.queries = append(a.queries, r.URL.RawQuery)
		a.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, running)
	case r.Method == http.MethodPost && r.URL.Path == pathStatements+"/"+asyncHandle+"/cancel":
		a.mu.Lock()
		a.cancels++
		a.mu.Unlock()
		_, _ = io.WriteString(w, `{"code":"000604","message":"SQL execution canceled","sqlState":"57014"}`)
	case r.Method == http.MethodGet && r.URL.Path == pathStatements+"/"+asyncHandle:
		a.mu.Lock()
		a.polls++
		n := a.polls
		a.mu.Unlock()
		a.once.Do(func() { close(a.seen) })
		if a.running < 0 || n <= a.running {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, running)
			return
		}
		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.final)
	default:
		a.t.Errorf("the driver sent %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

// newAsync starts an asyncServer and opens the driver against it. The driver
// sends the cancel.
func newAsync(t *testing.T, a *asyncServer) *Connector {
	t.Helper()
	a.t, a.seen = t, make(chan struct{})
	if a.status == 0 {
		a.status, a.final = http.StatusOK, oneRow
	}
	srv := httptest.NewServer(a)
	t.Cleanup(srv.Close)
	c := connector(config(), srv.URL, true)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// cancelCount returns how many cancels the server got.
func (a *asyncServer) cancelCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancels
}

// TestAsyncPolls holds D183 item 9: the driver starts the statement with
// async=true, polls the handle until the status is HTTP 200, with an interval
// that grows to its limit and that no poll comes before, and reads the result.
func TestAsyncPolls(t *testing.T) {
	t.Parallel()
	a := &asyncServer{running: 3}
	c := newAsync(t, a)
	db := openConnector(t, c)
	_, got, err := readAll(t, db, "SELECT SYSTEM$WAIT(3)")
	if err != nil {
		t.Fatal(err)
	}
	checkRows(t, got, [][]any{{int64(1)}})
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.queries) != 1 || a.queries[0] != "async=true" {
		t.Errorf("the statement was sent with the query %q, want async=true", a.queries)
	}
	if a.polls != 4 {
		t.Errorf("the driver polled %d times, want 4: three that ran and one that ended", a.polls)
	}
	// The statement, and then four polls after 1, 2, 4 and 4 milliseconds: the
	// interval doubles up to its limit. A timer never fires early, so each gap
	// is at least the interval.
	delays := []time.Duration{fastPoll, 2 * fastPoll, 4 * fastPoll, 4 * fastPoll}
	if len(a.times) != 5 {
		t.Fatalf("the server got %d requests, want 5", len(a.times))
	}
	for i, d := range delays {
		if gap := a.times[i+1].Sub(a.times[i]); gap < d {
			t.Errorf("poll %d came %v after the request before it, want at least %v", i+1, gap, d)
		}
	}
	if a.cancels != 0 {
		t.Errorf("the driver canceled a statement that ended, %d times", a.cancels)
	}
}

// TestPollSchedule holds D183 item 15: the waits between the polls are 25 ms,
// 50, 100, 200, 400, and then 500 ms for each poll that follows. The test
// replaces the wait with a recorder, so that it sleeps for no time, and counts
// the polls that the fake server answers.
func TestPollSchedule(t *testing.T) {
	t.Parallel()
	a := &asyncServer{running: 8}
	c := newAsync(t, a)
	c.pollMin, c.pollMax = pollMin, pollMax
	var waits []time.Duration
	c.wait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	db := openConnector(t, c)
	if _, _, err := readAll(t, db, "SELECT SYSTEM$WAIT(8)"); err != nil {
		t.Fatal(err)
	}
	ms := time.Millisecond
	want := []time.Duration{25 * ms, 50 * ms, 100 * ms, 200 * ms, 400 * ms, 500 * ms, 500 * ms, 500 * ms, 500 * ms}
	if !slices.Equal(waits, want) {
		t.Errorf("the driver waited %v, want %v", waits, want)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.polls != len(want) {
		t.Errorf("the driver polled %d times, want %d", a.polls, len(want))
	}
}

// openConnector opens a database on c.
func openConnector(t *testing.T, c *Connector) *sql.DB {
	t.Helper()
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	return db
}

// TestAsyncErrors holds that a statement that ends with an error while it is
// polled gives that error before any row, with no cancel, because the statement
// ended. The code 000604 is a statement that someone else canceled.
func TestAsyncErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		body   string
		code   string
		is     error
	}{
		{"a statement that failed", http.StatusUnprocessableEntity, `{"code":"100051","message":"Division by zero","sqlState":"22012","statementHandle":"` + asyncHandle + `"}`, "100051", nil},
		{"a statement that someone canceled", http.StatusUnprocessableEntity, `{"code":"000604","message":"SQL execution canceled","sqlState":"57014","statementHandle":"` + asyncHandle + `"}`, "000604", ErrCanceled},
		{"a statement that reached its timeout", http.StatusRequestTimeout, `{"code":"000630","message":"Statement reached its statement or warehouse timeout of 2 second(s) and was canceled.","sqlState":"57014"}`, "000630", ErrTimeout},
	} {
		a := &asyncServer{running: 2, status: tt.status, final: tt.body}
		c := newAsync(t, a)
		db := openConnector(t, c)
		_, got, err := readAll(t, db, "SELECT 1/0")
		if len(got) != 0 {
			t.Errorf("%s: got rows %v", tt.name, got)
		}
		e := serverError(t, err)
		if e.Code != tt.code {
			t.Errorf("%s: the code is %s, want %s", tt.name, e.Code, tt.code)
		}
		if tt.is != nil && !errors.Is(err, tt.is) {
			t.Errorf("%s: the error does not wrap %v", tt.name, tt.is)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", tt.name)
		}
		if n := a.cancelCount(); n != 0 {
			t.Errorf("%s: the driver canceled the statement %d times after it ended", tt.name, n)
		}
	}
}

// TestCancelWhenTheContextEnds holds D183 item 9: when the context ends while
// the statement runs, the driver sends the cancel by its handle, and the error
// is the error of the context, never driver.ErrBadConn.
func TestCancelWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	a := &asyncServer{running: -1}
	c := newAsync(t, a)
	db := openConnector(t, c)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		<-a.seen
		cancel()
	}()
	_, _, err := readAllContext(t, ctx, db, "SELECT SYSTEM$WAIT(20)")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if n := a.cancelCount(); n != 1 {
		t.Errorf("the driver sent the cancel %d times, want 1", n)
	}
	// The deadline is the other end of a context.
	a2 := &asyncServer{running: -1}
	db2 := openConnector(t, newAsync(t, a2))
	ctx2, cancel2 := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel2()
	if _, _, err := readAllContext(t, ctx2, db2, "SELECT SYSTEM$WAIT(20)"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	if n := a2.cancelCount(); n != 1 {
		t.Errorf("the driver sent the cancel %d times after the deadline, want 1", n)
	}
}

// TestCancelWhenTheRowsCloseEarly holds D183 item 9: rows that the caller
// closes before the end cancel the statement, and rows that the caller reads to
// the end do not. A QueryRow of a result of one row has read the end.
func TestCancelWhenTheRowsCloseEarly(t *testing.T) {
	t.Parallel()
	many := `{"resultSetMetaData":{"format":"jsonv2","rowType":[{"name":"A","type":"fixed","precision":1,"scale":0,"nullable":false}]},"data":[["1"],["2"],["3"]],"statementHandle":"` + asyncHandle + `"}`
	for _, tt := range []struct {
		name    string
		final   string
		read    int
		cancels int
	}{
		{"one row of three", many, 1, 1},
		{"every row of three", many, 3, 0},
		{"the one row of one", oneRow, 1, 0},
		{"no row of three", many, 0, 1},
	} {
		a := &asyncServer{running: 1, status: http.StatusOK, final: tt.final}
		db := openConnector(t, newAsync(t, a))
		rows, err := db.QueryContext(t.Context(), "SELECT a")
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		for range tt.read {
			if !rows.Next() {
				t.Fatalf("%s: no row: %v", tt.name, rows.Err())
			}
		}
		//nolint:sqlclosecheck // The test closes the rows at a chosen point, and counts the cancels after it.
		if err := rows.Close(); err != nil {
			t.Errorf("%s: closing the rows: %v", tt.name, err)
		}
		if n := a.cancelCount(); n != tt.cancels {
			t.Errorf("%s: the driver sent %d cancels, want %d", tt.name, n, tt.cancels)
		}
	}
}

// TestCancelByTheHandleOfTheLink holds that a statement that finishes at once,
// with HTTP 200, is canceled by the handle of the header Link when the caller
// closes the rows early.
func TestCancelByTheHandleOfTheLink(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Link", `</api/v2/statements/`+asyncHandle+`?requestId=a&partition=0>; rel="first"`)
		_, _ = io.WriteString(w, `{"resultSetMetaData":{"format":"jsonv2","rowType":[{"name":"A","type":"fixed","precision":1,"scale":0}]},"data":[["1"],["2"]]}`)
	}))
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, true)
	rows, err := db.QueryContext(t.Context(), "SELECT a")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	//nolint:sqlclosecheck // The test closes the rows at a chosen point, and looks at the requests after it.
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "POST " + pathStatements + "/" + asyncHandle + "/cancel"; len(paths) != 2 || paths[1] != want {
		t.Errorf("the requests are %v, want the statement and %q", paths, want)
	}
}

// TestNoRedirect holds D183 item 10: the driver follows no redirect, so the token
// goes to the host of the DSN only.
func TestNoRedirect(t *testing.T) {
	t.Parallel()
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the driver followed a redirect to another host")
	}))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect) //nolint:gosec // The test sends the client to a fake server on purpose.
	}))
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, true)
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	e := serverError(t, err)
	if e.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("the error is %+v, want HTTP 307", *e)
	}
}
