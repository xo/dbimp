package avatica

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D156).
const (
	keyTLS  = "tls"
	keyAuth = "auth"
)

// defaultPort is the port of a server of Avatica, for a DSN with no port
// (D156).
const defaultPort = 8765

// The values of the key auth, which say whether the driver also sends the
// user and the password by HTTP basic authentication (D156).
const (
	// AuthNone sends the user and the password in the info of
	// openConnection only. It is the default.
	AuthNone = "none"
	// AuthBasic also sends them by HTTP basic authentication, for a server
	// that checks them itself.
	AuthBasic = dbimp.AuthBasic
)

// auths are the values of the key auth.
var auths = []string{AuthNone, AuthBasic}

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the server, 8765 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which go in the info of
	// openConnection, to the database behind the server.
	User     string
	Password string
	// Auth is AuthNone or AuthBasic (D156).
	Auth string
}

// ParseDSN parses a DSN of the form avatica://user:pass@host:port?key=value
// (D27, D35 and D156). A DSN with a path is refused.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyAuth)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host: u.Hostname(),
		Port: defaultPort,
		Auth: q.String(keyAuth, AuthNone),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: the URL of Avatica has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if !slices.Contains(auths, cfg.Auth) {
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyAuth, cfg.Auth, dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
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
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	if cfg.Auth != "" && cfg.Auth != AuthNone {
		q.Set(keyAuth, cfg.Auth)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the server of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/"
}
