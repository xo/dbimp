package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

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
