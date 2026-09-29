package neo4j

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql/driver"
	"net/http"
	"sync"

	"github.com/xo/dbimp"
)

// Connector opens connections to one server. It owns its transport, and
// every connection shares it. A caller can build one from a Config and open
// it with sql.OpenDB.
type Connector struct {
	cfg       Config
	base      string
	transport *http.Transport
	client    *http.Client

	// mu guards metadata.
	mu sync.Mutex
	// metadata is whether the server takes txMetadata, once the connector
	// read the release of the server for cancel=metadata (D67). It is nil
	// until then.
	metadata *bool
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.Database == "" {
		cfg.Database = defaultDatabase
	}
	if cfg.Cancel == "" {
		cfg.Cancel = CancelTag
	}
	if cfg.Auth == "" {
		cfg.Auth = AuthBasic
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
// server, except a transaction while one is open. Each connection has a
// random id, which names its statements for cancel (D67).
func (c *Connector) Connect(context.Context) (driver.Conn, error) {
	return &conn{c: c, id: rand.Text()}, nil
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
