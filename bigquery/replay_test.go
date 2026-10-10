package bigquery //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded, under
// testdata/bigquery/. Each one decodes a real answer through the driver. The
// hosted recordings are the files bigquery-NNN, and the emulator recordings are
// bigquery-0.8.1-NNN. The recorder sent only the members that its script held,
// so the fake server matches a statement by its path, its text and its
// parameters, and ignores the other members that the driver sends (D189). A
// test never holds a response written as a string.

const testdata = "../testdata/bigquery"

// read reads the exchanges of the files that match pattern.
func read(pattern string) func() ([]*dbimptest.Exchange, error) {
	return sync.OnceValues(func() ([]*dbimptest.Exchange, error) {
		paths, err := filepath.Glob(filepath.Join(testdata, pattern))
		if err != nil {
			return nil, err
		}
		var out []*dbimptest.Exchange
		for _, path := range paths {
			ex, err := dbimptest.ReadExchange(path)
			if err != nil {
				return nil, err
			}
			out = append(out, ex)
		}
		return out, nil
	})
}

// The recorded exchanges of the hosted service, and of the emulator 0.8.1.
var (
	hosted   = read("bigquery-[0-9][0-9][0-9]-*.json")
	emulator = read("bigquery-0.8.1-*.json")
)

// sameJSON reports whether two bodies of JSON are the same in their canonical
// forms. Two bodies that are empty are the same.
func sameJSON(a, b []byte) bool {
	if len(bytes.TrimSpace(a)) == 0 || len(bytes.TrimSpace(b)) == 0 {
		return len(bytes.TrimSpace(a)) == len(bytes.TrimSpace(b))
	}
	va, vb := jsontext.Value(bytes.Clone(a)), jsontext.Value(bytes.Clone(b))
	if va.Canonicalize() != nil || vb.Canonicalize() != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(va, vb)
}

// statement is the part of the body of POST /queries that a recording and a
// request must share.
type statement struct {
	Query         string         `json:"query"`
	ParameterMode string         `json:"parameterMode"`
	Parameters    jsontext.Value `json:"queryParameters"`
	DryRun        bool           `json:"dryRun"`
	MaxResults    int            `json:"maxResults"`
}

// replayServer answers each request with a recorded exchange.
type replayServer struct {
	t   *testing.T
	set func() ([]*dbimptest.Exchange, error)
	// only, when it is not 0, limits the answers to the exchanges with that
	// status. Otherwise the answers of HTTP 401 are left out, because the
	// request that a wrong token made is the request of a plain statement.
	only int

	mu   sync.Mutex
	log  []string
	body [][]byte
	// query holds the query of each request.
	query []url.Values
	// inflight and most count the requests that run at once.
	inflight, most int
}

func (s *replayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.inflight++
	s.most = max(s.most, s.inflight)
	entry := r.Method + " " + r.URL.Path
	if p := r.URL.Query().Get("pageToken"); p != "" {
		entry += "?pageToken"
	}
	s.log = append(s.log, entry)
	s.query = append(s.query, r.URL.Query())
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading a request to the fake server: %v", err)
		return
	}
	s.mu.Lock()
	s.body = append(s.body, body)
	s.mu.Unlock()
	all, err := s.set()
	if err != nil {
		s.t.Errorf("reading the recorded exchanges: %v", err)
		return
	}
	i := slices.IndexFunc(all, func(ex *dbimptest.Exchange) bool { return s.matches(r, body, ex) })
	if i < 0 && strings.HasSuffix(r.URL.Path, "/cancel") {
		// The driver cancels a job that the test chose, and the recordings hold
		// the cancel of other jobs. The service answers the same to a cancel of a
		// job that ended (recorded: bigquery-213).
		i = slices.IndexFunc(all, func(ex *dbimptest.Exchange) bool {
			return ex.Request.Method == r.Method && strings.HasSuffix(ex.Request.Path, "/cancel") && ex.Response.Status == http.StatusOK
		})
	}
	if i < 0 {
		s.t.Errorf("no exchange matches %s %s %s", r.Method, r.URL, body)
		http.Error(w, "no exchange matches", http.StatusTeapot)
		return
	}
	res := all[i].Response
	for key, vals := range res.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	w.WriteHeader(res.Status)
	if _, err := w.Write(res.Content()); err != nil {
		s.t.Errorf("writing a response from the fake server: %v", err)
	}
}

// matches reports whether the request is the request of ex.
func (s *replayServer) matches(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	switch {
	case ex.Request.Method != r.Method || ex.Request.Path != r.URL.Path:
		return false
	case s.only != 0 && ex.Response.Status != s.only:
		return false
	case s.only == 0 && ex.Response.Status == http.StatusUnauthorized:
		return false
	}
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/queries"):
		var got, want statement
		if json.Unmarshal(body, &got) != nil || json.Unmarshal(ex.Request.Content(), &want) != nil {
			return false
		}
		return got.Query == want.Query && got.ParameterMode == want.ParameterMode && got.DryRun == want.DryRun && got.MaxResults == want.MaxResults && sameJSON(got.Parameters, want.Parameters)
	case r.Method == http.MethodGet:
		recorded, err := url.ParseQuery(ex.Request.Query)
		return err == nil && recorded.Get("pageToken") == r.URL.Query().Get("pageToken")
	}
	return true
}

