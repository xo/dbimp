package neo4j_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/neo4j"
)

// These tests replay the exchanges that step 6 recorded from real servers,
// under testdata/neo4j/. Each one decodes a real response through the
// driver.

const testdata = "../testdata/neo4j"

// The releases that step 6 recorded.
const (
	floor   = "neo4j-5.26.31"
	ceiling = "neo4j-2026.09.0"
)

var releases = []string{floor, ceiling}

// tagRE matches the comment that names the connection, at the end of a
// statement (D95).
var tagRE = regexp.MustCompile(`\n// dbimp:[A-Z2-7]+$`)

// match matches a request of the driver with a recorded one. It removes the
// comment of the connection and txMetadata, whose values change on each run,
// and reads a typed parameter and a plain one as the same value. A request
// recorded with a plain JSON Accept, or with JSON Lines, is never a match,
// because the driver asks for typed JSON (D62).
func match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path || r.URL.RawQuery != ex.Request.Query {
		return false
	}
	if accept := ex.Request.Header.Get("Accept"); accept != "" &&
		(!strings.HasPrefix(accept, "application/vnd.neo4j.query") || strings.Contains(accept, "jsonl")) {
		return false
	}
	return canon(body) == canon(ex.Request.Content())
}

// canon returns the canonical form of the body of a request.
func canon(b []byte) string {
	if len(bytes.TrimSpace(b)) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	if st, ok := m["statement"].(string); ok {
		m["statement"] = tagRE.ReplaceAllString(st, "")
	}
	delete(m, "txMetadata")
	if p, ok := m["parameters"].(map[string]any); ok {
		for k, v := range p {
			p[k] = plain(v)
		}
	}
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	return string(out)
}

// plain returns a typed value in the form of plain JSON where plain JSON has
// one, and any other value as it is.
func plain(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = plain(x[i])
		}
		return x
	case map[string]any:
		typ, ok := x["$type"].(string)
		if !ok || len(x) != 2 {
			for k := range x {
				x[k] = plain(x[k])
			}
			return x
		}
		val := x["_value"]
		switch typ {
		case "Integer", "Float":
			if s, ok := val.(string); ok {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					return f
				}
			}
		case "String", "Boolean":
			return val
		case "Null":
			return nil
		case "List", "Map":
			return plain(val)
		}
		return x
	}
	return v
}

// open opens the driver against the server at an http:// URL, in the
// database dbmeta, with the query of a DSN, such as "?cancel=none".
func open(t *testing.T, url, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open("neo4j", "neo4j://neo4j:pw@"+strings.TrimPrefix(url, "http://")+"/dbmeta"+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// replayDB opens the driver against the recordings of one release.
func replayDB(t *testing.T, release string) *sql.DB {
	t.Helper()
	return open(t, dbimptest.ReplayRelease(t, testdata, release, match).URL, "")
}

// exchanges returns the recorded exchanges of release, in the order that
// they were recorded, with the name of the file of each.
func exchanges(t *testing.T, release string) ([]*dbimptest.Exchange, []string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, release+"-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exs []*dbimptest.Exchange
	var names []string
	for _, p := range paths {
		ex, err := dbimptest.ReadExchange(p)
		if err != nil {
			t.Fatal(err)
		}
		exs, names = append(exs, ex), append(names, filepath.Base(p))
	}
	return exs, names
}

// replayOnly opens the driver against the exchanges of release that pick
// takes, for a test of one of several answers to the same request.
func replayOnly(t *testing.T, release, query string, pick func(ex *dbimptest.Exchange) bool) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	exs, names := exchanges(t, release)
	n := 0
	for i, ex := range exs {
		if !pick(ex) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(testdata, names[i]))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, names[i]), b, 0o600); err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n == 0 {
		t.Fatalf("no exchange of %s is picked", release)
	}
	return open(t, dbimptest.Replay(t, dir, match).URL, query)
}

