// Package databend is a database/sql driver for Databend, which runs SQL over
// the query API of HTTP. It registers the one name "databend", and takes a
// DSN of the form databend://user:pass@host:8000/database?key=value (D117).
//
//	db, err := sql.Open("databend", "databend://root:pass@localhost:8000/default")
//
// The server binds each argument: a positional argument fills each ? in
// order, and sql.Named("k", v) fills :k (D120). Each result arrives one page
// at a time, and when the context ends, or the rows close before their end,
// the driver kills the query on the server (D123). Every value arrives as
// text, and the driver decodes it by the type of its column, an array, a map
// and a tuple too (D118 and D119). A transaction and the settings of SET and
// USE are carried in the session of the connection (D121 and D122).
// docs/DATABEND.md holds what the driver knows about the server.
package databend

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "databend"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Databend.
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
