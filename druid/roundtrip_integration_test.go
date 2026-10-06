package druid_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/druid"
)

// rtConnector opens connections for dbimptest.RoundTrip. A query goes to the
// driver, as the principal of the test, and each write goes to the task API
// as the administrator, because the SQL API takes none (D163). The
// statements of a write are these: create, insert and update with the name
// of the datasource and the storage of the value, delete with the name of
// the datasource, and drop with the name of the datasource. Each key has a
// day of its own, so an insert and an update are a REPLACE of that day, and
// a delete marks the segment of that day unused through the Coordinator,
// which takes no task. TestIntegrationCRUD holds the delete by a REPLACE
// with no rows. create writes a row with the key filler on a day of its
// own, so that the datasource stays when the round trip deletes the row of
// each key.
type rtConnector struct {
	inner driver.Connector
	s     *api

	mu   sync.Mutex
	days map[string]int
}

func (rc *rtConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := rc.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &rtConn{Conn: conn, rc: rc}, nil
}

func (rc *rtConnector) Driver() driver.Driver { return rc.inner.Driver() }

// day returns the day of key, from 2000-01-02 on.
func (rc *rtConnector) day(key string) time.Time {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	n, ok := rc.days[key]
	if !ok {
		n = len(rc.days) + 1
		rc.days[key] = n
	}
	return time.Date(2000, 1, 1+n, 0, 0, 0, 0, time.UTC)
}

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
	if len(words) < 2 {
		return nil, fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	table := words[1]
	storage := ""
	if len(words) > 2 {
		storage = words[2]
	}
	key := func(i int) string {
		s, _ := args[i].Value.(string)
		return s
	}
	var err error
	switch words[0] {
	case "create":
		// A segment whose JSON column holds only NULL stores it as a
		// VARCHAR (measured), so the filler of JSON holds an object.
		var filler any
		if storage == storeJSON {
			filler = map[string]any{}
		}
		err = c.put(ctx, table, storage, "filler", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), filler)
	case "drop":
		err = c.rc.s.drop(ctx, table)
	case "insert":
		err = c.put(ctx, table, storage, key(0), c.rc.day(key(0)), args[1].Value)
	case "update":
		err = c.put(ctx, table, storage, key(1), c.rc.day(key(1)), args[0].Value)
	case "delete":
		d := c.rc.day(key(0))
		err = c.rc.s.markUnused(ctx, table, d.Format(time.RFC3339)+"/"+d.AddDate(0, 0, 1).Format(time.RFC3339))
	default:
		err = fmt.Errorf("the statement %q: %w", query, dbimp.ErrInvalidValue)
	}
	return driver.ResultNoRows, err
}

// put writes the row of key on the day d, with the value v in the storage of
// the round trip.
func (c *rtConn) put(ctx context.Context, table, storage, key string, d time.Time, v any) error {
	var expr, from string
	var err error
	if storage == storeArray {
		// The task API refuses a NULL literal of an ARRAY (measured), so an
		// array comes from a row of JSON that EXTERN reads.
		expr = "v"
		from, err = arrayFrom(v)
	} else {
		expr, err = storageExpr(storage, v)
	}
	if err != nil {
		return err
	}
	return c.rc.s.task(ctx, "REPLACE INTO "+table+" "+overTime(d)+" SELECT TIMESTAMP '"+d.Format(time.DateTime)+"' AS __time, "+
		sqlQuote(key)+" AS k, "+expr+" AS v"+from+" PARTITIONED BY DAY")
}

// arrayFrom returns the FROM clause that reads the array v, or NULL for nil,
// as the column v of one row of JSON.
func arrayFrom(v any) (string, error) {
	row, err := json.Marshal(map[string]any{"v": v})
	if err != nil {
		return "", err
	}
	source, err := json.Marshal(map[string]any{"type": "inline", "data": string(row)})
	if err != nil {
		return "", err
	}
	return " FROM TABLE(EXTERN(" + sqlQuote(string(source)) + `, '{"type":"json"}')) EXTEND (v VARCHAR ARRAY)`, nil
}

