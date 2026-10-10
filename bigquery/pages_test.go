package bigquery //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// bigQuery is the statement of the recording of a result of 3000 rows.
const bigQuery = "SELECT id, s FROM `dbimp-project`.dbimp_test.dbimp_it_big ORDER BY id"

// TestReplayPages holds D189 item 8 and D21 with the recording of a result of
// 3000 rows in six pages of 500: the driver reads the first page from the
// answer, and each later one with GET and the page token, in order and one at a
// time, and only when the caller has read the rows before it. Every call sends
// the location of the job and the size of a page.
func TestReplayPages(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	rows, err := db.QueryContext(t.Context(), bigQuery, WithMaxResults(500))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reading the first row: %v", rows.Err())
	}
	if got := s.requests(); len(got) != 1 {
		t.Errorf("the driver sent %v before the caller read a row of the second page, want the statement only", got)
	}
	n := 0
	for {
		var (
			id int64
			v  string
		)
		if err := rows.Scan(&id, &v); err != nil {
			t.Fatalf("scanning the row %d: %v", n, err)
		}
		if want := fmt.Sprintf("row%d", n+1); id != int64(n+1) || v != want {
			t.Fatalf("the row %d is %d and %q, want %d and %q", n, id, v, n+1, want)
		}
		n++
		if !rows.Next() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("after %d rows: %v", n, err)
	}
	if n != 3000 {
		t.Errorf("read %d rows, want 3000", n)
	}
	const (
		post = "POST /bigquery/v2/projects/dbimp-project/queries"
		get  = "GET /bigquery/v2/projects/dbimp-project/queries/job_I_Wb4l1v1Il1yjbF1Ipwn09A6RbP?pageToken"
	)
	if got, want := s.requests(), []string{post, get, get, get, get, get}; !slices.Equal(got, want) {
		t.Errorf("the requests are %v, want %v", got, want)
	}
	if s.most != 1 {
		t.Errorf("%d requests ran at once, want one at a time", s.most)
	}
	if body := s.bodyAt(t, 0); body["maxResults"] != float64(500) {
		t.Errorf("the statement holds maxResults %v, want 500", body["maxResults"])
	}
	for i, q := range s.queries()[1:] {
		if q.Get("location") != "US" || q.Get("maxResults") != "500" || q.Get("pageToken") == "" || q.Get("formatOptions.timestampOutputFormat") != "ISO8601_STRING" {
			t.Errorf("the request for the page %d has the query %v, want the location, the size, the token and the form of a timestamp", i+2, q)
		}
	}
}

// TestReplayOneAnswer holds that a result that the service sends whole in one
// answer, when the statement sets no maxResults, needs no second request.
func TestReplayOneAnswer(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	_, rows := readAll(t, db, bigQuery)
	if len(rows) != 3000 {
		t.Errorf("read %d rows, want 3000", len(rows))
	}
	if got := s.requests(); len(got) != 1 {
		t.Errorf("the requests are %v, want the statement only", got)
	}
}

// TestReplayPagesClosedEarly holds that rows that the caller closes in the
// first page fetch nothing more, and cancel nothing, because the job is done
// (D36 and D189).
func TestReplayPagesClosedEarly(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	rows, err := db.QueryContext(t.Context(), bigQuery, WithMaxResults(500))
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if !rows.Next() {
			t.Fatalf("reading a row: %v", rows.Err())
		}
	}
	//nolint:sqlclosecheck // The test closes the rows at a chosen point, and looks at the requests after it.
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got := s.requests(); len(got) != 1 {
		t.Errorf("the requests are %v, want the statement only", got)
	}
}

// TestReplayPageToken holds that a page token that the service does not accept
// is an error of the service, and that the driver sent the token as the
// service wrote it (recorded: bigquery-173).
func TestReplayPageToken(t *testing.T) {
	t.Parallel()
	ex := exchange(t, 173)
	e := newError(mustStatus(t, ex.Response.Status, string(ex.Response.Content())))
	if e.Reason != "invalid" || !strings.Contains(e.Message, "Invalid paging token") {
		t.Errorf("the error is %+v, want invalid with a message about the paging token", *e)
	}
}

// mustStatus makes the *dbimp.StatusError of a recorded response.
func mustStatus(tb testing.TB, code int, body string) *dbimp.StatusError {
	tb.Helper()
	return &dbimp.StatusError{Code: code, Body: body}
}

