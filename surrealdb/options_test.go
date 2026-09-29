package surrealdb //nolint:testpackage // The test of the CBOR body reads encode, which is not exported.

import (
	"bytes"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// sent is a request that the fake server of the option tests received.
type sent struct {
	header http.Header
	body   map[string]any
}

// optionServer is a fake server for the option tests. It answers each
// request of JSON with one result of one row.
type optionServer struct {
	mu   sync.Mutex
	reqs []sent
}

func (s *optionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s.mu.Lock()
	s.reqs = append(s.reqs, sent{header: r.Header, body: m})
	s.mu.Unlock()
	w.Header().Set("Content-Type", contentJSON)
	_, _ = io.WriteString(w, `{"result":[{"result":1,"status":"OK","time":"1µs"}]}`)
}

// open returns a database on the fake server, as a user of the database
// dbmeta of the namespace ns.
func (s *optionServer) open(t *testing.T) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db, err := sql.Open(Name, strings.Replace(srv.URL, "http://", "surrealdb://u:p@", 1)+"/ns/dbmeta?encoding=json&auth=database")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// requests returns what the server received, in order, and forgets it.
func (s *optionServer) requests() []sent {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs := s.reqs
	s.reqs = nil
	return reqs
}

// TestOptionArguments holds that the options of one statement reach its
// request as the headers and the keys of the body, that the credentials keep
// the database of the DSN, and that the options do not reach the next
// statement (D109).
func TestOptionArguments(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t)
	var one int64
	err := db.QueryRowContext(t.Context(), "RETURN $a", sql.Named("a", int64(1)),
		WithNamespace("ns2"), WithDatabase("other"), WithParameter("id", int64(7))).Scan(&one)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "RETURN 1").Scan(&one); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 {
		t.Fatalf("the server received %d requests, want 2", len(reqs))
	}
	first, second := reqs[0], reqs[1]
	params, _ := first.body["params"].([]any)
	switch {
	case first.header.Get("Surreal-Ns") != "ns2", first.header.Get("Surreal-Db") != "other":
		t.Errorf("the first request names %s/%s, want ns2/other", first.header.Get("Surreal-Ns"), first.header.Get("Surreal-Db"))
	case first.header.Get("Surreal-Auth-Ns") != "ns", first.header.Get("Surreal-Auth-Db") != "dbmeta":
		t.Errorf("the credentials of the first request name %s/%s, want ns/dbmeta of the DSN", first.header.Get("Surreal-Auth-Ns"), first.header.Get("Surreal-Auth-Db"))
	case first.body["id"] != 7.0 || len(params) != 2:
		t.Errorf("the first body is %v, want the id 7, the text and the variable a", first.body)
	}
	if second.header.Get("Surreal-Db") != "dbmeta" || second.body["id"] != nil {
		t.Errorf("the second request is %v in %s, want no id in dbmeta", second.body, second.header.Get("Surreal-Db"))
	}
}

// TestOptionUnsupported holds that WithTimeout and WithReadonly fail with
// dbimp.ErrNotSupported, because the RPC protocol has neither (D109), and
// send nothing.
func TestOptionUnsupported(t *testing.T) {
	t.Parallel()
	s := &optionServer{}
	db := s.open(t)
	for _, opt := range []Option{WithTimeout(time.Second), WithReadonly(true)} {
		if _, err := db.ExecContext(t.Context(), "RETURN 1", opt); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("an option that the server has no setting for gave %v, want dbimp.ErrNotSupported", err)
		}
	}
	if _, err := db.ExecContext(t.Context(), "RETURN 1", WithTimeout(-time.Second)); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("a negative timeout gave %v, want dbimp.ErrInvalidValue", err)
	}
	if reqs := s.requests(); len(reqs) != 0 {
		t.Errorf("the server received %v, want nothing", reqs)
	}
}

// TestOptionCBORBody holds that a key of WithParameter follows the keys of a
// body of CBOR, and replaces a key of the driver with the same name, as in
// JSON (D109).
func TestOptionCBORBody(t *testing.T) {
	t.Parallel()
	// encode sends nothing, so the connector opens no connection to close.
	c := &conn{c: NewConnector(Config{Host: "h", Namespace: "n", Database: "d"})}
	body := rpcRequest{Method: "query", Params: []any{"RETURN 1"}}
	var want dbimp.CBOREncoder
	want.Map(3)
	want.Text("params")
	want.Array(1)
	want.Text("RETURN 1")
	want.Text("id")
	want.Int(7)
	want.Text("method")
	want.Text("ping")
	got, _, err := c.encode(body, map[string]any{"id": int64(7), "method": "ping"})
	if err != nil || !bytes.Equal(got, want.Bytes()) {
		t.Errorf("the body is %x, %v, want %x", got, err, want.Bytes())
	}
}
