package neo4j_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// slow is a statement that runs for many seconds on a real server.
const slow = "UNWIND range(1, 40000) AS a UNWIND range(1, 40000) AS b WITH a + b AS s WHERE s < 0 RETURN count(s) AS n"

// request is a request that the fake server of the cancel tests received.
type request struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters"`
	TxMetadata map[string]any `json:"txMetadata"`
}

// cancelServer is a fake server for the cancel tests. It holds the slow
// statement until the client leaves, or sends its first row and then holds
// it, and answers SHOW TRANSACTIONS, TERMINATE TRANSACTION and GET / with the
// responses that step 6 recorded.
type cancelServer struct {
	t         *testing.T
	discovery *dbimptest.Exchange
	show      *dbimptest.Exchange
	terminate *dbimptest.Exchange
	// firstRow makes the slow statement send its first row before it holds.
	firstRow bool

	mu   sync.Mutex
	reqs []request
}

// newCancelServer returns a cancelServer that answers GET / as the release
// discovery does.
func newCancelServer(t *testing.T, discovery string) *cancelServer {
	t.Helper()
	s := &cancelServer{t: t}
	exs, _ := exchanges(t, ceiling)
	for _, ex := range exs {
		switch st := statementOf(ex); {
		case s.show == nil && strings.HasPrefix(st, "SHOW TRANSACTIONS YIELD transactionId, currentQuery WHERE"):
			s.show = ex
		case s.terminate == nil && st == "TERMINATE TRANSACTION $id" && ex.Response.Status == http.StatusAccepted:
			s.terminate = ex
		}
	}
	exs, _ = exchanges(t, discovery)
	for _, ex := range exs {
		if ex.Request.Method == http.MethodGet && ex.Request.Path == "/" {
			s.discovery = ex
		}
	}
	if s.show == nil || s.terminate == nil || s.discovery == nil {
		t.Fatal("the recordings hold no SHOW TRANSACTIONS, TERMINATE TRANSACTION or GET /")
	}
	return s
}

func (s *cancelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req request
	_ = json.Unmarshal(body, &req)
	for k, v := range req.Parameters {
		req.Parameters[k] = plain(v)
	}
	for k, v := range req.TxMetadata {
		req.TxMetadata[k] = plain(v)
	}
	s.mu.Lock()
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()
	reply := func(ex *dbimptest.Exchange) {
		w.Header().Set("Content-Type", ex.Response.Header.Get("Content-Type"))
		w.WriteHeader(ex.Response.Status)
		_, _ = w.Write(ex.Response.Content())
	}
	switch {
	case r.Method == http.MethodGet:
		reply(s.discovery)
	case strings.HasPrefix(req.Statement, "SHOW TRANSACTIONS"):
		reply(s.show)
	case strings.HasPrefix(req.Statement, "TERMINATE TRANSACTION"):
		reply(s.terminate)
	case strings.HasPrefix(req.Statement, few):
		w.Header().Set("Content-Type", "application/vnd.neo4j.query")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"data":{"fields":["n"],"values":[[{"$type":"Integer","_value":"1"}],[{"$type":"Integer","_value":"2"}]]},"bookmarks":["b"]}`)
	case strings.HasPrefix(req.Statement, slow):
		if s.firstRow {
			w.Header().Set("Content-Type", "application/vnd.neo4j.query")
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"data":{"fields":["n"],"values":[[{"$type":"Integer","_value":"0"}]`)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		<-r.Context().Done()
	default:
		s.t.Errorf("the fake server received %s", body)
		http.Error(w, "unknown", http.StatusTeapot)
	}
}

// requests returns what the server received, in order.
func (s *cancelServer) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.reqs...)
}

