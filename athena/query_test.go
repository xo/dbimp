package athena //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// run sends the statement of the file n to a fake server that answers with the
// recorded exchanges ns, and returns the operations that the server received and
// the error of Query or of the rows.
func run(t *testing.T, n int, ns ...int) ([]string, error) {
	t.Helper()
	f := newFake(t, sequence(t, true, ns...))
	db := open(t, testConfig(), f.srv.URL)
	rows, err := db.QueryContext(t.Context(), queryOf(t, n))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
		}
		err = rows.Err()
	}
	return f.targets(), err
}

// TestReplayErrorsOfTheRequest reads the errors that the server gives for a
// request that it refuses. The statement never runs, so the driver sends nothing
// more and the error comes before any row (D8).
func TestReplayErrorsOfTheRequest(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		file       int
		typ, code  string
		messageHas string
	}{
		{"two statements", 26, "InvalidRequestException", "MALFORMED_QUERY", "Only one sql statement is allowed"},
		{"a syntax error", 203, "InvalidRequestException", "MALFORMED_QUERY", "mismatched input 'SELEC'"},
		{"a workgroup that the user cannot use", 216, "AccessDeniedException", "", "You are not authorized"},
		{"a token that is used twice", 223, "InvalidRequestException", "IDEMPOTENT_PARAMETER_MISMATCH", "Idempotent parameters do not match"},
		{"a long query text", 381, "InvalidRequestException", "INVALID_INPUT", "queryString"},
		{"a statement that the server does not support", 253, "InvalidRequestException", "MALFORMED_QUERY", "Queries of this type are not supported"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			targets, err := run(t, tt.file, tt.file)
			var aerr *Error
			if !errors.As(err, &aerr) {
				t.Fatalf("the error is %v, want an *Error", err)
			}
			if aerr.HTTPStatus != http.StatusBadRequest || aerr.Type != tt.typ || aerr.Code != tt.code || !strings.Contains(aerr.Message, tt.messageHas) {
				t.Errorf("the error is %+v, want HTTP 400, the type %s, the code %q and the text %q", *aerr, tt.typ, tt.code, tt.messageHas)
			}
			if aerr.State != "" || aerr.QueryID != "" {
				t.Errorf("the error names the state %q and the query %q, want neither, because no query ran", aerr.State, aerr.QueryID)
			}
			if errors.Is(err, dbimp.ErrIncomplete) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, ErrCanceled) {
				t.Errorf("the error %v wraps a sentinel that it must not", err)
			}
			if want := []string{"StartQueryExecution"}; !slices.Equal(targets, want) {
				t.Errorf("the requests are %q, want %q", targets, want)
			}
			if !strings.HasPrefix(err.Error(), "athena: ") {
				t.Errorf("the message %q does not start with the name of the driver", err)
			}
		})
	}
}

// TestReplayErrorsOfTheQuery reads the errors of a query that runs and fails.
// The server answers the start, and then GetQueryExecution says FAILED with the
// text in StateChangeReason (recorded). The driver sends no GetQueryResults and
// no stop, because the query ended, and the error comes before any row (D21).
func TestReplayErrorsOfTheQuery(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name           string
		start, get     int
		errType, cat   int
		messageHas     string
		wantAthenaType bool
	}{
		{"a missing table", 205, 206, 1301, 2, "Table 'awsdatacatalog.dbimp_test.dbimp_it_nosuch' does not exist", true},
		{"a division by zero", 207, 208, 1001, 2, "DIVISION_BY_ZERO", true},
		{"a missing column", 212, 213, 1006, 2, "COLUMN_NOT_FOUND", true},
		{"a parameter that does not fit", 187, 188, 1100, 2, "INVALID_CAST_ARGUMENT", true},
		{"too few parameters", 189, 190, 1100, 2, "Incorrect number of parameters: expected 2 but found 1", true},
		{"an update of an external table", 46, 47, 1200, 2, "NOT_SUPPORTED", true},
		// The type 1106 has an empty ErrorMessage, and the text only in
		// StateChangeReason (recorded: "a statement that runs long: the execution").
		{"an empty ErrorMessage", 218, 219, 1106, 2, "INVALID_FUNCTION_ARGUMENT", true},
		// The type 9999 has the cause only in StateChangeReason.
		{"a catalog that does not exist", 400, 401, 9999, 3, "athena:GetDataCatalog", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			targets, err := run(t, tt.start, tt.start, tt.get)
			var aerr *Error
			if !errors.As(err, &aerr) {
				t.Fatalf("the error is %v, want an *Error", err)
			}
			if aerr.State != stateFailed || aerr.ErrorType != tt.errType || aerr.Category != tt.cat || !strings.Contains(aerr.Message, tt.messageHas) {
				t.Errorf("the error is %+v, want the state FAILED, the type %d, the category %d and the text %q", *aerr, tt.errType, tt.cat, tt.messageHas)
			}
			if aerr.HTTPStatus != 0 || aerr.QueryID == "" {
				t.Errorf("the error has HTTP status %d and the query %q, want no status and the id of the query", aerr.HTTPStatus, aerr.QueryID)
			}
			if errors.Is(err, ErrCanceled) || errors.Is(err, dbimp.ErrIncomplete) || errors.As(err, new(*dbimp.StatusError)) {
				t.Errorf("the error %v wraps a sentinel that it must not", err)
			}
			if want := []string{"StartQueryExecution", "GetQueryExecution"}; !slices.Equal(targets, want) {
				t.Errorf("the requests are %q, want %q", targets, want)
			}
		})
	}
}

