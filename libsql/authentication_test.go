package libsql_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/libsql"
)

// TestAuthenticationLibsql holds D197. The error of the driver matches
// dbimp.ErrAuthentication for the recorded answer of a wrong credential, and
// for no other recorded refusal. Each case serves the recorded response of
// one file under testdata/libsql/ and runs one statement.
func TestAuthenticationLibsql(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		want bool
	}{
		{"a wrong password", "libsql-0.24.33-055-post--v2-pipeline.json", true},
		{"a token that cannot write, HTTP 403", "libsql-0.24.33-146-post--v2-pipeline.json", false},
		{"a fault of the protocol, HTTP 400", "libsql-0.24.33-051-post--v2-pipeline.json", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ex, err := dbimptest.ReadExchange("../testdata/libsql/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if ct := ex.Response.Header.Get("Content-Type"); ct != "" {
					w.Header().Set("Content-Type", ct)
				}
				w.WriteHeader(ex.Response.Status)
				_, _ = w.Write(ex.Response.Content())
			}))
			t.Cleanup(srv.Close)
			db, err := sql.Open(libsql.Name, strings.Replace(srv.URL, "http://", "libsql://", 1)+"?tls=false")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			_, err = db.ExecContext(t.Context(), "SELECT 1")
			if err == nil {
				t.Fatal("the statement succeeded, want an error")
			}
			if _, ok := errors.AsType[*libsql.Error](err); !ok {
				t.Fatalf("the error is %v (%T), want an *libsql.Error", err, err)
			}
			wrapped := fmt.Errorf("running a statement: %w", err)
			joined := errors.Join(errors.New("other"), wrapped)
			for name, e := range map[string]error{"plain": err, "wrapped": wrapped, "joined": joined} {
				if got := errors.Is(e, dbimp.ErrAuthentication); got != tt.want {
					t.Errorf("%s: errors.Is(%v, dbimp.ErrAuthentication) is %t, want %t", name, err, got, tt.want)
				}
			}
			if errors.Is(err, dbimp.ErrNotSupported) {
				t.Error("the error matches dbimp.ErrNotSupported")
			}
		})
	}
}
