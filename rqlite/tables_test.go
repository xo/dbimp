package rqlite //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/RQLITE.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(name, goType string) dbimptest.TypeRow {
		r := &rows{types: []string{name}, classes: []class{classOf(name)}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("INTEGER", "int64, for each declared type whose name holds INT, such as BIGINT"),
		row("REAL", "float64, for each declared type whose name holds REAL, FLOA or DOUB"),
		row("TEXT", "string, for each declared type whose name holds CHAR, CLOB or TEXT"),
		row("BLOB", "[]byte, from an array of bytes"),
		row("NUMERIC", "int64 for a number with no fraction that fits, and else float64, for each declared type that no other row names, such as DECIMAL(10,2)"),
		row("BOOLEAN", "bool"),
		row("DATE", "dbimp.Date, from the RFC 3339 text of the server"),
		row("DATETIME", "dbimp.LocalDateTime, from the RFC 3339 text of the server"),
		row("TIMESTAMP", "time.Time, in the offset of the server"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1 on /db/query, which checks the credentials and needs only the permission query.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because each request stands alone and the driver has no transactions (D144).",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a uint64 and the civil types of the root package, which the driver writes itself (D143).",
		"driver.QueryerContext":                 "A query goes to /db/request, so that a write with RETURNING returns its rows (D142).",
		"driver.ExecerContext":                  "Exec goes to /db/execute, and gives the counts of the server for a statement that counts rows, and 0 for any other (D142).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time, because the server keeps no prepared statement.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because rqlite shares one write connection among every client (D144).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result (D142).",
		"driver.RowsColumnTypeScanType":         "types names the declared type of each column, whose affinity gives the Go type (D140).",
		"driver.RowsColumnTypeDatabaseTypeName": "types names the declared type of each column, such as BIGINT or VARCHAR(10).",
		"driver.RowsColumnTypeLength":           "types names the length as the statement wrote it, such as VARCHAR(10), and SQLite does not keep to it.",
		"driver.RowsColumnTypeNullable":         "The answer does not name NOT NULL, and a column of SQLite holds NULL unless its table forbids it.",
		"driver.RowsColumnTypePrecisionScale":   "types names the precision as the statement wrote it, such as DECIMAL(10,2), and SQLite does not keep to it.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a and D140).
var typeKinds = map[string]string{
	"INTEGER":   "integer",
	"REAL":      "float",
	"TEXT":      "string",
	"BLOB":      "binary",
	"NUMERIC":   "number",
	"BOOLEAN":   "boolean",
	"DATE":      "date",
	"DATETIME":  "local timestamp",
	"TIMESTAMP": "timestamp",
}