// TestPageFailures holds D21 and D107: a page after the first that fails, or
// that ends early, or that the service repeats, is an error from Rows.Next after
// the rows that came before it, and it wraps dbimp.ErrIncomplete.
func TestPageFailures(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		// page answers the request for the page n of the result, and returns
		// false to leave it to the recording.
		page func(w http.ResponseWriter, n int) bool
		want func(error) bool
		// rows is the count of rows that come before the error, and more is true
		// when up to 500 more can come, for a page that ends in the middle of a
		// row.
		rows int
		more bool
	}{
		{
			"the service refuses the page token",
			func(w http.ResponseWriter, n int) bool {
				if n == 2 {
					serve(w, exchange(t, 173))
					return true
				}
				return false
			},
			func(err error) bool { e, ok := errAs(err); return ok && e.Reason == "invalid" },
			1000,
			false,
		},
		{
			"the body of a page is cut",
			func(w http.ResponseWriter, n int) bool {
				if n == 2 {
					body := exchange(t, 168).Response.Content()
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body[:len(body)/2])
					return true
				}
				return false
			},
			func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) },
			1000,
			true,
		},
		{
			"the service repeats a page token",
			func(w http.ResponseWriter, n int) bool {
				if n >= 1 {
					serve(w, exchange(t, 167))
					return true
				}
				return false
			},
			func(err error) bool {
				return errors.Is(err, dbimp.ErrInvalidValue) && strings.Contains(err.Error(), "same page token")
			},
			1000,
			false,
		},
		{
			"the last page holds fewer rows than totalRows",
			func(w http.ResponseWriter, n int) bool {
				if n == 5 {
					// The recorded last page, with its rows cut to none.
					ex := exchange(t, 171)
					body := strings.Replace(string(ex.Response.Content()), `"rows"`, `"cut"`, 1)
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, body)
					return true
				}
				return false
			},
			func(err error) bool { return errors.Is(err, ErrCut) },
			2500,
			false,
		},
	} {
		s := &replayServer{t: t, set: hosted}
		pages := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				pages++
				if tt.page(w, pages) {
					return
				}
			}
			s.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		db := open(t, config(), srv.URL)
		n, err := countRows(t, db, bigQuery, WithMaxResults(500))
		if err == nil || !tt.want(err) {
			t.Errorf("%s: the error is %v", tt.name, err)
			continue
		}
		if !isIncomplete(err) {
			t.Errorf("%s: the error does not wrap dbimp.ErrIncomplete: %v", tt.name, err)
		}
		if n != tt.rows && (!tt.more || n <= tt.rows || n >= tt.rows+500) {
			t.Errorf("%s: %d rows came before the error, want %d", tt.name, n, tt.rows)
		}
	}
}

// countRows reads every row of a statement, and returns the count and the error
// of the rows.
func countRows(t *testing.T, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, query string, args ...any) (int, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}

// bigServer is a fake server for a result of pages that it writes while the
// driver reads them, so that the result never sits in memory in the test either.
type bigServer struct {
	pages, rows int
}

func (b *bigServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	page := 0
	if tok := r.URL.Query().Get("pageToken"); tok != "" {
		page, _ = strconv.Atoi(strings.TrimPrefix(tok, "page"))
	}
	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString(`{"kind":"bigquery#queryResponse","schema":{"fields":[{"name":"n","type":"INTEGER","mode":"NULLABLE"},{"name":"s","type":"STRING","mode":"NULLABLE"}]},"jobReference":{"projectId":"p","jobId":"job_1","location":"US"},"totalRows":"` +
		strconv.Itoa(b.pages*b.rows) + `",`)
	if page+1 < b.pages {
		_, _ = bw.WriteString(`"pageToken":"page` + strconv.Itoa(page+1) + `",`)
	}
	_, _ = bw.WriteString(`"rows":[`)
	pad := strings.Repeat("x", 80)
	for i := range b.rows {
		if i > 0 {
			_ = bw.WriteByte(',')
		}
		_, _ = bw.WriteString(`{"f":[{"v":"` + strconv.Itoa(page*b.rows+i) + `"},{"v":"` + pad + `"}]}`)
	}
	_, _ = bw.WriteString(`],"jobComplete":true}`)
	_ = bw.Flush()
}

// TestLargeResultHoldsLittleMemory holds D25: a result of three pages of 100000
// rows, about 35 MB of JSON, never sits in memory. The heap stays within 16 MiB
// of where it started while the rows are read.
func TestLargeResultHoldsLittleMemory(t *testing.T) { //nolint:paralleltest // The test reads the heap of the process, which another test changes.
	srv := httptest.NewServer(&bigServer{pages: 3, rows: 100000})
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL)
	rows, err := db.QueryContext(t.Context(), "SELECT n, s")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	runtime.GC()
	var base, now runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak uint64
	n := 0
	for rows.Next() {
		var (
			i int64
			s string
		)
		if err := rows.Scan(&i, &s); err != nil {
			t.Fatal(err)
		}
		if i != int64(n) {
			t.Fatalf("the row %d is %d", n, i)
		}
		if n++; n%50000 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&now)
			peak = max(peak, now.HeapAlloc)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("after %d rows: %v", n, err)
	}
	if n != 300000 {
		t.Errorf("read %d rows, want 300000", n)
	}
	if peak > base.HeapAlloc+16<<20 {
		t.Errorf("the heap grew by %d MiB while the driver read the rows, want less than 16 MiB (D25)", (peak-base.HeapAlloc)>>20)
	}
}
