package solr_test

import (
	"database/sql"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/solr"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/solr/. Each one decodes a real answer through the driver.
//
// Most recordings did not ask for includeMetadata, and the driver always
// does. So the fake server matches a statement by its text, whatever the
// other fields of the form hold, and the driver reads the columns of an
// answer with no metadata from the keys of its first row. The recordings of
// includeMetadata answer as they are.

const testdata = "../testdata/solr"

// releases are the releases that step 6 recorded. The three gave the same
// answer to each statement (docs/SOLR.md, Flavors).
var releases = []string{"solr-9.9.0", "solr-9.10.1", "solr-10.0.0"}

// stmtOf returns the statement of the body of a request, without the column
// typeName, which the driver does not ask for and the recordings did.
func stmtOf(body string) string {
	v, err := url.ParseQuery(body)
	if err != nil {
		return body
	}
	return strings.Replace(v.Get("stmt"), "columnName, dataType, typeName", "columnName, dataType", 1)
}

// match matches a request by its method and its path, and a statement by its
// text.
func match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path {
		return false
	}
	if strings.HasSuffix(r.URL.Path, "/sql") && r.Method == http.MethodPost {
		return stmtOf(string(body)) == stmtOf(ex.Request.Body)
	}
	return r.URL.RawQuery == ex.Request.Query
}

// open returns a database on a fake server that replays release, as the
// administrator of the collection dbimp.
func open(t *testing.T, release string) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, match)
	db, err := sql.Open(solr.Name, strings.Replace(srv.URL, "http://", "solr://admin:secret@", 1)+"/dbimp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// forEachRelease runs f for each release that step 6 recorded.
func forEachRelease(t *testing.T, f func(t *testing.T, db *sql.DB)) {
	t.Helper()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			f(t, open(t, release))
		})
	}
}

// read runs the query and returns its rows, each as the values of *any.
func read(t *testing.T, db *sql.DB, query string) ([]string, [][]any) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		row := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scanning a row of %q: %v", query, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the rows of %q: %v", query, err)
	}
	return cols, out
}

func utc(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v.UTC()
}

// everyType is the statement of "every type" (recorded).
const everyType = "SELECT id, n_i, n_l, n_f, n_d, n_b, n_s, n_t, n_st, n_dt, n_bin, n_u, n_loc, n_is, n_ls, n_ds, n_ss, n_bs, n_dts FROM dbimp WHERE id = '1' OR id = '2' OR id = '3' ORDER BY id LIMIT 3"

// TestReplayEveryType holds D166: each value has the Go type of the field
// type of its column, which the driver reads from metadata.COLUMNS and the
// luke handler. A BoolField is a bool, a BinaryField a []byte and a UUIDField
// a uuid.UUID, though the SQL layer names each one VARCHAR.
func TestReplayEveryType(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		cols, got := read(t, db, everyType)
		wantCols := []string{"id", "n_i", "n_l", "n_f", "n_d", "n_b", "n_s", "n_t", "n_st", "n_dt", "n_bin", "n_u", "n_loc", "n_is", "n_ls", "n_ds", "n_ss", "n_bs", "n_dts"}
		if !reflect.DeepEqual(cols, wantCols) {
			t.Fatalf("columns are %q, want %q", cols, wantCols)
		}
		want := [][]any{
			{
				"1", int64(2147483647), int64(9223372036854775807), 3.4028235e38, 1.7976931348623157e308, true,
				"héllo wörld 日本 🙂", "the quick brown fox", "Sortable Text", utc(t, "9999-12-31T23:59:59.999Z"),
				[]byte{0, 1, 2, 0xff}, uuid.MustParse("8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b"), "45.5,-122.6",
				[]any{int64(1), int64(2), int64(3)}, []any{int64(9223372036854775807), int64(-1)}, []any{0.5, -0.25},
				[]any{"a", "b"}, []any{true, false}, []any{"2026-10-01T00:00:00Z"},
			},
			{
				"2", int64(-2147483648), int64(-9223372036854775808), 0.1, 2.0, false,
				nil, nil, nil, utc(t, "0001-01-01T00:00:00Z"),
				nil, nil, nil, nil, nil, nil, nil, nil, nil,
			},
			{
				"3", nil, nil, nil, nil, nil, nil, nil, nil, utc(t, "2026-10-01T12:34:56.123Z"),
				nil, nil, nil, nil, nil, nil, nil, nil, nil,
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rows are\n%#v\nwant\n%#v", got, want)
		}
	})
}

