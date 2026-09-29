package databend //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/DATABEND.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(wire, name, goType string) dbimptest.TypeRow {
		ct, err := parseType("Nullable(" + name + ")")
		if err != nil {
			t.Fatal(err)
		}
		if goType == "" {
			goType = ct.scanType().String()
		}
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: ct.scanType().String(), DatabaseType: (&rows{p: &page{types: []*colType{ct}}}).ColumnTypeDatabaseTypeName(0), Nullable: ct.nullable}
	}
	dbimptest.TypeTable(t, doc, []dbimptest.TypeRow{
		row("boolean", "Boolean", ""),
		row("tinyint", "Int8", ""),
		row("smallint", "Int16", ""),
		row("int", "Int32", ""),
		row("bigint", "Int64", ""),
		row("uint8", "UInt8", ""),
		row("uint16", "UInt16", ""),
		row("uint32", "UInt32", ""),
		row("uint64", "UInt64", "int64, or *apd.Decimal above the range of int64"),
		row("float", "Float32", ""),
		row("double", "Float64", ""),
		row("decimal", "Decimal(38, 10)", ""),
		row("date", "Date", "time.Time, at midnight in UTC"),
		row("timestamp", "Timestamp", "time.Time, in the timezone of the session"),
		row("timestamp_tz", "Timestamp_Tz", "time.Time, with its offset"),
		row("interval", "Interval", "string, as the server writes it"),
		row("string", "String", ""),
		row("binary", "Binary", ""),
		row("array", "Array(Int32 NULL)", "[]any, of the Go types of its elements (D119)"),
		row("map", "Map(String, Int32 NULL)", "map[string]any, or map[any]any for a key that is not a String (D119)"),
		row("tuple", "Tuple(Int32 NULL, String NULL)", "[]any, of the Go types of its fields (D119)"),
		row("variant", "Variant", "the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any"),
		row("bitmap", "Bitmap", "none: reading a value fails with dbimp.ErrNotSupported (D119)"),
		row("vector", "Vector(3)", ""),
		row("geometry", "Geometry", "string, in WKT"),
		row("geography", "Geography", "string, in WKT"),
	})
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the credentials and the database.",
		"driver.SessionResetter":                "A USE or a SET stays in the session of the connection for as long as it lives, as on MySQL, so nothing needs a reset (D126).",
		"driver.Validator":                      "A connection holds its session in memory, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a uint64 and a decimal, which the server binds as JSON (D120).",
		"driver.QueryerContext":                 "The server binds each argument itself, through params (D120).",
		"driver.ExecerContext":                  "Exec reads the result to its end, and RowsAffected is the count that the result names.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, bound each time.",
		"driver.ConnBeginTx":                    "A transaction that the session carries, which a DDL statement ends (D121).",
		"driver.RowsColumnScanner":              "A value is decoded from its text when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so a response has one result.",
		"driver.RowsColumnTypeScanType":         "schema names the type of each column (D118).",
		"driver.RowsColumnTypeDatabaseTypeName": "schema names the type of each column, such as DECIMAL(38, 10).",
		"driver.RowsColumnTypeLength":           "schema names no length.",
		"driver.RowsColumnTypeNullable":         "schema wraps a type that can be NULL in Nullable.",
		"driver.RowsColumnTypePrecisionScale":   "A Decimal names its precision and its scale.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &tx{}, &stmt{})
}
