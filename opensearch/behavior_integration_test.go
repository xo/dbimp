package opensearch_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/opensearch"
)

// TestIntegrationVersion holds the answers of step 16: the version of the
// server, which the administrator reads with GET /, and which the ordinary user
// is refused with HTTP 403. SQL has no function for the version, so the driver
// reads none. The header X-OpenSearch-Version of the 3 series gives the
// version to the ordinary user with every answer (recorded: "the version" and
// "the version in SQL").
func TestIntegrationVersion(t *testing.T) {
	version := serverVersion(t)
	t.Logf("the version is %s", version)
	forEach(t, func(t *testing.T, e *target) {
		status, _, b, err := apiAs(t, e.p).send(t.Context(), http.MethodGet, "/", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if e.p == ordinary && status != http.StatusForbidden {
			t.Errorf("GET / as the ordinary user gave HTTP %d, want 403: %s", status, b)
		}
		if e.p == admin && status != http.StatusOK {
			t.Errorf("GET / as the administrator gave HTTP %d: %s", status, b)
		}
		_, _, err = e.read(t, "SELECT VERSION()")
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.HTTPStatus != http.StatusBadRequest {
			t.Errorf("SELECT VERSION() gave %v, want HTTP 400", err)
		}
		_, h, _ := rawSQL(t, e.p, "/_plugins/_sql", map[string]any{"query": "SELECT 1"})
		got := h.Get("X-Opensearch-Version")
		switch {
		case e.old && got != "":
			t.Errorf("a release of the 2 series sent the header X-OpenSearch-Version %q", got)
		case !e.old && got != "OpenSearch/"+version+" (opensearch)":
			t.Errorf("the header X-OpenSearch-Version is %q, want OpenSearch/%s (opensearch)", got, version)
		}
		if err := e.db.PingContext(t.Context()); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})
}

// TestIntegrationErrors holds that an error before any rows is the error of the
// server, that an error after some rows wraps dbimp.ErrIncomplete after exactly
// those rows (D168), that an error with HTTP 200 reads its status from the body,
// and that a wrong password is HTTP 401.
func TestIntegrationErrors(t *testing.T) {
	rows, lead := rowsIndex(t), leadIndex(t)
	forEach(t, func(t *testing.T, e *target) {
		_, _, err := e.read(t, "SELEC 1")
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.Status != http.StatusBadRequest || oe.Type != "SQLFeatureNotSupportedException" {
			t.Errorf("a syntax error gave %v", err)
		}
		_, _, err = e.read(t, "SELECT n FROM "+prefix+"none")
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.Status != http.StatusNotFound || oe.Type != "IndexNotFoundException" {
			t.Errorf("an unknown index gave %v", err)
		}
		_, _, err = e.read(t, "SELECT nosuch FROM "+rows)
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.Status != http.StatusBadRequest || oe.Type != "SemanticCheckException" {
			t.Errorf("an unknown column gave %v", err)
		}
		_, _, err = e.read(t, "SELECT n FROM "+rows+" WHERE n = 1 UNION SELECT n FROM "+rows+" WHERE n = 2")
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.HTTPStatus != http.StatusOK || oe.Status != http.StatusInternalServerError {
			t.Errorf("a union gave %v, want HTTP 200 with the status 500 in the body", err)
		}
		// With a page size, the row 121 fails on the second page, after 100
		// rows. A principal that cannot read a cursor gets no page size, and the
		// statement fails before any row (recorded: "a cast that fails in a
		// row").
		_, got, err := e.read(t, "SELECT n, CAST(s AS INT) FROM "+lead, opensearch.WithFetchSize(100))
		oe, ok := errors.AsType[*opensearch.Error](err)
		wantRows := 100
		if e.cannotPage() {
			wantRows = 0
		}
		if len(got) != wantRows || !ok || oe.Status != http.StatusBadRequest || oe.Type != "NumberFormatException" || errors.Is(err, dbimp.ErrIncomplete) != (wantRows > 0) {
			t.Errorf("an error after some rows gave %d rows and %v, want %d rows and the error of HTTP 400 for the cast (D168)", len(got), err, wantRows)
		}
		u, err := url.Parse(dsn(t, e.p))
		if err != nil {
			t.Fatal(err)
		}
		u.User = url.UserPassword(u.User.Username(), "wrong")
		wrong, err := sql.Open(opensearch.Name, u.String())
		if err != nil {
			t.Fatal(err)
		}
		defer wrong.Close()
		err = wrong.PingContext(t.Context())
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.HTTPStatus != http.StatusUnauthorized || strings.Contains(err.Error(), "wrong") {
			t.Errorf("a wrong password gave %v, want HTTP 401 with no password in it", err)
		}
	})
}

// numbersOf reads the first column of a result as int64, and fails the test on
// any other value.
func numbersOf(t *testing.T, rows [][]any) []int64 {
	t.Helper()
	out := make([]int64, len(rows))
	for i, r := range rows {
		n, ok := r[0].(int64)
		if !ok {
			t.Fatalf("row %d holds %#v, want an int64", i+1, r[0])
		}
		out[i] = n
	}
	return out
}

// TestIntegrationPages holds D168 against the server: a result of 300 rows in
// pages of every size reads each row once and in order, a LIMIT and a GROUP BY
// have no page size and give their rows, and a page that the result window of
// the index refuses is the error of the server. A principal that cannot read a
// cursor reads the result in one page.
func TestIntegrationPages(t *testing.T) {
	rows := rowsIndex(t)
	forEach(t, func(t *testing.T, e *target) {
		sizes := []int{1, 7, 100, 299, 300, 301, 1000}
		if e.cannotPage() {
			sizes = []int{1000}
		}
		for _, size := range sizes {
			var got [][]any
			var err error
			if e.cannotPage() {
				_, got, err = e.read(t, "SELECT n, s FROM "+rows+" ORDER BY n")
			} else {
				_, got, err = e.read(t, "SELECT n, s FROM "+rows+" ORDER BY n", opensearch.WithFetchSize(size))
			}
			if err != nil || len(got) != 300 {
				t.Fatalf("fetch_size %d: read %d rows and %v, want 300", size, len(got), err)
			}
			for i, row := range got {
				if row[0] != int64(i+1) || row[1] != "row "+strconv.Itoa(i+1) {
					t.Fatalf("fetch_size %d: row %d is %v", size, i+1, row)
				}
			}
		}
		// A statement with LIMIT or GROUP BY has no page size, and gives its rows.
		_, got, err := e.read(t, "SELECT n FROM "+rows+" ORDER BY n LIMIT 150")
		if err != nil || len(got) != 150 || got[149][0] != int64(150) {
			t.Errorf("a limit: read %d rows and %v, want 150", len(got), err)
		}
		_, got, err = e.read(t, "SELECT s, COUNT(*) AS c FROM "+rows+" GROUP BY s")
		if err != nil || len(got) != 300 || got[0][1] != int64(1) {
			t.Errorf("a group by: read %d rows and %v, want 300 groups with a count of 1", len(got), err)
		}
		_, got, err = e.read(t, "SELECT COUNT(*) AS c FROM "+rows)
		if err != nil || len(got) != 1 || got[0][0] != int64(300) {
			t.Errorf("a count: read %v and %v, want 300 as a long", got, err)
		}
		if e.cannotPage() {
			return
		}
		// A page larger than the result window of the index is the error of the
		// server, before any row (recorded: "a fetch size above the window").
		_, _, err = e.read(t, "SELECT n FROM "+rows, opensearch.WithFetchSize(20000))
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || oe.Status != http.StatusBadRequest || !strings.Contains(oe.Details, "Result window is too large") || errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("a page larger than the window gave %v", err)
		}
		// The page size of zero, which WithParameter sends, turns paging off.
		_, got, err = e.read(t, "SELECT n FROM "+rows+" ORDER BY n", opensearch.WithParameter("fetch_size", 0))
		if err != nil || len(got) != 300 {
			t.Errorf("fetch_size 0: read %d rows and %v, want 300", len(got), err)
		}
	})
}

