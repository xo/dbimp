package arangodb

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D93 and D94).
const (
	keyTLS    = "tls"
	keyCancel = "cancel"
	keyBatch  = "batch"
	keyAuth   = "auth"
)

// defaultPort is the port of a DSN with no port.
const defaultPort = 8529

// defaultDatabase is the database of a DSN with no path.
const defaultDatabase = "_system"

// defaultBatch is the batchSize of a cursor, as the server sets it.
const defaultBatch = 1000

// The values of the key cancel, which say how the driver stops a query on the
// server when its context ends (D90).
const (
	// CancelTag adds a line comment that names the connection and the query,
	// at the end of each query, or at the start of a long one (D99). It kills the query through /_api/query when
	// its context ends before the first batch, or when Close comes in the
	// first batch with more than 1 MiB of it left. It is the default.
	CancelTag = "tag"
	// CancelNone sends no comment, and a query in its first request runs on
	// to its end.
	CancelNone = "none"
)

// cancels are the values of the key cancel.
var cancels = []string{CancelTag, CancelNone}

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
	// Port is the port of the server, 8529 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials. The password can be a token
	// (D94).
	User     string
	Password string
	// Database is the database, _system by default.
	Database string
	// Cancel is CancelTag or CancelNone (D90).
	Cancel string
	// Batch is the batchSize of each cursor, 1000 by default.
	Batch int
	// Auth is AuthBasic or AuthBearer (D94).
	Auth string
}

// ParseDSN parses a DSN of the form
// arangodb://user:pass@host:port/database?key=value (D27, D35 and D93).
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyCancel, keyBatch, keyAuth)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:   u.Hostname(),
		Port:   defaultPort,
		Cancel: q.String(keyCancel, CancelTag),
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
	if cfg.Batch, err = q.Int(keyBatch, defaultBatch); err != nil {
		return nil, err
	}
	switch {
	case cfg.Batch < 1:
		return nil, fmt.Errorf("parsing key %q: %d: %w", keyBatch, cfg.Batch, dbimp.ErrInvalidValue)
	case !slices.Contains(cancels, cfg.Cancel):
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyCancel, cfg.Cancel, dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// parsePath returns the database that the path of u names, or _system for no
// path. A path of more than one segment is refused (D93).
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
	if cfg.Cancel != "" && cfg.Cancel != CancelTag {
		q.Set(keyCancel, cfg.Cancel)
	}
	if cfg.Batch != 0 && cfg.Batch != defaultBatch {
		q.Set(keyBatch, strconv.Itoa(cfg.Batch))
	}
	if cfg.Auth != "" && cfg.Auth != AuthBasic {
		q.Set(keyAuth, cfg.Auth)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the API of the database of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/_db/" + url.PathEscape(cfg.Database) + "/_api/"
}
