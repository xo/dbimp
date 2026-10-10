package bigquery //nolint:testpackage // The tests read the error types of the package, which build their values in the package.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// recordedError returns the error that the driver makes of the recorded hosted
// response with the number n.
func recordedError(t *testing.T, n int) error {
	t.Helper()
	ex := exchange(t, n)
	res := &http.Response{StatusCode: ex.Response.Status, Body: io.NopCloser(strings.NewReader(ex.Response.Body))}
	err := checkStatus(res, http.StatusOK)
	if err == nil {
		t.Fatalf("the recorded response %d has the status %d, want an error", n, ex.Response.Status)
	}
	return err
}

// TestAuthentication holds that an error matches dbimp.ErrAuthentication for
// a refused credential only (D197).
func TestAuthentication(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{`recorded: "a request with no credentials"`, recordedError(t, 219), true},
		{`recorded: "a request with a Bearer token that is wrong"`, recordedError(t, 420), true},
		{`recorded: "a token that is wholly wrong on a read"`, recordedError(t, 421), true},
		{"accessDenied, a dataset", recordedError(t, 15), false},
		{"accessDenied, a project", recordedError(t, 194), false},
		{"accessDenied, a caller without a role", recordedError(t, 222), false},
		{"accessDenied, a job", recordedError(t, 214), false},
		{"notFound", recordedError(t, 53), false},
		{"a job that was stopped", &Error{Reason: reasonStopped}, false},
	}
	for _, tt := range tests {
		forms := map[string]error{
			"plain":   tt.err,
			"wrapped": fmt.Errorf("x: %w", tt.err),
			"joined":  errors.Join(errors.New("other"), tt.err),
		}
		for form, err := range forms {
			if got := errors.Is(err, dbimp.ErrAuthentication); got != tt.want {
				t.Errorf("%s (%s): errors.Is(err, dbimp.ErrAuthentication) is %t, want %t", tt.name, form, got, tt.want)
			}
		}
	}
}

// TestAuthenticationTokenEndpoint holds that a key that the token endpoint of
// Google refuses matches dbimp.ErrAuthentication, for invalid_grant and
// invalid_client, and that another refusal does not (D197).
func TestAuthenticationTokenEndpoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"invalid_grant", &dbimp.StatusError{Code: http.StatusBadRequest, Body: `{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`}, true},
		{"invalid_client", &dbimp.StatusError{Code: http.StatusBadRequest, Body: `{"error":"invalid_client"}`}, true},
		{"a 401", &dbimp.StatusError{Code: http.StatusUnauthorized, Body: `{"error":"x"}`}, true},
		{"invalid_scope", &dbimp.StatusError{Code: http.StatusBadRequest, Body: `{"error":"invalid_scope"}`}, false},
		{"a page of HTML", &dbimp.StatusError{Code: http.StatusBadGateway, Body: `<html>`}, false},
	}
	for _, tt := range tests {
		err := fmt.Errorf("exchanging the key for a token: %w", tokenRefused(tt.err))
		if got := errors.Is(err, dbimp.ErrAuthentication); got != tt.want {
			t.Errorf("%s: errors.Is(err, dbimp.ErrAuthentication) is %t, want %t", tt.name, got, tt.want)
		}
		if _, ok := errors.AsType[*dbimp.StatusError](err); !ok {
			t.Errorf("%s: the error does not wrap the *dbimp.StatusError", tt.name)
		}
	}
}
