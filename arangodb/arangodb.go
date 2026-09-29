// Package arangodb is a database/sql driver for ArangoDB, which runs AQL over
// the cursor API of HTTP. It registers the one name "arangodb", and takes a
// DSN of the form arangodb://user:pass@host:8529/database?key=value (D93).
//
//	db, err := sql.Open("arangodb", "arangodb://root:pass@localhost:8529/mydb")
//
// The server binds each argument: sql.Named("k", v) fills @k, and a
// positional argument n fills @n, such as @1. A named argument that the query
// uses as @@k names a collection. Each query streams, one batch at a time,
// and with cancel=tag the driver stops it on the server when its context
// ends (D90). A stored document or edge is one column, and the keys of any
// other object are the columns (D89). A transaction names every collection
// of the database that is not a system collection (D91). The driver also
// takes CREATE and DROP of collections and indexes, which AQL does not have
// (D92). docs/ARANGODB.md holds what the driver knows about the server.
package arangodb

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "arangodb"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for ArangoDB.
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
