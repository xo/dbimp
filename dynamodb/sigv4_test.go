package dynamodb //nolint:testpackage // The test sets the clock of the connector, which is not exported.

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSignV4OfARequest holds what the driver signs: the host, the content
// type, the target and the date, in the scope of the region and the service
// dynamodb (D169). With a session token, it also signs the header
// X-Amz-Security-Token.
func TestSignV4OfARequest(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, token, headers string
	}{
		{"keys", "", "content-type;host;x-amz-date;x-amz-target"},
		{"token", "sessiontoken", "content-type;host;x-amz-date;x-amz-security-token;x-amz-target"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewConnector(Config{Host: "localhost", Port: 8000, Region: "eu-west-2", User: "key", Password: "secret", Token: tt.token})
			c.now = func() time.Time { return time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC) }
			var got http.Header
			c.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				got = req.Header.Clone()
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-amz-json-1.0"}}, Body: http.NoBody, Request: req}, nil
			})
			res, err := c.call(t.Context(), "ExecuteStatement", []byte(`{"Statement":"SELECT 1"}`))
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			auth := got.Get("Authorization")
			for _, want := range []string{
				"AWS4-HMAC-SHA256 Credential=key/20261007/eu-west-2/dynamodb/aws4_request",
				"SignedHeaders=" + tt.headers + ",",
				"Signature=",
			} {
				if !strings.Contains(auth, want) {
					t.Errorf("Authorization = %q, want it to hold %q", auth, want)
				}
			}
			if got.Get("X-Amz-Target") != "DynamoDB_20120810.ExecuteStatement" || got.Get("Content-Type") != "application/x-amz-json-1.0" {
				t.Errorf("headers = %v", got)
			}
			if got.Get("X-Amz-Security-Token") != tt.token {
				t.Errorf("X-Amz-Security-Token = %q, want %q", got.Get("X-Amz-Security-Token"), tt.token)
			}
			if strings.Contains(auth, "secret") || (tt.token != "" && strings.Contains(auth, tt.token)) {
				t.Errorf("Authorization = %q holds a secret", auth)
			}
		})
	}
}

// roundTripFunc is an http.RoundTripper made of a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
