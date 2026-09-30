package databend_test

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databend"
	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/databend/. Each one decodes a real answer through the
// driver.

const testdata = "../testdata/databend"

// releases are the releases that step 6 recorded.
var releases = []string{"databend-1.2.881", "databend-1.2.948"}

// matcher returns a match that serves each recorded exchange once, in order,
// because a page of a query that runs is the same request each time. Two
// bodies of POST /v1/query match by their members other than the session,
// which the driver writes from the DSN and the recording did not.
func matcher() dbimptest.Match {
	var mu sync.Mutex
	used := map[*dbimptest.Exchange]bool{}
	return func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		mu.Lock()
		defer mu.Unlock()
		if used[ex] || r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path || r.URL.RawQuery != ex.Request.Query {
			return false
		}
		if canon(body) != canon(ex.Request.Content()) {
			return false
		}
		used[ex] = true
		return true
	}
}

// base are the settings that the driver sends with every statement, from the
// DSN of replay, which the recordings did not send.
var base = map[string]any{
	"format_null_as_str":     "0",
	"geometry_output_format": "WKT",
	"http_json_result_mode":  "display",
	"timezone":               "UTC",
}

// canon returns the canonical form of the body of a request. Of its session,
// it keeps the settings that are not in base.
func canon(b []byte) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	s, _ := m["session"].(map[string]any)
	delete(m, "session")
	settings, _ := s["settings"].(map[string]any)
	for k, v := range settings {
		if base[k] == v {
			delete(settings, k)
		}
	}
	if len(settings) > 0 {
		m["settings"] = settings
	}
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	return string(out)
}

// replay opens the driver against the recorded exchanges of release. A front
// answers the final URI of a query, which the recording followed only for a
// query of more than one page, and hands every other request to the replay.
func replay(t *testing.T, release string) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, matcher())
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var mu sync.Mutex
	followed := map[string]bool{}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen := followed[r.URL.Path]
		followed[r.URL.Path] = true
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/final") && !seen && !recorded(release, r.URL.Path) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"Succeeded","error":null,"data":[]}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	db, err := sql.Open(databend.Name, strings.Replace(front.URL, "http://", "databend://root:secret@", 1)+"/dbmeta?cancel=none&timezone=UTC")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	return db
}

// recorded reports whether the recordings of release hold a GET of path.
func recorded(release, path string) bool {
	m, err := dbimptest.ReadManifest(testdata + "/" + dbimptest.ManifestName)
	if err != nil {
		return false
	}
	name := strings.ReplaceAll(strings.ReplaceAll(strings.Trim(path, "/"), "/", "-"), "_", "-")
	for _, e := range m.Entries {
		if e.Release == release && strings.HasSuffix(e.File, "get--"+name+".json") {
			return true
		}
	}
	return false
}

// readAll runs query, and reads its columns and every row.
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

func dec(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// TestReplayTypes reads every type of the recorded table on each release
// (D118 and D119).
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := replay(t, release)
			cols, rows, err := readAll(t, db, "SELECT * FROM dbmeta.types ORDER BY id")
			if !errors.Is(err, dbimp.ErrNotSupported) {
				t.Fatalf("reading the table, whose rows hold a Bitmap, gave %v, want dbimp.ErrNotSupported (D119)", err)
			}
			if len(rows) != 0 || len(cols) != 28 {
				t.Fatalf("read %d rows of %d columns before the Bitmap, want 0 rows of 28", len(rows), len(cols))
			}
		})
	}
}

// TestReplayValues reads the values of the columns of the recorded table
// that the driver decodes, through a query that leaves out the Bitmap.
func TestReplayValues(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := replay(t, release)
			_, rows, err := readAll(t, db, "SELECT ts, tz FROM dbmeta.types WHERE id = 1", databend.WithParameter("session", map[string]any{"settings": map[string]string{"timezone": "Asia/Kolkata"}}))
			if err != nil {
				t.Fatal(err)
			}
			kolkata, err := time.LoadLocation("Asia/Kolkata")
			if err != nil {
				t.Skip(err)
			}
			ts, _ := rows[0][0].(time.Time)
			if want := time.Date(1000, 1, 1, 5, 53, 28, 0, kolkata); !ts.Equal(want) {
				t.Errorf("the Timestamp in Asia/Kolkata is %v, want %v", ts, want)
			}
			tz, _ := rows[0][1].(time.Time)
			if _, off := tz.Zone(); off != 5*3600+1800 || tz.Hour() != 12 {
				t.Errorf("the Timestamp_Tz is %v, want 12:34:56 at +05:30", tz)
			}
			_, rows, err = readAll(t, db, "SELECT bin FROM dbmeta.types WHERE id = 1", databend.WithParameter("session", map[string]any{"settings": map[string]string{"binary_output_format": "base64"}}))
			if err != nil || !reflect.DeepEqual(rows[0][0], []byte{0, 0xff}) {
				t.Errorf("the Binary in base64 is %v, %v, want 00ff", rows, err)
			}
			_, rows, err = readAll(t, db, "SELECT g, geo FROM dbmeta.types WHERE id = 1", databend.WithParameter("session", map[string]any{"settings": map[string]string{"geometry_output_format": "WKT"}}))
			if err != nil || !reflect.DeepEqual(rows, [][]any{{"POINT(1 2)", "POINT(3 4)"}}) {
				t.Errorf("the geometries are %v, %v, want POINT(1 2) and POINT(3 4)", rows, err)
			}
			_, rows, err = readAll(t, db, "SELECT bitmap_to_array(bm) AS a FROM dbmeta.types WHERE id = 1")
			if err != nil || !reflect.DeepEqual(rows, [][]any{{[]any{uint64(1), uint64(3), uint64(5)}}}) {
				t.Errorf("bitmap_to_array gave %v, %v, want [1 3 5]", rows, err)
			}
		})
	}
}

