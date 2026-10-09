package snowflake //nolint:testpackage // The tests point a connector at a fake server, and read a statement of its options, which only the package can do.

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json/jsontext"
	"errors"
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

// These tests replay the exchanges that step 6 recorded from a real account,
// under testdata/snowflake/. Each one decodes a real answer through the
// driver. The recordings hold the placeholders <role>, <user>, <locator> and
// <account>, so the tests build the driver with a Config whose role is <role>,
// and the other settings of the recorder, so that each request body matches the
// recorded one. The fake server answers by the statement and its bindings, and
// never by the handle.

const testdata = "../testdata/snowflake"

// recordedConfig is the configuration that the recorder used (step 6).
func recordedConfig() Config {
	cfg := config()
	cfg.Role, cfg.Warehouse, cfg.Database, cfg.Schema = "<role>", "DBMETA_WH", "DBMETA", "PUBLIC"
	cfg.Timeout = time.Minute
	return cfg
}

// exchanges holds every recorded exchange, read once.
var exchanges = sync.OnceValues(func() ([]*dbimptest.Exchange, error) {
	paths, err := filepath.Glob(filepath.Join(testdata, "snowflake-*.json"))
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

// replayServer answers each request with a recorded exchange.
type replayServer struct {
	t *testing.T
	// only, when it is not 0, limits the answers to the exchanges with that
	// status. Otherwise the answer of HTTP 401 is left out, because the request
	// of a ping matches the request that a wrong token made.
	only int
	// gzip answers a partition with gzip, as the server does (measured).
	gzip bool

	mu  sync.Mutex
	log []string
	// inflight and most count the requests that run at once.
	inflight, most int
}

func (s *replayServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.inflight++
	s.most = max(s.most, s.inflight)
	entry := r.Method + " " + r.URL.Path
	if p := r.URL.Query().Get("partition"); p != "" {
		entry += "?partition=" + p
	}
	s.log = append(s.log, entry)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inflight--
		s.mu.Unlock()
	}()
	if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
		s.t.Errorf("a request to the fake server carries no Bearer token")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("reading a request to the fake server: %v", err)
		return
	}
	all, err := exchanges()
	if err != nil {
		s.t.Errorf("reading the recorded exchanges: %v", err)
		return
	}
	i := slices.IndexFunc(all, func(ex *dbimptest.Exchange) bool { return s.matches(r, body, ex) })
	if i < 0 && strings.HasSuffix(r.URL.Path, "/cancel") {
		// The rows of a statement that the test closes early cancel it, and the
		// recordings hold one cancel. The server answers the same to a cancel of
		// a statement that is gone (measured).
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
	content := res.Content()
	// The recorder kept the body that the client read, after gzip, except for
	// the two exchanges that asked for gzip themselves.
	if !bytes.HasPrefix(content, []byte{0x1f, 0x8b}) {
		w.Header().Del("Content-Encoding")
	}
	if s.gzip && r.URL.Query().Has("partition") && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write(content)
		_ = zw.Close()
		content = buf.Bytes()
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.WriteHeader(res.Status)
	if _, err := w.Write(content); err != nil {
		s.t.Errorf("writing a response from the fake server: %v", err)
	}
}

// requests returns the method and the path of each request that the server
// got, with the query of a request for a partition.
func (s *replayServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.log)
}

// matches reports whether the request is the request of ex. The query async is
// the one that the driver always sends, so only the query partition counts.
func (s *replayServer) matches(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	switch {
	case ex.Request.Method != r.Method || ex.Request.Path != r.URL.Path:
		return false
	case s.only != 0 && ex.Response.Status != s.only:
		return false
	case s.only == 0 && ex.Response.Status == http.StatusUnauthorized:
		return false
	}
	recorded, err := url.ParseQuery(ex.Request.Query)
	if err != nil || recorded.Get("partition") != r.URL.Query().Get("partition") {
		return false
	}
	return sameJSON(body, ex.Request.Content())
}

// replay opens the driver against the recorded exchanges, with the
// configuration of the recorder.
func replay(t *testing.T) (*sql.DB, *replayServer) {
	t.Helper()
	return replayWith(t, &replayServer{t: t, gzip: true})
}

