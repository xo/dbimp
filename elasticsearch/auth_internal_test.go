package elasticsearch

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
	files, err := filepath.Glob(filepath.Join("..", "testdata", "elasticsearch", pattern))
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
		{"a wrong password", "elasticsearch-8.19.22-147-*", true},
		{"a wrong password on 9.4.6", "elasticsearch-9.4.6-148-*", true},
		{"a wrong password of the ordinary user", "elasticsearch-8.19.22-297-*", true},
		{"a missing privilege", "elasticsearch-8.19.22-179-*", false},
		{"a missing cluster privilege", "elasticsearch-8.19.22-287-*", false},
		{"a license that does not allow the feature", "elasticsearch-8.19.22-024-*", false},
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
