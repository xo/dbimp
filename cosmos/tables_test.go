package cosmos //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/COSMOS.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// A column has no type in Cosmos DB, because each document holds its own
	// values, so every row has the same scan type and the same database type
	// (D190).
	row := func(name, goType string) dbimptest.TypeRow {
		r := &rows{}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("string", "string"),
		row("number", "int64, float64, or *apd.Decimal for an integer too large for int64"),
		row("boolean", "bool"),
		row("null", "nil"),
		row("array", "[]any"),
		row("object", "map[string]any"),
		row("undefined", "nil"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping reads the account, GET /, which costs little and checks the endpoint and the signature.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because every request carries its own signature.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a decimal, a list and a map, which the driver binds as JSON values (D190).",
		"driver.QueryerContext":                 "The server binds each argument by its name (D190).",
		"driver.ExecerContext":                  "Exec fails with dbimp.ErrNotSupported for every statement, because the driver reads only (D190).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because a batch is atomic only inside one request and needs a write language (D190).",
		"driver.RowsColumnScanner":              "A value is decoded when its document is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "A column has no type, so the scan type is any (D190).",
		"driver.RowsColumnTypeDatabaseTypeName": "A column has no type, so the name is empty (D190).",
		"driver.RowsColumnTypeLength":           "A column has no type, so it has no length.",
		"driver.RowsColumnTypeNullable":         "A document can lack any key, so every column can be NULL.",
		"driver.RowsColumnTypePrecisionScale":   "A column has no type, so it has no precision and no scale.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// TestCatalogTable compares the table of the catalog statements in doc with the
// code, and writes it when DBIMP_UPDATE is set, so that the document cannot hold
// a column that the driver lacks.
func TestCatalogTable(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for _, c := range catalogs() {
		var form strings.Builder
		fmt.Fprintf(&form, `SELECT * FROM "%s"`, c.Name)
		for i, k := range c.Needs {
			sep := " AND "
			if i == 0 {
				sep = " WHERE "
			}
			fmt.Fprintf(&form, "%s%s = '...'", sep, k)
		}
		for _, k := range c.Allows {
			fmt.Fprintf(&form, " [AND %s = '...']", k)
		}
		fmt.Fprintf(&b, "`%s`\n\n| Column | Type | Source |\n| --- | --- | --- |\n", form.String())
		for _, col := range c.Columns {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", col.Name, col.Type, col.Note)
		}
		b.WriteString("\n")
	}
	dbimptest.WriteBlock(t, doc, "<!-- dbimp:catalog -->", "<!-- /dbimp:catalog -->", strings.TrimRight(b.String(), "\n")+"\n")
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"string":    "string",
	"number":    "number",
	"boolean":   "boolean",
	"null":      "null",
	"array":     "array",
	"object":    "map",
	"undefined": "null",
}
