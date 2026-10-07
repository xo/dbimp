package solr_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/solr"
)

// rtConnector opens connections for dbimptest.RoundTrip. A query goes to the
// driver, as the principal of the test, and each write goes to the Schema API
// and to the update handler as the administrator, because the SQL of Solr
// takes none (D163). The statements of a write are these: create, with the
// name of a field and its field type, adds the field, drop removes it, insert
// and update with the name of the field write a document, and delete removes
// it. An update replaces the document. Each write commits, so the next query sees it.
type rtConnector struct {
	inner driver.Connector
	a     *api
}

func (rc *rtConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := rc.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &rtConn{Conn: conn, rc: rc}, nil
}

func (rc *rtConnector) Driver() driver.Driver { return rc.inner.Driver() }

// rtConn is a connection of rtConnector.
type rtConn struct {
	driver.Conn

	rc *rtConnector
}

// CheckNamedValue keeps every value as it is, so that a write gets the Go
// value of the test, and a query hands it to the driver.
func (c *rtConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *rtConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, fmt.Errorf("the connection runs no query: %w", dbimp.ErrNotSupported)
	}
	return q.QueryContext(ctx, query, args)
}

func (c *rtConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	words := strings.Fields(query)
	if len(words) < 1 {
		return nil, fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	field := ""
	if len(words) > 1 {
		field = words[1]
	}
	arg := func(i int) any { return args[i].Value }
	key := func(i int) string {
		s, _ := args[i].Value.(string)
		return s
	}
	a := c.rc.a
	var err error
	switch words[0] {
	case "create":
		// The type text_general is multi-valued by default, so the field
		// says which it is.
		f := map[string]any{"name": field, "type": words[2], "multiValued": len(words) > 3}
		err = a.schema(ctx, collMain, map[string]any{"add-field": []any{f}})
	case "drop":
		err = a.schema(ctx, collMain, map[string]any{"delete-field": map[string]any{"name": field}})
	case "insert":
		doc := map[string]any{"id": key(0)}
		if v := arg(1); v != nil {
			doc[field] = jsonValue(v)
		}
		err = a.update(ctx, collMain, []any{doc})
	case "update":
		// A document that replaces another keeps no field of it. An atomic
		// set of an empty string stores it, where an add drops it (recorded:
		// "every type"), so the update is a replace, as the insert is.
		doc := map[string]any{"id": key(1)}
		if v := arg(0); v != nil {
			doc[field] = jsonValue(v)
		}
		err = a.update(ctx, collMain, []any{doc})
	case "delete":
		err = a.update(ctx, collMain, map[string]any{"delete": map[string]any{"id": key(0)}})
	default:
		err = fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	return driver.ResultNoRows, err
}

// jsonValue returns the value of a round trip as the value that the update
// handler reads: a time as the text of a date in UTC, and a UUID as its text.
func jsonValue(v any) any {
	switch v := v.(type) {
	case time.Time:
		return v.UTC().Format("2006-01-02T15:04:05.000Z")
	case uuid.UUID:
		return v.String()
	}
	return v
}

// noLiteral is the Literal of every round trip: the SQL of Solr takes no
// insert, so a value has no literal of one (D163). The literals of a query are
// held in TestIntegrationArguments.
func noLiteral(string, any) (string, error) {
	return "", fmt.Errorf("the SQL of Solr takes no insert, so a value has no literal of one: %w", dbimp.ErrNotSupported)
}

// absent is the value that a round trip wants for an empty string, an empty
// array and empty bytes: the update handler drops each of them, so a read gives
// NULL (recorded: "every type").
type absent struct{}

// rtType is the round trip of one type: the field type of its field, whether
// the field is multi-valued, the expression of the select over the field v,
// and its values.
type rtType struct {
	typ    string
	ftype  string
	multi  bool
	expr   string
	values []dbimptest.Value
	equal  func(got, want any) bool
	// group is true for an aggregate, whose select groups by the id, which is
	// the first column.
	group bool
	// filter is a condition that the select adds to the id, and order the
	// clause that follows it. The select of a score has both, because the
	// server sorts a result that reads the score by the score.
	filter string
	order  string
}

// roundTrip returns the round trip of rt for the principal p, in a field of
// its own.
func roundTrip(p principal, rt rtType) dbimptest.RoundTripCase {
	field := "rt_" + p.name[:3] + "_" + strings.Map(func(r rune) rune {
		switch {
		case 'A' <= r && r <= 'Z':
			return r + 'a' - 'A'
		case 'a' <= r && r <= 'z', '0' <= r && r <= '9':
			return r
		}
		return '_'
	}, rt.typ)
	expr := strings.ReplaceAll(rt.expr, "$", field)
	if expr == "" {
		expr = field
	}
	filter := strings.ReplaceAll(rt.filter, "$", field)
	sel := "SELECT " + expr + " AS v FROM " + collMain + " WHERE id = ?" + filter + rt.order + " LIMIT 1"
	if rt.order != "" {
		// The sort spec must name a field of the list, so the list holds the
		// score and not an alias of it.
		sel = "SELECT " + expr + " FROM " + collMain + " WHERE id = ?" + filter + rt.order + " LIMIT 1"
	}
	column := 0
	if rt.group {
		sel = "SELECT id, " + expr + " AS v FROM " + collMain + " WHERE id = ?" + filter + " GROUP BY id"
		column = 1
	}
	create := "create " + field + " " + rt.ftype
	if rt.multi {
		create += " multi"
	}
	equal := rt.equal
	if equal == nil {
		equal = func(got, want any) bool {
			if _, ok := want.(absent); ok {
				return got == nil
			}
			return reflect.DeepEqual(got, want)
		}
	}
	return dbimptest.RoundTripCase{
		Type:     rt.typ,
		Setup:    []string{create},
		Teardown: []string{"drop " + field},
		Insert:   "insert " + field,
		Literal:  noLiteral,
		Select:   sel,
		Update:   "update " + field,
		Delete:   "delete",
		Column:   column,
		Values:   rt.values,
		Wait:     visible,
		Equal:    equal,
	}
}

// rtTypes returns the round trip of each type that features.json marks yes.
func rtTypes() []rtType {
	val := func(name string, in any) dbimptest.Value { return dbimptest.Value{Name: name, In: in} }
	null := dbimptest.Value{Name: "null"}
	empty := func(in any) dbimptest.Value { return dbimptest.Value{Name: "empty", In: in, Want: absent{}} }
	long := strings.Repeat("é", 512)
	ts := utcTime
	anyScore := func(got, _ any) bool {
		f, ok := got.(float64)
		return ok && f > 0
	}
	return []rtType{
		{typ: "null", ftype: "string", values: []dbimptest.Value{null, {Name: "null again"}}},
		{typ: "StrField", ftype: "string", values: []dbimptest.Value{null, empty(""), val("unicode", "é'\"\\ x ☃ 日本 🙂"), val("long", long)}},
		{typ: "TextField", ftype: "text_general", values: []dbimptest.Value{null, empty(""), val("unicode", "the quick brown fox 日本 🙂"), val("long", long)}},
		{typ: "SortableTextField", ftype: "sortabletext", values: []dbimptest.Value{null, empty(""), val("unicode", "Sortable é 日本 🙂")}},
		{typ: "IntPointField", ftype: "pint", values: []dbimptest.Value{null, val("zero", int64(0)), val("min", int64(math.MinInt32)), val("max", int64(math.MaxInt32))}},
		{typ: "LongPointField", ftype: "plong", values: []dbimptest.Value{null, val("zero", int64(0)), val("min", int64(math.MinInt64)), val("max", int64(math.MaxInt64))}},
		// A float holds a float32, and the server writes its shortest text
		// (measured), which these values keep.
		{typ: "FloatPointField", ftype: "pfloat", values: []dbimptest.Value{null, val("zero", 0.0), val("step", 0.1), val("max", 3.4028235e38), val("negative", -1.5)}},
		{typ: "DoublePointField", ftype: "pdouble", values: []dbimptest.Value{null, val("zero", 0.0), val("step", 0.1), val("max", math.MaxFloat64), val("whole", 2.0)}},
		{typ: "BoolField", ftype: "boolean", values: []dbimptest.Value{null, val("true", true), val("false", false)}},
		{typ: "DatePointField", ftype: "pdate", values: []dbimptest.Value{
			null, val("milliseconds", ts("2026-10-01T12:34:56.789Z")),
			{Name: "zone", In: ts("2022-04-15T05:20:00.001Z").In(time.FixedZone("x", -7*3600)), Want: ts("2022-04-15T05:20:00.001Z")},
			val("before 1970", ts("1900-01-01T00:00:00Z")), val("first", ts("0001-01-01T00:00:00Z")), val("last", ts("9999-12-31T23:59:59.999Z"))}},
		{typ: "BinaryField", ftype: "binary", values: []dbimptest.Value{null, empty([]byte{}), val("bytes", []byte{0, 1, 2, 0xff}), val("long", []byte(strings.Repeat("\x00\xff", 2048)))}},
		{typ: "UUIDField", ftype: "uuid", values: []dbimptest.Value{null, val("uuid", uuid.MustParse("8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b")), val("zero", uuid.UUID{})}},
		{typ: "LatLonPointSpatialField", ftype: "location", values: []dbimptest.Value{null, val("point", "45.5,-122.6"), val("origin", "0,0")}},
		{typ: "SpatialRecursivePrefixTreeFieldType", ftype: "location_rpt", values: []dbimptest.Value{null, val("point", "POINT(-122.6 45.5)"), val("origin", "POINT(0 0)")}},
		{typ: "BBoxField", ftype: "bbox", values: []dbimptest.Value{null, val("box", "ENVELOPE(-10, 20, 15, 10)")}},
		{typ: "PointType", ftype: "point", values: []dbimptest.Value{null, val("point", "1.5,2.5"), val("negative", "-1.5,-2.5")}},
		// The select handler keeps the order of the values of a document.
		{typ: "multi-valued field", ftype: "pints", multi: true, values: []dbimptest.Value{
			null, empty([]int64{}), {Name: "ints", In: []int64{3, 1, 2}, Want: []any{int64(3), int64(1), int64(2)}},
			{Name: "limits", In: []int64{math.MaxInt32, math.MinInt32}, Want: []any{int64(math.MaxInt32), int64(math.MinInt32)}}}},
		{typ: "multi-valued field of strings", ftype: "strings", multi: true, values: []dbimptest.Value{
			null, {Name: "strings", In: []string{"b", "a", "é"}, Want: []any{"b", "a", "é"}}, {Name: "one", In: []string{"x"}, Want: []any{"x"}}}},
		{typ: "multi-valued field of booleans", ftype: "booleans", multi: true, values: []dbimptest.Value{
			null, {Name: "booleans", In: []bool{true, false}, Want: []any{true, false}}}},
		// An aggregate names no type, so its value reads by its JSON token.
		{typ: "aggregate", ftype: "plong", expr: "sum($)", group: true, values: []dbimptest.Value{
			val("zero", int64(0)), val("negative", int64(-7)), val("max", int64(math.MaxInt32))}},
		{typ: "score", ftype: "text_general", expr: "score", filter: " AND $ = 'quick'", order: " ORDER BY score DESC", equal: anyScore, values: []dbimptest.Value{
			val("one word", "quick"), val("two words", "quick brown"), val("a sentence", "the quick fox")}},
	}
}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type that
// features.json marks yes, as each principal. The administrator writes each
// value through the update handler, and the principal reads it through the
// driver. It also holds each type that features.json marks no.
func TestIntegrationRoundTrip(t *testing.T) {
	a := apiAs(t, admin)
	for _, rt := range rtTypes() {
		t.Run(rt.typ, func(t *testing.T) {
			for _, p := range principals {
				t.Run(p.name, func(t *testing.T) {
					connector, err := solr.Driver{}.OpenConnector(dsn(t, p, collMain))
					if err != nil {
						t.Fatal(err)
					}
					rdb := sql.OpenDB(&rtConnector{inner: connector, a: a})
					t.Cleanup(func() { rdb.Close() })
					c := roundTrip(p, rt)
					dbimptest.RoundTrip(t, rdb, c)
				})
			}
		})
	}
	// The types that the SQL layer cannot read fail the whole query, after the
	// rows before the value (recorded: "a vector" and "a date range"), and the
	// types that the schema cannot add fail at the Schema API.
	for _, tt := range []struct{ typ, field string }{{"DenseVectorField", "n_v"}, {"DateRangeField", "n_dr"}} {
		t.Run(tt.typ, func(t *testing.T) {
			forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
				_, rows, err := tryQuery(t.Context(), db, "SELECT id, "+tt.field+" FROM "+collMain+" ORDER BY id LIMIT 3")
				if len(rows) != 2 || !isIncomplete(err) {
					t.Errorf("read %d rows and %v, want 2 rows and an error that wraps ErrIncomplete", len(rows), err)
				}
			})
		})
	}
	t.Run("EnumFieldType", func(t *testing.T) {
		status, body, err := a.do(t.Context(), http.MethodPost, "/solr/"+collMain+"/schema", map[string]any{"add-field-type": map[string]any{
			"name": "enum", "class": "solr.EnumFieldType", "enumsConfig": "enumsConfig.xml", "enumName": "severity"}})
		if err != nil || status != http.StatusBadRequest {
			t.Errorf("the Schema API gave HTTP %d %s and %v for an enum with no configuration file, want HTTP 400", status, body, err)
		}
	})
	// The currency type breaks the collection that it fails in (recorded: "add
	// a currency type with no configuration file"), so the test makes a
	// collection of its own.
	t.Run("CurrencyFieldType", func(t *testing.T) {
		name := prefix + "_scratch"
		if err := a.makeCollection(t.Context(), name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := a.dropCollection(context.WithoutCancel(t.Context()), name); err != nil {
				t.Errorf("removing the scratch collection: %v", err)
			}
		})
		status, body, err := a.do(t.Context(), http.MethodPost, "/solr/"+name+"/schema", map[string]any{"add-field-type": map[string]any{
			"name": "currency", "class": "solr.CurrencyFieldType", "amountLongSuffix": "_l", "codeStrSuffix": "_s", "defaultCurrency": "USD", "currencyConfig": "currency.xml"}})
		if err != nil || status != http.StatusInternalServerError {
			t.Errorf("the Schema API gave HTTP %d %s and %v for a currency with no configuration file, want HTTP 500", status, body, err)
		}
	})
}
