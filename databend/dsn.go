package databend

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D117).
const (
	keyTLS      = "tls"
	keyAuth     = "auth"
	keyCancel   = "cancel"
	keyTimezone = "timezone"
)

// defaultPort is the port of a DSN with no port, for HTTP and HTTPS alike
// (D117).
const defaultPort = 8000

// defaultDatabase is the database of a DSN with no path.
const defaultDatabase = "default"

// The values of the key cancel, which say how the driver stops a query on the
// server when its context ends or its rows close early (D123).
const (
	// CancelKill sends GET on the kill URI of the query. It is the default.
	CancelKill = "kill"
	// CancelNone sends nothing, and the query runs on to its end.
	CancelNone = "none"
)

// cancels are the values of the key cancel.
var cancels = []string{CancelKill, CancelNone}

// The values of the key auth, which say how the driver sends the password
// (D94).
const (
	// AuthBasic sends the user and the password with basic authentication.
	// It is the default.
	AuthBasic = dbimp.AuthBasic
	// AuthBearer sends the password as a Bearer token, such as a JWT.
	AuthBearer = dbimp.AuthBearer
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the server, 8000 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials. The password can be a token
	// (D94).
	User     string
	Password string
	// Auth is AuthBasic or AuthBearer (D94).
	Auth string
	// Database is the database, default by default.
	Database string
	// Cancel is CancelKill or CancelNone (D123).
	Cancel string
	// Timezone is the timezone of the session, such as UTC, or "" for the
	// setting of the server.
	Timezone string
}

// ParseDSN parses a DSN of the form
// databend://user:pass@host:port/database?key=value (D27, D35 and D117).
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyAuth, keyCancel, keyTimezone)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:     u.Hostname(),
		Port:     defaultPort,
		Cancel:   q.String(keyCancel, CancelKill),
		Timezone: q.String(keyTimezone, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if cfg.Database, err = parsePath(u); err != nil {
		return nil, err
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if cfg.Auth, err = q.Auth(keyAuth); err != nil {
		return nil, err
	}
	if !slices.Contains(cancels, cfg.Cancel) {
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyCancel, cfg.Cancel, dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// parsePath returns the database that the path of u names, or default for no
// path. A path of more than one segment is refused (D117).
func parsePath(u *url.URL) (string, error) {
	p := u.EscapedPath()
	if p == "" || p == "/" {
		return defaultDatabase, nil
	}
	seg := strings.TrimPrefix(p, "/")
	if strings.Contains(seg, "/") {
		return "", fmt.Errorf("parsing the dsn: the path %q names more than one database: %w", p, dbimp.ErrInvalidValue)
	}
	db, err := url.PathUnescape(seg)
	if err != nil {
		return "", fmt.Errorf("parsing the dsn: the path %q: %w", p, dbimp.ErrInvalidValue)
	}
	return db, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN or NewConnector filled.
func (cfg *Config) FormatDSN() string {
	u := url.URL{
		Scheme:  Name,
		Host:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:    "/" + cfg.Database,
		RawPath: "/" + url.PathEscape(cfg.Database),
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := url.Values{}
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	if cfg.Auth != "" && cfg.Auth != AuthBasic {
		q.Set(keyAuth, cfg.Auth)
	}
	if cfg.Cancel != "" && cfg.Cancel != CancelKill {
		q.Set(keyCancel, cfg.Cancel)
	}
	if cfg.Timezone != "" {
		q.Set(keyTimezone, cfg.Timezone)
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
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