// replayWith opens the driver against s.
func replayWith(t *testing.T, s *replayServer) (*sql.DB, *replayServer) {
	t.Helper()
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return open(t, recordedConfig(), srv.URL, true), s
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	return readAllContext(t, t.Context(), db, query, args...)
}

// readAllContext is readAll with a context.
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

// serverError returns err as a *Error, and fails the test if it is not one.
func serverError(t *testing.T, err error) *Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *Error", err, err)
	}
	return e
}

// dec makes a decimal from its text.
func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("reading %q as a decimal: %v", s, err)
	}
	return d
}

// equal reports whether two values are the same: a decimal by its text, and a
// time by its instant and its offset.
func equal(a, b any) bool {
	switch x := a.(type) {
	case *apd.Decimal:
		y, ok := b.(*apd.Decimal)
		return ok && x.String() == y.String()
	case time.Time:
		y, ok := b.(time.Time)
		if !ok {
			return false
		}
		_, ox := x.Zone()
		_, oy := y.Zone()
		return x.Equal(y) && ox == oy
	case []any:
		y, ok := b.([]any)
		return ok && slices.EqualFunc(x, y, equal)
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if w, ok := y[k]; !ok || !equal(v, w) {
				return false
			}
		}
		return true
	case []byte:
		y, ok := b.([]byte)
		return ok && x != nil == (y != nil) && bytes.Equal(x, y)
	case dbimp.Vector[float64]:
		y, ok := b.(dbimp.Vector[float64])
		return ok && slices.Equal(x, y)
	}
	return a == b
}

// checkRows fails the test for each row that differs from want.
func checkRows(t *testing.T, got, want [][]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if !slices.EqualFunc(got[i], want[i], equal) {
			t.Errorf("row %d is\n%#v\nwant\n%#v", i, got[i], want[i])
		}
	}
}

func TestReplaySelect(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	cols, got, err := readAll(t, db, "SELECT 1 AS a, 'x' AS b")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cols, []string{"A", "B"}) {
		t.Errorf("the columns are %q, want A and B", cols)
	}
	checkRows(t, got, [][]any{{int64(1), "x"}})
	rows, err := db.QueryContext(t.Context(), "SELECT 1 AS a, 'x' AS b")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n, ok := cts[0].DatabaseTypeName(), true; n != "FIXED" || !ok {
		t.Errorf("the database type of A is %q, want FIXED", n)
	}
	if p, s, ok := cts[0].DecimalSize(); !ok || p != 1 || s != 0 {
		t.Errorf("the size of A is %d, %d, %v, want 1, 0, true", p, s, ok)
	}
	if l, ok := cts[1].Length(); !ok || l != 1 {
		t.Errorf("the length of B is %d, %v, want 1, true", l, ok)
	}
	if n, ok := cts[0].Nullable(); !ok || n {
		t.Errorf("A can be NULL: %v, %v, want false, true", n, ok)
	}
	if s := cts[0].ScanType().String(); s != "int64" {
		t.Errorf("the scan type of A is %s, want int64", s)
	}
}

// TestReplayColumns holds D18: the columns come from the metadata, with the
// order of the statement, with no rows, with a repeated name, and with a name
// in lower case (measured).
func TestReplayColumns(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, tt := range []struct {
		query string
		want  []string
		rows  int
	}{
		{"SELECT * FROM dbimp_it_types WHERE 1 = 0", strings.Fields("I N F S B BO D TM TN TL TZ V O A G GM"), 0},
		{"SELECT 1 AS a, 2 AS a", []string{"A", "A"}, 1},
		{`SELECT 1 AS "lower"`, []string{"lower"}, 1},
	} {
		cols, got, err := readAll(t, db, tt.query)
		if err != nil {
			t.Fatalf("%s: %v", tt.query, err)
		}
		if !slices.Equal(cols, tt.want) || len(got) != tt.rows {
			t.Errorf("%s: columns %q and %d rows, want %q and %d rows", tt.query, cols, len(got), tt.want, tt.rows)
		}
	}
}

