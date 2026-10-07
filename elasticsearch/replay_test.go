package elasticsearch_test

import (
	"database/sql"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/elasticsearch"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/elasticsearch/. Each one decodes a real answer through the
// driver.
//
// The recordings sent the body of the statement with the keys that the test
// named. The driver adds the page size 1000 and the time zone UTC to every
// statement, which are the defaults of the server (measured), so the fake
// server leaves both out of a body when they hold the default, and compares
// the rest.

const testdata = "../testdata/elasticsearch"

// releases are the releases that step 6 recorded.
var releases = []string{"elasticsearch-8.19.22", "elasticsearch-9.4.6", "elasticsearch-9.5.3"}

// normalize returns the canonical form of the body of a request to /_sql,
// without the page size and the time zone when they hold the default.
func normalize(b []byte) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	if m["fetch_size"] == float64(1000) {
		delete(m, "fetch_size")
	}
	if m["time_zone"] == "UTC" {
		delete(m, "time_zone")
	}
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	v := jsontext.Value(out)
	if err := v.Canonicalize(); err != nil {
		return string(out)
	}
	return string(v)
}

// replayServer answers each request with a recorded exchange of one release.
// It keeps the method and the path of each request.
type replayServer struct {
	t         *testing.T
	exchanges []*dbimptest.Exchange
	keep      func(*dbimptest.Exchange) bool

	mu   sync.Mutex
	logs []string
}

// newReplay starts a fake server with the exchanges of release for which
// keep is true, or every one for a nil keep. A recording of another format
// than JSON never answers.
func newReplay(t *testing.T, release string, keep func(*dbimptest.Exchange) bool) (*httptest.Server, *replayServer) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, release+"-*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("finding the exchanges of %s: %v", release, err)
	}
	s := &replayServer{t: t, keep: keep}
	for _, path := range paths {
		ex, err := dbimptest.ReadExchange(path)
		if err != nil {
			t.Fatal(err)
		}
		if f := ex.Request.Query; f != "" && f != "format=json" {
			continue
		}
		s.exchanges = append(s.exchanges, ex)
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv, s
}

func (s *replayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading a request to the fake server: %v", err)
		return
	}
	s.mu.Lock()
	s.logs = append(s.logs, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	if q := r.URL.RawQuery; q != "" && q != "format=json" {
		s.t.Errorf("the request has the query %q, want format=json or none", q)
	}
	want := normalize(body)
	for _, ex := range s.exchanges {
		if ex.Request.Method != r.Method || ex.Request.Path != r.URL.Path || (s.keep != nil && !s.keep(ex)) || normalize(ex.Request.Content()) != want {
			continue
		}
		res := ex.Response
		for key, vals := range res.Header {
			for _, val := range vals {
				w.Header().Add(key, val)
			}
		}
		// The recorder kept the body that the client read, after gzip.
		w.Header().Del("Content-Encoding")
		w.Header().Del("Content-Length")
		w.WriteHeader(res.Status)
		if _, err := w.Write(res.Content()); err != nil {
			s.t.Errorf("writing a response from the fake server: %v", err)
		}
		return
	}
	s.t.Errorf("no exchange matches %s %s %s", r.Method, r.URL, body)
	http.Error(w, "no exchange matches", http.StatusTeapot)
}

// requests returns the requests that the server received, as the method and
// the path of each.
func (s *replayServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.logs)
}

