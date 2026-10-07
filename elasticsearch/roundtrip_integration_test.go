package elasticsearch_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/elasticsearch"
)

// rtConnector opens connections for dbimptest.RoundTrip. A query goes to the
// driver, as the principal of the test, and each write goes to the document
// API as the administrator, because SQL takes none (D163). The statements of
// a write are these: create and drop with the name of the index, and
// insert, update and delete with the name of the index. create takes the
// type of the round trip, which names the mapping of the field v. Each
// write refreshes the index, so a query sees it at once.
type rtConnector struct {
	inner driver.Connector
	s     *api
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
	// The driver binds only what database/sql converts.
	for i := range args {
		if s, ok := args[i].Value.(string); ok {
			args[i].Value = s
		}
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
		status, b, err = c.rc.s.do(ctx, http.MethodPut, "/"+index, map[string]any{"mappings": map[string]any{"properties": map[string]any{"k": m("keyword"), "v": mapping}}})
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

// storeValue returns the JSON value that stores v in the field v. A time is
// text in ISO 8601, a date is the text of its midnight in UTC, a time of day is
// the text of that time on 1970-01-01, and bytes are base64.
func storeValue(v any) any {
	switch v := v.(type) {
	case []byte:
		return base64.StdEncoding.EncodeToString(v)
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case dbimp.Date:
		return v.String() + "T00:00:00Z"
	case dbimp.OffsetTime:
		return "1970-01-01T" + v.Time.String() + "Z"
	}
	return v
}

// rtMappings are the mapping of the field v of each type of the round trip. An
// interval has no field, and the select makes it from a number, and the type
// null has no field either.
var rtMappings = map[string]map[string]any{
	"null":          m("keyword"),
	"boolean":       m("boolean"),
	"byte":          m("byte"),
	"short":         m("short"),
	"integer":       m("integer"),
	"long":          m("long"),
	"unsigned_long": m("unsigned_long"),
	"half_float":    m("half_float"),
	"float":         m("float"),
	"double":        m("double"),
	"scaled_float":  {"type": "scaled_float", "scaling_factor": 100},
	"keyword":       m("keyword"),
	"text":          {"type": "text", "fields": map[string]any{"raw": m("keyword")}},
	"binary":        m("binary"),
	"ip":            m("ip"),
	"version":       m("version"),
	"datetime":      m("date_nanos"),
	"date":          m("date"),
	"time":          m("date"),
	"geo_point":     m("geo_point"),
	"geo_shape":     m("geo_shape"),
	"shape":         m("shape"),
}

// rtType is the round trip of one type: the mapping of its field, the
// expression of the select over the stored field v, and its values.
type rtType struct {
	typ    string
	store  string
	expr   string
	values []dbimptest.Value
	equal  func(got, want any) bool
}

// longText is a value of 5000 characters of several widths.
var longText = strings.Repeat("aé日😀", 1250)

// sameTime compares two times by their instant and their offset.
func sameTime(got, want any) bool {
	g, ok1 := got.(time.Time)
	w, ok2 := want.(time.Time)
	if !ok1 || !ok2 {
		return reflect.DeepEqual(got, want)
	}
	_, goff := g.Zone()
	_, woff := w.Zone()
	return g.Equal(w) && goff == woff
}

// intervalType returns the round trip of an interval type: a number that the
// select multiplies by an interval of the type, such as INTERVAL 1 DAY * v.
// An interval is SQL only, and a column of an index never has one (recorded:
// "every interval").
func intervalType(typ, base string, mult []int64, want func(n int64) any) rtType {
	vals := []dbimptest.Value{{Name: "null", In: nil}}
	for _, n := range mult {
		vals = append(vals, dbimptest.Value{Name: strconv.FormatInt(n, 10), In: n, Want: want(n)})
	}
	return rtType{typ: typ, store: "long", expr: base + " * v", values: vals}
}

// months returns the want function of an interval of n times the months.
func months(per int32) func(int64) any {
	return func(n int64) any { return dbimp.Interval{Months: int32(n) * per} } //nolint:gosec // The numbers of the test fit in an int32.
}

// span returns the want function of an interval of n times d.
func span(d time.Duration) func(int64) any {
	return func(n int64) any { return time.Duration(n) * d }
}

// clock returns the time of day h:m:s.ms in UTC.
func clock(h, m, s, ms int) dbimp.OffsetTime {
	return dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: ms * 1e6}}
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
	return []rtType{
		{typ: "null", store: "null", expr: "NULL", values: []dbimptest.Value{null, val("null again", nil)}},
		{typ: "boolean", store: "boolean", expr: "v", values: []dbimptest.Value{null, val("false", false), val("true", true)}},
		{typ: "byte", store: "byte", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(-128)}, {Name: "max", In: int64(127)}}},
		{typ: "short", store: "short", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(-32768)}, {Name: "max", In: int64(32767)}}},
		{typ: "integer", store: "integer", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(math.MinInt32)}, {Name: "max", In: int64(math.MaxInt32)}}},
		{typ: "long", store: "long", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(math.MinInt64)},
			{Name: "max", In: int64(math.MaxInt64)}, {Name: "last digit", In: int64(math.MaxInt64 - 1)}}},
		{typ: "unsigned_long", store: "unsigned_long", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: uint64(0)}, {Name: "max", In: uint64(math.MaxUint64)},
			{Name: "above int64", In: uint64(math.MaxInt64) + 1}, {Name: "last digit", In: uint64(math.MaxUint64 - 1)}}},
		{typ: "half_float", store: "half_float", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "max", In: 65504.0}, {Name: "min", In: -65504.0},
			{Name: "half", In: 0.5}, {Name: "quarter", In: 0.25}}},
		{typ: "float", store: "float", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "max", In: 3.4028235e38}, {Name: "min", In: -3.4028235e38},
			{Name: "smallest", In: 1.4e-45}, {Name: "fraction", In: 1.5}}},
		{typ: "double", store: "double", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "max", In: math.MaxFloat64}, {Name: "min", In: -math.MaxFloat64},
			{Name: "smallest", In: math.SmallestNonzeroFloat64}, {Name: "tenth", In: 0.1}, {Name: "last digit", In: math.Nextafter(1, 2)}}},
		{typ: "scaled_float", store: "scaled_float", expr: "v", values: []dbimptest.Value{null, {Name: "zero", In: 0.0}, {Name: "fraction", In: 1234.57},
			{Name: "negative", In: -1234.57}, {Name: "large", In: 1234567890123.45}}},
		{typ: "keyword", store: "keyword", expr: "v", values: []dbimptest.Value{null, val("empty", ""), val("unicode", "é'\"\\ x 日本語 😀"), val("long", longText)}},
		{typ: "text", store: "text", expr: "v", values: []dbimptest.Value{null, val("empty", ""), val("unicode", "é'\"\\ x 日本語 😀"), val("long", longText)}},
		{typ: "binary", store: "binary", expr: "v", values: []dbimptest.Value{null, val("empty", []byte{}), val("bytes", []byte{0, 255}), val("long", []byte(longText))}},
		{typ: "ip", store: "ip", expr: "v", values: []dbimptest.Value{null, val("zero", "0.0.0.0"), val("max", "255.255.255.255"), val("v6", "2001:db8::1"), val("loopback", "::1")}},
		{typ: "version", store: "version", expr: "v", values: []dbimptest.Value{null, val("zero", "0.0.0"), val("pre-release", "1.2.3-beta"), val("large", "999.999.999")}},
		{typ: "datetime", store: "datetime", expr: "v", equal: sameTime, values: []dbimptest.Value{null, val("epoch", ts("1970-01-01T00:00:00Z")),
			val("nanoseconds", ts("2026-10-01T12:34:56.123456789Z")), val("last digit", ts("2026-10-01T12:34:56.000000001Z")), val("max", ts("2262-04-11T23:47:16.854775807Z"))}},
		{typ: "date", store: "date", expr: "CAST(v AS DATE)", values: []dbimptest.Value{null, val("first day", dbimp.Date{Year: 1, Month: 1, Day: 1}),
			val("last day", dbimp.Date{Year: 9999, Month: 12, Day: 31}), val("day", dbimp.Date{Year: 2026, Month: 10, Day: 1}), val("epoch", dbimp.Date{Year: 1970, Month: 1, Day: 1})}},
		{typ: "time", store: "time", expr: "CAST(v AS TIME)", values: []dbimptest.Value{null, val("midnight", clock(0, 0, 0, 0)),
			val("last millisecond", clock(23, 59, 59, 999)), val("time", clock(12, 34, 56, 789)), val("first millisecond", clock(0, 0, 0, 1))}},
		{typ: "geo_point", store: "geo_point", expr: "v", values: []dbimptest.Value{null, val("origin", "POINT (0.0 0.0)"), val("point", "POINT (-71.34 41.12)"), val("edge", "POINT (179.5 89.5)")}},
		{typ: "geo_shape", store: "geo_shape", expr: "v", values: []dbimptest.Value{null, val("point", "POINT (0.0 0.0)"), val("line", "LINESTRING (0.0 0.0, 1.0 1.0)"), val("polygon", "POLYGON ((0.0 0.0, 1.0 0.0, 1.0 1.0, 0.0 0.0))")}},
		{typ: "shape", store: "shape", expr: "v", values: []dbimptest.Value{null, val("point", "POINT (1.5 2.5)"), val("line", "LINESTRING (0.0 0.0, 1.0 1.0)"), val("polygon", "POLYGON ((0.0 0.0, 1.0 0.0, 1.0 1.0, 0.0 0.0))")}},
		intervalType("interval_year", "INTERVAL 1 YEAR", []int64{0, 1, -2, 1000}, months(12)),
		intervalType("interval_month", "INTERVAL 1 MONTH", []int64{0, 1, -2, 12000}, months(1)),
		intervalType("interval_day", "INTERVAL 1 DAY", []int64{0, 1, -2, 1 << 10}, span(24*time.Hour)),
		intervalType("interval_hour", "INTERVAL 1 HOUR", []int64{0, 1, -2, 1 << 20}, span(time.Hour)),
		intervalType("interval_minute", "INTERVAL 1 MINUTE", []int64{0, 1, -2, 1 << 20}, span(time.Minute)),
		intervalType("interval_second", "INTERVAL 1 SECOND", []int64{0, 1, -2, 1 << 20}, span(time.Second)),
		intervalType("interval_year_to_month", "INTERVAL '1-2' YEAR TO MONTH", []int64{0, 1, -2, 1000}, months(14)),
		intervalType("interval_day_to_hour", "INTERVAL '1 2' DAY TO HOUR", []int64{0, 1, -2, 1 << 10}, span(26*time.Hour)),
		intervalType("interval_day_to_minute", "INTERVAL '1 2:3' DAY TO MINUTE", []int64{0, 1, -2, 1 << 10}, span(26*time.Hour+3*time.Minute)),
		intervalType("interval_day_to_second", "INTERVAL '1 02:03:04.5' DAY TO SECOND", []int64{0, 1, -2, 1 << 10}, span(26*time.Hour+3*time.Minute+4500*time.Millisecond)),
		intervalType("interval_hour_to_minute", "INTERVAL '2:3' HOUR TO MINUTE", []int64{0, 1, -2, 1 << 20}, span(2*time.Hour+3*time.Minute)),
		intervalType("interval_hour_to_second", "INTERVAL '2:3:4.5' HOUR TO SECOND", []int64{0, 1, -2, 1 << 20}, span(2*time.Hour+3*time.Minute+4500*time.Millisecond)),
		intervalType("interval_minute_to_second", "INTERVAL '3:4.5' MINUTE TO SECOND", []int64{0, 1, -2, 1 << 20}, span(3*time.Minute+4500*time.Millisecond)),
	}
}

