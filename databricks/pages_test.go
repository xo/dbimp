package databricks //nolint:testpackage // The tests point a connector at a fake server and set its poll, which only the package can do.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// pendingID is the id of the statement of the recorded exchanges 26 to 28, which
// the script of a poll answers with.
const pendingID = "01f1c46b-ecab-1b88-9e4a-8b6b3f39df9a"

// TestPollsAStatementThatRuns holds D193 item 8: a statement that the server
// answers with PENDING is polled by its id until it succeeds, and the driver reads
// the result from the answer of the poll. The answers are the recorded answers
// 26, which holds PENDING, and 28 for the polls.
func TestPollsAStatementThatRuns(t *testing.T) {
	t.Parallel()
	s, srv := newScript(t, 26, 26, 26, 28)
	db := open(t, recordedConfig(), srv.URL)
	_, got, err := readAll(t, db, "SELECT 1 AS a")
	if err != nil {
		t.Fatal(err)
	}
	checkRows(t, got, [][]any{{int64(1)}})
	want := []string{"POST " + pathStatements, "GET " + pathStatements + "/" + pendingID, "GET " + pathStatements + "/" + pendingID, "GET " + pathStatements + "/" + pendingID}
	if got := s.requests(); !slices.Equal(got, want) {
		t.Errorf("the requests are %v, want %v", got, want)
	}
	if n := s.cancelCount(); n != 0 {
		t.Errorf("the driver canceled a statement that ended, %d times", n)
	}
}

// TestPollSchedule holds that the waits between the polls are 25 ms, 50, 100,
// 200, 400, and then 500 ms for each poll that follows. The test replaces the
// wait with a recorder, so that it sleeps for no time.
func TestPollSchedule(t *testing.T) {
	t.Parallel()
	answers := []int{26}
	for range 8 {
		answers = append(answers, 26)
	}
	answers = append(answers, 28)
	s, srv := newScript(t, answers...)
	c := connector(recordedConfig(), srv.URL)
	c.pollMin, c.pollMax = pollMin, pollMax
	var waits []time.Duration
	c.wait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	if _, _, err := readAll(t, db, "SELECT 1 AS a"); err != nil {
		t.Fatal(err)
	}
	ms := time.Millisecond
	want := []time.Duration{25 * ms, 50 * ms, 100 * ms, 200 * ms, 400 * ms, 500 * ms, 500 * ms, 500 * ms, 500 * ms}
	if !slices.Equal(waits, want) {
		t.Errorf("the driver waited %v, want %v", waits, want)
	}
	if got := len(s.requests()); got != len(want)+1 {
		t.Errorf("the driver sent %d requests, want %d", got, len(want)+1)
	}
}

// TestPollError holds that a statement that fails while it is polled gives its
// error before any row, with no cancel, because the statement ended. The
// answers are the recorded exchanges 170 and 171.
func TestPollError(t *testing.T) {
	t.Parallel()
	s, srv := newScript(t, 170, 171)
	db := open(t, recordedConfig(), srv.URL)
	_, got, err := readAll(t, db, "SELEC 1")
	if len(got) != 0 {
		t.Errorf("got rows %v", got)
	}
	e := serverError(t, err)
	if e.SQLState != "42601" || !strings.Contains(e.Message, "PARSE_SYNTAX_ERROR") || e.StatementID == "" {
		t.Errorf("the error is %+v", *e)
	}
	if errors.Is(err, dbimp.ErrIncomplete) {
		t.Error("an error before any row wraps dbimp.ErrIncomplete")
	}
	if n := s.cancelCount(); n != 0 {
		t.Errorf("the driver canceled the statement %d times after it ended", n)
	}
}

// TestCanceledByAnotherClient holds that a statement in the state CANCELED, which
// someone else canceled, is an error that wraps ErrCanceled (measured).
func TestCanceledByAnotherClient(t *testing.T) {
	t.Parallel()
	_, srv := newScript(t, 26, 184)
	db := open(t, recordedConfig(), srv.URL)
	_, err := db.ExecContext(t.Context(), "SELECT 1 AS a")
	if !errors.Is(err, ErrCanceled) {
		t.Errorf("the error is %v, want ErrCanceled", err)
	}
	if e := serverError(t, err); e.Code != "" || e.SQLState != "" {
		t.Errorf("the error is %+v, want no code and no SQLSTATE", *e)
	}
}