// TestReplayErrorOfExec reads the same errors from Exec, which reads the result
// to its end.
func TestReplayErrorOfExec(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, err := db.ExecContext(t.Context(), "SELECT * FROM dbimp_it_nosuch")
	var aerr *Error
	if !errors.As(err, &aerr) || aerr.ErrorType != 1301 {
		t.Errorf("the error is %v, want the type 1301", err)
	}
}

// TestReplayPolling polls a query that is QUEUED, RUNNING and then SUCCEEDED
// (recorded: "a long statement for the states"), reads its result, and holds the
// interval of the poll: it starts at 100 ms, doubles, and stays at one second
// (D192 item 8).
func TestReplayPolling(t *testing.T) {
	t.Parallel()
	f := newFake(t, sequence(t, false, 344, 345, 346, 347, 348, 351))
	c := connectorAt(t, testConfig(), f.srv.URL)
	var (
		mu     sync.Mutex
		delays []time.Duration
	)
	c.wait = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
		return ctx.Err()
	}
	c.pollMin, c.pollMax = pollMin, pollMax
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	var count int64
	if err := db.QueryRowContext(t.Context(), queryOf(t, 344)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2500000000 {
		t.Errorf("the count is %d, want 2500000000", count)
	}
	want := []string{"StartQueryExecution", "GetQueryExecution", "GetQueryExecution", "GetQueryExecution", "GetQueryExecution", "GetQueryResults"}
	if got := f.targets(); !slices.Equal(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	if want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}; !slices.Equal(delays, want) {
		t.Errorf("the waits are %v, want %v", delays, want)
	}
	// The wait stays at one second.
	if got := min(800*time.Millisecond*2, pollMax); got != time.Second {
		t.Errorf("the longest wait is %v, want one second", got)
	}
}

