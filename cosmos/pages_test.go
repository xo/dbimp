package cosmos_test

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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
	"github.com/xo/dbimp/dbimptest"
)

// counted is a database whose transport counts the requests that it sends.
type counted struct {
	db *sql.DB
	n  atomic.Int64
}

type countTransport struct {
	next http.RoundTripper
	n    *atomic.Int64
}

func (c countTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.next.RoundTrip(r)
}

// openCounted returns a database for the fake server at rawURL, which counts
// the requests of the driver.
func openCounted(t *testing.T, rawURL, container, query string) *counted {
	t.Helper()
	cfg, err := cosmos.ParseDSN(dsnFor(rawURL, "/dbimp_it/"+container, query))
	if err != nil {
		t.Fatal(err)
	}
	c := &counted{}
	conn := cosmos.NewConnector(*cfg)
	conn.WrapTransport(func(rt http.RoundTripper) http.RoundTripper { return countTransport{next: rt, n: &c.n} })
	c.db = sql.OpenDB(conn)
	t.Cleanup(func() { c.db.Close() })
	return c
}

// TestRecordedPages holds D190: the driver follows the header X-Ms-Continuation
// to the last page. The hosted account answered pages of 1000, 1000 and 500
// rows over 2,500 rows, and the token is JSON text that the driver sends back
// as it is (recorded: "the first page with the default size", "the second
// page", "the third page"). The emulator answered pages of 100.
func TestRecordedPages(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, server, query, dsn string
		// rows is the number of rows that the test reads, and requests is the
		// number of requests that they take. The recording of the emulator
		// holds the first three pages of 100 rows with the default size.
		rows     int
		requests int64
	}{
		{"hosted default", hosted, "SELECT c.id, c.n FROM c", "", 2500, 3},
		{"hosted pages of 1000", hosted, "SELECT c.id FROM c", "pagesize=1000", 2500, 3},
		{"hosted page of 5000", hosted, "SELECT c.id FROM c", "pagesize=5000", 2500, 1},
		{"hosted page of minus one", hosted, "SELECT c.id FROM c", "pagesize=-1", 2500, 1},
		{"emulator default", emulator, "SELECT c.id, c.n FROM c", "", 300, 3},
		{"emulator pages of 1000", emulator, "SELECT c.id FROM c", "pagesize=1000", 2500, 3},
		{"emulator page of 5000", emulator, "SELECT c.id FROM c", "pagesize=5000", 2500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := replayServer(t, tt.server)
			c := openCounted(t, srv.URL, "bulk", tt.dsn)
			rows, err := c.db.QueryContext(t.Context(), tt.query)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var got [][]string
			cols, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			for len(got) < tt.rows && rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				got = append(got, []string{show(vals[slices.Index(cols, "id")])})
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.rows {
				t.Fatalf("the query gave %d rows, want %d", len(got), tt.rows)
			}
			if tt.rows == 2500 && rows.Next() {
				t.Fatal("a row came after the last one")
			}
			if want := `string("rp0-0")`; got[0][0] != want || !slices.Contains(cols, "id") {
				t.Errorf("columns %q, first value %q, want the key id and %s", cols, got[0][0], want)
			}
			if n := c.n.Load(); n != tt.requests {
				t.Errorf("the driver sent %d requests, want %d", n, tt.requests)
			}
		})
	}
}

// TestPagesWithThePartitionKey holds that the header of the continuation goes
// with the header of the partition key on the hosted account.
func TestOptionsForThePages(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	c := openCounted(t, srv.URL, "bulk", "")
	rows, err := c.db.QueryContext(t.Context(), "SELECT c.id FROM c", cosmos.WithPageSize(1000))
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := readRows(t, rows)
	if err != nil || len(got) != 2500 || c.n.Load() != 3 {
		t.Errorf("rows %d, error %v, requests %d, want 2500 rows in 3 requests", len(got), err, c.n.Load())
	}
}