// replayOnly opens the driver against the recorded exchanges of release for
// which keep is true, or every one for a nil keep. The first exchange that
// matches answers, and the administrator comes first in each recording.
func replayOnly(t *testing.T, release, query string, keep func(*dbimptest.Exchange) bool) (*sql.DB, *replayServer) {
	t.Helper()
	srv, s := newReplay(t, release, keep)
	db, err := sql.Open(elasticsearch.Name, strings.Replace(srv.URL, "http://", "elasticsearch://elastic:secret@", 1)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, s
}

// replay opens the driver against every recorded exchange of release.
func replay(t *testing.T, release string) *sql.DB {
	t.Helper()
	db, _ := replayOnly(t, release, "", nil)
	return db
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}

// serverError returns err as a *elasticsearch.Error, and fails the test if it
// is not one.
func serverError(t *testing.T, err error) *elasticsearch.Error {
	t.Helper()
	e, ok := errors.AsType[*elasticsearch.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *elasticsearch.Error", err, err)
	}
	return e
}

// utc returns the time of the text s in ISO 8601, in UTC.
func utc(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v.UTC()
}

// everyType is the statement of step 6 that reads every column of the index of
// every type.
const everyType = "SELECT * FROM dbmeta_types WHERE id < 5 ORDER BY id"

// TestReplayTypes holds D167 for each type, as the index of every type stored
// it: a NULL is nil, a field that a document lacks is nil, and a long, an
// unsigned_long and a double keep every digit (measured).
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		cols, got, err := readAll(t, db, everyType)
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		wantCols := strings.Fields("al b bin by ck d dn dt f gp gs hf i id ip k l mot o.a o.s sf sh shp t ul v w")
		if !slices.Equal(cols, wantCols) {
			t.Errorf("%s: the columns are %q, want %q", release, cols, wantCols)
		}
		want := [][]any{
			{int64(math.MaxInt32), true, []byte{0, 255}, int64(127), "fixed", math.MaxFloat64,
				utc(t, "2026-10-01T12:34:56.123456789Z"), utc(t, "2026-10-01T07:04:56.789Z"), 3.4028235e+38,
				"POINT (-71.34 41.12)", "POINT (-71.34 41.12)", 65504.0, int64(math.MaxInt32), int64(1), "2001:db8::1",
				"é'\"\\ x", int64(math.MaxInt64), "match only", int64(1), "x", 1234.57, int64(32767), "POINT (1.5 2.5)",
				"Some text", uint64(math.MaxUint64), "1.2.3-beta", "wild"},
			{int64(math.MinInt32), false, []byte{}, int64(-128), "fixed", math.SmallestNonzeroFloat64,
				utc(t, "1970-01-01T00:00:00Z"), utc(t, "1970-01-01T00:00:00Z"), -1.4e-45,
				nil, nil, -65504.0, int64(math.MinInt32), int64(2), "0.0.0.0",
				"", int64(math.MinInt64), "", nil, nil, 0.0, int64(-32768), nil,
				"", uint64(0), "0.0.0", ""},
			{nil, nil, nil, nil, "fixed", nil, nil, nil, nil, nil, nil, nil, nil, int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
			{nil, nil, nil, nil, "fixed", nil, nil, nil, nil, nil, nil, nil, nil, int64(4), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
		}
		if !reflect.DeepEqual(got, want) {
			for i := range want {
				if !reflect.DeepEqual(got[i], want[i]) {
					for j := range want[i] {
						if !reflect.DeepEqual(got[i][j], want[i][j]) {
							t.Errorf("%s: row %d, column %s is %#v, want %#v", release, i+1, cols[j], got[i][j], want[i][j])
						}
					}
				}
			}
		}
	}
}

// columnTypes returns the types of the columns of query.
func columnTypes(t *testing.T, db *sql.DB, query string, args ...any) []*sql.ColumnType {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
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
	return cts
}

// literals is the statement of step 6 that reads the SQL types that an index
// does not hold, from a cast and from a literal.
const literals = "SELECT CAST('2026-10-01' AS DATE) AS d, CAST('12:34:56.789' AS TIME) AS t, CAST('2026-10-01T12:34:56.789+05:30' AS DATETIME) AS dt, 1.5 AS dbl, 9223372036854775807 AS big, 18446744073709551615 AS ubig, CAST(1 AS TINYINT) AS ti, CAST(1 AS SMALLINT) AS si, CAST(1.5 AS REAL) AS r, CAST(1.5 AS FLOAT) AS fl, TRUE AS b, CAST('1.2.3.4' AS IP) AS ip, CAST('abc' AS TEXT) AS tx, CAST(NULL AS INTEGER) AS ni, NULL AS n"

// intervals is the statement of step 6 that reads every interval.
const intervals = "SELECT INTERVAL 1 YEAR AS y, INTERVAL 1 MONTH AS mo, INTERVAL 2 DAY AS d, INTERVAL 3 HOUR AS h, INTERVAL 3 MINUTE AS mi, INTERVAL 4.5 SECOND AS s, INTERVAL '1-2' YEAR TO MONTH AS ym, INTERVAL '1 2' DAY TO HOUR AS dh, INTERVAL '1 2:3' DAY TO MINUTE AS dm, INTERVAL '1 02:03:04.5' DAY TO SECOND AS ds, INTERVAL '2:3' HOUR TO MINUTE AS hm, INTERVAL '2:3:4.5' HOUR TO SECOND AS hs, INTERVAL '3:4.5' MINUTE TO SECOND AS ms, INTERVAL -1 DAY AS neg, INTERVAL '-1-2' YEAR TO MONTH AS negym"

// TestReplayColumnTypes holds that the type of each column is its type in
// upper case, with the scan type of D167.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query        string
			names, scans string
		}{
			{everyType,
				"INTEGER BOOLEAN BINARY BYTE KEYWORD DOUBLE DATETIME DATETIME FLOAT GEO_POINT GEO_SHAPE HALF_FLOAT INTEGER INTEGER IP KEYWORD LONG TEXT INTEGER KEYWORD SCALED_FLOAT SHORT SHAPE TEXT UNSIGNED_LONG VERSION KEYWORD",
				"int64 bool []uint8 int64 string float64 time.Time time.Time float64 string string float64 int64 int64 string string int64 string int64 string float64 int64 string string uint64 string string"},
			{literals, "DATE TIME DATETIME DOUBLE LONG UNSIGNED_LONG BYTE SHORT FLOAT DOUBLE BOOLEAN IP TEXT INTEGER NULL",
				"dbimp.Date dbimp.OffsetTime time.Time float64 int64 uint64 int64 int64 float64 float64 bool string string int64 interface_{}"},
			{intervals, "INTERVAL_YEAR INTERVAL_MONTH INTERVAL_DAY INTERVAL_HOUR INTERVAL_MINUTE INTERVAL_SECOND INTERVAL_YEAR_TO_MONTH INTERVAL_DAY_TO_HOUR INTERVAL_DAY_TO_MINUTE INTERVAL_DAY_TO_SECOND INTERVAL_HOUR_TO_MINUTE INTERVAL_HOUR_TO_SECOND INTERVAL_MINUTE_TO_SECOND INTERVAL_DAY INTERVAL_YEAR_TO_MONTH",
				"dbimp.Interval dbimp.Interval time.Duration time.Duration time.Duration time.Duration dbimp.Interval time.Duration time.Duration time.Duration time.Duration time.Duration time.Duration time.Duration dbimp.Interval"},
		} {
			var names, scans []string
			for _, ct := range columnTypes(t, db, tt.query) {
				names = append(names, ct.DatabaseTypeName())
				scans = append(scans, strings.ReplaceAll(ct.ScanType().String(), " ", "_"))
				if nullable, ok := ct.Nullable(); !nullable || !ok {
					t.Errorf("%s: the column %s cannot be NULL, want every column nullable", release, ct.Name())
				}
			}
			if got := strings.Join(names, " "); got != tt.names {
				t.Errorf("%s: %.40q: the types are %q, want %q", release, tt.query, got, tt.names)
			}
			if got := strings.Join(scans, " "); got != tt.scans {
				t.Errorf("%s: %.40q: the scan types are %q, want %q", release, tt.query, got, tt.scans)
			}
		}
	}
}

