package hive_test

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
	"github.com/xo/dbimp/hive"
)

// refusal returns the first recorded exchange of release with the status.
func refusal(t *testing.T, release string, status int) *dbimptest.Exchange {
	t.Helper()
	for _, r := range load(t, release) {
		if r.ex.Response.Status == status {
			return r.ex
		}
	}
	t.Fatalf("no recording of %s has the status %d", release, status)
	return nil
}

// TestAuthenticationHive holds D197. The error of the driver matches
// dbimp.ErrAuthentication for the recorded answer of Hive to a request with no
// credential, which is HTTP 401, and for no other recorded refusal. Both servers
// accept any user and any password, so no recorded wrong password exists.
func TestAuthenticationHive(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"hivehttp-4.2.1", "hivehttp-4.0.1"} {
		t.Run(release+" no credential", func(t *testing.T) {
			t.Parallel()
			ex := refusal(t, release, http.StatusUnauthorized)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(ex.Response.Status)
				_, _ = w.Write(ex.Response.Content())
			}))
			t.Cleanup(srv.Close)
			db, err := sql.Open(hive.Name, strings.Replace(srv.URL, "http://", "hive://u:secret@", 1))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			_, err = db.ExecContext(t.Context(), "SELECT 1")
			if err == nil {
				t.Fatal("the statement succeeded, want an error")
			}
			e, ok := errors.AsType[*hive.Error](err)
			if !ok {
				t.Fatalf("the error is %v (%T), want a *hive.Error", err, err)
			}
			if !e.AuthenticationRefused() {
				t.Error("AuthenticationRefused() is false, want true")
			}
			wrapped := fmt.Errorf("running a statement: %w", err)
			joined := errors.Join(errors.New("other"), wrapped)
			for name, e := range map[string]error{"plain": err, "wrapped": wrapped, "joined": joined} {
				if !errors.Is(e, dbimp.ErrAuthentication) {
					t.Errorf("%s: errors.Is(%v, dbimp.ErrAuthentication) is false, want true", name, err)
				}
			}
			if errors.Is(err, dbimp.ErrNotSupported) {
				t.Error("the error matches dbimp.ErrNotSupported")
			}
		})
	}
}

// TestAuthenticationNotAStatementError holds the other side of D197: the
// recorded refusal of a statement is no refusal of a credential.
func TestAuthenticationNotAStatementError(t *testing.T) {
	t.Parallel()
	const stmt = "select nope from nothing"
	db := open(t, "hivehttp-4.2.1", stmt)
	_, err := db.ExecContext(t.Context(), stmt)
	e, ok := errors.AsType[*hive.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *hive.Error", err, err)
	}
	if e.AuthenticationRefused() || errors.Is(err, dbimp.ErrAuthentication) {
		t.Error("the error of a statement matches dbimp.ErrAuthentication")
	}
}
