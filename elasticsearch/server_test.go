package elasticsearch_test

import (
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp/elasticsearch"
)

// fake is a fake server of Elasticsearch. It keeps what it received, and
// answers each request to /_sql with handle. It answers a request to
// /_sql/close with {"succeeded": true}, and keeps its cursor.
type fake struct {
	mu      sync.Mutex
	bodies  []map[string]any
	queries []string
	headers []http.Header
	closes  []string

	// handle writes the answer to the request whose body is m.
	handle func(w http.ResponseWriter, r *http.Request, m map[string]any)
}

// ServeHTTP satisfies http.Handler.
func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	f.mu.Lock()
	f.headers = append(f.headers, r.Header.Clone())
	if r.URL.Path == "/_sql/close" {
		f.closes = append(f.closes, fmt.Sprint(m["cursor"]))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"succeeded":true}`)
		return
	}
	f.bodies = append(f.bodies, m)
	f.queries = append(f.queries, r.URL.RawQuery)
	f.mu.Unlock()
	if f.handle == nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"columns":[{"name":"a","type":"long"}],"rows":[[1]]}`)
		return
	}
	f.handle(w, r, m)
}

// open starts the fake server and returns a database on it. userinfo is the
// user and the password of the DSN with its @, such as "u:p@", or empty, and
// query is its query with its ?, or empty.
func (f *fake) open(t *testing.T, userinfo, query string) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	db, err := sql.Open(elasticsearch.Name, "elasticsearch://"+userinfo+strings.TrimPrefix(srv.URL, "http://")+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// requests returns the bodies that the server received, and forgets them.
func (f *fake) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.bodies
	f.bodies = nil
	return b
}

// closed returns the cursors that the server was asked to close.
func (f *fake) closed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closes...)
}

// page returns the body of a page. A first page has the column a, a long. A
// page has the rows of the numbers from first to last, and the cursor, if it
// is not empty.
func page(first bool, from, to int, cursor string) string {
	var b strings.Builder
	b.WriteString("{")
	if first {
		b.WriteString(`"columns":[{"name":"a","type":"long"}],`)
	}
	b.WriteString(`"rows":[`)
	for i := from; i <= to; i++ {
		if i > from {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "[%d]", i)
	}
	b.WriteString("]")
	if cursor != "" {
		fmt.Fprintf(&b, `,"cursor":%q`, cursor)
	}
	b.WriteString("}")
	return b.String()
}

// reply writes a JSON body with a status.
func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
