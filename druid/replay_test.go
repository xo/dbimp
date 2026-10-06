package druid_test

import (
	"bytes"
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
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/druid"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/druid/. Each one decodes a real answer through the driver.
//
// The driver asks for arrayLines, and most of the recordings asked for
// array, which holds the same lines in one JSON array (measured). So a
// recording of array also answers a request for arrayLines, and the fake
// server writes each value of its array on a line of its own, and then the
// empty line, as the server writes arrayLines. It leaves the empty line out
// when the array has no end, as the server does after an error (measured).
// The recordings of arrayLines answer as they are.

const testdata = "../testdata/druid"

// releases are the releases that step 6 recorded.
var releases = []string{"druid-36.0.0", "druid-37.0.0"}

// headerKeys are the keys of the body that ask for the three rows of the
// header, which the driver sends with each query.
var headerKeys = []string{"header", "typesHeader", "sqlTypesHeader"}

// normalize returns the canonical form of the body of POST /druid/v2/sql,
// without resultFormat and the keys of the header, and without the keys of
// the context that the recordings left out: the id of the query, which the
// driver chooses each time, and the time zone UTC, which is the default of
// the server. It also returns the result format, and whether the body asked
// for the three rows of the header.
func normalize(b []byte) (string, string, bool) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b), "", false
	}
	format, _ := m["resultFormat"].(string)
	delete(m, "resultFormat")
	header := true
	for _, k := range headerKeys {
		header = header && m[k] == true
		delete(m, k)
	}
	if ctx, ok := m["context"].(map[string]any); ok {
		delete(ctx, "sqlQueryId")
		if ctx["sqlTimeZone"] == "UTC" {
			delete(ctx, "sqlTimeZone")
		}
		if len(ctx) == 0 {
			delete(m, "context")
		}
	}
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b), format, header
	}
	v := jsontext.Value(out)
	if err := v.Canonicalize(); err != nil {
		return string(out), format, header
	}
	return string(v), format, header
}

// fakeServer answers each request with a recorded exchange of one release.
type fakeServer struct {
	t         *testing.T
	exchanges []*dbimptest.Exchange
	keep      func(*dbimptest.Exchange) bool
}

// newReplay starts a fake server with the exchanges of release for which
// keep is true, or every one for a nil keep.
func newReplay(t *testing.T, release string, keep func(*dbimptest.Exchange) bool) *httptest.Server {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, release+"-*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("finding the exchanges of %s: %v", release, err)
	}
	s := &fakeServer{t: t, keep: keep}
	for _, path := range paths {
		ex, err := dbimptest.ReadExchange(path)
		if err != nil {
			t.Fatal(err)
		}
		s.exchanges = append(s.exchanges, ex)
	}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv
}

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading a request to the fake server: %v", err)
		return
	}
	ex, lines := s.find(r, body)
	if ex == nil {
		s.t.Errorf("no exchange matches %s %s %s", r.Method, r.URL, body)
		http.Error(w, "no exchange matches", http.StatusTeapot)
		return
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
	content := res.Content()
	if lines && res.Status == http.StatusOK {
		content = toLines(s.t, content)
	}
	if _, err := w.Write(content); err != nil {
		s.t.Errorf("writing a response from the fake server: %v", err)
	}
}

// find returns the first exchange that answers the request, preferring a
// recording of arrayLines to one of array, and whether the answer of array
// must be written as lines. A recording that did not ask for the header
// answers only when the server refused it, because a refusal does not hang
// on the header.
func (s *fakeServer) find(r *http.Request, body []byte) (*dbimptest.Exchange, bool) {
	want, _, _ := normalize(body)
	var asArray *dbimptest.Exchange
	for _, ex := range s.exchanges {
		if ex.Request.Method != r.Method || ex.Request.Path != r.URL.Path || (s.keep != nil && !s.keep(ex)) {
			continue
		}
		got, format, header := normalize(ex.Request.Content())
		switch {
		case got != want:
		case !header && ex.Response.Status < http.StatusBadRequest:
		case format == "arrayLines":
			return ex, false
		case format == "array" && asArray == nil:
			asArray = ex
		}
	}
	return asArray, asArray != nil
}

