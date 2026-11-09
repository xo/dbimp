package hive

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// The names that the servers give for GetInfo, in lower case (D235).
const (
	flavorHive   = "hive"
	flavorImpala = "impala"
)

// closeTimeout bounds the calls that end an operation or a session after the
// context of a statement ended, so that a stuck server cannot hold a caller.
const closeTimeout = 10 * time.Second

// Connector opens connections to one server. It owns its transport, and every
// connection shares it. A caller can build one from a Config and open it with
// sql.OpenDB.
type Connector struct {
	cfg       Config
	endpoint  string
	transport *http.Transport
	client    *http.Client
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of cfg,
// and fills each field that is zero with its default.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	if cfg.HTTPPath == "" {
		cfg.HTTPPath = defaultHTTPPath
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		endpoint:  cfg.endpoint(),
		transport: t,
		// The driver follows no redirect, so the credentials go to the host of
		// the DSN only (D235).
		client: dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. It opens a session, and asks the server
// which flavor it is.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	session, err := c.openSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening a session: %w", err)
	}
	cn := &conn{c: c, session: session, ctx: context.WithoutCancel(ctx)}
	// The name and the version tell the flavors apart (D235). A server that
	// refuses GetInfo is still served, with no flavor.
	if name, err := c.getInfo(ctx, session, infoServerName); err == nil {
		cn.flavor = strings.ToLower(name)
	}
	if ver, err := c.getInfo(ctx, session, infoDBMSVer); err == nil {
		cn.version = ver
	}
	if db := c.cfg.Database; db != "" {
		if err := cn.run(ctx, "USE "+quoteIdent(db)); err != nil {
			_ = cn.close(context.WithoutCancel(ctx))
			return nil, fmt.Errorf("using the database %q: %w", db, err)
		}
	}
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
