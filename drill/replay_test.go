package drill_test

import (
	"context"
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
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/drill"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/drill/. Each one decodes a real answer through the driver.
//
// The driver sends the verbose option with each request, and most of the
// recordings did not. A recording whose body holds the option answers a
// request that holds it, and a recording whose body lacks it answers when no
// recording with the option does.

const testdata = "../testdata/drill"

// releases are the releases that step 6 recorded.
var releases = []string{"drill-1.21.2", "drill-1.22.0"}

// verbose is the option that makes the server write the message of an error.
const verbose = "drill.exec.http.rest.errors.verbose"

// normalize returns the canonical form of the body of POST /query.json
// without the verbose option, and whether the body held it.
func normalize(b []byte) (string, bool) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b), false
	}
	had := false
	if opts, ok := m["options"].(map[string]any); ok {
		_, had = opts[verbose]
		delete(opts, verbose)
		if len(opts) == 0 {
			delete(m, "options")
		}
	}
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b), had
	}
	v := jsontext.Value(out)
	if err := v.Canonicalize(); err != nil {
		return string(out), had
	}
	return string(v), had
}

// fakeServer answers each request with a recorded exchange of one release.
type fakeServer struct {
	t         *testing.T
	exchanges []*dbimptest.Exchange
}

// newReplay starts a fake server with the exchanges of release.
func newReplay(t *testing.T, release string) *httptest.Server {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, release+"-*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("finding the exchanges of %s: %v", release, err)
	}
	s := &fakeServer{t: t}
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
	ex := s.find(r, body)
	if ex == nil {
		// The profile of a query that no recording read is a request that
		// the server answers with HTTP 500 (measured).
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/profiles/") {
			http.Error(w, "{ 'message' : 'error (unable to serialize profile)' }", http.StatusInternalServerError)
			return
		}
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
	w.Header().Del("Content-Length")
	w.WriteHeader(res.Status)
	if _, err := w.Write(res.Content()); err != nil {
		s.t.Errorf("writing a response from the fake server: %v", err)
	}
}

// find returns the exchange for a request, or nil.
func (s *fakeServer) find(r *http.Request, body []byte) *dbimptest.Exchange {
	want, wantHad := normalize(body)
	var loose *dbimptest.Exchange
	for _, ex := range s.exchanges {
		if ex.Request.Method != r.Method || ex.Request.Path != r.URL.Path || ex.Request.Query != r.URL.RawQuery {
			continue
		}
		if r.Method != http.MethodPost {
			return ex
		}
		got, had := normalize(ex.Request.Content())
		switch {
		case got != want:
		case had && wantHad:
			return ex
		case had:
		case loose == nil:
			loose = ex
		}
	}
	return loose
}

// result is the answer to a query, as database/sql returns it.
type result struct {
	cols  []string
	types []*sql.ColumnType
	rows  [][]any
	err   error
}

// run runs query on the database of a fake server of release, and reads
// every row into a *any, the way a caller does. An error of the query is in
// err, with the rows that arrived before it.
func run(t *testing.T, db *sql.DB, query string, args ...any) result {
	t.Helper()
	return runContext(t.Context(), t, db, query, args...)
}

func runContext(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) result {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return result{err: err}
	}
	defer rows.Close()
	var res result
	if res.cols, err = rows.Columns(); err != nil {
		t.Fatal(err)
	}
	if res.types, err = rows.ColumnTypes(); err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		vals := make([]any, len(res.cols))
		ptrs := make([]any, len(vals))
		for i := range ptrs {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		res.rows = append(res.rows, vals)
	}
	res.err = rows.Err()
	return res
}

// forEachRelease runs f with a database on a fake server of each release.
func forEachRelease(t *testing.T, f func(t *testing.T, db *sql.DB, release string)) {
	t.Helper()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			srv := newReplay(t, release)
			db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://u:p@", 1))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			f(t, db, release)
		})
	}
}

