package athena //nolint:testpackage // The tests read the error types of the package, which build their values in the package.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// recordedError returns the error that the driver makes of the recorded
// response with the number n.
func recordedError(t *testing.T, n int) error {
	t.Helper()
	ex := exchange(t, n)
	res := &http.Response{StatusCode: ex.Response.Status, Body: io.NopCloser(strings.NewReader(ex.Response.Body))}
	err := checkStatus(res)
	if err == nil {
		t.Fatalf("the recorded response %d has the status %d, want an error", n, ex.Response.Status)
	}
	return err
}

// TestAuthentication holds that an error matches dbimp.ErrAuthentication for
// a refused credential only (D197).
func TestAuthentication(t *testing.T) {
	t.Parallel()
	// The form that AWS documents for an access key that it does not know. No
	// recording holds it, because the recorded run used a known key.
	unrecognized := newError(&dbimp.StatusError{
		Code: http.StatusBadRequest,
		Body: `{"__type":"UnrecognizedClientException","message":"The security token included in the request is invalid."}`,
	})
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{`recorded: "a wrong secret", the start`, recordedError(t, 224), true},
		{`recorded: "a wrong secret", the execution`, recordedError(t, 225), true},
		{"UnrecognizedClientException, not recorded", unrecognized, true},
		{"AccessDeniedException of a workgroup", recordedError(t, 216), false},
		{"AccessDeniedException of a role", recordedError(t, 226), false},
		{"AccessDeniedException of a catalog list", recordedError(t, 441), false},
		{"InvalidRequestException", recordedError(t, 26), false},
		{"a query that was canceled", &Error{State: stateCancelled}, false},
		{"a query that failed", &Error{State: stateFailed, Message: "x"}, false},
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
