package snowflake //nolint:testpackage // The tests build the error from a recorded answer with the function of the package.

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
	ex, err := dbimptest.ReadExchange(filepath.Join(testdata, file))
	if err != nil {
		t.Fatal(err)
	}
	res := &http.Response{
		StatusCode: ex.Response.Status,
		Header:     ex.Response.Header,
		Body:       io.NopCloser(strings.NewReader(ex.Response.Body)),
	}
	err = checkStatus(res, http.StatusOK)
	if err == nil {
		t.Fatalf("the answer in %s has the status %d, which is not an error", file, ex.Response.Status)
	}
	return err
}

// TestAuthentication holds the rule of D197: the error matches
// dbimp.ErrAuthentication for the code 390144 and for HTTP 401, and not for a
// statement that the role may not run (code 002003, HTTP 422) or any other
// error. The files are the exchanges that step 6 recorded.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		want bool
	}{
		{"a wrong token, code 390144 with HTTP 401", "snowflake-078-post--api-v2-statements.json", true},
		{"an object that the role may not read, code 002003", "snowflake-063-post--api-v2-statements.json", false},
		{"a schema that the role may not read, code 002003", "snowflake-079-post--api-v2-statements.json", false},
		{"a syntax error, HTTP 422", "snowflake-062-post--api-v2-statements.json", false},
		{"an empty statement, code 000900, HTTP 422", "snowflake-020-post--api-v2-statements.json", false},
		{"a request that the server refused, HTTP 400", "snowflake-111-post--api-v2-statements.json", false},
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

// TestAuthenticationKeepsTheSentinels checks that the other sentinels still
// match after the rule for ErrAuthentication, and that they never match it.
func TestAuthenticationKeepsTheSentinels(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		err    *Error
		target error
	}{
		{&Error{Code: codeCanceled}, ErrCanceled},
		{&Error{Code: codeTimeout, HTTPStatus: http.StatusRequestTimeout}, ErrTimeout},
	} {
		if !errors.Is(tt.err, tt.target) {
			t.Errorf("errors.Is(%v, %v) = false, want true", tt.err, tt.target)
		}
		if errors.Is(tt.err, dbimp.ErrAuthentication) {
			t.Errorf("errors.Is(%v, dbimp.ErrAuthentication) = true, want false", tt.err)
		}
	}
}
