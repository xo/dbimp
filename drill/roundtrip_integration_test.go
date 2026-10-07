package drill_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/drill"
)

// The round trip of a type (step 14a) needs a row that a test can insert, select
// by its key, update and delete. The server refuses INSERT, UPDATE and DELETE
// (D163), and runs CREATE TABLE AS, so a table that is a directory holds one
// row for each key, and each row is a table of its own in the directory. The
// directory has the columns k, the key, and v, the value. A statement of the
// round trip is a word of its own, and rtConn turns it into the statement that
// the server runs:
//
//	create T   makes the row of the key filler, so that the directory stays
//	insert T   makes the table of the key, with the value of the bound argument
//	update T   makes the table of the key again, because the server cannot
//	           change a row, and a table that is made again is a new row
//	delete T   drops the table of the key
//	select T   selects v of the directory with the key
//	drop T     drops the directory
//
// The driver binds every value, so a bound value is a value that the driver
// writes as a literal (D165). The literal of the round trip is written by the
// test itself, in lit, so that the test does not depend on the writer of the
// driver for it.

// rtConnector opens connections for dbimptest.RoundTrip.
type rtConnector struct {
	inner driver.Connector
	// dir is the directory of the round trip, and it holds one type.
	dir string
	// expr is the SQL of the value of the type, with one ? for the value, and
	// filler is the SQL of the value of the row of the filler.
	expr, filler string
}

func (rc *rtConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := rc.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &rtConn{Conn: conn, rc: rc}, nil
}

func (rc *rtConnector) Driver() driver.Driver { return rc.inner.Driver() }

// table returns the SQL name of the table of the key.
func (rc *rtConnector) table(key string) string {
	return "dfs.tmp.`" + rc.dir + "/" + key + "`"
}

// rtConn is a connection of rtConnector.
type rtConn struct {
	driver.Conn

	rc *rtConnector
}

// CheckNamedValue keeps every value as it is, so that the driver gets the Go
// value of the test.
func (c *rtConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (c *rtConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, fmt.Errorf("the connection runs no query: %w", dbimp.ErrNotSupported)
	}
	if op, _, _ := strings.Cut(query, " "); op == "select" {
		query = "SELECT v FROM dfs.tmp.`" + c.rc.dir + "` WHERE k = ?"
	}
	return q.QueryContext(ctx, query, args)
}

func (c *rtConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	e, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, fmt.Errorf("the connection runs no statement: %w", dbimp.ErrNotSupported)
	}
	rc := c.rc
	ctas := func(table string, args []driver.NamedValue, value string) (driver.Result, error) {
		return e.ExecContext(ctx, "CREATE TABLE "+table+" AS SELECT ? AS k, "+value+" AS v", args)
	}
	op, _, _ := strings.Cut(query, " ")
	switch op {
	case "create":
		return e.ExecContext(ctx, "CREATE TABLE "+rc.table("filler")+" AS SELECT 'filler' AS k, "+rc.filler+" AS v", nil)
	case "insert", "update":
		key, value := args[0], args[1]
		if op == "update" {
			key, value = args[1], args[0]
		}
		table := rc.table(fmt.Sprint(key.Value))
		// The row is a table, and a table that exists is dropped first.
		if _, err := e.ExecContext(ctx, "DROP TABLE IF EXISTS "+table, nil); err != nil {
			return nil, err
		}
		pair := []driver.NamedValue{{Ordinal: 1, Value: key.Value}, {Ordinal: 2, Value: value.Value}}
		return ctas(table, pair, rc.expr)
	case "delete":
		return e.ExecContext(ctx, "DROP TABLE "+rc.table(fmt.Sprint(args[0].Value)), nil)
	case "drop":
		return e.ExecContext(ctx, "DROP TABLE IF EXISTS dfs.tmp.`"+rc.dir+"`", nil)
	}
	return e.ExecContext(ctx, query, args)
}

