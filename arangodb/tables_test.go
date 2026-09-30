package arangodb //nolint:testpackage // The tables read the wire types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/ARANGODB.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		{Wire: "null", Go: "nil", ScanType: "interface {}", Nullable: true},
		{Wire: "boolean", Go: "bool", ScanType: "interface {}", Nullable: true},
		{Wire: "integer", Go: "int64, for a number with no fraction that fits", ScanType: "interface {}", Nullable: true},
		{Wire: "double", Go: "float64, and a number above the range of int64", ScanType: "interface {}", Nullable: true},
		{Wire: "string", Go: "string", ScanType: "interface {}", Nullable: true},
		{Wire: "array", Go: "[]any", ScanType: "interface {}", Nullable: true},
		{Wire: "object", Go: "map[string]any, and a document in one column (D89)", ScanType: "interface {}", Nullable: true},
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping reads the version of the database, which checks the credentials and the database.",
		"driver.SessionResetter":                "A transaction ends before database/sql hands the connection on, so nothing needs a reset.",
		"driver.Validator":                      "A connection holds no state on the server outside a transaction, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps a uint64, a decimal, a slice and a map, which AQL takes as they are.",
		"driver.QueryerContext":                 "The server binds each argument itself, through bindVars.",
		"driver.ExecerContext":                  "Exec reads the result to its end, and RowsAffected is writesExecuted.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, bound each time.",
		"driver.ConnBeginTx":                    "A stream transaction that names every collection of the database (D91).",
		"driver.RowsColumnScanner":              "A value is decoded from JSON when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A query holds one statement, so a cursor has one result.",
		"driver.RowsColumnTypeScanType":         "The cursor names no types, and each value names its own.",
		"driver.RowsColumnTypeDatabaseTypeName": "The cursor names no types.",
		"driver.RowsColumnTypeLength":           "The cursor names no types, so no column has a length.",
		"driver.RowsColumnTypeNullable":         "The cursor names no types, and any value can be null.",
		"driver.RowsColumnTypePrecisionScale":   "The cursor names no types, so no column has a precision or a scale.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &tx{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"null":    "null",
	"boolean": "boolean",
	"integer": "number",
	"double":  "number",
	"string":  "string",
	"array":   "array",
	"object":  "map",
}
