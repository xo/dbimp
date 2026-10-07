package dynamodb_test

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dynamodb"
)

// page is one answer of a fake server.
type page struct {
	status int
	body   string
}

// fake is a fake server. It answers each request by the NextToken of its
// body, and keeps each body and each header.
type fake struct {
	// pages maps a NextToken to its answer. The first request has none, so
	// its answer is pages[""].
	pages map[string]page

	mu      sync.Mutex
	bodies  []map[string]any
	headers []http.Header
	// after runs after a page is sent, with its NextToken.
	after func(token string)
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	token, _ := m["NextToken"].(string)
	f.mu.Lock()
	f.bodies = append(f.bodies, m)
	f.headers = append(f.headers, r.Header.Clone())
	f.mu.Unlock()
	p, ok := f.pages[token]
	if !ok {
		p = page{status: http.StatusBadRequest, body: `{"__type":"com.amazon.coral.validate#ValidationException","Message":"Invalid NextToken"}`}
	}
	w.Header().Set("Content-Type", "application/x-amz-json-1.0")
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
	db, err := sql.Open(dynamodb.Name, strings.Replace(srv.URL, "http://", "dynamodb://key:secret@", 1)+"?region=us-east-1&tls=false")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ids reads the first column of every row as an int64 string.
func ids(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			return out, err
		}
		out = append(out, fmt.Sprint(v))
	}
	return out, rows.Err()
}

// closeRows closes rows, and fails the test for the error.
func closeRows(t *testing.T, rows *sql.Rows) {
	t.Helper()
	if err := rows.Close(); err != nil {
		t.Error(err)
	}
}

// TestPages holds D169: the driver follows NextToken to the end, a page can
// hold no item and still carry a token, NextToken can come before or after
// Items, and a member that the driver does not know is skipped (recorded: "a
// limit with a filter").
func TestPages(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {body: `{"Items":[{"id":{"N":"1"}},{"id":{"N":"2"}}],"NextToken":"t1","ConsumedCapacity":{"TableName":"t"}}`},
		"t1": {body: `{"NextToken":"t2","Items":[]}`},
		"t2": {body: `{"Items":[],"NextToken":"t3"}`},
		"t3": {body: `{"Items":[{"id":{"N":"3"}}]}`},
	}}
	db := f.open(t)
	rows, err := db.QueryContext(dynamodb.WithOptions(t.Context(), dynamodb.WithParameter("ConsistentRead", true)), "SELECT id FROM t")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ids(rows)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1", "2", "3"}; !equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	var tokens []string
	for _, b := range f.bodies {
		s, _ := b["NextToken"].(string)
		tokens = append(tokens, s)
		if b["Statement"] != "SELECT id FROM t" || b["ConsistentRead"] != true {
			t.Errorf("a page was asked with %v, want the statement and ConsistentRead on each page", b)
		}
	}
	if want := []string{"", "t1", "t2", "t3"}; !equal(tokens, want) {
		t.Errorf("the requests carried the tokens %q, want %q", tokens, want)
	}
}

func equal(a, b []string) bool {
	return len(a) == len(b) && strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

// TestFirstPagesWithNoItem holds that an error on a page that comes before
// any row does not wrap dbimp.ErrIncomplete, and reaches the caller from
// QueryContext (D107).
func TestFirstPagesWithNoItem(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {body: `{"Items":[],"NextToken":"t1"}`},
		"t1": {status: http.StatusBadRequest, body: `{"__type":"com.amazon.coral.validate#ValidationException","Message":"boom"}`},
	}}
	db := f.open(t)
	_, err := db.ExecContext(t.Context(), "SELECT id FROM t")
	var derr *dynamodb.Error
	if !errors.As(err, &derr) || derr.Type != "ValidationException" || derr.Message != "boom" {
		t.Fatalf("QueryContext = %v, want the ValidationException", err)
	}
	if errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error %v wraps dbimp.ErrIncomplete, and no row reached the caller", err)
	}
}

