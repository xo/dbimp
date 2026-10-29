package main

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: the test computes the MD5 that Tablestore asks for.
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

func TestHostless(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"http://127.0.0.1:8080/v1/statement/queued/q1/y1/1?slug=x": "/v1/statement/queued/q1/y1/1?slug=x",
		"/v1/statement": "/v1/statement",
		"/db/query?q=1": "/db/query?q=1",
		"":              "",
	} {
		if got := hostless(in); got != want {
			t.Errorf("hostless(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCaptureHeaders(t *testing.T) {
	t.Parallel()
	h := http.Header{"X-Trino-Started-Transaction-Id": {"tx1"}}
	kept := map[string]string{}
	captureHeaders(h, map[string]string{"tx": "header:X-Trino-Started-Transaction-Id", "no": "header:X-Missing", "body": "nextUri"}, kept)
	if len(kept) != 1 || kept["tx"] != "tx1" {
		t.Errorf("kept %v, want the transaction id only", kept)
	}
}

func TestCaptureSkipsHeadersAndEmptyBodies(t *testing.T) {
	t.Parallel()
	kept := map[string]string{}
	if err := capture(nil, map[string]string{"a": "nextUri"}, kept); err != nil || len(kept) != 0 {
		t.Errorf("an empty body kept %v, %v, want nothing and no error", kept, err)
	}
	if err := capture([]byte(`{"id":"q1"}`), map[string]string{"id": "id", "tx": "header:X", "next": "nextUri"}, kept); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept["id"] != "q1" {
		t.Errorf("kept %v, want the id only: a header is not a member, and a missing member is skipped", kept)
	}
}

func TestAuthorizeFollowsTheFormOfTheRequest(t *testing.T) {
	t.Parallel()
	base := &url.URL{Scheme: "https", Host: "example.test", User: url.UserPassword("key", "secret")}
	tests := []struct {
		name  string
		r     Request
		wrong bool
		want  func(*http.Request) bool
	}{
		{"basic", Request{}, false, func(req *http.Request) bool {
			u, p, ok := req.BasicAuth()
			return ok && u == "key" && p == "secret"
		}},
		{"bearer", Request{auth: dbimp.AuthBearer}, false, func(req *http.Request) bool {
			return req.Header.Get("Authorization") == "Bearer secret"
		}},
		{"a wrong bearer", Request{auth: dbimp.AuthBearer}, true, func(req *http.Request) bool {
			return req.Header.Get("Authorization") == "Bearer secret-wrong"
		}},
		{"no credentials", Request{auth: dbimp.AuthBearer, Auth: "none"}, false, func(req *http.Request) bool {
			return req.Header.Get("Authorization") == ""
		}},
		{"sigv4", Request{auth: authSigV4, region: "us-east-1", service: "dynamodb"}, false, func(req *http.Request) bool {
			return strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ")
		}},
		{"odps", Request{auth: authODPS, region: "ap-southeast-5"}, false, func(req *http.Request) bool {
			return strings.HasPrefix(req.Header.Get("Authorization"), "ODPS key/") && req.Header.Get("Date") != ""
		}},
		{"ots", Request{auth: authOTS}, false, func(req *http.Request) bool {
			return req.Header.Get("X-Ots-Accesskeyid") == "key" && req.Header.Get("X-Ots-Signature") != ""
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/x", nil)
			if err != nil {
				t.Fatal(err)
			}
			authorize(req, base, tt.r, nil, tt.wrong)
			if !tt.want(req) {
				t.Errorf("the headers are %v, and do not hold the form of %s", req.Header, tt.name)
			}
		})
	}
}

func TestCosmosTarget(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path, rtype, link string
	}{
		{"/", "", ""},
		{"/dbs", "dbs", ""},
		{"/dbs/db", "dbs", "dbs/db"},
		{"/dbs/db/colls", "colls", "dbs/db"},
		{"/dbs/db/colls/c", "colls", "dbs/db/colls/c"},
		{"/dbs/db/colls/c/docs", "docs", "dbs/db/colls/c"},
		{"/dbs/db/colls/c/docs/d?x=1", "docs", "dbs/db/colls/c/docs/d"},
		{"/dbs/db/colls/c/pkranges", "pkranges", "dbs/db/colls/c"},
	} {
		rtype, link := cosmosTarget(tt.path)
		if rtype != tt.rtype || link != tt.link {
			t.Errorf("%s: the target is %q and %q, want %q and %q", tt.path, rtype, link, tt.rtype, tt.link)
		}
	}
}

// TestSignCosmos holds the example of the documentation of Cosmos DB: the master key
// below is the one that the example names, and the signature is the one that it gives.
func TestSignCosmos(t *testing.T) {
	t.Parallel()
	const key = "dsZQi3KtZmCv1ljt3VNWNm7sQUF1y5rJfC6kv5JiwvW0EndXdDku/dkKBp8/ufDToSxLzR4y+O/0H/t4bQtVNw=="
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/dbs/ToDoList", nil)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2017, time.April, 27, 0, 51, 12, 0, time.UTC)
	if err := signCosmos(req, key, when); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Ms-Date"); got != "thu, 27 apr 2017 00:51:12 gmt" {
		t.Errorf("the date is %q", got)
	}
	want := "type%3Dmaster%26ver%3D1.0%26sig%3Dc09PEVJrgp2uQRkr934kFbTqhByc7TVr3OHyqlu%2Bc%2Bc%3D"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("the authorization is %q, want %q", got, want)
	}
}

