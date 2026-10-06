// Package druid is a database/sql driver for Apache Druid, which runs SQL
// on its Router or its Broker over HTTP. It registers the one name "druid",
// and takes a DSN of the form druid://user:pass@host:8888?key=value (D164).
//
//	db, err := sql.Open("druid", "druid://admin:pass@localhost:8888")
//
// The driver reads only. The SQL API of Druid refuses INSERT, REPLACE,
// UPDATE and DELETE, and the driver returns that refusal (D163). The server
// binds each argument as a typed parameter (D164). The driver reads each
// answer as arrayLines, one line at a time, with the names and the types of
// the columns in its first three lines. An answer that ends with no empty
// line is cut short, and the driver returns an error for it (D21 and D164).
// Each query has an id of its own, and when the context of a query ends
// while the query runs, the driver cancels it on the server by that id,
// because the server runs a query on when its client leaves (D164). Druid
// has no transactions. docs/DRUID.md holds what the driver knows about the
// server.
package druid

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "druid"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Apache Druid.
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
