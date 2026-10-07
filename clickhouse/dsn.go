package clickhouse

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// keyTLS is the one key of the query of a DSN (D177).
const keyTLS = "tls"

// The ports of a DSN with no port (D177).
const (
	defaultPort    = 8123
	defaultTLSPort = 8443
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the HTTP interface, 8123 by default and 8443 with
	// TLS.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which the driver sends with
	// basic authentication, if either one is not empty. With neither, the
	// server uses its user default.
	User     string
	Password string
	// Database is the database of each statement, and empty to leave the
	// choice to the server.
	Database string
}

// ParseDSN parses a DSN of the form
// clickhouse://user:password@host:port/database?tls=true (D27, D35 and D177).
// The path is the database, and it is optional. The one key is tls, and the
// driver refuses any other.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Host: u.Hostname()}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if cfg.Port = defaultPort; cfg.TLS {
		cfg.Port = defaultTLSPort
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.Database, err = parseDatabase(u); err != nil {
		return nil, err
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
		if cfg.Password == "" {
			u.User = url.User(cfg.User)
		}
	}
	if cfg.Database != "" {
		// RawPath keeps a slash in a name escaped, which Path cannot.
		u.Path = "/" + cfg.Database
		u.RawPath = "/" + url.PathEscape(cfg.Database)
	}
	if cfg.TLS {
		u.RawQuery = url.Values{keyTLS: {"true"}}.Encode()
	}
	return u.String()
}

// parseDatabase reads the database from the path of u.
func parseDatabase(u *url.URL) (string, error) {
	p := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if p == "" {
		return "", nil
	}
	name, err := url.PathUnescape(p)
	if err != nil || name == "" || strings.Contains(p, "/") {
		return "", fmt.Errorf("parsing the dsn: the path %q is not one database: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	}
	return name, nil
}

// baseURL returns the URL of the server of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
