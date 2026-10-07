package main

import (
	"net/http"
	"testing"
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
