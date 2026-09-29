package influxdb_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/influxdb"
)

// These tests replay the exchanges that step 6 recorded from real servers,
// under testdata/influxdb/. Each one decodes a real answer through the
// driver.

const testdata = "../testdata/influxdb"

// The releases that step 6 recorded, by major release.
var (
	v1 = []string{"influxdb-1.11.8", "influxdb-1.13.1"}
	v2 = []string{"influxdb-2.8.0", "influxdb-2.9.1"}
	v3 = []string{"influxdb-3.9.13", "influxdb-3.10.6", "influxdb-3.11.5"}
)

// every returns every release that step 6 recorded.
func every() []string {
	return append(append(append([]string{}, v1...), v2...), v3...)
}

// majorOf returns the major release of a release, such as "1" for
// influxdb-1.13.1.
func majorOf(release string) string {
	return strings.TrimPrefix(release, "influxdb-")[:1]
}

// match matches a request of the driver with a recorded one. It reads the
// query and a form body as values, because the recorder wrote a space as %20
// and net/url writes it as +, and it reads a JSON body in its canonical form.
func match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path {
		return false
	}
	if !sameValues(r.URL.RawQuery, ex.Request.Query) {
		return false
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return sameValues(string(body), string(ex.Request.Content()))
	}
	return canon(body) == canon(ex.Request.Content())
}

// sameValues reports whether two queries hold the same values.
func sameValues(a, b string) bool {
	va, erra := url.ParseQuery(a)
	vb, errb := url.ParseQuery(b)
	return erra == nil && errb == nil && reflect.DeepEqual(va, vb)
}

// canon returns the canonical form of a JSON body.
func canon(b []byte) string {
	if len(bytes.TrimSpace(b)) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	return string(out)
}

// replay opens the driver against the exchanges of release, with the keys
// of query.
func replay(t *testing.T, release, query string) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, match)
	host := strings.TrimPrefix(srv.URL, "http://")
	db, err := sql.Open(influxdb.Name, "influxdb://_admin:secret@"+host+"/dbmeta?"+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// influxQL opens the driver against release with the InfluxQL dialect, in the
// form that the driver sends to that release (D83).
func influxQL(t *testing.T, release string) *sql.DB {
	t.Helper()
	return replay(t, release, "sqlmode=disable&version="+majorOf(release))
}

// set is one result set: its columns and its rows.
type set struct {
	cols []string
	rows [][]any
}

// readAll runs query, and reads every row of every result set.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]set, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sets []set
	for {
		cols, err := rows.Columns()
		if err != nil {
			return sets, err
		}
		s := set{cols: cols}
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return sets, err
			}
			s.rows = append(s.rows, vals)
		}
		if err := rows.Err(); err != nil {
			return append(sets, s), err
		}
		sets = append(sets, s)
		if !rows.NextResultSet() {
			return sets, rows.Err()
		}
	}
}

// ts returns the time of a text in RFC 3339.
func ts(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// check compares the sets that a query gave with want.
func check(t *testing.T, got, want []set) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the query gave %d result sets, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i].cols, want[i].cols) {
			t.Errorf("set %d has the columns %q, want %q", i, got[i].cols, want[i].cols)
		}
		if !reflect.DeepEqual(got[i].rows, want[i].rows) {
			t.Errorf("set %d has the rows\n%#v\nwant\n%#v", i, got[i].rows, want[i].rows)
		}
	}
}

func TestReplayConnectChoosesTheDialect(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := replay(t, release, "")
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var dialect, version string
			err = conn.Raw(func(dc any) error {
				if dialect, err = influxdb.Dialect(dc); err != nil {
					return err
				}
				version, err = influxdb.Version(t.Context(), dc)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			want := influxdb.InfluxQL
			if majorOf(release) == "3" {
				want = influxdb.SQL
			}
			if dialect != want {
				t.Errorf("the dialect is %q, want %q (D78)", dialect, want)
			}
			if wantV := strings.TrimPrefix(release, "influxdb-"); version != wantV {
				t.Errorf("the version is %q, want %q, without the v of InfluxDB 2", version, wantV)
			}
		})
	}
}

