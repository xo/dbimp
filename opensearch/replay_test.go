package opensearch_test

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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/opensearch"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/opensearch/. Each one decodes a real answer through the
// driver.
//
// The recordings sent a statement with the keys that the test named. The
// driver adds the page size 1000 to every plain SELECT, which the server reads
// as the default of its own cursor in the new engine, so the fake server leaves
// it out of a body when it holds 1000, and compares the rest. Every other
// request must match a recording to the key.

const testdata = "../testdata/opensearch"

// releases are the releases that step 6 recorded.
var releases = []string{"opensearch-2.19.6", "opensearch-3.9.0"}

// normalize returns the canonical form of the body of a request to
// /_plugins/_sql, without the page size when it holds 1000.
func normalize(b []byte) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	if m["fetch_size"] == float64(1000) {
		delete(m, "fetch_size")
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
	// tolerate is true for a test that sends a request that no recording
	// holds, which the server then answers with HTTP 418 and no failure.
	tolerate bool

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
		if ex.Request.Query != "" || !strings.HasPrefix(ex.Request.Path, "/_plugins/_sql") {
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
	if r.URL.RawQuery != "" {
		s.t.Errorf("the request has the query %q, want none", r.URL.RawQuery)
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
	if !s.tolerate {
		s.t.Errorf("no exchange matches %s %s %s", r.Method, r.URL, body)
	}
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
func replayOnly(t *testing.T, release string, keep func(*dbimptest.Exchange) bool) (*sql.DB, *replayServer) {
	t.Helper()
	srv, s := newReplay(t, release, keep)
	db, err := sql.Open(opensearch.Name, strings.Replace(srv.URL, "http://", "opensearch://admin:secret@", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, s
}

// replay opens the driver against every recorded exchange of release.
func replay(t *testing.T, release string) *sql.DB {
	t.Helper()
	db, _ := replayOnly(t, release, nil)
	return db
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	return readAllContext(t, t.Context(), db, query, args...)
}

// readAllContext is readAll with the context ctx.
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

// serverError returns err as a *opensearch.Error, and fails the test if it is
// not one.
func serverError(t *testing.T, err error) *opensearch.Error {
	t.Helper()
	e, ok := errors.AsType[*opensearch.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *opensearch.Error", err, err)
	}
	return e
}

// refused keeps the exchanges whose answer is a refusal for the ordinary user
// of the recordings, and no other (recorded as dbmeta_user).
func refused(ex *dbimptest.Exchange) bool {
	return strings.Contains(string(ex.Response.Content()), "User [name=dbmeta_user")
}

// TestReplayStatement holds D168 against a recorded answer: the columns come
// in the order of the statement, and each value has the Go type of its column.
func TestReplayStatement(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		cols, got, err := readAll(t, db, "SELECT n, s FROM dbmeta_rows WHERE n <= 2 ORDER BY n")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if want := []string{"n", "s"}; !slices.Equal(cols, want) {
			t.Errorf("%s: the columns are %v, want %v", release, cols, want)
		}
		if want := [][]any{{int64(1), "row 1"}, {int64(2), "row 2"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the rows are %v, want %v", release, got, want)
		}
		cts := columnTypes(t, db, "SELECT n, s FROM dbmeta_rows WHERE n <= 2 ORDER BY n")
		for i, want := range []struct {
			name string
			typ  reflect.Type
		}{{"INTEGER", reflect.TypeFor[int64]()}, {"KEYWORD", reflect.TypeFor[string]()}} {
			nullable, ok := cts[i].Nullable()
			if cts[i].DatabaseTypeName() != want.name || cts[i].ScanType() != want.typ || !nullable || !ok {
				t.Errorf("%s: column %d is %s %v %v %v, want %s %v and nullable", release, i, cts[i].DatabaseTypeName(), cts[i].ScanType(), nullable, ok, want.name, want.typ)
			}
		}
	}
}

// columnTypes runs the query and returns the types of its columns.
func columnTypes(t *testing.T, db *sql.DB, query string) []*sql.ColumnType {
	t.Helper()
	_, cts := columnsOf(t, db, query)
	return cts
}

// columnsOf runs the query and returns the names and the types of its columns.
func columnsOf(t *testing.T, db *sql.DB, query string) ([]string, []*sql.ColumnType) {
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
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols, cts
}

// TestReplayAlias holds that a column with AS has the label of its alias, and a
// column with no alias has the text of its expression (recorded: "a column
// with an alias"), and that a result with no rows has its columns (recorded:
// "no rows").
func TestReplayAlias(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		cols, got, err := readAll(t, db, "SELECT n AS x, n + 1 FROM dbmeta_rows ORDER BY n LIMIT 2")
		if err != nil || !slices.Equal(cols, []string{"x", "n + 1"}) || !reflect.DeepEqual(got, [][]any{{int64(1), int64(2)}, {int64(2), int64(3)}}) {
			t.Errorf("%s: the columns are %v, the rows %v, and the error %v", release, cols, got, err)
		}
		cols, got, err = readAll(t, db, "SELECT n, s FROM dbmeta_rows WHERE n > 1000")
		if err != nil || !slices.Equal(cols, []string{"n", "s"}) || len(got) != 0 {
			t.Errorf("%s: a result with no rows gave the columns %v, the rows %v and %v", release, cols, got, err)
		}
	}
}

// wantTypes are the rows of "every type" that the two releases agree on, except
// the one value that TestReplayValues names, by the
// id of the row, and by the name of the column (recorded: "every type"). A
// field with several values is a []any (D168), and NULL is nil.
func wantTypes() map[int64]map[string]any {
	ts := func(s string) time.Time {
		t, err := time.Parse("2006-01-02 15:04:05.999999999", s)
		if err != nil {
			panic(err)
		}
		return t
	}
	obj := func(a int64, b string) map[string]any { return map[string]any{"a": a, "b": b} }
	return map[int64]map[string]any{
		1: {
			"bin": []byte{0, 255}, "bo": true, "dt": ts("2026-10-01 07:04:56.123"), "sf": 12.345, "sh": int64(-32768), "by": int64(-128),
			"dtf": dbimp.Date{Year: 2026, Month: 10, Day: 1}, "id": int64(1), "dtn": ts("2026-10-01 12:34:56.123456789"), "d": 0.1,
			"f": 3.4028235e+38, "ip": "192.168.0.1", "gp": map[string]any{"lat": 41.12, "lon": -71.34}, "i": int64(-2147483648),
			"k": "é'\"\\ x", "l": int64(math.MinInt64), "n": []any{obj(1, "x"), obj(2, "y")}, "o": obj(1, "x"), "t": "hello world",
			"tk": "Hello Raw", "tm": dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56}, "hf": 0.1,
		},
		2: {"id": int64(2)},
		3: {"id": int64(3)},
		4: {
			"bin": []byte{}, "bo": false, "dt": ts("2026-10-01 12:34:56.123"), "sf": -0.01, "sh": int64(32767), "by": int64(127),
			"dtf": dbimp.Date{Year: 1, Month: 1, Day: 1}, "id": int64(4), "dtn": ts("2262-04-11 23:47:16.854775807"),
			"d": []any{1.5, nil, 2.5}, "f": 1.4e-45, "ip": "::1", "i": []any{int64(1), int64(2), int64(3)}, "k": []any{"a", "b"},
			"l": int64(math.MaxInt64), "n": []any{obj(3, "z")}, "o": []any{obj(1, "x"), obj(2, "y")}, "t": "",
			"tm": dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59}, "hf": 65504.0,
		},
		5: {
			"bo": true, "dt": ts("1001-01-01 00:00:00"), "id": int64(5), "dtn": ts("1970-01-01 00:00:00"), "d": 1.5, "i": int64(42), "k": "", "l": int64(1),
		},
	}
}

// TestReplayValues holds D135 and D168 against the recorded answer for every
// type: each value read through Rows.Scan into a *any has the Go type of the
// type table, NULL is nil, a field with several values is a []any, and a time
// is in UTC.
func TestReplayValues(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		cols, got, err := readAll(t, db, "SELECT * FROM dbmeta_types ORDER BY id")
		if err != nil {
			t.Fatalf("%s: %v", release, err)
		}
		if len(got) != 5 {
			t.Fatalf("%s: read %d rows, want 5", release, len(got))
		}
		idCol := slices.Index(cols, "id")
		for _, row := range got {
			id, ok := row[idCol].(int64)
			if !ok {
				t.Fatalf("%s: the id is %v", release, row[idCol])
			}
			want := wantTypes()[id]
			for i, col := range cols {
				w, known := want[col]
				if release == "opensearch-3.9.0" && id == 5 && col == "l" {
					// 3.9.0 reads 1.9 in a long as NULL. 2.19.6 read it as 1.
					w, known = nil, true
				}
				if !known {
					if slices.Contains([]string{"mot", "al", "kv"}, col) {
						continue
					}
					w = nil
				}
				if !reflect.DeepEqual(row[i], w) {
					t.Errorf("%s: id %d: column %s is %#v, want %#v", release, id, col, row[i], w)
				}
			}
		}
	}
}

// TestReplayColumnTypes holds the type table against the recorded schema: the
// database type name and the scan type of each column.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	want := map[string]struct {
		name string
		typ  reflect.Type
	}{
		"bin": {"BINARY", reflect.TypeFor[[]byte]()},
		"bo":  {"BOOLEAN", reflect.TypeFor[bool]()},
		"dt":  {"TIMESTAMP", reflect.TypeFor[time.Time]()},
		"sf":  {"DOUBLE", reflect.TypeFor[float64]()},
		"sh":  {"SHORT", reflect.TypeFor[int64]()},
		"by":  {"BYTE", reflect.TypeFor[int64]()},
		"dtf": {"DATE", reflect.TypeFor[dbimp.Date]()},
		"id":  {"INTEGER", reflect.TypeFor[int64]()},
		"f":   {"FLOAT", reflect.TypeFor[float64]()},
		"ip":  {"IP", reflect.TypeFor[string]()},
		"gp":  {"GEO_POINT", reflect.TypeFor[map[string]any]()},
		"l":   {"LONG", reflect.TypeFor[int64]()},
		"n":   {"NESTED", reflect.TypeFor[[]any]()},
		"o":   {"OBJECT", reflect.TypeFor[map[string]any]()},
		"t":   {"TEXT", reflect.TypeFor[string]()},
		"tm":  {"TIME", reflect.TypeFor[dbimp.LocalTime]()},
	}
	for _, release := range releases {
		db := replay(t, release)
		cols, cts := columnsOf(t, db, "SELECT * FROM dbmeta_types ORDER BY id")
		for i, col := range cols {
			w, ok := want[col]
			if !ok {
				continue
			}
			if cts[i].DatabaseTypeName() != w.name || cts[i].ScanType() != w.typ {
				t.Errorf("%s: column %s is %s and %v, want %s and %v", release, col, cts[i].DatabaseTypeName(), cts[i].ScanType(), w.name, w.typ)
			}
		}
	}
}