// requests returns the method and the path of each request that the server got.
func (s *replayServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.log)
}

// queries returns the query of each request that the server got.
func (s *replayServer) queries() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.query)
}

// lastBody returns the body of the last request that the server got, as a map.
func (s *replayServer) lastBody(t *testing.T) map[string]any {
	t.Helper()
	return s.bodyAt(t, -1)
}

// bodyAt returns the body of the request number i, as a map, and the last one
// for -1.
func (s *replayServer) bodyAt(t *testing.T, i int) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.body) == 0 {
		t.Fatal("the server got no request")
	}
	if i < 0 {
		i = len(s.body) - 1
	}
	var m map[string]any
	if err := json.Unmarshal(s.body[i], &m); err != nil {
		t.Fatalf("reading the body %d: %v", i, err)
	}
	return m
}

// replay opens the driver against the hosted recordings.
func replay(t *testing.T) (*sql.DB, *replayServer) {
	t.Helper()
	return replayWith(t, config(), &replayServer{t: t, set: hosted})
}

// replayEmulator opens the driver against the recordings of the emulator 0.8.1,
// whose project is dbmeta.
func replayEmulator(t *testing.T) (*sql.DB, *replayServer) {
	t.Helper()
	cfg := config()
	cfg.Project = emulatorProject
	return replayWith(t, cfg, &replayServer{t: t, set: emulator})
}

// replayWith opens the driver against s.
func replayWith(t *testing.T, cfg Config, s *replayServer) (*sql.DB, *replayServer) {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return open(t, cfg, srv.URL), s
}

// readAll runs a statement and reads every row into *any.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any) {
	t.Helper()
	cols, out, err := readAllContext(t, t.Context(), db, query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return cols, out
}

// readAllContext is readAll that returns the error, and takes a context.
func readAllContext(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
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

// dec makes a decimal.
func dec(tb testing.TB, s string) *apd.Decimal {
	tb.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		tb.Fatalf("reading %q as a decimal: %v", s, err)
	}
	return d
}

// same reports whether two values are the same: a decimal by its number, a time
// by its instant, a NaN with a NaN, and a list and a map by their members.
func same(got, want any) bool {
	switch w := want.(type) {
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case time.Time:
		g, ok := got.(time.Time)
		return ok && g.Equal(w) && g.Location() == w.Location()
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case []any:
		g, ok := got.([]any)
		return ok && slices.EqualFunc(g, w, same)
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, v := range w {
			if gv, ok := g[k]; !ok || !same(gv, v) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// check fails the test when the rows differ from want.
func check(t *testing.T, name string, got, want [][]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: got %d rows, want %d: %v", name, len(got), len(want), got)
		return
	}
	for i, w := range want {
		g := got[i]
		if len(g) != len(w) {
			t.Errorf("%s: row %d has %d values, want %d", name, i, len(g), len(w))
			continue
		}
		for j, wv := range w {
			if gv := g[j]; !same(gv, wv) {
				t.Errorf("%s: row %d column %d is %#v (%T), want %#v (%T)", name, i, j, gv, gv, wv, wv)
			}
		}
	}
}

// at makes a time of the second 1704164645, which is 2024-01-02 03:04:05 UTC, with
// the microseconds.
func at(micro int) time.Time {
	return time.Unix(1704164645, int64(micro)*1000).UTC()
}

// TestReplaySelect holds D189 with the recording of a plain statement: the
// columns keep their order, the values have their Go types, and the request
// holds the members that the driver always sends.
func TestReplaySelect(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	cols, rows := readAll(t, db, "SELECT 1 AS a, 'x' AS b")
	if !slices.Equal(cols, []string{"a", "b"}) {
		t.Errorf("the columns are %v", cols)
	}
	check(t, "select", rows, [][]any{{int64(1), "x"}})
	if got, want := s.requests(), []string{"POST /bigquery/v2/projects/dbimp-project/queries"}; !slices.Equal(got, want) {
		t.Errorf("the requests are %v, want %v", got, want)
	}
	body := s.lastBody(t)
	if body["useLegacySql"] != false {
		t.Errorf("useLegacySql is %v, want false (D189)", body["useLegacySql"])
	}
	if fo, _ := body["formatOptions"].(map[string]any); fo["timestampOutputFormat"] != "ISO8601_STRING" {
		t.Errorf("formatOptions is %v, want ISO8601_STRING (D189)", body["formatOptions"])
	}
	for _, key := range []string{"queryParameters", "parameterMode", "location", "defaultDataset", "maxResults", "jobTimeoutMs"} {
		if _, ok := body[key]; ok {
			t.Errorf("the body holds %s, which this statement did not set: %v", key, body)
		}
	}
}

// TestReplayEveryType holds D135 and D189 with the recording of a table that
// holds a column of every type, whose second row is NULL in every column. The
// default form of a TIMESTAMP, a double with an exponent, goes through the
// reader of the emulator form too.
func TestReplayEveryType(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	cols, rows := readAll(t, db, "SELECT * FROM `dbimp-project`.dbimp_test.dbimp_it_types ORDER BY id")
	if want := []string{"id", "i", "f", "n", "bn", "b", "s", "y", "d", "t", "dt", "ts", "j", "g", "iv", "arr", "st", "r"}; !slices.Equal(cols, want) {
		t.Errorf("the columns are %v, want %v", cols, want)
	}
	check(t, "types", rows, [][]any{
		{
			int64(1), int64(-5), 1.25, dec(t, "1.5"), dec(t, "1.5"), true, "x", []byte("abc"),
			dbimp.Date{Year: 2024, Month: time.January, Day: 2},
			dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000},
			dbimp.LocalDateTime{Date: dbimp.Date{Year: 2024, Month: time.January, Day: 2}, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5, Nanosecond: 123456000}},
			at(123456),
			map[string]any{"a": int64(1)},
			"POINT(1 2)",
			dbimp.Interval{Days: 1},
			[]any{int64(1), int64(2)},
			map[string]any{"a": int64(1), "b": "x"},
			"[2024-01-01, 2024-02-01)",
		},
		{int64(2), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, []any{}, nil, nil},
	})
}

