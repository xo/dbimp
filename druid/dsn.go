package druid

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D164).
const (
	keyTLS      = "tls"
	keyTimeZone = "timezone"
	keyTimeout  = "timeout"
)

// defaultPort is the port of the Router, for a DSN with no port (D164).
const defaultPort = 8888

// defaultTimeZone is the time zone of a DSN with no key timezone (D164).
const defaultTimeZone = "UTC"

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the Router or of the Broker.
	Host string
	// Port is the port of the server, 8888 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which the driver sends with
	// basic authentication.
	User     string
	Password string
	// TimeZone is the time zone of each query, which the driver sends as
	// sqlTimeZone, such as UTC or Asia/Jakarta. It is UTC by default.
	TimeZone string
	// Timeout is the time that the server gives each query, which the driver
	// sends as timeout in the context of the query. Zero sends none.
	Timeout time.Duration
}

// ParseDSN parses a DSN of the form druid://user:pass@host:port?key=value
// (D27, D35 and D164). A DSN with a path is refused, because Druid has no
// database to choose, and so is every key that the driver does not know.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyTimeZone, keyTimeout)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:     u.Hostname(),
		Port:     defaultPort,
		TimeZone: q.String(keyTimeZone, defaultTimeZone),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: Druid has no database to choose, so the URL has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if cfg.TimeZone == "" {
		return nil, fmt.Errorf("parsing key %q: the time zone is empty: %w", keyTimeZone, dbimp.ErrInvalidValue)
	}
	if cfg.Timeout, err = q.Duration(keyTimeout, 0); err != nil {
		return nil, err
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("parsing key %q: %v: %w", keyTimeout, cfg.Timeout, dbimp.ErrInvalidValue)
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
	if cfg.TimeZone != "" && cfg.TimeZone != defaultTimeZone {
		q.Set(keyTimeZone, cfg.TimeZone)
	}
	if cfg.Timeout > 0 {
		q.Set(keyTimeout, cfg.Timeout.String())
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
