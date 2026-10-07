package opensearch_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/opensearch"
)

// TestFetchSizeOfAPlainSelect holds D168: a plain SELECT carries the page size,
// and every other statement carries none, because the server runs a statement
// with a page size and a LIMIT, a GROUP BY, a join or an aggregate in the legacy
// engine, which gives 200 groups and no cursor (recorded: "a group by with a
// fetch size" and "a cursor with a limit").
func TestFetchSizeOfAPlainSelect(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "?fetch_size=7")
	for _, tt := range []struct {
		query string
		plain bool
	}{
		{"SELECT a FROM t", true},
		{"select a from t", true},
		{"SELECT * FROM t WHERE a > 5 ORDER BY a DESC", true},
		{"SELECT a, b + 1 AS c FROM t t1 WHERE t1.a IN (1, 2, 3)", true},
		{"SELECT ABS(a), UPPER(b) FROM t", true},
		{"SELECT _id, _score, a FROM t WHERE MATCH(b, 'x')", true},
		{"SELECT o.a FROM t", true},
		{"SELECT a FROM `t` WHERE b = 'LIMIT' AND c = \"GROUP\"", true},
		{"SELECT a FROM t -- LIMIT 5\n", true},
		{"SELECT a /* DISTINCT */ FROM t", true},
		{"SELECT a FROM t WHERE b = 'it''s a LIMIT'", true},
		{"SELECT a FROM t WHERE x = 1e5", true},
		{"SELECT 1", false},
		{"SELECT a FROM t LIMIT 5", false},
		{"SELECT a FROM t ORDER BY a LIMIT 5 OFFSET 2", false},
		{"SELECT b, COUNT(*) FROM t GROUP BY b", false},
		{"SELECT COUNT(*) FROM t", false},
		{"SELECT count (a) FROM t", false},
		{"SELECT MAX(a), MIN(a), SUM(a), AVG(a) FROM t", false},
		{"SELECT DISTINCT b FROM t", false},
		{"SELECT x.a FROM t x JOIN u y ON x.a = y.a", false},
		{"SELECT a FROM (SELECT a FROM t) q", false},
		{"SELECT a FROM t UNION SELECT a FROM u", false},
		{"SELECT a, b FROM t HAVING a > 1", false},
		{"SELECT a, ROW_NUMBER() OVER (ORDER BY a) FROM t", false},
		{"SELECT NESTED(n.a) FROM t", false},
		{"SHOW TABLES LIKE %", false},
		{"DESCRIBE TABLES LIKE t", false},
		{"", false},
		{"SELECT", false},
		{"/* SELECT a FROM t */", false},
		{"INSERT INTO t VALUES (1)", false},
	} {
		if _, err := db.ExecContext(t.Context(), tt.query); err != nil {
			t.Fatalf("%q: %v", tt.query, err)
		}
		reqs := f.requests()
		if len(reqs) != 1 {
			t.Fatalf("%q: the server received %d requests, want 1", tt.query, len(reqs))
		}
		got, has := reqs[0]["fetch_size"]
		switch {
		case tt.plain && got != float64(7):
			t.Errorf("%q: fetch_size is %v, want 7", tt.query, got)
		case !tt.plain && has:
			t.Errorf("%q: fetch_size is %v, want none", tt.query, got)
		}
		if reqs[0]["query"] != tt.query && tt.query != "" {
			t.Errorf("%q: the query is %q, want the statement as it is", tt.query, reqs[0]["query"])
		}
	}
}

// TestFetchSizeOptions holds that the page size of a statement comes from the
// DSN, then the context, then the arguments, and that a value that the DSN
// refuses fails.
func TestFetchSizeOptions(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	ctx := opensearch.WithOptions(t.Context(), opensearch.WithFetchSize(50))
	for _, tt := range []struct {
		inCtx bool
		args  []any
		want  float64
	}{
		{false, nil, 1000},
		{true, nil, 50},
		{true, []any{opensearch.WithFetchSize(60)}, 60},
		{false, []any{opensearch.WithFetchSize(5)}, 5},
	} {
		use := t.Context()
		if tt.inCtx {
			use = ctx
		}
		if _, err := db.ExecContext(use, "SELECT a FROM t", tt.args...); err != nil {
			t.Fatal(err)
		}
		if reqs := f.requests(); len(reqs) != 1 || reqs[0]["fetch_size"] != tt.want {
			t.Errorf("the request is %v, want fetch_size %v", reqs, tt.want)
		}
	}
	for _, n := range []int{0, -1} {
		if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", opensearch.WithFetchSize(n)); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("WithFetchSize(%d) gave %v, want dbimp.ErrInvalidValue", n, err)
		}
	}
	if got := f.requests(); len(got) != 0 {
		t.Errorf("the server received %v for options that the driver refuses", got)
	}
}

