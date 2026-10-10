package opensearch_test

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

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/opensearch"
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
// the cursor c2 with the number 5 and no cursor (measured: a page of the new
// engine has the schema again, and the last page has no cursor).
func threePages(w http.ResponseWriter, _ *http.Request, m map[string]any) {
	switch m["cursor"] {
	case nil:
		reply(w, http.StatusOK, page(1, 2, "n:c1"))
	case "n:c1":
		reply(w, http.StatusOK, page(3, 4, "n:c2"))
	case "n:c2":
		reply(w, http.StatusOK, page(5, 5, ""))
	default:
		reply(w, http.StatusNotFound, notFound)
	}
}

// notFound is the answer for a cursor that is read already (recorded: "a page
// that was read already").
const notFound = `{"error":{"reason":"Error occurred in OpenSearch engine: all shards failed","details":"Shard[0]: SearchContextMissingException[No search context found for id [1]]","type":"SearchContextMissingException"},"status":404}`

// TestPages holds D168: the driver follows the cursor to the last page, with
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
	for i, want := range []string{"n:c1", "n:c2"} {
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
		if q != "/_plugins/_sql?" {
			t.Errorf("the request %d went to %q, want the path of the SQL plugin and no query", i+1, q)
		}
	}
}

// TestEachCursorServesOnce holds that the driver sends each cursor once, and
// never reads a page again, because the server serves a cursor once (measured).
func TestEachCursorServesOnce(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	served := map[any]int{}
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, m map[string]any) {
		mu.Lock()
		served[m["cursor"]]++
		n := served[m["cursor"]]
		mu.Unlock()
		if n > 1 {
			reply(w, http.StatusNotFound, notFound)
			return
		}
		threePages(w, r, m)
	}}
	db := f.open(t, "", "?fetch_size=2")
	got, err := numbers(t, db)
	if err != nil || !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("read %v and %v, want 1 to 5", got, err)
	}
}

// TestCursorBeforeTheRows holds that the driver reads a page of the legacy
// engine: the cursor comes before the rows, a later page has no schema, and
// the last page has no cursor (recorded: "a cursor with a limit" and "the
// second page of a cursor with a limit").
func TestCursorBeforeTheRows(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, m map[string]any) {
		switch m["cursor"] {
		case nil:
			reply(w, http.StatusOK, `{"schema":[{"name":"a","type":"long"}],"cursor":"d:c1","total":5,"datarows":[[1],[2]],"size":2,"status":200}`)
		case "d:c1":
			reply(w, http.StatusOK, `{"cursor":"d:c2","datarows":[[3],[4]]}`)
		case "d:c2":
			reply(w, http.StatusOK, `{"datarows":[[5]]}`)
		default:
			reply(w, http.StatusNotFound, notFound)
		}
	}}
	db := f.open(t, "", "")
	got, err := numbers(t, db)
	if err != nil || !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Fatalf("read %v and %v, want 1 to 5", got, err)
	}
	if c := f.closed(); len(c) != 0 {
		t.Errorf("the driver closed %v after the end", c)
	}
}

// TestCloseBeforeTheEnd holds D168: rows that the caller closes before the
// end close the cursor on the server. The cursor follows the rows of a page,
// so Close reads the rest of the page to learn it.
func TestCloseBeforeTheEnd(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		read int
		want []string
	}{
		{"in the middle of the first page", 1, []string{"n:c1"}},
		{"after the last row of the first page", 2, []string{"n:c1"}},
		{"in the middle of the second page", 3, []string{"n:c2"}},
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

// TestCloseWithTheCursorBeforeTheRows holds that Close closes a cursor that
// the legacy engine wrote before the rows, with no read of the page.
func TestCloseWithTheCursorBeforeTheRows(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"schema":[{"name":"a","type":"long"}],"cursor":"d:c1","total":5,"datarows":[[1],[2]],"size":2,"status":200}`)
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
	if got := f.closed(); !slices.Equal(got, []string{"d:c1"}) {
		t.Errorf("the driver closed %v, want d:c1", got)
	}
}

// TestCloseOfALargePage holds that Close reads at most 256 KiB of the rest of
// a page to find the cursor, and leaves the cursor to the server when the
// page is larger (D36 and D168).
func TestCloseOfALargePage(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"schema":[{"name":"a","type":"long"}],"datarows":[`)
		for i := range 200000 {
			sep := ","
			if i == 0 {
				sep = ""
			}
			if _, err := io.WriteString(w, sep+"[1]"); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `],"total":200000,"size":200000,"status":200,"cursor":"n:big"}`)
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