// lit writes v as the literal of a statement.
func lit(v any) (string, error) {
	switch v := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		return strings.ToUpper(strconv.FormatBool(v)), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		switch {
		case math.IsNaN(v):
			return "CAST('NaN' AS DOUBLE)", nil
		case math.IsInf(v, 1):
			return "CAST('Infinity' AS DOUBLE)", nil
		case math.IsInf(v, -1):
			return "CAST('-Infinity' AS DOUBLE)", nil
		}
		return "CAST('" + strconv.FormatFloat(v, 'g', -1, 64) + "' AS DOUBLE)", nil
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'", nil
	case []byte:
		var b strings.Builder
		for _, c := range v {
			fmt.Fprintf(&b, `\x%02x`, c)
		}
		return "binary_string('" + b.String() + "')", nil
	case *apd.Decimal:
		return "'" + v.Text('f') + "'", nil
	case dbimp.Date:
		return "DATE '" + v.String() + "'", nil
	case dbimp.LocalTime:
		return fmt.Sprintf("TIME '%02d:%02d:%02d.%03d'", v.Hour, v.Minute, v.Second, v.Nanosecond/1e6), nil
	case dbimp.LocalDateTime:
		return fmt.Sprintf("TIMESTAMP '%s %02d:%02d:%02d.%03d'", v.Date, v.Time.Hour, v.Time.Minute, v.Time.Second, v.Time.Nanosecond/1e6), nil
	case dbimp.Interval:
		if v.Months != 0 || v.Days == 0 && v.Nanoseconds == 0 {
			m := int64(v.Months)
			sign := ""
			if m < 0 {
				sign, m = "-", -m
			}
			return fmt.Sprintf("INTERVAL '%s%d-%d' YEAR TO MONTH", sign, m/12, m%12), nil
		}
		ms := int64(v.Days)*86400000 + v.Nanoseconds/1e6
		sign := ""
		if ms < 0 {
			sign, ms = "-", -ms
		}
		return fmt.Sprintf("INTERVAL '%s%d %02d:%02d:%02d.%03d' DAY TO SECOND", sign, ms/86400000, ms%86400000/3600000, ms%3600000/60000, ms%60000/1000, ms%1000), nil
	}
	return "", fmt.Errorf("writing a literal of %T: %w", v, dbimp.ErrNotSupported)
}

// rtType is the round trip of one type.
type rtType struct {
	typ string
	// cast is the SQL of the value of the type with a ? for the value, and
	// filler is the SQL of the value of the filler.
	cast, filler string
	values       []dbimptest.Value
	equal        func(got, want any) bool
}

// cast returns the SQL of the value of a type, with a ? for the value.
func cast(typ string) string { return "CAST(? AS " + typ + ")" }

