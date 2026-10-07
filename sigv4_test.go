package dbimp_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// signed returns the Authorization of the request method to the root of
// https://example.amazonaws.com/, with the header name set to value when name
// is not empty, signed in the scope of the test suite of AWS Signature
// Version 4 at the time that the suite uses.
func signed(t *testing.T, method, name, value string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, "https://example.amazonaws.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if name != "" {
		req.Header.Set(name, value)
	}
	dbimp.SignV4(req, nil, "AKIDEXAMPLE", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "us-east-1", "service", time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC))
	return req
}

// TestSignV4 signs the request "get-vanilla" of the test suite of AWS
// Signature Version 4, and compares the signature with the one that the suite
// gives.
func TestSignV4(t *testing.T) {
	t.Parallel()
	req := signed(t, http.MethodGet, "", "")
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got := req.Header.Get("X-Amz-Date"); got != "20150830T123600Z" {
		t.Errorf("X-Amz-Date = %q", got)
	}
}

// TestSignV4OfASessionToken signs the request "post-sts-header-before" of the
// test suite, which holds the header X-Amz-Security-Token when it is signed,
// so that the signature covers that header.
func TestSignV4OfASessionToken(t *testing.T) {
	t.Parallel()
	const sts = "AQoDYXdzEPT//////////wEXAMPLEtc764bNrC9SAPBSM22wDOk4x4HIZ8j4FZTwdQWLWsKWHGBuFqwAeMicRXmxfpSPfIeoIYRqTflfKD8YUuwthAx7mSEI/qkPpKPi/kMcGdQrmGdeehM4IC1NtBmUpp2wUE8phUZampKsburEDy0KPkyQDYwT7WZ0wq5VSXDvp75YU9HFvlRd8Tx6q6fE8YQcHNVXAkiY9q6d+xo0rKwT38xVqr7ZD0u0iPPkUL64lIZbqBAz+scqKmlzm8FDrypNC9Yjc8fPOLn9FX9KSYvKTr4rvx3iSIlTJabIQwj2ICCR/oLxBA=="
	req := signed(t, http.MethodPost, "X-Amz-Security-Token", sts)
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date;x-amz-security-token, Signature=85d96828115b5dc0cfc3bd16ad9e210dd772bbebba041836c64533a82be05ead"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
	if got := req.Header.Get("X-Amz-Security-Token"); got != sts {
		t.Errorf("X-Amz-Security-Token = %q, want it kept", got)
	}
}
