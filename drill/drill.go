// Package drill is a database/sql driver for Apache Drill, which runs SQL
// over its REST interface. It registers the one name "drill", and takes a DSN
// of the form drill://user:pass@host:8047?key=value (D165).
//
//	db, err := sql.Open("drill", "drill://admin:pass@localhost:8047")
//
// The driver reads. The server runs CREATE TABLE AS and refuses INSERT,
// UPDATE and DELETE, and the driver returns that refusal (D163). The server
// binds no argument, so the driver writes each argument as a literal (D165).
// The driver reads the answer one row at a time, and reads queryState at its
// end. A query that fails after some rows ends with FAILED and no message,
// and the driver reads the message from the profile of the query (D165). The
// server runs a query on when its client leaves, so the driver cancels it by
// its queryId when the context of the query ends (D165). Drill has no
// transactions. docs/DRILL.md holds what the driver knows about the server.
//
// A column whose type changes between two files gives NULL for each value of
// the second type, with no sign in the answer. The driver cannot see this.
// Write typeof(column) in the statement to find such a value (D165).
package drill

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "drill"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Apache Drill.
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
