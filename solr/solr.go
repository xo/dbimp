// Package solr is a database/sql driver for Apache Solr, which runs SQL on a
// collection over HTTP, as Parallel SQL. It registers the one name "solr",
// and takes a DSN of the form solr://user:pass@host:8983/collection (D166).
//
//	db, err := sql.Open("solr", "solr://admin:pass@localhost:8983/books")
//
// The driver reads only. The SQL of Solr refuses INSERT, UPDATE and DELETE,
// and the driver returns that refusal (D163). The server binds no argument,
// so the driver writes each one into the statement as a literal (D166). The
// answer names no type, so the driver reads the type of each column from
// metadata.COLUMNS and from the luke handler, once for each table and
// connection, and decodes each value by it (D166). Each request asks for the
// first tuple that names the columns, and the driver reads the tuples one at
// a time, to the tuple EOF. Solr has no transactions, and no way to stop a
// statement of /sql. docs/SOLR.md holds what the driver knows about the
// server.
package solr

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "solr"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Apache Solr.
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
