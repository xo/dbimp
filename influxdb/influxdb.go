// Package influxdb is a database/sql driver for InfluxDB 1, InfluxDB 2, and
// InfluxDB 3 and later (D78). It registers the one name "influxdb", and takes
// a DSN of the form influxdb://user:pass@host:port/database?key=value (D82).
//
//	db, err := sql.Open("influxdb", "influxdb://_admin:apiv3_token@localhost:8181/mydb")
//
// The driver speaks two dialects. SQL, the dialect "influxdb", goes to
// /api/v3/query_sql, which only InfluxDB 3 and later have. InfluxQL, the
// dialect "influxql", goes to /query on every release. The key sqlmode says
// which one a connection speaks, as sslmode does for PostgreSQL, and its
// default, prefer, asks the server for its release (D78). Dialect and Version
// tell a caller what a connection speaks and what it talks to.
//
// The server binds each argument: sql.Named("k", v) fills $k, and a
// positional argument n fills $n, such as $1. SQL reads its columns and their
// types from DESCRIBE (D80). InfluxQL gives each series of a statement a
// result set of its own, with its name and its tags as the first columns
// (D81 and D83). InfluxDB has no transactions. docs/INFLUXDB.md holds what
// the driver knows about the server.
package influxdb

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "influxdb"

// The dialects of a connection (D78).
const (
	// SQL is the dialect of SQL on InfluxDB 3 and later. Its name is the name
	// of the driver, as dburl D29 names it.
	SQL = "influxdb"
	// InfluxQL is the dialect of InfluxQL, on every release.
	InfluxQL = "influxql"
)

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for InfluxDB.
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
