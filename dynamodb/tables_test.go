package dynamodb //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/DYNAMODB.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// A column has no type in DynamoDB, so every row has the same scan type
	// and the same database type (D169).
	row := func(name, goType string) dbimptest.TypeRow {
		r := &rows{}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("NULL", "nil"),
		row("BOOL", "bool"),
		row("N", "*apd.Decimal, from the text of the number"),
		row("S", "string"),
		row("B", "[]byte, from base64"),
		row("L", "[]any, of the Go types of its elements"),
		row("M", "map[string]any, of the Go types of its values"),
		row("SS", "[]any, of string"),
		row("NS", "[]any, of *apd.Decimal"),
		row("BS", "[]any, of []byte"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping sends ListTables with a limit of one table, which costs little and checks the signature.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because every request carries its own signature.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a decimal, a Set, a list and a map, which the driver binds with a type of its own (D169).",
		"driver.QueryerContext":                 "The server binds each argument as a typed value (D169).",
		"driver.ExecerContext":                  "Exec reads the result to its end. The server gives no count of the items that a write changed, so RowsAffected fails.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because DynamoDB runs a transaction only as one request of reads or of writes (D169).",
		"driver.RowsColumnScanner":              "A value is decoded when its item is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "A column has no type, so the scan type is any (D169).",
		"driver.RowsColumnTypeDatabaseTypeName": "A column has no type, so the name is empty (D169).",
		"driver.RowsColumnTypeLength":           "A column has no type, so it has no length.",
		"driver.RowsColumnTypeNullable":         "An item can lack any attribute, so every column can be NULL.",
		"driver.RowsColumnTypePrecisionScale":   "A column has no type, so it has no precision and no scale.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"NULL": "null",
	"BOOL": "boolean",
	"N":    "decimal",
	"S":    "string",
	"B":    "binary",
	"L":    "array",
	"M":    "map",
	"SS":   "set",
	"NS":   "set",
	"BS":   "set",
}