// toLines writes the answer b of array as arrayLines: each value of the
// array on a line, and then an empty line if the array has its end.
func toLines(t *testing.T, b []byte) []byte {
	t.Helper()
	dec := jsontext.NewDecoder(bytes.NewReader(b))
	if _, err := dec.ReadToken(); err != nil {
		t.Fatalf("reading a recorded array: %v", err)
	}
	var out bytes.Buffer
	for dec.PeekKind() != ']' {
		v, err := dec.ReadValue()
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return out.Bytes()
		}
		if err != nil {
			t.Fatalf("reading a recorded array: %v", err)
		}
		out.Write(v)
		out.WriteByte('\n')
	}
	if _, err := dec.ReadToken(); err != nil {
		// The array ends with no ], as it does after an error (measured).
		return out.Bytes()
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// replayOnly opens the driver against the recorded exchanges of release for
// which keep is true, or every one for a nil keep. The first exchange that
// matches answers, and the administrator comes first in each recording.
func replayOnly(t *testing.T, release, query string, keep func(*dbimptest.Exchange) bool) *sql.DB {
	t.Helper()
	srv := newReplay(t, release, keep)
	db, err := sql.Open(druid.Name, strings.Replace(srv.URL, "http://", "druid://admin:secret@", 1)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// replay opens the driver against every recorded exchange of release.
func replay(t *testing.T, release string) *sql.DB {
	t.Helper()
	return replayOnly(t, release, "", nil)
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

// serverError returns err as a *druid.Error, and fails the test if it is
// not one.
func serverError(t *testing.T, err error) *druid.Error {
	t.Helper()
	e, ok := errors.AsType[*druid.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *druid.Error", err, err)
	}
	return e
}

// utc returns the time of the text s in ISO 8601, in UTC.
func utc(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v.UTC()
}

// everyType is the statement of step 6 that reads every column of the
// datasource of every type.
const everyType = "SELECT * FROM dbimp_types ORDER BY __time"

// TestReplayTypes holds D164 for each type, as the datasource of every type
// stored it, with arrays as text and as arrays: a NULL is nil, a multi-value
// string with two values is a []any, and a boolean stored in a datasource is
// a BIGINT (measured).
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		want := [][]any{
			{utc(t, "1970-01-01T00:00:00Z"), int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
			{utc(t, "2026-10-01T00:00:00Z"), int64(2), "", int64(math.MinInt64), -1.5, -0.1, []any{}, nil, nil, nil, "z", int64(0)},
			{utc(t, "2026-10-01T12:34:56.789Z"), int64(1), "é'\"\\ x", int64(math.MaxInt64), 3.4e38, math.MaxFloat64,
				map[string]any{"k": []any{int64(1), "two", nil, map[string]any{"n": 1.5}}},
				[]any{"a", nil, "c"}, []any{int64(math.MinInt64), int64(math.MaxInt64)}, []any{0.1, 1.5e300}, []any{"x", "y"}, int64(1)},
		}
		for _, args := range [][]any{nil, {druid.WithParameter("context", map[string]any{"sqlStringifyArrays": false})}} {
			cols, got, err := readAll(t, db, everyType, args...)
			if err != nil {
				t.Fatalf("%s: %v", release, err)
			}
			if wantCols := strings.Fields("__time id s l f d j sa la da mv b"); !slices.Equal(cols, wantCols) {
				t.Errorf("%s: the columns are %q, want %q", release, cols, wantCols)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the rows are\n%#v\nwant\n%#v", release, got, want)
			}
		}
	}
}

// columnTypes returns the types of the columns of query.
func columnTypes(t *testing.T, db *sql.DB, query string) []*sql.ColumnType {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
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

// castsAndLiterals are the statements of step 6 that read the types that a
// datasource does not store: a cast of each, and a literal of each.
const (
	casts    = "SELECT CAST(id AS INTEGER) AS i, CAST(d AS DECIMAL(38, 10)) AS dc, CAST(__time AS DATE) AS dt, CAST(f AS REAL) AS r, CAST(l AS VARCHAR) AS lv, id = 1 AS bo, CAST(NULL AS VARCHAR) AS nv FROM dbimp_types ORDER BY __time"
	literals = `SELECT 'x' AS c, NULL AS n, TRUE AS b, 1.5 AS dc, DATE '2026-10-01' AS dt, TIMESTAMP '2026-10-01 12:34:56.789' AS ts, ARRAY['a', NULL] AS a, PARSE_JSON('{"k":1}') AS j FROM dbimp_types WHERE id = 1`
)

// TestReplayColumnTypes holds that the type of each column is the SQL type
// of the header, with the scan type of D164.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query        string
			names, scans string
		}{
			{everyType, "TIMESTAMP BIGINT VARCHAR BIGINT FLOAT DOUBLE OTHER ARRAY ARRAY ARRAY VARCHAR BIGINT",
				"time.Time int64 string int64 float64 float64 interface_{} []interface_{} []interface_{} []interface_{} string int64"},
			{casts, "INTEGER DECIMAL DATE REAL VARCHAR BOOLEAN VARCHAR", "int64 float64 dbimp.Date float64 string bool string"},
			{literals, "CHAR NULL BOOLEAN DECIMAL DATE TIMESTAMP ARRAY OTHER",
				"string interface_{} bool float64 dbimp.Date time.Time []interface_{} interface_{}"},
			{"SELECT DS_HLL(s) AS h FROM dbimp_types", "OTHER", "[]uint8"},
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
				t.Errorf("%s: %q: the types are %q, want %q", release, tt.query, got, tt.names)
			}
			if got := strings.Join(scans, " "); got != tt.scans {
				t.Errorf("%s: %q: the scan types are %q, want %q", release, tt.query, got, tt.scans)
			}
		}
	}
}