// TestReplayEveryTypeOnTheEmulator reads the same table from the recording of
// the emulator 0.8.1, which writes a TIME with three digits, a TIMESTAMP as
// seconds with six fixed digits, and a GEOGRAPHY with a space after its name.
func TestReplayEveryTypeOnTheEmulator(t *testing.T) {
	t.Parallel()
	db, _ := replayEmulator(t)
	_, rows := readAll(t, db, "SELECT * FROM dbmeta.dbmeta.dbimp_it_types ORDER BY id")
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %v", len(rows), rows)
	}
	if got, want := rows[0][11], at(123456); !same(got, want) {
		t.Errorf("the timestamp is %v, want %v", got, want)
	}
	if got, want := rows[0][9], (dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}); !same(got, want) {
		t.Errorf("the time is %v, want %v", got, want)
	}
	if got := rows[0][13]; got != "POINT (1 2)" {
		t.Errorf("the geography is %#v, want the text of the emulator", got)
	}
}

// sumOfTenths returns 0.1 + 0.2 as a float64 adds them at run time, which is
// not the constant 0.3 that Go makes of two untyped constants.
func sumOfTenths() float64 {
	a, b := 0.1, 0.2
	return a + b
}

// TestReplayNumbers holds D135 with the limits of the numeric types: the
// extremes of an INT64, a FLOAT64 in the form of Java, the infinities and NaN
// as text, and the NUMERIC and BIGNUMERIC limits with their last digit.
func TestReplayNumbers(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, tt := range []struct {
		name  string
		query string
		want  []any
	}{
		{"int64", "SELECT CAST(9223372036854775807 AS INT64) AS hi, CAST(-9223372036854775808 AS INT64) AS lo", []any{int64(math.MaxInt64), int64(math.MinInt64)}},
		{"float64", "SELECT 1.5 AS a, 1e308 AS big, 5e-324 AS tiny, -0.0 AS negzero, 0.1 + 0.2 AS sum", []any{1.5, 1e308, 5e-324, math.Copysign(0, -1), sumOfTenths()}},
		{"infinity", "SELECT IEEE_DIVIDE(1, 0) AS pinf, IEEE_DIVIDE(-1, 0) AS ninf, IEEE_DIVIDE(0, 0) AS nan", []any{math.Inf(1), math.Inf(-1), math.NaN()}},
		{"bool", "SELECT TRUE AS t, FALSE AS f", []any{true, false}},
	} {
		_, rows := readAll(t, db, tt.query)
		check(t, tt.name, rows, [][]any{tt.want})
	}
	_, rows := readAll(t, db, "SELECT NUMERIC '99999999999999999999999999999.999999999' AS hi, NUMERIC '-99999999999999999999999999999.999999999' AS lo, NUMERIC '0.000000001' AS small, NUMERIC '1' AS one")
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	for i, want := range []string{"99999999999999999999999999999.999999999", "-99999999999999999999999999999.999999999", "0.000000001", "1"} {
		if got, ok := rows[0][i].(*apd.Decimal); !ok || got.Text('f') != want {
			t.Errorf("the numeric %d is %v, want %s", i, rows[0][i], want)
		}
	}
}

