// Package elasticsearch is a database/sql driver for Elasticsearch, which
// runs SQL through POST /_sql over HTTP. It registers the one name
// "elasticsearch", and takes a DSN of the form
// elasticsearch://user:pass@host:9200?key=value (D167).
//
//	db, err := sql.Open("elasticsearch", "elasticsearch://elastic:pass@localhost:9200")
//
// The driver reads only. SQL in Elasticsearch takes SELECT, SHOW, DESCRIBE
// and SYS, and the server refuses every write with a parse error, which the
// driver returns (D163). A write goes through the document API of the
// server, which the driver does not speak. The server binds each argument
// from the array params. The driver reads each page one token at a time,
// follows the cursor to the last page, and closes the cursor of a result
// that the caller leaves before the end (D167). A failure on a later page
// wraps dbimp.ErrIncomplete (D107). The server cancels the statement when
// the client leaves, so a cancelled context only closes the request
// (measured). Elasticsearch has no transactions. docs/ELASTICSEARCH.md holds
// what the driver knows about the server.
package elasticsearch

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "elasticsearch"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Elasticsearch.
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
