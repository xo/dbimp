package neo4j_test

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
	db := open(t, srv.URL, "")
	_, _, err = rowsOf(t, db, "RETURN 1")
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
		{"a wrong password, 5.26.31", "neo4j-5.26.31-089-post--db-dbmeta-query-v2.json", true},
		{"a wrong password, 2026.09.0", "neo4j-2026.09.0-088-post--db-dbmeta-query-v2.json", true},
		{"a missing privilege, Security.Forbidden", "neo4j-2026.09.0-310-post--db-system-query-v2.json", false},
		{"a missing privilege, 5.26.31", "neo4j-5.26.31-312-post--db-system-query-v2.json", false},
		{"a syntax error", "neo4j-5.26.31-074-post--db-dbmeta-query-v2.json", false},
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
