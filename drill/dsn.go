package drill

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D165).
const (
	keyTLS       = "tls"
	keySchema    = "schema"
	keyAutoLimit = "autolimit"
)

// defaultPort is the port of the web server of a Drillbit, for a DSN with no
// port (D165).
const defaultPort = 8047

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the Drillbit.
	Host string
	// Port is the port of the web server, 8047 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which the driver sends with
	// basic authentication.
	User     string
	Password string
	// Schema is the schema of a table whose name has none, such as dfs.tmp,
	// which the driver sends as defaultSchema. It is empty by default, and
	// the driver then sends none.
	Schema string
	// AutoLimit is the most rows that the server sends for one query, which
	// the driver sends as autoLimit. Zero sends none, and the server then
	// sends every row. A result as long as AutoLimit does not say whether
	// rows were left out (D165).
	AutoLimit int
}

// ParseDSN parses a DSN of the form drill://user:pass@host:port?key=value
// (D27, D35 and D165). A DSN with a path is refused, because the schema is a
// key and not a path, and so is every key that the driver does not know.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keySchema, keyAutoLimit)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:   u.Hostname(),
		Port:   defaultPort,
		Schema: q.String(keySchema, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: the schema is the key %s, so the URL has no path: %w", p, keySchema, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if q.Has(keySchema) && cfg.Schema == "" {
		return nil, fmt.Errorf("parsing key %q: the schema is empty: %w", keySchema, dbimp.ErrInvalidValue)
	}
	if cfg.AutoLimit, err = q.Int(keyAutoLimit, 0); err != nil {
		return nil, err
	}
	if q.Has(keyAutoLimit) && cfg.AutoLimit < 1 {
		return nil, fmt.Errorf("parsing key %q: %d: the limit is 1 or more: %w", keyAutoLimit, cfg.AutoLimit, dbimp.ErrInvalidValue)
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
	if cfg.Schema != "" {
		q.Set(keySchema, cfg.Schema)
	}
	if cfg.AutoLimit > 0 {
		q.Set(keyAutoLimit, strconv.Itoa(cfg.AutoLimit))
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