// statementOf returns the statement of the request of ex, or "".
func statementOf(ex *dbimptest.Exchange) string {
	var b struct {
		Statement string `json:"statement"`
	}
	_ = json.Unmarshal(ex.Request.Content(), &b)
	return b.Statement
}

// replayTx opens the driver against one transaction of release: the one
// whose first statement is statement, with its begin and its end, as the
// administrator recorded it.
func replayTx(t *testing.T, release, statement string) *sql.DB {
	t.Helper()
	exs, _ := exchanges(t, release)
	var id string
	for _, ex := range exs {
		if before, after, ok := strings.Cut(ex.Request.Path, "/query/v2/tx/"); ok && before != "" && statementOf(ex) == statement {
			id, _, _ = strings.Cut(after, "/")
			break
		}
	}
	if id == "" {
		t.Fatalf("no transaction of %s runs %q", release, statement)
	}
	return replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
		if strings.HasSuffix(ex.Request.Path, "/query/v2/tx") {
			return strings.Contains(string(ex.Response.Content()), `"id":"`+id+`"`)
		}
		return strings.Contains(ex.Request.Path, "/query/v2/tx/"+id)
	})
}

// rowsOf runs query and returns its columns and every row, read into *any.
func rowsOf(t *testing.T, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, query string, args ...any,
) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		row := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range dest {
			dest[i] = &row[i]
		}
		if err := rows.Scan(dest...); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

// row returns the one row of query, by the name of each column.
func row(t *testing.T, db *sql.DB, query string, args ...any) map[string]any {
	t.Helper()
	cols, rows, err := rowsOf(t, db, query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s gave %d rows, want 1", query, len(rows))
	}
	m := map[string]any{}
	for i, c := range cols {
		m[c] = rows[0][i]
	}
	return m
}

// check compares got with want, by name.
func check(t *testing.T, label string, got, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if !reflect.DeepEqual(got[k], v) {
			t.Errorf("%s: %s is %#v (%T), want %#v (%T)", label, k, got[k], got[k], v, v)
		}
	}
}

// serverError returns the error of the server in err, and fails the test if
// there is none.
func serverError(t *testing.T, err error) *neo4j.ResponseError {
	t.Helper()
	re, ok := errors.AsType[*neo4j.ResponseError](err)
	if !ok {
		t.Fatalf("the error is %v, want a *neo4j.ResponseError", err)
	}
	if len(re.Errs) == 0 {
		t.Fatalf("the error %v holds no error of the server", err)
	}
	return re
}

func oslo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skipf("the system has no zone Europe/Oslo: %v", err)
	}
	return loc
}

func TestReplayColumnOrder(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		cols, rows, err := rowsOf(t, replayDB(t, release), "RETURN 1 AS b, 2 AS a, 3 AS c")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if want := []string{"b", "a", "c"}; !slices.Equal(cols, want) {
			t.Errorf("%s: the columns are %q, want %q (D66)", release, cols, want)
		}
		if want := []any{int64(1), int64(2), int64(3)}; !reflect.DeepEqual(rows[0], want) {
			t.Errorf("%s: the row is %#v, want %#v", release, rows[0], want)
		}
	}
}

