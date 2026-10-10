// Package databricks is a database/sql driver for Databricks, which runs SQL
// over its SQL Statement Execution API, /api/2.0/sql/statements, with HTTPS
// and JSON. It registers the one name "databricks", and takes a DSN of the form
// databricks://token:<personal access token>@<workspace host>/<warehouse id>
// (D193).
//
//	db, err := sql.Open("databricks", "databricks://token:"+pat+"@dbc-1234.cloud.databricks.com/1111222233334444?catalog=main&schema=default")
//
// The password of the DSN is the personal access token of a user or of a
// service principal. The driver sends it as a Bearer token, and only to the
// host of the DSN. It reads the result in the format JSON_ARRAY with the
// disposition INLINE, one token at a time. It sends each statement with a
// wait of 50 seconds, and it polls a statement that is still running. The
// server has no session across two requests, so the driver has no
// transactions, and a SET, a USE, a temporary view or a variable does not
// reach the next statement. docs/DATABRICKS.md holds what the driver knows
// about the server.
package databricks

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "databricks"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Databricks.
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