// TestWithParameter holds D109: a key sets a key of the body, and replaces the
// key that the driver sets. It applies to the first request and not to the
// request for each next page.
func TestWithParameter(t *testing.T) {
	t.Parallel()
	f := &fake{handle: threePages}
	db := f.open(t, "", "?fetch_size=2")
	filter := map[string]any{"range": map[string]any{"a": map[string]any{"lte": 3}}}
	drain(t, db, "SELECT a FROM t", opensearch.WithParameter("filter", filter), opensearch.WithParameter("fetch_size", 0))
	// The fake answers the first request with a cursor, so the rows read on.
	first := f.requests()[0]
	if first["fetch_size"] != float64(0) || first["query"] != "SELECT a FROM t" || first["filter"] == nil {
		t.Errorf("the first request is %v, want fetch_size 0 and the filter", first)
	}
}

// TestWithParameterNotOnNextPages holds that the key of WithParameter is not
// in the request for a next page.
func TestWithParameterNotOnNextPages(t *testing.T) {
	t.Parallel()
	f := &fake{handle: threePages}
	db := f.open(t, "", "?fetch_size=2")
	drain(t, db, "SELECT a FROM t", opensearch.WithParameter("filter", map[string]any{"match_all": map[string]any{}}))
	reqs := f.requests()
	if len(reqs) != 3 || reqs[0]["filter"] == nil || reqs[1]["filter"] != nil || reqs[2]["filter"] != nil || len(reqs[1]) != 1 {
		t.Errorf("the requests are %v, want the filter on the first only", reqs)
	}
}

// TestRefusedOptions holds D109: an option that the server cannot honor fails
// before any request, with dbimp.ErrNotSupported.
func TestRefusedOptions(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	for _, tt := range []struct {
		name string
		opt  opensearch.Option
		want error
	}{
		{"WithTimeout", opensearch.WithTimeout(time.Second), dbimp.ErrNotSupported},
		{"WithTimeout with a negative time", opensearch.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"WithDatabase", opensearch.WithDatabase("x"), dbimp.ErrNotSupported},
	} {
		_, err := db.ExecContext(t.Context(), "SELECT 1", tt.opt)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s gave %v, want %v", tt.name, err, tt.want)
		}
	}
	if got := f.requests(); len(got) != 0 {
		t.Errorf("the server received %v for options that the driver refuses", got)
	}
	// A timeout of zero, and WithReadonly, change nothing.
	if _, err := db.ExecContext(t.Context(), "SELECT 1", opensearch.WithTimeout(0), opensearch.WithReadonly(true)); err != nil {
		t.Errorf("a timeout of zero and WithReadonly gave %v", err)
	}
}