// TestReplayText holds D135 with a string, which keeps an empty value, a newline
// and text outside ASCII, and with bytes, which come as base64.
func TestReplayText(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	_, rows := readAll(t, db, "SELECT '' AS empty, 'héllo 世界' AS unicode, 'a\\nb' AS newline, REPEAT('x', 1000) AS long")
	check(t, "strings", rows, [][]any{{"", "héllo 世界", "a\nb", strings.Repeat("x", 1000)}})
	_, rows = readAll(t, db, "SELECT b'' AS empty, b'\\x00\\xff' AS binary")
	check(t, "bytes", rows, [][]any{{[]byte{}, []byte{0, 0xff}}})
	_, rows = readAll(t, db, "SELECT ST_GEOGPOINT(1, 2) AS pt, ST_GEOGFROMTEXT('LINESTRING(0 0, 1 1)') AS line, ST_GEOGFROMTEXT('POLYGON((0 0, 1 0, 1 1, 0 0))') AS poly")
	check(t, "geography", rows, [][]any{{"POINT(1 2)", "LINESTRING(0 0, 1 1)", "POLYGON((0 0, 1 0, 1 1, 0 0))"}})
}

// TestReplayTimes holds D135 and D189 with the types of time: a DATE, a TIME and
// a DATETIME at their limits, and a TIMESTAMP in the ISO form that the driver
// asks for, and in the default form of the service, a double that has lost the
// microseconds at the year 9999 (recorded: bigquery-087).
func TestReplayTimes(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	_, rows := readAll(t, db, "SELECT DATE '0001-01-01' AS lo, DATE '9999-12-31' AS hi")
	check(t, "date", rows, [][]any{{dbimp.Date{Year: 1, Month: time.January, Day: 1}, dbimp.Date{Year: 9999, Month: time.December, Day: 31}}})
	_, rows = readAll(t, db, "SELECT TIME '00:00:00' AS lo, TIME '23:59:59.999999' AS hi")
	check(t, "time", rows, [][]any{{dbimp.LocalTime{}, dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999000}}})
	_, rows = readAll(t, db, "SELECT DATETIME '0001-01-01 00:00:00' AS lo, DATETIME '9999-12-31 23:59:59.999999' AS hi, DATETIME '2024-01-02 03:04:05' AS whole")
	check(t, "datetime", rows, [][]any{{
		dbimp.LocalDateTime{Date: dbimp.Date{Year: 1, Month: time.January, Day: 1}},
		dbimp.LocalDateTime{Date: dbimp.Date{Year: 9999, Month: time.December, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999000}},
		dbimp.LocalDateTime{Date: dbimp.Date{Year: 2024, Month: time.January, Day: 2}, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5}},
	}})
	_, rows = readAll(t, db, "SELECT TIMESTAMP '2024-01-02 03:04:05.123456+00' AS ts")
	check(t, "timestamp in the ISO form", rows, [][]any{{at(123456)}})
	if got, ok := rows[0][0].(time.Time); !ok || got.Location() != time.UTC {
		t.Errorf("the timestamp is %v, want a time in UTC", rows[0][0])
	}
	// The default form is a double, which loses the microseconds of a large value.
	// The recording shows the year 9999 as the first instant of the year 10000,
	// which no time of the service holds, and the driver names the column.
	_, _, err := readAllContext(t, t.Context(), db, "SELECT TIMESTAMP '0001-01-01 00:00:00+00' AS lo, TIMESTAMP '9999-12-31 23:59:59.999999+00' AS hi, TIMESTAMP '2024-01-02 03:04:05+00' AS whole, TIMESTAMP '2024-01-02 03:04:05.5+00' AS half, TIMESTAMP '1970-01-01 00:00:00+00' AS epoch")
	if err == nil || !strings.Contains(err.Error(), "column hi") || !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("a timestamp of the default form out of range gave %v, want an error that names the column hi", err)
	}
}