func TestReplayTypes(t *testing.T) {
	t.Parallel()
	loc := oslo(t)
	const query = "RETURN 9223372036854775807 AS imax, -9223372036854775808 AS imin, 0.1 AS f, 1.5e300 AS fbig, " +
		"'héllo' AS s, true AS b, null AS n, [1, 'a', null] AS l, {k: 1, a: [2]} AS m, date('2026-09-27') AS d, " +
		"localtime('12:50:35.556123456') AS lt, time('12:50:35.556+01:00') AS zt, localdatetime('2026-09-27T10:00:00.123456789') AS ldt, " +
		"datetime('2026-09-27T10:00:00.123456789+02:00') AS odt, datetime('2026-09-27T10:00:00[Europe/Oslo]') AS zdt, " +
		"duration('P1Y2M3DT4H5M6.007S') AS dur, point({x: 1.5, y: 2.5}) AS p2, point({longitude: 10.7, latitude: 59.9, height: 3}) AS p3"
	want := map[string]any{
		"imax": int64(math.MaxInt64), "imin": int64(math.MinInt64), "f": 0.1, "fbig": 1.5e300,
		"s": "héllo", "b": true, "n": nil,
		"l":   []any{int64(1), "a", nil},
		"m":   map[string]any{"k": int64(1), "a": []any{int64(2)}},
		"d":   dbimp.Date{Year: 2026, Month: 9, Day: 27},
		"lt":  dbimp.LocalTime{Hour: 12, Minute: 50, Second: 35, Nanosecond: 556123456},
		"ldt": dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 9, Day: 27}, Time: dbimp.LocalTime{Hour: 10, Minute: 0, Second: 0, Nanosecond: 123456789}},
		"dur": dbimp.Interval{Months: 14, Days: 3, Nanoseconds: 14706007000000},
		"p2":  neo4j.Point{SRID: 7203, X: 1.5, Y: 2.5, Dims: 2},
		"p3":  neo4j.Point{SRID: 4979, X: 10.7, Y: 59.9, Z: 3, Dims: 3},
	}
	for _, release := range releases {
		got := row(t, replayDB(t, release), query)
		check(t, release, got, want)
		// A time.Time of a fixed zone holds a pointer, so these compare by
		// their instant and their offset.
		for name, w := range map[string]time.Time{
			"zt":  time.Date(0, 1, 1, 12, 50, 35, 556e6, time.FixedZone("", 3600)),
			"odt": time.Date(2026, 9, 27, 10, 0, 0, 123456789, time.FixedZone("", 7200)),
			"zdt": time.Date(2026, 9, 27, 10, 0, 0, 0, loc),
		} {
			var g time.Time
			switch v := got[name].(type) {
			case dbimp.OffsetTime:
				g = v.ToTime()
			case time.Time:
				g = v
			default:
				t.Errorf("%s: %s is %T, want a time", release, name, got[name])
				continue
			}
			_, goff := g.Zone()
			_, woff := w.Zone()
			if !g.Equal(w) || goff != woff {
				t.Errorf("%s: %s is %v, want %v", release, name, g, w)
			}
		}
		if _, ok := got["zt"].(dbimp.OffsetTime); !ok {
			t.Errorf("%s: zt is %T, want dbimp.OffsetTime (D139)", release, got["zt"])
		}
		if zdt, ok := got["zdt"].(time.Time); !ok || zdt.Location().String() != "Europe/Oslo" {
			t.Errorf("%s: zdt is %#v, want a time.Time in Europe/Oslo (D63)", release, got["zdt"])
		}
	}
}

