package tablestore //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/TABLESTORE.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the type of a column, with the Go type and the name of
	// the row in the table, which is the name of the type in the survey.
	row := func(name string, c column, goType string) dbimptest.TypeRow {
		r := &rows{cols: []column{c}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	complexOf := func(l logicType) column {
		return column{data: dataComplex, logic: l, encode: dataLong}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("BIGINT", column{data: dataLong}, "int64"),
		row("DOUBLE", column{data: dataDouble}, "float64"),
		row("BOOL", column{data: dataBoolean}, "bool"),
		row("BOOLEAN", column{data: dataBoolean}, "bool"),
		row("MEDIUMTEXT", column{data: dataString}, "string"),
		row("MEDIUMBLOB", column{data: dataBinary}, "[]byte"),
		row("DATETIME", complexOf(logicDatetime), "dbimp.LocalDateTime"),
		row("DATE", complexOf(logicDate), "dbimp.Date"),
		row("TIMESTAMP", complexOf(logicTimestamp), "time.Time"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and the credentials that the driver signs each request with (D222).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping sends ListTable, which costs nothing and checks the endpoint, the signature and the credentials.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because each statement is its own request, so there is nothing to reset.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, and hands every other value to the default converter, because the driver writes each value as a literal (D224).",
		"driver.QueryerContext":                 "The statement goes to SQLQuery with its arguments written into its text, because the server binds none (D34 and D224).",
		"driver.ExecerContext":                  "Exec reads the answer to its end with no decoding, and RowsAffected is the count of the answer, or an error that wraps dbimp.ErrNotSupported when the answer has none (D178).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time. The server has no prepared statement.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because the server refuses BEGIN and START TRANSACTION (D20 and D224).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so a statement has one result.",
		"driver.RowsColumnTypeScanType":         "The rows name the type of each column, and each type has one Go type (D135 and D223).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type of the column in upper case, such as BIGINT. A BOOL column is BOOL.",
		"driver.RowsColumnTypeLength":           "The server sends no length for a column, so the driver reports none.",
		"driver.RowsColumnTypeNullable":         "The rows mark a NULL for any column, and the server sends no constraint, so every column can be NULL.",
		"driver.RowsColumnTypePrecisionScale":   "No type of the mapping has a precision or a scale.",
	}, NewConnector(Config{}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step 8a
// and D223).
var typeKinds = map[string]string{
	"BIGINT":     "integer",
	"DOUBLE":     "float",
	"BOOL":       "boolean",
	"BOOLEAN":    "boolean",
	"MEDIUMTEXT": "string",
	"MEDIUMBLOB": "binary",
	"DATETIME":   "local timestamp",
	"DATE":       "date",
	"TIMESTAMP":  "timestamp",
}