// TestReplayValues holds the Go value of each type that a datasource does
// not store, from a cast and from a literal, and the values that a float
// and a timestamp hold at their limits.
func TestReplayValues(t *testing.T) {
	t.Parallel()
	date := dbimp.Date{Year: 2026, Month: 10, Day: 1}
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			want  [][]any
		}{
			{casts, [][]any{
				{int64(3), nil, dbimp.Date{Year: 1970, Month: 1, Day: 1}, nil, nil, false, nil},
				{int64(2), -0.1, date, -1.5, "-9223372036854775808", false, nil},
				{int64(1), math.MaxFloat64, date, 3.3999999521443642e38, "9223372036854775807", true, nil},
			}},
			{literals, [][]any{{"x", nil, true, 1.5, date, utc(t, "2026-10-01T12:34:56.789Z"), []any{"a", nil}, map[string]any{"k": int64(1)}}}},
			{"SELECT id = 1 AS bo, id > 5 AS bf, s IS NULL AS bn FROM dbimp_types ORDER BY __time", [][]any{
				{false, false, true}, {false, false, false}, {true, false, false},
			}},
			{"SELECT f, CAST(f AS DOUBLE) AS fd FROM dbimp_types ORDER BY __time", [][]any{
				{nil, nil}, {-1.5, -1.5}, {3.4e38, 3.3999999521443642e38},
			}},
			{"SELECT TIMESTAMP '1900-01-01 00:00:00' AS ts, TIME_PARSE('0001-01-01T00:00:00Z') AS early FROM dbimp_types WHERE id = 1", [][]any{
				{utc(t, "1900-01-01T00:00:00Z"), utc(t, "0001-01-01T00:00:00Z")},
			}},
			{"SELECT DS_HLL(s) AS h FROM dbimp_types", [][]any{{[]byte{2, 1, 7, 12, 3, 8, 1, 0, 246, 126, 189, 5}}}},
		} {
			_, got, err := readAll(t, db, tt.query)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %q gave\n%#v, %v\nwant\n%#v", release, tt.query, got, err, tt.want)
			}
		}
		_, got, err := readAll(t, db, "SELECT d * 10 AS inf, -d * 10 AS ninf, (d * 10) - (d * 10) AS nan FROM dbimp_types WHERE id = 1")
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: the infinities gave %v, %v", release, got, err)
		}
		inf, _ := got[0][0].(float64)
		ninf, _ := got[0][1].(float64)
		nan, ok := got[0][2].(float64)
		if !math.IsInf(inf, 1) || !math.IsInf(ninf, -1) || !ok || !math.IsNaN(nan) {
			t.Errorf("%s: the infinities and NaN are %v", release, got[0])
		}
	}
}

