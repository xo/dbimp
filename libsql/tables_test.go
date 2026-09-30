package libsql //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/LIBSQL.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(name, goType string) dbimptest.TypeRow {
		r := &rows{decls: []string{name}, columns: []column{columnOf(name)}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("INTEGER", "int64, for each declared type whose name holds INT, such as BIGINT"),
		row("REAL", "float64, for each declared type whose name holds REAL, FLOA or DOUB. An infinity is +Inf, because the server loses its sign"),
		row("TEXT", "string, for each declared type whose name holds CHAR, CLOB or TEXT"),
		row("BLOB", "[]byte, from base64"),
		row("NUMERIC", "int64 for an integer, and float64 for a float, for each declared type that no other row names, such as DECIMAL(10,2)"),
		row("BOOLEAN", "bool, from an integer: 0 is false, and any other is true"),
		row("DATE", "dbimp.Date, from text of the form YYYY-MM-DD"),
		row("DATETIME", "dbimp.LocalDateTime, from text with no zone"),
		row("TIMESTAMP", "time.Time, from RFC 3339 text, or text with no zone in UTC"),
		row("F32_BLOB", "dbimp.Vector[float32], from the bytes of the BLOB, and []byte for a BLOB that is not 4n bytes"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1 on a new stream, which checks the token.",
		"driver.SessionResetter":                "A connection holds a stream only while a transaction is open, and database/sql ends the transaction before it reuses the connection (D102).",
		"driver.Validator":                      "A connection holds nothing on the server outside a transaction, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a uint64 and the civil types of the root package, which the driver writes itself (D152).",
		"driver.QueryerContext":                 "A query reads its rows from /v3/cursor, one entry at a time (D149).",
		"driver.ExecerContext":                  "Exec runs on /v3/pipeline, and gives affected_row_count and last_insert_rowid (D152).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx sends BEGIN on a stream, which the baton holds across requests. ReadOnly fails, because the server keeps no transaction read-only (D150).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A cursor holds one statement, so an answer has one result (D149).",
		"driver.RowsColumnTypeScanType":         "decltype names the declared type of each column, whose affinity gives the Go type (D147).",
		"driver.RowsColumnTypeDatabaseTypeName": "decltype names the declared type of each column, such as BIGINT or F32_BLOB(3).",
		"driver.RowsColumnTypeLength":           "decltype names the length as the statement wrote it, and SQLite does not keep to it.",
		"driver.RowsColumnTypeNullable":         "The answer does not name NOT NULL, and a column of SQLite holds NULL unless its table forbids it.",
		"driver.RowsColumnTypePrecisionScale":   "decltype names the precision as the statement wrote it, and SQLite does not keep to it.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a and D147).
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
	"F32_BLOB":  "vector",
}
