package clickhouse //nolint:testpackage // The tests build the error from a recorded answer with the function of the package.

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

// recordedError returns the error that the driver makes from the answer in
// the file, which is an answer whose status is not 200.
func recordedError(t *testing.T, file string) error {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join(recorded, file))
	if err != nil {
		t.Fatal(err)
	}
	res := &http.Response{
		StatusCode: ex.Response.Status,
		Header:     ex.Response.Header,
		Body:       io.NopCloser(strings.NewReader(ex.Response.Body)),
	}
	err = checkStatus(res)
	if err == nil {
		t.Fatalf("the answer in %s has the status %d, which is not an error", file, ex.Response.Status)
	}
	return err
}

// TestAuthentication holds the rule of D197: the error matches
// dbimp.ErrAuthentication for the codes 194 and 516 and for HTTP 401, and not
// for a missing privilege (code 497) or any other error. The files are the
// exchanges that step 6 recorded.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		want bool
	}{
		{"a wrong password of default, 25.3, code 194 with HTTP 401", "clickhouse-25.3-256-post.json", true},
		{"a wrong password of default, 26.8, code 194 with HTTP 401", "clickhouse-26.8-031-post.json", true},
		{"a wrong password of another user, 25.3, code 516 with HTTP 403", "clickhouse-25.3-813-post.json", true},
		{"a wrong password of another user, 25.8, code 516 with HTTP 403", "clickhouse-25.8-823-post.json", true},
		{"a user that is not known, 26.8, code 516 with HTTP 403", "clickhouse-26.8-262-post.json", true},
		{"two forms of credential in one request, code 516", "clickhouse-25.3-031-post.json", true},
		{"a missing privilege on a table, code 497", "clickhouse-26.8-1012-post.json", false},
		{"a missing privilege to create a database, code 497", "clickhouse-26.8-1031-post.json", false},
		{"a missing privilege to read a URL, code 497", "clickhouse-26.9-1080-post.json", false},
		{"a syntax error, HTTP 400", "clickhouse-25.3-280-post.json", false},
		{"a statement that the server does not allow, HTTP 500", "clickhouse-25.3-394-post.json", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := recordedError(t, tt.file)
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

// TestAuthenticationFromTheCode checks that the code alone decides, as it does
// when the error comes after some rows with HTTP 200.
func TestAuthenticationFromTheCode(t *testing.T) {
	t.Parallel()
	for code, want := range map[int]bool{194: true, 516: true, 497: false, 60: false, 0: false} {
		err := &Error{HTTPStatus: http.StatusOK, Code: code}
		if got := errors.Is(err, dbimp.ErrAuthentication); got != want {
			t.Errorf("errors.Is(code %d, dbimp.ErrAuthentication) = %t, want %t", code, got, want)
		}
	}
}
