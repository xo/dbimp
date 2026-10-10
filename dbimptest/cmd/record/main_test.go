package main

import (
	"net/http"
	"net/url"
	"strings"
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
