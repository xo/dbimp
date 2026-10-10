package cosmos

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

// recorded returns the error that the driver builds from the response that
// the file of the recording holds.
func recorded(t *testing.T, pattern string) error {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "testdata", "cosmos", pattern))
	if err != nil || len(files) == 0 {
		t.Fatalf("no recording matches %s: %v", pattern, err)
	}
	ex, err := dbimptest.ReadExchange(files[0])
	if err != nil {
		t.Fatal(err)
	}
	res := &http.Response{
		StatusCode: ex.Response.Status,
		Status:     http.StatusText(ex.Response.Status),
		Header:     ex.Response.Header,
		Body:       io.NopCloser(strings.NewReader(ex.Response.Body)),
	}
	return checkStatus(res)
}

// TestAuthentication holds that the error of a recorded wrong credential
// matches dbimp.ErrAuthentication, and that the error of a refusal for a
// missing permission and of other answers does not (D197).
func TestAuthentication(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, pattern string
		want          bool
	}{
		{"a request with no signature", "cosmos-185-*", true},
		{"a refusal that is not a privilege", "cosmos-153-*", false},
		{"the options of the server", "cosmos-203-*", false},
		{"a body that the server cannot parse", "cosmos-EN20260907-065-*", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := recorded(t, tt.pattern)
			wrapped := fmt.Errorf("querying: %w", err)
			joined := errors.Join(errors.New("other"), wrapped)
			for _, e := range []error{err, wrapped, joined} {
				if got := errors.Is(e, dbimp.ErrAuthentication); got != tt.want {
					t.Errorf("errors.Is(%q, dbimp.ErrAuthentication) is %t, want %t", e, got, tt.want)
				}
			}
		})
	}
}
