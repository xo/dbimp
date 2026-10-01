// Package avatica is a database/sql driver for Apache Calcite Avatica, which
// carries the calls of JDBC over HTTP to a server that runs them on a
// database behind it. It serves the standalone server of Avatica and the
// Apache Phoenix Query Server, over JSON (D153). It registers the one name
// "avatica", and takes a DSN of the form avatica://user:pass@host:8765
// (D156).
//
//	db, err := sql.Open("avatica", "avatica://SA@localhost:8765")
//
// One connection of database/sql is one connection of Avatica, with
// autoCommit on (D157). A query reads its rows in frames, one token at a
// time, and fetches each next frame (D157). The server binds each argument
// as a typed value (D158). A value has the Go type that fits the name of its
// type of JDBC (D155). Transactions are those of the database behind the
// server, and the server has no way to stop a statement (D159).
// docs/AVATICA.md holds what the driver knows about the server.
package avatica

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "avatica"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Avatica.
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
