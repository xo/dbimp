package hive

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D234).
const (
	keyHTTPPath = "httpPath"
	keyTLS      = "tls"
)

const (
	// defaultPort is the HTTP port of HiveServer2, for a DSN with no port
	// (D234). Impala listens on 28000, which a caller names.
	defaultPort = 10001
	// defaultHTTPPath is the path of the endpoint on Hive (D234).
	defaultHTTPPath = "cliservice"
	// defaultUser is the user of a DSN that names none. Both servers accept any
	// user, and Hive answers HTTP 401 to a request with no credential, so the
	// driver always sends one (docs/HIVE.md, Requests).
	defaultUser = "anonymous"
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the HTTP port of the server, 10001 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which go by HTTP Basic
	// authentication. With no user, the driver sends the user "anonymous".
	User     string
	Password string
	// Database is the database of the session, or "" for the default of the
	// server.
	Database string
	// HTTPPath is the path of the endpoint, "cliservice" by default.
	HTTPPath string
}

// ParseDSN parses a DSN of the form hive://user:pass@host:port/database?key=value
// (D27, D35 and D234). It refuses a key that it does not know, and a key that
// repeats.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyHTTPPath, keyTLS)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:     u.Hostname(),
		Port:     defaultPort,
		Database: strings.TrimPrefix(u.Path, "/"),
		HTTPPath: strings.TrimLeft(q.String(keyHTTPPath, defaultHTTPPath), "/"),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if strings.Contains(cfg.Database, "/") {
		return nil, fmt.Errorf("parsing the dsn: the path %q holds more than a database: %w", u.Path, dbimp.ErrInvalidValue)
	}
	if cfg.HTTPPath == "" {
		return nil, fmt.Errorf("parsing key %q: empty: %w", keyHTTPPath, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a Config
// that ParseDSN or NewConnector filled.
func (cfg *Config) FormatDSN() string {
	u := url.URL{
		Scheme: Name,
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	if cfg.Database != "" {
		u.Path = "/" + cfg.Database
	}
	q := url.Values{}
	if cfg.HTTPPath != "" && cfg.HTTPPath != defaultHTTPPath {
		q.Set(keyHTTPPath, cfg.HTTPPath)
	}
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// endpoint returns the URL that every call goes to.
func (cfg *Config) endpoint() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	path := cfg.HTTPPath
	if path == "" {
		path = defaultHTTPPath
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/" + strings.TrimLeft(path, "/")
}

// user returns the user of the credentials.
func (cfg *Config) user() string {
	if cfg.User == "" {
		return defaultUser
	}
	return cfg.User
}
