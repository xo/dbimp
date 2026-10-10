package athena //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// The files of the recording that hold the statement of 2500 rows, "a result of
// 2500 rows": the start, the execution, and the three pages (recorded: "the
// second page", "the third page").
var pagedFiles = []int{195, 196, 197, 198, 199}

// TestReplayPages reads a result of 2500 rows across three pages, with the
// requests that the recording holds. Each page after the first names the token of
// the page before (D21 and D192).
func TestReplayPages(t *testing.T) {
	t.Parallel()
	f := newFake(t, sequence(t, false, pagedFiles...))
	db := open(t, testConfig(), f.srv.URL)
	rows, err := db.QueryContext(t.Context(), queryOf(t, 195))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		if n++; v != int64(n) {
			t.Fatalf("row %d holds %d: the header row was kept, or a page was skipped", n, v)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 2500 {
		t.Errorf("read %d rows, want 2500", n)
	}
	want := []string{"StartQueryExecution", "GetQueryExecution", "GetQueryResults", "GetQueryResults", "GetQueryResults"}
	if got := f.targets(); !slices.Equal(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	// Each request is the recorded one: the id of the query, and the token of the
	// page before.
	for i, r := range f.requests() {
		ex := exchange(t, pagedFiles[i])
		if !reflect.DeepEqual(bodyOf([]byte(r.body)), bodyOf(ex.Request.Content())) {
			t.Errorf("request %d is %s, want %s", i, r.body, ex.Request.Content())
		}
	}
}

// TestPagesAreReadOneAtATime holds that the driver fetches the next page only
// when the caller asks for the row after the last row of the page, and that rows
// closed before the end fetch no more (D36).
func TestPagesAreReadOneAtATime(t *testing.T) {
	t.Parallel()
	f := newFake(t, sequence(t, false, pagedFiles...))
	db := open(t, testConfig(), f.srv.URL)
	rows, err := db.QueryContext(t.Context(), queryOf(t, 195))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	// The header row and 999 rows are the first page (recorded: "a result of
	// 2500 rows").
	for range 999 {
		if !rows.Next() {
			t.Fatalf("the rows ended early: %v", rows.Err())
		}
	}
	if got := len(f.requests()); got != 3 {
		t.Errorf("after 999 rows the driver sent %d requests, want 3", got)
	}
	if !rows.Next() {
		t.Fatalf("the rows ended early: %v", rows.Err())
	}
	if got := len(f.requests()); got != 4 {
		t.Errorf("after the first row of the second page the driver sent %d requests, want 4: the next page comes when the caller asks for the row after the last row of the page", got)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(f.requests()); got != 4 {
		t.Errorf("closing the rows sent %d requests, want 4: Close reads and sends nothing (D36)", got)
	}
}

// TestReplayPageError ends a result with an error on its second page: the server
// refuses the token (recorded: "a wrong token of a page"). The rows of the first
// page reach the caller, and the error wraps dbimp.ErrIncomplete and carries the
// error of the server (D21 and D107).
func TestReplayPageError(t *testing.T) {
	t.Parallel()
	f := newFake(t, sequence(t, false, 195, 196, 197, 201))
	db := open(t, testConfig(), f.srv.URL)
	rows, err := db.QueryContext(t.Context(), queryOf(t, 195))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if n != 999 {
		t.Errorf("read %d rows before the error, want 999", n)
	}
	err = rows.Err()
	if !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want dbimp.ErrIncomplete", err)
	}
	var aerr *Error
	if !errors.As(err, &aerr) || aerr.Code != "INVALID_INPUT" || aerr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("the error is %v, want the INVALID_INPUT of the server with HTTP 400", err)
	}
}

// pagedServer answers a query whose result has pages pages of perPage rows, each
// row a number and a long text. It answers StartQueryExecution and
// GetQueryExecution with the recorded answers of a SELECT, and GetQueryResults
// with a page in the form of the server: NextToken, then ResultSet with the
// columns, the rows, and the same two again in the form that the SDK names.
func pagedServer(t *testing.T, pages, perPage int) *fake {
	t.Helper()
	start, execution := exchange(t, 17), exchange(t, 18)
	pad := strings.Repeat("x", 36)
	const cols = `[{"Name":"n","Type":"bigint","Precision":19,"Scale":0},{"Name":"s","Type":"varchar","Precision":2147483647,"Scale":0}]`
	return newFake(t, func(w http.ResponseWriter, r request, _ int) {
		switch r.target {
		case "StartQueryExecution":
			serve(w, start)
			return
		case "GetQueryExecution":
			serve(w, execution)
			return
		}
		var m struct{ NextToken string }
		_ = json.Unmarshal([]byte(r.body), &m)
		page := 0
		if m.NextToken != "" {
			_, _ = fmt.Sscan(m.NextToken, &page)
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		var err error
		write := func(s string) {
			if err == nil {
				_, err = io.WriteString(w, s)
			}
		}
		if page+1 < pages {
			write(fmt.Sprintf(`{"NextToken":"%d",`, page+1))
		} else {
			write(`{`)
		}
		write(`"ResultSet":{"ColumnInfos":` + cols + `,"ResultRows":[`)
		for i := range perPage {
			if i > 0 {
				write(",")
			}
			write(fmt.Sprintf(`{"Data":["%d","%s"]}`, page*perPage+i, pad))
		}
		write(`],"ResultSetMetadata":{"ColumnInfo":` + cols + `},"Rows":[`)
		for i := range perPage {
			if i > 0 {
				write(",")
			}
			write(fmt.Sprintf(`{"Data":[{"VarCharValue":"%d"},{"VarCharValue":"%s"}]}`, page*perPage+i, pad))
		}
		write(`]},"UpdateCount":0}`)
	})
}

// TestLargeResult holds D25: the driver reads a result of 64 MiB across pages one
// row at a time, and the memory in use stays far below the size of the result.
// It does not run in parallel, so that the memory of other tests does not count.
// A page of the server holds at most 1000 rows, and these pages hold more, so
// the test holds that the driver reads a page token by token too.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const (
		pages   = 8
		perPage = 1 << 16
	)
	f := pagedServer(t, pages, perPage)
	db := open(t, testConfig(), f.srv.URL)
	rows, err := db.QueryContext(t.Context(), "SELECT n, s FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var (
		n    int
		peak uint64
		v    int64
		s    string
	)
	for rows.Next() {
		if err := rows.Scan(&v, &s); err != nil {
			t.Fatal(err)
		}
		// The header row is dropped, so the first row is 1, as the server sends
		// it, and each value is the number of its row.
		if n++; v != int64(n) {
			t.Fatalf("row %d holds %d", n, v)
		}
		if n%(pages*perPage/16) == 0 {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
		}
	}
	if err := rows.Err(); err != nil || n != pages*perPage-1 {
		t.Fatalf("read %d rows and %v, want %d rows", n, err, pages*perPage-1)
	}
	if peak > base.HeapAlloc+16<<20 {
		t.Errorf("the heap grew by %d MiB while the driver read a result of 64 MiB, want less than 16 MiB (D25)", (peak-base.HeapAlloc)>>20)
	}
}

