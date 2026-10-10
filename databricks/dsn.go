package databricks

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D193).
const (
	keyCatalog = "catalog"
	keySchema  = "schema"
	keyTimeout = "timeout"
	keyTLS     = "tls"
)

// userName is the user name that a DSN names for a personal access token,
// as databricks-sql-go and dburl write it (D193 item 4). The token is the
// password.
const userName = "token"

// defaultPort is the port of the server, for a DSN with no port (measured).
const defaultPort = 443

// maxTimeout is the most time that a DSN names for the timeout of a
// statement. It keeps the sum of a time and a deadline inside the range of a
// time.Duration.
const maxTimeout = 24 * 365 * time.Hour

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the workspace, such as
	// dbc-1234.cloud.databricks.com.
	Host string
	// Port is the port of the server, 443 by default.
	Port int
	// Token is the personal access token, which the driver sends as a Bearer
	// token (D193 item 4 and D94).
	Token string
	// Warehouse is the id of the SQL warehouse, which the path of the DSN
	// holds. It is 16 hexadecimal digits for a warehouse of Databricks.
	Warehouse string
	// Catalog and Schema are the catalog and the schema of each statement,
	// which the driver sends in the body. Each one is optional.
	Catalog string
	Schema  string
	// Timeout is the longest time that the driver waits for one statement,
	// from the request until the end of its result. Zero waits as long as the
	// context of the caller allows.
	Timeout time.Duration
	// Insecure makes the driver use HTTP and not HTTPS. A test of the driver
	// sets it, for a fake server on the local host (D193 item 11). The DSN
	// writes it as tls=false.
	Insecure bool
}

// ParseDSN parses a DSN of the form
// databricks://token:<personal access token>@<host>/<warehouse id>?catalog=c&schema=s
// (D27, D35 and D193). The password is the token. The path is the id of the
// SQL warehouse, and the catalog and the schema are keys of the query. The
// keys are catalog, schema, timeout and tls. The timeout is a duration with a
// unit, such as 5m, and a bare number is refused. The key tls is true by
// default, and tls=false makes the driver use HTTP, for a fake server of a
// test.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyCatalog, keySchema, keyTimeout, keyTLS)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:    u.Hostname(),
		Port:    defaultPort,
		Catalog: q.String(keyCatalog, ""),
		Schema:  q.String(keySchema, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if u.User != nil {
		if name := u.User.Username(); name != "" && name != userName {
			return nil, fmt.Errorf("parsing the dsn: the user name is %q, and a token is written %s:<token>: %w", name, userName, dbimp.ErrInvalidValue)
		}
		cfg.Token, _ = u.User.Password()
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("parsing the dsn: no token, which is the password: %w", dbimp.ErrInvalidValue)
	}
	if cfg.Warehouse, err = parsePath(u); err != nil {
		return nil, err
	}
	for _, key := range []string{keyCatalog, keySchema} {
		if q.Has(key) && q.String(key, "") == "" {
			return nil, fmt.Errorf("parsing key %q: the value is empty: %w", key, dbimp.ErrInvalidValue)
		}
	}
	if cfg.Timeout, err = parseTimeout(q); err != nil {
		return nil, err
	}
	secure, err := q.Bool(keyTLS, true)
	if err != nil {
		return nil, err
	}
	cfg.Insecure = !secure
	return cfg, nil
}

// parseTimeout reads the key timeout, a duration with a unit. A bare number
// has no unit, so the error names the form.
func parseTimeout(q dbimp.Query) (time.Duration, error) {
	if v := q.String(keyTimeout, ""); v != "" && v != "0" && strings.Trim(v, "0123456789") == "" {
		return 0, fmt.Errorf("parsing key %q: %q has no unit, write a duration such as 60s or 5m: %w", keyTimeout, v, dbimp.ErrInvalidValue)
	}
	d, err := q.Duration(keyTimeout, 0)
	if err != nil {
		return 0, err
	}
	if d < 0 || d > maxTimeout {
		return 0, fmt.Errorf("parsing key %q: %v: %w", keyTimeout, d, dbimp.ErrInvalidValue)
	}
	return d, nil
}

// parsePath reads the id of the warehouse from the path of u. It is one
// name, and it is required.
func parsePath(u *url.URL) (string, error) {
	p := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	bad := fmt.Errorf("parsing the dsn: the path %q is not the id of a warehouse: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	if p == "" {
		return "", fmt.Errorf("parsing the dsn: no id of a warehouse in the path: %w", dbimp.ErrInvalidValue)
	}
	if strings.Contains(p, "/") {
		return "", bad
	}
	id, err := url.PathUnescape(p)
	if err != nil || id == "" || strings.ContainsFunc(id, func(r rune) bool { return r < ' ' || r == '/' }) {
		return "", bad
	}
	return id, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN filled.
func (cfg *Config) FormatDSN() string {
	host := cfg.Host
	if cfg.Port != 0 && cfg.Port != defaultPort {
		host = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u := url.URL{Scheme: Name, Host: host, User: url.UserPassword(userName, cfg.Token)}
	if cfg.Token == "" {
		u.User = url.User(userName)
	}
	// RawPath keeps a slash in a name escaped, which Path cannot.
	u.Path = "/" + cfg.Warehouse
	u.RawPath = "/" + url.PathEscape(cfg.Warehouse)
	q := url.Values{}
	if cfg.Catalog != "" {
		q.Set(keyCatalog, cfg.Catalog)
	}
	if cfg.Schema != "" {
		q.Set(keySchema, cfg.Schema)
	}
	if cfg.Timeout > 0 {
		q.Set(keyTimeout, cfg.Timeout.String())
	}
	if cfg.Insecure {
		q.Set(keyTLS, "false")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the server of cfg.
func (cfg *Config) baseURL() string {
	scheme := "https"
	if cfg.Insecure {
		scheme = "http"
	}
	port := cfg.Port
	if port == 0 {
		port = defaultPort
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(port))
}