// page is one answer of a fake server.
type page struct {
	status int
	token  string
	body   string
}

// fake is a fake server. It answers each request by the header
// X-Ms-Continuation, and keeps each request.
type fake struct {
	// pages maps a continuation to its answer. The first request has none, so
	// its answer is pages[""].
	pages map[string]page

	mu      sync.Mutex
	bodies  []string
	headers []http.Header
	paths   []string
	// after runs after a page is sent, with its continuation.
	after func(token string)
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	token := r.Header.Get("X-Ms-Continuation")
	f.mu.Lock()
	f.bodies = append(f.bodies, string(b))
	f.headers = append(f.headers, r.Header.Clone())
	f.paths = append(f.paths, r.URL.Path)
	f.mu.Unlock()
	p, ok := f.pages[token]
	if !ok {
		p = page{status: http.StatusBadRequest, body: `{"code":"BadRequest","message":"Invalid Continuation Token\r\nActivityId: x"}`}
	}
	w.Header().Set("Content-Type", "application/json")
	if p.token != "" {
		w.Header().Set("X-Ms-Continuation", p.token)
	}
	if p.status != 0 {
		w.WriteHeader(p.status)
	}
	_, _ = io.WriteString(w, p.body)
	if f.after != nil {
		f.after(token)
	}
}

// open returns a database on the fake server.
func (f *fake) open(t *testing.T) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return openFake(t, srv.URL, "c", "")
}

// docs is the body of a page of the documents that hold only an id.
func docs(ids ...string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = `{"id":"` + id + `"}`
	}
	return `{"_rid":"r","Documents":[` + strings.Join(parts, ",") + `],"_count":` + strconv.Itoa(len(ids)) + `}`
}

// TestPages holds D190: a page can hold no document and still carry a token,
// the members around Documents are skipped, and each page is asked with the
// token of the page before it.
func TestPages(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: `{"_rid":"r","Documents":[{"id":"1"},{"id":"2"}],"_count":2}`},
		"t1": {token: "t2", body: `{"_rid":"r","Documents":[],"_count":0}`},
		"t2": {token: "t3", body: `{"Documents":[],"_count":0,"_rid":"r"}`},
		"t3": {body: `{"_count":1,"Documents":[{"id":"3"}],"_rid":"r"}`},
	}}
	db := f.open(t)
	_, got, err := read(t, db, "SELECT c.id FROM c")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{`string("1")`}, {`string("2")`}, {`string("3")`}}
	if len(got) != 3 || got[0][0] != want[0][0] || got[1][0] != want[1][0] || got[2][0] != want[2][0] {
		t.Errorf("rows = %q, want %q", got, want)
	}
	var tokens []string
	for _, h := range f.headers {
		tokens = append(tokens, h.Get("X-Ms-Continuation"))
		if h.Get("Content-Type") != "application/query+json" || h.Get("X-Ms-Documentdb-Isquery") != "True" || h.Get("X-Ms-Documentdb-Query-Enablecrosspartition") != "True" {
			t.Errorf("a page was asked with the headers %v, want the headers of a query on each page", h)
		}
	}
	if want := []string{"", "t1", "t2", "t3"}; !equal(tokens, want) {
		t.Errorf("the requests carried the tokens %q, want %q", tokens, want)
	}
	for _, b := range f.bodies {
		if b != f.bodies[0] {
			t.Errorf("a page was asked with the body %s, want %s on each page", b, f.bodies[0])
		}
	}
}

// TestFirstPagesWithNoDocument holds that the columns come from the first
// document, even when it is on a later page.
func TestFirstPagesWithNoDocument(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: `{"Documents":[]}`},
		"t1": {token: "t2", body: `{"_rid":"r"}`},
		"t2": {body: `{"Documents":[{"b":1,"a":2}]}`},
	}}
	db := f.open(t)
	cols, got, err := read(t, db, "SELECT c.b, c.a FROM c")
	if err != nil {
		t.Fatal(err)
	}
	if !equal(cols, []string{"b", "a"}) || len(got) != 1 {
		t.Errorf("columns %q rows %q", cols, got)
	}
	if len(f.headers) != 3 {
		t.Errorf("the driver sent %d requests, want 3", len(f.headers))
	}
}