// TestReplayValues holds the Go value of each type that an index does not
// hold, of each interval, and of the values at the limits of a date, a
// datetime and a double.
func TestReplayValues(t *testing.T) {
	t.Parallel()
	kolkata := time.FixedZone("", 5*3600+1800)
	clock := dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}}
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			args  []any
			want  [][]any
		}{
			{literals, nil, [][]any{{dbimp.Date{Year: 2026, Month: 10, Day: 1}, clock, utc(t, "2026-10-01T07:04:56.789Z"), 1.5,
				int64(math.MaxInt64), uint64(math.MaxUint64), int64(1), int64(1), 1.5, 1.5, true, "1.2.3.4", "abc", nil, nil}}},
			{intervals, nil, [][]any{{
				dbimp.Interval{Months: 12}, dbimp.Interval{Months: 1}, 48 * time.Hour, 3 * time.Hour, 3 * time.Minute, 4 * time.Second,
				dbimp.Interval{Months: 14}, 26 * time.Hour, 26*time.Hour + 3*time.Minute, 26*time.Hour + 3*time.Minute + 4500*time.Millisecond,
				2*time.Hour + 3*time.Minute, 2*time.Hour + 3*time.Minute + 4500*time.Millisecond, 3*time.Minute + 4500*time.Millisecond,
				-24 * time.Hour, dbimp.Interval{Months: -14},
			}}},
			{"SELECT CAST('0001-01-01' AS DATE) AS a, CAST('-0001-01-01' AS DATE) AS b, CAST('9999-12-31T23:59:59.999Z' AS DATETIME) AS c, CAST('2026-10-01T12:34:56.123456789Z' AS DATETIME) AS d", nil,
				[][]any{{dbimp.Date{Year: 1, Month: 1, Day: 1}, dbimp.Date{Year: -1, Month: 1, Day: 1}, utc(t, "9999-12-31T23:59:59.999Z"), utc(t, "2026-10-01T12:34:56.123456789Z")}}},
			// time_zone moves the offset of each time, and the day of a date
			// stays the day of the text (D167).
			{"SELECT dt, dn, CAST(dt AS DATE) AS d, CAST(dt AS TIME) AS t FROM dbmeta_types WHERE id = 1", []any{elasticsearch.WithTimeZone("+05:30")},
				[][]any{{time.Date(2026, 10, 1, 12, 34, 56, 789e6, kolkata), time.Date(2026, 10, 1, 18, 4, 56, 123456789, kolkata),
					dbimp.Date{Year: 2026, Month: 10, Day: 1},
					dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}, Offset: 19800}}}},
			{"SELECT SQRT(-1) AS nan, 1e308 * 10 AS inf", nil, nil},
			{"SELECT HISTOGRAM(n, 100) AS h, COUNT(*) AS c FROM dbmeta_paging GROUP BY h ORDER BY h", nil, [][]any{
				{int64(0), int64(99)}, {int64(100), int64(100)}, {int64(200), int64(51)},
			}},
			{"SELECT AVG(n) AS a, SUM(n) AS s, MIN(s) AS m, COUNT(*) AS c FROM dbmeta_paging", nil, [][]any{{125.5, int64(31375), "row 1", int64(250)}}},
			{"SELECT t, SCORE() AS sc FROM dbmeta_types WHERE MATCH(t, 'text')", nil, [][]any{{"Some text", 0.2876821}}},
			{"SELECT ST_AsWKT(gp) AS w, ST_X(gp) AS x, ST_Y(gp) AS y, ST_GeometryType(gs) AS g FROM dbmeta_types WHERE id = 1", nil, [][]any{{"POINT (-71.34 41.12)", -71.34, 41.12, "POINT"}}},
			{"SELECT t, t.raw FROM dbmeta_types WHERE id = 1", nil, [][]any{{"Some text", "Some text"}}},
			{"SELECT al FROM dbmeta_types WHERE id = 1", nil, [][]any{{int64(math.MaxInt32)}}},
			{"SELECT o.a, o.s FROM dbmeta_types WHERE id = 1", nil, [][]any{{int64(1), "x"}}},
			{"SELECT id, n.a FROM dbmeta_types WHERE id < 3 ORDER BY id", nil, [][]any{{int64(1), int64(1)}, {int64(1), int64(2)}}},
		} {
			_, got, err := readAll(t, db, tt.query, tt.args...)
			if tt.want == nil {
				nan, ok1 := got[0][0].(float64)
				inf, ok2 := got[0][1].(float64)
				if err != nil || len(got) != 1 || !ok1 || !ok2 || !math.IsNaN(nan) || !math.IsInf(inf, 1) {
					t.Errorf("%s: NaN and the infinity gave %v, %v", release, got, err)
				}
				continue
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %.60q gave\n%#v, %v\nwant\n%#v", release, tt.query, got, err, tt.want)
			}
		}
	}
}

