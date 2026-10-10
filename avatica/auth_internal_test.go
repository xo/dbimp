package avatica

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

// recordedStatus returns the response of the exchange in the file name under
// testdata/avatica/, with the status, the headers and the body that the
// server sent.
func recordedStatus(t *testing.T, name string) *http.Response {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join("..", "testdata", "avatica", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return &http.Response{
		StatusCode: ex.Response.Status,
		Header:     ex.Response.Header,
		Body:       io.NopCloser(strings.NewReader(ex.Response.Body)),
	}
}

// TestAuthentication holds that the error of a refused credential matches
// dbimp.ErrAuthentication, and that no other error does (D197). Each case is
// a response that step 6 recorded.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
		want bool
	}{
		{"a wrong password", "avatica-1.28.0-047-post.json", true},
		{"a wrong password on 1.29.0", "avatica-1.29.0-047-post.json", true},
		{"an unknown table", "avatica-1.28.0-040-post.json", false},
		{"a division by zero", "avatica-1.28.0-044-post.json", false},
		{"a body that is not JSON", "avatica-1.28.0-043-post.json", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := checkStatus(recordedStatus(t, test.file))
			if err == nil {
				t.Fatalf("no error for %s", test.file)
			}
			if got := errors.Is(err, dbimp.ErrAuthentication); got != test.want {
				t.Errorf("errors.Is(%v, dbimp.ErrAuthentication) = %t, want %t", err, got, test.want)
			}
			wrapped := fmt.Errorf("running a statement: %w", err)
			if got := errors.Is(wrapped, dbimp.ErrAuthentication); got != test.want {
				t.Errorf("wrapped: got %t, want %t", got, test.want)
			}
			joined := errors.Join(errors.New("closing"), wrapped)
			if got := errors.Is(joined, dbimp.ErrAuthentication); got != test.want {
				t.Errorf("joined: got %t, want %t", got, test.want)
			}
		})
	}
}