// TestAResultWithNoDocument holds that a result with no document has no
// column and no row, with or without the member Documents.
func TestAResultWithNoDocument(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"_rid":"r","Documents":[],"_count":0}`, `{"_rid":"r","_count":0}`, `{"Documents":null}`} {
		f := &fake{pages: map[string]page{"": {body: body}}}
		cols, got, err := read(t, f.open(t), "SELECT c.id FROM c")
		if err != nil || len(cols) != 0 || len(got) != 0 {
			t.Errorf("%s: columns %q rows %q error %v, want none", body, cols, got, err)
		}
	}
}

// TestALaterDocumentWithAMissingKey holds D18 and D190: a key that a later
// document lacks is nil, in the same page and on a later page.
func TestALaterDocumentWithAMissingKey(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: `{"Documents":[{"a":1,"b":2},{"a":3}]}`},
		"t1": {body: `{"Documents":[{"b":4},{"b":null,"a":5}]}`},
	}}
	cols, got, err := read(t, f.open(t), "SELECT * FROM c")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"int64(1)", "int64(2)"}, {"int64(3)", "nil"}, {"nil", "int64(4)"}, {"int64(5)", "nil"}}
	if !equal(cols, []string{"a", "b"}) || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("columns %q rows %q, want %q", cols, got, want)
	}
}

// TestALaterDocumentWithAnExtraKey holds D18 and D190: a key that only a later
// document has is an error that names the key, after the rows that came
// before it, and never a new column.
func TestALaterDocumentWithAnExtraKey(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		pages map[string]page
		rows  int
	}{
		{"in the page", map[string]page{"": {body: `{"Documents":[{"a":1},{"a":2,"extra":3}]}`}}, 1},
		{"on a later page", map[string]page{
			"":   {token: "t1", body: `{"Documents":[{"a":1}]}`},
			"t1": {body: `{"Documents":[{"a":2},{"extra":3}]}`},
		}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &fake{pages: tt.pages}
			rows, err := f.open(t).QueryContext(t.Context(), "SELECT * FROM c")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			n := 0
			for rows.Next() {
				n++
			}
			err = rows.Err()
			if !errors.Is(err, dbimp.ErrExtraColumn) || !errors.Is(err, dbimp.ErrIncomplete) || !strings.Contains(err.Error(), "extra") {
				t.Errorf("the error is %v, want %v that names the key, and %v", err, dbimp.ErrExtraColumn, dbimp.ErrIncomplete)
			}
			if n != tt.rows {
				t.Errorf("read %d rows before the error, want %d", n, tt.rows)
			}
		})
	}
}

// TestScalarRows holds that the rows of SELECT VALUE are the column $1 on
// every page, and that a later row of another kind is a value, and never an
// error.
func TestScalarRows(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: `{"Documents":[1,"a",null]}`},
		"t1": {body: `{"Documents":[[1,2],{"k":true},2.5]}`},
	}}
	cols, got, err := read(t, f.open(t), "SELECT VALUE c.v FROM c")
	if err != nil {
		t.Fatal(err)
	}
	want := "[[int64(1)] [string(\"a\")] [nil] [[int64(1) int64(2)]] [{k:bool(true)}] [float64(2.5)]]"
	if !equal(cols, []string{"$1"}) || fmt.Sprint(got) != want {
		t.Errorf("columns %q rows %v, want %s", cols, got, want)
	}
}

// TestAnObjectThenAScalar holds that a row that is not an object, after rows
// that are objects, is an error, because the columns are the keys of the first
// row.
func TestAnObjectThenAScalar(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{"": {body: `{"Documents":[{"a":1},2]}`}}}
	rows, err := f.open(t).QueryContext(t.Context(), "SELECT VALUE c.v FROM c")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); !errors.Is(err, dbimp.ErrInvalidValue) || n != 1 {
		t.Errorf("read %d rows and the error %v, want 1 row and %v", n, err, dbimp.ErrInvalidValue)
	}
}

// TestNumbersWithAThreeDigitExponent holds D190: the hosted account writes an
// exponent with three digits, such as e+019, in the answer of a read by id, and
// the driver reads it (recorded: "numbers beyond 2^53 and the numbers that JSON
// loses"). A number that is too large for int64, and has no exponent, is an
// *apd.Decimal.
func TestNumbersWithAThreeDigitExponent(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{"": {body: `{"Documents":[{"a":1.8446744073709552e+019,"b":1.2345678901234568e+029,"c":18446744073709551615,"d":-1e-005,"e":1E+3}]}`}}}
	_, got, err := read(t, f.open(t), "SELECT * FROM c")
	if err != nil {
		t.Fatal(err)
	}
	want := "[[float64(1.8446744073709552e+19) float64(1.2345678901234568e+29) decimal(18446744073709551615) float64(-1e-05) float64(1000)]]"
	if fmt.Sprint(got) != want {
		t.Errorf("rows = %v, want %s", got, want)
	}
}

// TestAnErrorOnALaterPage holds D21 and D107: an error on a page after the
// first reaches the caller from Rows.Err, after the rows of the pages before
// it, and wraps dbimp.ErrIncomplete and the error of the server.
func TestAnErrorOnALaterPage(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: docs("1", "2")},
		"t1": {status: http.StatusTooManyRequests, body: `{"code":"TooManyRequests","message":"Message: {\"Errors\":[\"Request rate is large.\"]}\r\nActivityId: x"}`},
	}}
	rows, err := f.open(t).QueryContext(t.Context(), "SELECT c.id FROM c")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	err = rows.Err()
	if _, ok := errors.AsType[*cosmos.Error](err); !ok || !errors.Is(err, dbimp.ErrIncomplete) || n != 2 {
		t.Errorf("read %d rows and the error %v, want 2 rows and an *Error that wraps %v", n, err, dbimp.ErrIncomplete)
	}
}

// TestAnErrorOnTheFirstPage holds D107: an error before the first row does not
// wrap dbimp.ErrIncomplete, and QueryContext returns it.
func TestAnErrorOnTheFirstPage(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"Documents":[`, `{"Documents":`, `{`, ``, `[]`, `{"Documents":{}}`} {
		f := &fake{pages: map[string]page{"": {body: body}}}
		err := drain(t, f.open(t), "SELECT c.id FROM c")
		if err == nil {
			t.Errorf("the body %q gave no error", body)
			continue
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("the body %q gave %v, which wraps %v before the first row", body, err, dbimp.ErrIncomplete)
		}
	}
}