// scanDests returns a destination for each column of cols, of the Go type of
// its type in the type table, or sql.Null of it when null is true, and *any
// for a column that it does not know. The map holds the destination of each
// known column by its name.
func scanDests(cols []string, null bool) ([]any, map[string]any) {
	typed := map[string]any{
		"id": new(int64), "sh": new(int64), "by": new(int64), "l": new(int64), "f": new(float64), "sf": new(float64), "d": new(float64), "hf": new(float64),
		"bo": new(bool), "k": new(string), "ip": new(string), "t": new(string), "tk": new(string), "bin": new([]byte), "dt": new(time.Time), "dtn": new(time.Time),
		"dtf": new(dbimp.Date), "tm": new(dbimp.LocalTime), "gp": new(map[string]any), "o": new(map[string]any), "n": new([]any), "i": new(int64),
	}
	if null {
		typed = map[string]any{
			"id": new(int64), "sh": new(sql.Null[int64]), "by": new(sql.Null[int64]), "l": new(sql.Null[int64]), "f": new(sql.Null[float64]), "sf": new(sql.Null[float64]),
			"d": new(sql.Null[float64]), "hf": new(sql.Null[float64]), "bo": new(sql.Null[bool]), "k": new(sql.Null[string]), "ip": new(sql.Null[string]),
			"t": new(sql.Null[string]), "tk": new(sql.Null[string]), "bin": new([]byte), "dt": new(sql.Null[time.Time]), "dtn": new(sql.Null[time.Time]),
			"dtf": new(sql.Null[dbimp.Date]), "tm": new(sql.Null[dbimp.LocalTime]), "gp": new(sql.Null[map[string]any]), "o": new(sql.Null[map[string]any]), "n": new(sql.Null[[]any]), "i": new(sql.Null[int64]),
		}
	}
	out := make([]any, len(cols))
	for i, c := range cols {
		if d, ok := typed[c]; ok {
			out[i] = d
		} else {
			out[i] = new(any)
		}
	}
	return out, typed
}

