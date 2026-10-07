package trino

import (
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D175). A key that starts with
// sessionPrefix sets a property of the session.
const (
	keyTLS        = "tls"
	keySource     = "source"
	keyTimeZone   = "timezone"
	keyTimeout    = "timeout"
	keyFlavor     = "flavor"
	sessionPrefix = "session."
)

// The two products that the driver serves, as the key flavor names them
// (D173 and D175).
const (
	FlavorTrino  = "trino"
	FlavorPresto = "presto"
)

// The ports of a DSN with no port (D175).
const (
	defaultPort    = 8080
	defaultTLSPort = 8443
)

// defaultSource is the source of each statement, which the servers show in
// their list of queries, for a DSN with no key source.
const defaultSource = "dbimp"

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the coordinator.
	Host string
	// Port is the port of the coordinator, 8080 by default and 8443 with
	// TLS.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User is the user of each statement. The servers need one. Password is
	// the secret that the driver sends with basic authentication, if it is
	// not empty.
	User     string
	Password string
	// Catalog and Schema are the catalog and the schema that a connection
	// starts in. Each is empty to start in none, and Schema needs a Catalog.
	Catalog string
	Schema  string
	// Source is the source of each statement, which the server shows in its
	// list of queries. It is "dbimp" by default.
	Source string
	// TimeZone is the time zone of each statement, which the driver sends as
	// the header Time-Zone, such as UTC or Asia/Jakarta. It is empty by
	// default, and the server uses its own.
	TimeZone string
	// Timeout is the time that the server gives each statement, which the
	// driver sends as the session property query_max_execution_time. Zero
	// sends none.
	Timeout time.Duration
	// Session holds the properties of the session that each connection
	// starts with, from the keys session.<name> of the DSN.
	Session map[string]string
	// Flavor is FlavorTrino or FlavorPresto, and empty to ask the server
	// with GET /v1/info.
	Flavor string
}

// ParseDSN parses a DSN of the form
// trino://user@host:port/catalog/schema?key=value (D27, D35 and D175). The
// path names the catalog and the schema, and each is optional. A key that
// the driver does not know is refused.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	raw, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("parsing the query of the dsn: %w", err)
	}
	known := []string{keyTLS, keySource, keyTimeZone, keyTimeout, keyFlavor}
	for key := range raw {
		if strings.HasPrefix(key, sessionPrefix) {
			known = append(known, key)
		}
	}
	q, err := dbimp.NewQuery(u, known...)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:     u.Hostname(),
		Source:   q.String(keySource, defaultSource),
		TimeZone: q.String(keyTimeZone, ""),
		Flavor:   q.String(keyFlavor, ""),
	}
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
	if cfg.User == "" {
		return nil, fmt.Errorf("parsing the dsn: no user, and both servers need one: %w", dbimp.ErrInvalidValue)
	}
	if err := cfg.parsePath(u); err != nil {
		return nil, err
	}
	if cfg.Source == "" {
		return nil, fmt.Errorf("parsing key %q: the source is empty: %w", keySource, dbimp.ErrInvalidValue)
	}
	if q.Has(keyTimeZone) && cfg.TimeZone == "" {
		return nil, fmt.Errorf("parsing key %q: the time zone is empty: %w", keyTimeZone, dbimp.ErrInvalidValue)
	}
	if q.Has(keyFlavor) && !slices.Contains([]string{FlavorTrino, FlavorPresto}, cfg.Flavor) {
		return nil, fmt.Errorf("parsing key %q: %q is neither %q nor %q: %w", keyFlavor, cfg.Flavor, FlavorTrino, FlavorPresto, dbimp.ErrInvalidValue)
	}
	if cfg.Timeout, err = q.Duration(keyTimeout, 0); err != nil {
		return nil, err
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("parsing key %q: %v: %w", keyTimeout, cfg.Timeout, dbimp.ErrInvalidValue)
	}
	for key := range raw {
		name, ok := strings.CutPrefix(key, sessionPrefix)
		if !ok {
			continue
		}
		if err := checkProperty(name); err != nil {
			return nil, fmt.Errorf("parsing key %q: %w", key, err)
		}
		if cfg.Session == nil {
			cfg.Session = map[string]string{}
		}
		cfg.Session[name] = q.String(key, "")
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
	if cfg.Catalog != "" {
		// RawPath keeps a slash in a name escaped, which Path cannot.
		u.Path = "/" + cfg.Catalog
		u.RawPath = "/" + url.PathEscape(cfg.Catalog)
		if cfg.Schema != "" {
			u.Path += "/" + cfg.Schema
			u.RawPath += "/" + url.PathEscape(cfg.Schema)
		}
	}
	q := url.Values{}
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	if cfg.Source != "" && cfg.Source != defaultSource {
		q.Set(keySource, cfg.Source)
	}
	if cfg.TimeZone != "" {
		q.Set(keyTimeZone, cfg.TimeZone)
	}
	if cfg.Timeout > 0 {
		q.Set(keyTimeout, cfg.Timeout.String())
	}
	if cfg.Flavor != "" {
		q.Set(keyFlavor, cfg.Flavor)
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Session)) {
		q.Set(sessionPrefix+name, cfg.Session[name])
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// parsePath reads the catalog and the schema from the path of u.
func (cfg *Config) parsePath(u *url.URL) error {
	p := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		return fmt.Errorf("parsing the dsn: the path %q has more than a catalog and a schema: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	}
	for i, part := range parts {
		name, err := url.PathUnescape(part)
		if err != nil || name == "" {
			return fmt.Errorf("parsing the dsn: the path %q: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
		}
		parts[i] = name
	}
	cfg.Catalog = parts[0]
	if len(parts) == 2 {
		cfg.Schema = parts[1]
	}
	return nil
}

// baseURL returns the URL of the coordinator of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}

// checkProperty returns an error for a name that cannot be the name of a
// property of the session. The header of the session writes a property as
// name=value, so the name holds no equals sign, no comma and no space.
func checkProperty(name string) error {
	if name == "" || strings.ContainsAny(name, "=, \t\r\n") {
		return fmt.Errorf("the property %q is not a name: %w", name, dbimp.ErrInvalidValue)
	}
	return nil
}