func TestReplaySQLRequireRefusesInfluxDB2(t *testing.T) {
	t.Parallel()
	for _, release := range append(append([]string{}, v1...), v2...) {
		db := replay(t, release, "sqlmode=require")
		if err := db.PingContext(t.Context()); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: sqlmode=require gave %v, want dbimp.ErrNotSupported (D78)", release, err)
		}
	}
}

func TestReplaySQLOnAReleaseWithoutIt(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"influxdb-1.13.1", "influxdb-2.9.1"} {
		db := replay(t, release, "sqlmode=allow&describe=disable&version="+majorOf(release))
		_, err := readAll(t, db, "SELECT 1 AS a")
		ierr, ok := errors.AsType[*influxdb.Error](err)
		switch {
		case !ok:
			t.Errorf("%s: SQL gave %v, want an *influxdb.Error", release, err)
		case release == "influxdb-1.13.1" && ierr.HTTPStatus != http.StatusNotFound:
			t.Errorf("%s: SQL gave the status %d, want 404 (measured)", release, ierr.HTTPStatus)
		case release == "influxdb-2.9.1" && !strings.Contains(ierr.Message, "text/html"):
			t.Errorf("%s: SQL gave %q, want an error that names the page of HTML (measured)", release, ierr.Message)
		}
	}
}

func TestReplaySQLColumns(t *testing.T) {
	t.Parallel()
	for _, release := range v3 {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := replay(t, release, "sqlmode=allow")
			got, err := readAll(t, db, "SELECT NULL AS a, 1 AS b")
			if err != nil {
				t.Fatal(err)
			}
			// The answer is [{"b":1}], and DESCRIBE names a (D80).
			check(t, got, []set{{cols: []string{"a", "b"}, rows: [][]any{{nil, int64(1)}}}})

			got, err = readAll(t, db, "SELECT time, s, f, region FROM types ORDER BY time")
			if err != nil {
				t.Fatal(err)
			}
			check(t, got, []set{{
				cols: []string{"time", "s", "f", "region"},
				rows: [][]any{
					{ts(t, "2023-11-14T22:13:20Z"), "text", 1.5, nil},
					{ts(t, "2023-11-14T22:13:21Z"), "", -2.25, nil},
					{ts(t, "2023-11-14T22:13:22Z"), nil, 0.0, "x"},
					{ts(t, "2023-11-14T22:13:23Z"), nil, nil, nil},
				},
			}})

			got, err = readAll(t, db, "SELECT time, s FROM types WHERE s = 'none'")
			if err != nil {
				t.Fatal(err)
			}
			check(t, got, []set{{cols: []string{"time", "s"}}})
		})
	}
}

func TestReplaySQLColumnsWithoutDescribe(t *testing.T) {
	t.Parallel()
	db := replay(t, "influxdb-3.11.5", "sqlmode=allow&describe=disable")
	// Without DESCRIBE, the first row names the columns (D77), and the
	// later key region is not one of them.
	_, err := readAll(t, db, "SELECT time, s, f, region FROM types ORDER BY time")
	if !errors.Is(err, dbimp.ErrExtraColumn) {
		t.Errorf("the query gave %v, want dbimp.ErrExtraColumn (D77)", err)
	}
}