// TestReplayStructured holds D189 items 6 and 7 with the structured types: JSON
// is the decoded value, a STRUCT is a map by the names of its members, an ARRAY
// is a list, an INTERVAL has its three parts, and a RANGE is its text.
func TestReplayStructured(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	_, rows := readAll(t, db, `SELECT JSON '{"a":[1,2,{"b":null}]}' AS obj, JSON 'null' AS jnull, JSON '1' AS num, JSON '"s"' AS str, CAST(NULL AS JSON) AS sqlnull, JSON '12345678901234567890' AS big`)
	if len(rows) == 1 {
		check(t, "json", [][]any{rows[0][:4]}, [][]any{{map[string]any{"a": []any{int64(1), int64(2), map[string]any{"b": nil}}}, nil, int64(1), "s"}})
		if rows[0][4] != nil {
			t.Errorf("a SQL NULL JSON is %#v, want nil", rows[0][4])
		}
		if got := rows[0][5]; !same(got, dec(t, "12345678901234567890")) {
			t.Errorf("a JSON number of 20 digits is %#v, want a decimal that holds every digit", got)
		}
	}
	_, rows = readAll(t, db, "SELECT STRUCT([1, 2] AS inside) AS s")
	check(t, "struct with an array", rows, [][]any{{map[string]any{"inside": []any{int64(1), int64(2)}}}})
	_, rows = readAll(t, db, "SELECT [STRUCT(1 AS a, 'x' AS b), STRUCT(2, 'y')] AS s")
	check(t, "array of structs", rows, [][]any{{[]any{map[string]any{"a": int64(1), "b": "x"}, map[string]any{"a": int64(2), "b": "y"}}}})
	_, rows = readAll(t, db, "SELECT STRUCT(1 AS a, 'x' AS b) AS s, STRUCT(CAST(NULL AS INT64) AS a) AS withnull, CAST(NULL AS STRUCT<a INT64>) AS nullstruct")
	check(t, "struct", rows, [][]any{{map[string]any{"a": int64(1), "b": "x"}, map[string]any{"a": nil}, nil}})
	_, rows = readAll(t, db, "SELECT STRUCT(STRUCT(1 AS c) AS b, [1, 2] AS d) AS a")
	check(t, "nested struct", rows, [][]any{{map[string]any{"b": map[string]any{"c": int64(1)}, "d": []any{int64(1), int64(2)}}}})
	_, rows = readAll(t, db, "SELECT INTERVAL 1 DAY AS d, INTERVAL '1-2 3 4:5:6.789' YEAR TO SECOND AS whole, INTERVAL -5 MONTH AS neg, MAKE_INTERVAL(year => 10000) AS big")
	check(t, "interval", rows, [][]any{{
		dbimp.Interval{Days: 1},
		dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60+6)*int64(time.Second) + 789*int64(time.Millisecond)},
		dbimp.Interval{Months: -5},
		dbimp.Interval{Months: 120000},
	}})
}

// TestReplayColumns holds D18: a column that shares a name is renamed by the
// service, a column with no name has the name f0_, and a result with no rows
// still has its columns.
func TestReplayColumns(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	cols, rows := readAll(t, db, "SELECT 1 AS a, 2 AS a")
	if !slices.Equal(cols, []string{"a", "a_1"}) {
		t.Errorf("the columns are %v, want a and a_1", cols)
	}
	check(t, "shared name", rows, [][]any{{int64(1), int64(2)}})
	cols, rows = readAll(t, db, "SELECT 1, 'x', 2.5")
	if !slices.Equal(cols, []string{"f0_", "f1_", "f2_"}) {
		t.Errorf("the columns are %v, want f0_, f1_ and f2_", cols)
	}
	check(t, "no name", rows, [][]any{{int64(1), "x", 2.5}})
	cols, rows = readAll(t, db, "SELECT id, s FROM `dbimp-project`.dbimp_test.dbimp_it_dml WHERE FALSE")
	if !slices.Equal(cols, []string{"id", "s"}) || len(rows) != 0 {
		t.Errorf("a result with no rows gave the columns %v and %d rows", cols, len(rows))
	}
}

// TestReplayColumnTypes holds that the column types follow the schema: the
// database type name, the scan type, and whether the column can be NULL.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM `dbimp-project`.dbimp_test.dbimp_it_types ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		i        int
		name     string
		scan     reflect.Type
		nullable bool
	}{
		{0, "INTEGER", typeOfInt64, true},
		{3, "NUMERIC", typeOfDecimal, true},
		{11, "TIMESTAMP", typeOfInstant, true},
		{12, "JSON", typeOfAny, true},
		{14, "INTERVAL", typeOfInterval, true},
		{15, "ARRAY", typeOfList, false},
		{16, "RECORD", typeOfMap, true},
	} {
		ct := types[tt.i]
		if ct.DatabaseTypeName() != tt.name || ct.ScanType() != tt.scan {
			t.Errorf("the column %d is %s and %v, want %s and %v", tt.i, ct.DatabaseTypeName(), ct.ScanType(), tt.name, tt.scan)
		}
		if nullable, ok := ct.Nullable(); !ok || nullable != tt.nullable {
			t.Errorf("the column %d has nullable %v, want %v", tt.i, nullable, tt.nullable)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestReplayErrors holds D8 and D189: an error of the service is an *Error with
// its HTTP status, reason and message, before any row, and an error that comes
// from a result that failed halfway gives no row at all.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, tt := range []struct {
		name   string
		query  string
		status int
		reason string
		state  string
		text   string
	}{
		{"syntax", "SELEC 1", 400, "invalidQuery", "INVALID_ARGUMENT", "Syntax error"},
		{"no table", "SELECT * FROM `dbimp-project`.dbimp_test.dbimp_it_nosuch", 404, "notFound", "NOT_FOUND", "Not found: Table"},
		{"no dataset", "SELECT * FROM `dbimp-project`.nosuchdataset.t", 403, "accessDenied", "PERMISSION_DENIED", "perhaps it does not exist"},
		{"division halfway", "SELECT 1 / (x - 1500) AS v FROM UNNEST(GENERATE_ARRAY(1, 3000)) AS x ORDER BY x", 400, "invalidQuery", "INVALID_ARGUMENT", "division by zero"},
		{"array with a NULL", "SELECT [1, 2, 3] AS a, ARRAY<INT64>[] AS empty, [CAST(NULL AS INT64), 1] AS withnull, CAST(NULL AS ARRAY<INT64>) AS nullarray", 400, "invalidQuery", "INVALID_ARGUMENT", "null element"},
		{"no version", "SELECT @@version", 400, "invalidQuery", "INVALID_ARGUMENT", "Unrecognized name"},
	} {
		_, rows, err := readAllContext(t, t.Context(), db, tt.query)
		if len(rows) != 0 {
			t.Errorf("%s: got rows %v", tt.name, rows)
		}
		e, ok := errAs(err)
		if !ok {
			t.Errorf("%s: the error is %v (%T), want a *bigquery.Error", tt.name, err, err)
			continue
		}
		if e.HTTPStatus != tt.status || e.Reason != tt.reason || e.Status != tt.state || !strings.Contains(e.Message, tt.text) {
			t.Errorf("%s: the error is %+v, want %d %s %s with %q", tt.name, *e, tt.status, tt.reason, tt.state, tt.text)
		}
		if isIncomplete(err) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", tt.name)
		}
		if tt.reason == "invalidQuery" && e.Location != "q" {
			t.Errorf("%s: the location is %q, want q", tt.name, e.Location)
		}
	}
}