// TestReplayColumnTypes holds D166: the database type name is the SQL type of
// metadata.COLUMNS, and the scan type is the Go type of the value.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	db := open(t, "solr-10.0.0")
	rows, err := db.QueryContext(t.Context(), everyType)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil || rows.Err() != nil {
		t.Fatal(err, rows.Err())
	}
	want := map[string][2]string{
		"id":    {"VARCHAR", "string"},
		"n_i":   {"BIGINT", "int64"},
		"n_f":   {"DOUBLE", "float64"},
		"n_b":   {"VARCHAR", "bool"},
		"n_dt":  {"TIMESTAMP", "time.Time"},
		"n_bin": {"VARCHAR", "[]uint8"},
		"n_u":   {"VARCHAR", "uuid.UUID"},
		"n_loc": {"VARCHAR", "string"},
		"n_is":  {"ANY", "[]interface {}"},
		"n_bs":  {"ANY", "[]interface {}"},
	}
	for _, ct := range cts {
		w, ok := want[ct.Name()]
		if !ok {
			continue
		}
		if got := [2]string{ct.DatabaseTypeName(), ct.ScanType().String()}; got != w {
			t.Errorf("column %s is %v, want %v", ct.Name(), got, w)
		}
		if nullable, ok := ct.Nullable(); !nullable || !ok {
			t.Errorf("column %s nullable is %v %v, want true true", ct.Name(), nullable, ok)
		}
	}
}

// TestReplayAggregates holds D166: an aggregate names no type, so its value
// reads by its JSON token, and an answer with no metadata names its columns
// by the keys of its first row (recorded: "the average of integers", "an
// aggregate with no name").
func TestReplayAggregates(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		cols, got := read(t, db, "SELECT avg(n_i) AS a, sum(n_i) AS s, count(*) AS c, avg(n_d) AS ad FROM dbimp WHERE id = '1' OR id = '2'")
		if want := []string{"a", "s", "c", "ad"}; !reflect.DeepEqual(cols, want) {
			t.Errorf("columns are %q, want %q", cols, want)
		}
		if want := [][]any{{int64(0), int64(-1), int64(2), 8.988465674311579e307}}; !reflect.DeepEqual(got, want) {
			t.Errorf("rows are %#v, want %#v", got, want)
		}
		cols, got = read(t, db, "SELECT count(*), min(n_i) FROM dbimp")
		if want := []string{"EXPR$0", "EXPR$1"}; !reflect.DeepEqual(cols, want) {
			t.Errorf("columns are %q, want %q", cols, want)
		}
		if want := [][]any{{int64(303), int64(-2147483648)}}; !reflect.DeepEqual(got, want) {
			t.Errorf("rows are %#v, want %#v", got, want)
		}
	})
}

// TestReplayMetadata holds D166: the columns come from the metadata, in the
// order of the SELECT, with the alias of each field, also for a result with no
// rows (D18).
func TestReplayMetadata(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		cols, got := read(t, db, "SELECT n_l, id AS x, n_i FROM dbimp ORDER BY id LIMIT 3")
		if want := []string{"n_l", "x", "n_i"}; !reflect.DeepEqual(cols, want) {
			t.Errorf("columns are %q, want %q", cols, want)
		}
		want := [][]any{
			{int64(9223372036854775807), "1", int64(2147483647)},
			{int64(-9223372036854775808), "2", int64(-2147483648)},
			{nil, "3", nil},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rows are %#v, want %#v", got, want)
		}
		cols, got = read(t, db, "SELECT n_l, id AS x FROM dbimp WHERE id = 'none' LIMIT 3")
		if want := []string{"n_l", "x"}; !reflect.DeepEqual(cols, want) {
			t.Errorf("columns of a result with no rows are %q, want %q", cols, want)
		}
		if len(got) != 0 {
			t.Errorf("read %d rows, want none", len(got))
		}
	})
}

