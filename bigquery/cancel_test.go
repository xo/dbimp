package bigquery //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// slowJobPath is the path of the poll of the job of the slow statement of the
// recordings (recorded: bigquery-215).
const slowJobPath = "/bigquery/v2/projects/dbimp-project/queries/job_2oUOuvSXfy5PqbbN5ZzzA6h1NeND"

// slowQuery is the statement that runs longer than its wait on the service
// (recorded: bigquery-215).
const slowQuery = "SELECT COUNT(*) AS c FROM UNNEST(GENERATE_ARRAY(1, 1000000)) AS a CROSS JOIN UNNEST(GENERATE_ARRAY(1, 30000)) AS b"

// jobServer answers the statement of a job that runs with the recorded answer
// jobComplete false, and each poll of the job with the recorded answer of a poll
// that is not done, for the first running polls. Then it answers the poll with
// done, if it is not nil, and with the poll that is not done again if it is.
type jobServer struct {
	t       *testing.T
	running int
	// done is the recorded exchange that ends the polls, or nil to poll for ever.
	done *dbimptest.Exchange
	// rest answers the requests that follow, such as the pages of a result.
	rest http.Handler

	mu      sync.Mutex
	polls   int
	queries []url.Values
	cancels []url.Values
	log     []string
	seen    chan struct{}
	once    sync.Once
}

func newJobServer(t *testing.T, running int, done *dbimptest.Exchange, rest http.Handler) *jobServer {
	t.Helper()
	return &jobServer{t: t, running: running, done: done, rest: rest, seen: make(chan struct{})}
}

func (j *jobServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	j.mu.Lock()
	j.log = append(j.log, r.Method+" "+r.URL.Path)
	j.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/queries"):
		serve(w, exchange(j.t, 215))
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel"):
		j.mu.Lock()
		j.cancels = append(j.cancels, r.URL.Query())
		j.mu.Unlock()
		serve(w, exchange(j.t, 213))
	case r.Method == http.MethodGet && r.URL.Path == slowJobPath && r.URL.Query().Get("pageToken") == "":
		j.mu.Lock()
		j.polls++
		n := j.polls
		j.queries = append(j.queries, r.URL.Query())
		j.mu.Unlock()
		j.once.Do(func() { close(j.seen) })
		if n <= j.running || j.done == nil {
			serve(w, exchange(j.t, 217))
			return
		}
		serve(w, j.done)
	case j.rest != nil:
		j.rest.ServeHTTP(w, r)
	default:
		j.t.Errorf("the driver sent %s %s", r.Method, r.URL)
		http.Error(w, "no", http.StatusTeapot)
	}
}

// pollCount returns the count of the polls that the server got.
func (j *jobServer) pollCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.polls
}

// cancelQueries returns the query of each cancel that the server got.
func (j *jobServer) cancelQueries() []url.Values {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.cancels)
}

// TestPollsUntilTheJobEnds holds D189: a statement that is not done answers
// jobComplete false, and the driver polls the job with getQueryResults, with the
// location of the job, until the answer holds the schema and the first rows.
// The recorded answer of a finished poll is the first page of a result of 3000
// rows, which the driver then reads with the page tokens.
func TestPollsUntilTheJobEnds(t *testing.T) {
	t.Parallel()
	pages := &replayServer{t: t, set: hosted}
	rest := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The recorded pages name the job of the statement of 3000 rows.
		r.URL.Path = "/bigquery/v2/projects/dbimp-project/queries/job_I_Wb4l1v1Il1yjbF1Ipwn09A6RbP"
		pages.ServeHTTP(w, r)
	})
	j := newJobServer(t, 3, exchange(t, 166), rest)
	srv := httptest.NewServer(j)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL)
	n, err := countRows(t, db, slowQuery, WithMaxResults(500))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3000 {
		t.Errorf("read %d rows, want 3000", n)
	}
	if got := j.pollCount(); got != 4 {
		t.Errorf("the driver polled %d times, want 3 that were not done and one that was", got)
	}
	if got := j.cancelQueries(); len(got) != 0 {
		t.Errorf("the driver canceled a job that ended: %v", got)
	}
	// Each poll sends the location of the job, the wait and the size of a page.
	j.mu.Lock()
	defer j.mu.Unlock()
	for i, q := range j.queries {
		if q.Get("location") != "US" || q.Get("timeoutMs") != "5000" || q.Get("maxResults") != "500" || q.Get("formatOptions.timestampOutputFormat") != "ISO8601_STRING" {
			t.Errorf("the poll %d has the query %v, want the location, the wait, the size and the form of a timestamp", i+1, q)
		}
	}
}