// TestErrorOnALaterPage holds D168 and D107: a page that the server refuses
// ends the rows with an error that wraps dbimp.ErrIncomplete, and keeps the
// error of the server (recorded: "the second page that fails"). The error of
// the first page does not wrap it.
func TestErrorOnALaterPage(t *testing.T) {
	t.Parallel()
	refuse := func(w http.ResponseWriter, _ *http.Request, m map[string]any) {
		if m["cursor"] == nil {
			reply(w, http.StatusOK, page(1, 2, "n:c1"))
			return
		}
		reply(w, http.StatusBadRequest, `{"error":{"reason":"Invalid SQL query","details":"For input string: \"x121\"","type":"NumberFormatException"},"status":400}`)
	}
	f := &fake{handle: refuse}
	db := f.open(t, "", "")
	got, err := numbers(t, db)
	if !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("read %v before the error, want 1 and 2", got)
	}
	e, ok := errors.AsType[*opensearch.Error](err)
	switch {
	case !errors.Is(err, dbimp.ErrIncomplete):
		t.Errorf("the error is %v, want one that wraps dbimp.ErrIncomplete (D107)", err)
	case !ok || e.HTTPStatus != http.StatusBadRequest || e.Status != http.StatusBadRequest || e.Type != "NumberFormatException":
		t.Errorf("the error is %v, want the error of HTTP 400 with NumberFormatException", err)
	case errors.Is(err, driver.ErrBadConn):
		t.Errorf("the error of a page wraps driver.ErrBadConn, and database/sql runs the statement again (D8)")
	}
}

// TestErrorWithHTTP200 holds D168: an error that arrives with HTTP 200 reads
// its status from the body, before any row and on a later page (recorded: "a
// union" and "a statement with a filter of the query DSL").
func TestErrorWithHTTP200(t *testing.T) {
	t.Parallel()
	const body = `{"error":{"reason":"There was internal problem at backend","details":"The following method is not supported in Schema: MULTI_VALUE","type":"UnsupportedOperationException"},"status":500}`
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, m map[string]any) {
		if m["cursor"] != nil {
			reply(w, http.StatusOK, body)
			return
		}
		if strings.Contains(fmt.Sprint(m["query"]), "UNION") {
			reply(w, http.StatusOK, body)
			return
		}
		reply(w, http.StatusOK, page(1, 2, "n:c1"))
	}}
	db := f.open(t, "", "")
	_, err := db.ExecContext(t.Context(), "SELECT a FROM t UNION SELECT a FROM u")
	e, ok := errors.AsType[*opensearch.Error](err)
	if !ok || e.HTTPStatus != http.StatusOK || e.Status != http.StatusInternalServerError || e.Type != "UnsupportedOperationException" || errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error before any row is %v, want the status 500 of the body, with HTTP 200 and no dbimp.ErrIncomplete", err)
	}
	if s, ok := errors.AsType[*dbimp.StatusError](err); ok {
		t.Errorf("the error of an answer with HTTP 200 wraps %v, want no status error", s)
	}
	got, err := numbers(t, db)
	e, ok = errors.AsType[*opensearch.Error](err)
	if !slices.Equal(got, []int64{1, 2}) || !ok || e.Status != http.StatusInternalServerError || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("read %v and %v, want 1 and 2 and the status 500 of the body with dbimp.ErrIncomplete", got, err)
	}
}

// TestErrorBeforeRows holds that an error of the first request comes from
// QueryContext, does not wrap dbimp.ErrIncomplete, and has the status and the
// fields of the body.
func TestErrorBeforeRows(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusBadRequest, `{"error":{"reason":"Invalid SQL query","details":"Query must start with SELECT, DELETE, SHOW or DESCRIBE: SELEC 1","type":"SQLFeatureNotSupportedException"},"status":400}`)
	}}
	db := f.open(t, "", "")
	_, err := db.ExecContext(t.Context(), "SELEC 1")
	e, ok := errors.AsType[*opensearch.Error](err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Type != "SQLFeatureNotSupportedException" || !strings.HasPrefix(e.Details, "Query must start with SELECT") || errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want the error of HTTP 400 with SQLFeatureNotSupportedException and no dbimp.ErrIncomplete", err)
	}
	if want := "opensearch: SQLFeatureNotSupportedException: Invalid SQL query"; err.Error() != want {
		t.Errorf("the message is %q, want %q", err, want)
	}
	if s, ok := errors.AsType[*dbimp.StatusError](err); !ok || s.Code != http.StatusBadRequest {
		t.Errorf("the error wraps %v, want the status error of HTTP 400", s)
	}
}

