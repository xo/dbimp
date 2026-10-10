// Package cosmos is a database/sql driver for Azure Cosmos DB with the API for
// NoSQL. It registers the one name "cosmos", and takes a DSN of the form
// cosmos://x:key@host:port/database/container (D190).
//
//	db, err := sql.Open("cosmos", "cosmos://x:"+key+"@account.documents.azure.com/mydb/mycontainer")
//
// A statement is the SQL of Cosmos DB, which the driver sends as a query to
// the documents of one container. The master key of the account is the
// password of the DSN, and signs each request. The driver reads only: the
// SQL of Cosmos DB has no statement that writes, so Exec fails with
// dbimp.ErrNotSupported (D190).
//
// The columns of a result are the keys of its first row, in the order that
// they arrive, and a later row that lacks a key has nil for it. A later row
// with a key that the first row lacks is an error (D18 and D190). A result
// whose rows are not objects, such as the rows of SELECT VALUE, has one
// column, named $1. The driver follows the header X-Ms-Continuation to the
// end of a result, one page at a time, and reads each page one document at a
// time (D25).
//
// The driver sends a query as it is. It does not plan a query across
// partitions or merge the answers, so the hosted service answers an
// aggregate, TOP, ORDER BY, OFFSET LIMIT, DISTINCT or GROUP BY across
// partitions with an error. The caller names the partition key, with
// WithPartitionKey or the DSN key partitionkey (D190). docs/COSMOS.md holds
// what the driver knows about the server.
package cosmos

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "cosmos"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Azure Cosmos DB.
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
