package maxcompute //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/MAXCOMPUTE.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the type that the text names, as EXPLAIN OUTPUT writes
	// it, with the Go type and the name of the row in the table, which is the name
	// of the type in the survey.
	row := func(wire, text, goType string) dbimptest.TypeRow {
		typ, err := parseType(text)
		if err != nil {
			t.Fatalf("reading the type %q: %v", text, err)
		}
		r := &rows{cols: []column{{name: "c", typ: typ}}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("TINYINT", "TINYINT", "int64"),
		row("SMALLINT", "SMALLINT", "int64"),
		row("INT", "INT", "int64"),
		row("BIGINT", "BIGINT", "int64"),
		row("FLOAT", "FLOAT", "float64"),
		row("DOUBLE", "DOUBLE", "float64"),
		row("DECIMAL", "DECIMAL(38,18)", "*apd.Decimal"),
		row("BOOLEAN", "BOOLEAN", "bool"),
		row("STRING", "STRING", "string"),
		row("VARCHAR", "VARCHAR(5)", "string"),
		row("CHAR", "CHAR(5)", "string"),
		row("BINARY", "BINARY", "[]byte"),
		row("DATE", "DATE", "dbimp.Date"),
		row("DATETIME", "DATETIME", "time.Time"),
		row("TIMESTAMP", "TIMESTAMP", "time.Time"),
		row("TIMESTAMP_NTZ", "TIMESTAMP_NTZ", "dbimp.LocalDateTime"),
		row("INTERVAL_DAY_TIME", "INTERVAL_DAY_TIME", "dbimp.Interval"),
		row("INTERVAL_YEAR_MONTH", "INTERVAL_YEAR_MONTH", "dbimp.Interval"),
		row("JSON", "JSON", "any"),
		row("ARRAY", "ARRAY<STRING>", "string"),
		row("MAP", "MAP<STRING,BIGINT>", "string"),
		row("STRUCT", "STRUCT<x:BIGINT>", "string"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and signs each request with the key of the DSN for the host of the DSN only (D218 and D221).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping reads the project, which checks the key and the project and costs no query.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because each statement is its own instance (recorded).",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, and the values that the driver writes as literals of their own: a decimal, a dbimp.Date, a dbimp.LocalDateTime, a dbimp.Interval, and a list and a map that the binding refuses with dbimp.ErrArguments (D220).",
		"driver.QueryerContext":                 "The driver sends EXPLAIN OUTPUT for the types and then the statement, with its arguments written into its text, and reads the CSV (D219 and D220).",
		"driver.ExecerContext":                  "Exec sends the statement and reads no result. RowsAffected is an error that wraps dbimp.ErrNotSupported, because the server gives no count (D178 and D220).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because the server has no transaction (D20 and D220).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "EXPLAIN OUTPUT names the type of each column, and each type has one Go type (D135 and D219).",
		"driver.RowsColumnTypeDatabaseTypeName": "The name of the type in upper case, with no parameters, such as DECIMAL.",
		"driver.RowsColumnTypeLength":           "The length of a VARCHAR or a CHAR column, from the text of its type. The other types have none.",
		"driver.RowsColumnTypeNullable":         "EXPLAIN OUTPUT has no nullability, so every column can be NULL and the second result is false (recorded).",
		"driver.RowsColumnTypePrecisionScale":   "A DECIMAL column has its precision and its scale in the text of its type.",
	}, NewConnector(recordedConfig()), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step 8a and
// D219).
var typeKinds = map[string]string{
	"TINYINT":             "integer",
	"SMALLINT":            "integer",
	"INT":                 "integer",
	"BIGINT":              "integer",
	"FLOAT":               "float",
	"DOUBLE":              "float",
	"DECIMAL":             "decimal",
	"BOOLEAN":             "boolean",
	"STRING":              "string",
	"VARCHAR":             "string",
	"CHAR":                "string",
	"BINARY":              "binary",
	"DATE":                "date",
	"DATETIME":            "timestamp",
	"TIMESTAMP":           "timestamp",
	"TIMESTAMP_NTZ":       "local timestamp",
	"INTERVAL_DAY_TIME":   "interval",
	"INTERVAL_YEAR_MONTH": "interval",
	"JSON":                "json",
	"ARRAY":               "other",
	"MAP":                 "other",
	"STRUCT":              "other",
}