func TestReplaySQLTypes(t *testing.T) {
	t.Parallel()
	query := "SELECT CAST('12345678901234567890.123456789' AS DECIMAL(38,9)) AS dec, " +
		"DATE '2024-01-02' AS d, CAST('12:30:00' AS TIME) AS tm, INTERVAL '1 day 2 hours' AS iv, " +
		"arrow_cast(TIMESTAMP '2024-01-02T03:04:05.123456789', 'Timestamp(Nanosecond, Some(\"UTC\"))') AS tz, " +
		"TIMESTAMP '2024-01-02T03:04:05.123456789' AS ts, " +
		"arrow_cast('ab', 'Binary') AS bin, [1, 2, 3] AS arr, named_struct('a', 1) AS st, " +
		"CAST(1 AS TINYINT) AS i8, CAST(1.5 AS FLOAT) AS f32, arrow_cast(255, 'UInt8') AS u8, " +
		"9223372036854775807 AS imax, arrow_cast(18446744073709551615, 'UInt64') AS umax, " +
		"'NaN'::DOUBLE AS nan, 'inf'::DOUBLE AS inf, 'é' AS s, '' AS empty, true AS b"
	dec, _, err := apd.NewFromString("12345678901234567890.123456789")
	if err != nil {
		t.Fatal(err)
	}
	for _, release := range v3 {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := replay(t, release, "sqlmode=allow")
			rows, err := db.QueryContext(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			types, err := rows.ColumnTypes()
			if err != nil {
				t.Fatal(err)
			}
			if p, s, ok := types[0].DecimalSize(); !ok || p != 38 || s != 9 {
				t.Errorf("the decimal has the size %d, %d, %v, want 38, 9", p, s, ok)
			}
			if name := types[0].DatabaseTypeName(); name != "DECIMAL128(38, 9)" {
				t.Errorf("the decimal is the type %q, want DECIMAL128(38, 9)", name)
			}
			if st := types[13].ScanType(); st != reflect.TypeFor[uint64]() {
				t.Errorf("umax scans into %v, want uint64", st)
			}
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			vals := make([]any, len(types))
			ptrs := make([]any, len(types))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			if d, ok := vals[0].(*apd.Decimal); !ok || d.Cmp(dec) != 0 {
				t.Errorf("dec is %v, want %v with every digit (D33)", vals[0], dec)
			}
			instant := time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)
			for i, want := range map[int]any{
				1: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
				2: "12:30:00", 3: "1 days 2 hours", 4: instant, 5: instant,
				6: []byte("ab"), 7: []any{int64(1), int64(2), int64(3)}, 8: map[string]any{"a": int64(1)},
				9: int64(1), 10: 1.5, 11: uint64(255), 12: int64(math.MaxInt64), 13: uint64(math.MaxUint64),
				16: "é", 17: "", 18: true,
			} {
				if !reflect.DeepEqual(vals[i], want) {
					t.Errorf("column %d is %#v, want %#v", i, vals[i], want)
				}
			}
			// The JSON writes NaN and +Inf alike, as an explicit null, and
			// the driver reads each as NaN, and never as NULL (D80).
			for _, i := range []int{14, 15} {
				if f, ok := vals[i].(float64); !ok || !math.IsNaN(f) {
					t.Errorf("column %d is %#v, want NaN", i, vals[i])
				}
			}
			if st := types[14].ScanType(); st != reflect.TypeFor[float64]() {
				t.Errorf("nan scans into %v, want float64, as DESCRIBE says it is not nullable", st)
			}
		})
	}
}

