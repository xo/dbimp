package trino_test

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp/trino"
)

// request is a request that a fake server received.
type request struct {
	method string
	uri    string
	header http.Header
	body   string
}

// fake is a server that answers with a function and keeps the requests that
// it received. Its handler runs for every request, with the number of the
// request, from 0.
type fake struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []request
}

// newFake starts a fake server, which closes with the test.
func newFake(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body string, n int)) *fake {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		n := len(f.reqs)
		f.reqs = append(f.reqs, request{r.Method, r.URL.RequestURI(), r.Header.Clone(), string(body)})
		f.mu.Unlock()
		h(w, r, string(body), n)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// requests returns the requests that arrived so far.
func (f *fake) requests() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.reqs...)
}

// host returns the host and the port of the server.
func (f *fake) host() string {
	return strings.TrimPrefix(f.srv.URL, "http://")
}

// dsn returns a DSN for the server with the user trino and the keys in query.
func (f *fake) dsn(query string) string {
	d := "trino://trino@" + f.host()
	if query != "" {
		d += "?" + query
	}
	return d
}

// open opens a database on the server, which closes with the test.
func (f *fake) open(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	cfg, err := trino.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(trino.NewConnector(*cfg))
	t.Cleanup(func() { db.Close() })
	return db
}

// bigint is the column of an answer that holds bigint values, named name.
func bigint(name string) string {
	return fmt.Sprintf(`{"name":%q,"type":"bigint","typeSignature":{"rawType":"bigint","arguments":[]}}`, name)
}

// page returns a page. next, columns and data are the members that the page
// holds, or "" for a member that it lacks.
func page(next, columns, data string) string {
	s := `{"id":"q1","infoUri":"http://elsewhere.invalid/ui/query.html?q1"`
	if next != "" {
		s += `,"nextUri":"` + next + `"`
	}
	if columns != "" {
		s += `,"columns":[` + columns + `]`
	}
	if data != "" {
		s += `,"data":` + data
	}
	return s + `,"stats":{"state":"RUNNING"}}`
}

// writePage writes a page as the servers do.
func writePage(w http.ResponseWriter, body string, headers ...string) {
	w.Header().Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		w.Header().Add(headers[i], headers[i+1])
	}
	_, _ = io.WriteString(w, body) //nolint:gosec // G705: the body is a page that the test wrote.
}

// one is a handler that answers every request with a page of one row.
func one(w http.ResponseWriter, _ *http.Request, _ string, _ int) {
	writePage(w, page("", bigint("a"), "[[1]]"))
}
