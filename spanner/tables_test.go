package spanner //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/SPANNER.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the type typ, with the Go type that a value has and
	// the name of the type in the survey, which is its code.
	row := func(typ wireType, goType string) dbimptest.TypeRow {
		r := &rows{types: []wireType{typ}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: typ.Code, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	array := wireType{Code: wireArray, Elem: &wireType{Code: wireInt64}}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row(wireType{Code: wireBool}, "bool"),
		row(wireType{Code: wireInt64}, "int64"),
		row(wireType{Code: wireFloat32}, "float64"),
		row(wireType{Code: wireFloat64}, "float64"),
		row(wireType{Code: wireNumeric}, "*apd.Decimal"),
		row(wireType{Code: wireString}, "string"),
		row(wireType{Code: wireBytes}, "[]byte"),
		row(wireType{Code: wireDate}, "dbimp.Date"),
		row(wireType{Code: wireTimestamp}, "time.Time"),
		row(wireType{Code: wireJSON}, "the decoded JSON value"),
		row(wireType{Code: wireUUID}, "uuid.UUID"),
		row(wireType{Code: wireInterval}, "dbimp.Interval"),
		row(array, "[]any"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, the token that the driver gets with the key file of the DSN, and the multiplexed session of each database (D191).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport. A multiplexed session cannot be deleted, so the server ends it.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the token, the session and the login, and costs little.",
		"driver.SessionResetter":                "A connection holds a transaction only, and database/sql ends it before it reuses the connection (D102). The session is the connector's, and it holds no state.",
		"driver.Validator":                      "A connection holds nothing on the server that can go bad. A session that the server loses is dropped by the connector, and the statement that found it returns driver.ErrBadConn.",
		"driver.NamedValueChecker":              "It keeps an Option, and the values that the driver binds with a type of its own: a decimal, a dbimp.Date, a dbimp.Interval, a UUID, a map for JSON, and a slice for an ARRAY (D191).",
		"driver.QueryerContext":                 "The statement goes to executeStreamingSql with its arguments as named parameters with their types (D191).",
		"driver.ExecerContext":                  "Exec reads the result to its end with no decoding, and RowsAffected is rowCountExact, or an error that wraps dbimp.ErrNotSupported when the answer has no count (D178).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx calls beginTransaction, for a read-write or a read-only transaction. An ABORTED answer returns ErrAborted (D191).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, because the server refuses several (recorded: \"two statements in one request\").",
		"driver.RowsColumnTypeScanType":         "The metadata names the type of each column, and each type has one Go type (D135 and D191).",
		"driver.RowsColumnTypeDatabaseTypeName": "The code of the type in upper case, such as INT64. An array is ARRAY.",
		"driver.RowsColumnTypeLength":           "The wire type carries no length, because the length of STRING(100) is in the catalog and not in the metadata.",
		"driver.RowsColumnTypeNullable":         "The metadata names no nullability, so every column can be NULL (D18).",
		"driver.RowsColumnTypePrecisionScale":   "A NUMERIC column has a precision of 38 and a scale of 9.",
	}, NewConnector(config()), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a and D191).
var typeKinds = map[string]string{
	"BOOL":      "boolean",
	"INT64":     "integer",
	"FLOAT32":   "float",
	"FLOAT64":   "float",
	"NUMERIC":   "decimal",
	"STRING":    "string",
	"BYTES":     "binary",
	"DATE":      "date",
	"TIMESTAMP": "timestamp",
	"JSON":      "json",
	"UUID":      "uuid",
	"INTERVAL":  "interval",
	"ARRAY":     "array",
}