// timeRange is the condition of the day d.
func timeRange(d time.Time) string {
	return "__time >= TIMESTAMP '" + d.Format(time.DateTime) + "' AND __time < TIMESTAMP '" + d.AddDate(0, 0, 1).Format(time.DateTime) + "'"
}

// overTime is the clause of a REPLACE that overwrites the day d.
func overTime(d time.Time) string {
	return "OVERWRITE WHERE " + timeRange(d)
}

// The storages of a value. A datasource stores a boolean, a time and a date
// as a BIGINT (measured), so the select of a round trip turns the stored
// value into its type.
const (
	storeLong   = "long"
	storeFloat  = "float"
	storeDouble = "double"
	storeString = "string"
	storeArray  = "array"
	storeJSON   = "json"
)

// storageExpr returns the SQL expression of v in the storage, which the
// task writes. NULL is a NULL of the type of the storage.
func storageExpr(storage string, v any) (string, error) {
	if v == nil {
		switch storage {
		case storeLong:
			return "CAST(NULL AS BIGINT)", nil
		case storeFloat:
			return "CAST(NULL AS FLOAT)", nil
		case storeDouble:
			return "CAST(NULL AS DOUBLE)", nil
		case storeString:
			return "CAST(NULL AS VARCHAR)", nil
		case storeJSON:
			return "PARSE_JSON(CAST(NULL AS VARCHAR))", nil
		}
		return "", fmt.Errorf("the storage %q: %w", storage, dbimp.ErrInvalidValue)
	}
	switch v := v.(type) {
	case bool:
		if v {
			return "CAST(1 AS BIGINT)", nil
		}
		return "CAST(0 AS BIGINT)", nil
	case int64:
		return "CAST(" + strconv.FormatInt(v, 10) + " AS BIGINT)", nil
	case time.Time:
		return "CAST(" + strconv.FormatInt(v.UnixMilli(), 10) + " AS BIGINT)", nil
	case dbimp.Date:
		return "CAST(" + strconv.FormatInt(v.In(time.UTC).UnixMilli(), 10) + " AS BIGINT)", nil
	case float64:
		typ := "DOUBLE"
		if storage == storeFloat {
			typ = "FLOAT"
		}
		return "CAST(" + strconv.FormatFloat(v, 'g', -1, 64) + " AS " + typ + ")", nil
	case string:
		if storage == storeJSON {
			return "PARSE_JSON(" + sqlQuote(v) + ")", nil
		}
		return sqlQuote(v), nil
	case map[string]any:
		b, err := json.Marshal(v, json.Deterministic(true))
		if err != nil {
			return "", err
		}
		return "PARSE_JSON(" + sqlQuote(string(b)) + ")", nil
	}
	return "", fmt.Errorf("a value of %T: %w", v, dbimp.ErrNotSupported)
}

// noLiteral is the Literal of every round trip: the SQL API takes no insert,
// so a value has no literal of one (D163). The literals of a query are held
// in TestReplayValues.
func noLiteral(string, any) (string, error) {
	return "", fmt.Errorf("the SQL API of Druid takes no insert, so a value has no literal of one: %w", dbimp.ErrNotSupported)
}

// rtType is the round trip of one type: its storage, the expression of the
// select over the stored column v, and its values.
type rtType struct {
	typ     string
	storage string
	expr    string
	values  []dbimptest.Value
	equal   func(got, want any) bool
	// group is true for an aggregate, whose select groups by the key.
	group bool
}

// roundTrip returns the round trip of rt for the principal p, in a
// datasource of its own.
func roundTrip(p principal, rt rtType) dbimptest.RoundTripCase {
	table := prefix + "rt_" + p.name[:3] + "_" + strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		if 'a' <= r && r <= 'z' {
			return r
		}
		return '_'
	}, rt.typ)
	sel := "SELECT " + rt.expr + " AS v FROM " + table + " WHERE k = ?"
	if rt.group {
		sel += " GROUP BY k"
	}
	return dbimptest.RoundTripCase{
		Type:     rt.typ,
		Setup:    []string{"create " + table + " " + rt.storage},
		Teardown: []string{"drop " + table},
		Insert:   "insert " + table + " " + rt.storage,
		Literal:  noLiteral,
		Select:   sel,
		Update:   "update " + table + " " + rt.storage,
		Delete:   "delete " + table,
		Values:   rt.values,
		Wait:     visible,
		Equal:    rt.equal,
	}
}