// TestReplayEveryType holds D183 for each type, as the table of every type
// stored it: a fixed of scale 0 and precision 18 or less is an int64 and any
// other is a decimal, and every other type is the Go type of the type table.
func TestReplayEveryType(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	query := "SELECT * FROM dbimp_it_types ORDER BY i NULLS LAST"
	cols, got, err := readAll(t, db, query)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Fields("I N F S B BO D TM TN TL TZ V O A G GM"); !slices.Equal(cols, want) {
		t.Fatalf("the columns are %q, want %q", cols, want)
	}
	local := time.Unix(1791549296, 123456789).UTC()
	want := [][]any{
		{
			dec(t, "12345678901234567890123456789012345678"), dec(t, "12345678.91"), 1.5, "héllo", []byte{0xDE, 0xAD, 0xBE, 0xEF}, true,
			dbimp.Date{Year: 2026, Month: time.October, Day: 9},
			dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789},
			dbimp.LocalDateTimeOf(local),
			time.Unix(1791524096, 123456789).In(time.Local),
			time.Unix(1791524096, 123456789).In(time.FixedZone("", 7*3600)),
			map[string]any{"a": int64(1)},
			map[string]any{"k": "v"},
			[]any{int64(1), "x", nil},
			map[string]any{"coordinates": []any{int64(1), int64(2)}, "type": "Point"},
			map[string]any{"coordinates": []any{3.0, 4.0}, "type": "Point"},
		},
		make([]any, 16),
	}
	checkRows(t, got, want)
	// Every type has the Go type of its column type, and a NULL is nil.
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantTypes := strings.Fields("FIXED FIXED REAL TEXT BINARY BOOLEAN DATE TIME TIMESTAMP_NTZ TIMESTAMP_LTZ TIMESTAMP_TZ VARIANT OBJECT ARRAY OBJECT OBJECT")
	for i, ct := range cts {
		if ct.DatabaseTypeName() != wantTypes[i] {
			t.Errorf("the database type of %s is %s, want %s", ct.Name(), ct.DatabaseTypeName(), wantTypes[i])
		}
		if v := want[0][i]; v != nil && ct.ScanType() != nil && wantTypes[i] != "VARIANT" {
			if got := typeName(v); got != ct.ScanType().String() {
				t.Errorf("the scan type of %s is %s, and its value is %s (D135)", ct.Name(), ct.ScanType(), got)
			}
		}
	}
}

// typeName returns the type of v as ColumnTypeScanType writes it.
func typeName(v any) string {
	return reflect.TypeOf(v).String()
}

// TestReplayScan holds that the real path of a caller works: each type scans
// into the destination that fits it, and a NULL into sql.Null.
func TestReplayScan(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM dbimp_it_types ORDER BY i NULLS LAST")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	var (
		i          apd.Decimal
		n          string
		f          float64
		s          string
		b          []byte
		bo         bool
		d          dbimp.Date
		tm         dbimp.LocalTime
		tn         dbimp.LocalDateTime
		tl, tz     time.Time
		v, o, a, g any
		gm         map[string]any
	)
	// A column that holds an object scans into a *any, and the *any holds a
	// map, so the test scans the geometry through one.
	var gmAny any
	if err := rows.Scan(&i, &n, &f, &s, &b, &bo, &d, &tm, &tn, &tl, &tz, &v, &o, &a, &g, &gmAny); err != nil {
		t.Fatal(err)
	}
	if i.String() != "12345678901234567890123456789012345678" || n != "12345678.91" || f != 1.5 || s != "héllo" || !bytes.Equal(b, []byte{0xDE, 0xAD, 0xBE, 0xEF}) || !bo {
		t.Errorf("scanned %s, %s, %v, %s, %x, %v", i.String(), n, f, s, b, bo)
	}
	if d.String() != "2026-10-09" || tm.String() != "12:34:56.123456789" || tn.String() != "2026-10-09T12:34:56.123456789" {
		t.Errorf("scanned %s, %s, %s", d, tm, tn)
	}
	if !tl.Equal(time.Unix(1791524096, 123456789)) || !tz.Equal(tl) {
		t.Errorf("scanned %v and %v, want the same instant", tl, tz)
	}
	var ok bool
	if gm, ok = gmAny.(map[string]any); !ok || gm["type"] != "Point" {
		t.Errorf("scanned the geometry %v, want a map of GeoJSON", gmAny)
	}
	for name, val := range map[string]any{"variant": v, "object": o, "array": a, "geography": g} {
		if val == nil {
			t.Errorf("scanned a NULL for the %s", name)
		}
	}
	if !rows.Next() {
		t.Fatalf("no second row: %v", rows.Err())
	}
	var (
		nd sql.Null[apd.Decimal]
		ns sql.Null[string]
		nt sql.Null[time.Time]
		nb sql.Null[bool]
	)
	dests := make([]any, 16)
	for k := range dests {
		dests[k] = new(any)
	}
	dests[0], dests[3], dests[9], dests[5] = &nd, &ns, &nt, &nb
	if err := rows.Scan(dests...); err != nil {
		t.Fatal(err)
	}
	if nd.Valid || ns.Valid || nt.Valid || nb.Valid {
		t.Errorf("a NULL scanned as valid: %v %v %v %v", nd.Valid, ns.Valid, nt.Valid, nb.Valid)
	}
}