// TestReplayMultiValue holds D164: a multi-value string with two values is a
// []any of its strings, one value is a string, and GROUP BY gives one row for
// each value (measured).
func TestReplayMultiValue(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			want  [][]any
		}{
			{"SELECT id, mv, MV_LENGTH(mv) AS n FROM dbimp_types WHERE MV_CONTAINS(mv, 'x') OR mv = 'z'", [][]any{
				{int64(2), "z", int64(1)}, {int64(1), []any{"x", "y"}, int64(2)},
			}},
			{"SELECT mv, COUNT(*) AS c FROM dbimp_types GROUP BY mv ORDER BY mv", [][]any{
				{nil, int64(1)}, {"x", int64(1)}, {"y", int64(1)}, {"z", int64(1)},
			}},
		} {
			_, got, err := readAll(t, db, tt.query)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %q gave %#v, %v, want %#v", release, tt.query, got, err, tt.want)
			}
		}
	}
	// MV_TO_ARRAY gives an ARRAY, which step 7 recorded on 36.0.0 only.
	_, got, err := readAll(t, replay(t, "druid-36.0.0"), "SELECT id, MV_TO_ARRAY(mv) AS a FROM dbimp_types WHERE id = 1",
		druid.WithParameter("context", map[string]any{"sqlStringifyArrays": false}))
	if want := [][]any{{int64(1), []any{"x", "y"}}}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("MV_TO_ARRAY gave %#v, %v, want %#v", got, err, want)
	}
}

// TestReplayParameters holds D164: the server binds each argument as the
// typed parameter that the recording sent, and a named argument is refused.
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		ts := dbimp.LocalDateTimeOf(time.Date(2026, 10, 1, 12, 34, 56, 789e6, time.UTC))
		_, got, err := readAll(t, db, "SELECT ? AS s, ? AS d, ? AS ts, ? AS b, ? AS big, ? AS nul, id FROM dbimp_types WHERE id = ?",
			"é'x", 1.5, ts, true, int64(math.MaxInt64), nil, int64(1))
		want := [][]any{{"é'x", 1.5, utc(t, "2026-10-01T12:34:56.789Z"), true, int64(math.MaxInt64), nil, int64(1)}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the typed parameters gave %#v, %v, want %#v", release, got, err, want)
		}
		big, _, _ := apd.NewFromString("1.2345678901234568e18")
		for _, tt := range []struct {
			query string
			arg   any
			want  [][]any
		}{
			{"SELECT ? AS arr FROM dbimp_types WHERE id = 1", []string{"a", "b"}, [][]any{{[]any{"a", "b"}}}},
			{"SELECT id FROM dbimp_types WHERE __time = ?", time.UnixMilli(1790858096789).In(time.FixedZone("x", 3600)), [][]any{{int64(1)}}},
			{"SELECT id FROM dbimp_types WHERE __time >= ?", dbimp.Date{Year: 2026, Month: 10, Day: 1}, [][]any{{int64(2)}, {int64(1)}}},
			// The server binds a DECIMAL as the type of its JSON number
			// (measured).
			{"SELECT ? AS d FROM dbimp_types WHERE id = 1", big, [][]any{{int64(1234567890123456768)}}},
			{"SELECT '?' AS q, ? AS p FROM dbimp_types WHERE id = 1", "x", [][]any{{"?", "x"}}},
		} {
			args := []any{tt.arg}
			_, got, err := readAll(t, db, tt.query, args...)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %q with %v gave %#v, %v, want %#v", release, tt.query, args, got, err, tt.want)
			}
		}
		// Too many arguments: the server ignores the second (measured).
		if _, got, err := readAll(t, db, "SELECT ? AS a FROM dbimp_types WHERE id = 1", int64(1), int64(2)); err != nil || !reflect.DeepEqual(got, [][]any{{int64(1)}}) {
			t.Errorf("%s: too many arguments gave %v, %v", release, got, err)
		}
		_, _, err = readAll(t, db, "SELECT ? AS a, ? AS b FROM dbimp_types WHERE id = 1", int64(1))
		if e := serverError(t, err); e.HTTPStatus != http.StatusBadRequest || !strings.Contains(e.Message, "No value bound for parameter") {
			t.Errorf("%s: too few arguments gave %v", release, err)
		}
		if _, _, err := readAll(t, db, "SELECT id FROM dbimp_types WHERE id = ?", sql.Named("id", 1)); !errors.Is(err, dbimp.ErrArguments) {
			t.Errorf("%s: a named argument gave %v, want dbimp.ErrArguments", release, err)
		}
		_, _, err = readAll(t, db, "SELECT id FROM dbimp_types WHERE id = :id", int64(1))
		if e := serverError(t, err); e.Category != "INVALID_INPUT" {
			t.Errorf("%s: a named parameter in the text gave %v, want a syntax error", release, err)
		}
	}
}