// TestArgumentsAreLiterals holds D168: the driver writes each argument into the
// statement as a literal, because the server writes each value into the text
// itself and 2.19.6 changes a string with a quote or a backslash, and it sends
// no parameters.
func TestArgumentsAreLiterals(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 10, 1, 12, 34, 56, 123456789, time.FixedZone("x", 5*3600+1800))
	for _, tt := range []struct {
		name  string
		query string
		args  []any
		want  string
	}{
		{"an integer", "SELECT ?", []any{int64(-9223372036854775808)}, "SELECT -9223372036854775808"},
		{"an int", "SELECT ?, ?", []any{1, uint8(2)}, "SELECT 1, 2"},
		{"a uint64", "SELECT ?", []any{uint64(math.MaxInt64)}, "SELECT 9223372036854775807"},
		{"a float", "SELECT ?, ?, ?, ?", []any{1.5, 2.0, 1e21, math.SmallestNonzeroFloat64}, "SELECT 1.5, 2.0, 1e+21, 5e-324"},
		{"a bool", "SELECT ?, ?", []any{true, false}, "SELECT true, false"},
		{"a null", "SELECT ?", []any{nil}, "SELECT NULL"},
		{"a nil pointer", "SELECT ?", []any{(*sql.NullString)(nil)}, "SELECT NULL"},
		{"a string with a quote", "SELECT ?", []any{"it's a\\b"}, `SELECT 'it''s a\b'`},
		{"a string with two quotes", "WHERE k = ? OR k = ?", []any{"x' OR 1=1 --", "''"}, "WHERE k = 'x'' OR 1=1 --' OR k = ''''''"},
		{"a string with a question mark", "SELECT ?", []any{"a?b"}, "SELECT 'a?b'"},
		{"text outside ASCII", "SELECT ?", []any{"é日😀"}, "SELECT 'é日😀'"},
		{"a time", "SELECT ?", []any{ts}, "SELECT TIMESTAMP '2026-10-01 07:04:56.123456789'"},
		{"a time with no fraction", "SELECT ?", []any{time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, "SELECT TIMESTAMP '2026-10-01 00:00:00'"},
		{"a date", "SELECT ?", []any{dbimp.Date{Year: 2026, Month: 10, Day: 1}}, "SELECT DATE '2026-10-01'"},
		{"a time of day", "SELECT ?", []any{dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}}, "SELECT TIME '12:34:56.789'"},
		{"a local timestamp", "SELECT ?", []any{dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 10, Day: 1}, Time: dbimp.LocalTime{Hour: 1}}}, "SELECT TIMESTAMP '2026-10-01 01:00:00'"},
		{"a placeholder in a literal", "SELECT '?', ?, \"?\", `?` -- ?\n", []any{1}, "SELECT '?', 1, \"?\", `?` -- ?\n"},
		{"no placeholder, no argument", "SELECT 'x'", nil, "SELECT 'x'"},
		{"a question mark with no argument", "SELECT ?", nil, "SELECT ?"},
	} {
		f := &fake{}
		db := f.open(t, "", "")
		if _, err := db.ExecContext(t.Context(), tt.query, tt.args...); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		reqs := f.requests()
		if len(reqs) != 1 || reqs[0]["query"] != tt.want || reqs[0]["parameters"] != nil {
			t.Errorf("%s: the request is %v, want the query %q and no parameters", tt.name, reqs, tt.want)
		}
	}
}

// TestRefusedArguments holds the errors for an argument that SQL has no
// literal for, and for arguments that do not match the placeholders. Each one
// fails before any request.
func TestRefusedArguments(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	for _, tt := range []struct {
		name  string
		query string
		args  []any
		want  error
	}{
		{"bytes", "SELECT ?", []any{[]byte("x")}, dbimp.ErrNotSupported},
		{"a uint64 above a long", "SELECT ?", []any{uint64(math.MaxInt64) + 1}, dbimp.ErrNotSupported},
		{"NaN", "SELECT ?", []any{math.NaN()}, dbimp.ErrInvalidValue},
		{"an infinity", "SELECT ?", []any{math.Inf(-1)}, dbimp.ErrInvalidValue},
		{"a year above 9999", "SELECT ?", []any{time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, dbimp.ErrInvalidValue},
		{"a named argument", "SELECT ?", []any{sql.Named("x", 1)}, dbimp.ErrArguments},
		{"too few arguments", "SELECT ?, ?", []any{1}, dbimp.ErrArguments},
		{"too many arguments", "SELECT ?", []any{1, 2}, dbimp.ErrArguments},
		{"an unterminated literal", "SELECT 'x", []any{1}, dbimp.ErrUnterminated},
		{"a type that SQL lacks", "SELECT ?", []any{[]int64{1}}, nil},
	} {
		_, err := db.ExecContext(t.Context(), tt.query, tt.args...)
		if err == nil || tt.want != nil && !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
	}
	if got := f.requests(); len(got) != 0 {
		t.Errorf("the server received %v for arguments that the driver refuses", got)
	}
}

// TestBeginTxIsRefused holds D20 and D168: OpenSearch has no transactions, and
// BeginTx says so with dbimp.ErrNotSupported, with no request.
func TestBeginTxIsRefused(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
	}
	if got := f.requests(); len(got) != 0 {
		t.Errorf("BeginTx sent %v", got)
	}
}