func TestReplayDatesAtTheirLimits(t *testing.T) {
	t.Parallel()
	got := row(t, replayDB(t, floor), "RETURN date('+999999999-12-31') AS dmax, date('-999999999-01-01') AS dmin, datetime('1600-01-01T00:00:00Z') AS old")
	check(t, floor, got, map[string]any{
		"dmax": dbimp.Date{Year: 999999999, Month: 12, Day: 31},
		"dmin": dbimp.Date{Year: -999999999, Month: 1, Day: 1},
		"old":  time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if d, ok := got["dmax"].(dbimp.Date); !ok || d.String() != "+999999999-12-31" {
		t.Errorf("dmax is %#v, want the Date whose String is the form of the server", got["dmax"])
	}
}

func TestReplayEdgeForms(t *testing.T) {
	t.Parallel()
	const query = "RETURN localtime('12:00') AS a, time('12:00Z') AS b, time('12:00:00.5-05:30') AS c, datetime('2026-01-01T00:00Z') AS d, " +
		"localdatetime('2026-01-01T00:00') AS e, duration('PT0S') AS f, duration({months: -14, days: -3, seconds: -5, nanoseconds: -7}) AS g, " +
		"duration('PT-0.5S') AS h, date('0001-01-01') AS i, date('-0001-06-01') AS j, date('+10000-01-01') AS k, point({x: 1, y: 2, z: 3}) AS l, " +
		"point({longitude: 1.5, latitude: 2.5}) AS m, datetime('2026-07-01T00:00[America/New_York]') AS n, " +
		"datetime({year: 2026, month: 1, day: 1, timezone: '+01:30:15'}) AS o, time({hour: 1, timezone: 'Europe/Oslo'}) AS p, " +
		"duration({seconds: 1, nanoseconds: -1}) AS q, point({x: 0.1, y: 1e300}) AS r, duration('P3D') AS s, duration('PT-3700S') AS u"
	for _, release := range releases {
		got := row(t, replayDB(t, release), query)
		check(t, release, got, map[string]any{
			"a": dbimp.LocalTime{Hour: 12, Minute: 0, Second: 0, Nanosecond: 0},
			"b": dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12}},
			"d": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			"e": dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 1, Day: 1}, Time: dbimp.LocalTime{Hour: 0, Minute: 0, Second: 0, Nanosecond: 0}},
			"f": dbimp.Interval{},
			"g": dbimp.Interval{Months: -14, Days: -3, Nanoseconds: -5000000007},
			"h": dbimp.Interval{Nanoseconds: -500000000},
			"i": dbimp.Date{Year: 1, Month: 1, Day: 1},
			"j": dbimp.Date{Year: -1, Month: 6, Day: 1},
			"k": dbimp.Date{Year: 10000, Month: 1, Day: 1},
			"l": neo4j.Point{SRID: 9157, X: 1, Y: 2, Z: 3, Dims: 3},
			"m": neo4j.Point{SRID: 4326, X: 1.5, Y: 2.5, Dims: 2},
			"q": dbimp.Interval{Nanoseconds: 999999999},
			"r": neo4j.Point{SRID: 7203, X: 0.1, Y: 1e300, Dims: 2},
			"s": dbimp.Interval{Days: 3},
			"u": dbimp.Interval{Nanoseconds: -3700000000000},
		})
		// Each value writes itself in the form that the server sent.
		for name, want := range map[string]string{
			"a": "12:00:00", "b": "12:00:00Z", "c": "12:00:00.5-05:30", "f": "PT0S",
			"g": "P-1Y-2M-3DT-5.000000007S", "h": "PT-0.5S", "i": "0001-01-01", "j": "-0001-06-01",
			"k": "+10000-01-01", "q": "PT0.999999999S", "s": "P3D", "u": "PT-1H-1M-40S",
			"p": "01:00:00+02:00",
		} {
			if s, ok := got[name].(fmt.Stringer); !ok || s.String() != want {
				t.Errorf("%s: %s is %#v, want a value whose String is %q", release, name, got[name], want)
			}
		}
		n, ok := got["n"].(time.Time)
		if _, off := n.Zone(); !ok || off != -4*3600 || n.Location().String() != "America/New_York" {
			t.Errorf("%s: n is %#v, want a time.Time in America/New_York at -04:00", release, got["n"])
		}
		o, ok := got["o"].(time.Time)
		if _, off := o.Zone(); !ok || off != 3600+30*60+15 {
			t.Errorf("%s: o is %#v, want an offset of +01:30:15", release, got["o"])
		}
	}
}

func TestReplaySpecialFloats(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		got := row(t, replayDB(t, release), "RETURN 0.0 / 0.0 AS nan, 1.0 / 0.0 AS inf, -0.0 AS nz")
		nan, _ := got["nan"].(float64)
		inf, _ := got["inf"].(float64)
		nz, _ := got["nz"].(float64)
		if !math.IsNaN(nan) || !math.IsInf(inf, 1) || nz != 0 || !math.Signbit(nz) {
			t.Errorf("%s: the floats are %v, want NaN, +Inf and -0", release, got)
		}
	}
}