// TestReplayNumbers holds the values that the recordings name: a decimal of
// 38 digits, an integer beyond int64, the special values of a float, a date
// before 1970, a scale, a variant of each kind, and a vector (D183).
func TestReplayNumbers(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, tt := range []struct {
		query string
		want  []any
	}{
		{"SELECT 1.2345678901234567890123456789012345678::NUMBER(38,37)", []any{dec(t, "1.2345678901234567890123456789012345678")}},
		{"SELECT 9223372036854775808::NUMBER(38,0)", []any{dec(t, "9223372036854775808")}},
		{"SELECT 12.50::NUMBER(10,4)", []any{dec(t, "12.5000")}},
		{"SELECT '1900-01-01'::DATE", []any{dbimp.Date{Year: 1900, Month: time.January, Day: 1}}},
		{"SELECT TIMESTAMPDIFF('second', '2026-01-01'::TIMESTAMP_NTZ, '2026-01-02'::TIMESTAMP_NTZ)", []any{int64(86400)}},
		{`SELECT PARSE_JSON('1'), PARSE_JSON('"s"'), PARSE_JSON('null'), PARSE_JSON('[1,{"a":2}]'), PARSE_JSON('{"a":[1,2]}')`, []any{
			int64(1), "s", nil, []any{int64(1), map[string]any{"a": int64(2)}}, map[string]any{"a": []any{int64(1), int64(2)}},
		}},
		{"SELECT [1.0,2.0,3.0]::VECTOR(FLOAT,3)", []any{dbimp.Vector[float64]{1, 2, 3}}},
	} {
		_, got, err := readAll(t, db, tt.query)
		if err != nil {
			t.Fatalf("%s: %v", tt.query, err)
		}
		checkRows(t, got, [][]any{tt.want})
	}
	_, got, err := readAll(t, db, "SELECT 'NaN'::FLOAT, 'inf'::FLOAT, '-inf'::FLOAT")
	if err != nil {
		t.Fatal(err)
	}
	nan, ok := got[0][0].(float64)
	if !ok || !math.IsNaN(nan) || got[0][1] != any(math.Inf(1)) || got[0][2] != any(math.Inf(-1)) {
		t.Errorf("the special values are %v, want NaN, +Inf and -Inf", got[0])
	}
}