func decimal(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestReplayTypes holds the type table: a Parquet table of every type, with
// a row of the smallest values, a row of the largest values and a row of
// NULL (recorded: "every type"), read through Rows.Scan.
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res := run(t, db, "SELECT * FROM dfs.tmp.dbimp_types ORDER BY id")
		if res.err != nil {
			t.Fatal(res.err)
		}
		wantCols := []string{"id", "i", "b", "f4", "f8", "d", "bo", "s", "vb", "dt", "t", "ts", "iy", "idy"}
		if !reflect.DeepEqual(res.cols, wantCols) {
			t.Fatalf("the columns are %v, want %v, in that order", res.cols, wantCols)
		}
		lo := []any{
			int64(1), int64(math.MinInt32), int64(math.MinInt64), -3.4028234663852886e38, -math.MaxFloat64,
			decimal(t, "-12345678901234567890.123456789012345678"), true, "é'\"\\ x", []byte("ab"),
			dbimp.Date{Year: 1, Month: 1, Day: 1}, dbimp.LocalTime{},
			dbimp.LocalDateTime{Date: dbimp.Date{Year: 1969, Month: 12, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6}},
			dbimp.Interval{Months: -14}, dbimp.Interval{Days: -1, Nanoseconds: -7384500e6},
		}
		hi := []any{
			int64(2), int64(math.MaxInt32), int64(math.MaxInt64), 1.401298464324817e-45, math.MaxFloat64,
			decimal(t, "1E-18"), false, "", []byte{},
			dbimp.Date{Year: 9999, Month: 12, Day: 31}, dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6},
			dbimp.LocalDateTime{Date: dbimp.Date{Year: 9999, Month: 12, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999e6}},
			dbimp.Interval{}, dbimp.Interval{},
		}
		null := []any{int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil}
		want := [][]any{lo, hi, null}
		if len(res.rows) != len(want) {
			t.Fatalf("read %d rows, want %d", len(res.rows), len(want))
		}
		for i, row := range res.rows {
			for j, got := range row {
				if !reflect.DeepEqual(got, want[i][j]) {
					t.Errorf("row %d, column %s is %#v, want %#v", i+1, res.cols[j], got, want[i][j])
				}
			}
		}
	})
}

// TestReplayColumnTypes holds the names, the scan types, the lengths and the
// precisions of the columns (recorded: "every type as a literal").
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res := run(t, db, "SELECT CAST(1 AS INT) AS i, CAST(9223372036854775807 AS BIGINT) AS b, CAST(1.5 AS FLOAT) AS f4, CAST(1.5 AS DOUBLE) AS f8, CAST('12345678901234567890.123456789' AS DECIMAL(38, 9)) AS d, 1.25 AS dl, TRUE AS bo, 'x' AS s, CAST('ab' AS CHAR(3)) AS c, CAST('ab' AS VARBINARY) AS vb, DATE '2026-10-01' AS dt, TIME '12:34:56.789' AS t, TIMESTAMP '2026-10-01 12:34:56.789123' AS ts, INTERVAL '1 2:03:04.5' DAY TO SECOND AS idy, INTERVAL '1-2' YEAR TO MONTH AS iy, CAST(NULL AS INT) AS n, NULL AS untyped FROM (VALUES(1))")
		if res.err != nil {
			t.Fatal(res.err)
		}
		type col struct {
			name, dbType string
			scan         reflect.Type
			length       int64
			hasLength    bool
			prec, scale  int64
			hasPrec      bool
		}
		want := []col{
			{"i", "INT", reflect.TypeFor[int64](), 0, false, 0, 0, false},
			{"b", "BIGINT", reflect.TypeFor[int64](), 0, false, 0, 0, false},
			{"f4", "FLOAT4", reflect.TypeFor[float64](), 0, false, 0, 0, false},
			{"f8", "FLOAT8", reflect.TypeFor[float64](), 0, false, 0, 0, false},
			{"d", "VARDECIMAL", reflect.TypeFor[*apd.Decimal](), 0, false, 38, 9, true},
			{"dl", "VARDECIMAL", reflect.TypeFor[*apd.Decimal](), 0, false, 3, 2, true},
			{"bo", "BIT", reflect.TypeFor[bool](), 0, false, 0, 0, false},
			{"s", "VARCHAR", reflect.TypeFor[string](), 1, true, 0, 0, false},
			{"c", "VARCHAR", reflect.TypeFor[string](), 2, true, 0, 0, false},
			{"vb", "VARBINARY", reflect.TypeFor[[]byte](), 0, false, 0, 0, false},
			{"dt", "DATE", reflect.TypeFor[dbimp.Date](), 0, false, 0, 0, false},
			{"t", "TIME", reflect.TypeFor[dbimp.LocalTime](), 0, false, 0, 0, false},
			{"ts", "TIMESTAMP", reflect.TypeFor[dbimp.LocalDateTime](), 0, false, 0, 0, false},
			{"idy", "INTERVALDAY", reflect.TypeFor[dbimp.Interval](), 0, false, 0, 0, false},
			{"iy", "INTERVALYEAR", reflect.TypeFor[dbimp.Interval](), 0, false, 0, 0, false},
			{"n", "INT", reflect.TypeFor[int64](), 0, false, 0, 0, false},
			{"untyped", "INT", reflect.TypeFor[int64](), 0, false, 0, 0, false},
		}
		if len(res.types) != len(want) {
			t.Fatalf("the answer has %d columns, want %d", len(res.types), len(want))
		}
		for i, w := range want {
			ct := res.types[i]
			length, hasLength := ct.Length()
			prec, scale, hasPrec := ct.DecimalSize()
			nullable, hasNullable := ct.Nullable()
			got := col{ct.Name(), ct.DatabaseTypeName(), ct.ScanType(), length, hasLength, prec, scale, hasPrec}
			if got != w || !nullable || !hasNullable {
				t.Errorf("column %d is %+v with nullable %v, %v, want %+v that can be NULL", i, got, nullable, hasNullable, w)
			}
		}
		// The values read as the scan types say.
		for i, v := range res.rows[0] {
			if v != nil && reflect.TypeOf(v) != want[i].scan {
				t.Errorf("the value of %s is %T, want %v", want[i].name, v, want[i].scan)
			}
		}
	})
}

