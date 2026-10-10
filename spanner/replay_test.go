package spanner //nolint:testpackage // The tests point a connector at a fake server, and read the state of a connector, which only the package can do.

import (
	"bytes"
	"database/sql"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from the hosted
// service, under testdata/spanner/. Each one decodes a real answer through the
// driver. The recordings are the answers of one session, and the fake server
// answers a statement of any session of the driver with them. The recorder sent
// most statements to executeSql, which answers one ResultSet, and the driver
// sends executeStreamingSql. So the fake server turns the ResultSet of a
// recorded answer into the one message of a stream that carries the same
// metadata, rows and stats, and it sends a recorded stream as it is. A recorded
// error needs no change.

const testdata = "../testdata/spanner"

// exchange is a recorded exchange with its number, which is the number in its
// file name, and one more than its place in the script of requests.json.
type exchange struct {
	*dbimptest.Exchange

	n int
}

// recorded holds every recorded exchange, read once, in the order of the files.
var recorded = sync.OnceValues(func() ([]exchange, error) {
	paths, err := filepath.Glob(filepath.Join(testdata, "spanner-*.json"))
	if err != nil {
		return nil, err
	}
	var out []exchange
	for _, path := range paths {
		ex, err := dbimptest.ReadExchange(path)
		if err != nil {
			return nil, err
		}
		var n int
		if _, err := fmt.Sscanf(filepath.Base(path), "spanner-%d-", &n); err != nil {
			return nil, err
		}
		out = append(out, exchange{ex, n})
	}
	return out, nil
})

// file returns the recorded exchange with the number n in its file name.
func file(tb testing.TB, n int) exchange {
	tb.Helper()
	all, err := recorded()
	if err != nil {
		tb.Fatalf("reading the recorded exchanges: %v", err)
	}
	for _, ex := range all {
		if ex.n == n {
			return ex
		}
	}
	tb.Fatalf("no recorded exchange has the number %d", n)
	return exchange{}
}

// session returns the name of the session that the recorded exchange n made.
func session(tb testing.TB, n int) string {
	tb.Helper()
	var s struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(file(tb, n).Response.Content(), &s); err != nil {
		tb.Fatal(err)
	}
	return s.Name
}

// split returns the path of a request without its verb, with the session of the
// recorder and the multiplexed session written as one, and the verb. The
// recorded statements ran in one session, and the driver runs in another.
func split(tb testing.TB, path string) (string, string) {
	tb.Helper()
	main, multi := session(tb, 10), session(tb, 12)
	for _, s := range []string{main, multi} {
		path = strings.ReplaceAll(path, s, "SESSION")
	}
	slash := strings.LastIndex(path, "/")
	if colon := strings.Index(path[slash:], ":"); colon >= 0 {
		return path[:slash+colon], path[slash+colon+1:]
	}
	return path, ""
}

// sqlVerb reports whether the verb runs a statement.
func sqlVerb(verb string) bool {
	return verb == "executeSql" || verb == "executeStreamingSql"
}

// sameField reports whether the JSON objects a and b have the same value for
// the member name. It uses the canonical form of the value.
func sameMember(a, b []byte, name string) bool {
	return member(a, name) == member(b, name)
}

// member returns the canonical text of the member name of the object in b, and ""
// when b has none.
func member(b []byte, name string) string {
	var m map[string]jsontext.Value
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	v, ok := m[name]
	if !ok {
		return ""
	}
	v = bytes.Clone(v)
	if v.Canonicalize() != nil {
		return string(v)
	}
	return string(v)
}

// match reports whether the exchange ex answers the request r. It matches the
// method, the path and the part of the body that names the work: the statement,
// its transaction and its parameters, the options of a transaction, the id of a commit, the
// statements of a DDL call.
func match(tb testing.TB, r *http.Request, body []byte, ex exchange) bool {
	tb.Helper()
	if r.Method != ex.Request.Method {
		return false
	}
	rp, rv := split(tb, r.URL.Path)
	ep, ev := split(tb, ex.Request.Path)
	if rp != ep || rv != ev && (!sqlVerb(rv) || !sqlVerb(ev)) {
		return false
	}
	eb := ex.Request.Content()
	switch {
	case sqlVerb(rv):
		return sameMember(body, eb, "sql") && sameMember(body, eb, "transaction") && sameMember(body, eb, "params") && sameMember(body, eb, "paramTypes")
	case rv == "beginTransaction":
		return sameMember(body, eb, "options")
	case rv == "commit" || rv == "rollback":
		return sameMember(body, eb, "transactionId")
	case rv == "" && strings.HasSuffix(rp, "/sessions"):
		return sameMember(body, eb, "session")
	case strings.HasSuffix(rp, "/ddl") && r.Method == http.MethodPatch:
		return sameMember(body, eb, "statements")
	}
	return true
}

// resultSet is the answer of executeSql.
type resultSet struct {
	Metadata  jsontext.Value     `json:"metadata,omitzero"`
	Rows      [][]jsontext.Value `json:"rows,omitzero"`
	Stats     jsontext.Value     `json:"stats,omitzero"`
	Precommit jsontext.Value     `json:"precommitToken,omitzero"`
}

// message is one message of a stream.
type message struct {
	Values    []jsontext.Value `json:"values"`
	Metadata  jsontext.Value   `json:"metadata,omitzero"`
	Stats     jsontext.Value   `json:"stats,omitzero"`
	Precommit jsontext.Value   `json:"precommitToken,omitzero"`
	Last      bool             `json:"last"`
}

// asStream returns the body of a stream of one message that holds what the
// ResultSet in body holds.
func asStream(tb testing.TB, body []byte) []byte {
	tb.Helper()
	var rs resultSet
	if err := json.Unmarshal(body, &rs); err != nil {
		tb.Fatalf("reading a recorded ResultSet: %v", err)
	}
	m := message{Values: []jsontext.Value{}, Metadata: rs.Metadata, Stats: rs.Stats, Precommit: rs.Precommit, Last: true}
	for _, row := range rs.Rows {
		m.Values = append(m.Values, row...)
	}
	out, err := json.Marshal([]message{m})
	if err != nil {
		tb.Fatal(err)
	}
	return out
}

// replayServer answers each request with a recorded exchange. A request that
// matches more than one gets the first that it did not get before, and then the
// last.
type replayServer struct {
	t *testing.T

	mu   sync.Mutex
	used map[int]bool
	log  []string
}

// newReplay starts a replay server, and returns it with its URL.
func newReplay(t *testing.T) (*replayServer, *httptest.Server) {
	t.Helper()
	s := &replayServer{t: t, used: map[int]bool{}}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *replayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading a request to the fake server: %v", err)
		return
	}
	all, err := recorded()
	if err != nil {
		s.t.Errorf("reading the recorded exchanges: %v", err)
		return
	}
	_, verb := split(s.t, r.URL.Path)
	s.mu.Lock()
	entry := r.Method + " " + verb
	if verb == "" {
		entry = r.Method + " " + r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	}
	s.log = append(s.log, entry)
	var found []exchange
	for _, ex := range all {
		if match(s.t, r, body, ex) {
			found = append(found, ex)
		}
	}
	// A recorded exchange of the same verb comes before one of the other verb
	// for a statement.
	slices.SortStableFunc(found, func(a, b exchange) int {
		_, av := split(s.t, a.Request.Path)
		_, bv := split(s.t, b.Request.Path)
		return b2i(bv == verb) - b2i(av == verb)
	})
	var ex exchange
	switch {
	case len(found) == 0:
		s.mu.Unlock()
		s.t.Errorf("no recorded exchange matches %s %s %s", r.Method, r.URL, body)
		http.Error(w, "no exchange matches", http.StatusTeapot)
		return
	default:
		ex = found[len(found)-1]
		for _, f := range found {
			if !s.used[f.n] {
				ex = f
				break
			}
		}
		s.used[ex.n] = true
	}
	s.mu.Unlock()
	res := ex.Response
	out := res.Content()
	_, ev := split(s.t, ex.Request.Path)
	if verb == "executeStreamingSql" && ev == "executeSql" && res.Status == http.StatusOK {
		out = asStream(s.t, out)
	}
	for key, vals := range res.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.Header().Del("Content-Length")
	w.WriteHeader(res.Status)
	_, _ = w.Write(out)
}

