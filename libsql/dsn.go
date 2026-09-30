package libsql

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D148).
const (
	keyTLS       = "tls"
	keyAuth      = "auth"
	keyNamespace = "namespace"
)

// defaultPort is the port of HTTPS, for a DSN with TLS and no port (D148).
// A DSN without TLS names its port.
const defaultPort = 443

// The values of the key auth, which say how the driver sends the password
// (D94 and D148).
const (
	// AuthBearer sends the password as a Bearer token. It is the default.
	AuthBearer = dbimp.AuthBearer
	// AuthBasic sends the user and the password with basic authentication,
	// for sqld with SQLD_HTTP_AUTH.
	AuthBasic = dbimp.AuthBasic
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the server, 443 by default with TLS.
	Port int
	// NoTLS is true to speak HTTP, which the key tls=false asks for. TLS is
	// on by default (D148).
	NoTLS bool
	// User and Password are the credentials. The password is the token
	// (D94).
	User     string
	Password string
	// Auth is AuthBearer or AuthBasic (D94).
	Auth string
	// Namespace is the namespace of each request, sent as x-namespace, or ""
	// for the default one.
	Namespace string
}

// ParseDSN parses a DSN of the form libsql://user:token@host:port?key=value
// (D27, D35 and D148). A DSN with a path is refused, and so is a DSN with
// tls=false and no port.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyAuth, keyNamespace)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:      u.Hostname(),
		Port:      defaultPort,
		Namespace: q.String(keyNamespace, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: the URL of libSQL has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	tls, err := q.Bool(keyTLS, true)
	if err != nil {
		return nil, err
	}
	cfg.NoTLS = !tls
	if cfg.Auth, err = q.Auth(keyAuth); err != nil {
		return nil, err
	}
	// Query.Auth gives basic when the key is missing, and libSQL takes a
	// token by default (D148).
	if !q.Has(keyAuth) {
		cfg.Auth = AuthBearer
	}
	if q.Has(keyNamespace) && cfg.Namespace == "" {
		return nil, fmt.Errorf("parsing key %q: an empty namespace: %w", keyNamespace, dbimp.ErrInvalidValue)
	}
	switch p := u.Port(); {
	case p != "":
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	case cfg.NoTLS:
		return nil, fmt.Errorf("parsing the dsn: tls=false needs a port: %w", dbimp.ErrInvalidValue)
	}
	return cfg, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN or NewConnector filled.
func (cfg *Config) FormatDSN() string {
	u := url.URL{
		Scheme: Name,
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := url.Values{}
	if cfg.NoTLS {
		q.Set(keyTLS, "false")
	}
	if cfg.Auth != "" && cfg.Auth != AuthBearer {
		q.Set(keyAuth, cfg.Auth)
	}
	if cfg.Namespace != "" {
		q.Set(keyNamespace, cfg.Namespace)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the server of cfg.
func (cfg *Config) baseURL() string {
	scheme := "https"
	if cfg.NoTLS {
		scheme = "http"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
