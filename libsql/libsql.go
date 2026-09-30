// Package libsql is a database/sql driver for libSQL, the fork of SQLite by
// Turso, which its server sqld and Turso Cloud serve over the Hrana protocol
// on HTTP. It registers the one name "libsql", and takes a DSN of the form
// libsql://user:token@host:port?key=value (D148). dburl names turso as an
// alias.
//
//	db, err := sql.Open("libsql", "libsql://:token@mydb-myorg.turso.io")
//
// TLS is on unless tls=false, and the token goes as Bearer (D148). A query
// reads its rows from /v3/cursor one token at a time, and Exec runs on
// /v3/pipeline (D149). A transaction lives on a stream of Hrana, which its
// baton holds across requests (D150). The server binds each argument as a
// typed value (D152). A value has the Go type of the affinity of its declared
// type, and a value of another storage class keeps its own Go type (D140 and
// D147). docs/LIBSQL.md holds what the driver knows about the server.
package libsql

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "libsql"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for libSQL.
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
