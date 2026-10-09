package snowflake //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp"
)

// TestReplayPartitions holds D183 item 8 with the recording of a result of 20000
// rows in four partitions of 2063, 4682, 9404 and 3851 rows: the driver reads
// the first partition from the answer and each later one with GET, in order, one
// at a time, and only when the caller has read the rows before it. The server
// compresses a later partition with gzip, as it does (measured).
func TestReplayPartitions(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	rows, err := db.QueryContext(t.Context(), "SELECT seq4() AS n, uuid_string() AS u FROM TABLE(GENERATOR(ROWCOUNT => 20000))")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reading the first row: %v", rows.Err())
	}
	if got := s.requests(); len(got) != 1 {
		t.Errorf("the driver sent %v before the caller read a row of the second partition, want the statement only", got)
	}
	n := 0
	for {
		var (
			i int64
			u string
		)
		if err := rows.Scan(&i, &u); err != nil {
			t.Fatalf("scanning the row %d: %v", n, err)
		}
		if i != int64(n) || len(u) != 36 {
			t.Fatalf("the row %d is %d and %q, want %d and a UUID", n, i, u, n)
		}
		n++
		if !rows.Next() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("after %d rows: %v", n, err)
	}
	if n != 20000 {
		t.Errorf("read %d rows, want 20000", n)
	}
	base := "GET /api/v2/statements/01c79a10-0001-ac43-0000-d07900024c6a?partition="
	if got, want := s.requests(), []string{"POST /api/v2/statements", base + "1", base + "2", base + "3"}; !slices.Equal(got, want) {
		t.Errorf("the requests are %v, want %v", got, want)
	}
	if s.most != 1 {
		t.Errorf("%d requests ran at once, want one at a time", s.most)
	}
}

// TestReplayPartitionsClosedEarly holds that rows that the caller closes in
// the first partition fetch nothing more, and cancel the statement (D183).
func TestReplayPartitionsClosedEarly(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	rows, err := db.QueryContext(t.Context(), "SELECT seq4() AS n, uuid_string() AS u FROM TABLE(GENERATOR(ROWCOUNT => 20000))")
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
	want := []string{"POST /api/v2/statements", "POST /api/v2/statements/01c79a10-0001-ac43-0000-d07900024c6a/cancel"}
	if got := s.requests(); !slices.Equal(got, want) {
		t.Errorf("the requests are %v, want %v", got, want)
	}
}

// pagedServer is a fake server for a result in partitions that the test
// writes. The first answer holds the first partition, and each GET holds the
// body of its partition, or the status that the test sets.
type pagedServer struct {
	t *testing.T
	// first is the body of the answer of the statement.
	first string
	// link is the header Link of the first answer.
	link string
	// partitions holds the body of each later partition, and status the status
	// of it, 200 by default.
	partitions []string
	status     map[int]int

	mu   sync.Mutex
	gets []string
}