// decimalOf returns the decimal of s.
func decimalOf(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// sameDecimal compares two decimals by value, because the server keeps the
// scale of the column and the test writes the scale of the value.
func sameDecimal(got, want any) bool {
	g, gok := got.(*apd.Decimal)
	w, wok := want.(*apd.Decimal)
	if !gok || !wok {
		return got == nil && want == nil
	}
	return g.Cmp(w) == 0
}

// sameFloat compares two floats, and two NaN are the same.
func sameFloat(got, want any) bool {
	g, gok := got.(float64)
	w, wok := want.(float64)
	if gok && wok && math.IsNaN(g) && math.IsNaN(w) {
		return true
	}
	return reflect.DeepEqual(got, want)
}

// ts returns the date and the time of day of a TIMESTAMP.
func ts(y, m, d, h, mi, s, ms int) dbimp.LocalDateTime {
	return dbimp.LocalDateTime{Date: dbimp.Date{Year: y, Month: time.Month(m), Day: d}, Time: dbimp.LocalTime{Hour: h, Minute: mi, Second: s, Nanosecond: ms * 1e6}}
}

// rtTypes returns the types of the round trip, for each type that features.json
// marks yes.
func rtTypes() []rtType {
	long := strings.Repeat("0123456789", 6000)
	allBytes := make([]byte, 0, 256*10)
	for range 10 {
		for b := range 256 {
			allBytes = append(allBytes, byte(b))
		}
	}
	null := dbimptest.Value{Name: "null"}
	return []rtType{
		{typ: "INT", cast: cast("INT"), filler: "CAST(NULL AS INT)", values: []dbimptest.Value{
			null, {Name: "min", In: int64(math.MinInt32)}, {Name: "max", In: int64(math.MaxInt32)}, {Name: "zero", In: int64(0)}}},
		{typ: "BIGINT", cast: cast("BIGINT"), filler: "CAST(NULL AS BIGINT)", values: []dbimptest.Value{
			null, {Name: "min", In: int64(math.MinInt64)}, {Name: "max", In: int64(math.MaxInt64)}, {Name: "zero", In: int64(0)}}},
		{typ: "FLOAT4", cast: cast("FLOAT"), filler: "CAST(NULL AS FLOAT)", values: []dbimptest.Value{
			null, {Name: "min", In: -math.MaxFloat32}, {Name: "smallest", In: float64(math.SmallestNonzeroFloat32)}, {Name: "fraction", In: 1.5}, {Name: "zero", In: 0.0}}},
		{typ: "FLOAT8", cast: cast("DOUBLE"), filler: "CAST(NULL AS DOUBLE)", equal: sameFloat, values: []dbimptest.Value{
			null, {Name: "min", In: -math.MaxFloat64}, {Name: "max", In: math.MaxFloat64}, {Name: "smallest", In: math.SmallestNonzeroFloat64},
			{Name: "tenth", In: 0.1}, {Name: "infinity", In: math.Inf(1)}, {Name: "negative infinity", In: math.Inf(-1)}, {Name: "nan", In: math.NaN()}, {Name: "zero", In: 0.0}}},
		{typ: "VARDECIMAL", cast: cast("DECIMAL(38, 18)"), filler: "CAST(NULL AS DECIMAL(38, 18))", equal: sameDecimal, values: []dbimptest.Value{
			null, {Name: "min", In: decimalOf("-12345678901234567890.123456789012345678")}, {Name: "last digit", In: decimalOf("0.000000000000000001")},
			{Name: "max", In: decimalOf("99999999999999999999.999999999999999999")}, {Name: "zero", In: decimalOf("0")}}},
		{typ: "BIT", cast: cast("BOOLEAN"), filler: "CAST(NULL AS BOOLEAN)", values: []dbimptest.Value{
			null, {Name: "true", In: true}, {Name: "false", In: false}}},
		{typ: "VARCHAR", cast: cast("VARCHAR"), filler: "CAST(NULL AS VARCHAR)", values: []dbimptest.Value{
			null, {Name: "empty", In: ""}, {Name: "unicode", In: "é'\"\\ x 日本語 😀"}, {Name: "long", In: long}, {Name: "line", In: "a\nb\tc"}}},
		// The server cannot write a NULL of VARBINARY with CREATE TABLE AS. It fails
		// with "Unable to convert the value of null:VARBINARY(65535) and type
		// VARBINARY to a Drill constant expression", and a NULL that is not a
		// constant has no type in its file, so the other files of the directory
		// read as NULL (measured). TestIntegrationTypes reads a NULL of VARBINARY
		// from a row of a table that holds other rows.
		{typ: "VARBINARY", cast: cast("VARBINARY"), filler: "CAST(binary_string('\\x00') AS VARBINARY)", values: []dbimptest.Value{
			{Name: "empty", In: []byte{}}, {Name: "bytes", In: []byte{0, 1, 2, 255}}, {Name: "every byte", In: allBytes}}},
		{typ: "DATE", cast: cast("DATE"), filler: "CAST(NULL AS DATE)", values: []dbimptest.Value{
			null, {Name: "first", In: dbimp.Date{Year: 1, Month: 1, Day: 1}}, {Name: "last", In: dbimp.Date{Year: 9999, Month: 12, Day: 31}},
			{Name: "epoch", In: dbimp.Date{Year: 1970, Month: 1, Day: 1}}, {Name: "leap day", In: dbimp.Date{Year: 2024, Month: 2, Day: 29}}}},
		{typ: "TIME", cast: cast("TIME"), filler: "CAST(NULL AS TIME)", values: []dbimptest.Value{
			null, {Name: "midnight", In: dbimp.LocalTime{}}, {Name: "last", In: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6}},
			{Name: "millisecond", In: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}}}},
		{typ: "TIMESTAMP", cast: cast("TIMESTAMP"), filler: "CAST(NULL AS TIMESTAMP)", values: []dbimptest.Value{
			null, {Name: "first", In: ts(1, 1, 1, 0, 0, 0, 0)}, {Name: "last", In: ts(9999, 12, 31, 23, 59, 59, 999)},
			{Name: "before 1970", In: ts(1969, 12, 31, 23, 59, 59, 999)}, {Name: "millisecond", In: ts(2026, 10, 1, 12, 34, 56, 789)}}},
		// A NULL interval that is alone in its file reads back as an interval of
		// garbage, such as P-805306368M32751DT-805306.240S (measured), so the
		// round trip of an interval has no NULL.
		{typ: "INTERVAL", cast: "?", filler: "INTERVAL '0' MONTH", values: []dbimptest.Value{
			{Name: "months", In: dbimp.Interval{Months: -14}}, {Name: "days", In: dbimp.Interval{Days: 1, Nanoseconds: 7384500e6}},
			{Name: "zero", In: dbimp.Interval{}}}},
		{typ: "INTERVALDAY", cast: "?", filler: "INTERVAL '0' SECOND", values: []dbimptest.Value{
			{Name: "positive", In: dbimp.Interval{Days: 1, Nanoseconds: 7384500e6}}, {Name: "negative", In: dbimp.Interval{Days: -1, Nanoseconds: -7384500e6}},
			{Name: "millisecond", In: dbimp.Interval{Nanoseconds: 1e6}}}},
		{typ: "INTERVALYEAR", cast: "?", filler: "INTERVAL '0' MONTH", values: []dbimptest.Value{
			{Name: "positive", In: dbimp.Interval{Months: 14}}, {Name: "negative", In: dbimp.Interval{Months: -14}},
			{Name: "month", In: dbimp.Interval{Months: 1}}, {Name: "years", In: dbimp.Interval{Months: 12*99 + 11}}}},
		// The server writes a map and a list of its own types, and a file whose
		// map has other keys has another type, so the values of one round trip
		// have one shape. A NULL map and an empty list have no type in a file,
		// so the round trip has neither.
		{typ: "MAP", cast: "convert_from(?, 'JSON')", filler: `convert_from('{"k":0,"s":""}', 'JSON')`, values: []dbimptest.Value{
			{Name: "numbers", In: `{"k":1,"s":"a"}`, Want: map[string]any{"k": int64(1), "s": "a"}},
			{Name: "smallest", In: `{"k":-9223372036854775808,"s":"é'\""}`, Want: map[string]any{"k": int64(math.MinInt64), "s": "é'\""}},
			{Name: "zero", In: `{"k":0,"s":""}`, Want: map[string]any{"k": int64(0), "s": ""}}}},
		{typ: "LIST", cast: "convert_from(?, 'JSON')", filler: `convert_from('[[0]]', 'JSON')`, values: []dbimptest.Value{
			{Name: "lists", In: `[[1,2],[3]]`, Want: []any{[]any{int64(1), int64(2)}, []any{int64(3)}}},
			{Name: "extremes", In: `[[-9223372036854775808],[9223372036854775807,5]]`, Want: []any{[]any{int64(math.MinInt64)}, []any{int64(math.MaxInt64), int64(5)}}},
			{Name: "zero", In: `[[0]]`, Want: []any{[]any{int64(0)}}}}},
	}
}