// roundTrip returns the round trip of rt for the principal p, in an index of
// its own.
func roundTrip(p principal, rt rtType) dbimptest.RoundTripCase {
	index := prefix + "rt_" + p.name[:3] + "_" + rt.typ
	return dbimptest.RoundTripCase{
		Type:     rt.typ,
		Setup:    []string{"create " + index + " " + rt.store},
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

// noLiteral is the Literal of every round trip: SQL takes no insert, so a
// value has no literal of one (D163). The literals of a query are held in
// TestReplayValues.
func noLiteral(string, any) (string, error) {
	return "", fmt.Errorf("SQL in Elasticsearch takes no insert, so a value has no literal of one: %w", dbimp.ErrNotSupported)
}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type that
// features.json marks yes, as each principal. The administrator writes each
// value through the document API, and the principal reads it through the
// driver. An interval is SQL only, so its round trip stores a number and the
// select multiplies an interval by it.
func TestIntegrationRoundTrip(t *testing.T) {
	s := newAdminAPI(t)
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		for _, rt := range rtTypes() {
			t.Run(rt.typ, func(t *testing.T) {
				connector, err := elasticsearch.Driver{}.OpenConnector(dsn(t, p))
				if err != nil {
					t.Fatal(err)
				}
				rdb := sql.OpenDB(&rtConnector{inner: connector, s: s})
				t.Cleanup(func() { rdb.Close() })
				c := roundTrip(p, rt)
				dbimptest.RoundTrip(t, rdb, c)
			})
		}
		refusedTypes(t, db)
	})
}