func (p *pagedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodPost {
		if p.link != "" {
			w.Header().Set("Link", p.link)
		}
		_, _ = io.WriteString(w, p.first)
		return
	}
	n, err := strconv.Atoi(r.URL.Query().Get("partition"))
	p.mu.Lock()
	p.gets = append(p.gets, r.URL.Path+"?"+r.URL.RawQuery)
	p.mu.Unlock()
	if err != nil || n < 1 || n > len(p.partitions) {
		p.t.Errorf("the driver asked for the partition %q", r.URL.RawQuery)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if st := p.status[n]; st != 0 {
		w.WriteHeader(st)
	}
	_, _ = io.WriteString(w, p.partitions[n-1])
}

// pagedHead is the start of an answer with one column, and the counts of the
// rows of each partition.
func pagedHead(counts ...int) string {
	parts := make([]string, len(counts))
	for i, n := range counts {
		parts[i] = fmt.Sprintf(`{"rowCount":%d}`, n)
	}
	return `{"resultSetMetaData":{"format":"jsonv2","partitionInfo":[` + strings.Join(parts, ",") + `],"rowType":[{"name":"N","type":"fixed","precision":10,"scale":0,"nullable":false}]},"data":[`
}

// pagedRows returns the rows from first to last, as the array of data.
func pagedRows(first, last int) string {
	var rows []string
	for i := first; i <= last; i++ {
		rows = append(rows, fmt.Sprintf(`["%d"]`, i))
	}
	return strings.Join(rows, ",")
}

const (
	handleLinkText = `</api/v2/statements/H1?requestId=a&partition=0>; rel="first",</api/v2/statements/H1?requestId=b&partition=1>; rel="next"`
	tailWithHandle = `],"code":"090001","statementHandle":"H1","message":"Statement executed successfully."}`
)

// TestPartitionFailures holds D183 item 8: an error in a later partition, and a
// partition with the wrong count of rows, reach the caller after the rows of
// the partitions before it, and wrap dbimp.ErrIncomplete (D107).
func TestPartitionFailures(t *testing.T) {
	t.Parallel()
	good := func(first, last int) string { return `{"data":[` + pagedRows(first, last) + `]}` }
	for _, tt := range []struct {
		name  string
		p     *pagedServer
		rows  int
		check func(*testing.T, error)
	}{
		{
			name: "a partition that the server refuses",
			p: &pagedServer{
				first:      pagedHead(2, 2, 2) + pagedRows(1, 2) + tailWithHandle,
				link:       handleLinkText,
				partitions: []string{good(3, 4), `{"code":"391922","message":"Invalid partition parameter.","sqlState":"22023","statementHandle":"H1"}`},
				status:     map[int]int{2: http.StatusBadRequest},
			},
			rows: 4,
			check: func(t *testing.T, err error) {
				t.Helper()
				if e := serverError(t, err); e.Code != "391922" || e.HTTPStatus != http.StatusBadRequest {
					t.Errorf("the error is %+v", *e)
				}
			},
		},
		{
			name: "a partition with fewer rows",
			p: &pagedServer{
				first:      pagedHead(2, 3) + pagedRows(1, 2) + tailWithHandle,
				link:       handleLinkText,
				partitions: []string{good(3, 4)},
			},
			rows: 4,
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, ErrCut) {
					t.Errorf("the error is %v, want ErrCut", err)
				}
			},
		},
		{
			name: "a partition with more rows",
			p: &pagedServer{
				first:      pagedHead(2, 1) + pagedRows(1, 2) + tailWithHandle,
				link:       handleLinkText,
				partitions: []string{good(3, 4)},
			},
			rows: 4,
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, dbimp.ErrInvalidValue) {
					t.Errorf("the error is %v, want dbimp.ErrInvalidValue", err)
				}
			},
		},
		{
			name: "a partition that ends in the middle",
			p: &pagedServer{
				first:      pagedHead(2, 3) + pagedRows(1, 2) + tailWithHandle,
				link:       handleLinkText,
				partitions: []string{`{"data":[["3"],["4"],["5"`},
			},
			rows: 4,
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("the error is %v, want io.ErrUnexpectedEOF", err)
				}
			},
		},
		{
			name: "a partition that is not an object",
			p: &pagedServer{
				first:      pagedHead(2, 2) + pagedRows(1, 2) + tailWithHandle,
				link:       handleLinkText,
				partitions: []string{`[["3"]]`},
			},
			rows: 2,
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, dbimp.ErrInvalidValue) {
					t.Errorf("the error is %v, want dbimp.ErrInvalidValue", err)
				}
			},
		},
		{
			name: "a partition with no handle to fetch it by",
			p: &pagedServer{
				first:      pagedHead(2, 2) + pagedRows(1, 2) + `],"code":"090001"}`,
				partitions: []string{good(3, 4)},
			},
			rows: 2,
			check: func(t *testing.T, err error) {
				t.Helper()
				if !errors.Is(err, dbimp.ErrInvalidValue) {
					t.Errorf("the error is %v, want dbimp.ErrInvalidValue", err)
				}
			},
		},
	} {
		tt.p.t = t
		srv := httptest.NewServer(tt.p)
		t.Cleanup(srv.Close)
		db := open(t, config(), srv.URL, true)
		rows, err := db.QueryContext(t.Context(), "SELECT n")
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		n := 0
		for rows.Next() {
			n++
		}
		err = rows.Err()
		//nolint:sqlclosecheck // The loop of cases closes the rows of each one when its rows end.
		rows.Close()
		if err == nil {
			t.Errorf("%s: no error after %d rows", tt.name, n)
			continue
		}
		if n != tt.rows {
			t.Errorf("%s: %d rows before the error, want %d", tt.name, n, tt.rows)
		}
		if !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: the error %v does not wrap dbimp.ErrIncomplete (D107)", tt.name, err)
		}
		tt.check(t, err)
	}
}

