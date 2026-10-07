// Package opensearch is a database/sql driver for OpenSearch, which runs SQL
// through POST /_plugins/_sql over HTTP. It registers the one name
// "opensearch", and takes a DSN of the form
// opensearch://user:pass@host:9200?key=value (D168).
//
//	db, err := sql.Open("opensearch", "opensearch://admin:pass@localhost:9200")
//
// The driver reads only. SQL in OpenSearch takes SELECT, SHOW and DESCRIBE,
// and the server refuses every write, which the driver returns (D163). A
// write goes through the document API of the server, which the driver does
// not speak. The server binds no argument that it keeps apart from the text
// of the statement, so the driver writes each argument into the statement as
// a literal (D34 and D168).
//
// The driver sends a page size with each plain SELECT, reads each page one
// token at a time, follows the cursor to the last page, and closes the
// cursor of a result that the caller leaves before the end (D168). A cursor
// serves once, so a page is never read twice. A failure on a later page
// wraps dbimp.ErrIncomplete (D107). The SQL plugin has no way to stop a
// statement, so a cancelled context closes the request and the cursor only.
// OpenSearch has no transactions. docs/OPENSEARCH.md holds what the driver
// knows about the server, and the caps that the server applies to a result
// with no sign (D163).
package opensearch

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "opensearch"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for OpenSearch.
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
