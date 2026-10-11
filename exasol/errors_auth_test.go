package exasol_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/exasol"
)

// TestAuthenticationExasol holds D197. The error of the driver matches
// dbimp.ErrAuthentication for the recorded answers of a wrong user and a wrong
// password, and for no other recorded refusal. Each case logs in with the
// recorded answers, and runs one statement if the login worked.
func TestAuthenticationExasol(t *testing.T) {
	t.Parallel()
	key := recorded(t, 1).Reply
	for _, tt := range []struct {
		name string
		// login is the answer to the second message of the login, and stmt the
		// answer to the statement that follows a good login.
		login dbimptest.WebSocketFrame
		stmt  *dbimptest.WebSocketFrame
		want  bool
	}{
		{"a wrong user", recorded(t, 281).Reply, nil, true},
		{"a wrong password", recorded(t, 288).Reply, nil, true},
		{"a login with no key, code 00000", recorded(t, 285).Reply, nil, false},
		{"a login on a connection with no TLS, the code 08004", failure("08004", "Only TLS connections are allowed."), nil, false},
		{"a missing privilege, code 42500", recorded(t, 2).Reply, &recorded(t, 272).Reply, false},
		{"a syntax error, code 42000", recorded(t, 2).Reply, &recorded(t, 146).Reply, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			steps := []dbimptest.WebSocketStep{{Reply: key}, {Reply: tt.login}}
			if tt.stmt != nil {
				steps = append(steps, dbimptest.WebSocketStep{Reply: *tt.stmt})
			}
			srv := dbimptest.ServeWebSocketScript(t, steps)
			db := open(t, srv.Addr(), "")
			_, err := db.ExecContext(t.Context(), "SELECT 1")
			if err == nil {
				t.Fatal("the statement succeeded, want an error")
			}
			if _, ok := errors.AsType[*exasol.Error](err); !ok {
				t.Fatalf("the error is %v (%T), want an *exasol.Error", err, err)
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
