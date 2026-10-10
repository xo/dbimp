package databend_test

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databend"
	"github.com/xo/dbimp/dbimptest"
)

// TestAuthenticationDatabend holds D197. The error of the driver matches
// dbimp.ErrAuthentication for the recorded answer of a wrong credential, and
// for no other recorded refusal. Each case serves the recorded response of
// one file under testdata/databend/ and runs one statement.
func TestAuthenticationDatabend(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		file string
		want bool
	}{
		{"1.2.881 a wrong password", "databend-1.2.881-073-post--v1-query.json", true},
		{"1.2.948 a wrong password", "databend-1.2.948-072-post--v1-query.json", true},
		{"1.2.881 a database that needs a privilege, code 1063", "databend-1.2.881-225-post--v1-query.json", false},
		{"1.2.881 a statement that does not parse, code 1005", "databend-1.2.881-150-post--v1-query.json", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ex, err := dbimptest.ReadExchange("../testdata/databend/" + tt.file)
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
			db, err := sql.Open(databend.Name, strings.Replace(srv.URL, "http://", "databend://u:p@", 1)+"/dbmeta?cancel=none&timezone=UTC")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			_, err = db.ExecContext(t.Context(), "SELECT 1")
			if err == nil {
				t.Fatal("the statement succeeded, want an error")
			}
			if _, ok := errors.AsType[*databend.Error](err); !ok {
				t.Fatalf("the error is %v (%T), want an *databend.Error", err, err)
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