// TestReplayLeniency holds D167: a field with several values fails the
// statement, and fails with the leniency off before any row, and with
// field_multi_value_leniency the first value arrives.
func TestReplayLeniency(t *testing.T) {
	t.Parallel()
	const (
		query  = "SELECT id, i, k, o.a FROM dbmeta_types ORDER BY id"
		strict = "SELECT id, i, k FROM dbmeta_types ORDER BY id"
	)
	for _, release := range releases {
		db := replay(t, release)
		_, got, err := readAll(t, db, strict)
		e := serverError(t, err)
		if len(got) != 0 || e.HTTPStatus != http.StatusBadRequest || !strings.Contains(e.Reason, "Arrays (returned by [i]) are not supported") || errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: a field with several values gave %v and %v, want the error of HTTP 400 before any row", release, got, err)
		}
		want := [][]any{
			{int64(1), int64(math.MaxInt32), "é'\"\\ x", int64(1)},
			{int64(2), int64(math.MinInt32), "", nil},
			{int64(3), nil, nil, nil},
			{int64(4), nil, nil, nil},
			{int64(5), int64(1), "a", int64(1)},
		}
		for name, opts := range map[string][]any{
			"the option": {elasticsearch.WithFieldMultiValueLeniency(true)},
			"the DSN":    nil,
		} {
			d := db
			if opts == nil {
				d, _ = replayOnly(t, release, "?field_multi_value_leniency=true", nil)
			}
			_, got, err := readAll(t, d, query, opts...)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("%s: with leniency from %s the rows are %v, %v, want %v", release, name, got, err, want)
			}
		}
	}
}

// TestReplayResults holds that a result keeps the order of its columns and its
// rows, that two columns can have one name, that a result with no rows has its
// columns, and that a result of 250 rows comes whole in the page of 1000.
func TestReplayResults(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			args  []any
			cols  string
			rows  int
			first any
		}{
			{"SELECT s, n FROM dbmeta_paging WHERE n = 1", nil, "s n", 1, "row 1"},
			{"SELECT n, n FROM dbmeta_paging WHERE n = 1", nil, "n n", 1, int64(1)},
			{"SELECT * FROM dbmeta_paging WHERE n = 1", nil, "id n s", 1, int64(1)},
			{"SELECT n, s FROM dbmeta_paging WHERE n = 0", nil, "n s", 0, nil},
			{"SELECT n FROM dbmeta_paging ORDER BY n", nil, "n", 250, int64(1)},
			{"SELECT TOP 2 n FROM dbmeta_paging ORDER BY n", nil, "n", 2, int64(1)},
			{"SELECT n FROM dbmeta_alias ORDER BY n", nil, "n", 3, int64(1)},
			{"SELECT n FROM dbmeta_window ORDER BY n", []any{elasticsearch.WithFetchSize(100)}, "n", 250, int64(1)},
		} {
			cols, got, err := readAll(t, db, tt.query, tt.args...)
			switch {
			case err != nil:
				t.Errorf("%s: %.50q: %v", release, tt.query, err)
			case strings.Join(cols, " ") != tt.cols || len(got) != tt.rows:
				t.Errorf("%s: %.50q gave the columns %q and %d rows, want %q and %d", release, tt.query, cols, len(got), tt.cols, tt.rows)
			case tt.rows > 0 && got[0][0] != tt.first:
				t.Errorf("%s: %.50q gave the first value %v, want %v", release, tt.query, got[0][0], tt.first)
			}
		}
	}
}