// TestReplayLiterals reads literals of each kind (D118).
func TestReplayLiterals(t *testing.T) {
	t.Parallel()
	db := replay(t, releases[1])
	_, rows, err := readAll(t, db, "SELECT 1 AS a, 300 AS b, 70000 AS c, 5000000000 AS d, 1.5 AS e, 'x' AS f, true AS g, NULL AS h, 1e10 AS i, 123456789012345678901234567890 AS j")
	if err != nil {
		t.Fatal(err)
	}
	// The server types 1e10 as an integer, and a positive literal above the
	// range of an Int32 as a UInt64, which is a uint64 (recorded, D138).
	want := []any{int64(1), int64(300), int64(70000), uint64(5000000000), dec("1.5"), "x", true, nil, uint64(1e10), dec("123456789012345678901234567890")}
	if len(rows) != 1 || len(rows[0]) != len(want) {
		t.Fatalf("the literals are %v, want one row of %d", rows, len(want))
	}
	for i, w := range want {
		g := rows[0][i]
		if gd, ok := g.(*apd.Decimal); ok {
			if wd, ok := w.(*apd.Decimal); !ok || gd.Cmp(wd) != 0 {
				t.Errorf("column %d is %v, want %v", i, g, w)
			}
			continue
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("column %d is %#v, want %#v", i, g, w)
		}
	}
}

// TestReplayPages reads a result of three pages, by next_uri (D123).
func TestReplayPages(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := replay(t, release)
			_, rows, err := readAll(t, db, "SELECT number FROM numbers(25000)", databend.WithParameter("pagination", map[string]any{"max_rows_per_page": 10000, "wait_time_secs": 1}))
			if err != nil || len(rows) != 25000 {
				t.Fatalf("read %d rows, %v, want 25000", len(rows), err)
			}
			seen := map[uint64]bool{}
			for _, r := range rows {
				// numbers gives a UInt64 (D138).
				n, _ := r[0].(uint64)
				seen[n] = true
			}
			if len(seen) != 25000 {
				t.Errorf("read %d numbers, want 25000 of them", len(seen))
			}
		})
	}
}

// TestReplayErrorAfterRows reads the rows of two pages, and then the error
// of a later page, which wraps dbimp.ErrIncomplete (D107 and D123).
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		t.Run(release, func(t *testing.T) {
			t.Parallel()
			db := replay(t, release)
			_, rows, err := readAll(t, db, "SELECT number AS n FROM numbers(20000) UNION ALL SELECT to_uint8(300 + sleep(2)) AS n", databend.WithParameter("pagination", map[string]any{"max_rows_per_page": 10000, "wait_time_secs": 1}))
			var e *databend.Error
			if !errors.Is(err, dbimp.ErrIncomplete) || !errors.As(err, &e) || e.Code != 1006 {
				t.Errorf("the error is %v, want the code 1006, which wraps dbimp.ErrIncomplete", err)
			}
			if len(rows) != 20000 {
				t.Errorf("read %d rows before the error, want 20000", len(rows))
			}
		})
	}
}

// TestReplayErrors holds the error of a statement that fails before its
// rows.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db := replay(t, releases[0])
	for _, tt := range []struct {
		query string
		code  int
	}{
		{"SELEC 1", 1005},
		{"SELECT * FROM dbmeta.nosuch", 1025},
		{"SELECT 1; SELECT 2", 1005},
	} {
		_, _, err := readAll(t, db, tt.query)
		var e *databend.Error
		if !errors.As(err, &e) || e.Code != tt.code || errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s gave %v, want the code %d before any row", tt.query, err, tt.code)
		}
	}
}

