package opensearch_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/opensearch"
)

// rtConnector opens connections for dbimptest.RoundTrip. A query goes to the
// driver, as the principal of the test, with the options of the case, and each
// write goes to the document API as the administrator, because SQL takes none
// (D163). The statements of a write are these: create and drop with the name
// of the index, and insert, update and delete with the name of the index.
// create takes the type of the round trip, which names the mapping of the field
// v. Each write refreshes the index, so a query sees it at once.
type rtConnector struct {
	inner driver.Connector
	s     *api
	opts  []opensearch.Option
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

// CheckNamedValue keeps every value as it is, so that a write gets the Go value
// of the test, and a query hands it to the driver.
func (c *rtConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *rtConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, fmt.Errorf("the connection runs no query: %w", dbimp.ErrNotSupported)
	}
	for _, opt := range c.rc.opts {
		args = append(args, driver.NamedValue{Ordinal: len(args) + 1, Value: opt})
	}
	return q.QueryContext(ctx, query, args)
}

func (c *rtConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	words := strings.Fields(query)
	if len(words) < 2 {
		return nil, fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	index := words[1]
	key := func(i int) string {
		s, _ := args[i].Value.(string)
		return s
	}
	var (
		status int
		b      []byte
		err    error
	)
	switch words[0] {
	case "create":
		typ := strings.Join(words[2:], " ")
		mapping, ok := rtMappings[typ]
		if !ok {
			return nil, fmt.Errorf("the statement %q: no mapping for %s: %w", query, typ, dbimp.ErrInvalidValue)
		}
		status, b, err = c.rc.s.do(ctx, http.MethodPut, "/"+index, map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{"k": m("keyword"), "v": mapping}}})
	case "drop":
		err = c.rc.s.drop(ctx, index)
		status = http.StatusOK
	case "insert":
		status, b, err = c.put(ctx, index, key(0), args[1].Value)
	case "update":
		status, b, err = c.put(ctx, index, key(1), args[0].Value)
	case "delete":
		status, b, err = c.rc.s.do(ctx, http.MethodDelete, "/"+index+"/_doc/"+url.PathEscape(key(0))+"?refresh=true", nil)
	default:
		err = fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	if err == nil && status >= 300 {
		err = fmt.Errorf("%q gave HTTP %d: %s", query, status, b)
	}
	return driver.ResultNoRows, err
}

// put writes the document of key with the value v, which replaces the whole
// document, and waits for the refresh.
func (c *rtConn) put(ctx context.Context, index, key string, v any) (int, []byte, error) {
	return c.rc.s.do(ctx, http.MethodPut, "/"+index+"/_doc/"+url.PathEscape(key)+"?refresh=true", map[string]any{"k": key, "v": storeValue(v)})
}

// storeValue returns the JSON value that stores v in the field v. A time is text
// in ISO 8601, a date is the text of its day, a time of day is its text, and
// bytes are base64.
func storeValue(v any) any {
	switch v := v.(type) {
	case []byte:
		return base64.StdEncoding.EncodeToString(v)
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case dbimp.Date:
		return v.String()
	case dbimp.LocalTime:
		return v.String()
	}
	return v
}

// rtMappings are the mapping of the field v of each type of the round trip. The
// type undefined has no field, and the select makes it, and a range is a field
// that only the legacy engine reads.
var rtMappings = map[string]map[string]any{
	"undefined":     m("keyword"),
	"boolean":       m("boolean"),
	"byte":          m("byte"),
	"short":         m("short"),
	"integer":       m("integer"),
	"long":          m("long"),
	"float":         m("float"),
	"double":        m("double"),
	"half_float":    m("half_float"),
	"scaled_float":  {"type": "scaled_float", "scaling_factor": 100},
	"keyword":       m("keyword"),
	"text":          m("text"),
	"ip":            m("ip"),
	"binary":        m("binary"),
	"date":          {"type": "date", "format": "yyyy-MM-dd"},
	"time":          {"type": "date", "format": "HH:mm:ss"},
	"timestamp":     m("date"),
	"datetime":      m("date_nanos"),
	"geo_point":     m("geo_point"),
	"object":        {"type": "object", "properties": map[string]any{"a": m("integer"), "b": m("keyword")}},
	"nested":        {"type": "nested", "properties": map[string]any{"a": m("integer"), "b": m("keyword")}},
	"integer_range": m("integer_range"),
}

// rtType is the round trip of one type: the expression of the select over the
// stored field v, the options of the query, and the values.
type rtType struct {
	typ    string
	expr   string
	legacy bool
	values []dbimptest.Value
	equal  func(got, want any) bool
}

