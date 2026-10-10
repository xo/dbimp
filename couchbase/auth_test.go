package couchbase_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// recordedError runs a statement against a fake server that sends the
// recorded response in the named file, and returns the error of the driver.
func recordedError(t *testing.T, name string) error {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join(testdata, name))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ex.Response.Status)
		_, _ = w.Write(ex.Response.Content())
	}))
	t.Cleanup(srv.Close)
	db := openAt(t, srv.URL)
	t.Cleanup(func() { db.Close() })
	_, _, err = rowsOf(t, db, "SELECT 1")
	if err == nil {
		t.Fatalf("%s gave no error", name)
	}
	return err
}

func TestAuthenticationRecorded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
		want bool
	}{
		{"a wrong password, 7.6.12", "couchbase-7.6.12-032-post--query-service.json", true},
		{"a wrong password, 8.0.3", "couchbase-8.0.3-032-post--query-service.json", true},
		{"a missing role, HTTP 401 and code 13014", "couchbase-8.0.3-209-post--query-service.json", false},
		{"a write in read only mode, HTTP 403", "couchbase-8.0.3-012-post--query-service.json", false},
		{"a syntax error", "couchbase-8.0.3-067-post--query-service.json", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := recordedError(t, tt.file)
			wrapped := fmt.Errorf("running the statement: %w", err)
			joined := errors.Join(errors.New("other"), err)
			for label, e := range map[string]error{"the error": err, "wrapped": wrapped, "joined": joined} {
				if got := errors.Is(e, dbimp.ErrAuthentication); got != tt.want {
					t.Errorf("errors.Is(%s, dbimp.ErrAuthentication) is %t, want %t: %v", label, got, tt.want, e)
				}
			}
		})
	}
}
