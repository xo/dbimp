package trino //nolint:testpackage // The tests set the capabilities that a recorded request sent, which only the package can.

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// recorded is the folder of the exchanges of step 6.
const recorded = "../testdata/trino"

// The releases that step 6 recorded.
var releases = []string{"trino-476", "trino-483", "presto-0.299"}

// flavorOf returns the flavor of a release, as dbrun names it.
func flavorOf(release string) string {
	if strings.HasPrefix(release, "presto") {
		return FlavorPresto
	}
	return FlavorTrino
}

// headerOf returns the value of the header that a request holds under the
// name of either flavor, such as X-Trino-Session or X-Presto-Session.
func headerOf(h http.Header, name string) string {
	return strings.Join(append(h.Values("X-Trino-"+name), h.Values("X-Presto-"+name)...), "|")
}

// match finds the recorded exchange of a request. It compares the method, the
// path and the query, which hold the id of a query and the token of a page,
// and so find each poll of the exchanges in order. A statement is the same text
// under many names, so a POST compares its headers of the session, the
// capabilities, the time zone and the transaction too, and takes only the
// exchanges that the server sent in plain text, because the replay does not
// compress.
func match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	if r.Method == http.MethodDelete {
		// The cancel of a query that the recordings did not cancel gets the
		// answer of one that they did.
		return ex.Request.Method == http.MethodDelete && ex.Response.Status == http.StatusNoContent
	}
	if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path || r.URL.RawQuery != ex.Request.Query {
		return false
	}
	if ex.Response.Header.Get("Content-Encoding") != "" {
		return false
	}
	if r.Method != http.MethodPost {
		return true
	}
	if string(body) != ex.Request.Body {
		return false
	}
	for _, name := range []string{"Client-Capabilities", "Session", "Prepared-Statement", "Transaction-Id", "Time-Zone"} {
		if headerOf(r.Header, name) != headerOf(ex.Request.Header, name) {
			return false
		}
	}
	return true
}

// replayDB opens a database on the exchanges of one release. The capabilities
// are those that the recorded request sent, and "" sends none.
func replayDB(t *testing.T, release, caps string) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, recorded, release, match)
	dsn := strings.Replace(srv.URL, "http://", "trino://trino@", 1) + "?flavor=" + flavorOf(release)
	cfg, err := ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	c := NewConnector(*cfg)
	c.caps = &caps
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	return db
}

// answer is the answer to a query: the names of the types of its columns, and
// its rows.
type answer struct {
	types []string
	rows  [][]any
}

// queryAll runs the query and reads every row, with each column into a *any.
func queryAll(t *testing.T, db *sql.DB, query string) answer {
	t.Helper()
	res, err := queryRows(t, db, query)
	if err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
	return res
}

// queryRows is queryAll that returns the error, which is the error of the
// query or of any row.
func queryRows(t *testing.T, db *sql.DB, query string) (answer, error) {
	t.Helper()
	var res answer
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		return res, err
	}
	for _, ct := range cts {
		res.types = append(res.types, ct.DatabaseTypeName())
	}
	for rows.Next() {
		row := make([]any, len(cts))
		ptrs := make([]any, len(cts))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return res, err
		}
		res.rows = append(res.rows, row)
	}
	return res, rows.Err()
}

// dec returns the decimal that text writes.
func dec(t *testing.T, text string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(text)
	if err != nil {
		t.Fatalf("reading %q as a decimal: %v", text, err)
	}
	return d
}