// TestReplayPages holds D167: the driver follows the cursor to the last page
// and reads every row once, in order.
func TestReplayPages(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db, s := replayOnly(t, release, "?fetch_size=100", nil)
		_, got, err := readAll(t, db, "SELECT n, s FROM dbmeta_paging ORDER BY n")
		if err != nil || len(got) != 250 {
			t.Fatalf("%s: read %d rows and %v, want 250 rows", release, len(got), err)
		}
		for i, row := range got {
			if row[0] != int64(i+1) || row[1] != "row "+strconv.Itoa(i+1) {
				t.Fatalf("%s: row %d is %v, want the row %d", release, i+1, row, i+1)
			}
		}
		if reqs := s.requests(); !slices.Equal(reqs, []string{"POST /_sql", "POST /_sql", "POST /_sql"}) {
			t.Errorf("%s: the requests are %v, want one for each of the 3 pages and no close", release, reqs)
		}
	}
}

// TestReplayCloseCursor holds D167: rows that the caller closes before the
// end close the cursor on the server, and the answer {"succeeded": true} is
// no error.
func TestReplayCloseCursor(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db, s := replayOnly(t, release, "?fetch_size=100", nil)
		rows, err := db.QueryContext(t.Context(), "SELECT n FROM dbmeta_paging")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if !rows.Next() {
			t.Fatalf("%s: no row: %v", release, rows.Err())
		}
		if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test closes the rows early on purpose, and holds the error and the cursor.
			t.Errorf("%s: closing the rows: %v", release, err)
		}
		if reqs := s.requests(); !slices.Equal(reqs, []string{"POST /_sql", "POST /_sql/close"}) {
			t.Errorf("%s: the requests are %v, want the statement and the close of its cursor", release, reqs)
		}
	}
}

// TestReplayErrors holds that an error before any rows is the error of the
// server, with its status and its type, and that it does not wrap
// dbimp.ErrIncomplete (D107).
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query  string
			args   []any
			status int
			typ    string
			text   string
		}{
			{"SELEC 1", nil, http.StatusBadRequest, "parsing_exception", "mismatched input 'SELEC'"},
			{"SELECT a FROM dbmeta_none", nil, http.StatusBadRequest, "verification_exception", "Unknown index [dbmeta_none]"},
			{"SELECT none FROM dbmeta_paging", nil, http.StatusBadRequest, "verification_exception", "Unknown column [none]"},
			{"SELECT n / 0 AS x FROM dbmeta_paging WHERE n = 1", nil, http.StatusInternalServerError, "arithmetic_exception", "/ by zero"},
			{"SELECT id, i FROM dbmeta_types ORDER BY id", nil, http.StatusBadRequest, "invalid_argument_exception", "Arrays (returned by [i]) are not supported"},
			{"SELECT o FROM dbmeta_types WHERE id = 1", nil, http.StatusBadRequest, "verification_exception", "Cannot use field [o] type [object]"},
			{"SELECT n FROM dbmeta_types WHERE id = 1", nil, http.StatusBadRequest, "verification_exception", "Cannot use field [n] type [nested]"},
			{"SELECT dv FROM dbmeta_types", nil, http.StatusBadRequest, "verification_exception", "unsupported type [dense_vector]"},
			{"SELECT fl FROM dbmeta_types", nil, http.StatusBadRequest, "verification_exception", "unsupported type [flattened]"},
			{"SELECT _id FROM dbmeta_types", nil, http.StatusBadRequest, "verification_exception", "Unknown column [_id]"},
			{"SELECT 123456789012345678901234567890 AS x", nil, http.StatusBadRequest, "parsing_exception", "is too large"},
			{"SELECT 1.0E400 AS x", nil, http.StatusBadRequest, "parsing_exception", "is too large"},
			{"SELECT 1 AS one;", nil, http.StatusBadRequest, "parsing_exception", "extraneous input ';'"},
			{"SELECT 1; SELECT 2", nil, http.StatusBadRequest, "parsing_exception", "mismatched input ';'"},
			{"SELECT n FROM dbmeta_paging ORDER BY n LIMIT 2 OFFSET 1", nil, http.StatusBadRequest, "parsing_exception", "mismatched input 'OFFSET'"},
			{"SELECT * FROM information_schema.tables", nil, http.StatusBadRequest, "parsing_exception", "mismatched input '.'"},
			{"SELECT 1", []any{elasticsearch.WithParameter("nope", 1)}, http.StatusBadRequest, "x_content_parse_exception", "unknown field [nope]"},
		} {
			_, _, err := readAll(t, db, tt.query, tt.args...)
			e := serverError(t, err)
			typ := e.RootType
			if typ == "" {
				typ = e.Type
			}
			if e.HTTPStatus != tt.status || typ != tt.typ || !strings.Contains(err.Error(), tt.text) || errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("%s: %.50q gave %v, HTTP %d, want %s with HTTP %d and %q, before any row", release, tt.query, err, e.HTTPStatus, tt.typ, tt.status, tt.text)
			}
		}
	}
}

