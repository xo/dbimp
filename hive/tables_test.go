package hive //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/HIVE.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(wire string, id int32, goType string) dbimptest.TypeRow {
		r := &rows{cols: []colDesc{{typeID: id}}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("BOOLEAN", typeBoolean, "bool"),
		row("TINYINT", typeTinyint, "int64"),
		row("SMALLINT", typeSmallint, "int64"),
		row("INT", typeInt, "int64"),
		row("BIGINT", typeBigint, "int64"),
		row("FLOAT", typeFloat, "float64, as the server sends it"),
		row("DOUBLE", typeDouble, "float64"),
		row("DECIMAL", typeDecimal, "*apd.Decimal, with the scale of its column"),
		row("STRING", typeString, "string"),
		row("VARCHAR", typeVarchar, "string"),
		row("CHAR", typeChar, "string, with its padding"),
		row("DATE", typeDate, "dbimp.Date"),
		row("TIMESTAMP", typeTimestamp, "dbimp.LocalDateTime"),
		row("BINARY", typeBinary, "[]byte"),
		row("hive ARRAY", typeArray, "[]any, read from the JSON text of the server"),
		row("hive MAP", typeMap, "map[string]any, read from the JSON text of the server"),
		row("hive STRUCT", typeStruct, "map[string]any, read from the JSON text of the server"),
		row("hive INTERVAL_YEAR_MONTH", typeIntervalYM, "dbimp.Interval"),
		row("hive INTERVAL_DAY_TIME", typeIntervalDT, "dbimp.Interval"),
		row("hive UNIONTYPE", typeUnion, "string, the text of the server"),
		row("hive TIMESTAMP WITH LOCAL TIME ZONE", typeTimestampLocal, "time.Time"),
		row("hive NULL", typeNull, "nil"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares. Connect opens a session.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the credentials and the session.",
		"driver.SessionResetter":                "A connection holds a session, but a statement changes no state of it that database/sql does not end, because WithDatabase fails and a transaction does not exist.",
		"driver.Validator":                      "A connection whose session the server no longer knows is not valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a uint64 and the civil types of the root package, which the driver writes as literals itself (D235).",
		"driver.QueryerContext":                 "A query starts an operation, waits for it, and reads its rows page by page (D235).",
		"driver.ExecerContext":                  "Exec runs the statement to its end and reads no row. Hive gives the count of rows that it changed, and Impala does not.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments written as literals, each time, because the server binds none.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because neither flavor has a transaction over this interface (D235).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned, so a decimal keeps its scale.",
		"driver.RowsNextResultSet":              "A statement has one result.",
		"driver.RowsColumnTypeScanType":         "The type id of each column gives the Go type (D234).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type id of each column gives its name in upper case.",
		"driver.RowsColumnTypeLength":           "A VARCHAR and a CHAR name their length in the qualifiers of the type.",
		"driver.RowsColumnTypeNullable":         "The metadata does not name NOT NULL, so each column can hold NULL.",
		"driver.RowsColumnTypePrecisionScale":   "A DECIMAL names its precision and its scale in the qualifiers of the type.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step 8a
// and D234).
var typeKinds = map[string]string{
	"BOOLEAN":                             "boolean",
	"TINYINT":                             "integer",
	"SMALLINT":                            "integer",
	"INT":                                 "integer",
	"BIGINT":                              "integer",
	"FLOAT":                               "float",
	"DOUBLE":                              "float",
	"DECIMAL":                             "decimal",
	"STRING":                              "string",
	"VARCHAR":                             "string",
	"CHAR":                                "string",
	"DATE":                                "date",
	"TIMESTAMP":                           "local timestamp",
	"BINARY":                              "binary",
	"hive ARRAY":                          "array",
	"hive MAP":                            "map",
	"hive STRUCT":                         "map",
	"hive INTERVAL_YEAR_MONTH":            "interval",
	"hive INTERVAL_DAY_TIME":              "interval",
	"hive UNIONTYPE":                      "other",
	"hive TIMESTAMP WITH LOCAL TIME ZONE": "timestamp",
	"hive NULL":                           "null",
}
