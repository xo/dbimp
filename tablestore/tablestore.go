// Package tablestore is a database/sql driver for Alibaba Cloud Tablestore,
// which runs SQL over its HTTPS API. It registers the one name "tablestore", and
// takes a DSN of the form
// tablestore://key:secret@instance.region.ots.aliyuncs.com/instance (D222).
//
//	db, err := sql.Open("tablestore", "tablestore://key:secret@myinstance.ap-southeast-5.ots.aliyuncs.com/myinstance")
//
// A request and an answer are protobuf, and the rows of an answer are
// FlatBuffers. The driver writes both encodings with the standard library
// (D13 and D222). It signs each request with the first version of the
// signature, and it reads no credential from the environment or from a file
// (D7).
//
// A statement is one call, SQLQuery. The server binds no parameter, so the
// driver writes each argument into the text of the statement (D34 and D224). The
// SQL works over a mapping table, which names a table of Tablestore. The server
// runs SELECT, INSERT, UPDATE and DELETE, and refuses a second statement in the
// same call. The columns of a result come with their types, and the driver reads
// each value by the type of its column (D223).
//
// Tablestore has no transaction in SQL, so BeginTx fails with
// dbimp.ErrNotSupported (D20). docs/TABLESTORE.md holds what the driver knows
// about the server.
package tablestore

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the scheme
// of its DSN.
const Name = "tablestore"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Tablestore.
type Driver struct{}

// Open satisfies driver.Driver. database/sql opens a connection through
// OpenConnector, and Open returns an error, because opening a connection needs a
// context.
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
