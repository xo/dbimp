package dynamodb

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D169).
const (
	keyTLS    = "tls"
	keyRegion = "region"
	keyToken  = "token"
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the endpoint, such as localhost or
	// dynamodb.us-east-1.amazonaws.com.
	Host string
	// Port is the port of the endpoint. Zero means the port of the scheme,
	// 443 for HTTPS and 80 for HTTP.
	Port int
	// TLS is true to speak HTTPS. The DSN turns it on by default.
	TLS bool
	// Region is the region of AWS that each signature names, such as
	// us-east-1. It has no default.
	Region string
	// User is the access key, and Password is the secret key, which the
	// driver signs each request with (D94 and D169).
	User     string
	Password string
	// Token is the session token of temporary credentials. The driver sends
	// it in the header X-Amz-Security-Token and signs that header. It is a secret,
	// like Password (D94). It is empty for the credentials of a user.
	Token string
}

// ParseDSN parses a DSN of the form
// dynamodb://key:secret@host:port?region=us-east-1 (D27, D35 and D169). The
// keys are tls, which is true by default, region, which has no default and
// must be set, and token, the session token of temporary credentials, which
// is empty by default. A DSN with a path is refused, because DynamoDB has no
// database to choose, and so is every other key. The signature needs the
// access key and the secret key, so a DSN with no user or no password is
// refused.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyRegion, keyToken)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Host: u.Hostname(), Region: q.String(keyRegion, ""), Token: q.String(keyToken, "")}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: DynamoDB has no database to choose, so the URL has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if cfg.TLS, err = q.Bool(keyTLS, true); err != nil {
		return nil, err
	}
	if !validRegion(cfg.Region) {
		return nil, fmt.Errorf("parsing key %q: the region %q is empty or has a character that a region does not have: %w", keyRegion, cfg.Region, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.User == "" || cfg.Password == "" {
		return nil, fmt.Errorf("parsing the dsn: the signature needs the access key as the user and the secret key as the password: %w", dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// validRegion reports whether s can name a region: letters, digits and
// hyphens.
func validRegion(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c == '-', '0' <= c && c <= '9', 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		default:
			return false
		}
	}
	return true
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN filled.
func (cfg *Config) FormatDSN() string {
	u := url.URL{Scheme: Name, Host: cfg.hostPort()}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := url.Values{keyRegion: {cfg.Region}}
	if !cfg.TLS {
		q.Set(keyTLS, "false")
	}
	if cfg.Token != "" {
		q.Set(keyToken, cfg.Token)
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

// baseURL returns the URL of the endpoint of cfg. It has no port when the
// port is the port of the scheme, so that the host that the driver signs is
// the host that the service of AWS reads.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + cfg.hostPort()
}
