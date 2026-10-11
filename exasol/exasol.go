// Package exasol is a database/sql driver for Exasol, which takes its
// requests as JSON messages of a WebSocket. It registers the one name
// "exasol", and takes a DSN of the form
// exasol://user:password@host:port/schema?key=value (D205).
//
//	db, err := sql.Open("exasol", "exasol://sys:exasol@localhost:8563?validateservercertificate=false")
//
// The connection uses TLS, and it checks the certificate of the server unless
// validateservercertificate is false (D206). The messages are compressed with
// zlib unless compression is false (D214). A query reads its rows with the
// command fetch, by the handle of the result set, about one MiB at a time
// (D210). The server binds the arguments of a statement, which the driver
// prepares for it (D208). A transaction turns autocommit off while it lasts
// (D209). When a context ends, the driver sends abortQuery, reads the answer of
// the aborted statement, and keeps the connection (D211). docs/EXASOL.md holds
// what the driver knows about the server.
package exasol

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "exasol"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Exasol.
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