// TestCloseBeforeTheEnd holds D36: Rows.Close before the end reads no more
// page.
func TestCloseBeforeTheEnd(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: docs("1", "2")},
		"t1": {token: "t2", body: docs("3")},
		"t2": {body: docs("4")},
	}}
	rows, err := f.open(t).QueryContext(t.Context(), "SELECT c.id FROM c")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.headers) != 1 {
		t.Errorf("the driver sent %d requests, want 1", len(f.headers))
	}
}

// TestCancelBetweenPages holds D8 and D36: a context that ends between two
// pages stops the next request, and the error is the error of the context.
func TestCancelBetweenPages(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: docs("1")},
		"t1": {body: docs("2")},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := f.open(t).QueryContext(ctx, "SELECT c.id FROM c")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	cancel()
	if rows.Next() {
		t.Fatal("a second row came after the context ended")
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want %v", err, context.Canceled)
	}
	if len(f.headers) != 1 {
		t.Errorf("the driver sent %d requests, want 1", len(f.headers))
	}
}

// TestACancelBeforeTheAnswer holds that a context that ends while the driver
// waits for the headers of the answer stops the request with the error of the
// context, and never driver.ErrBadConn (D8). The server cancels nothing: it
// abandons the request, because Cosmos DB has no call to cancel one (D190).
func TestACancelBeforeTheAnswer(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server sees that the client left only after it read the body.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	db := openFake(t, srv.URL, "c", "")
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()
	err := drainContext(t, ctx, db, "SELECT c.id FROM c")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want %v", err, context.Canceled)
	}
}

