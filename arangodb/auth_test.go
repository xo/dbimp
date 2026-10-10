package arangodb_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/arangodb"
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
	db, err := sql.Open(arangodb.Name, strings.Replace(srv.URL, "http://", "arangodb://", 1)+"/db?cancel=none")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, _, err = readAll(t, db, "RETURN 1")
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
		{"a wrong password", "arangodb-3.12.12-059-post---db-dbmeta--api-cursor.json", true},
		{"no credentials", "arangodb-3.12.12-175-post---db-dbmeta--api-cursor.json", true},
		{"no access to a database, HTTP 401", "arangodb-3.12.12-176-get---api-collection.json", false},
		{"no right to make a database, HTTP 401", "arangodb-3.12.12-177-post---api-database.json", false},
		{"a parse error", "arangodb-3.12.12-040-post---db-dbmeta--api-cursor.json", false},
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
