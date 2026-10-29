package maxcompute //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// testdata holds the exchanges that step 6 recorded from a project.
const testdata = "../testdata/maxcompute"

// testHost is the host of the project of the tests. A test never sends a request
// to it, because each connector of the tests points at a fake server.
const testHost = "service.ap-southeast-5.maxcompute.aliyun.com"

// fastPoll is the interval of the poll of the tests, so that no test waits a fixed
// time.
const fastPoll = time.Millisecond

// recordedConfig is the configuration that the recorder used (step 6). The id and
// the secret are no real key.
func recordedConfig() Config {
	return Config{Host: testHost, ID: "test-id", Secret: "test-secret", Project: "dbimp", Region: "ap-southeast-5"}
}

// connectorAt returns a connector for cfg that talks to the fake server at base.
func connectorAt(tb testing.TB, cfg Config, base string) *Connector {
	tb.Helper()
	c := NewConnector(cfg)
	c.base = base
	c.pollMin, c.pollMax = fastPoll, 4*fastPoll
	return c
}

// open returns a database on the fake server at base, with the connector of cfg.
func open(t *testing.T, cfg Config, base string) *sql.DB {
	t.Helper()
	c := connectorAt(t, cfg, base)
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}

// exchange returns the recorded exchange with the number n, which is the number
// in the name of its file.
func exchange(tb testing.TB, n int) *dbimptest.Exchange {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, fmt.Sprintf("maxcompute-%03d-*.json", n)))
	if err != nil || len(paths) != 1 {
		tb.Fatalf("finding the recorded exchange %d: %v, %d files", n, err, len(paths))
	}
	ex, err := dbimptest.ReadExchange(paths[0])
	if err != nil {
		tb.Fatal(err)
	}
	return ex
}

// exchangeCount is the number of the last recorded exchange that the tests look
// at when they search.
const exchangeCount = 600

// flow is the three exchanges of a statement that ran: the creation of the
// instance, the poll of its state, and the read of its result.
type flow struct {
	create, status, result *dbimptest.Exchange
}

// recordedFlow returns the exchanges of the first statement whose query is text,
// without its semicolon, whose create answered 201 and whose state is the one in
// the poll that follows.
func recordedFlow(tb testing.TB, text, state string) flow {
	tb.Helper()
	for n := 1; n < exchangeCount; n++ {
		paths, _ := filepath.Glob(filepath.Join(testdata, fmt.Sprintf("maxcompute-%03d-*.json", n)))
		if len(paths) != 1 {
			break
		}
		ex := exchange(tb, n)
		if ex.Request.Method != http.MethodPost || ex.Response.Status != http.StatusCreated || normalize(queryOf(string(ex.Request.Content()))) != text {
			continue
		}
		next := exchange(tb, n+1)
		if !strings.Contains(string(next.Response.Content()), "<Status>"+state+"</Status>") {
			continue
		}
		return flow{create: ex, status: next, result: exchange(tb, n+2)}
	}
	tb.Fatalf("no recorded statement with %q that ended in %s", text, state)
	return flow{}
}

// normalize returns a query with no white space at its ends and no semicolon at its
// end.
func normalize(q string) string {
	return strings.TrimRight(strings.TrimSpace(q), "; \t\r\n")
}

// queryOf returns the text of the element Query of a request body, with its
// entities decoded.
func queryOf(body string) string {
	_, rest, ok := strings.Cut(body, "<Query>")
	if !ok {
		return ""
	}
	q, _, _ := strings.Cut(rest, "</Query>")
	return strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&#39;", "'", "&#34;", `"`, "&#xA;", "\n").Replace(q)
}

// respond returns the response of an exchange, as a client sees it.
func respond(req *http.Request, ex *dbimptest.Exchange) *http.Response {
	h := http.Header{}
	maps.Copy(h, ex.Response.Header)
	h.Del("Content-Length")
	content := ex.Response.Content()
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", ex.Response.Status, http.StatusText(ex.Response.Status)),
		StatusCode:    ex.Response.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(content)),
		ContentLength: int64(len(content)),
		Request:       req,
	}
}

// resultParts splits the recorded result of a SELECT at the start of its character
// data. The head ends with the opening of the data, and the tail starts with its
// end, so that a test puts rows between them (step 11).
func resultParts(tb testing.TB, ex *dbimptest.Exchange) (string, string) {
	tb.Helper()
	body := string(ex.Response.Content())
	i := strings.Index(body, cdataStart)
	j := strings.LastIndex(body, cdataEnd)
	if i < 0 || j < i {
		tb.Fatal("the recorded result holds no character data")
	}
	return body[:i+len(cdataStart)], body[j:]
}

// csvOf returns the CSV text that the recorded result ex holds.
func csvOf(tb testing.TB, ex *dbimptest.Exchange) string {
	tb.Helper()
	a, err := readAnswer(bytes.NewReader(ex.Response.Content()))
	if err != nil {
		tb.Fatal(err)
	}
	text, err := a.readText()
	if err != nil {
		tb.Fatal(err)
	}
	return text
}
