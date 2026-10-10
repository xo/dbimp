package surrealdb_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/surrealdb"
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
		for key, vals := range ex.Response.Header {
			for _, val := range vals {
				w.Header().Add(key, val)
			}
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(ex.Response.Status)
		_, _ = w.Write(ex.Response.Content())
	}))
	t.Cleanup(srv.Close)
	db := openAt(t, srv.URL, "?encoding=json")
	t.Cleanup(func() { db.Close() })
	_, _, err = sets(t, db, "RETURN 1")
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
		{"a wrong password, 3.3.0", "surrealdb-3.3.0-076-post--rpc.json", true},
		{"a wrong password, 2.7.0", "surrealdb-2.7.0-076-post--rpc.json", true},
		{"a wrong password of a database user", "surrealdb-3.3.0-234-post--rpc.json", true},
		{"a missing permission, IAM error", "surrealdb-3.3.0-235-post--rpc.json", false},
		{"a table that does not exist", "surrealdb-3.3.0-069-post--rpc.json", false},
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

// TestAuthenticationJSONRefusal holds the error that the driver builds for
// a refusal with a body of JSON, which no recording of a wrong credential
// has: the recordings hold the plain text of HTTP 401.
func TestAuthenticationJSONRefusal(t *testing.T) {
	t.Parallel()
	err := &surrealdb.ResponseError{HTTPStatus: 401, Errs: []surrealdb.Error{{Code: 401, Msg: "Authentication failed"}}}
	if !errors.Is(err, dbimp.ErrAuthentication) {
		t.Errorf("errors.Is(%v, dbimp.ErrAuthentication) is false, want true", err)
	}
	other := &surrealdb.ResponseError{HTTPStatus: 400, Errs: []surrealdb.Error{{Code: 400, Msg: "bad"}}}
	if errors.Is(other, dbimp.ErrAuthentication) {
		t.Errorf("errors.Is(%v, dbimp.ErrAuthentication) is true, want false", other)
	}
}
