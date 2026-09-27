package couchbase

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"net/http"

	"github.com/xo/dbimp"
)

// Connector opens connections to one query service. It owns its transport,
// and every connection shares it. A caller can build one from a Config and
// open it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		client:    dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. A connection holds no state on the
// server, except a transaction while one is open.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	return c.connect(ctx), nil
}

// Driver satisfies driver.Connector.
func (c *Connector) Driver() driver.Driver {
	return Driver{}
}

// Close closes the idle connections of the transport. database/sql calls it
// when the database closes.
func (c *Connector) Close() error {
	c.transport.CloseIdleConnections()
	return nil
}
