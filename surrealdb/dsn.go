package surrealdb

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D48, D49 and D51).
const (
	keyTLS      = "tls"
	keyAuth     = "auth"
	keyEncoding = "encoding"
)

// defaultPort is the port of the HTTP interface of SurrealDB, with TLS or
// without (D48).
const defaultPort = 8000

// The levels of a user, for the key auth (D51).
const (
	AuthRoot      = "root"
	AuthNamespace = "namespace"
	AuthDatabase  = "database"
)

// The encodings, for the key encoding (D49).
const (
	EncodingCBOR = "cbor"
	EncodingJSON = "json"
)

var (
	auths     = []string{AuthRoot, AuthNamespace, AuthDatabase}
	encodings = []string{EncodingCBOR, EncodingJSON}
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
	// User and Password are the credentials, sent with basic
	// authentication.
	User     string
	Password string
	// Namespace and Database name where each statement runs (D48).
	Namespace string
	Database  string
	// Auth is where the user is defined: AuthRoot, AuthNamespace or
	// AuthDatabase (D51).
	Auth string
	// Encoding is EncodingCBOR, or EncodingJSON to read the requests and the
	// responses while debugging (D49).
	Encoding string
}

// ParseDSN parses a DSN of the form
// surrealdb://user:pass@host:port/namespace/database?key=value (D27, D35
// and D48).
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyAuth, keyEncoding)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:     u.Hostname(),
		Port:     defaultPort,
		Auth:     q.String(keyAuth, AuthRoot),
		Encoding: q.String(keyEncoding, EncodingCBOR),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.Namespace, cfg.Database, err = parsePath(u); err != nil {
		return nil, err
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	switch {
	case !slices.Contains(auths, cfg.Auth):
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyAuth, cfg.Auth, dbimp.ErrInvalidValue)
	case !slices.Contains(encodings, cfg.Encoding):
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyEncoding, cfg.Encoding, dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// parsePath returns the namespace and the database of the path of u, which
// must hold exactly the two (D48). Each is decoded by the rules of net/url,
// so a name with a slash is written %2F.
func parsePath(u *url.URL) (string, string, error) {
	parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("parsing the dsn: the path %q is not /namespace/database: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	}
	ns, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("parsing the namespace of the dsn: %w", dbimp.ErrInvalidValue)
	}
	db, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("parsing the database of the dsn: %w", dbimp.ErrInvalidValue)
	}
	return ns, db, nil
}

// FormatDSN returns the DSN of cfg, which ParseDSN reads back as cfg.
func (cfg *Config) FormatDSN() string {
	u := url.URL{
		Scheme:  Name,
		Host:    net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:    "/" + cfg.Namespace + "/" + cfg.Database,
		RawPath: "/" + url.PathEscape(cfg.Namespace) + "/" + url.PathEscape(cfg.Database),
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := url.Values{}
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	if cfg.Auth != "" && cfg.Auth != AuthRoot {
		q.Set(keyAuth, cfg.Auth)
	}
	if cfg.Encoding != "" && cfg.Encoding != EncodingCBOR {
		q.Set(keyEncoding, cfg.Encoding)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the server.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