// refusedTypes holds the entries of features.json that mark a type no: the
// server refuses to select a field of each, or has no such type, and the
// driver returns the refusal (recorded: "a dense_vector", "a flattened
// field", "a range and a point", "an aggregate_metric_double", "an object",
// "a nested field" and "step 7: a decimal").
func refusedTypes(t *testing.T, db *sql.DB) {
	t.Helper()
	name := typesIndex(t)
	for _, tt := range []struct {
		typ, field, want string
	}{
		{"dense_vector", "dv", "unsupported type [dense_vector]"},
		{"flattened", "fl", "unsupported type [flattened]"},
		{"integer_range", "ir", "unsupported type [integer_range]"},
		{"point", "pt", "unsupported type [point]"},
		{"aggregate_metric_double", "amd", "unsupported type [aggregate_metric_double]"},
		{"object", "o", "type [object] only its subfields"},
		{"nested", "n", "type [nested] only its subfields"},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			_, _, err := readAll(t, db, "SELECT "+tt.field+" FROM "+name+" WHERE id = 1")
			e, ok := errors.AsType[*elasticsearch.Error](err)
			if !ok || e.HTTPStatus != http.StatusBadRequest || e.RootType != "verification_exception" || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("selecting a field of the type %s gave %v, want the refusal of the server with %q", tt.typ, err, tt.want)
			}
		})
	}
	t.Run("decimal", func(t *testing.T) {
		// There is no decimal type: a cast to DECIMAL gives a double.
		var d float64
		rows, err := db.QueryContext(t.Context(), "SELECT CAST(1.5 AS DECIMAL) AS d")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cts, err := rows.ColumnTypes()
		if err != nil || len(cts) != 1 || cts[0].DatabaseTypeName() != "DOUBLE" {
			t.Fatalf("the types of a cast to DECIMAL are %v, %v, want DOUBLE", cts, err)
		}
		if !rows.Next() || rows.Scan(&d) != nil || d != 1.5 {
			t.Errorf("a cast to DECIMAL gave %v, %v", d, rows.Err())
		}
	})
}