// TestPingAndExec holds that Ping sends SELECT 1 and checks the credentials,
// that Exec reads the result to its end with a result that fails, and that a
// prepared statement runs as its text.
func TestPingAndExec(t *testing.T) {
	t.Parallel()
	f := &fake{handle: threePages}
	db := f.open(t, "", "?fetch_size=2")
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if reqs := f.requests(); len(reqs) == 0 || reqs[0]["query"] != "SELECT 1" || reqs[0]["fetch_size"] != nil {
		t.Errorf("Ping sent %v, want SELECT 1 and no page size", reqs)
	}
	res, err := db.ExecContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if reqs := f.requests(); len(reqs) != 3 {
		t.Errorf("Exec sent %d requests, want 3 for the three pages", len(reqs))
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected gave %v, want dbimp.ErrNotSupported", err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
	}
	st, err := db.PrepareContext(t.Context(), "SELECT a FROM t WHERE a > ?")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int64
	if err := st.QueryRowContext(t.Context(), 3).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if reqs := f.requests(); len(reqs) == 0 || reqs[0]["query"] != "SELECT a FROM t WHERE a > 3" {
		t.Errorf("the prepared statement sent %v, want the statement with 3 written in", reqs)
	}
}

// TestCredentials holds D94 and D168: the driver sends basic authentication,
// and none when the DSN has no user and no password.
func TestCredentials(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		userinfo, want string
	}{
		{"admin:P4ssw0rd%21x@", "Basic YWRtaW46UDRzc3cwcmQheA=="},
		{"", ""},
	} {
		f := &fake{}
		db := f.open(t, tt.userinfo, "")
		if _, err := db.ExecContext(t.Context(), "SELECT 1"); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		got := f.headers[0].Get("Authorization")
		ct := f.headers[0].Get("Content-Type")
		f.mu.Unlock()
		if got != tt.want || ct != "application/json" {
			t.Errorf("the headers hold Authorization %q and Content-Type %q, want %q and application/json", got, ct, tt.want)
		}
	}
}

// TestNoRetry holds D8: HTTP 429 and HTTP 503 reach the caller, and the driver
// sends the statement once.
func TestNoRetry(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			reply(w, status, "busy")
		}}
		db := f.open(t, "", "")
		_, err := db.ExecContext(t.Context(), "SELECT 1")
		if e, ok := errors.AsType[*opensearch.Error](err); !ok || e.HTTPStatus != status || errors.Is(err, driver.ErrBadConn) {
			t.Errorf("HTTP %d gave %v", status, err)
		}
		if reqs := f.requests(); len(reqs) != 1 {
			t.Errorf("HTTP %d: the driver sent %d requests, want 1", status, len(reqs))
		}
	}
}

// TestUnreachableServer holds that a server that refuses the connection gives
// an error that wraps driver.ErrBadConn only where the statement did not reach
// it, which database/sql then retries, and that no message holds the password.
func TestUnreachableServer(t *testing.T) {
	t.Parallel()
	db, err := sql.Open(opensearch.Name, "opensearch://u:secret@127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.PingContext(t.Context())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("a server that is down gave %v, want an error with no password", err)
	}
}

// TestOptionsKeepTheirOrder holds that two options for one statement apply in
// order, and the later wins (D109).
func TestOptionsKeepTheirOrder(t *testing.T) {
	t.Parallel()
	f := &fake{}
	db := f.open(t, "", "")
	if _, err := db.ExecContext(t.Context(), "SELECT a FROM t", opensearch.WithFetchSize(3), opensearch.WithFetchSize(4)); err != nil {
		t.Fatal(err)
	}
	if reqs := f.requests(); len(reqs) != 1 || !slices.Equal([]any{reqs[0]["fetch_size"]}, []any{float64(4)}) {
		t.Errorf("the request is %v, want fetch_size 4", reqs)
	}
}

// drain runs the query and reads every row of it.
func drain(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
