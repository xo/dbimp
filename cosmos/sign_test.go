package cosmos //nolint:testpackage // The tests call the function that signs a request, which is not exported.

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

func TestTarget(t *testing.T) {
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
		rtype, link := target(tt.path)
		if rtype != tt.rtype || link != tt.link {
			t.Errorf("%s: the target is %q and %q, want %q and %q", tt.path, rtype, link, tt.rtype, tt.link)
		}
	}
}

// TestSign holds the example of the documentation of Cosmos DB, which is the
// same vector as the test of the recorder in dbimptest/cmd/record. The master
// key below is the one that the example names, and the signature is the one
// that it gives.
func TestSign(t *testing.T) {
	t.Parallel()
	const key = "dsZQi3KtZmCv1ljt3VNWNm7sQUF1y5rJfC6kv5JiwvW0EndXdDku/dkKBp8/ufDToSxLzR4y+O/0H/t4bQtVNw=="
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/dbs/ToDoList", nil)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2017, time.April, 27, 0, 51, 12, 0, time.UTC)
	if err := sign(req, key, when); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Ms-Date"); got != "thu, 27 apr 2017 00:51:12 gmt" {
		t.Errorf("the date is %q", got)
	}
	if got := req.Header.Get("X-Ms-Version"); got != apiVersion {
		t.Errorf("the version is %q, want %q", got, apiVersion)
	}
	want := "type%3Dmaster%26ver%3D1.0%26sig%3Dc09PEVJrgp2uQRkr934kFbTqhByc7TVr3OHyqlu%2Bc%2Bc%3D"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("the authorization is %q, want %q", got, want)
	}
}

// TestSignRefusesAKeyThatIsNotBase64 holds that the error names no key (D94).
func TestSignRefusesAKeyThatIsNotBase64(t *testing.T) {
	t.Parallel()
	const key = "not base64 text, which a secret could be"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	err = sign(req, key, time.Now())
	if !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Fatalf("the error is %v, want %v", err, dbimp.ErrInvalidValue)
	}
	if got := err.Error(); strings.Contains(got, key) {
		t.Errorf("the error holds the key: %q", got)
	}
}
