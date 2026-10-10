package cosmos

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D190).
const (
	keyTLS          = "tls"
	keyInsecure     = "insecure"
	keyPageSize     = "pagesize"
	keyPartitionKey = "partitionkey"
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the endpoint, such as
	// account.documents.azure.com or 127.0.0.1.
	Host string
	// Port is the port of the endpoint. Zero means the port of the scheme,
	// 443 for HTTPS and 80 for HTTP.
	Port int
	// TLS is true to speak HTTPS. The DSN turns it on by default, and only a
	// test with a fake server turns it off.
	TLS bool
	// Insecure is true to accept any certificate of the server, as the
	// emulator needs, because it makes a certificate for itself (D190). It is
	// false by default.
	Insecure bool
	// Database is the database of a statement, and Container is its
	// container. The path of the DSN holds them, and WithDatabase and
	// WithContainer change them for one statement. They can be empty, and
	// then each statement must name them.
	Database  string
	Container string
	// User is the user of the DSN, which the driver does not read. Key is
	// the master key of the account, as base64 text, and it is the password
	// of the DSN (D94 and D190). It signs each request.
	User string
	Key  string
	// PageSize is the number of documents that the server sends in one
	// page. Zero leaves the choice to the server, and -1 lets it choose the
	// size of each page.
	PageSize int
	// PartitionKey is the value of the partition key that a statement is
	// limited to, as text. It is nil when the statement is not limited to
	// one partition key.
	PartitionKey *string
}

// ParseDSN parses a DSN of the form
// cosmos://user:key@host:port/database/container (D27, D35 and D190). The
// user is any text, because the driver does not read it, and the password is
// the master key of the account, as base64 text. The path holds the database
// and the container, and it can hold the database only or nothing. The keys
// are tls, which is true by default, insecure, which is false by default and
// accepts any certificate of the server, pagesize, which is 0 by default,
// and partitionkey, which has no default. A DSN with another key, a key that
// appears twice, a path of more than two names, or no key is refused.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyInsecure, keyPageSize, keyPartitionKey)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Host: u.Hostname()}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if cfg.TLS, err = q.Bool(keyTLS, true); err != nil {
		return nil, err
	}
	if cfg.Insecure, err = q.Bool(keyInsecure, false); err != nil {
		return nil, err
	}
	if cfg.PageSize, err = q.Int(keyPageSize, 0); err != nil {
		return nil, err
	}
	if cfg.PageSize < -1 {
		return nil, fmt.Errorf("parsing key %q: %d is less than -1: %w", keyPageSize, cfg.PageSize, dbimp.ErrInvalidValue)
	}
	if q.Has(keyPartitionKey) {
		cfg.PartitionKey = new(q.String(keyPartitionKey, ""))
	}
	if cfg.Database, cfg.Container, err = parsePath(u); err != nil {
		return nil, err
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Key, _ = u.User.Password()
	}
	if cfg.Key == "" {
		return nil, fmt.Errorf("parsing the dsn: the signature needs the master key as the password: %w", dbimp.ErrInvalidValue)
	}
	// The error never holds the key (D94).
	if _, err := base64.StdEncoding.DecodeString(cfg.Key); err != nil {
		return nil, fmt.Errorf("parsing the dsn: the master key is not base64 text: %w", dbimp.ErrInvalidValue)
	}
	return cfg, nil
}

// parsePath reads the database and the container from the path of u. Each is
// a name with no slash, and a container needs a database.
func parsePath(u *url.URL) (string, string, error) {
	p, _ := strings.CutPrefix(u.EscapedPath(), "/")
	p, _ = strings.CutSuffix(p, "/")
	if p == "" {
		return "", "", nil
	}
	parts := strings.Split(p, "/")
	if len(parts) > 2 {
		return "", "", fmt.Errorf("parsing the dsn: the path has %d names, and it holds a database and a container only: %w", len(parts), dbimp.ErrInvalidValue)
	}
	names := make([]string, 2)
	for i, part := range parts {
		name, err := url.PathUnescape(part)
		if err != nil {
			return "", "", fmt.Errorf("parsing the dsn: the path: %w", dbimp.ErrInvalidValue)
		}
		if err := checkName(name); err != nil {
			return "", "", fmt.Errorf("parsing the dsn: the path: %w", err)
		}
		names[i] = name
	}
	return names[0], names[1], nil
}

// checkName returns an error for a name that cannot name a database or a
// container: an empty name, or a name with a character that the server
// refuses in a name, which are /, \, ? and # (recorded: "a container with a
// name that is not allowed").
func checkName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("the name is empty: %w", dbimp.ErrInvalidValue)
	case strings.ContainsAny(name, `/\?#`):
		return fmt.Errorf("the name %q has a character that a name cannot have: %w", name, dbimp.ErrInvalidValue)
	}
	return nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN filled.
func (cfg *Config) FormatDSN() string {
	u := url.URL{Scheme: Name, Host: cfg.hostPort(), User: url.UserPassword(cfg.User, cfg.Key)}
	switch {
	case cfg.Container != "":
		u.Path = "/" + cfg.Database + "/" + cfg.Container
	case cfg.Database != "":
		u.Path = "/" + cfg.Database
	}
	q := url.Values{}
	if !cfg.TLS {
		q.Set(keyTLS, "false")
	}
	if cfg.Insecure {
		q.Set(keyInsecure, "true")
	}
	if cfg.PageSize != 0 {
		q.Set(keyPageSize, strconv.Itoa(cfg.PageSize))
	}
	if cfg.PartitionKey != nil {
		q.Set(keyPartitionKey, *cfg.PartitionKey)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// hostPort returns the host of cfg, with its port when it has one, and with
// brackets for an IPv6 address.
func (cfg *Config) hostPort() string {
	host := cfg.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if cfg.Port == 0 {
		return host
	}
	return host + ":" + strconv.Itoa(cfg.Port)
}

// baseURL returns the URL of the endpoint of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + cfg.hostPort()
}
