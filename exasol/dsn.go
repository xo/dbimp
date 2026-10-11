package exasol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D205).
const (
	keyTLS          = "tls"
	keyValidate     = "validateservercertificate"
	keyFingerprint  = "certificatefingerprint"
	keyCompression  = "compression"
	keyAutocommit   = "autocommit"
	keyFetchSize    = "fetchsize"
	keyQueryTimeout = "querytimeout"
	keyClientName   = "clientname"
	keyProtocol     = "protocol"
)

// The defaults and the limits of the keys of the DSN (D205).
const (
	defaultPort       = 8563
	defaultFetchSize  = 1024
	maxFetchSize      = 64 << 10
	maxQueryTimeout   = 999999
	defaultClientName = "dbimp"
	defaultProtocol   = 3
	minProtocol       = 3
	maxProtocol       = 5
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7). The zero value of a field that has a default is the default,
// so a field that turns a feature off is named for the feature that it turns
// off.
type Config struct {
	// Host is the host of the server.
	Host string
	// Port is the port of the server, 8563 by default.
	Port int
	// User and Password are the credentials. The password is the secret of
	// the URL (D94).
	User     string
	Password string
	// Schema is the schema that the session opens, or "" for none. It is the
	// path of the DSN.
	Schema string
	// NoTLS is true to speak ws and not wss, which the key tls=false asks
	// for. The server answers a login on ws with an error, so it is for a
	// server behind a proxy that ends TLS.
	NoTLS bool
	// SkipVerify is true to accept any certificate, which the key
	// validateservercertificate=false asks for.
	SkipVerify bool
	// CertificateFingerprint is the SHA-256 of the certificate of the server,
	// in hex. If it is set, it takes the place of the check of the chain of
	// the certificate.
	CertificateFingerprint string
	// NoCompression is true to send and read plain text, which the key
	// compression=false asks for (D214).
	NoCompression bool
	// NoAutocommit is true to log in with autocommit off, which the key
	// autocommit=false asks for.
	NoAutocommit bool
	// FetchSize is the size of a page of a result in KiB, 1024 by default
	// (D210).
	FetchSize int
	// QueryTimeout is the number of seconds that the server lets a statement
	// run, or 0 for no limit.
	QueryTimeout int
	// ClientName is the name that the login sends, "dbimp" by default.
	ClientName string
	// Protocol is the version of the WebSocket API that the login asks for,
	// 3, 4 or 5. The default is 3.
	Protocol int
}

// ParseDSN parses a DSN of the form
// exasol://user:password@host:port/schema?key=value (D27, D35 and D205). A key
// that is unknown or repeated is an error. The password never shows in an
// error.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyValidate, keyFingerprint, keyCompression, keyAutocommit, keyFetchSize, keyQueryTimeout, keyClientName, keyProtocol)
	if err != nil {
		return nil, err
	}
	cfg := &Config{Host: u.Hostname(), Port: defaultPort}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.Port(); p != "" {
		if cfg.Port, err = strconv.Atoi(p); err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, fmt.Errorf("parsing the dsn: the port %q: %w", p, dbimp.ErrInvalidValue)
		}
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("parsing the dsn: no user: %w", dbimp.ErrInvalidValue)
	}
	cfg.User = u.User.Username()
	cfg.Password, _ = u.User.Password()
	schema := strings.TrimPrefix(u.Path, "/")
	if strings.Contains(schema, "/") {
		return nil, fmt.Errorf("parsing the dsn: the path %q holds more than one schema: %w", u.Path, dbimp.ErrInvalidValue)
	}
	cfg.Schema = schema
	if err := cfg.readQuery(q); err != nil {
		return nil, err
	}
	return cfg, nil
}

// normalizeFingerprint returns the SHA-256 fingerprint s in lower case hex,
// without the colons and the spaces that tools write between the bytes.
func normalizeFingerprint(s string) (string, error) {
	s = strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(s))
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != sha256.Size {
		return "", fmt.Errorf("parsing key %q: the value is not a SHA-256 in hex: %w", keyFingerprint, dbimp.ErrInvalidValue)
	}
	return s, nil
}

