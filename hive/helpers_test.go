package hive_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// testdata is the folder of the recordings of step 6.
const testdata = "../testdata/hive"

// password is the password that the recorder wrote in place of the real one in
// each OpenSession of the recordings. It has the length of the real password, so
// that the request is a message of Thrift.
const password = "REDACTED**"

// recorded is one file of the recordings.
type recorded struct {
	path string
	ex   *dbimptest.Exchange
	call string
	stmt string
}

// callOf reads the name of the call in a request, and the statement of an
// ExecuteStatement.
func callOf(b []byte) (string, string) {
	d := dbimp.NewThriftDecoder(bytes.NewReader(b))
	name, _, _, err := d.ReadMessage()
	if err != nil {
		return "", ""
	}
	var stmt string
	// The args struct holds the request as its field 1, and the statement is
	// the field 2 of the request.
	if t, _, err := d.ReadField(); err != nil || t != dbimp.ThriftStruct {
		return name, ""
	}
	for {
		t, id, err := d.ReadField()
		if err != nil || t == dbimp.ThriftStop {
			return name, stmt
		}
		if id == 2 && t == dbimp.ThriftString {
			stmt, _ = d.ReadString()
			continue
		}
		if err := d.Skip(t); err != nil {
			return name, stmt
		}
	}
}

// load reads the recordings of release in the order of their numbers.
func load(tb testing.TB, release string) []recorded {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, release+"-*.json"))
	if err != nil {
		tb.Fatal(err)
	}
	num := func(p string) int {
		m := regexp.MustCompile(`-(\d+)-[^-]+--`).FindStringSubmatch(filepath.Base(p))
		n, _ := strconv.Atoi(m[1])
		return n
	}
	slices.SortFunc(paths, func(a, b string) int { return num(a) - num(b) })
	var out []recorded
	for _, p := range paths {
		ex, err := dbimptest.ReadExchange(p)
		if err != nil {
			tb.Fatal(err)
		}
		call, stmt := callOf(ex.Request.Content())
		out = append(out, recorded{path: p, ex: ex, call: call, stmt: stmt})
	}
	if len(out) == 0 {
		tb.Fatalf("no recording of %s in %s", release, testdata)
	}
	return out
}

// session returns the recordings of the first session of release that ran the
// statement, from its OpenSession to its CloseSession.
func session(tb testing.TB, release, statement string) []recorded {
	tb.Helper()
	all := load(tb, release)
	start := 0
	for i, r := range all {
		switch {
		case r.call == "OpenSession":
			start = i
		case r.call == "ExecuteStatement" && r.stmt == statement:
			end := i
			for end < len(all) && all[end].call != "CloseSession" {
				end++
			}
			return all[start:min(end+1, len(all))]
		}
	}
	tb.Fatalf("no session of %s ran %q", release, statement)
	return nil
}

// ranker answers the k-th request with a body with the k-th recorded response
// to that body, and the last one after that, so that the polls of a statement
// and the pages of a result replay in order.
type ranker struct {
	mu     sync.Mutex
	order  map[string][][]byte
	counts map[string]int
	cur    *http.Request
	want   int
}

func newRanker(rec []recorded) *ranker {
	rk := &ranker{order: map[string][][]byte{}, counts: map[string]int{}}
	for _, r := range rec {
		k := string(r.ex.Request.Content())
		rk.order[k] = append(rk.order[k], r.ex.Response.Content())
	}
	return rk
}

func (rk *ranker) match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	rk.mu.Lock()
	defer rk.mu.Unlock()
	list := rk.order[string(body)]
	if rk.cur != r {
		rk.cur = r
		k := rk.counts[string(body)]
		rk.counts[string(body)]++
		rk.want = min(k, len(list)-1)
	}
	return len(list) > 0 && bytes.Equal(ex.Request.Content(), body) && bytes.Equal(ex.Response.Content(), list[rk.want])
}

// replay starts a fake server that answers the session of release that ran the
// statement, and returns its DSN.
func replay(t *testing.T, release, statement string) string {
	t.Helper()
	rec := session(t, release, statement)
	dir := t.TempDir()
	for _, r := range rec {
		b, err := os.ReadFile(r.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(r.path)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	srv := dbimptest.Replay(t, dir, newRanker(rec).match)
	return dsnOf(release, srv)
}

// dsnOf returns the DSN of a replay server for release.
func dsnOf(release string, srv *httptest.Server) string {
	user := "hive"
	if strings.HasPrefix(release, "impala") {
		user = "impala"
	}
	return strings.Replace(srv.URL, "http://", "hive://"+user+":"+password+"@", 1)
}

// releases are the releases that the replay tests run on.
var releases = []string{"hivehttp-4.2.1", "impalahttp-4.5.3"}