// TestReplaySameName holds D166: a statement whose columns share a name is
// refused, because the server gives wrong values for them (recorded: "two
// columns with one name").
func TestReplaySameName(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		_, _, err := tryQuery(t.Context(), db, "SELECT id AS a, id AS b, n_i AS b FROM dbimp ORDER BY id LIMIT 2")
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("the error is %v, want dbimp.ErrNotSupported", err)
		}
	})
}

// TestReplayLargeResult reads the result of a statement with no limit, which
// the export handler sends (recorded: "a result with no limit"). The result is
// one body, and the driver reads it to its end.
func TestReplayLargeResult(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		_, got := read(t, db, "SELECT id, n_i FROM dbimp")
		if len(got) != 303 {
			t.Fatalf("read %d rows, want 303", len(got))
		}
		if want := []any{"p299", int64(299)}; !reflect.DeepEqual(got[0], want) {
			t.Errorf("the first row is %#v, want %#v", got[0], want)
		}
	})
}

// TestReplaySpatial holds D166: a spatial field is the text of its value
// (recorded: "lead: spatial fields").
func TestReplaySpatial(t *testing.T) {
	t.Parallel()
	db := open(t, "solr-10.0.0")
	_, got := read(t, db, "SELECT id, n_rpt, n_bbox, n_pt FROM dbimp WHERE id = 'x1' LIMIT 1")
	want := [][]any{{"x1", "POINT(-122.6 45.5)", "ENVELOPE(-10, 20, 15, 10)", "1.5,2.5"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows are %#v, want %#v", got, want)
	}
}

// TestReplayStatementErrors holds D166: an exception of the server before any
// row is the error of the query, and not ErrIncomplete (recorded: "a syntax
// error", "a table that does not exist" and "a column that does not exist").
func TestReplayStatementErrors(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		for _, tt := range []struct{ query, want string }{
			{"SELEC id FROM dbimp", "parse failed"},
			{"SELECT id FROM nothere LIMIT 1", "nothere"},
			{"SELECT nothere FROM dbimp LIMIT 1", "nothere"},
			{"INSERT INTO dbimp (id, n_s) VALUES ('c1', 'inserted')", "INSERT"},
		} {
			_, _, err := tryQuery(t.Context(), db, tt.query)
			var serr *solr.Error
			switch {
			case err == nil:
				t.Errorf("%q gave no error", tt.query)
			case !errors.As(err, &serr) || serr.HTTPStatus != http.StatusOK:
				t.Errorf("%q gave %v, want a *solr.Error with HTTP 200", tt.query, err)
			case !strings.Contains(err.Error(), tt.want):
				t.Errorf("%q gave %v, want a message with %q", tt.query, err, tt.want)
			case errors.Is(err, dbimp.ErrIncomplete):
				t.Errorf("%q gave %v, which wraps ErrIncomplete for an error before any row", tt.query, err)
			}
		}
	})
}

// TestReplayInfinity holds docs/SOLR.md, Types: a document whose double holds
// Infinity fails the query before any row, so the driver returns the error
// of the server from the query (recorded: "lead: infinities", on 10.0.0 only).
func TestReplayInfinity(t *testing.T) {
	t.Parallel()
	_, _, err := tryQuery(t.Context(), open(t, "solr-10.0.0"), "SELECT id, n_d, n_f, n_ds FROM dbimp WHERE id = 'x1' LIMIT 1")
	var serr *solr.Error
	if !errors.As(err, &serr) || !strings.Contains(serr.Message, "cannot be cast") || errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want a *solr.Error about a cast, and not ErrIncomplete", err)
	}
}

