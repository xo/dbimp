package maxcompute //nolint:testpackage // The tests read the signature, which is not exported.

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignSetsTheHeaders(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, time.October, 29, 1, 2, 3, 0, time.UTC)
	sign1 := func(secret, query string) *http.Request {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/api/projects/p/instances"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/xml")
		sign(req, "key", secret, "ap-southeast-5", when)
		return req
	}
	a, b := sign1("secret", "?curr_project=p"), sign1("secret", "?curr_project=p")
	if got := a.Header.Get("Date"); got != "Thu, 29 Oct 2026 01:02:03 GMT" {
		t.Errorf("the date is %q", got)
	}
	const prefix = "ODPS key/20261029/ap-southeast-5/odps/aliyun_v4_request:"
	if got := a.Header.Get("Authorization"); !strings.HasPrefix(got, prefix) {
		t.Errorf("the authorization is %q, want the prefix %q (D221 item 1)", got, prefix)
	}
	if a.Header.Get("Authorization") != b.Header.Get("Authorization") {
		t.Error("the same request gave two signatures")
	}
	if sign1("other", "?curr_project=p").Header.Get("Authorization") == a.Header.Get("Authorization") {
		t.Error("a different secret gave the same signature")
	}
	if sign1("secret", "?curr_project=q").Header.Get("Authorization") == a.Header.Get("Authorization") {
		t.Error("a different query gave the same signature")
	}
}

func TestSortedQuery(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.test/x?taskstatus&curr_project=p", nil)
	if got := sortedQuery(req.URL.Query()); got != "?curr_project=p&taskstatus" {
		t.Errorf("sortedQuery = %q, want the keys in order, and a key with no value alone", got)
	}
	if got := sortedQuery(nil); got != "" {
		t.Errorf("sortedQuery of no query = %q", got)
	}
}
