// Package clickhouse is a database/sql driver for ClickHouse, which takes a
// statement over its HTTP interface. It registers the one name "clickhouse",
// and takes a DSN of the form clickhouse://user:password@host:8123/database
// (D177).
//
//	db, err := sql.Open("clickhouse", "clickhouse://default:secret@localhost:8123/default")
//
// The driver sends a statement with POST /, and reads the answer in the format
// JSONCompactEachRowWithNamesAndTypes, one row at a time, with the settings that
// make each value exact (D176). It binds each argument as a typed parameter of
// the server, such as {p1:Int64} with param_p1 in the query string (D176). An
// error after some rows comes from the marker that the server writes in the
// stream, and wraps dbimp.ErrIncomplete (D107 and D176). The server runs a
// query on when its client leaves, so the driver names each query and sends
// KILL QUERY when the context ends and when the caller closes the rows early
// (D176). The server has no transaction, so BeginTx fails with
// dbimp.ErrNotSupported (D177). docs/CLICKHOUSE.md holds what the driver knows
// about the server.
package clickhouse

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "clickhouse"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for ClickHouse.
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
