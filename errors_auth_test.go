package dbimp_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/xo/dbimp"
)

func TestStatusErrorMatchesAuthenticationFor401(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		code int
		want bool
	}{
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, false},
		{http.StatusBadRequest, false},
		{http.StatusInternalServerError, false},
	} {
		err := fmt.Errorf("reading: %w", errors.Join(errors.New("other"), &dbimp.StatusError{Code: tt.code}))
		if got := errors.Is(err, dbimp.ErrAuthentication); got != tt.want {
			t.Errorf("errors.Is for HTTP %d is %t, want %t", tt.code, got, tt.want)
		}
	}
	if errors.Is(&dbimp.StatusError{Code: http.StatusUnauthorized}, dbimp.ErrNotSupported) {
		t.Error("a 401 matches ErrNotSupported")
	}
}

// refuser is the interface that a caller declares for itself, as usql does
// (D227).
type refuser interface {
	AuthenticationRefused() bool
}

func TestRootErrorsSayWhetherTheServerRefusedTheCredential(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		err  refuser
		want bool
	}{
		{"ErrAuthentication", dbimp.ErrAuthentication, true},
		{"ErrNotSupported", dbimp.ErrNotSupported, false},
		{"401", &dbimp.StatusError{Code: http.StatusUnauthorized}, true},
		{"403", &dbimp.StatusError{Code: http.StatusForbidden}, false},
		{"500", &dbimp.StatusError{Code: http.StatusInternalServerError}, false},
	} {
		if got := tt.err.AuthenticationRefused(); got != tt.want {
			t.Errorf("%s: AuthenticationRefused is %t, want %t", tt.name, got, tt.want)
		}
	}
	// a wrapped sentinel stays visible to a caller that walks the tree
	err := fmt.Errorf("%w: %w", dbimp.ErrAuthentication, errors.New("token refused"))
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("%T has no method Unwrap() []error", err)
	}
	var found bool
	for _, e := range joined.Unwrap() {
		if r, ok := e.(refuser); ok && r.AuthenticationRefused() {
			found = true
		}
	}
	if !found {
		t.Error("no error in the tree of a wrapped ErrAuthentication refuses the credential")
	}
}
