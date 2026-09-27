// Package neo4j is a database/sql driver for Neo4j, which runs Cypher over
// the Query API of HTTP. It registers the one name "neo4j" (D60), and takes a
// DSN of the form neo4j://user:pass@host:7474/database?key=value (D61).
//
//	db, err := sql.Open("neo4j", "neo4j://neo4j:pass@localhost:7474/neo4j")
//
// The driver speaks HTTP to port 7474, or HTTPS to port 7473 with tls=true.
// A URL that the tools of Neo4j write, such as neo4j://host:7687, names the
// port of Bolt, which this driver does not speak.
//
// The server binds each argument: sql.Named("k", v) fills $k, and a
// positional argument n fills $n, such as $1 (D64). Each value keeps its type
// in typed JSON (D62 and D63). A transaction is an explicit transaction of
// the Query API (D65). The server does not stop a query when the client
// disconnects, so the key cancel says how the driver stops one when its
// context ends (D67). docs/NEO4J.md holds what the driver knows about the
// server.
package neo4j

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "neo4j"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for Neo4j.
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