// roundTrip returns the case of dbimptest.RoundTrip for one type, with the
// directory dir.
func roundTrip(rt rtType, dir string) dbimptest.RoundTripCase {
	c := dbimptest.RoundTripCase{
		Type:     rt.typ,
		Setup:    []string{"create " + rt.typ},
		Teardown: []string{"drop " + rt.typ},
		Insert:   "insert " + rt.typ,
		Select:   "select " + rt.typ,
		Update:   "update " + rt.typ,
		Delete:   "delete " + rt.typ,
		Values:   rt.values,
		Equal:    rt.equal,
	}
	c.Literal = func(key string, v any) (string, error) {
		text, err := lit(v)
		if err != nil {
			return "", err
		}
		value := strings.Replace(rt.cast, "?", text, 1)
		if rt.typ == "VARDECIMAL" {
			value = "CAST(" + text + " AS DECIMAL(38, 18))"
		}
		return "CREATE TABLE dfs.tmp.`" + dir + "/" + key + "` AS SELECT '" + key + "' AS k, " + value + " AS v", nil
	}
	if c.Equal == nil {
		c.Equal = reflect.DeepEqual
	}
	return c
}

// TestIntegrationRoundTrip runs dbimptest.RoundTrip for each type that
// features.json marks yes, as each principal. Each other type has a test that
// sends it, and shows what the server does with it. The ordinary user can make
// and drop a table in dfs.tmp (recorded), so both principals write and read.
func TestIntegrationRoundTrip(t *testing.T) {
	for _, rt := range rtTypes() {
		t.Run(rt.typ, func(t *testing.T) {
			forEach(t, func(t *testing.T, p principal, db *sql.DB) {
				dir := name(p, "rt_"+strings.ToLower(rt.typ))
				connector, err := drill.Driver{}.OpenConnector(dsn(t, p))
				if err != nil {
					t.Fatal(err)
				}
				rdb := sql.OpenDB(&rtConnector{inner: connector, dir: dir, expr: rt.cast, filler: rt.filler})
				t.Cleanup(func() { rdb.Close() })
				dbimptest.RoundTrip(t, rdb, roundTrip(rt, dir))
			})
		})
	}
	for _, rt := range refusedTypes() {
		each(t, rt.name, rt.test)
	}
}