// TestReplayErrorAfterRows holds D167 and D107: a statement whose page of 2
// rows is followed by a page of 2 rows and then by a page that the server
// refuses, ends with the error of the server, which wraps dbimp.ErrIncomplete,
// after exactly the rows that arrived.
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		_, got, err := readAll(t, db, "SELECT id, i FROM dbmeta_types ORDER BY id", elasticsearch.WithFetchSize(2))
		e := serverError(t, err)
		want := [][]any{{int64(1), int64(math.MaxInt32)}, {int64(2), int64(math.MinInt32)}, {int64(3), nil}, {int64(4), nil}}
		if !reflect.DeepEqual(got, want) || !errors.Is(err, dbimp.ErrIncomplete) || e.HTTPStatus != http.StatusBadRequest || e.Type != "invalid_argument_exception" {
			t.Errorf("%s: the result is %v and %v, want 4 rows and then dbimp.ErrIncomplete with the error of HTTP 400", release, got, err)
		}
	}
}

// TestReplayWrites holds D163: SQL refuses every write and every statement of
// a schema with a parse error that names the statements it takes, and the
// driver returns the refusal.
func TestReplayWrites(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, query := range []string{
			"INSERT INTO dbmeta_crud (id, s) VALUES (1, 'a')",
			"UPDATE dbmeta_crud SET s = 'b' WHERE id = 1",
			"DELETE FROM dbmeta_crud WHERE id = 1",
			"CREATE TABLE dbmeta_x (a INT PRIMARY KEY)",
			"CREATE VIEW dbmeta_v AS SELECT n FROM dbmeta_paging",
			"BEGIN",
			"COMMIT",
		} {
			_, err := db.ExecContext(t.Context(), query)
			e := serverError(t, err)
			if e.HTTPStatus != http.StatusBadRequest || e.Type != "parsing_exception" || !strings.Contains(e.Reason, "expecting {'DEBUG', 'DESC', 'DESCRIBE', 'EXPLAIN', 'SELECT', 'SHOW', 'SYS', 'WITH', '('}") {
				t.Errorf("%s: %q gave %v, want the parse error of the server", release, query, err)
			}
		}
	}
}

// TestReplayPrincipals holds that a wrong password is HTTP 401, and that an
// index that the ordinary user cannot read is unknown to it, each an error
// before any row with no password in it.
func TestReplayPrincipals(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db, _ := replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusUnauthorized
		})
		_, _, err := readAll(t, db, "SELECT 1")
		if e := serverError(t, err); e.HTTPStatus != http.StatusUnauthorized || e.Type != "security_exception" || !strings.Contains(e.Reason, "unable to authenticate") {
			t.Errorf("%s: a wrong password gave %v", release, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: the error %v holds the password", release, err)
		}
		db, _ = replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusBadRequest
		})
		_, _, err = readAll(t, db, "SELECT a FROM dbimp_secret")
		if e := serverError(t, err); e.HTTPStatus != http.StatusBadRequest || !strings.Contains(e.Reason, "Unknown index [dbimp_secret]") {
			t.Errorf("%s: an index that the ordinary user cannot read gave %v", release, err)
		}
	}
}

// TestReplayTimeout holds that a statement that passes its request_timeout is
// an error of the server, HTTP 504 on 8.19.22 and HTTP 429 on 9.4.6 and 9.5.3,
// that the driver sends no request again for HTTP 429 (D8), and that the
// root cause names the timeout.
func TestReplayTimeout(t *testing.T) {
	t.Parallel()
	const filter = `{"script":{"script":{"source":"double x = doc[\"n\"].value; for (int i = 0; i < 900000; i++) { x = Math.sin(x) + Math.sqrt(i); } return x != 12345.0;"}}}`
	var f any
	if err := json.Unmarshal([]byte(filter), &f); err != nil {
		t.Fatal(err)
	}
	for _, release := range releases {
		db, s := replayOnly(t, release, "", nil)
		_, _, err := readAll(t, db, "SELECT n FROM dbmeta_paging ORDER BY s DESC",
			elasticsearch.WithFetchSize(10), elasticsearch.WithParameter("filter", f), elasticsearch.WithTimeout(100*time.Millisecond))
		e := serverError(t, err)
		status := http.StatusTooManyRequests
		if release == "elasticsearch-8.19.22" {
			status = http.StatusGatewayTimeout
		}
		if e.HTTPStatus != status || e.RootType != "search_timeout_exception" || e.Type != "search_phase_execution_exception" {
			t.Errorf("%s: the error is %v, want search_timeout_exception with HTTP %d", release, err, status)
		}
		if n := len(s.requests()); n != 1 {
			t.Errorf("%s: the driver sent %d requests, want 1", release, n)
		}
	}
}

