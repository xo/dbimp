package drill

import (
	"encoding/json/v2"
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

// readRecorded returns the exchange in the file name under testdata/drill/.
func readRecorded(t *testing.T, name string) *dbimptest.Exchange {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join("..", "testdata", "drill", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return ex
}

// recordedError returns the error that the driver makes from the recorded
// answer in the file name. An answer with a status that is not 2xx goes
// through checkStatus. An answer with HTTP 200 and the state FAILED goes
// through failure, as the reader of rows does.
func recordedError(t *testing.T, name string) error {
	t.Helper()
	ex := readRecorded(t, name)
	if ex.Response.Status != http.StatusOK {
		return checkStatus(&http.Response{
			StatusCode: ex.Response.Status,
			Header:     ex.Response.Header,
			Body:       io.NopCloser(strings.NewReader(ex.Response.Body)),
		})
	}
	var body struct {
		QueryID   string `json:"queryId"`
		Exception string `json:"exception"`
		Message   string `json:"errorMessage"`
	}
	if err := json.Unmarshal([]byte(ex.Response.Body), &body); err != nil {
		t.Fatalf("decoding %s: %v", name, err)
	}
	return failure(body.QueryID, body.Exception, body.Message)
}

// TestAuthentication holds that the error of a refused credential matches
// dbimp.ErrAuthentication, and that no other error does (D197). Each case is
// an answer that step 6 recorded.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  string
		want  bool
		login bool
	}{
		{"a wrong password", "drill-1.22.0-132-post--query-json.json", true, true},
		{"a wrong password on 1.21.2", "drill-1.21.2-132-post--query-json.json", true, true},
		{"a permission error", "drill-1.22.0-276-post--query-json.json", false, false},
		{"a permission error on 1.21.2", "drill-1.21.2-277-post--query-json.json", false, false},
		{"a login form that does not exist", "drill-1.22.0-018-post--j-security-check.json", false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := recordedError(t, test.file)
			if err == nil {
				t.Fatalf("no error for %s", test.file)
			}
			if got := errors.Is(err, dbimp.ErrAuthentication); got != test.want {
				t.Errorf("errors.Is(%v, dbimp.ErrAuthentication) = %t, want %t", err, got, test.want)
			}
			if got := errors.Is(err, ErrLogin); got != test.login {
				t.Errorf("errors.Is(%v, ErrLogin) = %t, want %t", err, got, test.login)
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

// TestAuthenticationRedirect holds that a redirect that does not go to the
// login page is not a refused credential, though it is still ErrLogin (D197).
func TestAuthenticationRedirect(t *testing.T) {
	t.Parallel()
	serr := &dbimp.StatusError{Code: http.StatusTemporaryRedirect}
	err := newError(serr, "http://127.0.0.1/elsewhere")
	if errors.Is(err, dbimp.ErrAuthentication) {
		t.Errorf("errors.Is(%v, dbimp.ErrAuthentication) is true for a redirect that is not to the login page", err)
	}
}
