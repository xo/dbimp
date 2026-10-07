package solr //nolint:testpackage // The tables read the types of the driver, which are not exported.

import (
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// doc is the product document of the driver.
const doc = "../docs/SOLR.md"

// TestTables compares the type table and the interface table of step 10 of
// docs/DRIVER.md in doc with the code. Run it with DBIMP_UPDATE=1 to write
// them.
func TestTables(t *testing.T) {
	t.Parallel()
	row := func(wire string, c column, goType string) dbimptest.TypeRow {
		r := &rows{types: []column{c}}
		nullable, _ := r.ColumnTypeNullable(0)
		if goType == "" {
			goType = scanType(c).String()
		}
		return dbimptest.TypeRow{Wire: wire, Go: goType, ScanType: r.ColumnTypeScanType(0).String(), DatabaseType: r.ColumnTypeDatabaseTypeName(0), Nullable: nullable}
	}
	varchar := func(class string) column { return column{sql: typeVarchar, class: class} }
	dbimptest.TypeTable(t, doc, dbimptest.Kinds(t, []dbimptest.TypeRow{
		row("null", column{}, "nil"),
		row("StrField", varchar("StrField"), ""),
		row("TextField", varchar("TextField"), "string, the stored text"),
		row("SortableTextField", varchar("SortableTextField"), "string, the stored text"),
		row("IntPointField", column{sql: typeBigint, class: "IntPointField"}, ""),
		row("LongPointField", column{sql: typeBigint, class: "LongPointField"}, ""),
		row("FloatPointField", column{sql: typeDouble, class: "FloatPointField"}, "float64, from the text of the float32"),
		row("DoublePointField", column{sql: typeDouble, class: "DoublePointField"}, ""),
		row("BoolField", varchar(classBool), `bool, from the text "true" or "false", by the field type of the luke handler (D166)`),
		row("DatePointField", column{sql: typeTimestamp, class: "DatePointField"}, "time.Time, in UTC, with milliseconds"),
		row("BinaryField", varchar(classBinary), "[]byte, from the base64 text, by the field type of the luke handler (D166)"),
		row("UUIDField", varchar(classUUID), "uuid.UUID, from its text, by the field type of the luke handler (D166)"),
		row("LatLonPointSpatialField", varchar("LatLonPointSpatialField"), `string, such as "45.5,-122.6"`),
		row("SpatialRecursivePrefixTreeFieldType", varchar("SpatialRecursivePrefixTreeFieldType"), "string, the WKT text"),
		row("BBoxField", varchar("BBoxField"), `string, such as "ENVELOPE(-10, 20, 15, 10)"`),
		row("PointType", varchar("PointType"), `string, such as "1.5,2.5"`),
		row("multi-valued field", column{sql: typeAny}, "[]any, of the JSON values of its elements"),
		row("aggregate", column{}, "int64, float64, or *apd.Decimal for an integer too large for int64"),
		row("score", column{sql: typeDouble}, ""),
	}, typeKinds))
	dbimptest.InterfaceTable(t, doc, map[string]string{
		"driver.DriverContext":                  "OpenConnector parses the DSN once, for every connection.",
		"driver.Connector":                      "The connector owns the transport, which every connection shares.",
		"io.Closer on the connector":            "Close closes the idle connections of the transport.",
		"driver.Pinger":                         "Ping counts the documents of the collection of the DSN, which checks the credentials and the handler of SQL. The ordinary user cannot read the version.",
		"driver.SessionResetter":                "A connection holds nothing on the server, because the SQL of Solr has no sessions.",
		"driver.Validator":                      "A connection holds nothing on the server, so it is always valid.",
		"driver.NamedValueChecker":              "It keeps an Option, a decimal, a dbimp.Date, a dbimp.LocalDateTime and a uuid.UUID, which the driver writes as literals (D166).",
		"driver.QueryerContext":                 "The server binds no argument, so the driver writes each one into the statement as a literal (D166).",
		"driver.ExecerContext":                  "Exec reads the result to its end. The SQL of Solr takes no write, so RowsAffected fails (D163).",
		"driver.ConnPrepareContext":             "A prepared statement runs as its text, with its arguments, each time.",
		"driver.ConnBeginTx":                    "BeginTx fails with dbimp.ErrNotSupported, because Solr has no transactions (D166).",
		"driver.RowsColumnScanner":              "A value is decoded when its row is read, and assigned when it is scanned.",
		"driver.RowsNextResultSet":              "A request holds one statement, so an answer has one result.",
		"driver.RowsColumnTypeScanType":         "The driver reads the type of each column from metadata.COLUMNS and the luke handler (D166).",
		"driver.RowsColumnTypeDatabaseTypeName": "The SQL type of metadata.COLUMNS, such as BIGINT, and empty for a column that it does not name, such as an aggregate.",
		"driver.RowsColumnTypeLength":           "The server names no length.",
		"driver.RowsColumnTypeNullable":         "The server does not say whether a column can be NULL, and every type can be.",
		"driver.RowsColumnTypePrecisionScale":   "The server names no precision and no scale.",
	}, NewConnector(Config{Host: "localhost"}), Driver{}, &conn{}, &rows{}, &stmt{})
}

// typeKinds are the kinds of docs/TYPES.md of the types of the driver (step
// 8a).
var typeKinds = map[string]string{
	"null":                                "null",
	"StrField":                            "string",
	"TextField":                           "string",
	"SortableTextField":                   "string",
	"IntPointField":                       "integer",
	"LongPointField":                      "integer",
	"FloatPointField":                     "float",
	"DoublePointField":                    "float",
	"BoolField":                           "boolean",
	"DatePointField":                      "timestamp",
	"BinaryField":                         "binary",
	"UUIDField":                           "uuid",
	"LatLonPointSpatialField":             "string",
	"SpatialRecursivePrefixTreeFieldType": "string",
	"BBoxField":                           "string",
	"PointType":                           "string",
	"multi-valued field":                  "array",
	"aggregate":                           "number",
	"score":                               "float",
}