// TestReplayResults holds that a result keeps the order of its columns and
// its rows, that a result with no rows has its columns, and that a result of
// 400 rows comes whole.
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
			{"SELECT n FROM dbimp_big ORDER BY __time", nil, "n", 400, int64(1)},
			{"SELECT n, s FROM dbimp_big ORDER BY __time", nil, "n s", 400, int64(1)},
			{"SELECT n FROM dbimp_big ORDER BY __time", []any{druid.WithParameter("context", map[string]any{"sqlOuterLimit": 5})}, "n", 5, int64(1)},
			{"SELECT n FROM dbimp_big ORDER BY __time LIMIT 3 OFFSET 397", nil, "n", 3, int64(398)},
			{"SELECT id, s FROM dbimp_types WHERE id = 99", nil, "id s", 0, nil},
			{"SELECT * FROM dbimp_types LIMIT 0", nil, "__time id s l f d j sa la da mv b", 0, nil},
			{"SELECT id AS a, l AS a FROM dbimp_types WHERE id = 1", nil, "a a", 1, int64(1)},
			{"SELECT id, s FROM dbimp_types ORDER BY __time", nil, "id s", 3, int64(3)},
		} {
			cols, got, err := readAll(t, db, tt.query, tt.args...)
			switch {
			case err != nil:
				t.Errorf("%s: %q: %v", release, tt.query, err)
			case strings.Join(cols, " ") != tt.cols || len(got) != tt.rows:
				t.Errorf("%s: %q gave the columns %q and %d rows, want %q and %d", release, tt.query, cols, len(got), tt.cols, tt.rows)
			case tt.rows > 0 && got[0][0] != tt.first:
				t.Errorf("%s: %q gave the first value %v, want %v", release, tt.query, got[0][0], tt.first)
			}
		}
	}
}

// TestReplayErrors holds that an error before any rows is the error of the
// server, with its status and its category, and that it does not wrap
// dbimp.ErrIncomplete (D107).
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query    string
			status   int
			category string
		}{
			{"SELEC 1", http.StatusBadRequest, "INVALID_INPUT"},
			{"SELECT * FROM dbimp_none", http.StatusBadRequest, "INVALID_INPUT"},
			{"SELECT n, 100 / (z - z) AS q FROM dbimp_big", http.StatusInternalServerError, "RUNTIME_FAILURE"},
			{"SELECT 12345678901234567890.123 AS big FROM dbimp_types WHERE id = 1", http.StatusBadRequest, "INVALID_INPUT"},
			{"SELECT 1 AS a FROM dbimp_types LIMIT 1; SELECT 2 AS b FROM dbimp_types LIMIT 1", http.StatusBadRequest, "INVALID_INPUT"},
			{"SELECT CAST(id AS TINYINT) AS ti FROM dbimp_types", http.StatusBadRequest, "INVALID_INPUT"},
			{"SELECT CAST(id AS SMALLINT) AS si FROM dbimp_types", http.StatusBadRequest, "INVALID_INPUT"},
			{"SELECT CAST('12:34:56' AS TIME) AS t FROM dbimp_types WHERE id = 1", http.StatusInternalServerError, "RUNTIME_FAILURE"},
			{"SELECT CAST(s AS VARBINARY) AS vb FROM dbimp_types WHERE id = 1", http.StatusNotImplemented, "UNSUPPORTED"},
			{"BEGIN", http.StatusBadRequest, "INVALID_INPUT"},
			{"COMMIT", http.StatusBadRequest, "INVALID_INPUT"},
		} {
			_, _, err := readAll(t, db, tt.query)
			e := serverError(t, err)
			if e.HTTPStatus != tt.status || e.Category != tt.category || errors.Is(err, dbimp.ErrIncomplete) || e.Message == "" {
				t.Errorf("%s: %q gave %v, HTTP %d, want %s with HTTP %d, before any row", release, tt.query, err, e.HTTPStatus, tt.category, tt.status)
			}
		}
	}
}

