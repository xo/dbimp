package opensearch //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/OPENSEARCH.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(wire, goType string) dbimptest.TypeRow {
		r := &rows{types: []string{wire}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("undefined", "nil"),
		row("boolean", "bool"),
		row("byte", "int64"),
		row("short", "int64"),
		row("integer", "int64"),
		row("long", "int64"),
		row("float", "float64"),
		row("double", "float64"),
		row("half_float", "float64, from the legacy engine"),
		row("scaled_float", "float64, from the legacy engine"),
		row("keyword", "string"),
		row("text", "string"),
		row("ip", "string, because the server gives the text and no type of Go fits both forms"),
		row("binary", "[]byte, from base64"),
		row("date", "dbimp.Date, from the text of a day"),
		row("time", "dbimp.LocalTime"),
		row("timestamp", "time.Time, in UTC, because the server writes each instant in UTC with no zone"),
		row("datetime", "time.Time, in UTC, as a timestamp of 3.9.0"),
		row("geo_point", "map[string]any, with lat and lon, as the server writes it"),
		row("object", "map[string]any"),
		row("nested", "[]any, of a map[string]any for each nested object"),
		row("integer_range", "map[string]any, with gte and lte, because the server sends an object and the driver never returns the text of JSON"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the credentials as each principal. GET / needs a cluster privilege that the ordinary user lacks.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because the SQL plugin has no sessions.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option and a uint64, which the driver refuses above the range of a long (D168), and hands every other value to database/sql.",
		"driver.QueryerContext":                 "The driver writes each argument into the statement as a literal, because the server writes each value into the text itself (D168).",
		"driver.ExecerContext":                  "Exec reads the result to its end. SQL takes no write, so RowsAffected fails (D163).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because OpenSearch has no transactions (D168).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, and its pages are one result.",
		"driver.RowsColumnTypeScanType":         "The answer names the type of each column (D168).",
		"driver.RowsColumnTypeDatabaseTypeName": "The answer names the type of each column, which the driver writes in upper case.",
		"driver.RowsColumnTypeLength":           "The answer names no length.",
		"driver.RowsColumnTypeNullable":         "The answer does not say whether a column can be NULL, and every type can be.",
		"driver.RowsColumnTypePrecisionScale":   "The answer names no precision and no scale, and there is no decimal type.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"undefined":     "null",
	"boolean":       "boolean",
	"byte":          "integer",
	"short":         "integer",
	"integer":       "integer",
	"long":          "integer",
	"float":         "float",
	"double":        "float",
	"half_float":    "float",
	"scaled_float":  "float",
	"keyword":       "string",
	"text":          "string",
	"ip":            "string",
	"binary":        "binary",
	"date":          "date",
	"time":          "time of day",
	"timestamp":     "timestamp",
	"datetime":      "timestamp",
	"geo_point":     "geometry",
	"object":        "map",
	"nested":        "array",
	"integer_range": "range",
}