// longText is a value of 5000 characters of several widths.
var longText = strings.Repeat("aé日😀", 1250)

// sameTime compares two times by their instant and their zone, which is UTC.
func sameTime(got, want any) bool {
	g, ok1 := got.(time.Time)
	w, ok2 := want.(time.Time)
	if !ok1 || !ok2 {
		return reflect.DeepEqual(got, want)
	}
	return g.Equal(w) && g.Location() == time.UTC
}

// clock returns the time of day h:m:s.
func clock(h, m, s int) dbimp.LocalTime {
	return dbimp.LocalTime{Hour: h, Minute: m, Second: s}
}

// rtTypes returns the round trip of each type that features.json marks yes.
func rtTypes() []rtType {
	val := func(name string, in any) dbimptest.Value { return dbimptest.Value{Name: name, In: in} }
	null := val("null", nil)
	ts := func(s string) time.Time {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			panic(err)
		}
		return t.UTC()
	}
	obj := func(a int64, b string) map[string]any { return map[string]any{"a": a, "b": b} }
	rng := func(lo, hi int64) map[string]any { return map[string]any{"gte": lo, "lte": hi} }
	return []rtType{
		{typ: "undefined", expr: "NULL", values: []dbimptest.Value{null, val("null again", nil)}},
		{typ: "boolean", expr: "v", values: []dbimptest.Value{null, val("false", false), val("true", true)}},
		{typ: "byte", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(-128)}, {Name: "max", In: int64(127)}}},
		{typ: "short", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(-32768)}, {Name: "max", In: int64(32767)}}},
		{typ: "integer", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(math.MinInt32)}, {Name: "max", In: int64(math.MaxInt32)}}},
		{typ: "long", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(math.MinInt64)},
			{Name: "max", In: int64(math.MaxInt64)}, {Name: "last digit", In: int64(math.MaxInt64 - 1)}}},
		{typ: "float", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "max", In: 3.4028235e38}, {Name: "min", In: -3.4028235e38},
			{Name: "smallest", In: 1.4e-45}, {Name: "fraction", In: 1.5}}},
		{typ: "double", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "max", In: math.MaxFloat64}, {Name: "min", In: -math.MaxFloat64},
			{Name: "smallest", In: math.SmallestNonzeroFloat64}, {Name: "tenth", In: 0.1}, {Name: "last digit", In: math.Nextafter(1, 2)}}},
		{typ: "half_float", expr: "v", legacy: true, values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "max", In: 65504.0}, {Name: "min", In: -65504.0},
			{Name: "half", In: 0.5}, {Name: "quarter", In: 0.25}}},
		{typ: "scaled_float", expr: "v", legacy: true, values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "fraction", In: 12.345},
			{Name: "negative", In: -1234.57}, {Name: "large", In: 1234567890123.45}}},
		{typ: "keyword", expr: "v", values: []dbimptest.Value{null, val("empty", ""), val("unicode", "é'\"\\ x 日本語 😀"), val("long", longText)}},
		{typ: "text", expr: "v", values: []dbimptest.Value{null, val("empty", ""), val("unicode", "é'\"\\ x 日本語 😀"), val("long", longText)}},
		{typ: "binary", expr: "v", values: []dbimptest.Value{null, val("empty", []byte{}), val("bytes", []byte{0, 255}), val("long", []byte(longText))}},
		{typ: "ip", expr: "v", values: []dbimptest.Value{null, val("zero", "0.0.0.0"), val("max", "255.255.255.255"), val("v6", "2001:db8::1"), val("loopback", "::1")}},
		{typ: "date", expr: "v", values: []dbimptest.Value{null, val("first day", dbimp.Date{Year: 1, Month: 1, Day: 1}),
			val("last day", dbimp.Date{Year: 9999, Month: 12, Day: 31}), val("day", dbimp.Date{Year: 2026, Month: 10, Day: 1}), val("epoch", dbimp.Date{Year: 1970, Month: 1, Day: 1})}},
		{typ: "time", expr: "v", values: []dbimptest.Value{null, val("midnight", clock(0, 0, 0)), val("last second", clock(23, 59, 59)), val("time", clock(12, 34, 56)), val("first second", clock(0, 0, 1))}},
		{typ: "timestamp", expr: "v", equal: sameTime, values: []dbimptest.Value{null, val("epoch", ts("1970-01-01T00:00:00Z")),
			val("milliseconds", ts("2026-10-01T12:34:56.123Z")), val("last millisecond", ts("2026-10-01T12:34:56.001Z")), val("last day", ts("9999-12-31T23:59:59.999Z"))}},
		{typ: "datetime", expr: "v", equal: sameTime, values: []dbimptest.Value{null, val("epoch", ts("1970-01-01T00:00:00Z")),
			val("nanoseconds", ts("2026-10-01T12:34:56.123456789Z")), val("last digit", ts("2026-10-01T12:34:56.000000001Z")), val("max", ts("2262-04-11T23:47:16.854775807Z"))}},
		{typ: "geo_point", expr: "v", values: []dbimptest.Value{null, val("origin", map[string]any{"lat": 0.0, "lon": 0.0}), val("point", map[string]any{"lat": 41.12, "lon": -71.34}),
			val("edge", map[string]any{"lat": 89.5, "lon": 179.5})}},
		{typ: "object", expr: "v", values: []dbimptest.Value{null, val("object", obj(1, "x")), val("unicode", obj(-2147483648, "é'\"\\ x 日本語 😀")), val("max", obj(2147483647, ""))}},
		{typ: "nested", expr: "v", values: []dbimptest.Value{null, val("one", []any{obj(1, "x")}), val("two", []any{obj(1, "x"), obj(2, "y")}), val("three", []any{obj(3, "z"), obj(-1, "é"), obj(0, "")})}},
		{typ: "integer_range", expr: "v", legacy: true, values: []dbimptest.Value{null, val("range", rng(1, 5)), val("zero", rng(0, 0)), val("whole", rng(math.MinInt32, math.MaxInt32))}},
	}
}

