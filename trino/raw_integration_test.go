package trino_test

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/trino"
)

// rawResult is what a client that sends its own headers got from a statement.
type rawResult struct {
	// status is the status of the answer to the POST.
	status int
	// header holds the headers of the answer to the POST.
	header http.Header
	// id is the id of the query.
	id string
	// rows are the rows of the whole result.
	rows [][]any
	// pageSizes holds the count of the rows of each page that had rows.
	pageSizes []int
	// errName and errMessage are the error of the last page, or the text of an
	// answer that is not a page.
	errName    string
	errMessage string
	// state is the state of the last page.
	state string
	// headers holds every header of every answer, such as the Set-Path of SET PATH.
	headers http.Header
}

// rawRequest is a statement that a test sends with its own headers.
type rawRequest struct {
	statement string
	// headers are the headers of the POST, by name. The names that start with
	// Trino or Presto are written with the prefix of the flavor by with.
	headers map[string]string
	// pollQuery is added to the query of each poll, such as targetResultSize.
	pollQuery string
	// noUser leaves out the header of the user.
	noUser bool
}

// base returns the URL of the coordinator.
func (r release) base(t testing.TB) string {
	t.Helper()
	cfg, err := trino.ParseDSN(r.dsn)
	if err != nil {
		t.Fatal(err)
	}
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)
}

// prefix returns the start of the names of the headers of the flavor.
func (r release) prefix() string {
	if r.isPresto() {
		return "X-Presto-"
	}
	return "X-Trino-"
}

// raw sends a statement with net/http, and follows each nextUri to the end, as
// a client of its own would. The tests of the headers that the driver does not
// send use it, such as a statement with no header of the transaction.
func (e env) raw(t *testing.T, q rawRequest) rawResult {
	t.Helper()
	ctx := t.Context()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	base := e.base(t)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/statement", strings.NewReader(q.statement))
	if err != nil {
		t.Fatal(err)
	}
	if !q.noUser {
		req.Header.Set(e.prefix()+"User", "trino")
	}
	req.Header.Set(e.prefix()+"Catalog", "memory")
	req.Header.Set(e.prefix()+"Schema", schemaName)
	req.Header.Set("Content-Type", "text/plain")
	for k, v := range q.headers {
		req.Header.Set(k, v)
	}
	res := rawResult{headers: http.Header{}}
	for step := 0; req != nil; step++ {
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("sending %s %s: %v", req.Method, req.URL.Path, err)
		}
		var body io.Reader = resp.Body
		if resp.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(resp.Body)
			if err != nil {
				t.Fatalf("reading the gzip answer: %v", err)
			}
			body = zr
		}
		data, err := io.ReadAll(body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("reading the answer: %v", err)
		}
		if step == 0 {
			res.status, res.header = resp.StatusCode, resp.Header
		}
		for k, v := range resp.Header {
			res.headers[k] = append(res.headers[k], v...)
		}
		var pg struct {
			ID      string  `json:"id"`
			NextURI string  `json:"nextUri"`
			Data    [][]any `json:"data"`
			Stats   struct {
				State string `json:"state"`
			} `json:"stats"`
			Error *struct {
				Name    string `json:"errorName"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(data, &pg) != nil {
			res.errMessage = strings.TrimSpace(string(data))
			res.status = resp.StatusCode
			return res
		}
		res.id, res.state = pg.ID, pg.Stats.State
		if len(pg.Data) > 0 {
			res.rows = append(res.rows, pg.Data...)
			res.pageSizes = append(res.pageSizes, len(pg.Data))
		}
		if pg.Error != nil {
			res.errName, res.errMessage = pg.Error.Name, pg.Error.Message
		}
		if pg.NextURI == "" {
			return res
		}
		u, err := url.Parse(pg.NextURI)
		if err != nil {
			t.Fatal(err)
		}
		next := base + u.EscapedPath() + "?" + u.RawQuery
		if q.pollQuery != "" {
			if u.RawQuery != "" {
				next += "&"
			}
			next += q.pollQuery
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !q.noUser {
			req.Header.Set(e.prefix()+"User", "trino")
		}
		for k, v := range q.headers {
			if strings.Contains(k, "Authorization") {
				req.Header.Set(k, v)
			}
		}
	}
	return res
}

// basic returns the value of the header Authorization for a user and a password.
func basic(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// marker returns text that starts a statement as a comment, so that a test can find
// its query in system.runtime.queries.
func marker(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("/* dbimp-%s-%d */", suffix, time.Now().UnixNano()%1e6)
}

// queryState returns the state of the query whose text starts with the marker, or ""
// when no query does. The statement that asks holds the marker too, as the text of
// its EXECUTE, so a query matches only when its text starts with it.
func (e env) queryState(t *testing.T, mark string) string {
	t.Helper()
	rows := e.rows(t, "SELECT state FROM system.runtime.queries WHERE starts_with(query, ?)", mark)
	if len(rows) == 0 {
		return ""
	}
	state, _ := rows[0][0].(string)
	return state
}

// waitState waits until the query that holds the marker has one of the states, for
// 15 seconds at most, and returns its state.
func (e env) waitState(t *testing.T, mark string, states ...string) string {
	t.Helper()
	var state string
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		state = e.queryState(t, mark)
		if slices.Contains(states, state) {
			return state
		}
	}
	return state
}

// bigQuery is a statement of 9 million rows, which fills the output buffer of the
// server and then waits for the client, so it runs until somebody cancels it.
func bigQuery(mark string) string {
	return mark + " SELECT a.n, b.n, rpad('x', 100, 'y') FROM UNNEST(sequence(1, 3000)) AS a(n) CROSS JOIN UNNEST(sequence(1, 3000)) AS b(n)"
}
