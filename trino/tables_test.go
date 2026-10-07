package trino //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/TRINO.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the type whose rawType is raw. The wire name is the
	// name of the row in the table, which names the flavor of a type that one
	// release has.
	row := func(wire, raw, goType string) dbimptest.TypeRow {
		r := &rows{cols: []column{{sig: &signature{raw: raw}}}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("BOOLEAN", "boolean", "bool"),
		row("TINYINT", "tinyint", "int64"),
		row("SMALLINT", "smallint", "int64"),
		row("INTEGER", "integer", "int64"),
		row("BIGINT", "bigint", "int64"),
		row("REAL", "real", "float64, with the infinities and NaN from their strings"),
		row("DOUBLE", "double", "float64, with the infinities and NaN from their strings"),
		row("DECIMAL", "decimal", "*apd.Decimal, from the JSON string"),
		row("CHAR", "char", "string, with the padding of spaces that the server sends"),
		row("VARCHAR", "varchar", "string"),
		row("VARBINARY", "varbinary", "[]byte, from the base64 in a string"),
		row("JSON", "json", "the decoded JSON value: nil, bool, string, int64, float64, *apd.Decimal, []any or map[string]any, from the JSON text in a string"),
		row("DATE", "date", "dbimp.Date"),
		row("TIME", "time", "dbimp.LocalTime, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)"),
		row("TIME WITH TIME ZONE", "time with time zone", "dbimp.OffsetTime, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)"),
		row("TIMESTAMP", "timestamp", "dbimp.LocalDateTime, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)"),
		row("TIMESTAMP WITH TIME ZONE", "timestamp with time zone", "time.Time, in the zone that the text names, with the fraction to nanoseconds, and an error for a digit beyond them that is not zero (D175)"),
		row("INTERVAL YEAR TO MONTH", "interval year to month", "dbimp.Interval, with Months"),
		row("INTERVAL DAY TO SECOND", "interval day to second", "dbimp.Interval, with Days and Nanoseconds"),
		row("ARRAY", "array", "[]any, of the Go types of its elements, from the JSON array. On Presto, from the JSON text in a string"),
		row("MAP", "map", "map[string]any, with the keys as the strings of the JSON object. On Presto, from the JSON text in a string"),
		row("ROW", "row", "[]any, in the order of the fields, from the JSON array. On Presto, from the JSON text in a string"),
		row("UUID", "uuid", "uuid.UUID"),
		row("IPADDRESS", "ipaddress", "string, with the text of the server"),
		row("HYPERLOGLOG", "hyperloglog", "[]byte, from the base64 in a string"),
		row("P4HYPERLOGLOG", "p4hyperloglog", "[]byte, from the base64 in a string"),
		row("SETDIGEST", "setdigest", "[]byte, from the base64 in a string"),
		row("QDIGEST", "qdigest", "[]byte, from the base64 in a string"),
		row("TDIGEST", "tdigest", "[]byte, from the base64 in a string"),
		row("GEOMETRY", "geometry", "string, the WKT text of the server"),
		row("SPHERICALGEOGRAPHY", "sphericalgeography", "string, the WKT text of the server"),
		row("UNKNOWN", "unknown", "nil"),
		row("trino 483 NUMBER", "number", "*apd.Decimal, from the JSON string, with NaN and the infinities of apd"),
		row("trino 483 VARIANT", "variant", "the decoded JSON value, from the JSON value of the capability VARIANT"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and asks the server which product it is, once.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the user and the password. GET /v1/info needs neither.",
		"driver.SessionResetter":                "A connection keeps the catalog, the schema, the properties of the session and the prepared statements that the servers ask for, so ResetSession gives each caller the state of the DSN (D175).",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a decimal, the types of the root package, a UUID, a list and a map, which the driver writes as a literal of their own type (D175).",
		"driver.QueryerContext":                 "The driver sends each argument as a literal of EXECUTE name USING, on a statement that it names in the prepared-statement header (D175).",
		"driver.ExecerContext":                  "Exec reads the result to its end, and RowsAffected is the updateCount of the server, if it sent one.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx sends START TRANSACTION with the transaction headers, and keeps the id that the server answers (D175).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "The columns name their type, with its arguments, and each type has one Go type (D135 and D175).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type of a column, in upper case, without its arguments, such as BIGINT or TIMESTAMP WITH TIME ZONE.",
		"driver.RowsColumnTypeLength":           "A char and a varchar have the length in their type, and a varchar with none and a varbinary give the largest value.",
		"driver.RowsColumnTypeNullable":         "The columns do not say whether they can be NULL, and every type can be.",
		"driver.RowsColumnTypePrecisionScale":   "A decimal has its precision and its scale in its type.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"BOOLEAN":                  "boolean",
	"TINYINT":                  "integer",
	"SMALLINT":                 "integer",
	"INTEGER":                  "integer",
	"BIGINT":                   "integer",
	"REAL":                     "float",
	"DOUBLE":                   "float",
	"DECIMAL":                  "decimal",
	"CHAR":                     "string",
	"VARCHAR":                  "string",
	"VARBINARY":                "binary",
	"JSON":                     "json",
	"DATE":                     "date",
	"TIME":                     "time of day",
	"TIME WITH TIME ZONE":      "time of day with offset",
	"TIMESTAMP":                "local timestamp",
	"TIMESTAMP WITH TIME ZONE": "timestamp",
	"INTERVAL YEAR TO MONTH":   "interval",
	"INTERVAL DAY TO SECOND":   "interval",
	"ARRAY":                    "array",
	"MAP":                      "map",
	"ROW":                      "tuple",
	"UUID":                     "uuid",
	"IPADDRESS":                "other",
	"HYPERLOGLOG":              "binary",
	"P4HYPERLOGLOG":            "binary",
	"SETDIGEST":                "binary",
	"QDIGEST":                  "binary",
	"TDIGEST":                  "binary",
	"GEOMETRY":                 "geometry",
	"SPHERICALGEOGRAPHY":       "geometry",
	"UNKNOWN":                  "null",
	"trino 483 NUMBER":         "decimal",
	"trino 483 VARIANT":        "json",
}
