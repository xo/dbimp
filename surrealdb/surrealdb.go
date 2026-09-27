// Package surrealdb is a database/sql driver for SurrealDB, which runs
// SurrealQL over HTTP. It registers the one name "surrealdb" (D47), and
// takes a DSN of the form
// surrealdb://user:pass@host:8000/namespace/database?key=value (D48).
//
//	db, err := sql.Open("surrealdb", "surrealdb://root:root@localhost:8000/test/test")
//
// The driver sends each statement to POST /rpc in CBOR (D49 and D50). The
// key encoding=json makes it speak JSON, to read the traffic while
// debugging. The key auth names where the user is defined: root, namespace
// or database (D51). An argument is named, and sql.Named("x", v) binds $x.
//
// Each statement of a query is a result set, which Rows.NextResultSet moves
// to (D52). The columns of a result set are the keys of its first object, in
// the order that the server sends them, which is the order of their names. A
// record id is a RecordID, and each other type has the Go type of D53. The
// driver has no transactions, because a transaction of SurrealDB lives in
// one request (D54). docs/SURREALDB.md holds what the driver knows about the
// server.
package surrealdb

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "surrealdb"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for SurrealDB.
type Driver struct{}

// ensure the interfaces.
var (
	_ driver.DriverContext = Driver{}
	_ driver.Connector     = (*Connector)(nil)
)

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