// TestReplayScanIntoTheTypes holds that a caller scans the recorded values into
// the Go types of the type table, and into sql.Null of them for NULL, through
// Rows.Scan, the real path of a caller (dbmeta D93). A field with several
// values does not scan into a scalar.
func TestReplayScanIntoTheTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		scanRows(t, release, replay(t, release))
	}
}

// at returns the value that the destination named name of typed points to, or the
// zero value of T.
func at[T any](typed map[string]any, name string) T {
	var zero T
	p, ok := typed[name].(*T)
	if !ok || p == nil {
		return zero
	}
	return *p
}

// scanRows scans the rows of every type into the destinations of scanDests.
func scanRows(t *testing.T, release string, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM dbmeta_types ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	// The rows come in the order of their ids, 1 to 5.
	for id := 1; rows.Next(); id++ {
		dests, typed := scanDests(cols, id == 3)
		err := rows.Scan(dests...)
		switch id {
		case 1:
			if err != nil {
				t.Fatalf("%s: row 1: %v", release, err)
			}
			gp, o := at[map[string]any](typed, "gp"), at[map[string]any](typed, "o")
			if at[int64](typed, "id") != 1 || at[int64](typed, "sh") != -32768 || at[int64](typed, "l") != math.MinInt64 || at[float64](typed, "f") != 3.4028235e+38 ||
				at[float64](typed, "sf") != 12.345 || !at[bool](typed, "bo") || at[string](typed, "k") != "é'\"\\ x" || at[string](typed, "ip") != "192.168.0.1" ||
				!slices.Equal(at[[]byte](typed, "bin"), []byte{0, 255}) || !at[time.Time](typed, "dt").Equal(time.Date(2026, 10, 1, 7, 4, 56, 123e6, time.UTC)) ||
				at[time.Time](typed, "dt").Location() != time.UTC || at[dbimp.Date](typed, "dtf") != (dbimp.Date{Year: 2026, Month: 10, Day: 1}) ||
				at[dbimp.LocalTime](typed, "tm") != (dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56}) || gp["lat"] != 41.12 || o["b"] != "x" || len(at[[]any](typed, "n")) != 2 {
				t.Errorf("%s: row 1 scanned into the typed destinations gave %v", release, typed)
			}
		case 3:
			if err != nil {
				t.Fatalf("%s: row 3: %v", release, err)
			}
			for name, d := range typed {
				if name != "id" && !isNull(d) {
					t.Errorf("%s: row 3: %s is %v, want NULL", release, name, reflect.ValueOf(d).Elem())
				}
			}
		case 4:
			if err == nil {
				t.Errorf("%s: row 4: a field with several values scanned into a scalar", release)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", release, err)
	}
}

// isNull reports whether d, a destination of scanDests for a NULL, holds NULL:
// an sql.Null that is not valid, or a nil slice or map.
func isNull(d any) bool {
	v := reflect.ValueOf(d).Elem()
	switch v.Kind() {
	case reflect.Struct:
		return !v.FieldByName("Valid").Bool()
	case reflect.Slice, reflect.Map:
		return v.IsNil()
	}
	return false
}
