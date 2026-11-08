// Package hive is a database/sql driver for Apache Hive and Apache Impala. Both
// serve the Thrift service TCLIService of HiveServer2, and this driver speaks it
// over HTTP, in the binary protocol of Thrift that the root package writes
// (D232). It registers the one name "hive", with Impala as a flavor of it
// (D233), and takes a DSN of the form
// hive://user:pass@host:10001/database?httpPath=cliservice (D234).
//
//	db, err := sql.Open("hive", "hive://hive:secret@localhost:10001")
//
// A connection is one session of HiveServer2. A statement runs as an operation:
// the driver sends it, waits until it ends, and reads the rows page by page, one
// page of 1000 rows at a time (D235). The server binds no parameter, so the
// driver writes each argument into the text of the statement as a literal. The
// driver has no transactions. docs/HIVE.md holds what the driver knows about the
// servers.
package hive

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the scheme
// of its DSN.
const Name = "hive"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Hive and Impala.
type Driver struct{}

// Open satisfies driver.Driver. database/sql opens a connection through
// OpenConnector, and Open returns an error, because opening a connection needs a
// context.
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
