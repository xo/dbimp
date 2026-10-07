package elasticsearch //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/ELASTICSEARCH.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(wire, goType string) dbimptest.TypeRow {
		r := &rows{types: []string{wire}}
		nullable, _ := r.ColumnTypeNullable(0)
		if goType == "" {
			goType = scanType(wire).String()
		}
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("null", "nil"),
		row("boolean", "bool"),
		row("byte", "int64"),
		row("short", "int64"),
		row("integer", "int64, and a number with a fraction of zero, such as 100.0 from HISTOGRAM, as its integer"),
		row("long", "int64"),
		row("unsigned_long", "uint64, whatever its value (D138)"),
		row("half_float", "float64"),
		row("float", "float64"),
		row("double", "float64, with NaN and the infinities from their strings"),
		row("scaled_float", "float64, rounded by the scaling factor of the mapping"),
		row("keyword", "string"),
		row("text", "string"),
		row("binary", "[]byte, from base64"),
		row("ip", "string, because SQL names it VARCHAR"),
		row("version", "string, because SQL names it VARCHAR"),
		row("datetime", "time.Time, with the offset of time_zone and every digit of the second"),
		row("date", "dbimp.Date, from the date before the T"),
		row("time", "dbimp.OffsetTime, in milliseconds"),
		row("geo_point", "string, in WKT"),
		row("geo_shape", "string, in WKT"),
		row("shape", "string, in WKT"),
		row("interval_year", "dbimp.Interval, from the months of the ISO period"),
		row("interval_month", "dbimp.Interval, from the months of the ISO period"),
		row("interval_year_to_month", "dbimp.Interval, from the months of the ISO period"),
		row("interval_day", "time.Duration, from the ISO duration in hours"),
		row("interval_hour", "time.Duration, from the ISO duration in hours"),
		row("interval_minute", "time.Duration, from the ISO duration in hours"),
		row("interval_second", "time.Duration, from the ISO duration in hours"),
		row("interval_day_to_hour", "time.Duration, from the ISO duration in hours"),
		row("interval_day_to_minute", "time.Duration, from the ISO duration in hours"),
		row("interval_day_to_second", "time.Duration, from the ISO duration in hours"),
		row("interval_hour_to_minute", "time.Duration, from the ISO duration in hours"),
		row("interval_hour_to_second", "time.Duration, from the ISO duration in hours"),
		row("interval_minute_to_second", "time.Duration, from the ISO duration in hours"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the credentials. GET / needs the cluster privilege monitor, which the ordinary user lacks.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because the SQL API has no sessions.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option and a uint64, which the driver refuses above the range of int64 (D167), and hands every other value to database/sql.",
		"driver.QueryerContext":                 "The server binds each argument from the array params (D167).",
		"driver.ExecerContext":                  "Exec reads the result to its end. SQL takes no write, so RowsAffected fails (D163).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because Elasticsearch has no transactions (D167).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, and its pages are one result.",
		"driver.RowsColumnTypeScanType":         "The answer names the type of each column (D167).",
		"driver.RowsColumnTypeDatabaseTypeName": "The answer names the type of each column, which the driver writes in upper case, as SYS TYPES does.",
		"driver.RowsColumnTypeLength":           "The answer names no length.",
		"driver.RowsColumnTypeNullable":         "The answer does not say whether a column can be NULL, and every type can be.",
		"driver.RowsColumnTypePrecisionScale":   "The answer names no precision and no scale, and there is no decimal type.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"null":                      "null",
	"boolean":                   "boolean",
	"byte":                      "integer",
	"short":                     "integer",
	"integer":                   "integer",
	"long":                      "integer",
	"unsigned_long":             "unsigned integer",
	"half_float":                "float",
	"float":                     "float",
	"double":                    "float",
	"scaled_float":              "float",
	"keyword":                   "string",
	"text":                      "string",
	"binary":                    "binary",
	"ip":                        "string",
	"version":                   "string",
	"datetime":                  "timestamp",
	"date":                      "date",
	"time":                      "time of day with offset",
	"geo_point":                 "geometry",
	"geo_shape":                 "geometry",
	"shape":                     "geometry",
	"interval_year":             "interval",
	"interval_month":            "interval",
	"interval_year_to_month":    "interval",
	"interval_day":              "duration",
	"interval_hour":             "duration",
	"interval_minute":           "duration",
	"interval_second":           "duration",
	"interval_day_to_hour":      "duration",
	"interval_day_to_minute":    "duration",
	"interval_day_to_second":    "duration",
	"interval_hour_to_minute":   "duration",
	"interval_hour_to_second":   "duration",
	"interval_minute_to_second": "duration",
}