// TestPollIsBounded holds that the wait between two polls grows up to a limit.
func TestPollIsBounded(t *testing.T) {
	t.Parallel()
	j := newJobServer(t, 5, exchange(t, 35), nil)
	srv := httptest.NewServer(j)
	t.Cleanup(srv.Close)
	var waits []time.Duration
	c := connector(config(), srv.URL)
	c.pollMin, c.pollMax = 10*time.Millisecond, 40*time.Millisecond
	c.wait = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return sleep(ctx, time.Microsecond)
	}
	db := openConnector(t, c)
	res, err := db.ExecContext(t.Context(), slowQuery)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Errorf("RowsAffected is %d, %v, want the count of the poll that ended", n, err)
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
	if !slices.Equal(waits, want) {
		t.Errorf("the waits are %v, want %v", waits, want)
	}
}

// TestCancelWhenTheContextEnds holds D36 and D189: when the context ends while
// the job runs, the driver sends jobs.cancel with the location of the job, once,
// and the error is the error of the context, never driver.ErrBadConn.
func TestCancelWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	j := newJobServer(t, 0, nil, nil)
	srv := httptest.NewServer(j)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		<-j.seen
		cancel()
	}()
	_, _, err := readAllContext(t, ctx, db, slowQuery)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if errors.Is(err, errBadConn) {
		t.Errorf("the error wraps driver.ErrBadConn: %v", err)
	}
	got := j.cancelQueries()
	if len(got) != 1 || got[0].Get("location") != "US" {
		t.Errorf("the driver sent the cancels %v, want one with the location US", got)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if !slices.Contains(j.log, "POST /bigquery/v2/projects/dbimp-project/jobs/job_2oUOuvSXfy5PqbbN5ZzzA6h1NeND/cancel") {
		t.Errorf("the requests are %v, want the cancel of the job", j.log)
	}
}

// TestCancelAfterTheDeadline holds the same for the other end of a context, a
// deadline.
func TestCancelAfterTheDeadline(t *testing.T) {
	t.Parallel()
	j := newJobServer(t, 0, nil, nil)
	srv := httptest.NewServer(j)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	_, _, err := readAllContext(t, ctx, db, slowQuery)
	if !isDeadline(err) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	if got := j.cancelQueries(); len(got) != 1 {
		t.Errorf("the driver sent %d cancels after the deadline, want 1", len(got))
	}
}

// TestCanceledByAnotherClient holds that a job that someone else canceled ends
// the poll with ErrCanceled, and the driver sends no cancel of its own, because
// its context lives (recorded: bigquery-212).
func TestCanceledByAnotherClient(t *testing.T) {
	t.Parallel()
	j := newJobServer(t, 0, exchange(t, 212), nil)
	// The poll answers the 499 at once, after one poll that is not done.
	j.running = 1
	srv := httptest.NewServer(j)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL)
	_, _, err := readAllContext(t, t.Context(), db, slowQuery)
	if !errors.Is(err, ErrCanceled) {
		t.Errorf("the error is %v, want ErrCanceled", err)
	}
	e, ok := errAs(err)
	if !ok || e.HTTPStatus != 499 || e.JobID != "job_2oUOuvSXfy5PqbbN5ZzzA6h1NeND" {
		t.Errorf("the error is %+v, want HTTP 499 with the id of the job", e)
	}
	if got := j.cancelQueries(); len(got) != 0 {
		t.Errorf("the driver canceled a job that was canceled: %v", got)
	}
}

// TestNoRedirect holds D189 item 10: the driver follows no redirect, so the token
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
	db := open(t, config(), srv.URL)
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	e, ok := errAs(err)
	if !ok || e.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("the error is %v, want HTTP 307", err)
	}
}

// TestNoGoroutinesLeft holds step 12: after a poll, a cancel and a close, no
// goroutine of the driver is left running.
func TestNoGoroutinesLeft(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	j := newJobServer(t, 0, nil, nil)
	srv := httptest.NewServer(j)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := readAllContext(t, ctx, db, slowQuery); err == nil {
		t.Error("a job that never ends gave no error")
	}
}

// errBadConn is the error that a cancelled context never wraps.
var errBadConn = driver.ErrBadConn
