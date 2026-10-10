package elasticsearch_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/elasticsearch"
)

// numbers reads the first column of every row of SELECT a FROM t as an int64,
// and returns the numbers and the error of the rows.
func numbers(t *testing.T, db *sql.DB) ([]int64, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// threePages answers the first request with the numbers 1 and 2 and the
// cursor c1, the cursor c1 with the numbers 3 and 4 and the cursor c2, and
// the cursor c2 with the number 5 and no cursor (measured: the first page has
// columns, a later page has none, and the last page has no cursor).
func threePages(w http.ResponseWriter, _ *http.Request, m map[string]any) {
	switch m["cursor"] {
	case nil:
		reply(w, http.StatusOK, page(true, 1, 2, "c1"))
	case "c1":
		reply(w, http.StatusOK, page(false, 3, 4, "c2"))
	case "c2":
		reply(w, http.StatusOK, page(false, 5, 5, ""))
	default:
		reply(w, http.StatusNotFound, `{"error":{"root_cause":[{"type":"search_context_missing_exception","reason":"No search context found"}],"type":"search_context_missing_exception","reason":"No search context found"},"status":404}`)
	}
}

// TestPages holds D167: the driver follows the cursor to the last page, with
// a request that holds the cursor and no query, and closes no cursor of a
// result that it read to its end.
func TestPages(t *testing.T) {
	t.Parallel()
	f := &fake{handle: threePages}
	db := f.open(t, "", "?fetch_size=2")
	got, err := numbers(t, db)
	if err != nil || !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("read %v and %v, want 1 to 5", got, err)
	}
	reqs := f.requests()
	if len(reqs) != 3 {
		t.Fatalf("the server received %d requests, want 3", len(reqs))
	}
	if reqs[0]["query"] != "SELECT a FROM t" || reqs[0]["fetch_size"] != float64(2) || reqs[0]["cursor"] != nil {
		t.Errorf("the first request is %v, want the query and fetch_size 2", reqs[0])
	}
	for i, want := range []string{"c1", "c2"} {
		next := reqs[i+1]
		if next["cursor"] != want || next["query"] != nil || next["fetch_size"] != nil || len(next) != 1 {
			t.Errorf("the request for page %d is %v, want only the cursor %s", i+2, next, want)
		}
	}
	if c := f.closed(); len(c) != 0 {
		t.Errorf("the driver closed the cursors %v of a result that it read to its end", c)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, q := range f.queries {
		if q != "format=json" {
			t.Errorf("the request %d has the query %q, want format=json", i+1, q)
		}
	}
}

// TestPagesKeepTheSettings holds that the request for a next page keeps the
// settings that the statement set, which the cursor does not hold: the
// leniency and the time that the server gives it.
func TestPagesKeepTheSettings(t *testing.T) {
	t.Parallel()
	f := &fake{handle: threePages}
	db := f.open(t, "", "?field_multi_value_leniency=true")
	ctx := elasticsearch.WithOptions(t.Context(), elasticsearch.WithTimeout(1500*time.Millisecond))
	rows, err := db.QueryContext(ctx, "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	for i, req := range reqs {
		if req["field_multi_value_leniency"] != true || req["request_timeout"] != "1500ms" {
			t.Errorf("the request %d is %v, want the leniency and request_timeout 1500ms", i+1, req)
		}
	}
}

// TestCloseBeforeTheEnd holds D167: rows that the caller closes before the
// end close the cursor on the server. The cursor follows the rows of a page,
// so Close reads the rest of the page to learn it.
func TestCloseBeforeTheEnd(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		read int
		want []string
	}{
		{"in the middle of the first page", 1, []string{"c1"}},
		{"after the last row of the first page", 2, []string{"c1"}},
		{"in the middle of the second page", 3, []string{"c2"}},
		{"in the last page", 5, nil},
	} {
		f := &fake{handle: threePages}
		db := f.open(t, "", "?fetch_size=2")
		rows, err := db.QueryContext(t.Context(), "SELECT a FROM t")
		if err != nil {
			t.Fatal(err)
		}
		for range tt.read {
			if !rows.Next() {
				t.Fatalf("%s: no row: %v", tt.name, rows.Err())
			}
		}
		if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test closes the rows early on purpose, and holds the error and the cursor.
			t.Errorf("%s: closing the rows: %v", tt.name, err)
		}
		if got := f.closed(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: the driver closed the cursors %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestCloseOfALargePage holds that Close reads at most 256 KiB of the rest of
// a page to find the cursor, and leaves the cursor to the server when the
// page is larger (D36 and D167).
func TestCloseOfALargePage(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"columns":[{"name":"a","type":"long"}],"rows":[`)
		for i := range 200000 {
			sep := ","
			if i == 0 {
				sep = ""
			}
			if _, err := io.WriteString(w, sep+"[1]"); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `],"cursor":"big"}`)
	}}
	db := f.open(t, "", "")
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test closes the rows early on purpose, and holds the error and the cursor.
		t.Errorf("closing the rows: %v", err)
	}
	if got := f.closed(); len(got) != 0 {
		t.Errorf("the driver closed %v after a page that is larger than the bound, want none", got)
	}
}

// TestErrorOnALaterPage holds D167 and D107: a page that the server refuses
// ends the rows with an error that wraps dbimp.ErrIncomplete, and keeps the
// error of the server. The error of the first page does not wrap it.
func TestErrorOnALaterPage(t *testing.T) {
	t.Parallel()
	refuse := func(w http.ResponseWriter, _ *http.Request, m map[string]any) {
		if m["cursor"] == nil {
			reply(w, http.StatusOK, page(true, 1, 2, "c1"))
			return
		}
		reply(w, http.StatusBadRequest, `{"error":{"root_cause":[{"type":"invalid_argument_exception","reason":"Arrays (returned by [i]) are not supported"}],"type":"invalid_argument_exception","reason":"Arrays (returned by [i]) are not supported"},"status":400}`)
	}
	f := &fake{handle: refuse}
	db := f.open(t, "", "")
	got, err := numbers(t, db)
	if !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("read %v before the error, want 1 and 2", got)
	}
	e, ok := errors.AsType[*elasticsearch.Error](err)
	switch {
	case !errors.Is(err, dbimp.ErrIncomplete):
		t.Errorf("the error is %v, want one that wraps dbimp.ErrIncomplete (D107)", err)
	case !ok || e.HTTPStatus != http.StatusBadRequest || e.Type != "invalid_argument_exception" || !strings.Contains(e.Reason, "Arrays"):
		t.Errorf("the error is %v, want the error of HTTP 400 with invalid_argument_exception", err)
	case errors.Is(err, driver.ErrBadConn):
		t.Errorf("the error of a page wraps driver.ErrBadConn, and database/sql would run the statement again (D8)")
	}
}

// TestErrorBeforeRows holds that an error of the first request comes from
// QueryContext, and does not wrap dbimp.ErrIncomplete.
func TestErrorBeforeRows(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusBadRequest, `{"error":{"root_cause":[{"type":"parsing_exception","reason":"line 1:1: mismatched input 'SELEC'"}],"type":"parsing_exception","reason":"line 1:1: mismatched input 'SELEC'"},"status":400}`)
	}}
	db := f.open(t, "", "")
	_, err := db.ExecContext(t.Context(), "SELEC 1")
	e, ok := errors.AsType[*elasticsearch.Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Type != "parsing_exception" || errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want the error of HTTP 400 with parsing_exception and no dbimp.ErrIncomplete", err)
	}
}

// TestCutPage holds that a page that ends before its end is an error, with
// dbimp.ErrIncomplete after a row.
func TestCutPage(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"columns":[{"name":"a","type":"long"}],"rows":[[1],[2`)
	}}
	db := f.open(t, "", "")
	got, err := numbers(t, db)
	if !errors.Is(err, dbimp.ErrIncomplete) || !slices.Equal(got, []int64{1}) {
		t.Errorf("read %v and %v, want the number 1 and dbimp.ErrIncomplete", got, err)
	}
}

