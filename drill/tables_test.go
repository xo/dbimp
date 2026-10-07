package drill //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/DRILL.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(name, goType string) dbimptest.TypeRow {
		c := column{name: name}
		r := &rows{types: []column{c}, lists: []bool{false}}
		nullable, _ := r.ColumnTypeNullable(0)
		if goType == "" {
			goType = scanType(c, false).String()
		}
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("INT", ""),
		row("BIGINT", ""),
		row("FLOAT4", "float64, of the float32 that the server widens"),
		row("FLOAT8", "float64, with the infinities and NaN from their strings"),
		row("VARDECIMAL", "*apd.Decimal, from the digits of the JSON number"),
		row("BIT", ""),
		row("VARCHAR", ""),
		row("VARBINARY", "[]byte, from base64"),
		row("DATE", "dbimp.Date, from the milliseconds since 1970-01-01 in UTC"),
		row("TIME", "dbimp.LocalTime, from the milliseconds since midnight"),
		row("TIMESTAMP", "dbimp.LocalDateTime, from the milliseconds since 1970-01-01 in UTC"),
		row("INTERVAL", "dbimp.Interval, from its ISO 8601 text"),
		row("INTERVALDAY", "dbimp.Interval, from its ISO 8601 text"),
		row("INTERVALYEAR", "dbimp.Interval, from its ISO 8601 text"),
		row("MAP", "map[string]any, of the decoded JSON values"),
		row("LIST", "[]any, of the decoded JSON values of its elements"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1 AS one FROM (VALUES(1)), which checks the credentials. GET /status.json answers HTTP 200 to a wrong password.",
		"driver.SessionResetter":                "ALTER SESSION is state that the caller asks for, and D165 keeps it for the connection. The server ends an idle session by itself.",
		"driver.Validator":                      "A connection holds only a cookie, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a decimal, and the types of the root package that the driver writes as a literal of their own (D165).",
		"driver.QueryerContext":                 "The server binds no argument, so the driver writes each one as a literal (D165).",
		"driver.ExecerContext":                  "Exec reads the result to its end. RowsAffected is the count that CREATE TABLE AS answers.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because Drill has no transactions (D165).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "The metadata names the type of each column. A column that held a list names any (D165).",
		"driver.RowsColumnTypeDatabaseTypeName": "The metadata names the type of each column, such as VARDECIMAL, without its numbers.",
		"driver.RowsColumnTypeLength":           "The metadata names the width of a VARCHAR that has one, such as VARCHAR(5).",
		"driver.RowsColumnTypeNullable":         "The metadata holds no mode, and every type can be NULL.",
		"driver.RowsColumnTypePrecisionScale":   "The metadata names the precision and the scale of a VARDECIMAL, such as VARDECIMAL(38, 18).",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"INT":          "integer",
	"BIGINT":       "integer",
	"FLOAT4":       "float",
	"FLOAT8":       "float",
	"VARDECIMAL":   "decimal",
	"BIT":          "boolean",
	"VARCHAR":      "string",
	"VARBINARY":    "binary",
	"DATE":         "date",
	"TIME":         "time of day",
	"TIMESTAMP":    "local timestamp",
	"INTERVAL":     "interval",
	"INTERVALDAY":  "interval",
	"INTERVALYEAR": "interval",
	"MAP":          "map",
	"LIST":         "array",
}
