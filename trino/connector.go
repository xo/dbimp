package trino

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
)

// stopTimeout bounds the request that cancels a query after its context
// ended, as D67 bounds it for Neo4j and D133 for Pinot (D175).
const stopTimeout = 5 * time.Second

// maxAnswer is the most of the answer to a cancel, and of the answer to
// GET /v1/info, that the driver reads. Each is small (measured).
const maxAnswer = 1 << 20

// capabilities are the client capabilities that the driver sends to Trino
// (D175). PARAMETRIC_DATETIME makes the server keep up to twelve digits of the
// fraction of a time, where it cuts them to three without it. NUMBER and
// VARIANT make Trino 483 send those two types as they are. A server does not
// name a capability that it does not know, so the other releases take them
// and ignore them (measured). Presto has no capability, and the driver sends
// none to it.
const capabilities = "PARAMETRIC_DATETIME,NUMBER,VARIANT"

// Connector opens connections to one coordinator. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open
// it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
	// caps replaces the client capabilities, for the tests of the recorded
	// exchanges, which each name the capabilities of their request.
	caps *string

	mu     sync.Mutex
	flavor string
	// release is nodeVersion.version of GET /v1/info, as the server wrote it,
	// and "" until a request read it (D181).
	release string
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg, and fills each field that is zero with its default. Session is
// copied.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
		if cfg.TLS {
			cfg.Port = defaultTLSPort
		}
	}
	if cfg.Source == "" {
		cfg.Source = defaultSource
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		// The driver follows no redirect, so the credentials and the user go
		// to the host of the DSN only (D175).
		client: dbimp.NewClient(t, false),
		flavor: cfg.Flavor,
	}
}

// Connect satisfies driver.Connector. It asks the server which product it is,
// once, unless the DSN says. A connection holds its state on the client.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	flavor, err := c.detect(ctx)
	if err != nil {
		return nil, err
	}
	cn := &conn{c: c, flavor: flavor, prefix: prefixOf(flavor)}
	cn.reset()
	return cn, nil
}

// Driver satisfies driver.Connector.
func (c *Connector) Driver() driver.Driver {
	return Driver{}
}

// Close closes the idle connections of the transport.
func (c *Connector) Close() error {
	c.transport.CloseIdleConnections()
	return nil
}

// Flavor returns FlavorTrino or FlavorPresto, as the DSN names it or as the
// server answered, and "" while neither is known.
func (c *Connector) Flavor() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flavor
}

// detect returns the flavor of the server. The key flavor of the DSN names
// it, and then no request goes out. Otherwise it asks GET /v1/info once, and
// keeps the answer. The version of Trino is a number, such as 476, and the
// version of Presto is 0.299, with a dash and a commit after it (measured).
func (c *Connector) detect(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.flavor != "" {
		return c.flavor, nil
	}
	if err := c.info(ctx); err != nil {
		return "", err
	}
	return c.flavor, nil
}

// version returns the release of the server for SELECT version() on Presto
// (D181). It uses the answer that detect kept, and otherwise it asks GET
// /v1/info and keeps the answer. The endpoint needs no privilege (measured).
func (c *Connector) version(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.release == "" {
		if err := c.info(ctx); err != nil {
			return "", err
		}
	}
	return c.release, nil
}

// info sends GET /v1/info, and keeps the release and the flavor that its
// answer names. The caller holds mu. If the DSN named the flavor, the flavor
// stays as it is.
func (c *Connector) info(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/info", nil)
	if err != nil {
		return fmt.Errorf("making the request for the version: %w", err)
	}
	dbimp.SetAuth(req, dbimp.AuthBasic, "", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return err
	}
	if err := checkStatus(res); err != nil {
		return fmt.Errorf("asking the server for its version: %w", err)
	}
	defer res.Body.Close()
	var info struct {
		NodeVersion struct {
			Version string `json:"version"`
		} `json:"nodeVersion"`
	}
	if err := json.UnmarshalRead(io.LimitReader(res.Body, maxAnswer), &info); err != nil {
		return fmt.Errorf("reading the version of the server: %w", err)
	}
	version := info.NodeVersion.Version
	if version == "" {
		return fmt.Errorf("reading the version of the server: the answer holds none: %w", dbimp.ErrInvalidValue)
	}
	c.release = version
	if c.flavor == "" {
		c.flavor = FlavorTrino
		if strings.HasPrefix(version, "0.") {
			c.flavor = FlavorPresto
		}
	}
	return nil
}

// capabilitiesOf returns the value of the header of the client capabilities,
// or "" for none.
func (c *Connector) capabilitiesOf(flavor string) string {
	switch {
	case c.caps != nil:
		return *c.caps
	case flavor == FlavorPresto:
		return ""
	}
	return capabilities
}

// target returns the URL on the host of the DSN for a nextUri that the server
// wrote. Only its path and its query are used, and never its host, so the
// credentials go to the host of the DSN only, whatever the server names
// (D175). A server behind a proxy names its own address in the nextUri,
// which the client cannot reach (measured).
func (c *Connector) target(flavor, next string) (string, error) {
	u, err := url.Parse(next)
	if err != nil {
		if uerr, ok := errors.AsType[*url.Error](err); ok {
			err = uerr.Err
		}
		return "", fmt.Errorf("reading the nextUri: %w: %w", dbimp.ErrInvalidValue, err)
	}
	if !strings.HasPrefix(u.Path, "/") {
		return "", fmt.Errorf("reading the nextUri: the path %q is not absolute: %w", u.Path, dbimp.ErrInvalidValue)
	}
	// Presto puts the token in the query, and Trino puts it in the path
	// (measured), so a token in the query on a server that the DSN calls
	// Trino means that the key flavor is wrong.
	if flavor == FlavorTrino && u.Query().Has("slug") {
		return "", fmt.Errorf("reading the nextUri: it has a slug in its query, as the nextUri of Presto does, and the flavor is %q: %w", flavor, dbimp.ErrInvalidValue)
	}
	target := c.base + u.EscapedPath()
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return target, nil
}

// prefixOf returns the start of the names of the headers of a flavor, which
// is X-Trino- for Trino and X-Presto- for Presto. Each server ignores the
// headers of the other, and Presto fails a request with X-Trino-User with
// "User must be set" (measured).
func prefixOf(flavor string) string {
	if flavor == FlavorPresto {
		return "X-Presto-"
	}
	return "X-Trino-"
}