// roundTrip returns the round trip of rt for the principal p, in an index of
// its own.
func roundTrip(p principal, rt rtType) dbimptest.RoundTripCase {
	index := prefix + "rt_" + p.name[:3] + "_" + strings.ReplaceAll(rt.typ, "_", "")
	return dbimptest.RoundTripCase{
		Type:     rt.typ,
		Setup:    []string{"create " + index + " " + rt.typ},
		Teardown: []string{"drop " + index},
		Insert:   "insert " + index,
		Literal:  noLiteral,
		Select:   "SELECT " + rt.expr + " AS v FROM " + index + " WHERE k = ?",
		Update:   "update " + index,
		Delete:   "delete " + index,
		Values:   rt.values,
		Equal:    rt.equal,
	}
}

// noLiteral is the Literal of every round trip: SQL takes no insert, so a value
// has no literal of one (D163). The literals of a query are held in
// TestIntegrationLiterals.
func noLiteral(string, any) (string, error) {
	return "", fmt.Errorf("SQL in OpenSearch takes no insert, so a value has no literal of one: %w", dbimp.ErrNotSupported)
}

// matchAll is the filter that sends a statement to the legacy engine, which
// names the types half_float, scaled_float and integer_range (recorded: "every
// type on the legacy engine").
var matchAll = map[string]any{"match_all": map[string]any{}}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type that
// features.json marks yes, as each principal. The administrator writes each
// value through the document API, and the principal reads it through the driver.
// A type that only the legacy engine names, which a filter selects, runs with a
// filter, and the ordinary user of the 2 series cannot use one. The types that
// the entries mark no are refused, and the test holds each refusal.
func TestIntegrationRoundTrip(t *testing.T) {
	s := newAdminAPI(t)
	forEach(t, func(t *testing.T, e *target) {
		for _, rt := range rtTypes() {
			t.Run(rt.typ, func(t *testing.T) {
				var opts []opensearch.Option
				if e.cannotPage() {
					opts = append(opts, opensearch.WithParameter("fetch_size", 0))
				}
				if rt.legacy {
					if e.old && e.p == ordinary {
						t.Skip("on the 2 series the ordinary user cannot use a filter, which names this type (recorded: a statement with a filter of the query DSL)")
					}
					opts = append(opts, opensearch.WithParameter("filter", matchAll))
				}
				connector, err := opensearch.Driver{}.OpenConnector(dsn(t, e.p))
				if err != nil {
					t.Fatal(err)
				}
				rdb := sql.OpenDB(&rtConnector{inner: connector, s: s, opts: opts})
				t.Cleanup(func() { rdb.Close() })
				dbimptest.RoundTrip(t, rdb, roundTrip(e.p, rt))
			})
		}
		refusedTypes(t, e, s)
	})
}

// refusedType is a type that the entries of features.json mark no, with the
// mapping of its field and a document that holds it.
type refusedType struct {
	typ     string
	mapping map[string]any
	doc     any
}

