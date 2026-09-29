package dbimp

import (
	"fmt"
	"net/http"
	"slices"
)

// The values of the key auth of a DSN, which say how a driver sends the
// secret of its URL, the password (D94 and D110).
const (
	// AuthBasic sends the user and the secret with basic authentication. It
	// is the default.
	AuthBasic = "basic"
	// AuthBearer sends the secret as a token, such as a JWT, and no user.
	AuthBearer = "bearer"
)

// Auth returns the value of the key key, AuthBasic or AuthBearer, and
// AuthBasic if the query does not hold it.
func (q Query) Auth(key string) (string, error) {
	v := q.String(key, AuthBasic)
	if !slices.Contains([]string{AuthBasic, AuthBearer}, v) {
		return "", fmt.Errorf("parsing key %q: %q: %w", key, v, ErrInvalidValue)
	}
	return v, nil
}

// SetAuth sets the credentials on req, as auth says. AuthBasic sends user and
// secret with basic authentication. AuthBearer sends the header
// Authorization with scheme and secret, and no user. scheme is "Bearer" for
// most servers, and "Token" for InfluxDB 2, which refuses Bearer (D110).
// With no user and no secret, it sends nothing, for a server that runs with
// authentication off.
func SetAuth(req *http.Request, auth, scheme, user, secret string) {
	switch {
	case user == "" && secret == "":
	case auth == AuthBearer:
		req.Header.Set("Authorization", scheme+" "+secret)
	default:
		req.SetBasicAuth(user, secret)
	}
}