// TestBadPages holds the errors for an answer that is not a page.
func TestBadPages(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		body string
		want error
	}{
		{"no columns", `{"rows":[[1]]}`, dbimp.ErrInvalidValue},
		{"no rows", `{"columns":[{"name":"a","type":"long"}]}`, dbimp.ErrInvalidValue},
		{"a row of the wrong size", `{"columns":[{"name":"a","type":"long"}],"rows":[[1,2]]}`, dbimp.ErrColumnCount},
		{"an error with HTTP 200", `{"error":{"type":"x_exception","reason":"boom"}}`, nil},
		{"a value of the wrong kind", `{"columns":[{"name":"a","type":"long"}],"rows":[["x"]]}`, dbimp.ErrInvalidValue},
	} {
		f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			reply(w, http.StatusOK, tt.body)
		}}
		db := f.open(t, "", "")
		_, err := numbers(t, db)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", tt.name, err, tt.want)
		}
		if _, ok := errors.AsType[*elasticsearch.Error](err); tt.want == nil && !ok {
			t.Errorf("%s: the error is %v, want an *elasticsearch.Error", tt.name, err)
		}
	}
}

// TestCancel holds D167: a context that ends while the driver reads stops the
// read, and the error is the error of the context. The driver closes no
// cursor, because the cursor follows the rows that it did not read.
func TestCancel(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"columns":[{"name":"a","type":"long"}],"rows":[[1]`)
		_ = http.NewResponseController(w).Flush()
		<-r.Context().Done()
	}}
	db := f.open(t, "", "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	cancel()
	for rows.Next() {
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if c := f.closed(); len(c) != 0 {
		t.Errorf("the driver closed the cursors %v", c)
	}
}

// TestCancelBeforeTheAnswer holds that a context that ends while the server
// works gives the error of the context, and never driver.ErrBadConn.
func TestCancelBeforeTheAnswer(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	f := &fake{handle: func(_ http.ResponseWriter, r *http.Request, _ map[string]any) {
		close(started)
		<-r.Context().Done()
	}}
	db := f.open(t, "", "")
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()
	_, err := db.ExecContext(ctx, "SELECT a FROM t")
	if !errors.Is(err, context.Canceled) || errors.Is(err, driver.ErrBadConn) {
		t.Errorf("the error is %v, want context.Canceled and not driver.ErrBadConn (D36)", err)
	}
}

// TestRedirect holds D167: the driver follows no redirect, so it sends the
// credentials to the host of the DSN only, and a redirect is an error.
func TestRedirect(t *testing.T) {
	t.Parallel()
	var other sync.Mutex
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		other.Lock()
		reached = true
		other.Unlock()
	}))
	t.Cleanup(target.Close)
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		http.Redirect(w, r, target.URL+"/_sql", http.StatusTemporaryRedirect)
	}}
	db := f.open(t, "u:secret@", "")
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if e, ok := errors.AsType[*elasticsearch.Error](err); !ok || e.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("a redirect gave %v, want the error of HTTP 307", err)
	}
	other.Lock()
	defer other.Unlock()
	if reached {
		t.Error("the driver followed the redirect to another host")
	}
}

// TestLargeResult holds D25: the driver reads a result of 64 MiB in pages,
// one row at a time, and the memory in use stays far below the size of the
// result. It does not run in parallel, so that the memory of other tests
// does not count.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const (
		pageRows = 8192
		pages    = 128
	)
	row := `["` + strings.Repeat("x", 56) + `"]`
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, m map[string]any) {
		n := 0
		if c, ok := m["cursor"].(string); ok {
			if _, err := fmt.Sscanf(c, "p%d", &n); err != nil {
				reply(w, http.StatusBadRequest, `{"error":"bad cursor"}`)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if n == 0 {
			_, _ = io.WriteString(w, `{"columns":[{"name":"s","type":"keyword"}],`)
		} else {
			_, _ = io.WriteString(w, "{")
		}
		_, _ = io.WriteString(w, `"rows":[`)
		for i := range pageRows {
			sep := ","
			if i == 0 {
				sep = ""
			}
			if _, err := io.WriteString(w, sep+row); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, "]")
		if n+1 < pages {
			_, _ = fmt.Fprintf(w, `,"cursor":"p%d"`, n+1)
		}
		_, _ = io.WriteString(w, "}")
	}}
	db := f.open(t, "", "")
	rows, err := db.QueryContext(t.Context(), "SELECT s FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	gauge := dbimptest.NewHeapGauge()
	var (
		n int
		s string
	)
	for rows.Next() {
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		if n++; n%(pageRows*pages/4) == 0 {
			gauge.Sample()
		}
	}
	if err := rows.Err(); err != nil || n != pageRows*pages {
		t.Fatalf("read %d rows and %v, want %d rows", n, err, pageRows*pages)
	}
	gauge.Check(t)
}
