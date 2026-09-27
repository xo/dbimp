// Package couchbase is a database/sql driver for the query service of
// Couchbase Server, which runs SQL++ (N1QL) over HTTP. It registers the one
// name "couchbase" (D30), and takes a DSN of the form
// couchbase://user:pass@host:8093/?key=value (D38).
//
//	db, err := sql.Open("couchbase", "couchbase://user:pass@localhost:8093/")
//
// The driver sends each argument to the server, which binds it: an argument
// without a name fills $1 or ?, and sql.Named("x", v) fills $x (D40). A
// transaction is a transaction of the query service (D41), and the key
// durability_level sets its durability (D43). A string that scans into a
// []byte is decoded as base64 (D44). docs/COUCHBASE.md holds what the driver
// knows about the server.
package couchbase

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "couchbase"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Couchbase.
type Driver struct{}

// Open satisfies driver.Driver. database/sql opens a connection through
// OpenConnector, and Open returns an error, because opening a connection
// needs a context.
func (Driver) Open(string) (driver.Conn, error) {
	return nil, fmt.Errorf("opening a connection without a context: use sql.Open: %w", dbimp.ErrNotSupported)
}

// OpenConnector satisfies driver.DriverContext. It parses the DSN.
func (Driver) OpenConnector(dsn string) (driver.Connector, error) {
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	return NewConnector(*cfg), nil
}

// ensure the interfaces.
var (
	_ driver.DriverContext = Driver{}
	_ driver.Connector     = (*Connector)(nil)
)

// connect is the Connect of a Connector, split out for the tests.
func (c *Connector) connect(context.Context) *conn {
	return &conn{c: c}
}
