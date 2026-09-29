package influxdb //nolint:testpackage // The tables read the wire types of the driver, which are not exported.

import (
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/INFLUXDB.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	var types []dbimptest.TypeRow
	for _, w := range wireTypes {
		// Every column that DESCRIBE names can hold a NULL, except time
		// (measured).
		ct := columnType(w.arrow, w.name != "timestamp")
		goes := "nil"
		if w.goes != nil {
			goes = w.goes.String()
		}
		types = append(types, dbimptest.TypeRow{
			Wire:         w.name,
			Go:           goes + " in SQL, from the data_type " + w.arrow + ". InfluxQL: " + w.influxql,
			ScanType:     ct.scanType().String(),
			DatabaseType: strings.ToUpper(w.arrow),
			Nullable:     ct.nullable,
		})
	}
	dbimptest.TypeTable(t, doc, types)
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares. Connect sends GET /ping for sqlmode prefer and require (D78).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1 or SHOW MEASUREMENTS, which checks the credentials and the database, as GET /ping does not on InfluxDB 1 and 2.",
		"driver.SessionResetter":                "A connection holds no state on the server, so nothing needs a reset.",
		"driver.Validator":                      "A connection holds no state on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps a uint64 and a decimal, which the default converter refuses or turns into text.",
		"driver.QueryerContext":                 "The server binds each argument itself, from params, except in INSERT, which the driver binds (D85).",
		"driver.ExecerContext":                  "Exec reads every result to its end. InfluxDB counts no rows.",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, bound each time.",
		"driver.ConnBeginTx":                    "BeginTx returns dbimp.ErrNotSupported, because InfluxDB has no transactions (D20).",
		"driver.RowsColumnScanner":              "A value is decoded when it is scanned, by its type in SQL (D80) and by its text in InfluxQL (D83).",
		"driver.RowsNextResultSet":              "Each series of InfluxQL is a result set (D81). SQL has one statement and one result.",
		"driver.RowsColumnTypeScanType":         "SQL gives the type from DESCRIBE (D80). InfluxQL names no types, so each is any.",
		"driver.RowsColumnTypeDatabaseTypeName": "SQL gives the data_type of DESCRIBE in upper case. InfluxQL names no types.",
		"driver.RowsColumnTypeLength":           "No column of InfluxDB has a length.",
		"driver.RowsColumnTypeNullable":         "SQL gives is_nullable of DESCRIBE. InfluxQL names no types.",
		"driver.RowsColumnTypePrecisionScale":   "SQL gives the precision and the scale of a decimal from DESCRIBE.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &sqlRows{}, &qlRows{}, &stmt{})
}
