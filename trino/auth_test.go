package trino //nolint:testpackage // The tests build the error from a recorded answer with the functions of the package.

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

// recordedProtocolError returns the error that the driver makes from the
// answer in the file, which is an answer whose status is not 2xx.
func recordedProtocolError(t *testing.T, file string) error {
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

// recordedFailure returns the error that the driver makes from the error
// member of the page in the file.
func recordedFailure(t *testing.T, file string) error {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join(recorded, file))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Error *failure `json:"error"`
	}
	if err := json.Unmarshal([]byte(ex.Response.Body), &page); err != nil {
		t.Fatalf("reading the page in %s: %v", file, err)
	}
	if page.Error == nil {
		t.Fatalf("the page in %s has no error member", file)
	}
	return newFailure(*page.Error)
}

// TestAuthentication holds the rule of D197: the error matches
// dbimp.ErrAuthentication for HTTP 401, and for nothing else. The files are
// the exchanges that step 6 recorded.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		page bool
		want bool
	}{
		{"401 with no user, trino-476", "trino-476-852-post--v1-statement.json", false, true},
		{"401 with a password over HTTP, trino-476", "trino-476-853-post--v1-statement.json", false, true},
		{"401 with a password and a user, trino-476", "trino-476-854-post--v1-statement.json", false, true},
		{"PERMISSION_DENIED in a page", "trino-476-1136-get--v1-statement-executing-20261006-232453-00216-x3v5n-yd68a04ff8e980cc90885811487d8b383b4ca37cf-0.json", true, false},
		{"a syntax error in a page", "trino-476-1009-get--v1-statement-queued-20261006-232453-00186-x3v5n-y6cb7c2cfe26836d9d37c1e3f3832c6fdfbbdc3a6-1.json", true, false},
		{"400 for an empty statement", "trino-476-1005-post--v1-statement.json", false, false},
		{"404 for a query that is not known", "trino-476-825-get--v1-statement-executing-20260101-000000-00000-aaaaa-yaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-0.json", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			read := recordedProtocolError
			if tt.page {
				read = recordedFailure
			}
			err := read(t, tt.file)
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
