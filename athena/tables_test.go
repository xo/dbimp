package athena //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/ATHENA.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the type wire, with the Go type and the name of the
	// row in the table, which is the name of the type in the survey.
	row := func(wire, goType string) dbimptest.TypeRow {
		r := &rows{cols: []column{{Name: "v", Type: wire}}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: strings.ToUpper(wire), Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	// The server names a REAL float, so the row FLOAT holds the type float, and
	// the database type of every row is its name in upper case.
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("tinyint", "int64"),
		row("smallint", "int64"),
		row("integer", "int64"),
		row("bigint", "int64"),
		row("float", "float64, with the infinities and NaN from their text"),
		row("double", "float64, with the infinities and NaN from their text"),
		row("decimal", "*apd.Decimal, from the text"),
		row("varchar", "string"),
		row("char", "string, with the padding of spaces that the server sends"),
		row("string", "string"),
		row("boolean", "bool"),
		row("date", "dbimp.Date"),
		row("time", "dbimp.LocalTime"),
		row("time with time zone", "dbimp.OffsetTime, with no fraction when the text has none"),
		row("timestamp", "dbimp.LocalDateTime, with no fraction when the text has none"),
		row("timestamp with time zone", "time.Time, in the zone that the text names, an offset or a name"),
		row("varbinary", "[]byte, from the hex pairs that spaces separate"),
		row("json", "the decoded JSON value, from the JSON text"),
		row("ipaddress", "netip.Addr"),
		row("uuid", "uuid.UUID"),
		row("interval day to second", "dbimp.Interval, with Days and Nanoseconds"),
		row("interval year to month", "dbimp.Interval, with Months"),
		row("geometry", "string, the well known text of the server"),
		row("array", "string, the text of the server"),
		row("map", "string, the text of the server"),
		row("row", "string, the text of the server"),
		row("unknown", "nil"),
	}, upperKeys(typeKinds)))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and the credentials that the driver signs each request with (D192).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping sends GetWorkGroup, or ListWorkGroups when the DSN names no workgroup. Neither starts a query, so neither scans data or costs money.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because each query is its own request, so there is nothing to reset.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, and the values that the driver writes as a literal of a type of their own: a decimal, a dbimp.Date, a dbimp.LocalTime, a dbimp.OffsetTime, a dbimp.LocalDateTime, a dbimp.Interval, a uuid.UUID, a netip.Addr, a list and a map (D192).",
		"driver.QueryerContext":                 "The statement goes to StartQueryExecution with its arguments as ExecutionParameters, which the server binds to its ? (D192).",
		"driver.ExecerContext":                  "Exec reads the result to its end with no decoding, and RowsAffected is UpdateCount, or an error that wraps dbimp.ErrNotSupported when the answer has none (D178).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time. The driver does not use the prepared statements of Athena (D192 item 4).",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because the server refuses START TRANSACTION (D20 and D192).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so a query has one result. The driver reads the pages of one result as one set of rows.",
		"driver.RowsColumnTypeScanType":         "ColumnInfo names the type of each column, and each type has one Go type (D135 and D192).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type of the column in upper case, such as BIGINT. The server names a REAL float.",
		"driver.RowsColumnTypeLength":           "A char column has its length, and a varchar with no length, a string and a varbinary have the largest length.",
		"driver.RowsColumnTypeNullable":         "The member Nullable of ColumnInfo is always UNKNOWN, and every type can be NULL, so every column can.",
		"driver.RowsColumnTypePrecisionScale":   "A decimal column has its precision and its scale in ColumnInfo.",
	}, NewConnector(Config{}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step 8a
// and D192).
var typeKinds = map[string]string{
	"tinyint":                  "integer",
	"smallint":                 "integer",
	"integer":                  "integer",
	"bigint":                   "integer",
	"float":                    "float",
	"double":                   "float",
	"decimal":                  "decimal",
	"varchar":                  "string",
	"char":                     "string",
	"string":                   "string",
	"boolean":                  "boolean",
	"date":                     "date",
	"time":                     "time of day",
	"time with time zone":      "time of day with offset",
	"timestamp":                "local timestamp",
	"timestamp with time zone": "timestamp",
	"varbinary":                "binary",
	"json":                     "json",
	"ipaddress":                "ip address",
	"uuid":                     "uuid",
	"interval day to second":   "interval",
	"interval year to month":   "interval",
	"geometry":                 "geometry",
	"array":                    "other",
	"map":                      "other",
	"row":                      "other",
	"unknown":                  "null",
}

// upperKeys returns kinds with the names of its keys in upper case, as the type
// table writes the names of the wire types.
func upperKeys(kinds map[string]string) map[string]string {
	out := make(map[string]string, len(kinds))
	for k, v := range kinds {
		out[strings.ToUpper(k)] = v
	}
	return out
}
