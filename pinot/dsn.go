package pinot

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"

	"github.com/xo/dbimp"
)

// The keys of the query of a DSN (D129).
const (
	keyTLS    = "tls"
	keyAuth   = "auth"
	keyCancel = "cancel"
	keyEngine = "engine"
)

// defaultPort is the port of a Broker, for a DSN with no port, with HTTP and
// with HTTPS (D129).
const defaultPort = 8099

// The values of the key cancel, which say how the driver stops a query on the
// server when its context ends (D133).
const (
	// CancelKill sends DELETE /query/<id>?client=true for the id that the
	// driver gave the query. It is the default.
	CancelKill = "kill"
	// CancelNone sends nothing, and the query runs on to its end or its
	// timeout.
	CancelNone = "none"
)

// cancels are the values of the key cancel.
var cancels = []string{CancelKill, CancelNone}

// The values of the key engine, which say which engine of the Broker runs a
// query (D131).
const (
	// EngineMulti runs each query on the multi-stage engine, which returns
	// every row of a query that has no LIMIT. It is the default.
	EngineMulti = "multi"
	// EngineSingle runs each query on the single-stage engine, which returns
	// 10 rows of a query that has no LIMIT, and says nothing of the rows
	// that it left out.
	EngineSingle = "single"
)

// engines are the values of the key engine.
var engines = []string{EngineMulti, EngineSingle}

// The values of the key auth, which say how the driver sends the password
// (D94).
const (
	// AuthBasic sends the user and the password with basic authentication.
	// It is the default.
	AuthBasic = dbimp.AuthBasic
	// AuthBearer sends the password as a Bearer token.
	AuthBearer = dbimp.AuthBearer
)

// Config is the configuration of a connector, which the DSN holds. The caller
// owns it (D7).
type Config struct {
	// Host is the host of the Broker.
	Host string
	// Port is the port of the Broker, 8099 by default.
	Port int
	// TLS is true to speak HTTPS.
	TLS bool
	// User and Password are the credentials. The password can be a token
	// (D94).
	User     string
	Password string
	// Auth is AuthBasic or AuthBearer (D94).
	Auth string
	// Cancel is CancelKill or CancelNone (D133).
	Cancel string
	// Engine is EngineMulti or EngineSingle (D131).
	Engine string
}

// ParseDSN parses a DSN of the form pinot://user:pass@host:port?key=value
// (D27, D35 and D129). A DSN with a path is refused, because Pinot has no
// databases.
func ParseDSN(dsn string) (*Config, error) {
	u, err := dbimp.ParseURL(Name, dsn)
	if err != nil {
		return nil, err
	}
	q, err := dbimp.NewQuery(u, keyTLS, keyAuth, keyCancel, keyEngine)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Host:   u.Hostname(),
		Port:   defaultPort,
		Cancel: q.String(keyCancel, CancelKill),
		Engine: q.String(keyEngine, EngineMulti),
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("parsing the dsn: no host: %w", dbimp.ErrInvalidValue)
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return nil, fmt.Errorf("parsing the dsn: the path %q: Pinot has no databases, so the URL has no path: %w", p, dbimp.ErrInvalidValue)
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if cfg.TLS, err = q.Bool(keyTLS, false); err != nil {
		return nil, err
	}
	if cfg.Auth, err = q.Auth(keyAuth); err != nil {
		return nil, err
	}
	if !slices.Contains(cancels, cfg.Cancel) {
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyCancel, cfg.Cancel, dbimp.ErrInvalidValue)
	}
	if !slices.Contains(engines, cfg.Engine) {
		return nil, fmt.Errorf("parsing key %q: %q: %w", keyEngine, cfg.Engine, dbimp.ErrInvalidValue)
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
	if cfg.Auth != "" && cfg.Auth != AuthBasic {
		q.Set(keyAuth, cfg.Auth)
	}
	if cfg.Cancel != "" && cfg.Cancel != CancelKill {
		q.Set(keyCancel, cfg.Cancel)
	}
	if cfg.Engine != "" && cfg.Engine != EngineMulti {
		q.Set(keyEngine, cfg.Engine)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// baseURL returns the URL of the Broker of cfg.
func (cfg *Config) baseURL() string {
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
}