func TestReplayByteArray(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		got := row(t, replayDB(t, release), "RETURN $b AS b", sql.Named("b", []byte{0xde, 0xad, 0xbe, 0xef}))
		check(t, release, got, map[string]any{"b": []byte{0xde, 0xad, 0xbe, 0xef}})
	}
}

func TestReplayNull(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayDB(t, release)
		got := row(t, db, "CREATE (n:Dbimp {k: 'mp'}) RETURN n.k AS k, n.nothing AS missing")
		check(t, release, got, map[string]any{"k": "mp", "missing": nil})
	}
}

func TestReplayGraph(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		got := row(t, replayDB(t, release), "CREATE p = (a:Dbimp {k: 'n1'})-[r:DBIMP_R {w: 2}]->(b:Dbimp {k: 'n2'}) RETURN a, r, p")
		a, ok := got["a"].(neo4j.Node)
		if !ok || !slices.Equal(a.Labels, []string{"Dbimp"}) || !reflect.DeepEqual(a.Props, map[string]any{"k": "n1"}) || a.ElementID == "" {
			t.Errorf("%s: a is %#v, want the node n1", release, got["a"])
		}
		r, ok := got["r"].(neo4j.Relationship)
		if !ok || r.Type != "DBIMP_R" || r.StartElementID != a.ElementID || !reflect.DeepEqual(r.Props, map[string]any{"w": int64(2)}) {
			t.Errorf("%s: r is %#v, want the relationship from a", release, got["r"])
		}
		p, ok := got["p"].(neo4j.Path)
		if !ok || len(p.Nodes) != 2 || len(p.Relationships) != 1 || p.Nodes[0].ElementID != a.ElementID || p.Relationships[0].ElementID != r.ElementID {
			t.Errorf("%s: p is %#v, want the path a, r, b", release, got["p"])
		}
	}
}

func TestReplayVector(t *testing.T) {
	t.Parallel()
	db := replayDB(t, ceiling)
	got := row(t, db, "RETURN vector([1, 2], 2, INT8) AS i8, vector([1, 2], 2, INT16) AS i16, vector([1, 2], 2, INT32) AS i32, "+
		"vector([1, 2], 2, INT64) AS i64, vector([1.5, 2.5], 2, FLOAT32) AS f32, vector([1.5, 2.5], 2, FLOAT64) AS f64")
	check(t, ceiling, got, map[string]any{
		"i8":  dbimp.Vector[int8]{1, 2},
		"i16": dbimp.Vector[int16]{1, 2},
		"i32": dbimp.Vector[int32]{1, 2},
		"i64": dbimp.Vector[int64]{1, 2},
		"f32": dbimp.Vector[float32]{1.5, 2.5},
		"f64": dbimp.Vector[float64]{1.5, 2.5},
	})
	got = row(t, db, "RETURN $v AS v, valueType($v) AS t", sql.Named("v", dbimp.Vector[int8]{1, -2}))
	check(t, ceiling, got, map[string]any{"v": dbimp.Vector[int8]{1, -2}})
}

// TestReplayVectorInTheFirstVersion replays a vector in typed JSON v1.0,
// which cannot write one: the server sends a row with no values, and then the
// error (D66).
func TestReplayVectorInTheFirstVersion(t *testing.T) {
	t.Parallel()
	_, rows, err := rowsOf(t, replayDB(t, ceiling), "RETURN vector([1.0, 2.0, 3.0], 3, FLOAT32) AS v")
	if len(rows) != 0 {
		t.Errorf("read %d rows, want none", len(rows))
	}
	re := serverError(t, err)
	if !strings.Contains(re.Errs[0].Message, "VECTOR") {
		t.Errorf("the error is %v, want the error of the server about VECTOR", err)
	}
	if errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want no dbimp.ErrIncomplete, because no row came first (D107)", err)
	}
}

func TestReplayUUID(t *testing.T) {
	t.Parallel()
	u := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	got := row(t, replayDB(t, ceiling), "RETURN $u AS u, valueType($u) AS t", sql.Named("u", u))
	check(t, ceiling, got, map[string]any{"u": u, "t": "UUID NOT NULL"})
}