// FormatDSN returns the DSN of cfg. ParseDSN reads it back as cfg, for a
// Config that ParseDSN filled.
func (cfg *Config) FormatDSN() string {
	c := cfg.withDefaults()
	u := url.URL{
		Scheme: Name,
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		User:   url.UserPassword(c.User, c.Password),
	}
	if c.Schema != "" {
		u.Path = "/" + c.Schema
	}
	q := url.Values{}
	for key, off := range map[string]bool{keyTLS: c.NoTLS, keyValidate: c.SkipVerify, keyCompression: c.NoCompression, keyAutocommit: c.NoAutocommit} {
		if off {
			q.Set(key, "false")
		}
	}
	if c.CertificateFingerprint != "" {
		q.Set(keyFingerprint, c.CertificateFingerprint)
	}
	if c.FetchSize != defaultFetchSize {
		q.Set(keyFetchSize, strconv.Itoa(c.FetchSize))
	}
	if c.QueryTimeout != 0 {
		q.Set(keyQueryTimeout, strconv.Itoa(c.QueryTimeout))
	}
	if c.ClientName != defaultClientName {
		q.Set(keyClientName, c.ClientName)
	}
	if c.Protocol != defaultProtocol {
		q.Set(keyProtocol, strconv.Itoa(c.Protocol))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// readQuery reads the keys of the query into cfg.
func (cfg *Config) readQuery(q dbimp.Query) error {
	var err error
	flags := []struct {
		key string
		off *bool
	}{
		{keyTLS, &cfg.NoTLS},
		{keyValidate, &cfg.SkipVerify},
		{keyCompression, &cfg.NoCompression},
		{keyAutocommit, &cfg.NoAutocommit},
	}
	for _, f := range flags {
		on, err := q.Bool(f.key, true)
		if err != nil {
			return err
		}
		*f.off = !on
	}
	if cfg.FetchSize, err = q.Int(keyFetchSize, defaultFetchSize); err != nil {
		return err
	}
	if cfg.FetchSize < 1 || cfg.FetchSize > maxFetchSize {
		return fmt.Errorf("parsing key %q: %d is not from 1 to %d: %w", keyFetchSize, cfg.FetchSize, maxFetchSize, dbimp.ErrInvalidValue)
	}
	if cfg.QueryTimeout, err = q.Int(keyQueryTimeout, 0); err != nil {
		return err
	}
	if cfg.QueryTimeout < 0 || cfg.QueryTimeout > maxQueryTimeout {
		return fmt.Errorf("parsing key %q: %d is not from 0 to %d: %w", keyQueryTimeout, cfg.QueryTimeout, maxQueryTimeout, dbimp.ErrInvalidValue)
	}
	cfg.ClientName = q.String(keyClientName, defaultClientName)
	if q.Has(keyClientName) && cfg.ClientName == "" {
		return fmt.Errorf("parsing key %q: an empty name: %w", keyClientName, dbimp.ErrInvalidValue)
	}
	if cfg.Protocol, err = q.Int(keyProtocol, defaultProtocol); err != nil {
		return err
	}
	if cfg.Protocol < minProtocol || cfg.Protocol > maxProtocol {
		return fmt.Errorf("parsing key %q: %d is not from %d to %d: %w", keyProtocol, cfg.Protocol, minProtocol, maxProtocol, dbimp.ErrInvalidValue)
	}
	if q.Has(keyFingerprint) {
		fp, err := normalizeFingerprint(q.String(keyFingerprint, ""))
		if err != nil {
			return err
		}
		cfg.CertificateFingerprint = fp
	}
	return nil
}

// withDefaults returns a copy of cfg with each zero field that has a default set to
// it.
func (cfg *Config) withDefaults() Config {
	c := *cfg
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if c.FetchSize == 0 {
		c.FetchSize = defaultFetchSize
	}
	if c.ClientName == "" {
		c.ClientName = defaultClientName
	}
	if c.Protocol == 0 {
		c.Protocol = defaultProtocol
	}
	return c
}
