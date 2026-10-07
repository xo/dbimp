package solr

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D166).
const (
	keyTLS  = "tls"
	keyMode = "mode"
)

// defaultPort is the port of Solr, for a DSN with no port.
const defaultPort = 8983

// ModeFacet is the only value of the key mode. It is the aggregationMode
// that the server uses by default, and it gives every group of a GROUP BY.
const ModeFacet = "facet"

// modeMapReduce is the aggregationMode that the driver refuses, because the
// server cut a GROUP BY at 100 groups with no sign (D166).
const modeMapReduce = "map_reduce"

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the server, 8983 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which the driver sends with
	// basic authentication.
	User     string
	Password string
	// Collection is the collection whose /sql handler takes each statement.
	// FROM names the table, and any collection that the user can read serves
	// (D166). It is empty for a DSN with no path, and then each statement
	// needs WithDatabase.
	Collection string
	// Mode is the aggregationMode of each statement. It is ModeFacet by
	// default, and the only value that the driver takes (D166).
	Mode string
}

// ParseDSN parses a DSN of the form solr://user:pass@host:port/collection?key=value
// (D27, D35 and D166). A path with more than one segment is refused, and so is
// every key that the driver does not know.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyMode)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host: u.Hostname(),
		Port: defaultPort,
		Mode: q.String(keyMode, ModeFacet),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	switch cfg.Mode {
	case ModeFacet:
	case modeMapReduce:
		return nil, fmt.Errorf("parsing key %q: %q cut a GROUP BY at 100 groups with no sign, so the driver takes only %q: %w", keyMode, cfg.Mode, ModeFacet, dbimp.ErrNotSupported)
	default:
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyMode, cfg.Mode, dbimp.ErrInvalidValue)
	}
	if p := strings.TrimPrefix(u.Path, "/"); p != "" {
		if strings.Contains(p, "/") {
			return nil, fmt.Errorf("parsing the dsn: the path %q holds more than the name of one collection: %w", u.Path, dbimp.ErrInvalidValue)
		}
		cfg.Collection = p
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
	if cfg.Collection != "" {
		u.Path = "/" + cfg.Collection
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := url.Values{}
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	if cfg.Mode != "" && cfg.Mode != ModeFacet {
		q.Set(keyMode, cfg.Mode)
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