// refusedType is a type that the survey marks no, and the test that shows it.
type refusedType struct {
	name string
	test func(t *testing.T, p principal, db *sql.DB)
}

// failsWith runs a statement that the server refuses, and checks the message.
func failsWith(t *testing.T, db *sql.DB, query, want string) {
	t.Helper()
	err := queryErr(t.Context(), db, query)
	if e, ok := errors.AsType[*drill.Error](err); !ok || !strings.Contains(e.Message, want) {
		t.Errorf("%s gave %v, want an error that holds %q", query, err, want)
	}
}

// typeNames returns the names of the types of the columns of a query.
func typeNames(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ct := range cts {
		names = append(names, ct.DatabaseTypeName())
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

// refusedTypes are the types that the server refuses or never names (recorded
// in features.json).
func refusedTypes() []refusedType {
	var out []refusedType
	// SMALLINT and TINYINT fail with a pointer to DRILL-1959, and the unsigned
	// types are unknown names.
	for _, typ := range []string{"SMALLINT", "TINYINT"} {
		out = append(out, refusedType{typ, func(t *testing.T, _ principal, db *sql.DB) {
			failsWith(t, db, "SELECT CAST(1 AS "+typ+") AS n FROM (VALUES(1))", "DRILL-1959")
		}})
	}
	for _, typ := range []string{"UINT1", "UINT2", "UINT4", "UINT8"} {
		out = append(out, refusedType{typ, func(t *testing.T, _ principal, db *sql.DB) {
			err := queryErr(t.Context(), db, "SELECT CAST(1 AS "+typ+") AS u FROM (VALUES(1))")
			if _, ok := errors.AsType[*drill.Error](err); !ok {
				t.Errorf("CAST AS %s gave %v, want an error of the server", typ, err)
			}
		}})
	}
	// Each DECIMAL of the SQL is a VARDECIMAL on the wire, and no answer names
	// DECIMAL, DECIMAL28SPARSE or DECIMAL38SPARSE.
	for _, typ := range []string{"DECIMAL", "DECIMAL28SPARSE", "DECIMAL38SPARSE"} {
		out = append(out, refusedType{typ, func(t *testing.T, _ principal, db *sql.DB) {
			got := typeNames(t, db, "SELECT CAST('1' AS DECIMAL(9, 2)) AS a, CAST('1' AS DECIMAL(18, 2)) AS b, CAST('1' AS DECIMAL(28, 2)) AS c, CAST('1' AS DECIMAL(38, 2)) AS d, 1.5 AS e FROM (VALUES(1))")
			for i, name := range got {
				if name != "VARDECIMAL" {
					t.Errorf("the column %d of a DECIMAL is %s, want VARDECIMAL, never %s", i, name, typ)
				}
			}
		}})
	}
	out = append(out,
		refusedType{"BINARY", func(t *testing.T, _ principal, db *sql.DB) {
			if got := typeNames(t, db, "SELECT CAST('ab' AS BINARY(2)) AS b FROM (VALUES(1))"); !reflect.DeepEqual(got, []string{"VARBINARY"}) {
				t.Errorf("BINARY(2) is %v, want VARBINARY", got)
			}
		}},
		refusedType{"NULL", func(t *testing.T, _ principal, db *sql.DB) {
			// An untyped NULL is an INT, and its value is nil.
			rows := all(t, db, "SELECT NULL AS n FROM (VALUES(1))")
			if got := typeNames(t, db, "SELECT NULL AS n FROM (VALUES(1))"); !reflect.DeepEqual(got, []string{"INT"}) || !reflect.DeepEqual(rows, [][]any{{nil}}) {
				t.Errorf("an untyped NULL is %v with the value %v, want INT and nil", got, rows)
			}
		}},
		// A schema that names a MAP of strings and numbers still gives MAP, and
		// not DICT (recorded: "a dict from a provided schema").
		refusedType{"DICT", func(t *testing.T, p principal, db *sql.DB) {
			table := name(p, "dict")
			ctas(t, db, table, `SELECT convert_from('{"k":1}', 'JSON') AS m FROM (VALUES(1))`, jsonFormat)
			exec(t, db, "CREATE OR REPLACE SCHEMA (m MAP<VARCHAR, BIGINT>) FOR TABLE dfs.tmp.`"+table+"`")
			if got := typeNames(t, db, "SELECT m FROM dfs.tmp.`"+table+"`"); !reflect.DeepEqual(got, []string{"MAP"}) {
				t.Errorf("the column of a DICT schema is %v, want MAP", got)
			}
		}},
		// The option exec.enable_union_type changes nothing: a column that
		// changes its type between files keeps the first type, and gives NULL for
		// the second (recorded: "a type that changes, with the union type").
		refusedType{"UNION", func(t *testing.T, p principal, db *sql.DB) {
			dir := name(p, "union")
			ctas(t, db, dir+"/a", "SELECT CAST(1 AS BIGINT) AS x FROM (VALUES(1))", jsonFormat)
			ctas(t, db, dir+"/b", "SELECT 'text' AS x FROM (VALUES(1))", jsonFormat)
			union := drill.WithParameter("options", map[string]string{"exec.enable_union_type": "true", "drill.exec.http.rest.errors.verbose": "true"})
			rows := all(t, db, "SELECT x, typeof(x) AS t FROM dfs.tmp.`"+dir+"`", union)
			var nulls int
			for _, row := range rows {
				if row[0] == nil {
					nulls++
				}
			}
			if names := typeNames(t, db, "SELECT x FROM dfs.tmp.`"+dir+"`", union); len(rows) != 2 || nulls != 1 || !reflect.DeepEqual(names, []string{"BIGINT"}) {
				t.Errorf("with the union type the rows are %v and the type %v, want one NULL and BIGINT", rows, names)
			}
		}},
	)
	return out
}

// everyType is the query of the recorded table of every type: a row of the
// smallest values, a row of the largest values and a row of NULL.
const everyType = `SELECT id,
CASE id WHEN 1 THEN CAST(-2147483648 AS INT) WHEN 2 THEN CAST(2147483647 AS INT) END AS i,
CASE id WHEN 1 THEN CAST(-9223372036854775808 AS BIGINT) WHEN 2 THEN CAST(9223372036854775807 AS BIGINT) END AS b,
CASE id WHEN 1 THEN CAST(-3.4028235E38 AS FLOAT) WHEN 2 THEN CAST(1.4E-45 AS FLOAT) END AS f4,
CASE id WHEN 1 THEN CAST(-1.7976931348623157E308 AS DOUBLE) WHEN 2 THEN CAST(1.7976931348623157E308 AS DOUBLE) END AS f8,
CASE id WHEN 1 THEN CAST('-12345678901234567890.123456789012345678' AS DECIMAL(38,18)) WHEN 2 THEN CAST('0.000000000000000001' AS DECIMAL(38,18)) END AS d,
CASE id WHEN 1 THEN TRUE WHEN 2 THEN FALSE END AS bo,
CASE id WHEN 1 THEN 'é''"\ x' WHEN 2 THEN '' END AS s,
CASE id WHEN 1 THEN CAST('ab' AS VARBINARY) WHEN 2 THEN CAST('' AS VARBINARY) END AS vb,
CASE id WHEN 1 THEN DATE '0001-01-01' WHEN 2 THEN DATE '9999-12-31' END AS dt,
CASE id WHEN 1 THEN TIME '00:00:00' WHEN 2 THEN TIME '23:59:59.999' END AS t,
CASE id WHEN 1 THEN TIMESTAMP '1969-12-31 23:59:59.999' WHEN 2 THEN TIMESTAMP '9999-12-31 23:59:59.999' END AS ts,
CASE id WHEN 1 THEN INTERVAL '-1-2' YEAR TO MONTH WHEN 2 THEN INTERVAL '0' MONTH END AS iy,
CASE id WHEN 1 THEN INTERVAL '-1 02:03:04.5' DAY TO SECOND WHEN 2 THEN INTERVAL '0' SECOND END AS idy
FROM (VALUES (1), (2), (3)) AS v(id)`

// TestIntegrationTypes reads a table of every type through Rows.Scan, with a
// row of the smallest values, a row of the largest values and a row of NULL,
// as each principal. It holds the NULL of VARBINARY and of the intervals, which
// the round trip cannot write.
func TestIntegrationTypes(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		table := name(p, "types")
		ctas(t, db, table, everyType)
		rows := all(t, db, "SELECT * FROM dfs.tmp.`"+table+"` ORDER BY id")
		lo := []any{
			int64(1), int64(math.MinInt32), int64(math.MinInt64), -3.4028234663852886e38, -math.MaxFloat64,
			decimalOf("-12345678901234567890.123456789012345678"), true, "é'\"\\ x", []byte("ab"),
			dbimp.Date{Year: 1, Month: 1, Day: 1}, dbimp.LocalTime{}, ts(1969, 12, 31, 23, 59, 59, 999),
			dbimp.Interval{Months: -14}, dbimp.Interval{Days: -1, Nanoseconds: -7384500e6},
		}
		hi := []any{
			int64(2), int64(math.MaxInt32), int64(math.MaxInt64), 1.401298464324817e-45, math.MaxFloat64,
			decimalOf("1E-18"), false, "", []byte{},
			dbimp.Date{Year: 9999, Month: 12, Day: 31}, dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6}, ts(9999, 12, 31, 23, 59, 59, 999),
			dbimp.Interval{}, dbimp.Interval{},
		}
		null := make([]any, len(lo))
		null[0] = int64(3)
		want := [][]any{lo, hi, null}
		if len(rows) != len(want) {
			t.Fatalf("read %d rows, want %d", len(rows), len(want))
		}
		for i, row := range rows {
			for j, got := range row {
				if !reflect.DeepEqual(got, want[i][j]) {
					t.Errorf("row %d, column %d is %#v (%T), want %#v (%T)", i+1, j, got, got, want[i][j], want[i][j])
				}
			}
		}
		// The scan type of each column is the type of its values.
		rs, err := db.QueryContext(t.Context(), "SELECT * FROM dfs.tmp.`"+table+"`")
		if err != nil {
			t.Fatal(err)
		}
		defer rs.Close()
		cts, err := rs.ColumnTypes()
		if err != nil {
			t.Fatal(err)
		}
		for j, ct := range cts {
			if got := ct.ScanType(); got != reflect.TypeOf(lo[j]) {
				t.Errorf("the column %s has the scan type %v, want %T", ct.Name(), got, lo[j])
			}
		}
		for rs.Next() {
		}
		if err := rs.Err(); err != nil {
			t.Fatal(err)
		}
	})
}
