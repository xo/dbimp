// Package maxcompute is a database/sql driver for Alibaba Cloud MaxCompute,
// which runs SQL over its REST API with HTTPS and XML. It registers the one
// name "maxcompute", and takes a DSN of the form
// maxcompute://<access key id>:<access key secret>@service.<region>.maxcompute.aliyun.com/<project>
// (D218).
//
//	db, err := sql.Open("maxcompute", "maxcompute://"+id+":"+secret+"@service.ap-southeast-5.maxcompute.aliyun.com/myproject")
//
// The driver signs each request with signature version 4, and sends it to the
// host of the DSN only. It reads each result as CSV, and it types each column
// with the statement EXPLAIN OUTPUT (D219). A key of the DSN that starts with
// odps. is a setting of the server for every statement (D220). The server has
// no transaction, and it cuts a result at 10,000 rows with no sign.
// docs/MAXCOMPUTE.md holds what the driver knows about the server.
package maxcompute

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "maxcompute"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for MaxCompute.
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
