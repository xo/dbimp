// Package snowflake is a database/sql driver for Snowflake, which runs SQL
// over its SQL REST API, /api/v2/statements, with HTTPS and JSON. It
// registers the one name "snowflake", and takes a DSN of the form
// snowflake://user:key@<org>-<account>.snowflakecomputing.com/database/schema
// (D183).
//
//	db, err := sql.Open("snowflake", "snowflake://alice:"+key+"@org-acct.snowflakecomputing.com/db/public")
//
// The password of the DSN is the private key of the user, as the base64url
// text of its PKCS8 DER bytes. The driver signs a token with it, with RS256
// and the claims that Snowflake names, and signs a new one before the old
// one ends. The driver sends the token and never the key, and only to the
// host of the DSN.
//
// The driver starts each statement with async=true and polls its handle until
// it ends, so that it can cancel the statement when the context ends or the
// caller closes the rows before the end. The server runs the whole statement
// before it sends the first partition, so an error comes before any row. The
// driver reads the first partition from the answer, and each later one with
// GET, in order and one at a time, one token at a time. The server binds each
// argument as a typed binding. The server has no session across two
// requests, so the driver has no transactions. docs/SNOWFLAKE.md holds what
// the driver knows about the server.
package snowflake

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "snowflake"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Snowflake.
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