func TestReplaySQLTable(t *testing.T) {
	t.Parallel()
	db := replay(t, "influxdb-3.11.5", "sqlmode=allow")
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM types WHERE host = 'd' OR host = 'a'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ct := range types {
		names = append(names, ct.Name()+" "+ct.DatabaseTypeName()+" "+ct.ScanType().String())
	}
	want := []string{
		"b BOOLEAN sql.Null[bool]", "f FLOAT64 sql.Null[float64]", "host DICTIONARY(INT32, UTF8) sql.Null[string]",
		"i INT64 sql.Null[int64]", "region DICTIONARY(INT32, UTF8) sql.Null[string]", "s UTF8 sql.Null[string]",
		"time TIMESTAMP(NS) time.Time", "u UINT64 sql.Null[uint64]",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("the columns are\n%q\nwant\n%q", names, want)
	}
	var got [][]any
	for rows.Next() {
		var (
			b    sql.Null[bool]
			f    sql.Null[float64]
			host string
			i    sql.Null[int64]
			reg  sql.Null[string]
			s    sql.Null[string]
			at   time.Time
			u    sql.Null[uint64]
		)
		if err := rows.Scan(&b, &f, &host, &i, &reg, &s, &at, &u); err != nil {
			t.Fatal(err)
		}
		got = append(got, []any{b, f, host, i, reg, s, at, u})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantRows := [][]any{
		{sql.Null[bool]{}, sql.Null[float64]{}, "d", sql.Null[int64]{}, sql.Null[string]{}, sql.Null[string]{},
			ts(t, "2023-11-14T22:13:23Z"), sql.Null[uint64]{V: math.MaxUint64, Valid: true}},
		{sql.Null[bool]{V: true, Valid: true}, sql.Null[float64]{V: 1.5, Valid: true}, "a",
			sql.Null[int64]{V: math.MaxInt64, Valid: true}, sql.Null[string]{}, sql.Null[string]{V: "text", Valid: true},
			ts(t, "2023-11-14T22:13:20Z"), sql.Null[uint64]{}},
	}
	if !reflect.DeepEqual(got, wantRows) {
		t.Errorf("the rows are\n%#v\nwant\n%#v", got, wantRows)
	}
}

func TestReplaySQLParameters(t *testing.T) {
	t.Parallel()
	for _, release := range v3 {
		db := replay(t, release, "sqlmode=allow")
		got, err := readAll(t, db, "SELECT $a AS a, $b AS b, $c AS c, $d AS d",
			sql.Named("a", 1), sql.Named("b", 1.5), sql.Named("c", "x"), sql.Named("d", nil))
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		// DESCRIBE types a parameter as Null, and the value keeps its own
		// type (measured).
		check(t, got, []set{{cols: []string{"a", "b", "c", "d"}, rows: [][]any{{int64(1), 1.5, "x", nil}}}})
		got, err = readAll(t, db, "SELECT $1 AS a", "x")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		check(t, got, []set{{cols: []string{"a"}, rows: [][]any{{"x"}}}})
	}
	db := replay(t, "influxdb-3.11.5", "sqlmode=allow")
	got, err := readAll(t, db, "SELECT f FROM types WHERE time > $t", sql.Named("t", ts(t, "2023-11-14T22:13:20Z")))
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, []set{{cols: []string{"f"}, rows: [][]any{{-2.25}, {0.0}, {nil}}}})
}

func TestReplaySQLErrors(t *testing.T) {
	t.Parallel()
	for _, release := range v3 {
		db := replay(t, release, "sqlmode=allow")
		_, err := readAll(t, db, "SELECT nope FROM types")
		if ierr, ok := errors.AsType[*influxdb.Error](err); !ok || ierr.HTTPStatus != http.StatusInternalServerError ||
			!strings.Contains(ierr.Message, "No field named nope") {
			t.Errorf("%s: a column that does not exist gave %v, want HTTP 500 with the message of the server", release, err)
		}
		db = replay(t, release, "sqlmode=allow&describe=disable")
		_, err = readAll(t, db, "SELECT 1 AS a; SELECT 2 AS b")
		if _, ok := errors.AsType[*influxdb.Error](err); !ok {
			t.Errorf("%s: two statements gave %v, want the error of the server", release, err)
		}
		got, err := readAll(t, db, "SELECT n / (n - 10000) AS c FROM big ORDER BY time")
		if !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error after some rows gave %v, want dbimp.ErrIncomplete", release, err)
		}
		if len(got) != 1 || len(got[0].rows) == 0 {
			t.Errorf("%s: an error after some rows gave no rows before it", release)
		}
	}
}

func TestReplayInfluxQLSeries(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := influxQL(t, release)
			got, err := readAll(t, db, "SELECT f FROM types GROUP BY host")
			if err != nil {
				t.Fatal(err)
			}
			cols := []string{"measurement", "host", "time", "f"}
			want := []set{
				{cols: cols, rows: [][]any{{"types", "a", ts(t, "2023-11-14T22:13:20Z"), 1.5}}},
				{cols: cols, rows: [][]any{{"types", "b", ts(t, "2023-11-14T22:13:21Z"), -2.25}}},
				// A float that holds 0 arrives as 0 (D83).
				{cols: cols, rows: [][]any{{"types", "c", ts(t, "2023-11-14T22:13:22Z"), int64(0)}}},
			}
			// The point of host d has no f, so it has no series (measured).
			check(t, got, want)

			got, err = readAll(t, db, "SELECT * FROM types, big LIMIT 1")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0].rows[0][0] != "big" || got[1].rows[0][0] != "types" {
				t.Errorf("two measurements gave %v, want one set for each (D81)", got)
			}
		})
	}
}