// TestReplayColumnar holds that an answer with values in place of rows,
// which a caller can ask for with WithParameter("columnar", true), is an
// error, and never a result with the wrong shape.
func TestReplayColumnar(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		_, _, err := readAll(t, db, "SELECT n, s FROM dbmeta_paging WHERE n < 4 ORDER BY n", elasticsearch.WithParameter("columnar", true))
		if !errors.Is(err, dbimp.ErrInvalidValue) || errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: a columnar answer gave %v, want an error that wraps dbimp.ErrInvalidValue", release, err)
		}
	}
}

// TestReplayParameters holds D167: the server binds each argument from params,
// and the driver refuses what the server cannot bind.
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			args  []any
			want  [][]any
		}{
			{"SELECT n, s FROM dbmeta_paging WHERE n = ? AND s = ?", []any{int64(5), "row 5"}, [][]any{{int64(5), "row 5"}}},
			// A time is a string in ISO 8601, which the server compares with a
			// datetime column to the nanosecond.
			{"SELECT id FROM dbmeta_types WHERE dt = ?", []any{time.Date(2026, 10, 1, 14, 4, 56, 789e6, time.FixedZone("", 7*3600))}, [][]any{{int64(1)}}},
			{"SELECT n FROM dbmeta_paging WHERE n IN (?, ?) ORDER BY n", []any{int64(1), int64(2)}, [][]any{{int64(1)}, {int64(2)}}},
			{"SELECT '?' AS a, ? AS b", []any{int64(1)}, [][]any{{"?", int64(1)}}},
			// A value too many is ignored (measured).
			{"SELECT ? AS a", []any{int64(1), int64(2)}, [][]any{{int64(1)}}},
		} {
			_, got, err := readAll(t, db, tt.query, tt.args...)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %.50q with %v gave %#v, %v, want %#v", release, tt.query, tt.args, got, err, tt.want)
			}
		}
		_, _, err := readAll(t, db, "SELECT ? AS a, ? AS b", int64(1))
		if e := serverError(t, err); e.HTTPStatus != http.StatusBadRequest || !strings.Contains(e.Reason, "Not enough actual parameters") {
			t.Errorf("%s: too few arguments gave %v", release, err)
		}
		_, _, err = readAll(t, db, "SELECT n FROM dbmeta_paging ORDER BY n LIMIT ?", int64(1))
		if e := serverError(t, err); e.HTTPStatus != http.StatusBadRequest || e.Type != "parsing_exception" {
			t.Errorf("%s: a parameter in LIMIT gave %v, want a parse error", release, err)
		}
		if _, _, err := readAll(t, db, "SELECT n FROM dbmeta_paging WHERE n = :x", sql.Named("x", 1)); !errors.Is(err, dbimp.ErrArguments) {
			t.Errorf("%s: a named argument gave %v, want dbimp.ErrArguments", release, err)
		}
		if _, _, err := readAll(t, db, "SELECT ?", uint64(math.MaxUint64)); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: a uint64 above int64 gave %v, want dbimp.ErrNotSupported", release, err)
		}
		// The cast of a string keeps what a number cannot: every digit of an
		// unsigned_long.
		_, got, err := readAll(t, db, "SELECT CAST(? AS UNSIGNED_LONG) AS u, CAST(? AS DATE) AS d, ?::INTEGER AS i", "18446744073709551615", "2026-10-01", "7")
		want := [][]any{{uint64(math.MaxUint64), dbimp.Date{Year: 2026, Month: 10, Day: 1}, int64(7)}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the casts of strings gave %#v, %v, want %#v", release, got, err, want)
		}
	}
}

// TestReplayStatements holds the statements of the catalog and the features
// of a statement that a query shows: comments, the user and the cluster, and
// BeginTx.
func TestReplayStatements(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			cols  string
			rows  [][]any
		}{
			{"-- a comment\nSELECT /* inline */ 1 AS one -- the end", "one", [][]any{{int64(1)}}},
			{"SELECT DATABASE() AS d, USER() AS u", "d u", nil},
			{"SHOW TABLES LIKE 'dbimp%'", "catalog name type kind", nil},
			{"SHOW CATALOGS", "name type", nil},
			{"DESCRIBE dbmeta_types", "column type mapping", nil},
			{"SHOW COLUMNS IN dbmeta_types", "column type mapping", nil},
			{"SHOW FUNCTIONS LIKE 'DATE%'", "name type", nil},
		} {
			cols, got, err := readAll(t, db, tt.query)
			if err != nil || len(got) == 0 || (tt.cols != "" && strings.Join(cols, " ") != tt.cols) {
				t.Errorf("%s: %.50q gave the columns %q, %d rows and %v, want the columns %q and rows", release, tt.query, cols, len(got), err, tt.cols)
				continue
			}
			if tt.rows != nil && !reflect.DeepEqual(got, tt.rows) {
				t.Errorf("%s: %.50q gave %#v, want %#v", release, tt.query, got, tt.rows)
			}
		}
		_, got, err := readAll(t, db, "SELECT DATABASE() AS d, USER() AS u")
		if want := [][]any{{"docker-cluster", "elastic"}}; err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: DATABASE and USER gave %v, %v", release, got, err)
		}
		if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: BeginTx gave %v, want dbimp.ErrNotSupported (D167)", release, err)
		}
	}
}

