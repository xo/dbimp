// Package bigquery is a database/sql driver for Google BigQuery, which runs
// GoogleSQL over its REST API, POST /bigquery/v2/projects/{project}/queries,
// with HTTPS and JSON. It registers the one name "bigquery", and takes a DSN of
// the form bigquery://project/dataset?credential_file=/path/key.json (D189).
//
//	db, err := sql.Open("bigquery", "bigquery://my-project/my_dataset?credential_file=/path/key.json")
//
// The host of the DSN is the project, and the path is the dataset of each
// statement, with an optional location before it. The secret is the path of a
// service account key file, in the key credential_file, and never the text of
// the key. The driver reads the file, signs a token request with RS256, and
// exchanges it at the token endpoint that the file names. It keeps the access
// token and signs a new one before the old one ends. It sends the token only to
// the host of the endpoint. A caller can set a ready access token in the Config
// of a connector.
//
// An emulator has no login. A DSN for the emulator names its address in the key
// endpoint and sets disable_auth=true.
//
// The driver always sends useLegacySql as false, and asks for timestamps as
// ISO8601_STRING. If a query is still running after a short wait, the driver
// polls the job with jobs.getQueryResults, and it cancels the job with
// jobs.cancel when the context ends. It reads each page of a result with the
// page token of the service, one token at a time. The server binds each
// argument with an explicit type. The driver has no transactions in the first
// release, because a transaction needs a session across requests (D189).
// docs/BIGQUERY.md holds what the driver knows about the service.
package bigquery

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/xo/dbimp"
)

// Name is the name that the driver registers with database/sql, and the
// scheme of its DSN.
const Name = "bigquery"

func init() {
	sql.Register(Name, Driver{})
}

// Driver is the database/sql driver for BigQuery.
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