func TestReplayInfluxQLTypes(t *testing.T) {
	t.Parallel()
	db := influxQL(t, "influxdb-2.9.1")
	got, err := readAll(t, db, "SELECT * FROM types")
	if err != nil {
		t.Fatal(err)
	}
	check(t, got, []set{{
		cols: []string{"measurement", "time", "b", "f", "host", "i", "region", "s", "u"},
		rows: [][]any{
			{"types", ts(t, "2023-11-14T22:13:20Z"), true, 1.5, "a", int64(math.MaxInt64), nil, "text", nil},
			{"types", ts(t, "2023-11-14T22:13:21Z"), false, -2.25, "b", int64(math.MinInt64), nil, "", nil},
			{"types", ts(t, "2023-11-14T22:13:22Z"), nil, int64(0), "c", nil, "x", nil, nil},
			{"types", ts(t, "2023-11-14T22:13:23Z"), nil, nil, "d", nil, nil, nil, uint64(math.MaxUint64)},
		},
	}})
}

func TestReplayInfluxQLChunks(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		db := influxQL(t, release)
		got, err := readAll(t, db, "SELECT n FROM big")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		// InfluxDB 1 sends two chunks of 10,000 rows and less, and the series
		// continues across them (D83).
		if len(got) != 1 || len(got[0].rows) != 12000 {
			t.Errorf("%s: 12000 rows gave %d sets", release, len(got))
		}
		if len(got) == 1 && len(got[0].rows) == 12000 && got[0].rows[11999][2] != int64(11999) {
			t.Errorf("%s: the last row is %v, want n = 11999", release, got[0].rows[11999])
		}
	}
}

func TestReplayInfluxQLStatements(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		db := influxQL(t, release)
		got, err := readAll(t, db, "SELECT * FROM nothere; SELECT f FROM types LIMIT 1; SELECT * FROM nothere")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		want := []set{
			{cols: []string{}},
			{cols: []string{"measurement", "time", "f"}, rows: [][]any{{"types", ts(t, "2023-11-14T22:13:20Z"), 1.5}}},
			{cols: []string{}},
		}
		check(t, got, want)
	}
	for _, release := range append(append([]string{}, v1...), v2...) {
		db := influxQL(t, release)
		got, err := readAll(t, db,
			"DELETE FROM gap; SELECT f FROM types LIMIT 1; DROP MEASUREMENT gap_none; SELECT f FROM types LIMIT 1")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		// InfluxDB 2 leaves out the results 0 and 2, and the driver gives an
		// empty set for each (D83).
		row := set{cols: []string{"measurement", "time", "f"}, rows: [][]any{{"types", ts(t, "2023-11-14T22:13:20Z"), 1.5}}}
		check(t, got, []set{{cols: []string{}}, row, {cols: []string{}}, row})
	}
}

func TestReplayInfluxQLErrors(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		db := influxQL(t, release)
		got, err := readAll(t, db, "SELECT n FROM big LIMIT 2; SELECT nope(n) FROM big")
		ierr, ok := errors.AsType[*influxdb.Error](err)
		switch {
		case !ok:
			t.Errorf("%s: an error in the second statement gave %v, want an *influxdb.Error", release, err)
		case ierr.Statement != 1:
			t.Errorf("%s: the error names the statement %d, want 1", release, ierr.Statement)
		case len(got) != 1 || len(got[0].rows) != 2:
			t.Errorf("%s: the first statement gave %v, want its two rows before the error", release, got)
		}
		_, err = readAll(t, db, "SELEC 1")
		want := http.StatusBadRequest
		if majorOf(release) == "3" {
			want = http.StatusOK
		}
		if ierr, ok := errors.AsType[*influxdb.Error](err); !ok || ierr.HTTPStatus != want {
			t.Errorf("%s: a statement that does not parse gave %v, want an *influxdb.Error with HTTP %d (measured)", release, err, want)
		}
	}
}

