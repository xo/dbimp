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
