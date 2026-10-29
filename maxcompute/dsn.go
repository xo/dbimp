package maxcompute

import (
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D218).
const (
	keyRegion = "region"
	keyTLS    = "tls"
)

// settingPrefix starts each key of the query that is a setting of the server
// (D218 item 5 and D220 item 3).
const settingPrefix = "odps."

// defaultPort is the port of the server, for a DSN with no port.
const defaultPort = 443

// hostPrefix and hostSuffix are the two ends of the host of a service, which
// holds the region between them, such as service.ap-southeast-5.maxcompute.aliyun.com
// (D218 item 2).
const (
	hostPrefix = "service."
	hostSuffix = ".maxcompute.aliyun.com"
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the service, such as
	// service.ap-southeast-5.maxcompute.aliyun.com.
	Host string
	// Port is the port of the server, 443 by default.
	Port int
	// ID and Secret are the access key id and the access key secret. They
	// are the user and the password of the DSN (D94).
	ID     string
	Secret string
	// Project is the project, which the path of the DSN holds.
	Project string
	// Region is the region that the signature names. A DSN with no key
	// region takes it from the host.
	Region string
	// Settings are the settings of the server that every statement sends,
	// such as odps.sql.submit.mode. Each key starts with odps. in the DSN.
	Settings map[string]string
	// Insecure makes the driver use HTTP and not HTTPS. A test of the driver
	// sets it, for a fake server on the local host. The DSN writes it as
	// tls=false.
	Insecure bool
}

// ParseDSN parses a DSN of the form
// maxcompute://<id>:<secret>@service.<region>.maxcompute.aliyun.com/<project>
// (D27, D35 and D218). The user is the access key id, the password is the
// secret, and the path is the project. The key region names the region of a
// host that is not of that form. The key tls is true by default, and tls=false
// makes the driver use HTTP. A key that starts with odps. is a setting of the
// server. Any other key is refused, and so is a repeated key.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	known := []string{keyRegion, keyTLS}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("parsing the query of the dsn: %w", err)
	}
	for key := range values {
		if strings.HasPrefix(key, settingPrefix) {
			known = append(known, key)
		}
	}
	q, err := dbimp.NewQuery(u, known...)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host: u.Hostname(),
		Port: defaultPort,
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if u.User != nil {
		cfg.ID = u.User.Username()
		cfg.Secret, _ = u.User.Password()
	}
	if cfg.ID == "" || cfg.Secret == "" {
		return nil, fmt.Errorf("parsing the dsn: the access key id is the user and the secret is the password, and the dsn needs both: %w", dbimp.ErrInvalidValue)
	}
	if cfg.Project, err = parsePath(u); err != nil {
		return nil, err
	}
	cfg.Region = q.String(keyRegion, "")
	if q.Has(keyRegion) && cfg.Region == "" {
		return nil, fmt.Errorf("parsing key %q: the value is empty: %w", keyRegion, dbimp.ErrInvalidValue)
	}
	if cfg.Region == "" {
		if cfg.Region = regionOf(cfg.Host); cfg.Region == "" {
			return nil, fmt.Errorf("parsing the dsn: the host is not %s<region>%s, so the key %q must name the region: %w", hostPrefix, hostSuffix, keyRegion, dbimp.ErrInvalidValue)
		}
	}
	for _, key := range known[2:] {
		v := q.String(key, "")
		if v == "" {
			return nil, fmt.Errorf("parsing key %q: the value is empty: %w", key, dbimp.ErrInvalidValue)
		}
		if cfg.Settings == nil {
			cfg.Settings = map[string]string{}
		}
		cfg.Settings[key] = v
	}
	secure, err := q.Bool(keyTLS, true)
	if err != nil {
		return nil, err
	}
	cfg.Insecure = !secure
	return cfg, nil
}

// regionOf returns the region of a host of the form
// service.<region>.maxcompute.aliyun.com, and "" for any other host.
func regionOf(host string) string {
	region, ok := strings.CutPrefix(strings.ToLower(host), hostPrefix)
	if !ok {
		return ""
	}
	region, ok = strings.CutSuffix(region, hostSuffix)
	if !ok || region == "" || strings.Contains(region, ".") {
		return ""
	}
	return region
}

// parsePath reads the project from the path of u. It is one name, and it is
// required.
func parsePath(u *url.URL) (string, error) {
	p := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if p == "" {
		return "", fmt.Errorf("parsing the dsn: no project in the path: %w", dbimp.ErrInvalidValue)
	}
	bad := fmt.Errorf("parsing the dsn: the path %q is not the name of a project: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	if strings.Contains(p, "/") {
		return "", bad
	}
	name, err := url.PathUnescape(p)
	if err != nil || name == "" || strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == '/' }) {
		return "", bad
	}
	return name, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN filled.
func (cfg *Config) FormatDSN() string {
	host := cfg.Host
	if cfg.Port != 0 && cfg.Port != defaultPort {
		host = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u := url.URL{Scheme: Name, Host: host, User: url.UserPassword(cfg.ID, cfg.Secret)}
	if cfg.Secret == "" {
		u.User = url.User(cfg.ID)
	}
	// RawPath keeps a slash in a name escaped, which Path cannot.
	u.Path = "/" + cfg.Project
	u.RawPath = "/" + url.PathEscape(cfg.Project)
	q := url.Values{}
	if cfg.Region != regionOf(cfg.Host) {
		q.Set(keyRegion, cfg.Region)
	}
	for _, key := range slices.Sorted(maps.Keys(cfg.Settings)) {
		q.Set(key, cfg.Settings[key])
	}
	if cfg.Insecure {
		q.Set(keyTLS, "false")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the server of cfg.
func (cfg *Config) baseURL() string {
	scheme := "https"
	if cfg.Insecure {
		scheme = "http"
	}
	port := cfg.Port
	if port == 0 {
		port = defaultPort
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(port))
}