// run sends the slow statement with a context that ends after 300ms, and
// returns what the server received and the error.
func (s *cancelServer) run(t *testing.T, query string) ([]request, error) {
	t.Helper()
	dbimptest.CheckGoroutines(t)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	db := open(t, srv.URL, query)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	rows, err := db.QueryContext(ctx, slow)
	if err != nil {
		return s.requests(), err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return s.requests(), rows.Err()
}

// shownID is the id of the transaction in the recorded answer of SHOW
// TRANSACTIONS.
func (s *cancelServer) shownID(t *testing.T) string {
	t.Helper()
	var b struct {
		Data struct {
			Values [][]struct {
				Value string `json:"_value"`
			} `json:"values"`
		} `json:"data"`
	}
	if err := json.Unmarshal(s.show.Response.Content(), &b); err != nil || len(b.Data.Values) == 0 {
		t.Fatalf("reading the recorded answer of SHOW TRANSACTIONS: %v", err)
	}
	return b.Data.Values[0][0].Value
}

// TestCancelTag holds cancel=tag, the default (D67 and D95): the statement
// ends with the comment of the connection, and when the context ends, the driver finds the
// statement by the comment and terminates it.
func TestCancelTag(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	for _, firstRow := range []bool{false, true} {
		s := newCancelServer(t, ceiling)
		s.firstRow = firstRow
		reqs, err := s.run(t, "")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("the error is %v, want context.DeadlineExceeded", err)
		}
		if len(reqs) != 3 {
			t.Fatalf("the server received %d requests, want the statement, SHOW TRANSACTIONS and TERMINATE: %+v", len(reqs), reqs)
		}
		tag := tagRE.FindString(reqs[0].Statement)
		if tag == "" || reqs[0].TxMetadata != nil {
			t.Errorf("the statement %q has no comment of the connection at its end, or has txMetadata", reqs[0].Statement)
		}
		if got := reqs[1].Parameters["tag"]; got != tag {
			t.Errorf("SHOW TRANSACTIONS looked for %v, want %q", got, tag)
		}
		if got := reqs[2].Parameters["id"]; got != s.shownID(t) {
			t.Errorf("TERMINATE TRANSACTION stopped %v, want %q", got, s.shownID(t))
		}
	}
}

// TestCancelNone holds cancel=none: the driver sends nothing more.
func TestCancelNone(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	s := newCancelServer(t, ceiling)
	reqs, err := s.run(t, "?cancel=none")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	if len(reqs) != 1 || tagRE.MatchString(reqs[0].Statement) {
		t.Errorf("the server received %+v, want the statement alone, with no comment", reqs)
	}
}

// TestCancelMetadata holds cancel=metadata: on 2026.09.0 the driver names the
// connection in txMetadata and finds the transaction by it, and on 5.26.31,
// which refuses txMetadata, it uses the comment (D67 and D95).
func TestCancelMetadata(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	s := newCancelServer(t, ceiling)
	reqs, err := s.run(t, "?cancel=metadata")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	var stmts []request
	for _, r := range reqs {
		if r.Statement != "" {
			stmts = append(stmts, r)
		}
	}
	if len(stmts) != 3 {
		t.Fatalf("the server received %+v, want the statement, SHOW TRANSACTIONS and TERMINATE", reqs)
	}
	id, _ := stmts[0].TxMetadata["dbimp"].(string)
	if id == "" || tagRE.MatchString(stmts[0].Statement) {
		t.Errorf("the statement is %+v, want txMetadata and no comment", stmts[0])
	}
	if got := stmts[1].Parameters["id"]; got != id || !strings.Contains(stmts[1].Statement, "metaData") {
		t.Errorf("SHOW TRANSACTIONS is %+v, want a search of metaData for %q", stmts[1], id)
	}

	s = newCancelServer(t, floor)
	reqs, _ = s.run(t, "?cancel=metadata")
	for _, r := range reqs {
		if r.Statement == slow || strings.HasPrefix(r.Statement, slow) {
			if !tagRE.MatchString(r.Statement) || r.TxMetadata != nil {
				t.Errorf("on 5.26.31 the statement is %+v, want the comment and no txMetadata", r)
			}
		}
	}
}

// few is a statement whose whole answer the fake server sends at once.
const few = "UNWIND [1, 2] AS n RETURN n"

// TestCancelEarlyClose holds D105: a Close before the end stops a statement
// that still runs on the server, and sends nothing more for an answer that is
// already in the buffer, as for QueryRow.
func TestCancelEarlyClose(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	//nolint:paralleltest // The subtests count the goroutines of the process too.
	for _, tt := range []struct {
		name, statement string
		want            int
	}{
		{"running", slow, 3},
		{"complete", few, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dbimptest.CheckGoroutines(t)
			s := newCancelServer(t, ceiling)
			s.firstRow = true
			srv := httptest.NewServer(s)
			t.Cleanup(srv.Close)
			db := open(t, srv.URL, "")
			rows, err := db.QueryContext(t.Context(), tt.statement)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() {
				t.Fatalf("no first row: %v", rows.Err())
			}
			if err := rows.Close(); err != nil {
				t.Errorf("closing the rows early: %v", err)
			}
			if reqs := s.requests(); len(reqs) != tt.want {
				t.Errorf("the server received %d requests, want %d: %+v", len(reqs), tt.want, reqs)
			}
		})
	}
}
