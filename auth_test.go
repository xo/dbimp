package dbimp_test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/xo/dbimp"
)

func TestAuth(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query, want string
	}{
		{"", dbimp.AuthBasic},
		{"auth=basic", dbimp.AuthBasic},
		{"auth=bearer", dbimp.AuthBearer},
	} {
		u, _ := url.Parse("x://h/?" + tt.query)
		q, err := dbimp.NewQuery(u, "auth")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := q.Auth("auth"); err != nil || got != tt.want {
			t.Errorf("Auth of %q = %q, %v, want %q", tt.query, got, err, tt.want)
		}
	}
	u, _ := url.Parse("x://h/?auth=jwt")
	q, _ := dbimp.NewQuery(u, "auth")
	if _, err := q.Auth("auth"); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("Auth of auth=jwt gave %v, want ErrInvalidValue", err)
	}
}

func TestSetAuth(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		auth, scheme, user, secret, want string
	}{
		{dbimp.AuthBasic, "Bearer", "u", "p", "Basic dTpw"},
		{dbimp.AuthBearer, "Bearer", "u", "tok", "Bearer tok"},
		{dbimp.AuthBearer, "Token", "", "tok", "Token tok"},
		{dbimp.AuthBasic, "Bearer", "", "", ""},
		{dbimp.AuthBearer, "Bearer", "", "", ""},
	} {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://h/", nil)
		dbimp.SetAuth(req, tt.auth, tt.scheme, tt.user, tt.secret)
		if got := req.Header.Get("Authorization"); got != tt.want {
			t.Errorf("SetAuth(%q, %q, %q, %q) set %q, want %q", tt.auth, tt.scheme, tt.user, tt.secret, got, tt.want)
		}
	}
}
