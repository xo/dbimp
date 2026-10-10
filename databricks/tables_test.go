package databricks //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/DATABRICKS.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the type that the text names, as the manifest writes
	// type_text, with the Go type and the name of the row in the table, which is
	// the name of the type in the survey.
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
		row("BINARY", "BINARY", "[]byte"),
		row("DATE", "DATE", "dbimp.Date"),
		row("TIMESTAMP", "TIMESTAMP", "time.Time"),
		row("TIMESTAMP_NTZ", "TIMESTAMP_NTZ", "dbimp.LocalDateTime"),
		row("INTERVAL YEAR TO MONTH", "INTERVAL YEAR TO MONTH", "dbimp.Interval"),
		row("INTERVAL DAY TO SECOND", "INTERVAL DAY TO SECOND", "dbimp.Interval"),
		row("ARRAY", "ARRAY<INT>", "[]any"),
		row("MAP", "MAP<STRING, INT>", "map[string]any"),
		row("STRUCT", "STRUCT<a: INT, b: STRING>", "map[string]any"),
		row("VARIANT", "VARIANT", "the decoded JSON value"),
		row("GEOMETRY", "GEOMETRY(0)", "string"),
		row("GEOGRAPHY", "GEOGRAPHY(4326)", "string"),
		row("VOID", "VOID", "nil"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and sends the token of the DSN as a Bearer token to the host of the DSN only (D193).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the token and the warehouse, and wakes a warehouse that stopped. It costs little.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because each request is its own session (measured).",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, and the values that the driver binds with a type of their own: a decimal, a dbimp.Date, a dbimp.LocalDateTime, a dbimp.Interval, and a list and a map that fail with dbimp.ErrArguments (D193).",
		"driver.QueryerContext":                 "The statement goes to POST /api/2.0/sql/statements with its arguments as typed parameters, which the server binds to its ? and its :name (D193).",
		"driver.ExecerContext":                  "Exec reads the row of num_affected_rows, and RowsAffected is its value, or an error that wraps dbimp.ErrNotSupported when the answer has no such row (D178 and D193).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because the API has no session and a transaction does not last across requests (D20 and D193).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result. A script answers with the result of its last statement.",
		"driver.RowsColumnTypeScanType":         "The manifest names the type of each column in type_text, and each type has one Go type (D135 and D193).",
		"driver.RowsColumnTypeDatabaseTypeName": "The name of the type in upper case, with no parameters, such as DECIMAL. Each interval reports INTERVAL.",
		"driver.RowsColumnTypeLength":           "The manifest has no length for a string or a binary column (measured).",
		"driver.RowsColumnTypeNullable":         "The manifest has no nullability, so every column can be NULL and the second result is false (measured).",
		"driver.RowsColumnTypePrecisionScale":   "A DECIMAL column has its precision and its scale in the manifest.",
	}, NewConnector(recordedConfig()), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a and D193).
var typeKinds = map[string]string{
	"TINYINT":                "integer",
	"SMALLINT":               "integer",
	"INT":                    "integer",
	"BIGINT":                 "integer",
	"FLOAT":                  "float",
	"DOUBLE":                 "float",
	"DECIMAL":                "decimal",
	"BOOLEAN":                "boolean",
	"STRING":                 "string",
	"BINARY":                 "binary",
	"DATE":                   "date",
	"TIMESTAMP":              "timestamp",
	"TIMESTAMP_NTZ":          "local timestamp",
	"INTERVAL YEAR TO MONTH": "interval",
	"INTERVAL DAY TO SECOND": "interval",
	"ARRAY":                  "array",
	"MAP":                    "map",
	"STRUCT":                 "map",
	"VARIANT":                "json",
	"GEOMETRY":               "geometry",
	"GEOGRAPHY":              "geometry",
	"VOID":                   "null",
}