// same reports whether a value read equals the value wanted. A NaN equals a
// NaN, and a time is the same instant in the same offset.
func same(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case time.Time:
		g, ok := got.(time.Time)
		if !ok {
			return false
		}
		_, goff := g.Zone()
		_, woff := w.Zone()
		return g.Equal(w) && goff == woff
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !same(g[i], w[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// check compares the first row of an answer with want, and its types with the
// names that types holds, if it is not nil.
func check(t *testing.T, got answer, types []string, want []any) {
	t.Helper()
	if len(got.rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(got.rows))
	}
	if types != nil && !reflect.DeepEqual(got.types, types) {
		t.Errorf("the types are %v, want %v", got.types, types)
	}
	if len(got.rows[0]) != len(want) {
		t.Fatalf("got %d values, want %d", len(got.rows[0]), len(want))
	}
	for i, w := range want {
		if g := got.rows[0][i]; !same(g, w) {
			t.Errorf("column %d is %#v (%T), want %#v (%T)", i, g, g, w, w)
		}
	}
}

// uuidOf is the UUID that the recordings use.
var uuidOf = uuid.UUID{0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78}

// date, ltime and ldt build the values of the root package.
func date(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
func ltime(h, m, s, ns int) dbimp.LocalTime {
	return dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: ns}
}
func ldt(d dbimp.Date, tm dbimp.LocalTime) dbimp.LocalDateTime {
	return dbimp.LocalDateTime{Date: d, Time: tm}
}

// TestReplayTypes reads each type from the recorded exchanges of every
// release, through the real decoder, into a *any, and holds the Go type and the
// value (D135 and D175). Presto sends an array, a map and a row as strings of
// text, and the decoder gives the same values as for Trino.
func TestReplayTypes(t *testing.T) { //nolint:maintidx // One table of the values of every type.
	t.Parallel()
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			d := func(text string) *apd.Decimal { return dec(t, text) }
			db := replayDB(t, rel, "")
			check(t, queryAll(t, db, "SELECT TRUE, TINYINT '-128', SMALLINT '32767', INTEGER '-2147483648', BIGINT '9223372036854775807', BIGINT '-9223372036854775808', REAL '1.5', DOUBLE '1.7976931348623157E308', DECIMAL '12345678901234567890123456789012345678', DECIMAL '-0.5', DECIMAL '1234567890123456789012345678901234567.8', CHAR 'ab', VARCHAR 'é''\"\\ x', X'00ff', X'', ''"),
				[]string{"BOOLEAN", "TINYINT", "SMALLINT", "INTEGER", "BIGINT", "BIGINT", "REAL", "DOUBLE", "DECIMAL", "DECIMAL", "DECIMAL", "CHAR", "VARCHAR", "VARBINARY", "VARBINARY", "VARCHAR"},
				[]any{true, int64(-128), int64(32767), int64(-2147483648), int64(9223372036854775807), int64(-9223372036854775808), 1.5, 1.7976931348623157e+308,
					d("12345678901234567890123456789012345678"), d("-0.5"), d("1234567890123456789012345678901234567.8"), "ab", "é'\"\\ x", []byte{0, 0xff}, []byte{}, ""})
			check(t, queryAll(t, db, "SELECT CAST('NaN' AS double), CAST('Infinity' AS double), CAST('-Infinity' AS double), CAST('NaN' AS real), -0e0, REAL '3.4028235E38', DOUBLE '4.9E-324', REAL '-0e0'"),
				[]string{"DOUBLE", "DOUBLE", "DOUBLE", "REAL", "DOUBLE", "REAL", "DOUBLE", "REAL"},
				[]any{math.NaN(), math.Inf(1), math.Inf(-1), math.NaN(), math.Copysign(0, -1), 3.4028235e+38, 5e-324, math.Copysign(0, -1)})
			check(t, queryAll(t, db, "SELECT INTERVAL '1-2' YEAR TO MONTH, INTERVAL '3 04:05:06.789' DAY TO SECOND, INTERVAL '-1' DAY, JSON '{\"a\":[1,null]}', JSON 'null', JSON '\"x\"', UUID '12345678-1234-5678-1234-567812345678', IPADDRESS '10.0.0.1', IPADDRESS '::1'"),
				[]string{"INTERVAL YEAR TO MONTH", "INTERVAL DAY TO SECOND", "INTERVAL DAY TO SECOND", "JSON", "JSON", "JSON", "UUID", "IPADDRESS", "IPADDRESS"},
				[]any{dbimp.Interval{Months: 14}, dbimp.Interval{Days: 3, Nanoseconds: 14706789000000}, dbimp.Interval{Days: -1},
					map[string]any{"a": []any{int64(1), nil}}, nil, "x", uuidOf, "10.0.0.1", "::1"})
			check(t, queryAll(t, db, "SELECT ARRAY[1, NULL, 3], MAP(ARRAY['a', 'b'], ARRAY[1, 2]), MAP(ARRAY[1, 2], ARRAY['x', 'y']), CAST(ROW(1, 'x', NULL) AS ROW(a integer, b varchar, c double)), ROW(1, 2), ARRAY[ARRAY[1], ARRAY[2, 3]], CAST(NULL AS array(integer)), ARRAY[DATE '2026-10-01'], ARRAY[TIMESTAMP '2026-10-01 12:34:56.789'], ARRAY[CAST(1.5 AS DECIMAL(5,2))], ARRAY[X'00ff'], MAP(ARRAY[DATE '2026-10-01'], ARRAY[1])"),
				[]string{"ARRAY", "MAP", "MAP", "ROW", "ROW", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "MAP"},
				[]any{[]any{int64(1), nil, int64(3)}, map[string]any{"a": int64(1), "b": int64(2)}, map[string]any{"1": "x", "2": "y"}, []any{int64(1), "x", nil}, []any{int64(1), int64(2)},
					[]any{[]any{int64(1)}, []any{int64(2), int64(3)}}, nil, []any{date(2026, 10, 1)}, []any{ldt(date(2026, 10, 1), ltime(12, 34, 56, 789e6))},
					[]any{d("1.50")}, []any{[]byte{0, 0xff}}, map[string]any{"2026-10-01": int64(1)}})
			check(t, queryAll(t, db, "SELECT '', X'', ARRAY[], MAP(), CAST(ARRAY[] AS array(integer)), CAST(MAP() AS map(varchar, integer)), CAST('' AS char(3))"),
				nil, []any{"", []byte{}, []any{}, map[string]any{}, []any{}, map[string]any{}, "   "})
			check(t, queryAll(t, db, "SELECT ST_Point(1, 2), to_spherical_geography(ST_Point(1, 2)), ST_GeometryFromText('POLYGON ((0 0, 1 0, 1 1, 0 0))')"),
				[]string{"GEOMETRY", "SPHERICALGEOGRAPHY", "GEOMETRY"}, []any{"POINT (1 2)", "POINT (1 2)", "POLYGON ((0 0, 1 0, 1 1, 0 0))"})
			check(t, queryAll(t, db, "SELECT CAST(NULL AS boolean), CAST(NULL AS bigint), CAST(NULL AS double), CAST(NULL AS decimal(10,2)), CAST(NULL AS varchar), CAST(NULL AS varbinary), CAST(NULL AS date), CAST(NULL AS timestamp), CAST(NULL AS timestamp with time zone), CAST(NULL AS json), CAST(NULL AS uuid), CAST(NULL AS ipaddress), CAST(NULL AS map(varchar, integer)), CAST(NULL AS row(a integer)), CAST(NULL AS interval day to second), NULL"),
				nil, make([]any, 16))
			for _, tt := range []struct{ name, query string }{
				{"HYPERLOGLOG", "SELECT approx_set(CAST(1 AS bigint))"},
				{"P4HYPERLOGLOG", "SELECT CAST(approx_set(CAST(1 AS bigint)) AS P4HyperLogLog)"},
				{"SETDIGEST", "SELECT make_set_digest(CAST(1 AS bigint))"},
				{"QDIGEST", "SELECT qdigest_agg(CAST(1 AS bigint))"},
				{"TDIGEST", "SELECT tdigest_agg(CAST(1 AS double))"},
			} {
				got := queryAll(t, db, tt.query)
				if len(got.rows) != 1 || got.types[0] != tt.name {
					t.Errorf("%s: got %v", tt.name, got)
					continue
				}
				if b, ok := got.rows[0][0].([]byte); !ok || len(b) == 0 {
					t.Errorf("%s: got %#v, want bytes", tt.name, got.rows[0][0])
				}
			}
		})
	}
}