// TestReplayTimeZone holds that a timestamp_ltz value takes the time zone of
// the statement, and time.Local when none is named, and the parameter TIMEZONE
// goes in the body (measured).
func TestReplayTimeZone(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	// The recorded statements match the body that the recorder sent, which
	// names no zone, so the value takes time.Local. The tests of the options
	// name each zone.
	_, got, err := readAll(t, db, "SELECT CURRENT_TIMESTAMP()::TIMESTAMP_LTZ, CURRENT_TIMESTAMP()::STRING")
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := got[0][0].(time.Time)
	if !ok || !ts.Equal(time.Unix(1791499232, 43000000)) || ts.Location() != time.Local {
		t.Errorf("the value is %v, want the instant 1791499232.043 in time.Local", got[0][0])
	}
	// The parameter of the statement, which the DSN names with timezone.
	_, got, err = readAll(t, db, "SELECT CURRENT_TIMESTAMP()::STRING", WithTimeZone("UTC"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows", len(got))
	}
}

// TestReplayErrors holds D183: a failed statement is an *Error with its code,
// its SQLSTATE and its message, before any row.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, tt := range []struct {
		query    string
		opts     []any
		status   int
		code     string
		sqlState string
		is       error
	}{
		{query: "SELEC 1", status: 422, code: "001003", sqlState: "42000"},
		{query: "SELECT * FROM dbimp_it_nosuch", status: 422, code: "002003", sqlState: "42S02"},
		{query: "SELECT 1/0", status: 422, code: "100051", sqlState: "22012"},
		{query: "SELECT 'abc'::NUMBER", status: 422, code: "100038"},
		{query: "SELECT 1/(n-5000) FROM (SELECT seq4() AS n FROM TABLE(GENERATOR(ROWCOUNT => 20000))) ORDER BY n", status: 422, code: "100051"},
		{query: "SELECT 1", opts: []any{WithRole("ACCOUNTADMIN")}, status: 400, code: "390186"},
		{query: "SELECT SYSTEM$WAIT(10)", opts: []any{WithTimeout(2 * time.Second)}, status: 408, code: "000630", is: ErrTimeout},
	} {
		_, _, err := readAll(t, db, tt.query, tt.opts...)
		if err == nil {
			t.Errorf("%s: no error", tt.query)
			continue
		}
		e := serverError(t, err)
		if e.HTTPStatus != tt.status || e.Code != tt.code || (tt.sqlState != "" && e.SQLState != tt.sqlState) || e.Message == "" {
			t.Errorf("%s: the error is %+v, want HTTP %d, code %s and SQLSTATE %s", tt.query, *e, tt.status, tt.code, tt.sqlState)
		}
		if tt.is != nil && !errors.Is(err, tt.is) {
			t.Errorf("%s: the error does not wrap %v", tt.query, tt.is)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete (D107)", tt.query)
		}
		var se *dbimp.StatusError
		if !errors.As(err, &se) || se.Code != tt.status {
			t.Errorf("%s: the error does not unwrap to a *dbimp.StatusError with HTTP %d", tt.query, tt.status)
		}
	}
}

// TestReplayWrongToken holds that a token that the server refuses is an *Error
// with HTTP 401 and the code 390144 (measured).
func TestReplayWrongToken(t *testing.T) {
	t.Parallel()
	db, _ := replayWith(t, &replayServer{t: t, only: http.StatusUnauthorized})
	_, _, err := readAll(t, db, "SELECT 1")
	e := serverError(t, err)
	if e.HTTPStatus != http.StatusUnauthorized || e.Code != "390144" || e.SQLState != "" {
		t.Errorf("the error is %+v, want HTTP 401 and the code 390144", *e)
	}
}

// TestReplayBindings holds D183 item 6: each argument is the member of
// bindings that its Go type names, and the body is the body that the recorder
// sent, which the fake server matches.
func TestReplayBindings(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, tt := range []struct {
		name string
		args []any
		want any
	}{
		{"FIXED", []any{int64(42)}, nil},
		{"REAL", []any{1.5}, nil},
		{"TEXT", []any{"hé"}, nil},
		{"BOOLEAN", []any{true}, nil},
		{"DATE", []any{dbimp.Date{Year: 2026, Month: time.October, Day: 9}}, nil},
		{"TIME", []any{dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}}, nil},
		{"TIMESTAMP_NTZ", []any{dbimp.LocalDateTimeOf(time.Unix(1791549296, 123456789).UTC())}, nil},
		{"TIMESTAMP_TZ", []any{time.Unix(1791549296, 123456789).In(time.FixedZone("", 7*3600))}, nil},
		{"BINARY", []any{[]byte{0xDE, 0xAD, 0xBE, 0xEF}}, nil},
		{"NULL", []any{nil}, nil},
	} {
		if _, _, err := readAll(t, db, "SELECT ?", tt.args...); err != nil {
			t.Errorf("a binding of %s: %v", tt.name, err)
		}
	}
	// A repeated placeholder takes one binding, a named one takes its name, and
	// a binding with no placeholder is sent too, for the server to ignore.
	if _, _, err := readAll(t, db, "SELECT :1, :1", int64(7)); err != nil {
		t.Errorf("a numbered placeholder: %v", err)
	}
	if _, _, err := readAll(t, db, "SELECT :a", sql.Named("a", int64(7))); err != nil {
		t.Errorf("a named placeholder: %v", err)
	}
	if _, _, err := readAll(t, db, "SELECT ?", int64(1), int64(2)); err != nil {
		t.Errorf("more bindings than placeholders: %v", err)
	}
	res, err := db.ExecContext(t.Context(), "INSERT INTO dbimp_it_dml VALUES (?, ?)", int64(1), "a")
	if err != nil {
		t.Fatalf("a binding in an insert: %v", err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Errorf("the insert changed %d rows, %v, want 1", n, err)
	}
}

