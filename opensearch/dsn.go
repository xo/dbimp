package opensearch

import (
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D168).
const (
	keyTLS       = "tls"
	keyFetchSize = "fetch_size"
)

// defaultPort is the port of the HTTP interface, for a DSN with no port. It
// is the same with TLS (D168).
const defaultPort = 9200

// defaultFetchSize is the rows of a page, for a DSN with no key fetch_size
// (D168).
const defaultFetchSize = 1000

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the HTTP interface, 9200 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which the driver sends with
	// basic authentication. With neither, the driver sends none (D94).
	User     string
	Password string
	// FetchSize is the rows of a page of a plain SELECT, as fetch_size, 1000
	// by default. The server cannot give a page larger than
	// index.max_result_window of an index, which is 10000 by default
	// (measured).
	FetchSize int
}

// ParseDSN parses a DSN of the form opensearch://user:pass@host:port?key=value
// (D27, D35 and D168). A DSN with a path is refused, because OpenSearch has
// no database to choose, and so is every key that the driver does not know.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyFetchSize)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Host: u.Hostname(), Port: defaultPort}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: OpenSearch has no database to choose, so the URL has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if cfg.FetchSize, err = q.Int(keyFetchSize, defaultFetchSize); err != nil {
		return nil, err
	}
	if cfg.FetchSize < 1 {
		return nil, fmt.Errorf("parsing key %q: %d: %w", keyFetchSize, cfg.FetchSize, dbimp.ErrInvalidValue)
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
	if cfg.FetchSize != 0 && cfg.FetchSize != defaultFetchSize {
		q.Set(keyFetchSize, strconv.Itoa(cfg.FetchSize))
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
