package druid

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

// recorded returns the response of the exchange in the file name under
// testdata/druid/, with the status, the headers and the body that the server
// sent.
func recorded(t *testing.T, name string) *http.Response {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join("..", "testdata", "druid", name))
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
		{"a wrong password", "druid-37.0.0-107-post--druid-v2-sql.json", true},
		{"a refused privilege", "druid-37.0.0-126-post--druid-v2-sql.json", false},
		{"a refused task", "druid-37.0.0-139-post--druid-v2-sql.json", false},
		{"a division by zero", "druid-37.0.0-097-post--druid-v2-sql.json", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := checkStatus(recorded(t, test.file))
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