// TestReplayParameters binds positional and named arguments (D120).
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	db := replay(t, releases[1])
	_, rows, err := readAll(t, db, "SELECT ? AS a, ? AS b, ? AS c, ? AS d", int64(1), "x", nil, true)
	if err != nil || !reflect.DeepEqual(rows, [][]any{{int64(1), "x", nil, true}}) {
		t.Errorf("the positional arguments gave %v, %v, want 1, x, NULL, true", rows, err)
	}
	_, rows, err = readAll(t, db, "SELECT :a AS a, :b AS b", sql.Named("a", 1.5), sql.Named("b", "12345678901234567890.123456789"))
	if err != nil || !reflect.DeepEqual(rows, [][]any{{1.5, "12345678901234567890.123456789"}}) {
		t.Errorf("the named arguments gave %v, %v", rows, err)
	}
	_, rows, err = readAll(t, db, "SELECT ? AS a, ? AS b", int64(9223372036854775807), uint64(18446744073709551615))
	// The server types both as a UInt64 (recorded).
	if err != nil || len(rows) != 1 || rows[0][0] != uint64(9223372036854775807) || rows[0][1] != uint64(18446744073709551615) {
		t.Errorf("the large integers gave %v, %v", rows, err)
	}
	if _, _, err := readAll(t, db, "SELECT ? AS a, ? AS b", int64(1), sql.Named("b", 2)); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("mixed arguments gave %v, want dbimp.ErrArguments", err)
	}
	if _, _, err := readAll(t, db, "SELECT ? AS a", []byte{1}); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a []byte argument gave %v, want dbimp.ErrNotSupported (D120)", err)
	}
}

// TestReplayRowsAffected reads the count that a statement that changes rows
// returns.
func TestReplayRowsAffected(t *testing.T) {
	t.Parallel()
	db := replay(t, releases[1])
	for _, tt := range []struct {
		query string
		want  int64
	}{
		{"INSERT INTO dbmeta.crud VALUES (1, 'a'), (2, 'b')", 2},
		{"UPDATE dbmeta.crud SET v = 'c' WHERE k = 1", 1},
		{"DELETE FROM dbmeta.crud WHERE k = 2", 1},
	} {
		res, err := db.ExecContext(t.Context(), tt.query)
		if err != nil {
			t.Fatalf("%s: %v", tt.query, err)
		}
		if n, err := res.RowsAffected(); err != nil || n != tt.want {
			t.Errorf("%s affected %d, %v, want %d", tt.query, n, err, tt.want)
		}
	}
	res, err := db.ExecContext(t.Context(), "REPLACE INTO dbmeta.crud ON (k) VALUES (1, 'r')")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("REPLACE INTO, whose result names no count, gave %v, want dbimp.ErrNotSupported", err)
	}
	res, err = db.ExecContext(t.Context(), "MERGE INTO dbmeta.crud AS t USING (SELECT 1 AS k, 'm' AS v UNION ALL SELECT 5, 'n') AS s ON t.k = s.k WHEN MATCHED THEN UPDATE SET t.v = s.v WHEN NOT MATCHED THEN INSERT (k, v) VALUES (s.k, s.v)")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 2 {
		t.Errorf("MERGE INTO affected %d, %v, want 2, one insert and one update", n, err)
	}
}

// TestReplayTransaction runs a transaction that the session carries, and one
// that a DDL statement commits (D121 and D122).
func TestReplayTransaction(t *testing.T) {
	t.Parallel()
	db := replay(t, releases[1])
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO tx VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := tx.QueryRowContext(t.Context(), "SELECT count(*) AS n FROM tx").Scan(&n); err != nil || n != 1 {
		t.Errorf("the count in the transaction is %d, %v, want 1", n, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx, err = db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO tx VALUES (2)"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO tx VALUES (3)"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "CREATE OR REPLACE TABLE tx_ddl (k INT)"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err == nil {
		t.Error("a Commit after a DDL statement committed the transaction gave no error, want one (D121)")
	}
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true}); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a read-only transaction gave %v, want dbimp.ErrNotSupported", err)
	}
}

// TestReplayTimeout holds WithTimeout, which sends max_execute_time_in_seconds,
// and the error of the server when the time ends (D109).
func TestReplayTimeout(t *testing.T) {
	t.Parallel()
	db := replay(t, releases[0])
	_, _, err := readAll(t, db, "SELECT sum(number) FROM numbers(1000000000000)", databend.WithTimeout(1500*time.Millisecond), databend.WithParameter("pagination", map[string]any{"wait_time_secs": 1}))
	var e *databend.Error
	if !errors.As(err, &e) || e.Code != 1043 {
		t.Errorf("the query with WithTimeout gave %v, want the code 1043", err)
	}
}
