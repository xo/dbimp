package snowflake //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/SNOWFLAKE.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the column c, with the Go type and the name of the
	// row in the table, which is the name of the type in the survey.
	row := func(wire string, c column, goType string) dbimptest.TypeRow {
		r := &rows{types: []column{c}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	plain := func(wire string) column {
		return column{wire: wire, precision: -1, scale: -1, length: -1, nullable: true}
	}
	integer := column{wire: wireFixed, precision: 18, scale: 0, length: -1, nullable: true}
	// A fixed column is an integer or a decimal by its precision and its scale,
	// so it has two rows (D183). The names are those of the entries of
	// features.json.
	decimal := column{wire: wireFixed, precision: 38, scale: 10, length: -1, nullable: true}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("FIXED INTEGER", integer, "int64"),
		row("FIXED DECIMAL", decimal, "*apd.Decimal"),
		row("REAL", plain(wireReal), "float64"),
		row("TEXT", plain(wireText), "string"),
		row("BOOLEAN", plain(wireBoolean), "bool"),
		row("DATE", plain(wireDate), "dbimp.Date"),
		row("TIME", plain(wireTime), "dbimp.LocalTime"),
		row("TIMESTAMP_NTZ", plain(wireTimestampNTZ), "dbimp.LocalDateTime"),
		row("TIMESTAMP_LTZ", plain(wireTimestampLTZ), "time.Time"),
		row("TIMESTAMP_TZ", plain(wireTimestampTZ), "time.Time"),
		row("BINARY", plain(wireBinary), "[]byte"),
		row("VARIANT", plain(wireVariant), "the decoded JSON value"),
		row("OBJECT", plain(wireObject), "map[string]any"),
		row("ARRAY", plain(wireArray), "[]any"),
		row("GEOGRAPHY", plain(wireGeography), "map[string]any"),
		row("GEOMETRY", plain(wireGeometry), "map[string]any"),
		row("VECTOR", plain(wireVector), "dbimp.Vector[float64]"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and the token that the driver signs with the private key of the DSN (D183).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the token, the login and the warehouse, and costs little.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because each request is its own session (measured).",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, and the values that the driver binds with a type of their own: a decimal, a dbimp.Date, a dbimp.LocalTime, a dbimp.LocalDateTime, and a list and a map that fail with dbimp.ErrArguments (D183).",
		"driver.QueryerContext":                 "The statement goes to POST /api/v2/statements with its arguments as typed bindings, which the server binds to its ? (D183).",
		"driver.ExecerContext":                  "Exec reads the result to its end with no decoding, and RowsAffected is the sum of the counts in stats, or an error that wraps dbimp.ErrNotSupported when the answer has no count (D178).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because the server refuses BEGIN alone and each request is its own session (D183).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result. The driver reads the partitions of one result as one set of rows.",
		"driver.RowsColumnTypeScanType":         "The metadata names the type of each column, with its precision and its scale, and each type has one Go type (D135 and D183).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type of the column in upper case, such as FIXED. The server names a geography and a geometry object, so both report OBJECT (measured).",
		"driver.RowsColumnTypeLength":           "A text column and a binary column have the length that the metadata names.",
		"driver.RowsColumnTypeNullable":         "The metadata has the member nullable for each column.",
		"driver.RowsColumnTypePrecisionScale":   "A fixed column has its precision and its scale in the metadata.",
	}, NewConnector(config()), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a and D183).
var typeKinds = map[string]string{
	"FIXED INTEGER": "integer",
	"FIXED DECIMAL": "decimal",
	"REAL":          "float",
	"TEXT":          "string",
	"BOOLEAN":       "boolean",
	"DATE":          "date",
	"TIME":          "time of day",
	"TIMESTAMP_NTZ": "local timestamp",
	"TIMESTAMP_LTZ": "timestamp",
	"TIMESTAMP_TZ":  "timestamp",
	"BINARY":        "binary",
	"VARIANT":       "json",
	"OBJECT":        "map",
	"ARRAY":         "array",
	"GEOGRAPHY":     "geometry",
	"GEOMETRY":      "geometry",
	"VECTOR":        "vector",
}