// requests returns the log of the requests that the server got.
func (s *replayServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.log)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// replayDB returns a database on a replay server. Its connector has no
// session, so the first statement makes one, as the real driver does.
func replayDB(t *testing.T) (*sql.DB, *replayServer, *Connector) {
	t.Helper()
	s, srv := newReplay(t)
	c := connector(config(), srv.URL, false)
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db, s, c
}

// TestReplayWithTheHelperOfDbimptest holds that the match function of this
// package serves dbimptest.Replay as well: a ping makes the session, checks the
// dialect, and runs SELECT 1, which the recorder ran as a stream.
func TestReplayWithTheHelperOfDbimptest(t *testing.T) {
	t.Parallel()
	srv := dbimptest.Replay(t, testdata, func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		return match(t, r, body, exchange{Exchange: ex}) && ex.Response.Status == http.StatusOK && !strings.HasSuffix(ex.Request.Path, ":executeSql")
	})
	c := connector(config(), srv.URL, false)
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("pinging the fake server: %v", err)
	}
}

// TestReplayConnect holds D191 items 3 and 10: the first statement checks the
// dialect of the database, makes a multiplexed session, and then runs, and a
// later statement makes no request for a session.
func TestReplayConnect(t *testing.T) {
	t.Parallel()
	db, s, _ := replayDB(t)
	for range 2 {
		var n int64
		if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("SELECT 1 gave %d, want 1", n)
		}
	}
	want := []string{"GET dbimp_test", "POST sessions", "POST executeStreamingSql", "POST executeStreamingSql"}
	if got := s.requests(); !slices.Equal(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
}
