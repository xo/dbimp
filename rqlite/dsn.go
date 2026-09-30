package rqlite

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D141).
const (
	keyTLS       = "tls"
	keyLevel     = "level"
	keyFreshness = "freshness"
)

// defaultPort is the port of the HTTP API, for a DSN with no port (D141).
const defaultPort = 4001

// The values of the key level, the read consistency of a query (D141). An
// empty level sends none, and the server reads at LevelWeak.
const (
	// LevelNone reads the SQLite of the node, with no check of the leader.
	LevelNone = "none"
	// LevelWeak reads on the leader, after the node checks that it leads.
	LevelWeak = "weak"
	// LevelLinearizable reads on the leader, after a round of Raft that
	// shows that it still leads.
	LevelLinearizable = "linearizable"
	// LevelStrong sends the read through the log of Raft.
	LevelStrong = "strong"
	// LevelAuto is LevelNone on a read-only node, and LevelWeak on any
	// other.
	LevelAuto = "auto"
)

// levels are the values of the key level. The server reads an unknown level
// as weak with no error (measured), so the driver refuses one.
var levels = []string{"", LevelNone, LevelWeak, LevelLinearizable, LevelStrong, LevelAuto}

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the node.
	Host string
	// Port is the port of the HTTP API of the node, 4001 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, which go by HTTP Basic
	// authentication. With no user, the driver sends no credentials.
	User     string
	Password string
	// Level is the read consistency of a query, or "" for the default of
	// the server (D141).
	Level string
	// Freshness is how old the data of a read with LevelNone can be, or zero
	// for no bound.
	Freshness time.Duration
}

// ParseDSN parses a DSN of the form rqlite://user:pass@host:port?key=value
// (D27, D35 and D141). A DSN with a path is refused, because rqlite has one
// database.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyLevel, keyFreshness)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:  u.Hostname(),
		Port:  defaultPort,
		Level: q.String(keyLevel, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: rqlite has one database, so the URL has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if !slices.Contains(levels, cfg.Level) {
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyLevel, cfg.Level, dbimp.ErrInvalidValue)
	}
	if cfg.Freshness, err = q.Duration(keyFreshness, 0); err != nil {
		return nil, err
	}
	if cfg.Freshness < 0 {
		return nil, fmt.Errorf("parsing key %q: %v: %w", keyFreshness, cfg.Freshness, dbimp.ErrInvalidValue)
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
	if cfg.Level != "" {
		q.Set(keyLevel, cfg.Level)
	}
	if cfg.Freshness != 0 {
		q.Set(keyFreshness, cfg.Freshness.String())
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the node of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
