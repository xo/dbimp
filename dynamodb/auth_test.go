package dynamodb //nolint:testpackage // The tests build the error from an answer with the function of the package.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// answer returns the error that the driver makes from a response.
func answer(t *testing.T, status int, body string) error {
	t.Helper()
	res := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
	err := checkStatus(res)
	if err == nil {
		t.Fatalf("the status %d is not an error", status)
	}
	return err
}

// recordedAnswer returns the error that the driver makes from the answer in
// the file of DynamoDB Local that step 6 recorded.
func recordedAnswer(t *testing.T, file string) error {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join("..", "testdata", "dynamodb", file))
	if err != nil {
		t.Fatal(err)
	}
	return answer(t, ex.Response.Status, ex.Response.Body)
}

// TestAuthentication holds the rule of D197: the error matches
// dbimp.ErrAuthentication for the type UnrecognizedClientException or
// InvalidSignatureException and for HTTP 401, and not for
// AccessDeniedException or any other error.
//
// DynamoDB Local checks no key, so step 6 recorded no refused credential and
// no refusal for a missing permission. Alternator and the service of AWS
// answer those with the bodies below. The text of each body is the form that
// docs/DYNAMODB.md, under Principals, names for Alternator, in the form of
// the protocol, and it is not a recording. The two other cases are recorded.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	const (
		bad      = `{"__type":"UnrecognizedClientException","message":"The security token included in the request is invalid."}`
		wrongSig = `{"__type":"UnrecognizedClientException","message":"wrong signature"}`
		aws      = `{"__type":"com.amazon.coral.service#UnrecognizedClientException","message":"The security token included in the request is invalid."}`
		invalid  = `{"__type":"com.amazon.coral.service#InvalidSignatureException","message":"The request signature we calculated does not match the signature you provided."}`
		denied   = `{"__type":"AccessDeniedException","message":"MODIFY access on table dbimp.t is denied for role dbmeta_user"}`
		quota    = `{"__type":"com.amazonaws.dynamodb.v20120810#ProvisionedThroughputExceededException","message":"slow down"}`
	)
	for _, tt := range []struct {
		name   string
		file   string // The recorded answer, or empty for the fake one.
		status int
		body   string
		want   bool
	}{
		{"fake: Alternator, a wrong secret, 2025.1", "", http.StatusBadRequest, bad, true},
		{"fake: Alternator, a wrong secret, 2026.3", "", http.StatusBadRequest, wrongSig, true},
		{"fake: AWS, a key that is not known", "", http.StatusBadRequest, aws, true},
		{"fake: AWS, a wrong signature", "", http.StatusBadRequest, invalid, true},
		{"fake: a gateway that answers HTTP 401", "", http.StatusUnauthorized, "denied", true},
		{"fake: Alternator, a write that the user cannot do", "", http.StatusBadRequest, denied, false},
		{"fake: HTTP 403 with no type", "", http.StatusForbidden, "<html>forbidden</html>", false},
		{"fake: a quota", "", http.StatusBadRequest, quota, false},
		{"recorded: a syntax error", "dynamodb-3.2.0-057-post.json", 0, "", false},
		{"recorded: a table that is not there", "dynamodb-3.2.0-134-post.json", 0, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tt.file != "" {
				err = recordedAnswer(t, tt.file)
			} else {
				err = answer(t, tt.status, tt.body)
			}
			for form, e := range map[string]error{
				"plain":   err,
				"wrapped": fmt.Errorf("running a statement: %w", err),
				"joined":  errors.Join(errors.New("other"), err),
			} {
				if got := errors.Is(e, dbimp.ErrAuthentication); got != tt.want {
					t.Errorf("%s: errors.Is(%q, dbimp.ErrAuthentication) = %t, want %t", form, e, got, tt.want)
				}
			}
		})
	}
}
