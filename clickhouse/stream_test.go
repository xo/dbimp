package clickhouse //nolint:testpackage // The tests set no version and no cancel, which only the package can.

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// stream is a database on a fake server that answers every statement with one
// body, as the server does when it streams an answer.
type stream struct {
	db *sql.DB
}

// answer is what a fake server sends.
type answer struct {
	status int
	// header holds the headers of the response, in pairs.
	header []string
	body   string
	// abort is true to close the connection after the body, with no last chunk,
	// as the server does after the text of an error after some rows (measured).
	abort bool
}

// newStream opens a database on a fake server that answers with body, and with
// the tag as the header X-ClickHouse-Exception-Tag unless it is empty. If
// version is not empty, a connection asks SELECT version(), and the server
// answers it.
func newStream(t *testing.T, version, tag, body string) *stream {
	t.Helper()
	a := answer{body: body, abort: strings.Contains(body, marker)}
	if tag != "" {
		a.header = []string{tagHeader, tag}
	}
	return newAnswer(t, version, a)
}

// newAnswer is newStream for an answer that the test builds.
func newAnswer(t *testing.T, version string, a answer) *stream {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statement, _ := io.ReadAll(r.Body)
		if string(statement) == "SELECT version()" {
			_, _ = io.WriteString(w, `["version()"]`+"\n"+`["String"]`+"\n"+`["`+version+`"]`+"\n")
			return
		}
		for i := 0; i+1 < len(a.header); i += 2 {
			w.Header().Set(a.header[i], a.header[i+1])
		}
		w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
		if a.status != 0 {
			w.WriteHeader(a.status)
		}
		_, _ = io.WriteString(w, a.body)
		if a.abort {
			_ = http.NewResponseController(w).Flush()
			panic(http.ErrAbortHandler)
		}
	}))
	t.Cleanup(srv.Close)
	cfg, err := ParseDSN(strings.Replace(srv.URL, "http://", Name+"://", 1))
	if err != nil {
		t.Fatal(err)
	}
	c := NewConnector(*cfg)
	c.noStop, c.noVersion = true, version == ""
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	return &stream{db: db}
}

// count runs a statement, reads every row, and returns the count and the error of
// the query or of the rows.
func (s *stream) count(t *testing.T) (int, error) {
	t.Helper()
	rows, err := s.db.QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}

