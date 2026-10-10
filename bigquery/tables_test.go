package bigquery //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/BIGQUERY.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the column c, with the Go type and the name of the
	// row in the table, which is the name of the type in the survey.
	row := func(wire string, c column, goType string) dbimptest.TypeRow {
		r := &rows{cols: []column{c}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	plain := func(wire string) column {
		return column{name: "c", wire: wire, mode: modeNullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("INTEGER", plain(wireInteger), "int64"),
		row("FLOAT", plain(wireFloat), "float64"),
		row("NUMERIC", plain(wireNumeric), "*apd.Decimal"),
		row("BIGNUMERIC", plain(wireBigNumeric), "*apd.Decimal"),
		row("BOOLEAN", plain(wireBoolean), "bool"),
		row("STRING", plain(wireString), "string"),
		row("BYTES", plain(wireBytes), "[]byte"),
		row("DATE", plain(wireDate), "dbimp.Date"),
		row("TIME", plain(wireTime), "dbimp.LocalTime"),
		row("DATETIME", plain(wireDatetime), "dbimp.LocalDateTime"),
		row("TIMESTAMP", plain(wireTimestamp), "time.Time"),
		row("JSON", plain(wireJSON), "the decoded JSON value"),
		row("GEOGRAPHY", plain(wireGeography), "string"),
		row("INTERVAL", plain(wireInterval), "dbimp.Interval"),
		row("ARRAY", column{name: "c", wire: wireInteger, mode: modeRepeated}, "[]any"),
		row("RECORD", plain(wireRecord), "map[string]any"),
		row("RANGE", plain(wireRange), "string"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and the access token that the driver gets by signing a request with the key file of the DSN (D189).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1 as a dry run, which checks the token, the project and the right to make a job, and which the service does not bill (D189).",
		"driver.SessionResetter":                "A connection holds nothing on the server, because each request is its own job with no session (D189).",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, and the values that the driver binds with a type of their own: a decimal, a dbimp.Date, a dbimp.LocalTime, a dbimp.LocalDateTime, a dbimp.Interval, and a list and a map that fail with dbimp.ErrArguments (D189).",
		"driver.QueryerContext":                 "The statement goes to POST /queries with its arguments as typed parameters, which the server binds (D189).",
		"driver.ExecerContext":                  "Exec reads the head of the answer and no row, and RowsAffected is numDmlAffectedRows, or an error that wraps dbimp.ErrNotSupported when the answer has no count (D178).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because the service refuses BEGIN TRANSACTION alone and the first release has no session across requests (D20 and D189).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement or one script, and the answer holds the result of its last statement, so it has one result. The driver reads the pages of one result as one set of rows (D189).",
		"driver.RowsColumnTypeScanType":         "The schema names the type of each field, and each type has one Go type (D135 and D189).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type of the field in upper case, such as INTEGER, and ARRAY for a field of the mode REPEATED.",
		"driver.RowsColumnTypeLength":           "The schema names no length for a field (recorded: bigquery-070).",
		"driver.RowsColumnTypeNullable":         "The mode of the field: an ARRAY is never NULL, and a REQUIRED field is not NULL.",
		"driver.RowsColumnTypePrecisionScale":   "The schema names no precision and no scale for a field (recorded: bigquery-070).",
	}, NewConnector(config()), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step 8a
// and D189).
var typeKinds = map[string]string{
	"INTEGER":    "integer",
	"FLOAT":      "float",
	"NUMERIC":    "decimal",
	"BIGNUMERIC": "decimal",
	"BOOLEAN":    "boolean",
	"STRING":     "string",
	"BYTES":      "binary",
	"DATE":       "date",
	"TIME":       "time of day",
	"DATETIME":   "local timestamp",
	"TIMESTAMP":  "timestamp",
	"JSON":       "json",
	"GEOGRAPHY":  "geometry",
	"INTERVAL":   "interval",
	"ARRAY":      "array",
	"RECORD":     "tuple",
	"RANGE":      "range",
}