func TestReplayParameters(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayDB(t, release)
		got := row(t, db, "RETURN $s AS s, $i AS i, $f AS f, $b AS b, $n AS n, $l AS l, $m AS m",
			sql.Named("s", "it's"), sql.Named("i", 42), sql.Named("f", 1.5), sql.Named("b", true),
			sql.Named("n", nil), sql.Named("l", []any{1, "a"}), sql.Named("m", map[string]any{"k": []int{1}}))
		check(t, release, got, map[string]any{
			"s": "it's", "i": int64(42), "f": 1.5, "b": true, "n": nil,
			"l": []any{int64(1), "a"}, "m": map[string]any{"k": []any{int64(1)}},
		})
		// A positional argument n fills $n (D64).
		got = row(t, db, "RETURN $1 AS a, $2 AS b", "one", 2)
		check(t, release, got, map[string]any{"a": "one", "b": int64(2)})
		// Each typed argument keeps its type on the server (D63).
		dt := time.Date(2026, 9, 27, 10, 0, 0, 0, oslo(t))
		got = row(t, db, "RETURN valueType($d) AS d, valueType($dur) AS dur, valueType($p) AS p, valueType($zdt) AS zdt, valueType($b) AS b, $zdt AS zv",
			sql.Named("d", dbimp.Date{Year: 2026, Month: 9, Day: 27}),
			sql.Named("dur", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: 14706007000000}),
			sql.Named("p", neo4j.Point{SRID: 7203, X: 1.5, Y: 2.5, Dims: 2}),
			sql.Named("zdt", dt), sql.Named("b", []byte{0xde, 0xad, 0xbe, 0xef}))
		check(t, release, got, map[string]any{
			"d": "DATE NOT NULL", "dur": "DURATION NOT NULL", "p": "POINT NOT NULL", "zdt": "ZONED DATETIME NOT NULL",
		})
		if zv, ok := got["zv"].(time.Time); !ok || !zv.Equal(dt) || zv.Location().String() != "Europe/Oslo" {
			t.Errorf("%s: zv is %#v, want %v", release, got["zv"], dt)
		}
	}
}

func TestReplayRefusedArguments(t *testing.T) {
	t.Parallel()
	db := replayDB(t, ceiling)
	for _, arg := range []any{neo4j.Node{ElementID: "4:x:0"}, neo4j.Path{}, uint64(math.MaxUint64), struct{}{}} {
		if _, _, err := rowsOf(t, db, "RETURN $n AS n", sql.Named("n", arg)); err == nil {
			t.Errorf("an argument of %T gave no error", arg)
		}
	}
}

func TestReplayNoRows(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayDB(t, release)
		cols, rows, err := rowsOf(t, db, "UNWIND [] AS x RETURN x")
		if err != nil || !slices.Equal(cols, []string{"x"}) || len(rows) != 0 {
			t.Errorf("%s: no rows gave %q, %v and %v, want the column x and no rows", release, cols, rows, err)
		}
		cols, rows, err = rowsOf(t, db, "CREATE (:Dbimp {k: 'nocol'})")
		if err != nil || len(cols) != 0 || len(rows) != 0 {
			t.Errorf("%s: no columns gave %q, %v and %v, want nothing", release, cols, rows, err)
		}
	}
}

func TestReplayManyRows(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		rows, err := replayDB(t, release).QueryContext(t.Context(), "UNWIND range(1, 20000) AS x RETURN x")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := int64(0)
		for rows.Next() {
			var x int64
			if err := rows.Scan(&x); err != nil {
				t.Fatal(err)
			}
			if n++; x != n {
				t.Fatalf("%s: row %d is %d", release, n, x)
			}
		}
		if err := rows.Err(); err != nil || n != 20000 {
			t.Errorf("%s: read %d rows and %v, want 20000 rows", release, n, err)
		}
	}
}