// TestCancelWhenTheContextEnds holds D36 and D193 item 8: when the context ends
// while the statement runs, the driver sends the cancel by the id of the statement,
// and the error is the error of the context, never driver.ErrBadConn.
func TestCancelWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	s, srv := newScript(t, 26)
	db := open(t, recordedConfig(), srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		<-s.seen
		cancel()
	}()
	_, _, err := readAllContext(t, ctx, db, "SELECT 1 AS a")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if n := s.cancelCount(); n != 1 {
		t.Errorf("the driver sent the cancel %d times, want 1", n)
	}
	if got := s.requests(); !slices.Contains(got, "POST "+pathStatements+"/"+pendingID+"/cancel") {
		t.Errorf("the requests are %v, want the cancel of %s", got, pendingID)
	}
	// The deadline is the other end of a context.
	s2, srv2 := newScript(t, 26)
	db2 := open(t, recordedConfig(), srv2.URL)
	ctx2, cancel2 := context.WithTimeout(t.Context(), 400*time.Millisecond)
	defer cancel2()
	if _, _, err := readAllContext(t, ctx2, db2, "SELECT 1 AS a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	if n := s2.cancelCount(); n != 1 {
		t.Errorf("the driver sent the cancel %d times after the deadline, want 1", n)
	}
}

// TestTimeoutCancels holds that WithTimeout ends the wait with
// context.DeadlineExceeded, and cancels the statement on the server.
func TestTimeoutCancels(t *testing.T) {
	t.Parallel()
	s, srv := newScript(t, 26)
	db := open(t, recordedConfig(), srv.URL)
	_, err := db.ExecContext(t.Context(), "SELECT 1 AS a", WithTimeout(400*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	if n := s.cancelCount(); n != 1 {
		t.Errorf("the driver sent the cancel %d times, want 1", n)
	}
}

// TestWaitFollowsTheDeadline holds that the driver asks for a wait of 50 seconds,
// and for none when the context ends sooner, so that it learns the id of the
// statement and can cancel it (D193 item 8).
func TestWaitFollowsTheDeadline(t *testing.T) {
	t.Parallel()
	s, srv := newScript(t, 21)
	db := open(t, recordedConfig(), srv.URL)
	if _, _, err := readAll(t, db, "SELECT 1 AS a, 'x' AS b"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, _, err := readAllContext(t, ctx, db, "SELECT 1 AS a, 'x' AS b"); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel2()
	if _, _, err := readAllContext(t, ctx2, db, "SELECT 1 AS a, 'x' AS b"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, want := range []string{`"wait_timeout":"50s"`, `"wait_timeout":"0s"`, `"wait_timeout":"50s"`} {
		if !strings.Contains(s.bodies[i], want) {
			t.Errorf("the body %d is %s, want %s", i, s.bodies[i], want)
		}
	}
}

// TestResultsTheDriverRefuses holds D193 item 2: a result that the server cut,
// and a result in the form of external links, are errors before any row, and
// no row comes out of a statement that the server cut. The answers are recorded:
// 150 holds truncated, and 45 holds external links.
func TestResultsTheDriverRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		n    int
		is   error
	}{
		{"a result that the server cut at a byte limit", 150, ErrTruncated},
		{"a result that the server cut at a row limit of zero", 148, ErrTruncated},
		{"a result of external links", 45, dbimp.ErrNotSupported},
		{"a result of external links in the format of Arrow", 46, dbimp.ErrNotSupported},
	} {
		_, srv := newScript(t, tt.n)
		db := open(t, recordedConfig(), srv.URL)
		err := queryError(t.Context(), db, "SELECT 1")
		if err == nil {
			t.Errorf("%s: no error", tt.name)
			continue
		}
		if !errors.Is(err, tt.is) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.is)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", tt.name)
		}
	}
}

// chunked is a recorded answer, with its manifest changed to name two chunks and
// its result changed to link to the second, which no recording holds (D193 item 2).
func chunked(t *testing.T, n int, edit func(string) string) *httptest.Server {
	t.Helper()
	body := edit(string(exchange(t, n).Response.Content()))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestMoreThanOneChunk holds that a result of more than one chunk is an error,
// when the manifest names it before the rows, and an error after the rows when only
// the end of the result names the next chunk (D193 item 2).
func TestMoreThanOneChunk(t *testing.T) {
	t.Parallel()
	// The manifest names two chunks.
	srv := chunked(t, 136, func(s string) string {
		return strings.Replace(s, `"total_chunk_count":1`, `"total_chunk_count":2`, 1)
	})
	db := open(t, recordedConfig(), srv.URL)
	if err := queryError(t.Context(), db, "SELECT 1"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a manifest of two chunks gave %v, want dbimp.ErrNotSupported", err)
	}
	// Only the result links to the next chunk, after its rows.
	srv = chunked(t, 136, func(s string) string {
		i := strings.LastIndex(s, "]")
		return s[:i+1] + `,"next_chunk_index":1,"next_chunk_internal_link":"/api/2.0/sql/statements/x/result/chunks/1"` + s[i+1:]
	})
	db = open(t, recordedConfig(), srv.URL)
	_, got, err := readAll(t, db, "SELECT 1")
	if len(got) != 3000 {
		t.Errorf("read %d rows before the link to the next chunk, want 3000", len(got))
	}
	if !errors.Is(err, dbimp.ErrNotSupported) || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want dbimp.ErrNotSupported wrapped with dbimp.ErrIncomplete (D107)", err)
	}
}

