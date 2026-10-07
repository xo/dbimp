// Package trino is a database/sql driver for Trino and Presto, which run SQL
// over data that other systems hold, and take a statement over HTTP. It
// registers the one name "trino", and takes a DSN of the form
// trino://user@host:8080/catalog/schema?key=value (D175).
//
//	db, err := sql.Open("trino", "trino://user@localhost:8080/memory/default")
//
// One driver serves both products. It asks GET /v1/info which one answered,
// and the key flavor of the DSN skips that request (D175). The driver sends a
// statement with POST /v1/statement, and polls the nextUri of each page until
// a page has none. It reads each page one token at a time. An error arrives
// in the body of a page with HTTP 200, and after some rows it wraps
// dbimp.ErrIncomplete (D107 and D175). The server runs a query on when its
// client leaves, so the driver sends DELETE to the nextUri when the caller
// closes the rows early and when the context ends (D175).
//
// The driver sends each argument as a literal of EXECUTE name USING, on a
// statement that it names in the prepared-statement header (D175). It starts a
// transaction with START TRANSACTION and the transaction headers. The state
// of a session, such as USE and SET SESSION, stays on the connection, as the
// server asks in its response headers. docs/TRINO.md holds what the driver
// knows about the servers.
package trino

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "trino"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Trino and Presto.
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