// TestStreamEnds holds what the end of the body means (D21 and D176).
func TestStreamEnds(t *testing.T) {
	t.Parallel()
	hdr := "[\"a\"]\n[\"Int64\"]\n"
	text := "Code: 395. DB::Exception: boom. (FUNCTION_THROW_IF_VALUE_IS_NON_ZERO) (version 25.8.33.6 (official build))\n"
	for _, tt := range []struct {
		name string
		a    answer
		rows int
		// err is "" for an answer that completed, and else what the error wraps.
		err  error
		code int
		// early is true when the error comes from QueryContext, with no row read.
		early bool
	}{
		{name: "a complete answer", a: answer{body: hdr + "[1]\n[2]\n"}, rows: 2},
		{name: "no row", a: answer{body: hdr}, rows: 0},
		{name: "no white space after the last row", a: answer{body: hdr + "[1]\n[2]"}, rows: 2},
		{name: "the marker after rows", a: answer{body: hdr + "[1]\n[2]\n" + marker + "\r\n" + text, abort: true}, rows: 2, err: dbimp.ErrIncomplete, code: 395},
		{name: "the marker after rows and a last chunk", a: answer{body: hdr + "[1]\n" + marker + "\r\n" + text}, rows: 1, err: dbimp.ErrIncomplete, code: 395},
		{name: "the marker before any row", a: answer{body: hdr + marker + "\r\n" + text, abort: true}, early: true, code: 395},
		{name: "a cut body after rows", a: answer{body: hdr + "[1]\n[2]\n", abort: true}, rows: 2, err: io.ErrUnexpectedEOF},
		{name: "a cut body in a row", a: answer{body: hdr + "[1]\n[2", abort: true}, rows: 1, err: io.ErrUnexpectedEOF},
		{name: "a cut body in a row, closed", a: answer{body: hdr + "[1]\n[2"}, rows: 1, err: io.ErrUnexpectedEOF},
		{name: "a cut body in the marker", a: answer{body: hdr + "[1]\n__exc", abort: true}, rows: 1, err: io.ErrUnexpectedEOF},
		{name: "a cut body in the header", a: answer{body: "[\"a\"]\n", abort: true}, early: true, err: io.ErrUnexpectedEOF},
		{name: "no types", a: answer{body: "[\"a\"]\n"}, early: true, err: io.ErrUnexpectedEOF},
		{name: "text after the rows", a: answer{body: hdr + "[1]\nxyz\n"}, rows: 1, err: dbimp.ErrIncomplete},
		{name: "a value after the rows", a: answer{body: hdr + "[1]\n\"xyz\"\n"}, rows: 1, err: dbimp.ErrIncomplete},
		{name: "a row of two values for a column", a: answer{body: hdr + "[1]\n[1, 2]\n"}, rows: 1, err: dbimp.ErrColumnCount},
		{name: "a row of no value for a column", a: answer{body: hdr + "[1]\n[]\n"}, rows: 1, err: dbimp.ErrColumnCount},
		{name: "a value of the wrong kind", a: answer{body: hdr + "[1]\n[\"x\"]\n"}, rows: 1, err: dbimp.ErrInvalidValue},
		{name: "two names and one type", a: answer{body: "[\"a\", \"b\"]\n[\"Int64\"]\n"}, early: true, err: dbimp.ErrColumnCount},
		{name: "a type that is not one", a: answer{body: "[\"a\"]\n[\"Array(\"]\n"}, early: true, err: dbimp.ErrInvalidValue},
		{name: "a statement with a FORMAT clause", a: answer{header: []string{formatHeader, "JSON"}, body: "{}"}, early: true, err: dbimp.ErrNotSupported},
		{name: "HTTP 500 after rows", a: answer{status: 500, body: hdr + "[1]\n[2]\n" + text}, early: true, code: 395},
		{name: "HTTP 500 with the text alone", a: answer{status: 500, body: text}, early: true, code: 395},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newAnswer(t, "", tt.a)
			n, err := s.count(t)
			switch {
			case tt.err == nil && tt.code == 0 && err != nil:
				t.Errorf("read %d rows and %v, want %d rows and no error", n, err, tt.rows)
				return
			case tt.err == nil && tt.code == 0 && n != tt.rows:
				t.Errorf("read %d rows, want %d", n, tt.rows)
				return
			case (tt.err != nil || tt.code != 0) && err == nil:
				t.Errorf("read %d rows and no error, want an error", n)
				return
			}
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Errorf("the error is %v, want one that wraps %v", err, tt.err)
			}
			if tt.code != 0 {
				if e := serverError(t, err); e.Code != tt.code {
					t.Errorf("the error is %+v, want the code %d", e, tt.code)
				}
			}
			if tt.early && n != 0 {
				t.Errorf("read %d rows, want the error from QueryContext, before any row", n)
			}
			if tt.early && errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("an error before any row wraps dbimp.ErrIncomplete (D107): %v", err)
			}
			if !tt.early && tt.err != nil && n != tt.rows {
				t.Errorf("read %d rows before the error, want %d", n, tt.rows)
			}
			if err != nil && !tt.early && !errors.Is(err, dbimp.ErrIncomplete) && tt.rows > 0 && !errors.Is(err, dbimp.ErrColumnCount) && !errors.Is(err, dbimp.ErrInvalidValue) {
				t.Errorf("the error after %d rows is %v, want one that wraps dbimp.ErrIncomplete (D107)", n, err)
			}
		})
	}
}

