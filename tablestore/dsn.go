package tablestore

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D222).
const keyTLS = "tls"

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the endpoint, such as
	// instance.ap-southeast-5.ots.aliyuncs.com.
	Host string
	// Port is the port of the endpoint. Zero means the port of the scheme, 443
	// for HTTPS and 80 for HTTP.
	Port int
	// TLS is true to speak HTTPS. The DSN turns it on by default, and only a
	// test with a fake server turns it off.
	TLS bool
	// Instance is the instance of Tablestore that a statement runs in, which the
	// path of the DSN names. It is the first label of the host.
	Instance string
	// User is the access key id, and Password is the access key secret, which
	// the driver signs each request with (D94 and D222). A Config with no User
	// and no Password opens no connection, because the driver reads no
	// credential from the environment (D7).
	User     string
	Password string
}

// ParseDSN parses a DSN of the form
// tablestore://key:secret@instance.region.ots.aliyuncs.com/instance (D27, D35
// and D222). The path is the instance, with no further slash, and the DSN
// needs it. The only key is tls, which is true by default. The access key id
// and the secret are the user and the password of the URL. They can be left out,
// and then the Config has none and the connector refuses to connect (D7). Any
// other key, and a key that appears twice, is refused.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS)
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
	cfg.Instance = strings.TrimPrefix(u.Path, "/")
	if cfg.Instance == "" || strings.Contains(cfg.Instance, "/") {
		return nil, fmt.Errorf("parsing the dsn: the path %q is not the name of an instance: %w", u.Path, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
		if (cfg.User == "") != (cfg.Password == "") {
			return nil, fmt.Errorf("parsing the dsn: the access key id is the user and the secret is the password, and the URL has only one of them: %w", dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg.
func (cfg *Config) FormatDSN() string {
	u := url.URL{Scheme: Name, Host: cfg.hostPort(), Path: "/" + cfg.Instance}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	if !cfg.TLS {
		u.RawQuery = url.Values{keyTLS: {"false"}}.Encode()
	}
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
