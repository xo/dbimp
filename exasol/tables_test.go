package exasol //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/EXASOL.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(wire string, typ dataType, goType string) dbimptest.TypeRow {
		r := &rows{cols: []column{columnOf(wireColumn{Name: "C", DataType: typ})}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("BOOLEAN", dataType{Type: typeBoolean}, "bool"),
		row("DECIMAL INTEGER", dataType{Type: typeDecimal, Precision: 18}, "int64"),
		row("DECIMAL", dataType{Type: typeDecimal, Precision: 36, Scale: 10}, "*apd.Decimal"),
		row("DOUBLE", dataType{Type: typeDouble}, "float64"),
		row("CHAR", dataType{Type: typeChar, Size: 5}, "string"),
		row("VARCHAR", dataType{Type: typeVarchar, Size: 100}, "string"),
		row("DATE", dataType{Type: typeDate}, "dbimp.Date"),
		row("TIMESTAMP", dataType{Type: typeTimestamp}, "dbimp.LocalDateTime"),
		row("TIMESTAMP WITH LOCAL TIME ZONE", dataType{Type: typeTimestampTZ}, "time.Time"),
		row("INTERVAL YEAR TO MONTH", dataType{Type: typeIntervalYM}, "dbimp.Interval"),
		row("INTERVAL DAY TO SECOND", dataType{Type: typeIntervalDS}, "dbimp.Interval"),
		row("GEOMETRY", dataType{Type: typeGeometry}, "string"),
		row("HASHTYPE", dataType{Type: typeHashtype, Size: 32}, "[]byte"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "Connect opens a WebSocket and logs in, and the connector holds nothing else.",
		"io.Closer on the connector":            "The connector holds no transport or connection, because each connection owns its WebSocket.",
		"driver.Pinger":                         "Ping sends getAttributes, which costs the server nothing.",
		"driver.SessionResetter":                "A transaction is the only state that database/sql does not end, and it ends it before it reuses the connection (D102).",
		"driver.Validator":                      "IsValid is false after the server ended the session or the driver closed it after a fault, so database/sql drops the connection.",
		"driver.NamedValueChecker":              "It keeps an Option, a uint64, an *apd.Decimal and the civil types of the root package, which the driver writes in the form that the server reads (D208).",
		"driver.QueryerContext":                 "A statement with no argument goes in execute, and one with arguments is prepared, executed and closed (D208).",
		"driver.ExecerContext":                  "Exec reads the rowCount of the result. It has no last insert id.",
		"driver.ConnPrepareContext":             "PrepareContext sends createPreparedStatement, so a syntax error shows at once, and the server binds the arguments (D208).",
		"driver.ConnBeginTx":                    "BeginTx turns autocommit off, and Commit and Rollback turn it on. ReadOnly and an isolation level fail (D209).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A statement is one for each request, so an answer holds one result (recorded: \"two statements with a semicolon\").",
		"driver.RowsColumnTypeScanType":         "The type of each column gives its Go type (D206).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type of each column names its database type, such as DECIMAL or INTERVAL DAY TO SECOND.",
		"driver.RowsColumnTypeLength":           "A CHAR and a VARCHAR have their size in characters, and a HASHTYPE its size in bytes.",
		"driver.RowsColumnTypeNullable":         "The answer does not name NOT NULL, so every column can be NULL.",
		"driver.RowsColumnTypePrecisionScale":   "A DECIMAL has its precision and its scale.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step 8a
// and D206).
var typeKinds = map[string]string{
	"BOOLEAN":                        "boolean",
	"DECIMAL INTEGER":                "integer",
	"DECIMAL":                        "decimal",
	"DOUBLE":                         "float",
	"CHAR":                           "string",
	"VARCHAR":                        "string",
	"DATE":                           "date",
	"TIMESTAMP":                      "local timestamp",
	"TIMESTAMP WITH LOCAL TIME ZONE": "timestamp",
	"INTERVAL YEAR TO MONTH":         "interval",
	"INTERVAL DAY TO SECOND":         "interval",
	"GEOMETRY":                       "geometry",
	"HASHTYPE":                       "binary",
}