// TestIntegrationCursorsAreClosed holds D168: rows that the caller closes before
// the end close the cursor, so that the cluster holds no point in time that the
// driver left open (recorded: "a cursor that the client leaves").
func TestIntegrationCursorsAreClosed(t *testing.T) {
	rows := rowsIndex(t)
	s := newAdminAPI(t)
	forEach(t, func(t *testing.T, e *target) {
		e.skipCursor(t)
		before := openPITs(t, s)
		for _, read := range []int{1, 100, 101, 150, 299} {
			func() {
				r, err := e.db.QueryContext(e.ctx(t), "SELECT n FROM "+rows+" ORDER BY n", opensearch.WithFetchSize(100))
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				for range read {
					if !r.Next() {
						t.Fatalf("no row: %v", r.Err())
					}
				}
			}()
		}
		// A read to the end holds none either, and a statement that has no
		// cursor holds none.
		if _, _, err := e.read(t, "SELECT n FROM "+rows+" ORDER BY n", opensearch.WithFetchSize(100)); err != nil {
			t.Fatal(err)
		}
		if after := waitPITs(t, s, before); after != before {
			t.Errorf("the cluster holds %d points in time after the rows closed, and held %d before: the driver left a cursor open", after, before)
		}
	})
}

// pitWait is the longest that waitPITs waits. It is far below the keep_alive
// of one minute that the driver asks for, so a point in time that the driver
// left open is still there when the wait ends.
const pitWait = 15 * time.Second