// TestReplayCanceledJob holds that the reason stopped is ErrCanceled, for a
// job that someone else canceled (recorded: bigquery-212).
func TestReplayCanceledJob(t *testing.T) {
	t.Parallel()
	err := newError(&dbimp.StatusError{Code: 499, Body: `{"error":{"code":499,"message":"Job execution was cancelled: User requested cancellation","errors":[{"message":"x","domain":"global","reason":"stopped"}],"status":"CANCELLED"}}`})
	if !isCanceled(err) || err.Reason != "stopped" {
		t.Errorf("the error is %+v, want ErrCanceled", *err)
	}
	if isCanceled(newError(&dbimp.StatusError{Code: 400, Body: `{"error":{"message":"m","errors":[{"reason":"invalidQuery"}]}}`})) {
		t.Error("an error of a query is ErrCanceled")
	}
}

// TestReplayUnauthenticated holds D189: a request with no token or a wrong token
// is HTTP 401 with the reason required or authError, and the message names the
// credential and holds no token (recorded: bigquery-219 and bigquery-420).
func TestReplayUnauthenticated(t *testing.T) {
	t.Parallel()
	db, _ := replayWith(t, config(), &replayServer{t: t, set: hosted, only: http.StatusUnauthorized})
	_, _, err := readAllContext(t, t.Context(), db, "SELECT 1 AS a")
	e, ok := errAs(err)
	if !ok || e.HTTPStatus != http.StatusUnauthorized || e.Status != "UNAUTHENTICATED" {
		t.Fatalf("the error is %v, want HTTP 401 UNAUTHENTICATED", err)
	}
	if e.Reason != "required" && e.Reason != "authError" {
		t.Errorf("the reason is %q, want required or authError", e.Reason)
	}
}

// anyValue marks a test that checks that the statement runs, and not its value.
var anyValue = struct{ any bool }{true}

// TestReplayParameters holds D189 item 8 with the recorded requests: each Go
// type goes as the parameter type that the recording shows, named or
// positional, and the server binds it.
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	date := dbimp.Date{Year: 2024, Month: time.January, Day: 2}
	for _, tt := range []struct {
		name  string
		query string
		args  []any
		want  any
	}{
		{"named int64", "SELECT @p AS a", []any{sql.Named("p", int64(5))}, int64(5)},
		{"named float64", "SELECT @p AS a", []any{sql.Named("p", 1.5)}, 1.5},
		{"named decimal", "SELECT @p AS a", []any{sql.Named("p", dec(t, "1.5"))}, dec(t, "1.5")},
		{"named bool", "SELECT @p AS a", []any{sql.Named("p", true)}, true},
		{"named string", "SELECT @p AS a", []any{sql.Named("p", "x")}, "x"},
		{"named bytes", "SELECT @p AS a", []any{sql.Named("p", []byte("abc"))}, []byte("abc")},
		{"named date", "SELECT @p AS a", []any{sql.Named("p", date)}, date},
		{"named time", "SELECT @p AS a", []any{sql.Named("p", dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000})}, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}},
		{"named datetime", "SELECT @p AS a", []any{sql.Named("p", dbimp.LocalDateTime{Date: date, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5, Nanosecond: 123456000}})}, dbimp.LocalDateTime{Date: date, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5, Nanosecond: 123456000}}},
		{"named timestamp", "SELECT @p AS a", []any{sql.Named("p", at(123456))}, at(123456)},
		{"named interval", "SELECT @p AS a", []any{sql.Named("p", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60 + 6) * int64(time.Second)})}, dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60 + 6) * int64(time.Second)}},
		{"named NaN", "SELECT @p AS a", []any{sql.Named("p", math.NaN())}, math.NaN()},
		{"named infinity", "SELECT @p AS a", []any{sql.Named("p", math.Inf(1))}, math.Inf(1)},
		{"named largest", "SELECT @p AS a", []any{sql.Named("p", int64(math.MaxInt64))}, int64(math.MaxInt64)},
		{"positional int64", "SELECT ? AS a", []any{int64(5)}, int64(5)},
		{"positional float64", "SELECT ? AS a", []any{1.5}, 1.5},
		{"positional two", "SELECT ? AS a, ? AS b", []any{int64(5), "x"}, int64(5)},
		{"named null", "SELECT @p AS a", []any{sql.Named("p", nil)}, nil},
		{"positional null", "SELECT ? AS a", []any{nil}, nil},
		{"in a where clause", "SELECT id FROM `dbimp-project`.dbimp_test.dbimp_it_dml WHERE s = ?", []any{"k"}, anyValue},
		{"in a limit", "SELECT id FROM `dbimp-project`.dbimp_test.dbimp_it_dml ORDER BY id LIMIT @n", []any{sql.Named("n", int64(1))}, anyValue},
	} {
		_, rows := readAll(t, db, tt.query, tt.args...)
		if tt.want == anyValue {
			continue
		}
		if len(rows) != 1 || !same(rows[0][0], tt.want) {
			t.Errorf("%s: got %#v, want one row with %#v (%T)", tt.name, rows, tt.want, tt.want)
		}
	}
}

