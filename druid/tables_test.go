package druid //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/DRUID.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(name, goType string) dbimptest.TypeRow {
		c := column{sql: name}
		if strings.HasPrefix(name, complexPrefix) {
			c = column{sql: typeOther, native: name}
		}
		r := &rows{types: []column{c}}
		nullable, _ := r.ColumnTypeNullable(0)
		if goType == "" {
			goType = scanType(c).String()
		}
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("BIGINT", ""),
		row("INTEGER", ""),
		row("FLOAT", "float64, from the JSON number of a float32"),
		row("REAL", ""),
		row("DOUBLE", "float64, with the infinities and NaN from their strings"),
		row("DECIMAL", "float64, because the server computes it as a double (D164)"),
		row("BOOLEAN", ""),
		row("VARCHAR", "string. A multi-value string with more than one value is a []any of its strings (D164)"),
		row("CHAR", ""),
		row("TIMESTAMP", "time.Time, from the ISO 8601 text with the offset of sqlTimeZone"),
		row("DATE", "dbimp.Date, from the date of the ISO 8601 text of its midnight in sqlTimeZone"),
		row("ARRAY", "[]any, of the Go types of the elements that the native type names, from the JSON text or the JSON array"),
		row("COMPLEX<json>", "the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any, from the JSON text in a string"),
		row("COMPLEX<HLLSketch>", "[]byte, from the base64 in the JSON text of a string"),
		row("NULL", "nil"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the credentials. GET /status needs more than the privilege to read.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because the SQL API has no sessions.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a decimal, a dbimp.Date, a dbimp.LocalDateTime and a slice, which the driver binds with a type of their own (D164).",
		"driver.QueryerContext":                 "The server binds each argument as a typed parameter (D164).",
		"driver.ExecerContext":                  "Exec reads the result to its end. The SQL API takes no write, so RowsAffected fails (D163).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because Druid has no transactions (D164).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, after any SET, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "The header names the SQL type and the native type of each column (D164).",
		"driver.RowsColumnTypeDatabaseTypeName": "The header names the SQL type of each column, such as BIGINT, or OTHER for a complex type.",
		"driver.RowsColumnTypeLength":           "The header names no length.",
		"driver.RowsColumnTypeNullable":         "The header does not say whether a column can be NULL, and every type can be.",
		"driver.RowsColumnTypePrecisionScale":   "The header names no precision and no scale, and a DECIMAL is a double.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"BIGINT":             "integer",
	"INTEGER":            "integer",
	"FLOAT":              "float",
	"REAL":               "float",
	"DOUBLE":             "float",
	"DECIMAL":            "decimal",
	"BOOLEAN":            "boolean",
	"VARCHAR":            "string",
	"CHAR":               "string",
	"TIMESTAMP":          "timestamp",
	"DATE":               "date",
	"ARRAY":              "array",
	"COMPLEX<json>":      "json",
	"COMPLEX<HLLSketch>": "binary",
	"NULL":               "null",
}
