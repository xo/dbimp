// Package pinot is a database/sql driver for Apache Pinot, which runs SQL on
// a Broker over HTTP. It registers the one name "pinot", and takes a DSN of
// the form pinot://user:pass@host:8099?key=value (D129).
//
//	db, err := sql.Open("pinot", "pinot://admin:pass@localhost:8099")
//
// The driver runs queries and takes no write, because the Broker refuses
// every write of rows (D128). Each query runs on the multi-stage engine,
// unless the key engine=single or WithEngine chooses the single-stage
// engine, which cuts a result that has no LIMIT at 10 rows (D131). The server
// binds no argument, so the driver writes each ? as a literal of Pinot
// (D132). Every query sends enableNullHandling=true, so a NULL reaches the
// caller as nil (D130). The driver reads the answer one token at a time, and
// when the context ends before the answer arrives, it cancels the query by
// the id that it gave the query (D133). docs/PINOT.md holds what the driver
// knows about the server.
package pinot

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "pinot"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Apache Pinot.
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