// TestReplayTimes holds the dates and the times. The values are the same on
// each release, because the driver sends the capability that keeps the digits
// (D175).
func TestReplayTimes(t *testing.T) {
	t.Parallel()
	const query = "SELECT DATE '2026-10-01', DATE '9999-12-31', TIME '12:34:56.789', TIME '12:34:56.789 +05:30', TIMESTAMP '2026-10-01 12:34:56.789', TIMESTAMP '2026-10-01 12:34:56.789 Asia/Jakarta', TIMESTAMP '2026-10-01 12:34:56.789 +05:30', TIMESTAMP '1969-12-31 23:59:59.999', TIMESTAMP '0001-01-01 00:00:00.000', TIMESTAMP '9999-12-31 23:59:59.999'"
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skipf("the host has no zone data: %v", err)
	}
	want := []any{
		date(2026, 10, 1), date(9999, 12, 31), ltime(12, 34, 56, 789e6), dbimp.OffsetTime{Time: ltime(12, 34, 56, 789e6), Offset: 5*3600 + 30*60},
		ldt(date(2026, 10, 1), ltime(12, 34, 56, 789e6)),
		time.Date(2026, 10, 1, 12, 34, 56, 789e6, jakarta), time.Date(2026, 10, 1, 12, 34, 56, 789e6, time.FixedZone("", 5*3600+30*60)),
		ldt(date(1969, 12, 31), ltime(23, 59, 59, 999e6)), ldt(date(1, 1, 1), ltime(0, 0, 0, 0)), ldt(date(9999, 12, 31), ltime(23, 59, 59, 999e6)),
	}
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			db := replayDB(t, rel, "")
			check(t, queryAll(t, db, query),
				[]string{"DATE", "DATE", "TIME", "TIME WITH TIME ZONE", "TIMESTAMP", "TIMESTAMP WITH TIME ZONE", "TIMESTAMP WITH TIME ZONE", "TIMESTAMP", "TIMESTAMP", "TIMESTAMP"}, want)
			check(t, queryAll(t, db, "SELECT DATE '-0001-01-01'"), []string{"DATE"}, []any{date(-1, 1, 1)})
		})
	}
	t.Run("a name that has an offset in summer", func(t *testing.T) {
		t.Parallel()
		ny, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skipf("the host has no zone data: %v", err)
		}
		db := replayDB(t, "trino-476", "")
		check(t, queryAll(t, db, "SELECT CAST(TIMESTAMP '2026-10-01 12:00:00 UTC' AS timestamp with time zone) AT TIME ZONE 'America/New_York'"),
			nil, []any{time.Date(2026, 10, 1, 8, 0, 0, 0, ny)})
	})
	t.Run("a time with a fraction of twelve digits", func(t *testing.T) {
		t.Parallel()
		// Trino keeps twelve digits with the capability, and the driver fails
		// for a digit beyond the ninth that is not zero (D175). The query is
		// the one that step 6 recorded with the capability.
		db := replayDB(t, "trino-476", "PARAMETRIC_DATETIME")
		_, err := queryRows(t, db, "SELECT TIME '12:34:56.123456789012', TIME '12:34:56.1234 +05:30', TIMESTAMP '2026-10-01 12:34:56.123456789012', TIMESTAMP '2026-10-01 12:34:56.123456789012 UTC', TIMESTAMP '2026-10-01 12:34:56.789 +05:30', TIMESTAMP '1969-12-31 23:59:59.999999999999', TIMESTAMP '0001-01-01 00:00:00', TIMESTAMP '9999-12-31 23:59:59.999999999999'")
		if !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("the error is %v, want one that wraps dbimp.ErrInvalidValue", err)
		}
	})
	t.Run("a time with a fraction of twelve digits on Presto", func(t *testing.T) {
		t.Parallel()
		db := replayDB(t, "presto-0.299", "")
		_, err := queryRows(t, db, "SELECT TIME '12:34:56.123456789012', TIMESTAMP '2026-10-01 12:34:56.123456789012', TIMESTAMP '2026-10-01 12:34:56.123456789012 UTC'")
		var perr *Error
		if !errors.As(err, &perr) || perr.Name != "SYNTAX_ERROR" || !strings.Contains(perr.Message, "not a valid time literal") {
			t.Errorf("the error is %v, want the SYNTAX_ERROR of the server", err)
		}
	})
}