// TestErrorShapes holds the other forms of an error that the server sends: a
// string for an error, the plain text of HTTP 401, and an answer that is a page
// of HTML (recorded: "a wrong password", "a statement as plain text" and "a
// body with no query").
func TestErrorShapes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name         string
		status       int
		body         string
		typ, message string
	}{
		{"a wrong password", http.StatusUnauthorized, "Unauthorized", "", "opensearch: 401: Unauthorized"},
		{"a string", http.StatusNotAcceptable, `{"error":"Content-Type header [text/plain] is not supported","status":406}`, "", "opensearch: 406: Content-Type header [text/plain] is not supported"},
		{"no handler", http.StatusBadRequest, `{"error":"no handler found for uri [/_plugins/_sql/_cancel] and method [POST]"}`, "", "opensearch: 400: no handler found for uri [/_plugins/_sql/_cancel] and method [POST]"},
		{"html", http.StatusBadGateway, "<html>bad</html>", "", "opensearch: 502: Bad Gateway"},
		{"empty", http.StatusServiceUnavailable, "", "", "opensearch: 503: Service Unavailable"},
		{"details only", http.StatusInternalServerError, `{"error":{"details":"boom\nmore","type":"X"},"status":500}`, "X", "opensearch: X: boom"},
	} {
		f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			reply(w, tt.status, tt.body)
		}}
		db := f.open(t, "", "")
		_, err := db.ExecContext(t.Context(), "SELECT 1")
		e, ok := errors.AsType[*opensearch.Error](err)
		if !ok || e.HTTPStatus != tt.status || e.Type != tt.typ || err.Error() != tt.message {
			t.Errorf("%s: the error is %v, want %q with HTTP %d", tt.name, err, tt.message, tt.status)
		}
	}
}

// TestCutPage holds that a page that ends before its end is an error, with
// dbimp.ErrIncomplete after a row.
func TestCutPage(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"schema":[{"name":"a","type":"long"}],"datarows":[[1],[2`)
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
		{"no schema", `{"datarows":[[1]]}`, dbimp.ErrInvalidValue},
		{"no rows", `{"schema":[{"name":"a","type":"long"}]}`, dbimp.ErrInvalidValue},
		{"a row of the wrong size", `{"schema":[{"name":"a","type":"long"}],"datarows":[[1,2]]}`, dbimp.ErrColumnCount},
		{"an error with HTTP 200", `{"error":{"type":"x_exception","reason":"boom"},"status":500}`, nil},
		{"a value of the wrong kind", `{"schema":[{"name":"a","type":"long"}],"datarows":[["x"]]}`, dbimp.ErrInvalidValue},
		{"text that is not JSON", `SELECT`, nil},
	} {
		f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			reply(w, http.StatusOK, tt.body)
		}}
		db := f.open(t, "", "")
		_, err := numbers(t, db)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", tt.name, err, tt.want)
		}
		if _, ok := errors.AsType[*opensearch.Error](err); tt.name == "an error with HTTP 200" && !ok {
			t.Errorf("%s: the error is %v, want an *opensearch.Error", tt.name, err)
		}
	}
}

// TestCancel holds D168: a context that ends while the driver reads stops the
// read, and the error is the error of the context. The driver closes no
// cursor, because the cursor follows the rows that it did not read.
func TestCancel(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema":[{"name":"a","type":"long"}],"datarows":[[1]`)
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

// TestCancelAfterTheCursor holds that a context that ends after the driver
// read a cursor still closes the cursor on the server, with a request that the
// context does not bound (D168).
func TestCancelAfterTheCursor(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"schema":[{"name":"a","type":"long"}],"cursor":"d:c1","datarows":[[1],[2]]}`)
	}}
	db := f.open(t, "", "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	cancel()
	_ = rows.Close() //nolint:sqlclosecheck // The test closes the rows after the context ended on purpose.
	if got := f.closed(); !slices.Equal(got, []string{"d:c1"}) {
		t.Errorf("the driver closed %v, want d:c1", got)
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

// TestRedirect holds D168: the driver follows no redirect, so it sends the
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
		http.Redirect(w, r, target.URL+"/_plugins/_sql", http.StatusTemporaryRedirect)
	}}
	db := f.open(t, "u:secret@", "")
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if e, ok := errors.AsType[*opensearch.Error](err); !ok || e.HTTPStatus != http.StatusTemporaryRedirect {
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
		_, _ = io.WriteString(w, `{"schema":[{"name":"s","type":"keyword"}],"datarows":[`)
		for i := range pageRows {
			sep := ","
			if i == 0 {
				sep = ""
			}
			if _, err := io.WriteString(w, sep+row); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `],"total":8192,"size":8192,"status":200`)
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
