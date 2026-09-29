package neo4j //nolint:testpackage // The tables read the wire types of the driver, which are not exported.

import (
	"reflect"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/NEO4J.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	var types []dbimptest.TypeRow
	for _, w := range wireTypes {
		types = append(types, dbimptest.TypeRow{
			Wire:     w.name,
			Go:       w.goes + ", from the $type " + w.typ,
			ScanType: reflect.TypeFor[any]().String(),
			Nullable: true,
		})
	}
	dbimptest.TypeTable(t, doc, types)
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs RETURN 1, which checks the credentials and the database.",
		"driver.SessionResetter":                "A transaction ends before database/sql hands the connection on, so nothing needs a reset.",
		"driver.Validator":                      "A connection holds no state on the server outside a transaction, so it is always valid.",
		"driver.NamedValueChecker":              "An argument keeps its Go value for typed JSON (D63), and a positional one fills $n (D64).",
		"driver.QueryerContext":                 "The server binds each argument itself, through parameters.",
		"driver.ExecerContext":                  "Exec reads the result to its end, and has no count of rows (D66).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, bound each time.",
		"driver.ConnBeginTx":                    "An explicit transaction of the Query API (D65), which keeps the context of BeginTx (D69).",
		"driver.RowsColumnScanner":              "A value is decoded from typed JSON when it is scanned (D63).",
		"driver.RowsNextResultSet":              "A request holds one statement, so a response has one result.",
		"driver.RowsColumnTypeScanType":         "No type arrives for a column, and each value names its own type.",
		"driver.RowsColumnTypeDatabaseTypeName": "No type arrives for a column, and each value names its own type.",
		"driver.RowsColumnTypeLength":           "No type arrives for a column, so no column has a length.",
		"driver.RowsColumnTypeNullable":         "No type arrives for a column, and any value can be NULL.",
		"driver.RowsColumnTypePrecisionScale":   "No type arrives for a column, so no column has a precision or a scale.",
	}, NewConnector(Config{Host: "localhost", Port: portHTTP}), Driver{}, &conn{}, &rows{}, &tx{}, &stmt{})
}