func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayDB(t, release)
		for query, code := range map[string]string{
			"RETRUN 1":                        "Neo.ClientError.Statement.SyntaxError",
			"RETURN $nothing AS x":            "Neo.ClientError.Statement.ParameterMissing",
			"RETURN dbimp.nothing() AS x":     "Neo.ClientError.Statement.SyntaxError",
			"RETURN 1 AS a; RETURN 2 AS b":    "Neo.ClientError.Statement.SyntaxError",
			"RETURN 9223372036854775808 AS x": "Neo.ClientError.Statement.SyntaxError",
		} {
			_, _, err := rowsOf(t, db, query)
			if re := serverError(t, err); re.HTTPStatus != http.StatusBadRequest || re.Errs[0].Code != code {
				t.Errorf("%s: %q gave %v, want HTTP 400 and %s", release, query, err, code)
			}
			if !errors.Is(err, neo4j.Error{Code: code, Message: serverError(t, err).Errs[0].Message}) {
				t.Errorf("%s: errors.Is does not find the Error in %v", release, err)
			}
		}
		// An error after some rows arrives after them, with HTTP 202 (D66).
		_, rows, err := rowsOf(t, db, "UNWIND [1, 2, 0, 4] AS x RETURN 10 / x AS y")
		if want := [][]any{{int64(10)}, {int64(5)}}; !reflect.DeepEqual(rows, want) {
			t.Errorf("%s: the rows before the error are %v, want %v", release, rows, want)
		}
		if re := serverError(t, err); re.HTTPStatus != http.StatusAccepted || re.Errs[0].Code != "Neo.ClientError.Statement.ArithmeticError" {
			t.Errorf("%s: the error after the rows is %v", release, err)
		}
		// A database that does not exist is HTTP 404.
		srv := dbimptest.ReplayRelease(t, testdata, release, match)
		nothing, err := sql.Open("neo4j", "neo4j://neo4j:pw@"+strings.TrimPrefix(srv.URL, "http://")+"/dbimp_nothing")
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = rowsOf(t, nothing, "RETURN 1 AS a")
		nothing.Close()
		if re := serverError(t, err); re.HTTPStatus != http.StatusNotFound || re.Errs[0].Code != "Neo.ClientError.Database.DatabaseNotFound" {
			t.Errorf("%s: an unknown database gave %v", release, err)
		}
	}
}

func TestReplayWrongPassword(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusUnauthorized
		})
		_, _, err := rowsOf(t, db, "RETURN 1 AS a")
		if re := serverError(t, err); re.HTTPStatus != http.StatusUnauthorized || re.Errs[0].Code != "Neo.ClientError.Security.Unauthorized" {
			t.Errorf("%s: a wrong password gave %v", release, err)
		}
	}
}

func TestReplayPrivilege(t *testing.T) {
	t.Parallel()
	const query = "CREATE INDEX dbimp_ix IF NOT EXISTS FOR (n:Dbimp) ON (n.k)"
	for _, release := range releases {
		db := replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return statementOf(ex) == query && ex.Response.Status == http.StatusBadRequest
		})
		_, err := db.ExecContext(t.Context(), query)
		if re := serverError(t, err); re.Errs[0].Code != "Neo.ClientError.Security.Forbidden" {
			t.Errorf("%s: the ordinary user gave %v, want Forbidden", release, err)
		}
	}
}

func TestReplayStatements(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayDB(t, release)
		check(t, release, row(t, db, "RETURN 1 AS a;"), map[string]any{"a": int64(1)})
		check(t, release, row(t, db, "// a comment with $x\nRETURN 1 AS a /* ; RETURN 2 */, '$x // ;' AS s"), map[string]any{"a": int64(1), "s": "$x // ;"})
		check(t, release, row(t, db, "RETURN 1 AS a,\n2 AS b"), map[string]any{"a": int64(1), "b": int64(2)})
	}
}