// TestReplayErrorAfterRows holds D164: an answer that ends with no empty
// line is cut short, after exactly the rows that arrived, and its error
// wraps dbimp.ErrIncomplete and druid.ErrCut.
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, query := range []string{
			// The recording of arrayLines.
			"SELECT n, 100 / z AS q FROM dbimp_big ORDER BY __time",
			// The recording of array, whose array has no end.
			"SELECT n, s, 100 / z AS q FROM dbimp_big ORDER BY __time",
		} {
			_, got, err := readAll(t, db, query)
			if len(got) != 397 || !errors.Is(err, dbimp.ErrIncomplete) || !errors.Is(err, druid.ErrCut) {
				t.Errorf("%s: %q gave %d rows and %v, want 397 rows and then dbimp.ErrIncomplete and druid.ErrCut", release, query, len(got), err)
			}
		}
	}
}

// TestReplayWrites holds D163: the SQL API refuses every write and every
// statement of a schema, and the driver returns the refusal.
func TestReplayWrites(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, query := range []string{
			"INSERT INTO dbimp_crud SELECT TIME_PARSE('2026-10-03T00:00:00Z') AS __time, 3 AS id, 'three' AS v PARTITIONED BY DAY",
			"UPDATE dbimp_crud SET v = 'uno' WHERE id = 1",
			"DELETE FROM dbimp_crud WHERE id = 2",
			"CREATE TABLE dbimp_x (id BIGINT PRIMARY KEY, v VARCHAR DEFAULT 'a' UNIQUE)",
			"CREATE INDEX dbimp_i ON dbimp_crud (v)",
			"ALTER TABLE dbimp_crud ADD COLUMN w BIGINT",
			"DROP TABLE dbimp_crud",
		} {
			_, err := db.ExecContext(t.Context(), query)
			if e := serverError(t, err); e.HTTPStatus != http.StatusBadRequest || e.Category != "INVALID_INPUT" {
				t.Errorf("%s: %q gave %v, want the refusal of the server", release, query, err)
			}
		}
	}
}

// TestReplayPrincipals holds that a wrong password is HTTP 401, and that a
// system table that the ordinary user cannot read is HTTP 403, each an error
// before any row with no password in it.
func TestReplayPrincipals(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusUnauthorized
		})
		_, _, err := readAll(t, db, "SELECT 1 AS a FROM dbimp_types LIMIT 1")
		if e := serverError(t, err); e.HTTPStatus != http.StatusUnauthorized || e.Message != "Unauthorized" {
			t.Errorf("%s: a wrong password gave %v", release, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: the error %v holds the password", release, err)
		}
		db = replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusForbidden
		})
		_, _, err = readAll(t, db, "SELECT server, server_type FROM sys.servers ORDER BY server")
		if e := serverError(t, err); e.HTTPStatus != http.StatusForbidden || !strings.Contains(e.Message, "Unauthorized") {
			t.Errorf("%s: the ordinary user gave %v, want HTTP 403", release, err)
		}
	}
}