// TestReplayNumberAndVariant holds the two types of Trino 483, which the
// driver asks for with the capabilities NUMBER and VARIANT (D175). Without the
// capabilities, a NUMBER is a varchar and a VARIANT is a json (measured).
func TestReplayNumberAndVariant(t *testing.T) {
	t.Parallel()
	const (
		number  = "SELECT CAST(1 AS NUMBER), CAST('1e1000' AS NUMBER), CAST('NaN' AS NUMBER), CAST(NULL AS NUMBER)"
		variant = "SELECT CAST(1 AS VARIANT), CAST(NULL AS VARIANT), CAST(JSON '{\"a\":[1,\"x\"]}' AS VARIANT)"
	)
	d := func(text string) *apd.Decimal { return dec(t, text) }
	db := replayDB(t, "trino-483", "NUMBER")
	check(t, queryAll(t, db, number), []string{"NUMBER", "NUMBER", "NUMBER", "NUMBER"}, []any{d("1"), d("1E+1000"), d("NaN"), nil})
	db = replayDB(t, "trino-483", "VARIANT")
	check(t, queryAll(t, db, variant), []string{"VARIANT", "VARIANT", "VARIANT"}, []any{int64(1), nil, map[string]any{"a": []any{int64(1), "x"}}})
	db = replayDB(t, "trino-483", "")
	check(t, queryAll(t, db, number), []string{"VARCHAR", "VARCHAR", "VARCHAR", "VARCHAR"}, []any{"1", "1E+1000", "NaN", nil})
	check(t, queryAll(t, db, variant), []string{"JSON", "JSON", "JSON"}, []any{int64(1), nil, map[string]any{"a": []any{int64(1), "x"}}})
}