// TestRowCountIsChecked holds that a result with fewer rows than row_count names
// gives ErrCut after the rows, wrapped with dbimp.ErrIncomplete (D21 and D107).
func TestRowCountIsChecked(t *testing.T) {
	t.Parallel()
	srv := chunked(t, 136, func(s string) string {
		return strings.Replace(s, `"row_count":3000,"data_array"`, `"row_count":3001,"data_array"`, 1)
	})
	db := open(t, recordedConfig(), srv.URL)
	_, got, err := readAll(t, db, "SELECT 1")
	if len(got) != 3000 {
		t.Errorf("read %d rows, want 3000", len(got))
	}
	if !errors.Is(err, ErrCut) || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want ErrCut wrapped with dbimp.ErrIncomplete", err)
	}
}

// TestBigResult reads the recorded result of 3000 rows of 300 characters, which is
// the largest that the recordings hold, through the driver, and counts the rows.
func TestBigResult(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, got, err := readAll(t, db, statementOf(t, 140))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3000 {
		t.Fatalf("read %d rows, want 3000", len(got))
	}
	if s, _ := got[2999][1].(string); len(s) != 300 {
		t.Errorf("the last row holds a string of %d characters, want 300", len(s))
	}
}

// bigServer answers each statement with the recorded answer 136, with the rows
// made as the reader asks for them, so that the answer is larger than the memory
// that the test allows.
type bigServer struct {
	head, tail string
	rows       int
}

func (b *bigServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, b.head)
	pad := strings.Repeat("x", 200)
	for i := range b.rows {
		if i > 0 {
			_, _ = io.WriteString(w, ",")
		}
		if _, err := fmt.Fprintf(w, `["%d","row-%d-%s"]`, i, i, pad); err != nil {
			return
		}
	}
	_, _ = io.WriteString(w, b.tail)
}

// TestLargeResultHoldsLittleMemory holds D25: a result of 300000 rows, about 70
// MB of JSON, never sits in memory. The heap stays within 16 MiB of where it
// started while the rows are read. The head and the tail of the answer are those of
// the recorded answer 136.
func TestLargeResultHoldsLittleMemory(t *testing.T) { //nolint:paralleltest // The test reads the heap of the process, which another test changes.
	const n = 300000
	head, tail := split(t, 136)
	head = strings.ReplaceAll(head, `"row_count":3000`, fmt.Sprintf(`"row_count":%d`, n))
	srv := httptest.NewServer(&bigServer{head: head, tail: tail, rows: n})
	t.Cleanup(srv.Close)
	db := open(t, recordedConfig(), srv.URL)
	rows, err := db.QueryContext(t.Context(), "SELECT id, s")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	runtime.GC()
	var base, now runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak uint64
	count := 0
	for rows.Next() {
		var (
			i int64
			s string
		)
		if err := rows.Scan(&i, &s); err != nil {
			t.Fatal(err)
		}
		if count++; count%50000 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&now)
			peak = max(peak, now.HeapAlloc)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("after %d rows: %v", count, err)
	}
	if count != n {
		t.Errorf("read %d rows, want %d", count, n)
	}
	if peak > base.HeapAlloc+16<<20 {
		t.Errorf("the heap grew by %d MiB while the driver read the rows, want less than 16 MiB (D25)", (peak-base.HeapAlloc)>>20)
	}
}

// TestNoRedirect holds D193 item 9: the driver follows no redirect, so the token
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
	db := open(t, recordedConfig(), srv.URL)
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	e := serverError(t, err)
	if e.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("the error is %+v, want HTTP 307", *e)
	}
}

// TestRequestHeaders holds that each request carries the token as a Bearer token
// and no other credential, asks for JSON, and sends JSON (D193 item 9).
func TestRequestHeaders(t *testing.T) {
	t.Parallel()
	var got http.Header
	var path, method string
	answer := exchange(t, 21).Response.Content()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got, path, method = r.Header.Clone(), r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(answer)
	}))
	t.Cleanup(srv.Close)
	db := open(t, recordedConfig(), srv.URL)
	if _, _, err := readAll(t, db, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if auth := got.Get("Authorization"); auth != "Bearer "+testToken {
		t.Errorf("the header Authorization is %q, want a Bearer token", auth)
	}
	if got.Get("Content-Type") != "application/json" || got.Get("Accept") != "application/json" {
		t.Errorf("the headers are %v", got)
	}
	if method != http.MethodPost || path != pathStatements {
		t.Errorf("the request is %s %s", method, path)
	}
}