// TestReplayStatements holds the features of a statement that a query
// shows: comments, a semicolon at the end, SET, the time zone, the id of the
// query, a key of the context that the server does not know, and BeginTx.
func TestReplayStatements(t *testing.T) {
	t.Parallel()
	jakarta := time.FixedZone("", 7*3600)
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			args  []any
			want  [][]any
		}{
			{"-- a comment\nSELECT /* inline */ id FROM dbimp_types WHERE id = 1 -- the end", nil, [][]any{{int64(1)}}},
			{"SELECT id FROM dbimp_types WHERE id = 1;", nil, [][]any{{int64(1)}}},
			{"SET sqlTimeZone = 'Asia/Jakarta'; SELECT __time FROM dbimp_types WHERE id = 1", nil,
				[][]any{{time.Date(2026, 10, 1, 19, 34, 56, 789e6, jakarta)}}},
			{"SELECT __time, CAST(__time AS DATE) AS dt FROM dbimp_types WHERE id = 1", []any{druid.WithTimeZone("Asia/Jakarta")},
				[][]any{{time.Date(2026, 10, 1, 19, 34, 56, 789e6, jakarta), dbimp.Date{Year: 2026, Month: 10, Day: 1}}}},
			{"SELECT id FROM dbimp_types WHERE id = 1", []any{druid.WithParameter("context", map[string]any{"dbimpUnknown": true})}, [][]any{{int64(1)}}},
			{"SELECT 1 AS a FROM dbimp_types LIMIT 1", []any{druid.WithParameter("dbimpUnknown", 1)}, [][]any{{int64(1)}}},
		} {
			_, got, err := readAll(t, db, tt.query, tt.args...)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %q gave %#v, %v, want %#v", release, tt.query, got, err, tt.want)
			}
		}
		if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: BeginTx gave %v, want dbimp.ErrNotSupported (D164)", release, err)
		}
	}
}

// TestReplayFeatures holds the features of step 5a that a query shows, on
// the recorded answers.
func TestReplayFeatures(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			want  [][]any
		}{
			{"SELECT LOOKUP('a', 'dbimp_lookup') AS a, LOOKUP('q', 'dbimp_lookup') AS q", [][]any{{"apple", nil}}},
			{"SELECT k, v FROM lookup.dbimp_lookup", [][]any{{"a", "apple"}, {"b", "banana"}}},
			{"SELECT __time, v FROM dbimp_extern ORDER BY __time", [][]any{{utc(t, "2026-10-01T00:00:00Z"), int64(1)}, {utc(t, "2026-10-01T01:00:00Z"), int64(2)}}},
			{"SELECT JSON_VALUE(j, '$.k[1]') AS v, JSON_QUERY(j, '$.k[3]') AS q FROM dbimp_types WHERE id = 1", [][]any{{"two", map[string]any{"n": 1.5}}}},
			{"SELECT d.id, u.e FROM dbimp_types AS d CROSS JOIN UNNEST(d.sa) AS u(e)", [][]any{{int64(1), "a"}, {int64(1), nil}, {int64(1), "c"}}},
			{"SELECT TIME_FLOOR(__time, 'P1D') AS d, COUNT(*) AS c FROM dbimp_big GROUP BY 1 ORDER BY 1", [][]any{
				{utc(t, "2026-10-01T00:00:00Z"), int64(399)}, {utc(t, "2026-10-02T00:00:00Z"), int64(1)},
			}},
			{"SELECT __time, id, v FROM dbimp_crud ORDER BY __time", [][]any{
				{utc(t, "2026-10-01T00:00:00Z"), int64(1), "one"}, {utc(t, "2026-10-02T00:00:00Z"), int64(2), "two"},
				{utc(t, "2026-10-03T00:00:00Z"), int64(3), "three"},
			}},
		} {
			_, got, err := readAll(t, db, tt.query)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %q gave %#v, %v, want %#v", release, tt.query, got, err, tt.want)
			}
		}
		_, got, err := readAll(t, db, "SELECT APPROX_COUNT_DISTINCT(s) AS a, APPROX_COUNT_DISTINCT_DS_HLL(s) AS h, APPROX_QUANTILE_DS(l, 0.5) AS q FROM dbimp_types")
		if want := [][]any{{int64(1), int64(1), -9.223372036854776e18}}; err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the approximate functions gave %v, %v, want %v", release, got, err, want)
		}
		_, got, err = readAll(t, db, "SELECT datasource, COUNT(*) AS segments FROM sys.segments GROUP BY datasource ORDER BY datasource")
		if err != nil || len(got) < 4 || got[0][0] != "dbimp_big" {
			t.Errorf("%s: sys.segments gave %v, %v", release, got, err)
		}
	}
}
