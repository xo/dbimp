package surrealdb //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"reflect"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/SURREALDB.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	var types []dbimptest.TypeRow
	for _, w := range wireTypes {
		types = append(types, dbimptest.TypeRow{
			Wire:     w.name,
			Go:       w.goes,
			ScanType: reflect.TypeFor[any]().String(),
			Nullable: true,
		})
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, types, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping calls the RPC method ping, with the credentials that every request sends.",
		"driver.SessionResetter":                "A connection holds no state on the server, so nothing needs a reset.",
		"driver.Validator":                      "A connection holds no state, so it is always valid.",
		"driver.NamedValueChecker":              "An argument must have a name (D50), and it keeps its Go value for the encoding of D53.",
		"driver.QueryerContext":                 "The server binds each argument itself, through the vars of /rpc (D50).",
		"driver.ExecerContext":                  "Exec reads every result, and returns the first error (D55).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, bound each time.",
		"driver.ConnBeginTx":                    "BeginTx returns dbimp.ErrNotSupported for every option, because a transaction of SurrealDB lives in one request (D54).",
		"driver.RowsColumnScanner":              "A value is decoded when it is scanned, and a record id, a UUID and a duration scan into a string as their text.",
		"driver.RowsNextResultSet":              "Each statement of a request is a result set (D52).",
		"driver.RowsColumnTypeScanType":         "No type arrives for a column, and each record holds what it holds.",
		"driver.RowsColumnTypeDatabaseTypeName": "No type arrives for a column, and each record holds what it holds.",
		"driver.RowsColumnTypeLength":           "No type arrives for a column, so no column has a length.",
		"driver.RowsColumnTypeNullable":         "No type arrives for a column, and any field can be NONE or NULL.",
		"driver.RowsColumnTypePrecisionScale":   "No type arrives for a column, so no column has a precision or a scale.",
	}, NewConnector(Config{Host: "localhost", Port: defaultPort}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"none":     "null",
	"null":     "null",
	"bool":     "boolean",
	"int":      "integer",
	"float":    "float",
	"decimal":  "decimal",
	"string":   "string",
	"datetime": "timestamp",
	"duration": "duration",
	"uuid":     "uuid",
	"bytes":    "binary",
	"array":    "array",
	"set":      "set",
	"object":   "map",
	"record":   "record id",
	"geometry": "geometry",
	"range":    "range",
	"table":    "other",
}
