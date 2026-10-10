// Package athena is a database/sql driver for Amazon Athena, which runs SQL
// over its JSON API with HTTPS. It registers the one name "athena", and takes
// a DSN of the form
// athena://key:secret@athena.us-east-1.amazonaws.com/database?workgroup=name
// (D192).
//
//	db, err := sql.Open("athena", "athena://key:secret@athena.us-east-1.amazonaws.com/mydb?workgroup=main")
//
// The region comes from the host of the DSN. The driver signs each request
// with AWS Signature Version 4, which it writes with the standard library, and
// it reads no credential from the environment or from a file (D7).
//
// A statement is three or more calls. The driver sends StartQueryExecution,
// polls GetQueryExecution until the query ends, and reads the rows with
// GetQueryResults, one page at a time, each page one row at a time (D25 and
// D192). The server runs the whole query before the first row, so an error
// comes before any row. When the context ends while the query runs, the driver
// sends StopQueryExecution. The server binds each ? with ExecutionParameters,
// and the driver writes each argument as an SQL literal. Every value arrives as
// text, and the driver decodes it by the type of the column. The text of an
// ARRAY, a MAP and a ROW has no escape, so the driver returns it as a string.
//
// Athena has no transaction, so BeginTx fails with dbimp.ErrNotSupported
// (D20). docs/ATHENA.md holds what the driver knows about the server.
package athena

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	_ "time/tzdata" // D192 item 7: a zone name in a TIMESTAMP WITH TIME ZONE needs the zone database.

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "athena"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Amazon Athena.
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