// TestADeadline holds that a deadline gives context.DeadlineExceeded.
func TestADeadline(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	db := openFake(t, srv.URL, "c", "")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := drainContext(t, ctx, db, "SELECT c.id FROM c")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want %v", err, context.DeadlineExceeded)
	}
}

// TestTheGoroutinesEnd holds that no goroutine is left running after a
// result is read, closed early, or fails (D8). The test does not run in
// parallel, because it counts the goroutines of the process.
func TestTheGoroutinesEnd(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: docs("1", "2")},
		"t1": {body: docs("3")},
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	db, err := sql.Open(cosmos.Name, dsnFor(srv.URL, "/dbimp_it/c", ""))
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := drain(t, db, "SELECT c.id FROM c"); err != nil {
			t.Fatal(err)
		}
	}
	// A result that is closed before its end.
	func() {
		rows, err := db.QueryContext(t.Context(), "SELECT c.id FROM c")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("no row: %v", rows.Err())
		}
	}()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestTwoQueriesAtOnce holds that two queries run at the same time on one
// sql.DB.
func TestTwoQueriesAtOnce(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {token: "t1", body: docs("1", "2")},
		"t1": {body: docs("3")},
	}}
	db := f.open(t)
	var open []*sql.Rows
	for range 2 {
		rows, err := db.QueryContext(t.Context(), "SELECT c.id FROM c") //nolint:rowserrcheck // readRows below reads the rows and returns Err.
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		open = append(open, rows)
	}
	for _, rows := range open {
		_, got, err := readRows(t, rows)
		if err != nil || len(got) != 3 {
			t.Errorf("rows %q error %v, want 3 rows", got, err)
		}
	}
}

// TestLargeResult holds D25: the driver reads a result in pages, one document
// at a time, and the memory in use stays far below the size of the result. It
// does not run in parallel, so that the memory of other tests does not count.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const (
		pages   = 8
		perPage = 1 << 17
	)
	doc := `{"id":"` + strings.Repeat("x", 36) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := 0
		if tok := r.Header.Get("X-Ms-Continuation"); tok != "" {
			_, _ = fmt.Sscan(tok, &n)
		}
		w.Header().Set("Content-Type", "application/json")
		if n+1 < pages {
			w.Header().Set("X-Ms-Continuation", strconv.Itoa(n+1))
		}
		_, _ = io.WriteString(w, `{"_rid":"r","Documents":[`)
		for i := range perPage {
			sep := ","
			if i == 0 {
				sep = ""
			}
			if _, err := io.WriteString(w, sep+doc); err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `],"_count":`+strconv.Itoa(perPage)+`}`)
	}))
	t.Cleanup(srv.Close)
	db := openFake(t, srv.URL, "c", "")
	rows, err := db.QueryContext(t.Context(), "SELECT c.id FROM c")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var (
		n    int
		peak uint64
		s    string
	)
	for rows.Next() {
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		if n++; n%(pages*perPage/16) == 0 {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapInuse)
		}
	}
	if err := rows.Err(); err != nil || n != pages*perPage {
		t.Fatalf("read %d rows and %v, want %d rows", n, err, pages*perPage)
	}
	if peak > 16<<20 {
		t.Errorf("the heap in use reached %d bytes while the driver read a result of 64 MiB, want at most 16 MiB (D25)", peak)
	}
}