// TestReplayParameterErrors holds that the service tests the entries, and the
// driver sends them as the recordings show: a parameter that the statement
// does not name is HTTP 400 with invalidQuery.
func TestReplayParameterErrors(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	_, _, err := readAllContext(t, t.Context(), db, "SELECT @a AS a", sql.Named("b", int64(5)))
	e, ok := errAs(err)
	if !ok || e.HTTPStatus != 400 || e.Reason != "invalidQuery" || !strings.Contains(e.Message, "not found") {
		t.Errorf("the error is %v, want invalidQuery with a parameter that is not found", err)
	}
	_, _, err = readAllContext(t, t.Context(), db, "SELECT ?, ? AS a", int64(5))
	if e, ok := errAs(err); !ok || e.HTTPStatus != 400 || !strings.Contains(e.Message, "not defined") {
		t.Errorf("the error is %v, want a refusal of too few parameters", err)
	}
}

// TestReplayRowsAffected holds D178 and D189: RowsAffected is numDmlAffectedRows
// of the answer of a statement that changes rows, and an error that wraps
// dbimp.ErrNotSupported for a statement whose answer has no count.
func TestReplayRowsAffected(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	for _, tt := range []struct {
		query string
		want  int64
	}{
		{"INSERT INTO `dbimp-project`.dbimp_test.dbimp_it_dml (id, s) VALUES (1, 'a'), (2, 'b'), (3, 'c')", 3},
		{"UPDATE `dbimp-project`.dbimp_test.dbimp_it_dml SET s = 'z' WHERE id > 1", 2},
		{"DELETE FROM `dbimp-project`.dbimp_test.dbimp_it_dml WHERE id = 1", 1},
		{"TRUNCATE TABLE `dbimp-project`.dbimp_test.dbimp_it_dml", 3},
	} {
		res, err := db.ExecContext(t.Context(), tt.query)
		if err != nil {
			t.Fatalf("%s: %v", tt.query, err)
		}
		n, err := res.RowsAffected()
		if err != nil || n != tt.want {
			t.Errorf("%s: RowsAffected is %d, %v, want %d", tt.query, n, err, tt.want)
		}
		if _, err := res.LastInsertId(); !isNotSupported(err) {
			t.Errorf("%s: LastInsertId gave %v, want dbimp.ErrNotSupported", tt.query, err)
		}
	}
	// A select has no count.
	res, err := db.ExecContext(t.Context(), "SELECT 1 AS a, 'x' AS b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !isNotSupported(err) {
		t.Errorf("RowsAffected of a select gave %v, want dbimp.ErrNotSupported", err)
	}
	// An Exec reads the head only, and sends one request.
	if got := s.requests(); len(got) != 5 {
		t.Errorf("the driver sent %d requests, want 5: %v", len(got), got)
	}
}

// TestReplayRowsAffectedOnTheEmulator holds that the emulator sends no count,
// so RowsAffected is an error and not a zero (D178).
func TestReplayRowsAffectedOnTheEmulator(t *testing.T) {
	t.Parallel()
	db, _ := replayEmulator(t)
	res, err := db.ExecContext(t.Context(), "INSERT INTO dbmeta.dbmeta.dbimp_it_dml (id, s) VALUES (1, 'a'), (2, 'b'), (3, 'c')")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); !isNotSupported(err) {
		t.Errorf("RowsAffected is %d, %v, want an error that wraps dbimp.ErrNotSupported", n, err)
	}
}