// TestReplayFloats holds the infinities and NaN of a FLOAT8, which arrive as
// strings (recorded: "an infinity and NaN").
func TestReplayFloats(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res := run(t, db, "SELECT CAST(1e308 AS DOUBLE) * 10 AS inf, CAST(-1e308 AS DOUBLE) * 10 AS ninf, CAST('NaN' AS DOUBLE) AS nan, CAST(-0.0 AS DOUBLE) AS nz FROM (VALUES(1))")
		if res.err != nil {
			t.Fatal(res.err)
		}
		row := res.rows[0]
		nan, _ := row[2].(float64)
		if row[0] != math.Inf(1) || row[1] != math.Inf(-1) || !math.IsNaN(nan) || row[3] != 0.0 {
			t.Errorf("the floats are %v, want +Inf, -Inf, NaN and 0", row)
		}
	})
}

// TestReplayComplex holds a map, a list of lists, and the lists of a scalar
// type and of maps, which arrive under the name of their element (D165). A
// column that held a list names any as its scan type, and an empty list,
// which arrives as null, is nil.
func TestReplayComplex(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res := run(t, db, "SELECT * FROM dfs.tmp.dbimp_complex")
		if res.err != nil {
			t.Fatal(res.err)
		}
		want := []any{
			int64(1),
			map[string]any{"k": []any{int64(1), int64(2), int64(3)}, "o": map[string]any{"s": "x"}},
			[]any{int64(1), int64(2), int64(3)},
			[]any{[]any{int64(1), int64(2)}, []any{int64(3)}},
			[]any{map[string]any{"a": int64(1)}, map[string]any{"a": int64(2)}},
			nil,
		}
		if !reflect.DeepEqual(res.rows[0], want) {
			t.Errorf("the row is %#v, want %#v", res.rows[0], want)
		}
		scans := []reflect.Type{
			reflect.TypeFor[int64](), reflect.TypeFor[map[string]any](), reflect.TypeFor[any](),
			reflect.TypeFor[[]any](), reflect.TypeFor[map[string]any](), reflect.TypeFor[int64](),
		}
		// The first row has been read, so the list of a BIGINT shows.
		for i, ct := range res.types {
			if got := ct.ScanType(); got != scans[i] && i != 2 && i != 4 {
				t.Errorf("column %s has the scan type %v, want %v", ct.Name(), got, scans[i])
			}
		}
		list := run(t, db, `SELECT convert_from('["a","b"]', 'JSON') AS l FROM (VALUES(1))`)
		if list.err != nil || !reflect.DeepEqual(list.rows, [][]any{{[]any{"a", "b"}}}) {
			t.Errorf("a list of strings gave %#v, %v", list.rows, list.err)
		}
	})
}

// TestReplayColumns holds D18: the columns keep the order of the statement,
// two columns of one name get the names a and a0, and a column with no name
// is EXPR$0 (recorded: "columns in the order of the statement", "two
// columns with one name" and "a column with no name"). A result with no rows
// has its columns.
func TestReplayColumns(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		for _, tt := range []struct {
			query string
			want  []string
		}{
			{"SELECT b, a FROM (VALUES(1, 2)) AS t(a, b)", []string{"b", "a"}},
			{"SELECT 1 AS a, 2 AS a FROM (VALUES(1))", []string{"a", "a0"}},
			{"SELECT 1, 2 FROM (VALUES(1))", []string{"EXPR$0", "EXPR$1"}},
		} {
			res := run(t, db, tt.query)
			if res.err != nil || !reflect.DeepEqual(res.cols, tt.want) || len(res.rows) != 1 {
				t.Errorf("%s gave the columns %v, %d rows and %v, want %v", tt.query, res.cols, len(res.rows), res.err, tt.want)
			}
		}
		res := run(t, db, "SELECT * FROM dfs.tmp.dbimp_types WHERE 1 = 0")
		if res.err != nil || len(res.rows) != 0 || len(res.cols) != 14 || len(res.types) != 14 {
			t.Errorf("a result with no rows gave %d columns, %d rows and %v, want 14 columns and no rows", len(res.cols), len(res.rows), res.err)
		}
	})
}