// TestHandleFromTheAnswer holds that the driver takes the handle from the
// members after the rows when the answer has no header Link, and fetches each
// later partition by it.
func TestHandleFromTheAnswer(t *testing.T) {
	t.Parallel()
	p := &pagedServer{
		t:          t,
		first:      pagedHead(2, 2) + pagedRows(1, 2) + tailWithHandle,
		partitions: []string{`{"data":[` + pagedRows(3, 4) + `]}`},
	}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, true)
	_, got, err := readAll(t, db, "SELECT n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[3][0] != int64(4) {
		t.Errorf("the rows are %v, want 4 rows that end in 4", got)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if want := []string{"/api/v2/statements/H1?partition=1"}; !slices.Equal(p.gets, want) {
		t.Errorf("the requests for partitions are %v, want %v", p.gets, want)
	}
}

// bigServer writes a result of partitions of rows that it makes while the
// driver reads, so that the test holds none of it.
type bigServer struct {
	partitions, rows int
}

const bigValue = "0123456789012345678901234567890123456789"

func (b *bigServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	n := 0
	if r.Method == http.MethodGet {
		n, _ = strconv.Atoi(r.URL.Query().Get("partition"))
	} else {
		w.Header().Set("Link", handleLinkText)
	}
	counts := make([]int, b.partitions)
	for i := range counts {
		counts[i] = b.rows
	}
	if n == 0 {
		_, _ = io.WriteString(w, `{"resultSetMetaData":{"format":"jsonv2","partitionInfo":[`)
		for i, c := range counts {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = fmt.Fprintf(w, `{"rowCount":%d}`, c)
		}
		_, _ = io.WriteString(w, `],"rowType":[{"name":"N","type":"fixed","precision":10,"scale":0},{"name":"S","type":"text","length":40}]},"data":[`)
	} else {
		_, _ = io.WriteString(w, `{"data":[`)
	}
	for i := range b.rows {
		if i > 0 {
			_, _ = io.WriteString(w, ",")
		}
		_, _ = fmt.Fprintf(w, `["%d","%s"]`, i, bigValue)
	}
	if n == 0 {
		_, _ = io.WriteString(w, tailWithHandle)
		return
	}
	_, _ = io.WriteString(w, "]}")
}

// TestLargeResultHoldsLittleMemory holds D25: a result of three partitions of
// 250000 rows, about 60 MB of JSON, never sits in memory. The heap stays
// within 16 MiB of where it started while the rows are read.
func TestLargeResultHoldsLittleMemory(t *testing.T) { //nolint:paralleltest // The test reads the heap of the process, which another test changes.
	srv := httptest.NewServer(&bigServer{partitions: 3, rows: 250000})
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, true)
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
		if n++; n%50000 == 0 {
			runtime.GC()
			runtime.ReadMemStats(&now)
			peak = max(peak, now.HeapAlloc)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("after %d rows: %v", n, err)
	}
	if n != 750000 {
		t.Errorf("read %d rows, want 750000", n)
	}
	if peak > base.HeapAlloc+16<<20 {
		t.Errorf("the heap grew by %d MiB while the driver read the rows, want less than 16 MiB (D25)", (peak-base.HeapAlloc)>>20)
	}
}