func TestReplayExec(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		res, err := replayDB(t, release).ExecContext(t.Context(), "CREATE (:Dbimp {k: 'nocol'})")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: RowsAffected gave %v, want dbimp.ErrNotSupported (D66)", release, err)
		}
		if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: LastInsertId gave %v, want dbimp.ErrNotSupported", release, err)
		}
	}
}

func TestReplayPing(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		if err := replayDB(t, release).PingContext(t.Context()); err != nil {
			t.Errorf("%s: %v", release, err)
		}
	}
}

func TestReplayVersion(t *testing.T) {
	t.Parallel()
	for release, want := range map[string]string{floor: "5.26.31", ceiling: "2026.09.0"} {
		_, rows, err := rowsOf(t, replayDB(t, release), "CALL dbms.components() YIELD name, versions, edition RETURN name, versions, edition")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if w := []any{"Neo4j Kernel", []any{want}, "enterprise"}; len(rows) == 0 || !reflect.DeepEqual(rows[0], w) {
			t.Errorf("%s: the version is %v, want %v", release, rows, w)
		}
	}
}

func TestReplayTransactionCommit(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayTx(t, release, "CREATE (n:Dbimp {k: 'tx7'}) RETURN n.k AS k")
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		check(t, release, row2(t, tx, "CREATE (n:Dbimp {k: 'tx7'}) RETURN n.k AS k"), map[string]any{"k": "tx7"})
		if err := tx.Commit(); err != nil {
			t.Errorf("%s: the commit gave %v", release, err)
		}
	}
}

func TestReplayTransactionRollback(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayTx(t, release, "CREATE (n:Dbimp {k: 'tx8'}) RETURN n.k AS k")
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		check(t, release, row2(t, tx, "CREATE (n:Dbimp {k: 'tx8'}) RETURN n.k AS k"), map[string]any{"k": "tx8"})
		if err := tx.Rollback(); err != nil {
			t.Errorf("%s: the rollback gave %v", release, err)
		}
	}
}

func TestReplayTransactionReadOnly(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayTx(t, release, "CREATE (:Dbimp {k: 'ro'})")
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		check(t, release, row2(t, tx, "RETURN 1 AS a"), map[string]any{"a": int64(1)})
		_, err = tx.ExecContext(t.Context(), "CREATE (:Dbimp {k: 'ro'})")
		if re := serverError(t, err); re.Errs[0].Code != "Neo.ClientError.Statement.AccessMode" {
			t.Errorf("%s: a write in a read only transaction gave %v", release, err)
		}
		// The error ended the transaction on the server, so the rollback
		// sends nothing, and the fake server has nothing to answer (D65).
		if err := tx.Rollback(); err != nil {
			t.Errorf("%s: the rollback after the error gave %v", release, err)
		}
	}
}

func TestReplayTransactionError(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayTx(t, release, "RETURN 1 / 0 AS x")
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		_, _, err = rowsOf(t, tx, "RETURN 1 / 0 AS x")
		if re := serverError(t, err); re.Errs[0].Code != "Neo.ClientError.Statement.ArithmeticError" {
			t.Errorf("%s: the error in the transaction is %v", release, err)
		}
		// The error ended the transaction, so the commit returns it and
		// sends nothing (D65).
		if err := tx.Commit(); err == nil || !errors.Is(err, neo4j.Error{Code: "Neo.ClientError.Statement.ArithmeticError", Message: "/ by zero"}) {
			t.Errorf("%s: the commit after the error gave %v, want the error", release, err)
		}
	}
}

func TestReplayTransactionIsolation(t *testing.T) {
	t.Parallel()
	db := replayDB(t, ceiling)
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable}); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("an isolation level gave %v, want dbimp.ErrNotSupported (D65)", err)
	}
}

// row2 is row for a transaction.
func row2(t *testing.T, tx *sql.Tx, query string, args ...any) map[string]any {
	t.Helper()
	cols, rows, err := rowsOf(t, tx, query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s gave %d rows, want 1", query, len(rows))
	}
	m := map[string]any{}
	for i, c := range cols {
		m[c] = rows[0][i]
	}
	return m
}
