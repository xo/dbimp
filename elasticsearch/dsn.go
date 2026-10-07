package elasticsearch

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D167).
const (
	keyTLS       = "tls"
	keyAuth      = "auth"
	keyFetchSize = "fetch_size"
	keyTimeZone  = "time_zone"
	keyLeniency  = "field_multi_value_leniency"
	keyCatalog   = "catalog"
)

// The values of the key auth. AuthBasic sends the user and the password with
// basic authentication. AuthAPIKey sends the password as an API key, in the
// header Authorization with the scheme ApiKey, and no user (D94 and D167).
const (
	AuthBasic  = dbimp.AuthBasic
	AuthAPIKey = "apikey"
)

// defaultPort is the port of the HTTP interface, for a DSN with no port. It
// is the same with TLS (D167).
const defaultPort = 9200

// defaultFetchSize is the rows of a page, for a DSN with no key fetch_size.
// It is the default of the server (measured).
const defaultFetchSize = 1000

// defaultTimeZone is the time zone of a DSN with no key time_zone (D167).
const defaultTimeZone = "UTC"

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the HTTP interface, 9200 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// Auth is AuthBasic or AuthAPIKey, and AuthBasic by default.
	Auth string
	// User and Password are the credentials. With AuthBasic the driver sends
	// both with basic authentication. With AuthAPIKey it sends the password
	// as an API key. With neither, the driver sends no credentials.
	User     string
	Password string
	// FetchSize is the rows of a page, as fetch_size, 1000 by default. The
	// server cannot give a page larger than index.max_result_window of an
	// index, which is 10000 by default (measured).
	FetchSize int
	// TimeZone is the time zone of each statement, as time_zone, such as UTC,
	// Asia/Jakarta or +05:30. It is UTC by default.
	TimeZone string
	// FieldMultiValueLeniency sets field_multi_value_leniency. It is false by
	// default, and then a field that holds several values fails the
	// statement. When it is true, such a field gives its first value, and
	// the other values are lost with no sign (D167).
	FieldMultiValueLeniency bool
	// Catalog is the cluster of each statement, as catalog, and empty for the
	// local cluster.
	Catalog string
}

// ParseDSN parses a DSN of the form
// elasticsearch://user:pass@host:port?key=value (D27, D35 and D167). A DSN
// with a path is refused, because Elasticsearch has no database to choose,
// and so is every key that the driver does not know.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyAuth, keyFetchSize, keyTimeZone, keyLeniency, keyCatalog)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:     u.Hostname(),
		Port:     defaultPort,
		Auth:     q.String(keyAuth, AuthBasic),
		TimeZone: q.String(keyTimeZone, defaultTimeZone),
		Catalog:  q.String(keyCatalog, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: Elasticsearch has no database to choose, so the URL has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if cfg.Auth != AuthBasic && cfg.Auth != AuthAPIKey {
		return nil, fmt.Errorf("parsing key %q: %q is not %q or %q: %w", keyAuth, cfg.Auth, AuthBasic, AuthAPIKey, dbimp.ErrInvalidValue)
	}
	if cfg.Auth == AuthAPIKey && cfg.Password == "" {
		return nil, fmt.Errorf("parsing key %q: %q needs the API key as the password of the URL: %w", keyAuth, AuthAPIKey, dbimp.ErrInvalidValue)
	}
	if cfg.FetchSize, err = q.Int(keyFetchSize, defaultFetchSize); err != nil {
		return nil, err
	}
	if cfg.FetchSize < 1 {
		return nil, fmt.Errorf("parsing key %q: %d: %w", keyFetchSize, cfg.FetchSize, dbimp.ErrInvalidValue)
	}
	if cfg.TimeZone == "" {
		return nil, fmt.Errorf("parsing key %q: the time zone is empty: %w", keyTimeZone, dbimp.ErrInvalidValue)
	}
	if cfg.FieldMultiValueLeniency, err = q.Bool(keyLeniency, false); err != nil {
		return nil, err
	}
	if q.Has(keyCatalog) && cfg.Catalog == "" {
		return nil, fmt.Errorf("parsing key %q: the catalog is empty: %w", keyCatalog, dbimp.ErrInvalidValue)
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
	if cfg.Auth != "" && cfg.Auth != AuthBasic {
		q.Set(keyAuth, cfg.Auth)
	}
	if cfg.FetchSize != 0 && cfg.FetchSize != defaultFetchSize {
		q.Set(keyFetchSize, strconv.Itoa(cfg.FetchSize))
	}
	if cfg.TimeZone != "" && cfg.TimeZone != defaultTimeZone {
		q.Set(keyTimeZone, cfg.TimeZone)
	}
	if cfg.FieldMultiValueLeniency {
		q.Set(keyLeniency, "true")
	}
	if cfg.Catalog != "" {
		q.Set(keyCatalog, cfg.Catalog)
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