// refusedTypes holds the entries of features.json that mark a type no. SQL
// leaves a field of such a type out of SELECT *, and naming it fails with
// SemanticCheckException. A type that OpenSearch has not is refused by the
// mapping of the document API, and a type that a release lacks is refused by the
// mapping on that release (recorded: "a field of unsigned_long", "select a
// token_count", "make an index with one version").
func refusedTypes(t *testing.T, e *target, s *api) {
	t.Helper()
	for _, tt := range []refusedType{
		{"unsigned_long", m("unsigned_long"), jsontext.Value(`18446744073709551615`)},
		{"wildcard", m("wildcard"), "wild*card"},
		{"geo_shape", m("geo_shape"), map[string]any{"type": "point", "coordinates": []any{-71.34, 41.12}}},
		{"flat_object", m("flat_object"), map[string]any{"p": map[string]any{"q": "r"}}},
		{"token_count", map[string]any{"type": "token_count", "analyzer": "standard"}, "a b c"},
		{"completion", m("completion"), "x"},
		{"search_as_you_type", m("search_as_you_type"), "a b"},
		{"percolator", m("percolator"), map[string]any{"match_all": map[string]any{}}},
		{"join", map[string]any{"type": "join", "relations": map[string]any{"parent": "child"}}, "parent"},
		{"rank_feature", m("rank_feature"), 1.5},
		{"rank_features", m("rank_features"), map[string]any{"a": 1}},
		{"long_range", m("long_range"), map[string]any{"gte": 1, "lte": 5}},
		{"double_range", m("double_range"), map[string]any{"gte": 1.5, "lte": 5.5}},
		{"date_range", m("date_range"), map[string]any{"gte": "2026-10-01", "lte": "2026-10-02"}},
		{"ip_range", m("ip_range"), map[string]any{"gte": "10.0.0.1", "lte": "10.0.0.9"}},
		{"xy_point", m("xy_point"), map[string]any{"x": 1.5, "y": 2.5}},
		{"version", m("version"), "1.2.3"},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			index := prefix + "no_" + e.p.name[:3] + "_" + strings.ReplaceAll(tt.typ, "_", "")
			status, b, err := s.do(t.Context(), http.MethodPut, "/"+index, map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{"id": m("integer"), "v": tt.mapping}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.drop(context.WithoutCancel(t.Context()), index) })
			if status >= 300 {
				// A release that has no such mapping type refuses it here, which
				// the 2 series does for version (recorded: "make an index with one
				// version").
				if status != http.StatusBadRequest || !strings.Contains(string(b), "mapper_parsing_exception") {
					t.Fatalf("making the index gave HTTP %d: %s", status, b)
				}
				t.Logf("the release refuses the mapping type: %.150s", b)
				return
			}
			s.must(t, http.MethodPut, "/"+index+"/_doc/1?refresh=true", map[string]any{"id": 1, "v": tt.doc})
			_, _, err = e.read(t, "SELECT id, v FROM "+index)
			oe, ok := errors.AsType[*opensearch.Error](err)
			if !ok || oe.Status != http.StatusBadRequest || oe.Type != "SemanticCheckException" || !strings.Contains(oe.Details, "can't resolve Symbol") {
				t.Errorf("selecting a field of the type %s gave %v, want the refusal of the server", tt.typ, err)
			}
			// SELECT * leaves the field out.
			cols, _, err := e.read(t, "SELECT * FROM "+index)
			if err != nil || len(cols) != 1 || cols[0] != "id" {
				t.Errorf("SELECT * gave the columns %v and %v, want id only", cols, err)
			}
		})
	}
	for _, typ := range []string{"flattened", "dense_vector", "histogram", "sparse_vector", "semantic"} {
		t.Run(typ, func(t *testing.T) {
			index := prefix + "no_" + e.p.name[:3] + "_" + typ
			status, b, err := s.do(t.Context(), http.MethodPut, "/"+index, map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{"v": m(typ)}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.drop(context.WithoutCancel(t.Context()), index) })
			// A type of Elasticsearch has no handler (mapper_parsing_exception). The
			// type semantic is a type of OpenSearch from a later release of the 3
			// series, which refuses a mapping that names no model, so no test can
			// make such a field.
			noHandler := strings.Contains(string(b), "mapper_parsing_exception")
			needsModel := typ == "semantic" && strings.Contains(string(b), "model_id is required for the semantic field")
			if status != http.StatusBadRequest || !noHandler && !needsModel {
				t.Errorf("a mapping of the type %s gave HTTP %d: %.200s, want HTTP 400 with mapper_parsing_exception: it is a type of Elasticsearch", typ, status, b)
			}
		})
	}
	t.Run("interval", func(t *testing.T) {
		_, _, err := e.read(t, "SELECT INTERVAL 1 DAY AS v")
		oe, ok := errors.AsType[*opensearch.Error](err)
		if !ok || oe.Status != http.StatusInternalServerError {
			t.Errorf("an interval gave %v, want the error of HTTP 500 (recorded: an interval)", err)
		}
	})
}
