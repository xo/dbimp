package influxdb

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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
}

// NewConnector returns a Connector for cfg. The connector keeps a copy of
// cfg, and fills each field that is zero with its default.
func NewConnector(cfg Config) *Connector {
	var tlsCfg *tls.Config
	if cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.SQLMode == "" {
		cfg.SQLMode = SQLModePrefer
	}
	if cfg.Version == 0 {
		cfg.Version = defaultVersion
	}
	if cfg.Describe == "" {
		cfg.Describe = DescribeAlways
	}
	if cfg.Chunked == "" {
		cfg.Chunked = ChunkedPrefer
	}
	if cfg.Auth == "" {
		cfg.Auth = AuthBasic
	}
	if cfg.Port == 0 {
		cfg.Port = cfg.defaultPort()
	}
	t := dbimp.NewTransport(tlsCfg)
	return &Connector{
		cfg:       cfg,
		base:      cfg.baseURL(),
		transport: t,
		client:    dbimp.NewClient(t, false),
	}
}

// Connect satisfies driver.Connector. With sqlmode prefer or require, it
// sends GET /ping to learn the release, and chooses the dialect from it. With
// disable or allow, it sends nothing, and the key version names the release
// (D78).
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	cn := &conn{c: c, major: c.cfg.Version}
	switch c.cfg.SQLMode {
	case SQLModeDisable:
		cn.dialect = InfluxQL
		return cn, nil
	case SQLModeAllow:
		cn.dialect = SQL
		return cn, nil
	}
	v, err := c.ping(ctx)
	if err != nil {
		return nil, err
	}
	if cn.major, err = major(v); err != nil {
		return nil, err
	}
	cn.dialect = InfluxQL
	if cn.major >= 3 {
		cn.dialect = SQL
	}
	if c.cfg.SQLMode == SQLModeRequire && cn.dialect != SQL {
		return nil, fmt.Errorf("connecting with sqlmode=require: InfluxDB %s has no SQL: %w", v, dbimp.ErrNotSupported)
	}
	return cn, nil
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

// ping sends GET /ping, and returns the release that the header
// X-Influxdb-Version names, without the "v" that InfluxDB 2 writes before
// it. InfluxDB 3 needs the credentials, and InfluxDB 1 and 2 answer without
// them (measured).
func (c *Connector) ping(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/ping", nil)
	if err != nil {
		return "", fmt.Errorf("making the ping: %w", err)
	}
	// The release is not known yet. InfluxDB 1 and 2 answer the ping with no
	// credentials, and with any, so the ping sends Bearer (measured).
	c.auth(req, 0)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return "", fmt.Errorf("sending the ping: %w", err)
	}
	if err := checkStatus(res); err != nil {
		return "", fmt.Errorf("sending the ping: %w", err)
	}
	// The body of InfluxDB 3 is short, and the version is in the header.
	_ = res.Body.Close()
	v := strings.TrimPrefix(res.Header.Get("X-Influxdb-Version"), "v")
	if v == "" {
		return "", fmt.Errorf("sending the ping: the answer names no version: %w", dbimp.ErrInvalidValue)
	}
	return v, nil
}

// auth sets the credentials of the connector on req, for the major release
// major, or 0 when it is not known. A token goes as Token to InfluxDB 2,
// which refuses Bearer, and as Bearer to every other release (measured,
// D110).
func (c *Connector) auth(req *http.Request, major int) {
	scheme := "Bearer"
	if major == 2 {
		scheme = "Token"
	}
	dbimp.SetAuth(req, c.cfg.Auth, scheme, c.cfg.User, c.cfg.Password)
}

// major returns the major number of the release v, such as 2 for 2.9.1.
func major(v string) (int, error) {
	head, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(head)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("reading the release %q: %w", v, dbimp.ErrInvalidValue)
	}
	return n, nil
}
