package athena

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D192).
const (
	keyWorkGroup = "workgroup"
	keyOutput    = "output"
	keyToken     = "token"
	keyCatalog   = "catalog"
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the endpoint, such as athena.us-east-1.amazonaws.com.
	Host string
	// Port is the port of the endpoint. Zero means the port of the scheme, 443
	// for HTTPS and 80 for HTTP.
	Port int
	// TLS is true to speak HTTPS. ParseDSN sets it, and only a test of the
	// driver has a reason to turn it off, because the DSN has no key for it.
	TLS bool
	// Region is the region of AWS that each signature names, such as
	// us-east-1. ParseDSN reads it from the host.
	Region string
	// User is the access key, and Password is the secret key, which the driver
	// signs each request with (D94 and D192). A Config with no User and no
	// Password opens no connection, because the driver reads no credential from
	// the environment (D7).
	User     string
	Password string
	// Token is the session token of temporary credentials. The driver sends it
	// in the header X-Amz-Security-Token and signs that header. It is a secret,
	// like Password (D94). It is empty for the credentials of a user.
	Token string
	// Database is the database of the Glue Data Catalog that a statement
	// uses, which the path of the DSN names. It can be empty.
	Database string
	// WorkGroup is the workgroup that runs a statement. When it is empty, the
	// request names none, and Athena uses the workgroup primary.
	WorkGroup string
	// Output is the S3 location of the results, such as s3://bucket/prefix/.
	// A workgroup that enforces its own location ignores it (recorded).
	Output string
	// Catalog is the data catalog of a statement. When it is empty, the request
	// names none, and Athena uses AwsDataCatalog.
	Catalog string
}

// ParseDSN parses a DSN of the form
// athena://key:secret@athena.us-east-1.amazonaws.com/database?workgroup=name
// (D27, D35 and D192). The host names the endpoint, and the region comes from
// it: the label after athena or athena-fips, so
// athena.us-east-1.amazonaws.com and vpce-1.athena.us-east-1.vpce.amazonaws.com
// are in us-east-1. The path is the database, with no further slash. The keys
// are workgroup, output, token and catalog, and each is empty by default. The
// access key and the secret key are the user and the password of the URL. They
// can be left out, and then the Config has none and the connector refuses to
// connect (D7). Any other key is refused.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyWorkGroup, keyOutput, keyToken, keyCatalog)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:      u.Hostname(),
		TLS:       true,
		Token:     q.String(keyToken, ""),
		WorkGroup: q.String(keyWorkGroup, ""),
		Output:    q.String(keyOutput, ""),
		Catalog:   q.String(keyCatalog, ""),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if cfg.Region = regionOf(cfg.Host); cfg.Region == "" {
		return nil, fmt.Errorf("parsing the dsn: the host %q has no label athena followed by a region: %w", cfg.Host, dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if cfg.Database = strings.TrimPrefix(u.Path, "/"); strings.Contains(cfg.Database, "/") {
		return nil, fmt.Errorf("parsing the dsn: the path %q has more than the name of a database: %w", u.Path, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
		if (cfg.User == "") != (cfg.Password == "") {
			return nil, fmt.Errorf("parsing the dsn: the access key is the user and the secret key is the password, and the URL has only one of them: %w", dbimp.ErrInvalidValue)
		}
	}
	return cfg, nil
}

// regionOf returns the region that a host of Athena names, or "". It looks for
// the label athena or athena-fips and takes the label after it, when that label
// has the characters of a region.
func regionOf(host string) string {
	labels := strings.Split(strings.ToLower(host), ".")
	for i := 0; i+1 < len(labels); i++ {
		if (labels[i] == "athena" || labels[i] == "athena-fips") && validRegion(labels[i+1]) {
			return labels[i+1]
		}
	}
	return ""
}

// regionPattern matches the name of a region of AWS: two letters, words of
// letters, and a number, such as us-east-1, ap-southeast-2 and us-gov-west-1.
var regionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)

// validRegion reports whether s is the name of a region.
func validRegion(s string) bool {
	return regionPattern.MatchString(s)
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN filled. The DSN has no key for the region and for TLS,
// so it holds neither.
func (cfg *Config) FormatDSN() string {
	u := url.URL{Scheme: Name, Host: cfg.hostPort()}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	if cfg.Database != "" {
		u.Path = "/" + cfg.Database
	}
	q := url.Values{}
	for key, v := range map[string]string{keyWorkGroup: cfg.WorkGroup, keyOutput: cfg.Output, keyToken: cfg.Token, keyCatalog: cfg.Catalog} {
		if v != "" {
			q.Set(key, v)
		}
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

// baseURL returns the URL of the endpoint of cfg. It has no port when the port
// is the port of the scheme, so that the host that the driver signs is the host
// that the service of AWS reads.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + cfg.hostPort()
}
