package spanner //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// call is one request that a fake server got.
type call struct {
	method string
	// verb is the verb after the colon of the path, such as executeStreamingSql,
	// or the last part of the path when it has no verb, such as ddl.
	verb string
	path string
	body []byte
	// ctx is the context of the request, which ends when the client goes away.
	ctx context.Context //nolint:containedctx // The test handler reads the end of the request.
}

// fake is a fake server for the tests that look at the requests of the driver.
// It answers the request for the database and the request for a session as the
// real server does, and hands every other request to its handler.
type fake struct {
	t   *testing.T
	srv *httptest.Server

	mu    sync.Mutex
	calls []call
	// handle returns the status and the body of the answer to a request. A nil
	// handle answers HTTP 404.
	handle func(c call) (int, string)
}

// newFake starts a fake server with the handler h.
func newFake(t *testing.T, h func(c call) (int, string)) *fake {
	t.Helper()
	f := &fake{t: t, handle: h}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c := call{method: r.Method, path: r.URL.Path, body: body, ctx: r.Context()}
	last := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if _, verb, ok := strings.Cut(last, ":"); ok {
		c.verb = verb
	} else {
		c.verb = last
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	status, out := http.StatusNotFound, `{"error":{"code":404,"message":"the fake server has no answer","status":"NOT_FOUND"}}`
	switch {
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/databases/") && !strings.Contains(r.URL.Path[strings.Index(r.URL.Path, "/databases/")+len("/databases/"):], "/"):
		status, out = http.StatusOK, `{"databaseDialect":"GOOGLE_STANDARD_SQL"}`
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sessions"):
		status, out = http.StatusOK, `{"name":"`+r.URL.Path+`/test-session","multiplexed":true}`
	case f.handle != nil:
		status, out = f.handle(c)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, out) //nolint:gosec // G705: the body is a text that the test wrote.
}

// db returns a database on the fake server, with the configuration of the
// tests.
func (f *fake) db() *sql.DB {
	f.t.Helper()
	return open(f.t, config(), f.srv.URL, false)
}

// verbs returns the verbs of the requests, in order, without the requests for the
// database and for a session.
func (f *fake) verbs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c.method == http.MethodGet && strings.Contains(c.path, "/databases/") && !strings.Contains(c.path, "/operations") && !strings.HasSuffix(c.path, "/ddl") || c.verb == "sessions" && c.method == http.MethodPost {
			continue
		}
		out = append(out, c.verb)
	}
	return out
}

// bodies returns the bodies of the requests with the verb, as objects.
func (f *fake) bodies(verb string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, c := range f.calls {
		if c.verb != verb {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(c.body, &m); err != nil {
			f.t.Fatalf("reading the body of %s: %v: %s", verb, err, c.body)
		}
		out = append(out, m)
	}
	return out
}

// last returns the body of the last request with the verb.
func (f *fake) last(verb string) map[string]any {
	f.t.Helper()
	b := f.bodies(verb)
	if len(b) == 0 {
		f.t.Fatalf("the fake server got no %s", verb)
	}
	return b[len(b)-1]
}

// count returns the number of requests with the verb.
func (f *fake) count(verb string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.verb == verb {
			n++
		}
	}
	return n
}

// streamOf returns a stream of one message with the columns fields, which are
// the JSON of the fields, and the values.
func streamOf(fields, values string) string {
	return `[{"metadata":{"rowType":{"fields":[` + fields + `]}},"values":[` + values + `],"last":true}]`
}

// dmlStream is the stream of a DML statement that changed n rows.
func dmlStream(n string) string {
	return `[{"metadata":{"rowType":{}},"stats":{"rowCountExact":"` + n + `"},"last":true}]`
}

// intField is the field of an INT64 column.
func intField(name string) string {
	return `{"name":"` + name + `","type":{"code":"INT64"}}`
}

// failure runs a query to its end, and returns the error that it ended with, from the
// query or from the rows. A test uses it for a statement that the server refuses.
func failure(tb testing.TB, db *sql.DB, query string) error {
	tb.Helper()
	rows, err := db.QueryContext(tb.Context(), query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}