// TestReplayStatements holds that a statement is sent as the caller wrote it:
// a comment before and after it, a trailing semicolon, and an exchange of
// Exec through a prepared statement.
func TestReplayStatements(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, query := range []string{"/* a comment */ SELECT 1 -- and another", "SELECT 1;", "SELECT 1"} {
		if _, got, err := readAll(t, db, query); err != nil || len(got) != 1 {
			t.Errorf("%s: %d rows, %v", query, len(got), err)
		}
	}
	stmt, err := db.PrepareContext(t.Context(), "SELECT ?")
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	var got int64
	if err := stmt.QueryRowContext(t.Context(), int64(42)).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Errorf("a prepared statement gave %d, want 42", got)
	}
}

// TestReplayRowsAffected holds D178 item 14 and D183: RowsAffected is the sum of
// the counts in stats, and an answer with no count gives an error that wraps
// dbimp.ErrNotSupported.
func TestReplayRowsAffected(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	for _, tt := range []struct {
		query string
		want  int64
	}{
		{"INSERT INTO dbimp_it_dml VALUES (2,'b'),(3,'c')", 2},
		{"UPDATE dbimp_it_dml SET name = 'z' WHERE id < 3", 2},
		{"DELETE FROM dbimp_it_dml WHERE id = 3", 1},
		{"MERGE INTO dbimp_it_dml t USING (SELECT 1 AS id, 'm' AS name) s ON t.id = s.id WHEN MATCHED THEN UPDATE SET name = s.name", 1},
		{"TRUNCATE TABLE dbimp_it_dml", 2},
	} {
		res, err := db.ExecContext(t.Context(), tt.query)
		if err != nil {
			t.Fatalf("%s: %v", tt.query, err)
		}
		if n, err := res.RowsAffected(); err != nil || n != tt.want {
			t.Errorf("%s: RowsAffected is %d, %v, want %d", tt.query, n, err, tt.want)
		}
	}
	for _, query := range []string{"CREATE OR REPLACE TABLE dbimp_it_dml (id NUMBER, name VARCHAR)", "DROP TABLE IF EXISTS dbimp_it_nosuch"} {
		res, err := db.ExecContext(t.Context(), query)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if n, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) || n != 0 {
			t.Errorf("%s: RowsAffected is %d, %v, want an error that wraps dbimp.ErrNotSupported", query, n, err)
		}
	}
	res, err := db.ExecContext(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want an error that wraps dbimp.ErrNotSupported", err)
	}
}

// TestReplayTransactions holds D183 item 7: BeginTx fails with
// dbimp.ErrNotSupported, and sends no request.
func TestReplayTransactions(t *testing.T) {
	t.Parallel()
	db, s := replay(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("BeginTx returned a transaction")
	}
	if !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("the error is %v, want dbimp.ErrNotSupported", err)
	}
	if got := s.requests(); len(got) != 0 {
		t.Errorf("BeginTx sent %v", got)
	}
}

// TestReplayVersion holds that the statement that usql runs for the version
// works (step 16).
func TestReplayVersion(t *testing.T) {
	t.Parallel()
	db, _ := replay(t)
	var version string
	if err := db.QueryRowContext(t.Context(), "SELECT CURRENT_VERSION()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "10.36.101" {
		t.Errorf("the version is %q, want 10.36.101", version)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping: %v", err)
	}
}
