package cosmos_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp/cosmos"
	"github.com/xo/dbimp/dbimptest"
)

// The replay tests use the exchanges that step 6 recorded, under
// testdata/cosmos/. The files of the hosted account are named cosmos-NNN, and
// the files of the emulator are named cosmos-EN20260907-NNN. Each test decodes
// a real answer through the driver. A fake server answers each request with
// the exchange that matches the method, the path, the body and the headers
// that the driver sets for a query, and it fails the test for a request that
// nothing matches, so the driver must send what the recording holds.

const testdata = "../testdata/cosmos"

const (
	hosted   = "hosted"
	emulator = "cosmos-EN20260907"
)

// recordedHeaders are the headers of a request that a recorded exchange and a
// request of the driver must agree on. The others are the signature, which
// the recorder redacted, and the date.
var recordedHeaders = []string{
	"X-Ms-Documentdb-Isquery",
	"X-Ms-Documentdb-Query-Enablecrosspartition",
	"X-Ms-Documentdb-Partitionkey",
	"X-Ms-Max-Item-Count",
	"X-Ms-Continuation",
}

// match matches a request of the driver with a recorded one by the method,
// the path, the body and the headers of a query.
func match(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	for _, h := range recordedHeaders {
		if r.Header.Get(h) != strings.Join(ex.Request.Header[h], ",") {
			return false
		}
	}
	return dbimptest.DefaultMatch(r, body, ex)
}

// hostedName is the name of a file of the hosted account.
var hostedName = regexp.MustCompile(`^cosmos-[0-9]{3}-`)

// replayServer starts a fake server that replays the exchanges of one server:
// the files of the hosted account, which are the files whose number follows
// cosmos- at once, or the files of the emulator.
func replayServer(t *testing.T, server string) *httptest.Server {
	t.Helper()
	if server == emulator {
		return dbimptest.ReplayRelease(t, testdata, emulator, match)
	}
	dir := t.TempDir()
	files, err := filepath.Glob(filepath.Join(testdata, "cosmos-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		if !hostedName.MatchString(name) {
			continue
		}
		abs, err := filepath.Abs(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(abs, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dbimptest.Replay(t, dir, match)
}

// dsnFor returns the DSN of the fake server at url, with the key of the tests.
// path is the database and the container, and query is more keys of the query.
func dsnFor(rawURL, path, query string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	s := "cosmos://x:" + escapedKey + "@" + u.Host + path + "?tls=false"
	if query != "" {
		s += "&" + query
	}
	return s
}

// openFake opens the database of the fake server at url, for the database dbimp_it
// and the container that the replay tests use.
func openFake(t *testing.T, rawURL, container, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open(cosmos.Name, dsnFor(rawURL, "/dbimp_it/"+container, query))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// forEachServer runs f for the hosted account and for the emulator, with a
// database on a fake server that replays the exchanges of each, for the
// container kv.
func forEachServer(t *testing.T, f func(t *testing.T, server string, db *sql.DB)) {
	t.Helper()
	for _, server := range []string{hosted, emulator} {
		t.Run(server, func(t *testing.T) {
			t.Parallel()
			srv := replayServer(t, server)
			f(t, server, openFake(t, srv.URL, "kv", ""))
		})
	}
}

// show writes v with its Go type, so that a test compares the type of a value
// as well as its text.
func show(v any) string {
	switch v := v.(type) {
	case nil:
		return "nil"
	case *apd.Decimal:
		return "decimal(" + v.Text('f') + ")"
	case string:
		return fmt.Sprintf("string(%q)", v)
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = show(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ":" + show(v[k])
		}
		return "{" + strings.Join(parts, " ") + "}"
	}
	return fmt.Sprintf("%T(%v)", v, v)
}

// read runs a query and returns its columns and its rows, each value written
// by show.
func read(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]string, error) {
	t.Helper()
	return readContext(t, t.Context(), db, query, args...)
}

// readContext is read with the context ctx.
func readContext(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) ([]string, [][]string, error) {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	return readRows(t, rows)
}

// readRows reads every row of rows, each value written by show.
func readRows(t *testing.T, rows *sql.Rows) ([]string, [][]string, error) {
	t.Helper()
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = show(v)
		}
		out = append(out, row)
	}
	return cols, out, rows.Err()
}

// drain runs a query and reads every row, and returns the error of the query or
// of the rows.
func drain(t *testing.T, db *sql.DB, query string, args ...any) error {
	t.Helper()
	_, _, err := read(t, db, query, args...)
	return err
}

// drainContext is drain with the context ctx.
func drainContext(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) error {
	t.Helper()
	_, _, err := readContext(t, ctx, db, query, args...)
	return err
}
