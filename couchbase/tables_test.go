package couchbase //nolint:testpackage // The tables read the kinds of the driver, which are not exported.

import (
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/COUCHBASE.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	r := &rows{}
	var types []dbimptest.TypeRow
	for i, k := range kinds {
		r.kinds = append(r.kinds, k.name)
		types = append(types, dbimptest.TypeRow{
			Wire:         k.name,
			Go:           k.goes,
			ScanType:     r.ColumnTypeScanType(i).String(),
			DatabaseType: r.ColumnTypeDatabaseTypeName(i),
			Nullable:     true,
		})
	}
	types = append(types, dbimptest.TypeRow{
		Wire:         "bytes as a base64 string",
		Go:           "[]byte, decoded from base64 (D44)",
		ScanType:     "string",
		DatabaseType: strings.ToUpper("string"),
		Nullable:     true,
	})
	dbimptest.TypeTable(t, doc, types)
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT RAW 1, because /admin/ping needs no credentials, so it would not check them.",
		"driver.SessionResetter":                "ResetSession rolls back a transaction left open (D41).",
		"driver.Validator":                      "A connection holds no state on the server outside a transaction.",
		"driver.NamedValueChecker":              "An argument is any value that json/v2 encodes, and an Option is taken out (D40).",
		"driver.QueryerContext":                 "The query service binds each argument itself.",
		"driver.ExecerContext":                  "A write returns metrics.mutationCount as its rows affected.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, bound each time.",
		"driver.ConnBeginTx":                    "A transaction of the query service, with a txid (D41).",
		"driver.RowsColumnScanner":              "A value is decoded as it is scanned, and a byte slice gets base64 decoded (D44).",
		"driver.RowsNextResultSet":              "A request holds one statement, so a response has one result.",
		"driver.RowsColumnTypeScanType":         "From the kind that the signature names.",
		"driver.RowsColumnTypeDatabaseTypeName": "The kind that the signature names, in upper case.",
		"driver.RowsColumnTypeLength":           "A document has no schema, so no column has a length.",
		"driver.RowsColumnTypeNullable":         "Every column can be NULL or MISSING, because a document has no schema.",
		"driver.RowsColumnTypePrecisionScale":   "A number is a JSON number, with no precision or scale.",
	}, NewConnector(Config{Host: "localhost", Port: portHTTP}), Driver{}, &conn{}, r, &tx{}, &stmt{})
}
