package snowflake

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D183).
const (
	keyRole      = "role"
	keyWarehouse = "warehouse"
	keyTimeout   = "timeout"
	keyTimeZone  = "timezone"
)

// defaultPort is the port of the server, for a DSN with no port. The
// connection is always TLS (D183).
const defaultPort = 443

// hostSuffix ends the host of every account (docs/SNOWFLAKE.md).
const hostSuffix = ".snowflakecomputing.com"

// maxTimeout is the most time that a DSN names for the timeout of a
// statement, which is the most seconds that the body of a request holds. The
// server has its own limit.
const maxTimeout = (1<<31 - 1) * time.Second

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the account, such as
	// <org>-<account>.snowflakecomputing.com.
	Host string
	// Port is the port of the server, 443 by default.
	Port int
	// User is the login user. The claims of the token name it in upper case.
	User string
	// Password is the private key of the user, as the base64url text of its
	// PKCS8 DER bytes (D183). The driver signs a token with it, and sends the
	// token and never the key.
	Password string
	// Database and Schema are the database and the schema of each statement,
	// which the driver sends in the body. Each one is optional.
	Database string
	Schema   string
	// Role is the role of each statement, and empty for the default role of
	// the user.
	Role string
	// Warehouse is the warehouse of each statement, and empty for the
	// default warehouse of the user.
	Warehouse string
	// TimeZone is the time zone of the session of each statement, which the
	// driver sends as the parameter TIMEZONE, such as UTC or Asia/Jakarta.
	// Empty leaves the zone of the account. A timestamp_ltz value takes this
	// zone too, and time.Local when it is empty. The name Local is time.Local.
	TimeZone string
	// Timeout is the time that the server gives each statement. The driver
	// rounds it up to whole seconds for the server. Zero sends none.
	Timeout time.Duration
}

// ParseDSN parses a DSN of the form
// snowflake://user:key@<org>-<account>.snowflakecomputing.com/database/schema?role=r
// (D27, D35 and D183). The password is the private key. The path holds the
// database and the schema, and each is optional. The keys are role,
// warehouse, timeout and timezone. The timeout is a duration with a unit, such
// as 60s or 1m, and a bare number is refused, and the driver refuses any
// other. The host must end in .snowflakecomputing.com, unless the DSN names a
// port or the host is an IP address, which a test of the driver uses for a
// fake server.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyRole, keyWarehouse, keyTimeout, keyTimeZone)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:      u.Hostname(),
		Port:      defaultPort,
		Role:      q.String(keyRole, ""),
		Warehouse: q.String(keyWarehouse, ""),
		TimeZone:  q.String(keyTimeZone, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if !isAccountHost(cfg.Host) && u.Port() == "" && !isAddress(cfg.Host) {
		return nil, fmt.Errorf("parsing the dsn: the host %q does not end in %s: %w", cfg.Host, hostSuffix, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("parsing the dsn: no user: %w", dbimp.ErrInvalidValue)
	}
	if _, err := parseKey(cfg.Password); err != nil {
		return nil, fmt.Errorf("parsing the dsn: the password: %w", err)
	}
	if cfg.Database, cfg.Schema, err = parsePath(u); err != nil {
		return nil, err
	}
	for _, key := range []string{keyRole, keyWarehouse, keyTimeZone} {
		if q.Has(key) && q.String(key, "") == "" {
			return nil, fmt.Errorf("parsing key %q: the value is empty: %w", key, dbimp.ErrInvalidValue)
		}
	}
	if cfg.Timeout, err = parseTimeout(q); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseTimeout reads the key timeout, a duration with a unit. A bare number
// has no unit, so the error names the form.
func parseTimeout(q dbimp.Query) (time.Duration, error) {
	if v := q.String(keyTimeout, ""); v != "" && v != "0" && strings.Trim(v, "0123456789") == "" {
		return 0, fmt.Errorf("parsing key %q: %q has no unit, write a duration such as 60s or 1m: %w", keyTimeout, v, dbimp.ErrInvalidValue)
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

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN or NewConnector filled.
func (cfg *Config) FormatDSN() string {
	host := cfg.Host
	if cfg.Port != 0 && cfg.Port != defaultPort {
		host = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u := url.URL{Scheme: Name, Host: host}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
		if cfg.Password == "" {
			u.User = url.User(cfg.User)
		}
	}
	if cfg.Database != "" || cfg.Schema != "" {
		// RawPath keeps a slash in a name escaped, which Path cannot.
		u.Path = "/" + cfg.Database
		u.RawPath = "/" + url.PathEscape(cfg.Database)
		if cfg.Schema != "" {
			u.Path += "/" + cfg.Schema
			u.RawPath += "/" + url.PathEscape(cfg.Schema)
		}
	}
	q := url.Values{}
	if cfg.Role != "" {
		q.Set(keyRole, cfg.Role)
	}
	if cfg.Warehouse != "" {
		q.Set(keyWarehouse, cfg.Warehouse)
	}
	if cfg.Timeout > 0 {
		q.Set(keyTimeout, cfg.Timeout.String())
	}
	if cfg.TimeZone != "" {
		q.Set(keyTimeZone, cfg.TimeZone)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// parsePath reads the database and the schema from the path of u. Each is
// optional, and a schema with no database is written //schema.
func parsePath(u *url.URL) (string, string, error) {
	p := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if p == "" {
		return "", "", nil
	}
	parts := strings.Split(p, "/")
	bad := fmt.Errorf("parsing the dsn: the path %q is not a database and a schema: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	if len(parts) > 2 || len(parts) == 2 && parts[1] == "" || len(parts) == 1 && parts[0] == "" {
		return "", "", bad
	}
	names := make([]string, 2)
	for i, part := range parts {
		name, err := url.PathUnescape(part)
		if err != nil {
			return "", "", bad
		}
		names[i] = name
	}
	return names[0], names[1], nil
}

// isAccountHost reports whether host is the host of an account.
func isAccountHost(host string) bool {
	h := strings.ToLower(host)
	return strings.HasSuffix(h, hostSuffix) && len(h) > len(hostSuffix)
}

// isAddress reports whether host is an IP address.
func isAddress(host string) bool {
	_, err := netip.ParseAddr(host)
	return err == nil
}

// account returns the name of the account for the claims of the token: the
// host without its suffix, cut at the first dot, in upper case (D183 and
// docs/SNOWFLAKE.md). A host of a test, which has no suffix, is the account as
// it stands.
func (cfg *Config) account() string {
	h := strings.ToLower(cfg.Host)
	if isAccountHost(h) {
		h, _, _ = strings.Cut(strings.TrimSuffix(h, hostSuffix), ".")
	}
	return strings.ToUpper(h)
}

// baseURL returns the URL of the server of cfg. The connection is always
// TLS (D183).
func (cfg *Config) baseURL() string {
	return "https://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