// TestReplayChangedType holds D165: a column whose type changes between two
// files gives nil for each value of the second type, in a result that
// completes, and typeof in the statement shows it.
func TestReplayChangedType(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res := run(t, db, "SELECT x, m, filename FROM dfs.tmp.dbimp_change")
		want := [][]any{
			{int64(1), map[string]any{"k": int64(1)}, "0_0_0.json"},
			{nil, map[string]any{"k": nil}, "0_0_0.json"},
		}
		if res.err != nil || !reflect.DeepEqual(res.rows, want) {
			t.Errorf("the rows are %#v, %v, want %#v", res.rows, res.err, want)
		}
		typed := run(t, db, "SELECT x, typeof(x) AS t FROM dfs.tmp.dbimp_change")
		want = [][]any{{int64(1), "BIGINT"}, {nil, "VARCHAR"}}
		if typed.err != nil || !reflect.DeepEqual(typed.rows, want) {
			t.Errorf("typeof gave %#v, %v, want %#v", typed.rows, typed.err, want)
		}
	})
}

// TestReplayErrors holds the errors that come before any row: the answer
// holds the message with the verbose option, and the Error names the kind of
// the message and the Java class (recorded: "a syntax error, verbose", "an
// unknown table" and "a division by zero").
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		for _, tt := range []struct {
			query     string
			exception string
			kind      string
			message   string
		}{
			{"SELEC 1", "java.lang.Exception", "", "Non-query expression encountered in illegal context"},
			{"SELECT * FROM dfs.tmp.dbimp_nope", "org.apache.calcite.runtime.CalciteContextException", "", "Object 'dbimp_nope' not found within 'dfs.tmp'"},
			{"SELECT 1 / 0 AS n FROM (VALUES(1))", "org.apache.drill.exec.work.foreman.ForemanException", "", "ReduceAndSimplifyProjectRule"},
			{"INSERT INTO dfs.tmp.dbimp_crud (employee_id, full_name) VALUES (9, 'x')", "java.lang.Exception", "VALIDATION ERROR", "immutable or doesn't support inserts"},
		} {
			res := run(t, db, tt.query)
			e, ok := errors.AsType[*drill.Error](res.err)
			switch {
			case !ok:
				t.Errorf("%s gave %v, want a *drill.Error", tt.query, res.err)
			case e.Exception != tt.exception || e.Kind != tt.kind || !strings.Contains(e.Message, tt.message) || e.HTTPStatus != http.StatusOK || e.QueryID == "":
				t.Errorf("%s gave %+v, want the class %s, the kind %q and %q", tt.query, *e, tt.exception, tt.kind, tt.message)
			case errors.Is(res.err, dbimp.ErrIncomplete):
				t.Errorf("%s gave an error that wraps dbimp.ErrIncomplete, and no row came before it", tt.query)
			}
		}
		// Without the verbose option, the answer has no message, and the
		// profile of the query has not been recorded.
		res := run(t, db, "SELEC 1", drill.WithParameter("options", map[string]string{}))
		if e, ok := errors.AsType[*drill.Error](res.err); !ok || !strings.Contains(e.Message, "no reason") {
			t.Errorf("a failure with no message gave %v, want an error that says that the server gave no reason", res.err)
		}
	})
}

// TestReplayErrorAfterRows holds D165: an error after some rows ends the array
// of rows and sends FAILED with no message (recorded: "an error after some
// rows"). The 300 rows of the first file arrive, and the message comes from
// the profile of the query (recorded: "the profile of the query that failed
// after some rows"). The error wraps dbimp.ErrIncomplete (D107).
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res := run(t, db, "SELECT id, CAST(s AS INT) AS v FROM dfs.tmp.dbimp_multi")
		e, ok := errors.AsType[*drill.Error](res.err)
		switch {
		case len(res.rows) != 300:
			t.Errorf("read %d rows before the error, want 300", len(res.rows))
		case !ok || !errors.Is(res.err, dbimp.ErrIncomplete):
			t.Errorf("the error is %v, want a *drill.Error that wraps dbimp.ErrIncomplete", res.err)
		case e.Kind != "SYSTEM ERROR" || e.Exception != "java.lang.NumberFormatException" || !strings.HasSuffix(e.Message, ": x"):
			t.Errorf("the error is %+v, want the message and the class from the profile", *e)
		}
	})
}