// columnsOf returns the columns of a query, which it reads to the end.
func columnsOf(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols
}

// TestReplayColumns holds D18: the names and the order of the columns come from
// the columns of the page, a name can repeat, and a statement with no rows
// still has its columns.
func TestReplayColumns(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			db := replayDB(t, rel, "")
			for _, tt := range []struct {
				query string
				want  []string
			}{
				{"SELECT 1 AS a, 2 AS a, 3, 'x' AS \"Mixed Case\"", []string{"a", "a", "_col2", "Mixed Case"}},
				{"SELECT s, n, z FROM dbimp_big WHERE n = 1", []string{"s", "n", "z"}},
			} {
				if cols := columnsOf(t, db, tt.query); !reflect.DeepEqual(cols, tt.want) {
					t.Errorf("the columns of %q are %q, want %q, in that order", tt.query, cols, tt.want)
				}
			}
			got, err := queryRows(t, db, "SELECT n, z, s FROM dbimp_big WHERE n < 0")
			if err != nil || len(got.rows) != 0 || !reflect.DeepEqual(got.types, []string{"BIGINT", "BIGINT", "VARCHAR"}) {
				t.Errorf("a statement with no rows gave %v, %v, want its three types and no row", got, err)
			}
		})
	}
}

// TestReplayPaging reads results of many pages, which the servers send with
// the nextUri of each, and holds every row in its order (D21).
func TestReplayPaging(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			db := replayDB(t, rel, "")
			got := queryAll(t, db, "SELECT n, z, s FROM dbimp_big ORDER BY n")
			if len(got.rows) != 3000 {
				t.Fatalf("got %d rows, want 3000", len(got.rows))
			}
			for i, row := range got.rows {
				if n := int64(i + 1); row[0] != n || row[1] != 400-n {
					t.Fatalf("row %d is %v, want n %d and z %d", i, row[:2], n, 400-n)
				}
			}
			got = queryAll(t, db, "SELECT a.n * 20 + b.m AS n FROM UNNEST(sequence(1, 1000)) AS a(n) CROSS JOIN UNNEST(sequence(1, 20)) AS b(m)")
			if len(got.rows) != 20000 {
				t.Errorf("got %d rows, want 20000", len(got.rows))
			}
		})
	}
}