func TestReplayInfluxQLParameters(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		db := influxQL(t, release)
		got, err := readAll(t, db, "SELECT f FROM types WHERE host = $h", sql.Named("h", "a"))
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		check(t, got, []set{{cols: []string{"measurement", "time", "f"}, rows: [][]any{{"types", ts(t, "2023-11-14T22:13:20Z"), 1.5}}}})
	}
}

func TestReplayPing(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		db := replay(t, release, "")
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		if err := db.PingContext(ctx); err != nil {
			t.Errorf("%s: %v", release, err)
		}
		cancel()
	}
}

func TestReplaySQLDescribeRefused(t *testing.T) {
	t.Parallel()
	for _, release := range v3 {
		db := replay(t, release, "sqlmode=allow")
		// DESCRIBE refuses SHOW TABLES, so the statement runs alone, and its
		// first row names the columns (D80).
		got, err := readAll(t, db, "SHOW TABLES")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		want := []string{"table_catalog", "table_schema", "table_name", "table_type"}
		if len(got) != 1 || !reflect.DeepEqual(got[0].cols, want) || len(got[0].rows) == 0 {
			t.Errorf("%s: SHOW TABLES gave %v, want the columns %q and its rows", release, got, want)
		}
	}
}

// big opens release, and returns the statement of 12000 rows that step 6
// recorded on it: InfluxQL, or SQL with no DESCRIBE on InfluxDB 3.
func big(t *testing.T, release string) (*sql.DB, string) {
	t.Helper()
	if majorOf(release) == "3" {
		return replay(t, release, "sqlmode=allow&describe=disable"), "SELECT n FROM big ORDER BY time"
	}
	return influxQL(t, release), "SELECT n FROM big"
}

func TestReplayTwoQueriesAtOnce(t *testing.T) {
	t.Parallel()
	for _, release := range []string{"influxdb-1.13.1", "influxdb-3.11.5"} {
		db, query := big(t, release)
		errs := make(chan error, 8)
		for range cap(errs) {
			go func() {
				_, err := readAll(t, db, query)
				errs <- err
			}()
		}
		for range cap(errs) {
			if err := <-errs; err != nil {
				t.Errorf("%s: %v", release, err)
			}
		}
	}
}

func TestReplayLeavesNoGoroutine(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	//nolint:paralleltest // The subtests count the goroutines of the process too.
	for _, release := range []string{"influxdb-1.13.1", "influxdb-2.9.1", "influxdb-3.11.5"} {
		t.Run(release, func(t *testing.T) {
			dbimptest.CheckGoroutines(t)
			db, query := big(t, release)
			if _, err := readAll(t, db, query); err != nil {
				t.Fatal(err)
			}
			rows, err := db.QueryContext(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			// The rows close before their end, in the defer.
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestErrorsHideThePassword(t *testing.T) {
	t.Parallel()
	db, err := sql.Open(influxdb.Name, "influxdb://u:secret@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.PingContext(t.Context())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("a ping to a closed port gave %v, which holds the password or no error", err)
	}
}

func TestReplayInsert(t *testing.T) {
	t.Parallel()
	for _, release := range every() {
		for _, dsn := range []string{"sqlmode=disable&version=" + majorOf(release), "sqlmode=allow&version=" + majorOf(release)} {
			db := replay(t, release, dsn)
			// INSERT becomes a write of line protocol to /write, in both
			// dialects (D85).
			if _, err := db.ExecContext(t.Context(), "INSERT crud v=$v 1700000000000000000", sql.Named("v", 1)); err != nil {
				t.Errorf("%s, %s: %v", release, dsn, err)
			}
		}
	}
	db := influxQL(t, "influxdb-1.13.1")
	_, err := db.ExecContext(t.Context(), "INSERT types,host=d u=18446744073709551615u 1700000003000000000")
	if ierr, ok := errors.AsType[*influxdb.Error](err); !ok || ierr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("an unsigned integer on InfluxDB 1 gave %v, want the refusal of the server with HTTP 400 (measured)", err)
	}
}