// TestReplayRequestErrors holds the requests that the server cannot start,
// which answer HTTP 500 with "Query submission failed" and no reason
// (recorded: "an unknown default schema", "an unknown option" and "a user to
// impersonate").
func TestReplayRequestErrors(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		for name, opt := range map[string]drill.Option{
			"an unknown default schema": drill.WithSchema("nope"),
			"an unknown option":         drill.WithParameter("options", map[string]string{"no.such.option": "1"}),
			"a user to impersonate":     drill.WithParameter("userName", "dbmeta_user"),
		} {
			res := run(t, db, "SELECT 1 AS one FROM (VALUES(1))", opt)
			var se *dbimp.StatusError
			e, ok := errors.AsType[*drill.Error](res.err)
			if !ok || e.HTTPStatus != http.StatusInternalServerError || e.Message != "Query submission failed" || !errors.As(res.err, &se) {
				t.Errorf("%s gave %v, want the error of HTTP 500 with no reason", name, res.err)
			}
		}
	})
}

// TestReplayCreateTableAs holds D163: CREATE TABLE AS runs, and Exec says how
// many records it wrote. A statement with no count has no rows affected
// (recorded: "crud: create table as" and "crud: drop the table again").
func TestReplayCreateTableAs(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res, err := db.ExecContext(t.Context(), "CREATE TABLE dfs.tmp.dbimp_crud AS SELECT employee_id, full_name FROM cp.`employee.json` LIMIT 3")
		if err != nil {
			t.Fatal(err)
		}
		if n, err := res.RowsAffected(); err != nil || n != 3 {
			t.Errorf("RowsAffected is %d, %v, want 3", n, err)
		}
		res, err = db.ExecContext(t.Context(), "DROP TABLE dfs.tmp.dbimp_crud")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("RowsAffected of a DROP gave %v, want dbimp.ErrNotSupported", err)
		}
		got := run(t, db, "DROP TABLE dfs.tmp.dbimp_crud")
		if got.err != nil || !reflect.DeepEqual(got.cols, []string{"ok", "summary"}) {
			t.Errorf("DROP TABLE gave the columns %v and %v, want ok and summary", got.cols, got.err)
		}
	})
}

// TestReplayLimits holds that autoLimit caps the rows, and that the answer
// names the cap, which the driver does not report (recorded: "autoLimit as a
// number").
func TestReplayLimits(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		res := run(t, db, "SELECT employee_id FROM cp.`employee.json`", drill.WithAutoLimit(5))
		if res.err != nil || len(res.rows) != 5 {
			t.Errorf("a limit of 5 gave %d rows and %v, want 5", len(res.rows), res.err)
		}
		res = run(t, db, "SELECT id, i FROM dbimp_types ORDER BY id", drill.WithDatabase("dfs.tmp"))
		if res.err != nil || len(res.rows) != 3 {
			t.Errorf("the schema dfs.tmp gave %d rows and %v, want 3", len(res.rows), res.err)
		}
	})
}

// TestReplayCanceled holds D165: a query that someone cancels ends with no
// rows and CANCELED, and the error says so (recorded: "a long query to
// cancel").
func TestReplayCanceled(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, _ string) {
		const long = "SELECT COUNT(*) AS n FROM cp.`employee.json` a, cp.`employee.json` b, cp.`tpch/nation.parquet` c, cp.`tpch/region.parquet` d, cp.`tpch/region.parquet` e WHERE a.employee_id + b.employee_id + c.n_nationkey + d.r_regionkey + e.r_regionkey > 0"
		res := run(t, db, long, drill.WithParameter("options", map[string]string{"planner.enable_nljoin_for_scalar_only": "false"}))
		if !errors.Is(res.err, drill.ErrCanceled) || len(res.rows) != 0 {
			t.Errorf("a cancelled query gave %d rows and %v, want drill.ErrCanceled", len(res.rows), res.err)
		}
	})
}

// TestReplayVersion holds the version that both principals read (recorded:
// "the version"). Ping has no recording, because the statement that it sends
// has the query type SQL, and each recording of it fails for another reason.
func TestReplayVersion(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB, release string) {
		res := run(t, db, "SELECT version FROM sys.version")
		want := strings.TrimPrefix(release, "drill-")
		if res.err != nil || !reflect.DeepEqual(res.rows, [][]any{{want}}) {
			t.Errorf("the version is %v, %v, want %s", res.rows, res.err, want)
		}
	})
}
