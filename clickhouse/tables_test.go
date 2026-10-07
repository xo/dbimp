package clickhouse //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/CLICKHOUSE.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	// row makes the row of the type of the family. The wire name is the name of
	// the row in the table, which names the release of a type that one release
	// has.
	row := func(wire, family, goType string) dbimptest.TypeRow {
		r := &rows{cols: []*typ{{family: family, nullable: canBeNull(family)}}}
		nullable, _ := r.ColumnTypeNullable(0)
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("Nothing", "Nothing", "nil"),
		row("Bool", "Bool", "bool"),
		row("Int8", "Int8", "int64"),
		row("Int16", "Int16", "int64"),
		row("Int32", "Int32", "int64"),
		row("Int64", "Int64", "int64"),
		row("UInt8", "UInt8", "int64"),
		row("UInt16", "UInt16", "int64"),
		row("UInt32", "UInt32", "int64"),
		row("UInt64", "UInt64", "uint64"),
		row("Int128", "Int128", "*big.Int"),
		row("Int256", "Int256", "*big.Int"),
		row("UInt128", "UInt128", "*big.Int"),
		row("UInt256", "UInt256", "*big.Int"),
		row("Float32", "Float32", "float64"),
		row("Float64", "Float64", "float64"),
		row("BFloat16", "BFloat16", "float64"),
		row("Decimal", "Decimal", "*apd.Decimal"),
		row("String", "String", "string"),
		row("FixedString", "FixedString", "string"),
		row("Enum8", "Enum8", "string"),
		row("Enum16", "Enum16", "string"),
		row("Date", "Date", "dbimp.Date"),
		row("Date32", "Date32", "dbimp.Date"),
		row("DateTime", "DateTime", "time.Time"),
		row("DateTime64", "DateTime64", "time.Time"),
		row("Time", "Time", "time.Duration"),
		row("Time64", "Time64", "time.Duration"),
		row("Interval", "Interval", "dbimp.Interval"),
		row("UUID", "UUID", "uuid.UUID"),
		row("IPv4", "IPv4", "netip.Addr"),
		row("IPv6", "IPv6", "netip.Addr"),
		row("Array", "Array", "[]any"),
		row("Tuple", "Tuple", "[]any"),
		row("Map", "Map", "map[string]any"),
		row("Variant", "Variant", "the decoded JSON value"),
		row("Dynamic", "Dynamic", "the decoded JSON value"),
		row("JSON", "JSON", "the decoded JSON value"),
		row("Point", "Point", "[]any"),
		row("Ring", "Ring", "[]any"),
		row("Polygon", "Polygon", "[]any"),
		row("MultiPolygon", "MultiPolygon", "[]any"),
		row("LineString", "LineString", "[]any"),
		row("MultiLineString", "MultiLineString", "[]any"),
		row("26.9 MultiPoint", "MultiPoint", "[]any"),
		row("26.9 Geometry", "Geometry", "[]any"),
		row("AggregateFunction", "AggregateFunction", "[]byte"),
		row("26.9 QBit", "QBit", "dbimp.Vector[float32]"),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares, and each connection runs SELECT version() once (D177).",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping runs SELECT 1, which checks the user and the password. GET /ping needs neither.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because the driver sends no session, and the settings go with each request.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, and the values that the driver binds with a type of their own: a uint64, a decimal, a big integer, the types of the root package, a UUID, an address, a list and a map (D176).",
		"driver.QueryerContext":                 "The driver binds each argument as a typed parameter of the server, {pN:Type} in the statement and param_pN in the query string (D176).",
		"driver.ExecerContext":                  "Exec reads the result to its end, and RowsAffected fails, because the server counts no row that a statement changed (D176).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because the server answers every BEGIN with HTTP 501 (D177).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "The second line of the answer names the type of each column, with its arguments, and each type has one Go type (D135 and D177).",
		"driver.RowsColumnTypeDatabaseTypeName": "The type of a column in upper case, without its arguments and its wrappers, such as INT8 or DATETIME64.",
		"driver.RowsColumnTypeLength":           "A FixedString has its length in its type.",
		"driver.RowsColumnTypeNullable":         "A column can be NULL when its type is Nullable or holds a NULL of its own, and the type table says which types can.",
		"driver.RowsColumnTypePrecisionScale":   "A Decimal has its precision and its scale in its type.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a and D177).
var typeKinds = map[string]string{
	"Nothing":           "null",
	"Bool":              "boolean",
	"Int8":              "integer",
	"Int16":             "integer",
	"Int32":             "integer",
	"Int64":             "integer",
	"UInt8":             "integer",
	"UInt16":            "integer",
	"UInt32":            "integer",
	"UInt64":            "unsigned integer",
	"Int128":            "big integer",
	"Int256":            "big integer",
	"UInt128":           "big integer",
	"UInt256":           "big integer",
	"Float32":           "float",
	"Float64":           "float",
	"BFloat16":          "float",
	"Decimal":           "decimal",
	"String":            "string",
	"FixedString":       "string",
	"Enum8":             "string",
	"Enum16":            "string",
	"Date":              "date",
	"Date32":            "date",
	"DateTime":          "timestamp",
	"DateTime64":        "timestamp",
	"Time":              "duration",
	"Time64":            "duration",
	"Interval":          "interval",
	"UUID":              "uuid",
	"IPv4":              "ip address",
	"IPv6":              "ip address",
	"Array":             "array",
	"Tuple":             "tuple",
	"Map":               "map",
	"Variant":           "json",
	"Dynamic":           "json",
	"JSON":              "json",
	"Point":             "geometry",
	"Ring":              "geometry",
	"Polygon":           "geometry",
	"MultiPolygon":      "geometry",
	"LineString":        "geometry",
	"MultiLineString":   "geometry",
	"26.9 MultiPoint":   "geometry",
	"26.9 Geometry":     "geometry",
	"AggregateFunction": "binary",
	"26.9 QBit":         "vector",
}
