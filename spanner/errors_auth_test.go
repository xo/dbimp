package spanner //nolint:testpackage // The tests read the error types of the package, which build their values in the package.

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

// recordedError returns the error that the driver makes of the recorded
// response in the file name, which is under testdata/ and is a path with no
// folder of its own.
func recordedError(t *testing.T, name string) error {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	res := &http.Response{StatusCode: ex.Response.Status, Body: io.NopCloser(strings.NewReader(ex.Response.Body))}
	err = checkStatus(res)
	if err == nil {
		t.Fatalf("the recorded response %s has the status %d, want an error", name, ex.Response.Status)
	}
	return err
}

// TestAuthentication holds that an error matches dbimp.ErrAuthentication for
// a refused credential only (D197).
//
// The recorded run used the emulator, which accepted a wrong token (D191), so
// no recording of Spanner holds a refused credential. The test reads the
// recording of the same front end of Google for BigQuery, which answers a wrong
// token with HTTP 401 and the status UNAUTHENTICATED, and decodes it with the
// decoder of this driver.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	wrong := recordedError(t, "bigquery/bigquery-420-post--bigquery-v2-projects-dbimp-project-queries.json")
	missing := recordedError(t, "bigquery/bigquery-219-post--bigquery-v2-projects-dbimp-project-queries.json")
	operation := newErr(wireError{Code: 16, Message: "x"})
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"a wrong token, from the BigQuery recording", wrong, true},
		{"no token, from the BigQuery recording", missing, true},
		{"the code 16 of an operation", operation, true},
		{"PERMISSION_DENIED on the sessions", recordedError(t, "spanner/spanner-132-post--v1-projects-dbimp-project-instances-dbimp-spanner-databases-nosuch-sessions.json"), false},
		{"PERMISSION_DENIED on an instance", recordedError(t, "spanner/spanner-134-get--v1-projects-dbimp-project-instances-dbimp-spanner.json"), false},
		{"the code 7 of an operation", newErr(wireError{Code: 7, Message: "x"}), false},
		{"an aborted transaction", newErr(wireError{Code: 409, Status: "ABORTED"}), false},
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
