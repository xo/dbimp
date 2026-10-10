package spanner

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D191).
const (
	keyCredentialFile = "credential_file" //nolint:gosec // G101: the name of a key of the DSN, which holds a path and no secret.
	keyTLS            = "tls"
)

// The defaults of the DSN. The hosted service is at spanner.googleapis.com
// on HTTPS. The Cloud Spanner emulator serves REST on port 9020 with no TLS
// and no credential (docs/SPANNER.md).
const (
	defaultHost      = "spanner.googleapis.com"
	defaultPort      = 443
	defaultPlainPort = 9020
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the server. It is spanner.googleapis.com when the
	// DSN names none.
	Host string
	// Port is the port of the server: 443 with TLS, and 9020 without it, as
	// the emulator serves it.
	Port int
	// TLS is true for HTTPS. The key tls sets it, and a host that is
	// localhost or a loopback address defaults to false, so that the DSN of
	// the emulator needs no key. Without TLS the driver sends a token only
	// when the DSN names a credential file.
	TLS bool
	// Project, Instance and Database are the three parts of the path of the DSN
	// (D191). They name the database
	// projects/{Project}/instances/{Instance}/databases/{Database}.
	Project  string
	Instance string
	Database string
	// CredentialFile is the path to the key file of a service account, in the
	// JSON form that Google Cloud writes (D191 item 5). The driver signs a
	// token with the private key in it. The text of the key never sits in a
	// DSN (D94). A DSN with TLS must name a file, unless Token is set.
	CredentialFile string
	// Token returns an access token, for a caller that gets its tokens from
	// elsewhere. It replaces CredentialFile, and the driver calls it for each
	// request, so it must cache its token. It is not part of a DSN (D191 item
	// 5).
	Token func(ctx context.Context) (string, error)
}

// ParseDSN parses a DSN of the form
// spanner://host:port/project/instance/database?credential_file=/path/key.json
// (D27, D35 and D191). The path holds the project, the instance and the
// database, and all three are required. The host and the port are optional.
// The keys are credential_file, the path to the key file of a service
// account, and tls, a boolean. The driver refuses any other key. The password
// of the URL is not used, because the key file is too large for it, so the
// DSN refuses user information.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyCredentialFile, keyTLS)
	if err != nil {
		return nil, err
	}
	if u.User != nil {
		return nil, fmt.Errorf("parsing the dsn: user information: the credential is a key file, named by the key %s: %w", keyCredentialFile, dbimp.ErrInvalidValue)
	}
	cfg := &Config{Host: u.Hostname(), CredentialFile: q.String(keyCredentialFile, "")}
	if cfg.Host == "" {
		cfg.Host = defaultHost
	}
	cfg.TLS = !isLocal(cfg.Host)
	if q.Has(keyTLS) {
		if cfg.TLS, err = q.Bool(keyTLS, true); err != nil {
			return nil, err
		}
	}
	cfg.Port = cfg.defaultPort()
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if cfg.Project, cfg.Instance, cfg.Database, err = parsePath(u); err != nil {
		return nil, err
	}
	if q.Has(keyCredentialFile) && cfg.CredentialFile == "" {
		return nil, fmt.Errorf("parsing key %q: the value is empty: %w", keyCredentialFile, dbimp.ErrInvalidValue)
	}
	if cfg.TLS && cfg.CredentialFile == "" {
		return nil, fmt.Errorf("parsing the dsn: a server with TLS needs the key %s: %w", keyCredentialFile, dbimp.ErrInvalidValue)
	}
	return cfg, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN filled. It leaves out the host and the port when they
// are the defaults, and the key tls when it is the default for the host. It
// never writes Token.
func (cfg *Config) FormatDSN() string {
	u := url.URL{Scheme: Name}
	host := cfg.Host
	if host == defaultHost {
		host = ""
	}
	switch {
	case cfg.Port != 0 && cfg.Port != cfg.defaultPort():
		u.Host = net.JoinHostPort(host, strconv.Itoa(cfg.Port))
	case strings.Contains(host, ":"):
		u.Host = "[" + host + "]"
	default:
		u.Host = host
	}
	// RawPath keeps a slash in a name escaped, which Path cannot.
	names := []string{cfg.Project, cfg.Instance, cfg.Database}
	escaped := make([]string, len(names))
	for i, name := range names {
		escaped[i] = url.PathEscape(name)
	}
	u.Path = "/" + strings.Join(names, "/")
	u.RawPath = "/" + strings.Join(escaped, "/")
	q := url.Values{}
	if cfg.CredentialFile != "" {
		q.Set(keyCredentialFile, cfg.CredentialFile)
	}
	if cfg.TLS == isLocal(cfg.Host) {
		q.Set(keyTLS, strconv.FormatBool(cfg.TLS))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// defaultPort returns the port of the server when the DSN names none.
func (cfg *Config) defaultPort() int {
	if cfg.TLS {
		return defaultPort
	}
	return defaultPlainPort
}

// parsePath reads the project, the instance and the database from the path of
// u. A project can hold a colon and a dot, as a project of a domain does, so
// the path is cut at its slashes and each part is unescaped.
func parsePath(u *url.URL) (string, string, string, error) {
	bad := fmt.Errorf("parsing the dsn: the path %q is not /project/instance/database: %w", u.EscapedPath(), dbimp.ErrInvalidValue)
	p := strings.TrimPrefix(u.EscapedPath(), "/")
	parts := strings.Split(p, "/")
	if len(parts) != 3 {
		return "", "", "", bad
	}
	names := make([]string, 3)
	for i, part := range parts {
		name, err := url.PathUnescape(part)
		if err != nil || name == "" || strings.ContainsAny(name, "/") {
			return "", "", "", bad
		}
		names[i] = name
	}
	return names[0], names[1], names[2], nil
}

// isLocal reports whether host is localhost or a loopback address.
func isLocal(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

// databaseName returns the full name of a database of the project and the
// instance of cfg.
func (cfg *Config) databaseName(database string) string {
	return "projects/" + cfg.Project + "/instances/" + cfg.Instance + "/databases/" + database
}

// baseURL returns the URL of the server of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
