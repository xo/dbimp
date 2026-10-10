// Package spanner is a database/sql driver for Google Cloud Spanner, which
// runs GoogleSQL over its REST API, https://spanner.googleapis.com/v1, with
// HTTPS and JSON, and never over gRPC. It registers the one name "spanner", and
// takes a DSN of the form
// spanner://host:port/project/instance/database?credential_file=/path/key.json
// (D191).
//
//	db, err := sql.Open("spanner", "spanner:///my-project/my-instance/my-db?credential_file=/path/key.json")
//
// The key credential_file names the key file of a service account. The driver
// signs a JWT with its private key, with RS256 and the scopes spanner.admin and
// spanner.data, exchanges it at the token endpoint of the key file for an
// access token, and renews the token before it ends. The driver sends the token
// to the host of the DSN only. A caller that gets its tokens from elsewhere sets
// Config.Token, and a DSN for the emulator, such as
// spanner://localhost:9020/p/i/d, needs no credential.
//
// The driver makes one multiplexed session for each connector, and makes a new
// one when the server answers NOT_FOUND. It reads every result with
// executeStreamingSql, one token at a time, and joins a value that the server
// splits across messages. A DML statement outside a transaction runs in a
// transaction that the driver begins and commits. BeginTx calls
// beginTransaction. A transaction that the server aborts returns ErrAborted,
// and the caller runs the whole transaction again. A DDL statement goes to
// updateDatabaseDdl, and the driver waits until its operation is done.
// docs/SPANNER.md holds what the driver knows about the server.
package spanner

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "spanner"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Spanner.
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