// TestReplayErrors holds D21 and D107. A statement that fails answers HTTP 200
// and a page with an error. Before the first row it is an ordinary error, and
// after some rows it wraps dbimp.ErrIncomplete.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			db := replayDB(t, rel, "")
			for _, tt := range []struct {
				query string
				name  string
			}{
				{"SELEC 1", "SYNTAX_ERROR"},
				{"SELECT 1/0", "DIVISION_BY_ZERO"},
				{"SELECT BIGINT '9223372036854775807' + 1", "NUMERIC_VALUE_OUT_OF_RANGE"},
				{"SELECT CAST('x' AS integer)", "INVALID_CAST_ARGUMENT"},
			} {
				_, err := queryRows(t, db, tt.query)
				var perr *Error
				switch {
				case !errors.As(err, &perr) || perr.Name != tt.name:
					t.Errorf("%s: the error is %v, want %s", tt.query, err, tt.name)
				case errors.Is(err, dbimp.ErrIncomplete):
					t.Errorf("%s: the error comes before any row, and wraps dbimp.ErrIncomplete (D107)", tt.query)
				case perr.HTTPStatus != 0 || perr.Type != "USER_ERROR":
					t.Errorf("%s: the error is %+v, want one from the body of a page", tt.query, perr)
				}
			}
			_, err := queryRows(t, db, "SELECT * FROM dbimp_nosuch")
			want := "TABLE_NOT_FOUND"
			if rel == "presto-0.299" {
				want = "SYNTAX_ERROR"
			}
			if perr, ok := errors.AsType[*Error](err); !ok || perr.Name != want {
				t.Errorf("a table that does not exist: the error is %v, want %s", err, want)
			}
			got, err := queryRows(t, db, "SELECT n, rpad('x', 200, 'y') AS s, 1/(n-2900) AS r FROM dbimp_big")
			perr, ok := errors.AsType[*Error](err)
			if !ok || perr.Name != "DIVISION_BY_ZERO" || !errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("an error after some rows is %v, want DIVISION_BY_ZERO that wraps dbimp.ErrIncomplete", err)
			}
			if len(got.rows) == 0 {
				t.Errorf("an error after some rows came with no rows")
			}
		})
	}
}

// TestReplayWrites holds the result of Exec. A statement that writes answers
// updateCount, and DDL answers none (measured).
func TestReplayWrites(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			db := replayDB(t, rel, "")
			res, err := db.ExecContext(t.Context(), "INSERT INTO dbimp_crud VALUES (100, 'columns')")
			if err != nil {
				t.Fatal(err)
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				t.Errorf("the rows affected are %d, %v, want 1", n, err)
			}
			if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
			}
			res, err = db.ExecContext(t.Context(), "CREATE TABLE dbimp_ctas AS SELECT 1 AS id")
			if err != nil {
				t.Fatal(err)
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				t.Errorf("a CREATE TABLE AS affected %d rows, %v, want 1", n, err)
			}
			res, err = db.ExecContext(t.Context(), "CREATE TABLE dbimp_crud (id integer, v varchar)")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
				t.Errorf("DDL gave a count, %v, want dbimp.ErrNotSupported", err)
			}
		})
	}
}

// TestReplayFlavor holds D175: with no key flavor, the driver asks GET
// /v1/info, and the version of Presto has a dash and a commit.
func TestReplayFlavor(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			srv := dbimptest.ReplayRelease(t, recorded, rel, match)
			cfg, err := ParseDSN(strings.Replace(srv.URL, "http://", "trino://trino@", 1))
			if err != nil {
				t.Fatal(err)
			}
			c := NewConnector(*cfg)
			db := sql.OpenDB(c)
			defer db.Close()
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
			if got := c.Flavor(); got != flavorOf(rel) {
				t.Errorf("the flavor is %q, want %q", got, flavorOf(rel))
			}
		})
	}
}