// TestErrorOnALaterPage holds D21 and D107: an error on a page after some
// rows reaches the caller from Rows.Err, and wraps dbimp.ErrIncomplete.
func TestErrorOnALaterPage(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		page page
		want error
	}{
		{"an error of the server", page{status: http.StatusBadRequest, body: `{"__type":"com.amazon.coral.validate#ValidationException","Message":"boom"}`}, dbimp.ErrIncomplete},
		{"a throttled request", page{status: http.StatusTooManyRequests, body: `{"__type":"com.amazonaws.dynamodb.v20120810#ProvisionedThroughputExceededException","message":"slow down"}`}, dbimp.ErrIncomplete},
		{"a body that ends early", page{body: `{"Items":[{"id":{"N":"3"}},`}, dbimp.ErrIncomplete},
		{"an item that is not an object", page{body: `{"Items":[3]}`}, dbimp.ErrIncomplete},
	} {
		f := &fake{pages: map[string]page{
			"":   {body: `{"Items":[{"id":{"N":"1"}}],"NextToken":"t1"}`},
			"t1": tt.page,
		}}
		db := f.open(t)
		rows, err := db.QueryContext(t.Context(), "SELECT id FROM t")
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		got, err := ids(rows)
		if !errors.Is(err, tt.want) || len(got) == 0 {
			t.Errorf("%s: read %v, then %v, want some rows and %v", tt.name, got, err, tt.want)
		}
		if len(f.bodies) != 2 {
			t.Errorf("%s: the driver sent %d requests, want 2: it never sends a request again", tt.name, len(f.bodies))
		}
	}
}

// TestCancelBetweenPages holds D36: when the context ends between two pages,
// the error is context.Canceled, and the driver sends no more request.
func TestCancelBetweenPages(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {body: `{"Items":[{"id":{"N":"1"}}],"NextToken":"t1"}`},
		"t1": {body: `{"Items":[{"id":{"N":"2"}}]}`},
	}}
	db := f.open(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := db.QueryContext(ctx, "SELECT id FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reading the first row: %v", rows.Err())
	}
	cancel()
	if rows.Next() {
		t.Error("a second row after the context ended")
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if len(f.bodies) != 1 {
		t.Errorf("the driver sent %d requests, want 1", len(f.bodies))
	}
}

// TestCloseBeforeTheNextPage holds D36: Close before the end sends no request
// for the next page.
func TestCloseBeforeTheNextPage(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {body: `{"Items":[{"id":{"N":"1"}}],"NextToken":"t1"}`},
		"t1": {body: `{"Items":[{"id":{"N":"2"}}]}`},
	}}
	db := f.open(t)
	rows, err := db.QueryContext(t.Context(), "SELECT id FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("reading the first row: %v", rows.Err())
	}
	closeRows(t, rows)
	if len(f.bodies) != 1 {
		t.Errorf("the driver sent %d requests, want 1", len(f.bodies))
	}
}

// TestExecReadsEveryPage holds D21: Exec reads the result to its end, so an
// error on a later page reaches the caller.
func TestExecReadsEveryPage(t *testing.T) {
	t.Parallel()
	f := &fake{pages: map[string]page{
		"":   {body: `{"Items":[{"id":{"N":"1"}}],"NextToken":"t1"}`},
		"t1": {status: http.StatusInternalServerError, body: `{"__type":"com.amazon.coral.service#InternalFailure"}`},
	}}
	db := f.open(t)
	if _, err := db.ExecContext(t.Context(), "SELECT id FROM t"); !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("Exec = %v, want an error that wraps dbimp.ErrIncomplete", err)
	}
}

