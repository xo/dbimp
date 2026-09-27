package couchbase

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D38 and D43).
const (
	keyTLS             = "tls"
	keyQueryContext    = "query_context"
	keyScanConsistency = "scan_consistency"
	keyTimeout         = "timeout"
	keyDurability      = "durability_level"
	keyTxTimeout       = "txtimeout"
)

// The ports of the query service.
const (
	portHTTP  = 8093
	portHTTPS = 18093
)

// scanConsistencies are the values of the key scan_consistency. The server
// also takes at_plus, which needs a scan vector that only a client of the
// data service has (docs/COUCHBASE.md).
var scanConsistencies = []string{"not_bounded", "request_plus"}

// durabilities are the values of the key durability_level.
var durabilities = []string{"none", "majority", "majorityAndPersistActive", "persistToMajority"}

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the query service.
	Host string
	// Port is the port of the query service, 8093 by default, or 18093 with
	// TLS.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, sent with basic
	// authentication.
	User     string
	Password string

	// QueryContext names the bucket and the scope of a collection that a
	// statement names alone, such as "default:dbmeta._default".
	QueryContext string
	// ScanConsistency is "not_bounded" or "request_plus", or "" for the
	// default of the server.
	ScanConsistency string
	// Timeout is the timeout that the server enforces for each statement, or
	// zero for none.
	Timeout time.Duration
	// Durability is the durability of a transaction, or "" for the default
	// of the server (D43).
	Durability string
	// TxTimeout is how long a transaction lasts before the server ends it, or
	// zero for the default of the server, which is 15 seconds (D46).
	TxTimeout time.Duration
}

// ParseDSN parses a DSN of the form
// couchbase://user:pass@host:port/?key=value (D27, D35 and D38).
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q is not empty: %w", u.Path, dbimp.ErrInvalidValue)
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyQueryContext, keyScanConsistency, keyTimeout, keyDurability, keyTxTimeout)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:            u.Hostname(),
		QueryContext:    q.String(keyQueryContext, ""),
		ScanConsistency: q.String(keyScanConsistency, ""),
		Durability:      q.String(keyDurability, ""),
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
	if cfg.Timeout, err = q.Duration(keyTimeout, 0); err != nil {
		return nil, err
	}
	if cfg.TxTimeout, err = q.Duration(keyTxTimeout, 0); err != nil {
		return nil, err
	}
	switch {
	case cfg.ScanConsistency != "" && !slices.Contains(scanConsistencies, cfg.ScanConsistency):
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyScanConsistency, cfg.ScanConsistency, dbimp.ErrInvalidValue)
	case cfg.Durability != "" && !slices.Contains(durabilities, cfg.Durability):
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyDurability, cfg.Durability, dbimp.ErrInvalidValue)
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

// FormatDSN returns the DSN of cfg, which ParseDSN reads back as cfg.
func (cfg *Config) FormatDSN() string {
	u := url.URL{
		Scheme: Name,
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:   "/",
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := url.Values{}
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	if cfg.QueryContext != "" {
		q.Set(keyQueryContext, cfg.QueryContext)
	}
	if cfg.ScanConsistency != "" {
		q.Set(keyScanConsistency, cfg.ScanConsistency)
	}
	if cfg.Timeout != 0 {
		q.Set(keyTimeout, cfg.Timeout.String())
	}
	if cfg.Durability != "" {
		q.Set(keyDurability, cfg.Durability)
	}
	if cfg.TxTimeout != 0 {
		q.Set(keyTxTimeout, cfg.TxTimeout.String())
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the query service.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
