package neo4j

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D61 and D67).
const (
	keyTLS    = "tls"
	keyCancel = "cancel"
)

// The ports of the HTTP interface.
const (
	portHTTP  = 7474
	portHTTPS = 7473
)

// defaultDatabase is the database of a DSN with no path. A new server has
// it, and it is the only database of the Community Edition (D61).
const defaultDatabase = "neo4j"

// The values of the key cancel, which say how the driver stops a query on the
// server when its context ends (D67).
const (
	// CancelTag starts each statement with a comment that names the
	// connection, and stops the statement with TERMINATE TRANSACTION. It is
	// the default.
	CancelTag = "tag"
	// CancelMetadata names the connection in txMetadata, and uses the tag on
	// a release older than 2026.04, which refuses txMetadata.
	CancelMetadata = "metadata"
	// CancelNone sends nothing, and the query runs on until it ends.
	CancelNone = "none"
)

// cancels are the values of the key cancel.
var cancels = []string{CancelTag, CancelMetadata, CancelNone}

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the HTTP interface.
	Host string
	// Port is the port of the HTTP interface, 7474 by default, or 7473 with
	// TLS.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, sent with basic authentication.
	// With no user, the driver sends no credentials, for a server that runs
	// with authentication off.
	User     string
	Password string
	// Database is the database that each statement runs in, "neo4j" by
	// default.
	Database string
	// Cancel says how the driver stops a query when its context ends:
	// CancelTag, CancelMetadata or CancelNone (D67).
	Cancel string
}

// ParseDSN parses a DSN of the form
// neo4j://user:pass@host:port/database?key=value (D27, D35 and D61).
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyCancel)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:   u.Hostname(),
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
	if !slices.Contains(cancels, cfg.Cancel) {
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyCancel, cfg.Cancel, dbimp.ErrInvalidValue)
	}
	cfg.Port = portHTTP
	if cfg.TLS {
		cfg.Port = portHTTPS
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// parsePath returns the database that the path of u names. No path is the
// default database, and a path of more than one segment is refused (D61).
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

// FormatDSN returns the DSN of cfg, which ParseDSN reads back as cfg.
func (cfg *Config) FormatDSN() string {
	u := url.URL{
		Scheme: Name,
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/" + cfg.Database,
		// RawPath keeps a / in the name of the database as %2F.
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
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the HTTP interface.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