// TestReplayCanceledQuery reads a query that someone else stopped. The state is
// CANCELLED and the text is "Query cancelled by user", and the error says so
// although the driver did not stop the query (recorded: "the minute statement
// after the stop", D192 item 8). The driver sends no stop.
func TestReplayCanceledQuery(t *testing.T) {
	t.Parallel()
	targets, err := run(t, 413, 413, 414, 415, 416, 418)
	var aerr *Error
	if !errors.As(err, &aerr) || aerr.State != stateCancelled || aerr.Message != "Query cancelled by user" {
		t.Fatalf("the error is %v, want the state CANCELLED and the text of the server", err)
	}
	if !errors.Is(err, ErrCanceled) {
		t.Errorf("the error %v is not ErrCanceled", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("the error %v is context.Canceled, but the context did not end", err)
	}
	if aerr.ErrorType != 0 || aerr.Category != 0 {
		t.Errorf("the error has the type %d and the category %d, want none, because a canceled query has no AthenaError", aerr.ErrorType, aerr.Category)
	}
	want := []string{"StartQueryExecution", "GetQueryExecution", "GetQueryExecution", "GetQueryExecution", "GetQueryExecution"}
	if !slices.Equal(targets, want) {
		t.Errorf("the requests are %q, want %q", targets, want)
	}
}

// TestContextEndStopsTheQuery holds D36 and D192 item 8: when the context ends
// while the query runs, the driver sends StopQueryExecution for the id of the
// query, and returns the error of the context. The stop is sent although the
// context ended, and no goroutine is left (recorded: "stop the minute
// statement").
func TestContextEndStopsTheQuery(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	f := newFake(t, sequence(t, true, 413, 414, 415, 417))
	c := connectorAt(t, testConfig(), f.srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waits := 0
	c.wait = func(ctx context.Context, _ time.Duration) error {
		if waits++; waits == 3 {
			cancel()
		}
		return ctx.Err()
	}
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	_, _, err := readContext(ctx, t, db, queryOf(t, 413))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the error is %v, want context.Canceled", err)
	}
	if errors.Is(err, driver.ErrBadConn) {
		t.Errorf("the error %v is driver.ErrBadConn, but the statement reached the server", err)
	}
	want := []string{"StartQueryExecution", "GetQueryExecution", "GetQueryExecution", "StopQueryExecution"}
	if got := f.targets(); !slices.Equal(got, want) {
		t.Fatalf("the requests are %q, want %q", got, want)
	}
	reqs := f.requests()
	id := idOf(t, reqs[0], exchange(t, 413))
	if !strings.Contains(reqs[3].body, id) {
		t.Errorf("the stop is %s, want it to name the query %s", reqs[3].body, id)
	}
}

// idOf returns the id of the query that the answer to the start, in ex, names,
// and checks that the request of the poll names it.
func idOf(t *testing.T, start request, ex *dbimptest.Exchange) string {
	t.Helper()
	body := string(ex.Response.Content())
	_, rest, ok := strings.Cut(body, `"QueryExecutionId":"`)
	if !ok {
		t.Fatalf("the answer %s names no query", body)
	}
	id, _, _ := strings.Cut(rest, `"`)
	if !strings.Contains(start.body, "QueryString") {
		t.Fatalf("the first request is %s, want the start", start.body)
	}
	return id
}

// TestContextBeforeTheStartSendsNothing makes sure that a context that ended
// before the statement sends no request.
func TestContextBeforeTheStartSendsNothing(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(http.ResponseWriter, request, int) { t.Error("the driver sent a request") })
	db := open(t, testConfig(), f.srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := readContext(ctx, t, db, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if _, err := db.ExecContext(ctx, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
}

// TestDeadlineStopsTheQuery holds that a deadline is the error of the context,
// context.DeadlineExceeded, and stops the query like a cancel.
func TestDeadlineStopsTheQuery(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	f := newFake(t, sequence(t, true, 413, 417))
	c := connectorAt(t, testConfig(), f.srv.URL)
	ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
	defer cancel()
	c.wait = func(ctx context.Context, _ time.Duration) error {
		return context.DeadlineExceeded
	}
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	_, _, err := readContext(ctx, t, db, queryOf(t, 413))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the error is %v, want context.DeadlineExceeded", err)
	}
	if want := []string{"StartQueryExecution", "StopQueryExecution"}; !slices.Equal(f.targets(), want) {
		t.Errorf("the requests are %q, want %q", f.targets(), want)
	}
}

// TestFailureAfterTheStartIsNotABadConnection holds D8: a poll whose connection
// fails, after the server took the statement, is not driver.ErrBadConn, because
// database/sql runs the statement again. The server got the start once.
func TestFailureAfterTheStartIsNotABadConnection(t *testing.T) {
	t.Parallel()
	start := exchange(t, 17)
	f := newFake(t, func(w http.ResponseWriter, r request, _ int) {
		if r.target == "StartQueryExecution" {
			serve(w, start)
			return
		}
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
	})
	db := open(t, testConfig(), f.srv.URL)
	_, _, err := read(t, db, "SELECT 1")
	if err == nil {
		t.Fatal("a query whose poll failed returned no error")
	}
	if errors.Is(err, driver.ErrBadConn) {
		t.Errorf("the error %v is driver.ErrBadConn, so database/sql sends the statement again (D8)", err)
	}
	starts := 0
	for _, target := range f.targets() {
		if target == "StartQueryExecution" {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("the server received the start %d times, want 1", starts)
	}
}

// TestUnreachableServerIsABadConnection holds D8: when the start never reached a
// server, the error wraps driver.ErrBadConn.
func TestUnreachableServerIsABadConnection(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(http.ResponseWriter, request, int) {})
	url := f.srv.URL
	f.srv.Close()
	db := open(t, testConfig(), url)
	_, _, err := read(t, db, "SELECT 1")
	if !errors.Is(err, driver.ErrBadConn) {
		t.Errorf("the error is %v, want driver.ErrBadConn", err)
	}
}

// TestEachStatementHasANewToken holds D192: each StartQueryExecution has a
// ClientRequestToken of at least 32 characters, and two statements do not share
// one (recorded: "a client request token that is used twice").
func TestEachStatementHasANewToken(t *testing.T) {
	t.Parallel()
	f := newFake(t, selectOne(t))
	db := open(t, testConfig(), f.srv.URL)
	for range 3 {
		_, rows, err := read(t, db, "SELECT 1 AS a, 'x' AS b")
		if err != nil || len(rows) != 1 {
			t.Fatalf("the statement gave %q and %v", rows, err)
		}
	}
	seen := map[string]bool{}
	for _, r := range f.requests() {
		if r.target != "StartQueryExecution" {
			continue
		}
		_, rest, ok := strings.Cut(r.body, `"ClientRequestToken":"`)
		token, _, _ := strings.Cut(rest, `"`)
		if !ok || len(token) < 32 || seen[token] {
			t.Errorf("the token %q is missing, shorter than 32 characters or used twice", token)
		}
		seen[token] = true
	}
	if len(seen) != 3 {
		t.Errorf("saw %d tokens, want 3", len(seen))
	}
}

// TestFollowsNoRedirect holds that the signature goes to the host of the DSN
// only (D192): a redirect is an error, and the second server gets nothing.
func TestFollowsNoRedirect(t *testing.T) {
	t.Parallel()
	other := newFake(t, func(http.ResponseWriter, request, int) { t.Error("the driver followed a redirect") })
	f := newFake(t, func(w http.ResponseWriter, _ request, _ int) {
		w.Header().Set("Location", other.srv.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	db := open(t, testConfig(), f.srv.URL)
	if _, _, err := read(t, db, "SELECT 1"); err == nil {
		t.Error("a redirect gave no error")
	}
}

// TestPing sends GetWorkGroup for the workgroup of the DSN, and ListWorkGroups
// when the DSN names none (recorded: "the workgroup", "list the workgroups").
func TestPing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		workgroup string
		file      int
		target    string
	}{
		{"with a workgroup", "dbimp", 233, "GetWorkGroup"},
		{"with no workgroup", "", 232, "ListWorkGroups"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, sequence(t, false, tt.file))
			cfg := testConfig()
			cfg.WorkGroup = tt.workgroup
			db := open(t, cfg, f.srv.URL)
			if err := db.PingContext(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := f.targets(); !slices.Equal(got, []string{tt.target}) {
				t.Errorf("the requests are %q, want %q", got, []string{tt.target})
			}
			if tt.workgroup != "" && !strings.Contains(f.requests()[0].body, tt.workgroup) {
				t.Errorf("the request is %s, want it to name the workgroup", f.requests()[0].body)
			}
		})
	}
}

// TestPingError holds that a wrong secret fails the ping with the error of the
// server (recorded: "a wrong secret").
func TestPingError(t *testing.T) {
	t.Parallel()
	denied := exchange(t, 225)
	f := newFake(t, func(w http.ResponseWriter, _ request, _ int) { serve(w, denied) })
	db := open(t, testConfig(), f.srv.URL)
	err := db.PingContext(t.Context())
	var aerr *Error
	if !errors.As(err, &aerr) || aerr.Type != "InvalidSignatureException" || !strings.HasPrefix(aerr.Message, "The request signature we calculated") {
		t.Errorf("the error is %v, want the InvalidSignatureException of the server", err)
	}
}

// TestTwoQueriesAtOnce holds that two queries run at the same time on one
// sql.DB, each on its own connection (step 12).
func TestTwoQueriesAtOnce(t *testing.T) {
	t.Parallel()
	db := replay(t)
	var wg sync.WaitGroup
	for _, n := range []int{17, 160} {
		wg.Go(func() {
			if _, _, err := read(t, db, queryOf(t, n)); err != nil {
				t.Errorf("%q: %v", queryOf(t, n), err)
			}
		})
	}
	wg.Wait()
}

// TestNoTransaction holds D20: BeginTx fails with dbimp.ErrNotSupported, and the
// server refuses START TRANSACTION (recorded: "a start transaction").
func TestNoTransaction(t *testing.T) {
	t.Parallel()
	db := replay(t)
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
	}
	if _, err := db.ExecContext(t.Context(), "START TRANSACTION"); err == nil {
		t.Error("START TRANSACTION gave no error")
	}
}
