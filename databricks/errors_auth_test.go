package databricks //nolint:testpackage // The tests read the error types of the package, which build their values in the package.

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
	// No recording holds HTTP 401, because the workspace answered a wrong token
	// with HTTP 403. The body is the form of the same error object.
	unauthorized := newError(&dbimp.StatusError{Code: http.StatusUnauthorized, Body: `{"error_code":"UNAUTHENTICATED","message":"x"}`})
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{`recorded: "a statement with a wrong token"`, recordedError(t, 195), true},
		{`recorded: "a read of a statement with a wrong token"`, recordedError(t, 196), true},
		{`recorded: "a cancel with a wrong token"`, recordedError(t, 197), true},
		{`recorded: "the warehouse with a wrong token"`, recordedError(t, 198), true},
		{"HTTP 401, not recorded", unauthorized, true},
		{"PERMISSION_DENIED on a warehouse", recordedError(t, 173), false},
		{"PERMISSION_DENIED on the configuration", recordedError(t, 207), false},
		{"a bad request", recordedError(t, 32), false},
		{"a statement that was canceled", &Error{state: stateCanceled}, false},
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
