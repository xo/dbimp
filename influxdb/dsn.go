package influxdb

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D82).
const (
	keySQLMode  = "sqlmode"
	keyVersion  = "version"
	keyDescribe = "describe"
	keyChunked  = "chunked"
	keyRP       = "rp"
	keyTLS      = "tls"
)

// The values of the key sqlmode, which say which dialect a connection speaks
// (D78).
const (
	// SQLModeDisable speaks InfluxQL, and sends no request to learn the
	// release.
	SQLModeDisable = "disable"
	// SQLModeAllow speaks SQL, and sends no request to learn the release.
	SQLModeAllow = "allow"
	// SQLModePrefer learns the release from GET /ping, and speaks SQL to
	// InfluxDB 3 and later and InfluxQL to InfluxDB 1 and 2. It is the
	// default.
	SQLModePrefer = "prefer"
	// SQLModeRequire learns the release from GET /ping, and speaks SQL. The
	// connection fails on InfluxDB 1 and 2.
	SQLModeRequire = "require"
)

// sqlModes are the values of the key sqlmode.
var sqlModes = []string{SQLModeDisable, SQLModeAllow, SQLModePrefer, SQLModeRequire}

// The values of the key describe, which say where SQL reads its columns
// (D80).
const (
	// DescribeAlways sends DESCRIBE before each statement, and reads the
	// columns and their types from it. It is the default.
	DescribeAlways = "always"
	// DescribeDisable reads the columns from the keys of the first row, and
	// learns no types.
	DescribeDisable = "disable"
)

// describes are the values of the key describe.
var describes = []string{DescribeAlways, DescribeDisable}

// The values of the key chunked, which say whether InfluxQL asks for its
// answer in chunks (D83).
const (
	// ChunkedPrefer asks InfluxDB 1 for chunks, and InfluxDB 2 and 3 for one
	// document. It is the default.
	ChunkedPrefer = "prefer"
	// ChunkedDisable asks every release for one document.
	ChunkedDisable = "disable"
)

// chunkeds are the values of the key chunked.
var chunkeds = []string{ChunkedPrefer, ChunkedDisable}

// The ports of InfluxDB 1 and 2, and of InfluxDB 3 (D82).
const (
	portV1 = 8086
	portV3 = 8181
)

// defaultVersion is the major release that the driver assumes when it sends
// no ping (D78).
const defaultVersion = 3

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the server: 8086 for Version 1 or 2, and 8181
	// otherwise, by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials, sent with basic authentication
	// on every request. A token is the password. With neither, the driver
	// sends no credentials, for a server that runs with authentication off.
	User     string
	Password string
	// Database is the database, which the driver sends as db. It is empty
	// for a DSN with no path, and then a statement names its database.
	Database string
	// RetentionPolicy is the retention policy that InfluxQL reads, sent as
	// rp, or empty.
	RetentionPolicy string
	// SQLMode is SQLModeDisable, SQLModeAllow, SQLModePrefer or
	// SQLModeRequire (D78).
	SQLMode string
	// Version is the major release, 1, 2 or 3, that the driver talks to when
	// SQLMode is SQLModeDisable or SQLModeAllow (D78). It also chooses the
	// default port, under every SQLMode (D82).
	Version int
	// Describe is DescribeAlways or DescribeDisable (D80).
	Describe string
	// Chunked is ChunkedPrefer or ChunkedDisable (D83).
	Chunked string
}

// ParseDSN parses a DSN of the form
// influxdb://user:pass@host:port/database?key=value (D27, D35 and D82).
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keySQLMode, keyVersion, keyDescribe, keyChunked, keyRP, keyTLS)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:            u.Hostname(),
		RetentionPolicy: q.String(keyRP, ""),
		SQLMode:         q.String(keySQLMode, SQLModePrefer),
		Describe:        q.String(keyDescribe, DescribeAlways),
		Chunked:         q.String(keyChunked, ChunkedPrefer),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if cfg.Database, err = parsePath(u); err != nil {
		return nil, err
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if cfg.Version, err = q.Int(keyVersion, defaultVersion); err != nil {
		return nil, err
	}
	for _, k := range []struct {
		key, val string
		vals     []string
	}{
		{keySQLMode, cfg.SQLMode, sqlModes},
		{keyDescribe, cfg.Describe, describes},
		{keyChunked, cfg.Chunked, chunkeds},
	} {
		if !slices.Contains(k.vals, k.val) {
			return nil, fmt.Errorf("parsing key %q: %q: %w", k.key, k.val, dbimp.ErrInvalidValue)
		}
	}
	if cfg.Version < 1 || cfg.Version > defaultVersion {
		return nil, fmt.Errorf("parsing key %q: %d: %w", keyVersion, cfg.Version, dbimp.ErrInvalidValue)
	}
	cfg.Port = cfg.defaultPort()
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// parsePath returns the database that the path of u names, or "" for no
// path. A path of more than one segment is refused (D82).
func parsePath(u *url.URL) (string, error) {
	p := u.EscapedPath()
	if p == "" || p == "/" {
		return "", nil
	}
	seg := strings.TrimPrefix(p, "/")
	if strings.Contains(seg, "/") {
		return "", fmt.Errorf("parsing the dsn: the path %q names more than one database: %w", p, dbimp.ErrInvalidValue)
	}
	db, err := url.PathUnescape(seg)
	if err != nil {
		return "", fmt.Errorf("parsing the dsn: the path %q: %w", p, dbimp.ErrInvalidValue)
	}
	return db, nil
}

// FormatDSN returns the DSN of cfg, which ParseDSN reads back as cfg.
func (cfg *Config) FormatDSN() string {
	u := url.URL{
		Scheme: Name,
		Host:   net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
	}
	if cfg.Database != "" {
		// RawPath keeps a / in the name of the database as %2F.
		u.Path, u.RawPath = "/"+cfg.Database, "/"+url.PathEscape(cfg.Database)
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := url.Values{}
	if cfg.SQLMode != "" && cfg.SQLMode != SQLModePrefer {
		q.Set(keySQLMode, cfg.SQLMode)
	}
	if cfg.Version != 0 && cfg.Version != defaultVersion {
		q.Set(keyVersion, strconv.Itoa(cfg.Version))
	}
	if cfg.Describe != "" && cfg.Describe != DescribeAlways {
		q.Set(keyDescribe, cfg.Describe)
	}
	if cfg.Chunked != "" && cfg.Chunked != ChunkedPrefer {
		q.Set(keyChunked, cfg.Chunked)
	}
	if cfg.RetentionPolicy != "" {
		q.Set(keyRP, cfg.RetentionPolicy)
	}
	if cfg.TLS {
		q.Set(keyTLS, "true")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// defaultPort returns the port of a DSN with no port: 8086 for version 1 or
// 2, and 8181 otherwise (D82).
func (cfg *Config) defaultPort() int {
	if cfg.Version == 1 || cfg.Version == 2 {
		return portV1
	}
	return portV3
}

// baseURL returns the URL of the server.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