// TestReplayErrorAfterRows holds D107 and D166: an exception after some rows
// wraps dbimp.ErrIncomplete (recorded: "an error after some rows").
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	forEachRelease(t, func(t *testing.T, db *sql.DB) {
		rows, err := db.QueryContext(t.Context(), "SELECT id, n_v FROM dbimp ORDER BY id LIMIT 3")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		err = rows.Err()
		var serr *solr.Error
		if n != 2 || !errors.Is(err, dbimp.ErrIncomplete) || !errors.As(err, &serr) || !strings.Contains(serr.Message, "ArrayList") {
			t.Errorf("read %d rows and the error %v, want 2 rows and an error that wraps ErrIncomplete and a *solr.Error about the vector", n, err)
		}
	})
}

// serveExchange serves the response of the recorded exchange file to every
// request, and returns a database on it.
func serveExchange(t *testing.T, file string) *sql.DB {
	t.Helper()
	ex, err := dbimptest.ReadExchange(filepath.Join(testdata, file))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		maps.Copy(w.Header(), ex.Response.Header)
		w.Header().Del("Content-Length")
		w.WriteHeader(ex.Response.Status)
		_, _ = w.Write(ex.Response.Content())
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(solr.Name, strings.Replace(srv.URL, "http://", "solr://u:p@", 1)+"/dbimp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestReplayHTTPErrors holds D166: a status that is not 2xx gives a
// *solr.Error with the title of the page of HTML, which unwraps to the
// *dbimp.StatusError (recorded: "a wrong password", "a placeholder" and "a
// statement sent to a collection that does not exist").
func TestReplayHTTPErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		file   string
		status int
		want   string
	}{
		{"solr-10.0.0-080-post--solr-dbimp-sql.json", 401, "Bad credentials"},
		{"solr-10.0.0-059-post--solr-dbimp-sql.json", 500, "unsupported predicate expression"},
		{"solr-10.0.0-017-post--solr-nothere-sql.json", 405, "HTTP method POST is not supported by this URL"},
		{"solr-10.0.0-122-post--solr-nothere-sql.json", 403, "Unauthorized request"},
	} {
		_, _, err := tryQuery(t.Context(), serveExchange(t, tt.file), "SELECT id FROM dbimp LIMIT 1")
		var serr *solr.Error
		var status *dbimp.StatusError
		switch {
		case !errors.As(err, &serr) || serr.HTTPStatus != tt.status:
			t.Errorf("%s: the error is %v, want a *solr.Error with HTTP %d", tt.file, err, tt.status)
		case !strings.Contains(serr.Message, tt.want):
			t.Errorf("%s: the message is %q, want %q", tt.file, serr.Message, tt.want)
		case !errors.As(err, &status) || status.Code != tt.status:
			t.Errorf("%s: the error is %v, want one that wraps a *dbimp.StatusError with HTTP %d", tt.file, err, tt.status)
		case strings.Contains(err.Error(), "secret"):
			t.Errorf("%s: the error holds a password: %v", tt.file, err)
		}
	}
}

// TestReplayLuke reads the schema of the table once for each connection
// (D166): a second statement on the table sends no request for it.
func TestReplayLuke(t *testing.T) {
	t.Parallel()
	srv := dbimptest.ReplayRelease(t, testdata, "solr-10.0.0", match)
	var luke, columns int
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/admin/luke"):
			luke++
		case strings.Contains(string(body), "metadata.COLUMNS"):
			columns++
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, srv.URL+r.URL.RequestURI(), strings.NewReader(string(body)))
		if err != nil {
			t.Error(err)
			return
		}
		req.Header = r.Header.Clone()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		defer res.Body.Close()
		maps.Copy(w.Header(), res.Header)
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	t.Cleanup(counting.Close)
	db, err := sql.Open(solr.Name, strings.Replace(counting.URL, "http://", "solr://u:p@", 1)+"/dbimp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	for range 3 {
		read(t, db, everyType)
	}
	if luke != 1 || columns != 1 {
		t.Errorf("the driver read the luke handler %d times and metadata.COLUMNS %d times for three statements on one connection, want 1 and 1", luke, columns)
	}
}