// sketchOf returns the bytes of the HLL sketch of the strings vs, from a
// query of the server, for the value that a round trip of a sketch wants.
func sketchOf(t *testing.T, db *sql.DB, vs ...string) []byte {
	t.Helper()
	elems := "CAST(NULL AS VARCHAR)"
	if len(vs) > 0 {
		var q []string
		for _, v := range vs {
			q = append(q, sqlQuote(v))
		}
		elems = strings.Join(q, ", ")
	}
	var b []byte
	if err := db.QueryRowContext(t.Context(), "SELECT DS_HLL(x) AS h FROM UNNEST(ARRAY["+elems+"]) AS u(x)").Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

// rtTypes returns the round trip of each type that features.json marks yes.
func rtTypes(t *testing.T, db *sql.DB) []rtType {
	t.Helper()
	ts := func(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
	long := strings.Repeat("é", 512)
	anyNil := func(got, _ any) bool { return got == nil }
	return []rtType{
		{typ: "BIGINT", storage: storeLong, expr: "v", values: []dbimptest.Value{
			{Name: "null"}, {Name: "zero", In: int64(0)}, {Name: "min", In: int64(math.MinInt64)}, {Name: "max", In: int64(math.MaxInt64)}}},
		{typ: "INTEGER", storage: storeLong, expr: "CAST(v AS INTEGER)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "min", In: int64(math.MinInt32)}, {Name: "max", In: int64(math.MaxInt32)}}},
		// A FLOAT holds a float32, and the server writes its shortest text
		// (measured), which these values keep.
		{typ: "FLOAT", storage: storeFloat, expr: "v", values: []dbimptest.Value{
			{Name: "null"}, {Name: "step", In: 0.1}, {Name: "limit", In: -3.4e38}}},
		{typ: "REAL", storage: storeDouble, expr: "CAST(v AS REAL)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "step", In: 0.1}, {Name: "negative", In: -1.5}}},
		{typ: "DOUBLE", storage: storeDouble, expr: "v", values: []dbimptest.Value{
			{Name: "null"}, {Name: "zero", In: 0.0}, {Name: "step", In: 0.1}, {Name: "max", In: math.MaxFloat64}}},
		// A DECIMAL is a double on the server (D164).
		{typ: "DECIMAL", storage: storeDouble, expr: "CAST(v AS DECIMAL(38, 10))", values: []dbimptest.Value{
			{Name: "null"}, {Name: "digits", In: 1234567890.123}, {Name: "max", In: math.MaxFloat64}}},
		// A datasource stores a boolean as a BIGINT of 1 or 0 (measured).
		{typ: "BOOLEAN", storage: storeLong, expr: "v = 1", values: []dbimptest.Value{
			{Name: "null"}, {Name: "true", In: true}, {Name: "false", In: false}}},
		{typ: "VARCHAR", storage: storeString, expr: "v", values: []dbimptest.Value{
			{Name: "null"}, {Name: "empty", In: ""}, {Name: "unicode", In: "é'\"\\ x ☃"}, {Name: "long", In: long}}},
		{typ: "CHAR", storage: storeString, expr: "CAST(v AS CHAR)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "unicode", In: "é'\"\\ x ☃"}}},
		// A datasource stores a time as a BIGINT of milliseconds (measured).
		{typ: "TIMESTAMP", storage: storeLong, expr: "MILLIS_TO_TIMESTAMP(v)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "milliseconds", In: ts(1790858096789)},
			{Name: "zone", In: ts(1650000000001).In(time.FixedZone("x", -7*3600)), Want: ts(1650000000001)},
			{Name: "before 1970", In: ts(-2208988800000)}, {Name: "far", In: ts(253402300799999)}}},
		{typ: "DATE", storage: storeLong, expr: "CAST(MILLIS_TO_TIMESTAMP(v) AS DATE)", values: []dbimptest.Value{
			{Name: "null"}, {Name: "day", In: dbimp.Date{Year: 2026, Month: 10, Day: 1}}, {Name: "before 1970", In: dbimp.Date{Year: 1900, Month: 1, Day: 1}}}},
		{typ: "ARRAY", storage: storeArray, expr: "v", values: []dbimptest.Value{
			{Name: "null"}, {Name: "with null", In: []any{"a", nil, "c"}}, {Name: "unicode", In: []any{"é"}}}},
		{typ: "COMPLEX<json>", storage: storeJSON, expr: "v", values: []dbimptest.Value{
			{Name: "null"}, {Name: "object", In: map[string]any{"k": []any{int64(1), "two", nil, map[string]any{"n": 1.5}}}},
			{Name: "array", In: "[]", Want: []any{}}, {Name: "string", In: `"s"`, Want: "s"}}},
		// A sketch is the aggregate of the stored strings, and the sketch of
		// a NULL is the sketch of nothing.
		{typ: "COMPLEX<HLLSketch>", storage: storeString, expr: "DS_HLL(v)", group: true, values: []dbimptest.Value{
			{Name: "null", Want: sketchOf(t, db)}, {Name: "a", In: "a", Want: sketchOf(t, db, "a")}, {Name: "b", In: "b", Want: sketchOf(t, db, "b")}}},
		// The type NULL is the type of the literal NULL, which a stored row
		// selects.
		{typ: "NULL", storage: storeString, expr: "NULL", equal: anyNil, values: []dbimptest.Value{
			{Name: "null"}, {Name: "text", In: "x"}}},
	}
}

// checkType selects the row of the filler with the select of a round trip,
// and fails the test unless the SQL type of its column is the one of typ,
// which is OTHER for a complex type.
func checkType(t *testing.T, db *sql.DB, query, typ string) {
	t.Helper()
	want := typ
	if strings.HasPrefix(typ, "COMPLEX<") {
		want = "OTHER"
	}
	rows, err := db.QueryContext(t.Context(), query, "filler")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got := cts[0].DatabaseTypeName(); got != want {
		t.Errorf("the select %q gives the type %s, want %s", query, got, want)
	}
}

// envFull is the environment variable that makes the round trip run every
// type.
const envFull = "DBIMP_FULL"

// quickTypes are the types of the round trip that a run reads when envFull is
// not set: an integer, a string and a time.
var quickTypes = []string{"BIGINT", "VARCHAR", "TIMESTAMP"}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type that
// features.json marks yes, as each principal. The administrator writes each
// value through the task API, and the principal reads it through the
// driver.
//
// Each write is a task of the server that takes 5 to 12 seconds, so the round
// trip of every type takes more than an hour for a release. A run reads the
// types of quickTypes only, unless the environment variable DBIMP_FULL is set,
// which the workflow sets for the nightly run and for a manual one.
func TestIntegrationRoundTrip(t *testing.T) {
	s := newAdminAPI(t)
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		for _, rt := range rtTypes(t, db) {
			t.Run(rt.typ, func(t *testing.T) {
				if os.Getenv(envFull) == "" && !slices.Contains(quickTypes, rt.typ) {
					t.Skipf("the round trip of %s runs when %s is set, because each write is a task of the server", rt.typ, envFull)
				}
				connector, err := druid.Driver{}.OpenConnector(dsn(t, p))
				if err != nil {
					t.Fatal(err)
				}
				rdb := sql.OpenDB(&rtConnector{inner: connector, s: s, days: map[string]int{}})
				t.Cleanup(func() { rdb.Close() })
				c := roundTrip(p, rt)
				if c.Equal == nil {
					c.Equal = reflect.DeepEqual
				}
				dbimptest.RoundTrip(t, rdb, c)
				// The teardown runs in a cleanup, so the row of the filler
				// is still there, and names the type of the select.
				checkType(t, db, c.Select, rt.typ)
			})
		}
	})
}