func TestIsRecordingKeepsTheFilesOfAnotherRelease(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, release string
		want          bool
	}{
		{"bigquery-001-post--queries.json", "bigquery", true},
		{"bigquery-1000-get.json", "bigquery", true},
		{"bigquery-0.7.2-001-post--queries.json", "bigquery", false},
		{"bigquery-0.7.2-001-post--queries.json", "bigquery-0.7.2", true},
		{"cosmos-EN20260907-001-get.json", "cosmos", false},
		{"cosmos-001.json", "cosmos", false},
		{"requests.json", "cosmos", false},
	} {
		if got := isRecording(tt.name, tt.release); got != tt.want {
			t.Errorf("isRecording(%q, %q) is %t, want %t", tt.name, tt.release, got, tt.want)
		}
	}
}

func TestSignODPS(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, time.October, 29, 1, 2, 3, 0, time.UTC)
	sign := func(secret string) *http.Request {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/api/projects/p/instances?curr_project=p", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/xml")
		signODPS(req, "/api", "key", secret, "ap-southeast-5", when)
		return req
	}
	a, b := sign("secret"), sign("secret")
	if got := a.Header.Get("Date"); got != "Thu, 29 Oct 2026 01:02:03 GMT" {
		t.Errorf("the date is %q", got)
	}
	const prefix = "ODPS key/20261029/ap-southeast-5/odps/aliyun_v4_request:"
	if got := a.Header.Get("Authorization"); !strings.HasPrefix(got, prefix) {
		t.Errorf("the authorization is %q, want the prefix %q", got, prefix)
	}
	if a.Header.Get("Authorization") != b.Header.Get("Authorization") {
		t.Error("the same request gave two signatures")
	}
	if c := sign("other"); c.Header.Get("Authorization") == a.Header.Get("Authorization") {
		t.Error("a different secret gave the same signature")
	}
}

func TestSignOTS(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, time.October, 29, 1, 2, 3, 0, time.UTC)
	sign := func(payload string) *http.Request {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://inst.ap-southeast-5.ots.aliyuncs.com/SQLQuery", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		signOTS(req, []byte(payload), "key", "secret", when)
		return req
	}
	a := sign("abc")
	sum := md5.Sum([]byte("abc")) //nolint:gosec // G401: the test computes the MD5 that Tablestore asks for.
	for name, want := range map[string]string{
		"x-ots-instancename": "inst",
		"x-ots-accesskeyid":  "key",
		"x-ots-apiversion":   "2015-12-31",
		"x-ots-date":         "2026-10-29T01:02:03.000Z",
		"x-ots-contentmd5":   base64.StdEncoding.EncodeToString(sum[:]),
	} {
		if got := a.Header.Get(name); got != want {
			t.Errorf("the header %s is %q, want %q", name, got, want)
		}
	}
	if a.Header.Get("X-Ots-Signature") == sign("abd").Header.Get("X-Ots-Signature") {
		t.Error("a different body gave the same signature")
	}
}

// resetTransport fails with a reset for the first fails calls, and then answers.
type resetTransport struct{ fails, calls int }

func (r *resetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	if r.calls <= r.fails {
		return nil, syscall.ECONNRESET
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func TestDoRetry(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		method string
		retry  bool
		calls  int
		reset  bool
	}{
		{"a get is sent again", http.MethodGet, false, 2, false},
		{"a post that retries is sent again", http.MethodPost, true, 2, false},
		{"a post is sent once", http.MethodPost, false, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := &resetTransport{fails: 1}
			req, err := http.NewRequestWithContext(t.Context(), tt.method, "https://example.test/x", strings.NewReader("body"))
			if err != nil {
				t.Fatal(err)
			}
			res, err := doRetry(t.Context(), &http.Client{Transport: tr}, req, tt.retry)
			if got := errors.Is(err, syscall.ECONNRESET); got != tt.reset {
				t.Errorf("the error is %v, want a reset: %t", err, tt.reset)
			}
			if err == nil {
				res.Body.Close()
			}
			if tr.calls != tt.calls {
				t.Errorf("the transport ran %d times, want %d", tr.calls, tt.calls)
			}
		})
	}
}

func TestCaptureOfHeadersOnlyReadsNoBody(t *testing.T) {
	t.Parallel()
	kept := map[string]string{}
	if err := capture([]byte("<Error/>"), map[string]string{"loc": "header:Location"}, kept); err != nil {
		t.Errorf("an XML body with a capture of headers only gave %v", err)
	}
}

// handshakeTimeout is the error that a TLS handshake that ran out of time gives.
type handshakeTimeoutError struct{}

func (handshakeTimeoutError) Error() string   { return "net/http: TLS handshake timeout" }
func (handshakeTimeoutError) Timeout() bool   { return true }
func (handshakeTimeoutError) Temporary() bool { return true }

func TestTransient(t *testing.T) {
	t.Parallel()
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	for _, tt := range []struct {
		name  string
		ended bool
		err   error
		want  bool
	}{
		{"a reset", false, syscall.ECONNRESET, true},
		{"a handshake that timed out", false, handshakeTimeoutError{}, true},
		{"a timeout after the context ended", true, handshakeTimeoutError{}, false},
		{"another error", false, io.ErrUnexpectedEOF, false},
	} {
		ctx := t.Context()
		if tt.ended {
			ctx = ended
		}
		if got := transient(ctx, tt.err); got != tt.want {
			t.Errorf("%s: transient is %t, want %t", tt.name, got, tt.want)
		}
	}
}