// TestPagesWithNoRowsAreSkipped holds that a page with no rows and a token is
// not the end of the result.
func TestPagesWithNoRowsAreSkipped(t *testing.T) {
	t.Parallel()
	start, execution := exchange(t, 17), exchange(t, 18)
	const cols = `[{"Name":"n","Type":"bigint","Precision":19,"Scale":0}]`
	f := newFake(t, func(w http.ResponseWriter, r request, _ int) {
		switch r.target {
		case "StartQueryExecution":
			serve(w, start)
		case "GetQueryExecution":
			serve(w, execution)
		default:
			w.Header().Set("Content-Type", "application/x-amz-json-1.1")
			switch {
			case !strings.Contains(r.body, "NextToken"):
				_, _ = io.WriteString(w, `{"NextToken":"a","ResultSet":{"ColumnInfos":`+cols+`,"ResultRows":[{"Data":["n"]}]}}`)
			case strings.Contains(r.body, `"a"`):
				_, _ = io.WriteString(w, `{"NextToken":"b","ResultSet":{"ColumnInfos":`+cols+`,"ResultRows":[]}}`)
			default:
				_, _ = io.WriteString(w, `{"ResultSet":{"ColumnInfos":`+cols+`,"ResultRows":[{"Data":["7"]}]}}`)
			}
		}
	})
	db := open(t, testConfig(), f.srv.URL)
	_, rows, err := read(t, db, "SELECT n FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"int64(7)"}}; !equalRows(rows, want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
}

// TestRowsHoldTheContextOnlyForPages holds D192 item 5: the rows keep the
// context for the pages after the first, so the rows of a context that ended
// fail on the next page with the error of the context.
func TestRowsHoldTheContextOnlyForPages(t *testing.T) {
	t.Parallel()
	f := newFake(t, sequence(t, false, pagedFiles...))
	db := open(t, testConfig(), f.srv.URL)
	ctx, cancel := context.WithCancel(t.Context())
	rows, err := db.QueryContext(ctx, queryOf(t, 195))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cancel()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v after %d rows, want context.Canceled", err, n)
	}
}