// TestStreamValuesStay holds that the bytes of a string that are not UTF-8 reach
// the caller, as the server wrote them, and that the first column of each row
// keeps the order of the answer (hard rule 3).
func TestStreamValuesStay(t *testing.T) {
	t.Parallel()
	s := newAnswer(t, "", answer{body: "[\"z\", \"a\", \"m\"]\n[\"String\", \"Nullable(Int8)\", \"Array(String)\"]\n[\"\xff\\u0000\\n\", null, [\"\xfe\"]]\n"})
	rows, err := s.db.QueryContext(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if strings.Join(cols, ",") != "z,a,m" {
		t.Errorf("the columns are %q, want z, a and m in that order", cols)
	}
	if !rows.Next() {
		t.Fatalf("no row: %v", rows.Err())
	}
	var (
		z string
		a sql.Null[int64]
		m any
	)
	if err := rows.Scan(&z, &a, &m); err != nil {
		t.Fatal(err)
	}
	if z != "\xff\x00\n" || a.Valid || len(m.([]any)) != 1 || m.([]any)[0] != "\xfe" { //nolint:forcetypeassert // The scan of an array gives a []any.
		t.Errorf("the values are %q, %v and %q", z, a, m)
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []struct {
		name     string
		nullable bool
	}{{"STRING", false}, {"INT8", true}, {"ARRAY", false}} {
		if got := types[i].DatabaseTypeName(); got != want.name {
			t.Errorf("the database type of column %d is %q, want %q", i, got, want.name)
		}
		if n, ok := types[i].Nullable(); !ok || n != want.nullable {
			t.Errorf("the nullable of column %d is %v, %v, want %v", i, n, ok, want.nullable)
		}
	}
}

// TestLargeResult holds D25: the driver reads a result of 64 MiB one row at a
// time, and the memory in use stays far below the size of the result. It does not
// run in parallel, so that the memory of other tests does not count.
func TestLargeResult(t *testing.T) { //nolint:paralleltest // The test reads the memory in use of the process.
	const rowsCount = 1 << 20
	row := "[" + `"` + strings.Repeat("x", 57) + `"` + "]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "[\"s\"]\n[\"String\"]\n")
		for range rowsCount {
			if _, err := io.WriteString(w, row); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	cfg, err := ParseDSN(strings.Replace(srv.URL, "http://", Name+"://", 1))
	if err != nil {
		t.Fatal(err)
	}
	c := NewConnector(*cfg)
	c.noVersion = true
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	rows, err := db.QueryContext(t.Context(), "SELECT s FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var (
		n    int
		peak uint64
		s    string
	)
	for rows.Next() {
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		if n++; n%(rowsCount/4) == 0 {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapInuse)
		}
	}
	if err := rows.Err(); err != nil || n != rowsCount {
		t.Fatalf("read %d rows and %v, want %d rows", n, err, rowsCount)
	}
	if peak > 16<<20 {
		t.Errorf("the heap in use reached %d bytes while the driver read a result of 64 MiB, want at most 16 MiB (D25)", peak)
	}
}

// TestIdleTimeout holds that the connector lets go of an idle connection before the
// server does, which is after 10 seconds on 25.3 and 25.8 and 30 on 26.8 and 26.9 (the header
// Keep-Alive, measured), because the driver sends a POST once and a POST on a
// connection that the server just closed is an error.
func TestIdleTimeout(t *testing.T) {
	t.Parallel()
	c := NewConnector(Config{Host: "localhost"})
	t.Cleanup(func() { _ = c.Close() })
	if got := c.transport.IdleConnTimeout; got <= 0 || got >= 10*time.Second {
		t.Errorf("the transport keeps an idle connection for %v, want less than the 10 seconds of the server", got)
	}
	if c.client.CheckRedirect == nil {
		t.Error("the client follows redirects, and D177 says that it follows none")
	}
}