// TestItems holds what each shape of an item gives.
func TestItems(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		body  string
		want  string
		err   error
	}{
		{"a missing attribute is nil", "SELECT a, b FROM t", `{"Items":[{"b":{"S":"x"}}]}`, "[<nil> x]", nil},
		{"the order is the statement's", "SELECT b, a FROM t", `{"Items":[{"a":{"N":"1"},"b":{"S":"x"}}]}`, "[x 1]", nil},
		{"a row that lacks every attribute", "SELECT a, b FROM t", `{"Items":[{}]}`, "[<nil> <nil>]", nil},
		{"an attribute that is no column", "SELECT a FROM t", `{"Items":[{"a":{"N":"1"},"b":{"N":"2"}}]}`, "", dbimp.ErrExtraColumn},
		{"a type that DynamoDB lacks", "SELECT a FROM t", `{"Items":[{"a":{"D":"2026"}}]}`, "", dbimp.ErrInvalidValue},
		{"two types in a value", "SELECT a FROM t", `{"Items":[{"a":{"S":"x","N":"1"}}]}`, "", dbimp.ErrInvalidValue},
		{"a number that is not a string", "SELECT a FROM t", `{"Items":[{"a":{"N":1}}]}`, "", dbimp.ErrInvalidValue},
		{"a NULL that is false", "SELECT a FROM t", `{"Items":[{"a":{"NULL":false}}]}`, "", dbimp.ErrInvalidValue},
		{"binary that is not base64", "SELECT a FROM t", `{"Items":[{"a":{"B":"not base64!"}}]}`, "", dbimp.ErrInvalidValue},
		{"a map is the one column of SELECT *", "SELECT * FROM t", `{"Items":[{"a":{"N":"1"},"b":{"L":[{"S":"x"},{"BOOL":true}]}}]}`, "[map[a:1 b:[x true]]]", nil},
		{"an empty item of SELECT *", "SELECT * FROM t", `{"Items":[{}]}`, "[map[]]", nil},
		{"no Items", "SELECT a FROM t", `{}`, "", nil},
		{"Items that is null", "SELECT a FROM t", `{"Items":null}`, "", nil},
		{"a write that gives no item", "UPDATE t SET a = 1 WHERE pk = 'x'", `{"Items":[]}`, "", nil},
	} {
		f := &fake{pages: map[string]page{"": {body: tt.body}}}
		db := f.open(t)
		rows, err := db.QueryContext(t.Context(), tt.query)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		cols, _ := rows.Columns()
		var got []string
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Errorf("%s: %v", tt.name, err)
			}
			got = append(got, fmt.Sprint(vals))
		}
		err = rows.Err()
		closeRows(t, rows)
		switch {
		case tt.err != nil && !errors.Is(err, tt.err):
			t.Errorf("%s: error %v, want %v", tt.name, err, tt.err)
		case tt.err == nil && err != nil:
			t.Errorf("%s: %v", tt.name, err)
		case tt.err == nil && strings.Join(got, ",") != tt.want:
			t.Errorf("%s: rows %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestLargeResult holds D25: the driver reads a result of 64 MiB across
// pages one item at a time, and the memory in use stays far below the size of
// the result. It does not run in parallel, so that the memory of other tests
// does not count.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const (
		pages   = 8
		perPage = 1 << 17
	)
	item := `{"s":{"S":"` + strings.Repeat("x", 36) + `"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m struct{ NextToken string }
		_ = json.Unmarshal(b, &m)
		n := 0
		if m.NextToken != "" {
			_, _ = fmt.Sscan(m.NextToken, &n)
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = io.WriteString(w, `{"Items":[`)
		for i := range perPage {
			sep := ","
			if i == 0 {
				sep = ""
			}
			if _, err := io.WriteString(w, sep+item); err != nil {
				return
			}
		}
		if n+1 < pages {
			_, _ = fmt.Fprintf(w, `],"NextToken":"%d"}`, n+1)
			return
		}
		_, _ = io.WriteString(w, `]}`)
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(dynamodb.Name, strings.Replace(srv.URL, "http://", "dynamodb://key:secret@", 1)+"?region=us-east-1&tls=false")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	rows, err := db.QueryContext(t.Context(), "SELECT s FROM t")
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