// TestReplayStepSeven holds what step 7 recorded on 9.5.3 only: a decimal, the
// SQL types, a subquery, an index pattern, the catalog, runtime mappings, the
// statements that the server refuses, the nanoseconds of a parameter, and the
// keys that the server does not know.
func TestReplayStepSeven(t *testing.T) {
	t.Parallel()
	db := replay(t, "elasticsearch-9.5.3")
	for _, tt := range []struct {
		query string
		args  []any
		rows  [][]any
	}{
		{"SELECT CAST(1.5 AS DECIMAL) AS d", nil, [][]any{{1.5}}},
		{"SELECT n FROM (SELECT n FROM dbmeta_paging WHERE n < 3) ORDER BY n", nil, [][]any{{int64(1)}, {int64(2)}}},
		{`SELECT n FROM "dbmeta_p*" WHERE n < 3 ORDER BY n`, nil, [][]any{{int64(1)}, {int64(2)}}},
		{"SELECT n FROM dbmeta_paging WHERE n < 3 ORDER BY n", []any{elasticsearch.WithCatalog("docker-cluster")}, [][]any{{int64(1)}, {int64(2)}}},
		{"SELECT n, d FROM dbmeta_paging WHERE n < 3 ORDER BY n", []any{elasticsearch.WithParameter("runtime_mappings", map[string]any{
			"d": map[string]any{"type": "long", "script": map[string]any{"source": `emit(doc["n"].value * 2)`}},
		})}, [][]any{{int64(1), int64(2)}, {int64(2), int64(4)}}},
		{"SELECT id FROM dbmeta_types WHERE dn > ?", []any{time.Date(2026, 10, 1, 12, 34, 56, 123456788, time.UTC)}, [][]any{{int64(1)}}},
		{"SELECT n FROM dbmeta_paging WHERE n < 3 ORDER BY n", []any{elasticsearch.WithParameter("allow_partial_search_results", true)}, [][]any{{int64(1)}, {int64(2)}}},
	} {
		_, got, err := readAll(t, db, tt.query, tt.args...)
		if err != nil || !reflect.DeepEqual(got, tt.rows) {
			t.Errorf("%.50q gave %#v, %v, want %#v", tt.query, got, err, tt.rows)
		}
	}
	cols, got, err := readAll(t, db, "SYS TYPES")
	if err != nil || len(cols) != 19 || cols[0] != "TYPE_NAME" || len(got) < 30 {
		t.Errorf("SYS TYPES gave %d columns, %d rows and %v", len(cols), len(got), err)
	}
	for _, tt := range []struct {
		query  string
		args   []any
		status int
		typ    string
		text   string
	}{
		{"SELECT a.n FROM dbmeta_paging a JOIN dbmeta_window b ON a.n = b.n", nil, http.StatusBadRequest, "parsing_exception", "JOIN are not yet supported"},
		{"WITH x AS (SELECT n FROM dbmeta_paging) SELECT n FROM x", nil, http.StatusBadRequest, "verification_exception", "Unknown index [x]"},
		{"SELECT n FROM dbmeta_paging WHERE n < 3", []any{elasticsearch.WithCatalog("other")}, http.StatusNotFound, "no_such_remote_cluster_exception", "no such remote cluster"},
		{"SELECT n FROM dbmeta_paging WHERE n < 3 ORDER BY n", []any{elasticsearch.WithParameter("index_using_frozen", true)}, http.StatusBadRequest, "x_content_parse_exception", "unknown field [index_using_frozen]"},
		{"SELECT n FROM dbmeta_paging WHERE n < 3 ORDER BY n", []any{elasticsearch.WithParameter("project_routing", "_alias:_origin")}, http.StatusBadRequest, "invalid_argument_exception", "cross-project search"},
	} {
		_, _, err := readAll(t, db, tt.query, tt.args...)
		e := serverError(t, err)
		if e.HTTPStatus != tt.status || e.Type != tt.typ || !strings.Contains(err.Error(), tt.text) {
			t.Errorf("%.50q gave %v, HTTP %d, want %s with HTTP %d and %q", tt.query, err, e.HTTPStatus, tt.typ, tt.status, tt.text)
		}
	}
}
