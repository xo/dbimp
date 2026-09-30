package pinot //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/PINOT.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(name, goType string) dbimptest.TypeRow {
		r := &rows{types: []string{name}}
		nullable, _ := r.ColumnTypeNullable(0)
		if goType == "" {
			goType = scanType(name).String()
		}
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("INT", ""),
		row("LONG", ""),
		row("FLOAT", "float64, with NaN and the infinities"),
		row("DOUBLE", "float64, with NaN and the infinities"),
		row("BIG_DECIMAL", "*apd.Decimal, with every digit"),
		row("BOOLEAN", ""),
		row("TIMESTAMP", "time.Time, in UTC, to the millisecond"),
		row("STRING", ""),
		row("JSON", "the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any. The multi-stage engine names the type STRING, so the value is its text there"),
		row("BYTES", "[]byte, from hex"),
		row("MAP", "map[string]any, of the decoded JSON values"),
		row("INT_ARRAY", "[]any, of int64"),
		row("LONG_ARRAY", "[]any, of int64"),
		row("FLOAT_ARRAY", "[]any, of float64"),
		row("DOUBLE_ARRAY", "[]any, of float64"),
		row("BOOLEAN_ARRAY", "[]any, of bool"),
		row("STRING_ARRAY", "[]any, of string"),
		row("TIMESTAMP_ARRAY", "[]any, of time.Time"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the credentials.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because Pinot has no sessions.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a uint64 and a decimal, which the driver writes as literals of their own (D132).",
		"driver.QueryerContext":                 "The driver writes each argument into the text as a literal, because the Broker binds none (D132).",
		"driver.ExecerContext":                  "Exec reads the result to its end. The Broker takes no write, so RowsAffected fails (D128).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments written in each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because Pinot has no transactions (D133).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "dataSchema names the type of each column (D130).",
		"driver.RowsColumnTypeDatabaseTypeName": "dataSchema names the type of each column, such as LONG or INT_ARRAY.",
		"driver.RowsColumnTypeLength":           "dataSchema names no length.",
		"driver.RowsColumnTypeNullable":         "Every column can be NULL, because each query sends enableNullHandling=true (D130).",
		"driver.RowsColumnTypePrecisionScale":   "dataSchema names no precision and no scale, and a BIG_DECIMAL has any scale.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"INT":             "integer",
	"LONG":            "integer",
	"FLOAT":           "float",
	"DOUBLE":          "float",
	"BIG_DECIMAL":     "decimal",
	"BOOLEAN":         "boolean",
	"TIMESTAMP":       "timestamp",
	"STRING":          "string",
	"JSON":            "json",
	"BYTES":           "binary",
	"MAP":             "map",
	"INT_ARRAY":       "array",
	"LONG_ARRAY":      "array",
	"FLOAT_ARRAY":     "array",
	"DOUBLE_ARRAY":    "array",
	"BOOLEAN_ARRAY":   "array",
	"STRING_ARRAY":    "array",
	"TIMESTAMP_ARRAY": "array",
}
