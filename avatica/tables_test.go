package avatica //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/AVATICA.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(name, goType string) dbimptest.TypeRow {
		typ := colType{Type: "scalar", Name: name}
		if name == typeArray {
			typ = colType{Type: "array", Name: name, Component: &colType{Type: "scalar", Name: typeInteger}}
		}
		r := &rows{columns: []column{{Label: "C", Type: typ, Nullable: 1}}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: name, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row(typeTinyint, "int64"),
		row(typeSmallint, "int64"),
		row(typeInteger, "int64"),
		row(typeBigint, "int64"),
		row(typeUnsignedInt, "int64"),
		row(typeUnsignedLong, "int64, because Phoenix keeps it from 0 to 9223372036854775807"),
		row(typeDecimal, "*apd.Decimal, from the digits of the JSON number"),
		row(typeDouble, "float64, with the infinities and NaN from their strings"),
		row(typeFloat, "float64"),
		row(typeBoolean, "bool"),
		row(typeChar, "string, padded as the server sends it"),
		row(typeVarchar, "string"),
		row(typeBinary, "[]byte, from base64"),
		row(typeVarbinary, "[]byte, from base64"),
		row(typeDate, "dbimp.Date, from the days since 1970-01-01"),
		row(typeTime, "dbimp.LocalTime, from the milliseconds since midnight"),
		row(typeTimestamp, "dbimp.LocalDateTime, from the milliseconds since 1970-01-01 in UTC"),
		row(typeInterval, "dbimp.Interval, from the text of HSQLDB"),
		row(typeArray, "[]any, of the Go types of its elements"),
		row(typeUUID, "uuid.UUID, from its text"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping sends connectionSync with no change, which checks that the server knows the connection.",
		"driver.SessionResetter":                "database/sql ends a transaction before it reuses a connection, and the end sets autoCommit and the other properties back (D159).",
		"driver.Validator":                      "A connection that the server no longer knows is not valid, and database/sql closes it.",
		"driver.NamedValueChecker":              "It keeps an Option, a uint64, an *apd.Decimal, a uuid.UUID and the civil types of the root package, which the driver writes itself (D158).",
		"driver.QueryerContext":                 "A query sends prepareAndExecute, or prepare and execute with arguments, and reads its rows in frames (D157 and D158).",
		"driver.ExecerContext":                  "Exec runs as a query, and gives updateCount as the count of rows.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx sends connectionSync with autoCommit false, and with readOnly and transactionIsolation from the options (D159).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "The driver reads the first result only. HSQLDB gives the result of the last statement of a text, and Phoenix refuses a text of two.",
		"driver.RowsColumnTypeScanType":         "The signature names the type of each column, which gives the Go type (D155).",
		"driver.RowsColumnTypeDatabaseTypeName": "The signature names the type of each column, such as CHARACTER or INTEGER ARRAY.",
		"driver.RowsColumnTypeLength":           "The signature gives the precision of a text or a binary column, which is its length.",
		"driver.RowsColumnTypeNullable":         "The signature says whether each column can hold NULL, or that the server does not know.",
		"driver.RowsColumnTypePrecisionScale":   "The signature gives the precision and the scale of a DECIMAL.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a and D155).
var typeKinds = map[string]string{
	typeTinyint:      "integer",
	typeSmallint:     "integer",
	typeInteger:      "integer",
	typeBigint:       "integer",
	typeUnsignedInt:  "integer",
	typeUnsignedLong: "integer",
	typeDecimal:      "decimal",
	typeDouble:       "float",
	typeFloat:        "float",
	typeBoolean:      "boolean",
	typeChar:         "string",
	typeVarchar:      "string",
	typeBinary:       "binary",
	typeVarbinary:    "binary",
	typeDate:         "date",
	typeTime:         "time of day",
	typeTimestamp:    "local timestamp",
	typeInterval:     "interval",
	typeArray:        "array",
	typeUUID:         "uuid",
}