// TestReplayScripts holds D189 item 6: a request with several statements gives
// the rows of the last one, and a script that ends with a statement that has no
// rows gives no column. A transaction in one request is sent as it is.
func TestReplayScripts(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	cols, rows := readAll(t, db, "SELECT 1 AS a; SELECT 2 AS b")
	if !slices.Equal(cols, []string{"b"}) {
		t.Errorf("the columns are %v, want b", cols)
	}
	check(t, "two selects", rows, [][]any{{int64(2)}})
	cols, rows = readAll(t, db, "CREATE TABLE `dbimp-project`.dbimp_test.dbimp_it_scr (a INT64); INSERT INTO `dbimp-project`.dbimp_test.dbimp_it_scr VALUES (1); SELECT a FROM `dbimp-project`.dbimp_test.dbimp_it_scr; DROP TABLE `dbimp-project`.dbimp_test.dbimp_it_scr")
	if len(cols) != 0 || len(rows) != 0 {
		t.Errorf("a script that ends with DROP gave the columns %v and the rows %v", cols, rows)
	}
	if _, err := db.ExecContext(t.Context(), "BEGIN TRANSACTION; INSERT INTO `dbimp-project`.dbimp_test.dbimp_it_tx (id) VALUES (1); COMMIT TRANSACTION"); err != nil {
		t.Errorf("a transaction in one request: %v", err)
	}
}

// TestReplayTransactions holds D20 and D189 item 3: BeginTx returns the error of
// D20, and nothing is sent.
func TestReplayTransactions(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	if tx, err := db.BeginTx(t.Context(), nil); err == nil {
		_ = tx.Rollback()
		t.Error("BeginTx returned a transaction")
	} else if !isNotSupported(err) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
	}
	if got := s.requests(); len(got) != 0 {
		t.Errorf("BeginTx sent %v, want nothing", got)
	}
}

// TestReplayNoVersion holds that the driver sends SELECT version() to the
// service as any other statement, because BigQuery has no release that the
// driver can read, so D181 gives it no answer of its own.
func TestReplayNoVersion(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	if _, _, err := readAllContext(t, t.Context(), db, "SELECT @@version"); err == nil {
		t.Error("SELECT @@version gave no error")
	}
	if got := s.requests(); len(got) != 1 {
		t.Errorf("the driver sent %v, want the statement", got)
	}
}

// TestReplayDryRun holds D189 with the recording of a dry run: the answer has the
// schema and no job id and no rows. WithParameter sets the member of the
// request.
func TestReplayDryRun(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	cols, rows := readAll(t, db, "SELECT 1 AS a", WithParameter("dryRun", true))
	if !slices.Equal(cols, []string{"a"}) || len(rows) != 0 {
		t.Errorf("a dry run gave the columns %v and the rows %v, want a and none", cols, rows)
	}
	if got := s.lastBody(t)["dryRun"]; got != true {
		t.Errorf("the body holds dryRun %v, want true", got)
	}
}

// TestReplayErrorsOnTheEmulator holds that the emulator answers every error of a
// query with HTTP 400 and the reason jobInternalError, which the driver reports
// as it is, and sends the default dataset of the DSN in the request.
func TestReplayErrorsOnTheEmulator(t *testing.T) {
	t.Parallel()
	cfg := config()
	cfg.Project, cfg.Dataset = emulatorProject, "dbmeta"
	db, s := replayWith(t, cfg, &replayServer{t: t, set: emulator})
	_, _, err := readAllContext(t, t.Context(), db, "SELECT 1 AS a FROM dbimp_it_nosuch")
	e, ok := errAs(err)
	if !ok || e.HTTPStatus != http.StatusBadRequest || e.Reason != "jobInternalError" {
		t.Fatalf("the error is %v, want HTTP 400 and jobInternalError", err)
	}
	if ds, _ := s.lastBody(t)["defaultDataset"].(map[string]any); ds["projectId"] != "dbmeta" || ds["datasetId"] != "dbmeta" {
		t.Errorf("defaultDataset is %v, want the project and the dataset of the DSN", ds)
	}
}

// TestReplayDeadline holds that a context with a deadline reaches the request
// and ends it with the error of the context, never driver.ErrBadConn.
func TestReplayDeadline(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server sees that the client left only after it read the body.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, _, err := readAllContext(t, ctx, db, "SELECT 1")
	if err == nil || !isDeadline(err) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
}

// exchange returns the recorded exchange of the hosted request with the number n,
// such as 217 for bigquery-217.
func exchange(tb testing.TB, n int) *dbimptest.Exchange {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, fmt.Sprintf("bigquery-%03d-*.json", n)))
	if err != nil || len(paths) != 1 {
		tb.Fatalf("finding the recorded exchange %d: %v, %v", n, paths, err)
	}
	ex, err := dbimptest.ReadExchange(paths[0])
	if err != nil {
		tb.Fatal(err)
	}
	return ex
}

// serve writes the response of ex.
func serve(w http.ResponseWriter, ex *dbimptest.Exchange) {
	for key, vals := range ex.Response.Header {
		for _, val := range vals {
			w.Header().Add(key, val)
		}
	}
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	w.WriteHeader(ex.Response.Status)
	_, _ = w.Write(ex.Response.Content())
}
