// Package rqlite is a database/sql driver for rqlite, which runs SQLite on
// each node of a cluster, and takes SQL over HTTP. It registers the one name
// "rqlite", and takes a DSN of the form rqlite://user:pass@host:4001?key=value
// (D141).
//
//	db, err := sql.Open("rqlite", "rqlite://admin:pass@localhost:4001")
//
// Each call sends one statement in one request (D142). A query goes to
// /db/request, so that a write with RETURNING returns its rows, and Exec goes
// to /db/execute. The server binds each argument (D143). A value has the Go
// type of the affinity of its declared type, and a value that SQLite stored
// with another storage class keeps the Go type of its JSON form (D140).
// rqlite shares one write connection among every client, so the driver has
// no transactions (D144). When the context has a deadline, the server stops
// the statement at that deadline (D145). docs/RQLITE.md holds what the driver
// knows about the server.
package rqlite

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "rqlite"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for rqlite.
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