// waitPITs returns the count of points in time as soon as it is at most want,
// or the last count when pitWait ends. The server can answer the close of a
// cursor, or the last page of a statement, before it drops the point in time,
// so a count right after the answer can be one too high.
func waitPITs(t *testing.T, s *api, want int) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), pitWait)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		n := openPITs(t, s)
		if n <= want {
			return n
		}
		select {
		case <-ctx.Done():
			return n
		case <-tick.C:
		}
	}
}

// slowFilter is a filter whose script runs for a few seconds on the index of 300
// rows. The server runs a statement with a filter in the legacy engine.
var slowFilter = map[string]any{"script": map[string]any{"script": map[string]any{
	"source": `double x = doc['n'].value; for (int i = 0; i < 900000; i++) { x = Math.sin(x) + Math.sqrt(i); } return x != 12345.0;`,
}}}

// TestIntegrationCancel holds D168: when the context ends while the server
// works, the driver closes the request, and the error is the error of the
// context. The SQL plugin has no way to stop a statement, so the test measures
// only whether the server goes on, and logs it (docs/OPENSEARCH.md).
func TestIntegrationCancel(t *testing.T) {
	rows := rowsIndex(t)
	s := newAdminAPI(t)
	forEach(t, func(t *testing.T, e *target) {
		if e.old && e.p == ordinary {
			t.Skip("on the 2 series the ordinary user cannot use a filter: it needs indices:admin/aliases/get (recorded: a statement with a filter of the query DSL)")
		}
		// The statement runs to its end once, to learn how long the server needs.
		natural := time.Now()
		if _, err := e.db.ExecContext(t.Context(), "SELECT n FROM "+rows+" ORDER BY s DESC", opensearch.WithParameter("filter", slowFilter)); err != nil {
			t.Fatal(err)
		}
		t.Logf("the statement runs for %v when the client stays", time.Since(natural).Round(100*time.Millisecond))
		ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := e.db.ExecContext(ctx, "SELECT n FROM "+rows+" ORDER BY s DESC", opensearch.WithParameter("filter", slowFilter))
		if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) {
			t.Errorf("a statement whose context ended gave %v, want context.DeadlineExceeded", err)
		}
		if d := time.Since(start); d > 30*time.Second {
			t.Errorf("the statement took %v", d)
		}
		// The driver is ready for the next statement at once.
		if _, _, err := e.read(t, "SELECT n FROM "+rows+" WHERE n < 3 ORDER BY n"); err != nil {
			t.Errorf("a statement after the cancel gave %v", err)
		}
		type tasks struct {
			Nodes map[string]struct {
				Tasks map[string]struct {
					Action    string `json:"action"`
					Cancelled bool   `json:"cancelled"`
				} `json:"tasks"`
			} `json:"nodes"`
		}
		// Whether the server stops the statement is measured, and logged.
		deadline := time.Now().Add(20 * time.Second)
		for {
			var list tasks
			if err := s.getJSON(t.Context(), "/_tasks?actions=*search*&detailed=true", &list); err != nil {
				t.Fatal(err)
			}
			running := 0
			for _, n := range list.Nodes {
				for _, task := range n.Tasks {
					if !task.Cancelled {
						running++
					}
				}
			}
			if running == 0 {
				t.Logf("the search tasks of the server ended within %v of the cancel", time.Since(start).Round(100*time.Millisecond))
				break
			}
			if time.Now().After(deadline) {
				t.Logf("%d search tasks still run 20 seconds after the client left", running)
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
}

// TestIntegrationPrincipals holds what the ordinary user can and cannot do
// (recorded as dbmeta_user): it reads the indices dbmeta* through the driver,
// is refused an index outside them, and writes nothing through the document API.
func TestIntegrationPrincipals(t *testing.T) {
	s := newAdminAPI(t)
	secret := secretPrefix + "secret"
	s.index(t, secret, map[string]any{"settings": settings(), "mappings": map[string]any{"properties": map[string]any{"a": m("integer")}}})
	s.bulk(t, secret, []map[string]any{{"a": 1}})
	forEach(t, func(t *testing.T, e *target) {
		_, got, err := e.read(t, "SELECT a FROM "+secret)
		if e.p == admin {
			if err != nil || len(got) != 1 {
				t.Errorf("the administrator read %v and %v from the index, want one row", got, err)
			}
			return
		}
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || !strings.Contains(oe.Reason, "no permissions for [indices:admin/mappings/get]") {
			t.Errorf("the ordinary user read an index that it cannot read: %v, %v", got, err)
		}
		_, _, err = e.read(t, "SHOW TABLES LIKE '"+secretPrefix+"%'")
		if oe, ok := errors.AsType[*opensearch.Error](err); !ok || !strings.Contains(oe.Reason, "no permissions for [indices:admin/get]") {
			t.Errorf("SHOW TABLES of the ordinary user gave %v, want the refusal for indices:admin/get", err)
		}
		status, b, err := apiAs(t, e.p).do(t.Context(), http.MethodPut, "/"+prefix+"denied/_doc/1", map[string]any{"a": 1})
		if err != nil || status != http.StatusForbidden || !strings.Contains(string(b), "security_exception") {
			t.Errorf("the ordinary user wrote a document: HTTP %d, %v: %.200s, want HTTP 403", status, err, b)
		}
	})
}

// TestIntegrationLiterals holds D168 against the server: the driver writes each
// argument into the statement as a literal, and the server returns the value that
// the caller gave, with the Go type of its column. A string with a quote or a
// backslash is the case that 2.19.6 changed when the server wrote it itself
// (recorded: "a string parameter with a quote").
func TestIntegrationLiterals(t *testing.T) {
	types := typesIndex(t)
	long := strings.Repeat("aé日😀", 1250)
	forEach(t, func(t *testing.T, e *target) {
		for _, tt := range []struct {
			name string
			in   any
			want any
		}{
			{"zero", int64(0), int64(0)},
			{"the smallest long", int64(-9223372036854775808), int64(-9223372036854775808)},
			{"the largest long", int64(9223372036854775807), int64(9223372036854775807)},
			{"an int", 42, int64(42)},
			{"a float", 0.1, 0.1},
			{"the largest float", 1.7976931348623157e+308, 1.7976931348623157e+308},
			{"the smallest float", 5e-324, 5e-324},
			{"a negative float", -0.5, -0.5},
			{"a whole float", 2.0, 2.0},
			{"true", true, true},
			{"false", false, false},
			{"a string", "abc", "abc"},
			{"a string with a quote and a backslash", `it's a\b`, `it's a\b`},
			{"a string with two quotes", "''", "''"},
			{"a string that tries to inject", "x' OR 1=1 --", "x' OR 1=1 --"},
			{"a string with a question mark", "a?b", "a?b"},
			{"a string with a comment", "a -- b /* c */", "a -- b /* c */"},
			{"text outside ASCII", "é日😀", "é日😀"},
			{"a long string", long, long},
			{"a time", time.Date(2026, 10, 1, 12, 34, 56, 123456789, time.UTC), time.Date(2026, 10, 1, 12, 34, 56, 123456789, time.UTC)},
			{"a time with an offset", time.Date(2026, 10, 1, 12, 34, 56, 0, time.FixedZone("x", 5*3600+1800)), time.Date(2026, 10, 1, 7, 4, 56, 0, time.UTC)},
			{"a date", dbimp.Date{Year: 1, Month: 1, Day: 1}, dbimp.Date{Year: 1, Month: 1, Day: 1}},
			{"the last date", dbimp.Date{Year: 9999, Month: 12, Day: 31}, dbimp.Date{Year: 9999, Month: 12, Day: 31}},
			{"a time of day", dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}, dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}},
		} {
			// The empty string is no literal that the server keeps as a column
			// name, so each value has an alias.
			var got any
			err := e.db.QueryRowContext(e.ctx(t), "SELECT ? AS v", tt.in).Scan(&got)
			if err != nil {
				t.Errorf("%s: %v", tt.name, err)
				continue
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: the value is %#v, want %#v", tt.name, got, tt.want)
			}
		}
		var got any
		if err := e.db.QueryRowContext(e.ctx(t), "SELECT ? AS v", nil).Scan(&got); err != nil || got != nil {
			t.Errorf("NULL: the value is %#v and %v, want nil", got, err)
		}
		// Several arguments, each with an alias, keep their order.
		_, rows, err := e.read(t, "SELECT ? AS a, ? AS b, ? AS c", "x", int64(2), 3.5)
		if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], []any{"x", int64(2), 3.5}) {
			t.Errorf("three arguments gave %v and %v", rows, err)
		}
		// A string with a quote and a backslash finds the row that holds it, and
		// one that tries to inject finds none (recorded: "a string parameter that
		// tries to inject").
		_, rows, err = e.read(t, "SELECT id FROM "+types+" WHERE k = ?", "é'\"\\ x")
		if err != nil || len(rows) != 1 || rows[0][0] != int64(1) {
			t.Errorf("a string with a quote and a backslash found %v and %v, want the row 1", rows, err)
		}
		_, rows, err = e.read(t, "SELECT id FROM "+types+" WHERE k = ? OR id = 3", "x' OR 1=1 --")
		if err != nil || len(rows) != 1 || rows[0][0] != int64(3) {
			t.Errorf("a string that tries to inject found %v and %v, want the row 3 only", rows, err)
		}
		_, rows, err = e.read(t, "SELECT id FROM "+types+" WHERE id > ? AND id < ? ORDER BY id", int64(1), 4)
		if err != nil || !slices.Equal(numbersOf(t, rows), []int64{2, 3}) {
			t.Errorf("two numbers found %v and %v, want the rows 2 and 3", rows, err)
		}
	})
}
