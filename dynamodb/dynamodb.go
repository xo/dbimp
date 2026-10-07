// Package dynamodb is a database/sql driver for Amazon DynamoDB. It registers
// the one name "dynamodb", and takes a DSN of the form
// dynamodb://key:secret@host:port?region=us-east-1 (D169).
//
//	db, err := sql.Open("dynamodb", "dynamodb://key:secret@localhost:8000?region=us-east-1&tls=false")
//
// A statement is PartiQL, which the driver sends with ExecuteStatement. The
// server binds each ? as a typed value, and the driver refuses a count of
// arguments that is not the count of the placeholders, because the server
// takes too many with no error. The driver follows NextToken to the end of a
// result, one page at a time, and reads each page one item at a time (D25
// and D169). The columns of a result are the names that the statement
// gives, in its order. SELECT * gives one column, which holds each item as a
// map[string]any (D163). A missing attribute and NULL are one value, nil
// (D169). Each request is signed with AWS Signature Version 4, which the
// driver writes with the standard library.
//
// DynamoDB has no transaction that database/sql can express, so BeginTx
// fails with dbimp.ErrNotSupported (D169). The driver sends no DDL. A table
// is made through the API of DynamoDB, which is no SQL. docs/DYNAMODB.md
// holds what the driver knows about the server.
package dynamodb

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "dynamodb"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Amazon DynamoDB.
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
