package trino_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/trino"
)

// These tests need a server. TRINO_DSN is the url of dbrun for the
// administrator of a Trino or a Presto release, which is trino:// or presto:// from
// dbmeta v0.2.0, and http:// from an older one, and the tests read it as the DSN
// trino://user@host:port/catalog/schema (D9). No release has an ordinary user,
// because the servers of the dbrun images check no password (docs/TRINO.md,
// Principals). A name in the header of the user is the only other principal, and
// TestIntegrationUsers runs as one. A test skips when the variable is empty. The
// only catalog that writes is memory, and it refuses UPDATE, DELETE and MERGE on
// every release (D175), so the tests that change a row test the refusal. Each test
// makes its tables in a schema of its own, which TestMain drops at the end.

// suffix makes the names of this run unique.
var suffix = strings.ToLower(rand.Text()[:8])

// schemaName is the schema of this run in the catalog memory.
var schemaName = "dbimp_it_" + suffix

// toDSN returns the URL of dbrun as the DSN of the driver. A URL whose scheme
// is presto or trino has the form already. An http URL has the catalog and the
// schema in its query.
func toDSN(v string) string {
	u, err := url.Parse(v)
	if err != nil {
		return v
	}
	switch u.Scheme {
	case "presto", "trino":
		u.Scheme = trino.Name
	case "http", "https":
		q := u.Query()
		u.Path = "/" + q.Get("catalog")
		if s := q.Get("schema"); s != "" {
			u.Path += "/" + s
		}
		q.Del("catalog")
		q.Del("schema")
		if u.Scheme == "https" {
			q.Set("tls", "true")
		}
		u.RawQuery = q.Encode()
		u.Scheme = trino.Name
	}
	return u.String()
}

// serverDSN returns the DSN of the server, or skips the test when it is empty.
func serverDSN(tb testing.TB) string {
	tb.Helper()
	v := os.Getenv("TRINO_DSN")
	if v == "" {
		tb.Skip("TRINO_DSN is empty, so there is no server to test")
	}
	return toDSN(v)
}

// release is the server that the tests run on.
type release struct {
	// flavor is trino or presto, and version is its version, such as 476 or
	// 0.299-7d50721.
	flavor  string
	version string
	// major is the number of a Trino release, and 0 for Presto.
	major int
	dsn   string
}

// name returns the name of the release, such as trino 476.
func (r release) name() string {
	return r.flavor + " " + r.version
}

// isTrino and isPresto report the flavor.
func (r release) isTrino() bool  { return r.flavor == trino.FlavorTrino }
func (r release) isPresto() bool { return r.flavor == trino.FlavorPresto }

// connect returns a connector for dsn with the keys of query added.
func connect(tb testing.TB, dsn string, mutate func(*trino.Config)) *trino.Connector {
	tb.Helper()
	cfg, err := trino.ParseDSN(dsn)
	if err != nil {
		tb.Fatalf("parsing the DSN of the server: %v", err)
	}
	if mutate != nil {
		mutate(cfg)
	}
	return trino.NewConnector(*cfg)
}

var (
	releaseOnce sync.Once
	releaseInfo release
	errRelease  error
)

// theRelease asks the server for its flavor and its version, once.
func theRelease(tb testing.TB) release {
	tb.Helper()
	dsn := serverDSN(tb)
	releaseOnce.Do(func() {
		c := connect(tb, dsn, func(c *trino.Config) { c.Catalog, c.Schema = "memory", "" })
		db := sql.OpenDB(c)
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		var version string
		if err := db.QueryRowContext(ctx, "SELECT node_version FROM system.runtime.nodes WHERE coordinator = true").Scan(&version); err != nil {
			errRelease = fmt.Errorf("reading the version of the server: %w", err)
			return
		}
		releaseInfo = release{flavor: c.Flavor(), version: version, dsn: dsn}
		if releaseInfo.isTrino() {
			releaseInfo.major, _ = strconv.Atoi(strings.SplitN(version, "-", 2)[0])
		}
		// The schema of this run is made once, in the catalog memory.
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS memory."+schemaName); err != nil {
			errRelease = fmt.Errorf("making the schema %s: %w", schemaName, err)
		}
	})
	if errRelease != nil {
		tb.Fatal(errRelease)
	}
	return releaseInfo
}

// TestMain drops the schema of this run when the tests end, and fails if a test
// left a table in it.
func TestMain(m *testing.M) {
	code := m.Run()
	if v := os.Getenv("TRINO_DSN"); v != "" && releaseInfo.dsn != "" {
		if err := dropSchema(releaseInfo.dsn); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

// dropSchema drops each table of the schema of this run that a test left, and
// the schema, and reports each table that it found.
func dropSchema(dsn string) error {
	cfg, err := trino.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parsing the DSN: %w", err)
	}
	cfg.Catalog, cfg.Schema = "memory", ""
	db := sql.OpenDB(trino.NewConnector(*cfg))
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	left, err := leftIn(ctx, db)
	if err != nil {
		return err
	}
	for _, name := range left {
		_, _ = db.ExecContext(ctx, "DROP VIEW IF EXISTS memory."+schemaName+"."+name)
		_, _ = db.ExecContext(ctx, "DROP MATERIALIZED VIEW IF EXISTS memory."+schemaName+"."+name)
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS memory."+schemaName+"."+name)
	}
	_, _ = db.ExecContext(ctx, "DROP SCHEMA IF EXISTS memory."+schemaName+"_s")
	if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS memory."+schemaName); err != nil {
		return fmt.Errorf("dropping the schema %s: %w", schemaName, err)
	}
	if len(left) > 0 {
		return fmt.Errorf("the tests left %d tables in %s: %v", len(left), schemaName, left)
	}
	return nil
}

// leftIn returns the names of the tables that the schema of this run holds.
func leftIn(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT table_name FROM memory.information_schema.tables WHERE table_schema = '"+schemaName+"'")
	if err != nil {
		return nil, fmt.Errorf("listing what a test left in %s: %w", schemaName, err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("listing what a test left in %s: %w", schemaName, err)
		}
		left = append(left, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing what a test left in %s: %w", schemaName, err)
	}
	return left, nil
}

// env is what a test of a server uses: the release and a database in the schema
// of this run.
type env struct {
	release

	db *sql.DB
}

// newEnv opens a database on the server, in the schema of this run, for the
// administrator. The database closes with the test.
func newEnv(t *testing.T) env {
	t.Helper()
	return newEnvWith(t, nil)
}

// newEnvWith is newEnv with a change to the configuration, such as a user.
func newEnvWith(t *testing.T, mutate func(*trino.Config)) env {
	t.Helper()
	rel := theRelease(t)
	c := connect(t, rel.dsn, func(c *trino.Config) {
		c.Catalog, c.Schema = "memory", schemaName
		if mutate != nil {
			mutate(c)
		}
	})
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	return env{release: rel, db: db}
}

// table returns the name of a table of this run, made from the name of the
// test, and drops it when the test ends.
func (e env) table(t *testing.T, name string) string {
	t.Helper()
	tbl := "t_" + strings.ToLower(strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, name))
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_, _ = e.db.ExecContext(ctx, "DROP VIEW IF EXISTS "+tbl)
		_, _ = e.db.ExecContext(ctx, "DROP MATERIALIZED VIEW IF EXISTS "+tbl)
		_, _ = e.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+tbl)
	})
	return tbl
}

// must runs a statement and fails the test with its error.
func (e env) must(t *testing.T, query string, args ...any) sql.Result {
	t.Helper()
	res, err := e.db.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
	return res
}

// refused runs a statement that the server must refuse. A statement that a server
// accepts, or that fails for a reason other than the answer of the server, fails
// the test.
func (e env) refused(t *testing.T, query string, args ...any) {
	t.Helper()
	_ = e.refusal(t, query, args...)
}

// refusal is refused that returns the error of the server.
func (e env) refusal(t *testing.T, query string, args ...any) *trino.Error {
	t.Helper()
	_, err := e.db.ExecContext(t.Context(), query, args...)
	if err == nil {
		t.Fatalf("%q ran, and the server must refuse it", query)
	}
	perr, ok := errors.AsType[*trino.Error](err)
	if !ok {
		t.Fatalf("%q failed with %v, want the error of the server", query, err)
	}
	return perr
}

// rows reads every row of a query into a *any for each column.
func (e env) rows(t *testing.T, query string, args ...any) [][]any {
	t.Helper()
	rows, err := e.db.QueryContext(t.Context(), query, args...)
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
			t.Fatalf("scanning %q: %v", query, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading %q: %v", query, err)
	}
	return out
}

// readErr runs a query, reads every row, and returns the error of the query or of a row.
func (e env) readErr(t *testing.T, query string, args ...any) error {
	t.Helper()
	return e.readErrCtx(t.Context(), query, args...)
}

// readErrCtx is readErr with a context.
func (e env) readErrCtx(ctx context.Context, query string, args ...any) error {
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// scalar returns the one value of a query.
func (e env) scalar(t *testing.T, query string, args ...any) any {
	t.Helper()
	rows := e.rows(t, query, args...)
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("%q gave %v, want one value", query, rows)
	}
	return rows[0][0]
}

// pinned returns a connection of the pool, so that a statement that changes the
// session shows in the next, and closes it with the test.
func (e env) pinned(t *testing.T) *sql.Conn {
	t.Helper()
	conn, err := e.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// TestIntegrationBasics holds the plain path of a caller of database/sql: the
// flavor, a ping, the columns, and every value through Rows.Scan into the types
// that a caller uses.
func TestIntegrationBasics(t *testing.T) {
	e := newEnv(t)
	t.Logf("the server is %s", e.name())
	if err := e.db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	var (
		id    int64
		name  string
		ratio float64
		ok    bool
		nul   sql.Null[string]
		when  sql.Null[time.Time]
		raw   []byte
		num   sql.Null[float64]
		anyv  any
	)
	err := e.db.QueryRowContext(t.Context(), "SELECT BIGINT '9223372036854775807', 'é''x', DOUBLE '1.5', TRUE, CAST(NULL AS varchar), CAST(TIMESTAMP '2026-10-01 12:34:56.789 UTC' AS timestamp with time zone), X'00ff', CAST(NULL AS double), 1").
		Scan(&id, &name, &ratio, &ok, &nul, &when, &raw, &num, &anyv)
	if err != nil {
		t.Fatal(err)
	}
	if id != 9223372036854775807 || name != "é'x" || ratio != 1.5 || !ok || nul.Valid || !when.Valid || !when.V.Equal(time.Date(2026, 10, 1, 12, 34, 56, 789e6, time.UTC)) ||
		!reflect.DeepEqual(raw, []byte{0, 0xff}) || num.Valid || anyv != int64(1) {
		t.Errorf("the values are %v %q %v %v %v %v %v %v %v", id, name, ratio, ok, nul, when, raw, num, anyv)
	}
	// A NULL into a plain destination is an error of database/sql (D8).
	var s string
	if err := e.db.QueryRowContext(t.Context(), "SELECT CAST(NULL AS varchar)").Scan(&s); err == nil {
		t.Error("a NULL scanned into a *string gave no error")
	}
	rows, err := e.db.QueryContext(t.Context(), "SELECT 1 AS a, 'x' AS a, DECIMAL '1.50' AS c, CAST('ab' AS char(5)) AS d, CAST('x' AS varchar(7)) AS e")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if want := []string{"a", "a", "c", "d", "e"}; !reflect.DeepEqual(cols, want) {
		t.Errorf("the columns are %q, want %q", cols, want)
	}
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if cts[2].DatabaseTypeName() != "DECIMAL" || cts[3].DatabaseTypeName() != "CHAR" {
		t.Errorf("the types are %s and %s", cts[2].DatabaseTypeName(), cts[3].DatabaseTypeName())
	}
	if p, s, ok := cts[2].DecimalSize(); !ok || p != 3 || s != 2 {
		t.Errorf("the size of the decimal is %d, %d, %v, want 3, 2", p, s, ok)
	}
	if n, ok := cts[4].Length(); !ok || n != 7 {
		t.Errorf("the length of the varchar is %d, %v, want 7", n, ok)
	}
	if n, ok := cts[3].Nullable(); !n || !ok {
		t.Error("a column must say that it can be NULL")
	}
}

// TestIntegrationUsers runs as another user, which the servers take from the
// header (docs/TRINO.md, Principals).
func TestIntegrationUsers(t *testing.T) {
	e := newEnvWith(t, func(c *trino.Config) { c.User = "alice" })
	if got := e.scalar(t, "SELECT current_user"); got != "alice" {
		t.Errorf("the user is %v, want alice", got)
	}
	if err := e.db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestIntegrationVersion holds D175: the driver learns the flavor from the
// server, and the key flavor skips the request.
func TestIntegrationVersion(t *testing.T) {
	rel := theRelease(t)
	t.Logf("the release is %s", rel.name())
	if rel.isPresto() != strings.HasPrefix(rel.version, "0.") {
		t.Errorf("the flavor is %q for the version %q", rel.flavor, rel.version)
	}
	// SELECT version() gives the release as the server writes it. Trino
	// answers it itself, and the driver answers it on Presto from GET /v1/info
	// (D181). That endpoint needs no privilege, so every user reads it (and
	// no release here has an ordinary user).
	for _, key := range []string{"", "flavor=" + rel.flavor} {
		for _, user := range []string{"", "alice"} {
			c := connect(t, rel.dsn, func(c *trino.Config) {
				if key != "" {
					c.Flavor = rel.flavor
				}
				if user != "" {
					c.User = user
				}
			})
			db := sql.OpenDB(c)
			for _, query := range []string{"SELECT version()", "select VERSION();"} {
				var got string
				if err := db.QueryRowContext(t.Context(), query).Scan(&got); err != nil || got != rel.version {
					t.Errorf("%s with the key %q and the user %q gave %q and %v, want %q", query, key, user, got, err, rel.version)
				}
			}
			if got, err := preparedVersion(t, db); err != nil || got != rel.version {
				t.Errorf("the prepared SELECT version() gave %q and %v, want %q", got, err, rel.version)
			}
			if rel.isPresto() {
				// Any other statement goes to the server, which has no such
				// function.
				_, err := db.ExecContext(t.Context(), "SELECT version(), 1")
				if perr, ok := errors.AsType[*trino.Error](err); !ok || !strings.Contains(perr.Message, "version") {
					t.Errorf("SELECT version(), 1 gave %v, want the error of the server", err)
				}
			}
			db.Close()
		}
	}
	if !rel.isPresto() {
		// Trino takes the user from the basic authentication that the driver
		// sends, so it runs a statement that has the headers of Presto.
		return
	}
	// Presto needs the headers of its own product, and answers HTTP 400 to the
	// others (measured).
	c := connect(t, rel.dsn, func(c *trino.Config) { c.Flavor = trino.FlavorTrino })
	db := sql.OpenDB(c)
	defer db.Close()
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if perr, ok := errors.AsType[*trino.Error](err); !ok || perr.HTTPStatus != 400 {
		t.Errorf("a statement with the headers of Trino on %s gave %v, want HTTP 400", rel.name(), err)
	}
}

// preparedVersion runs SELECT version() as a prepared statement.
func preparedVersion(t *testing.T, db *sql.DB) (string, error) {
	t.Helper()
	stmt, err := db.PrepareContext(t.Context(), "SELECT version()")
	if err != nil {
		return "", err
	}
	defer stmt.Close()
	var got string
	err = stmt.QueryRowContext(t.Context()).Scan(&got)
	return got, err
}

// equalValues reports whether a value read equals the value wanted. A NaN equals a
// NaN, a time is the same instant in the same offset, and a decimal is the same
// number.
func equalValues(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case time.Time:
		g, ok := got.(time.Time)
		if !ok {
			return false
		}
		_, goff := g.Zone()
		_, woff := w.Zone()
		return g.Equal(w) && goff == woff
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !equalValues(g[i], w[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, v := range w {
			if !equalValues(g[k], v) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// TestIntegrationPaging reads results of many pages, each row once and in its order
// (D21), and holds that an error after some rows wraps dbimp.ErrIncomplete (D107).
func TestIntegrationPaging(t *testing.T) {
	e := newEnv(t)
	rows, err := e.db.QueryContext(t.Context(), "SELECT a.n * 200 + b.m AS n, rpad('x', 200, 'y') FROM UNNEST(sequence(0, 99)) AS a(n) CROSS JOIN UNNEST(sequence(1, 200)) AS b(m) ORDER BY n")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var want int64
	for rows.Next() {
		want++
		var n int64
		var s string
		if err := rows.Scan(&n, &s); err != nil || n != want || len(s) != 200 {
			t.Fatalf("row %d is %d, %d characters, %v", want, n, len(s), err)
		}
	}
	if err := rows.Err(); err != nil || want != 20000 {
		t.Fatalf("read %d rows, %v, want 20000", want, err)
	}
	// An error after some rows, which the server finds late in the result.
	rows2, err := e.db.QueryContext(t.Context(), "SELECT a.n * 200 + b.m AS n, rpad('x', 200, 'y'), 1/(a.n * 200 + b.m - 5000) FROM UNNEST(sequence(0, 99)) AS a(n) CROSS JOIN UNNEST(sequence(1, 200)) AS b(m) ORDER BY n")
	if err != nil {
		// The server can find the fault before it sends a row, which is a fault of
		// the statement and not of the result.
		t.Logf("the server found the fault before any row: %v", err)
		return
	}
	defer rows2.Close()
	read := 0
	for rows2.Next() {
		read++
	}
	err = rows2.Err()
	if perr, ok := errors.AsType[*trino.Error](err); !ok || perr.Name != "DIVISION_BY_ZERO" {
		t.Fatalf("the error after %d rows is %v, want DIVISION_BY_ZERO", read, err)
	}
	if read > 0 && !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("an error after %d rows does not wrap dbimp.ErrIncomplete: %v", read, err)
	}
}

// TestIntegrationErrors holds the errors of the server before any row.
func TestIntegrationErrors(t *testing.T) {
	e := newEnv(t)
	for _, tt := range []struct{ query, name string }{
		{"SELEC 1", "SYNTAX_ERROR"},
		{"SELECT 1/0", "DIVISION_BY_ZERO"},
		{"SELECT CAST('x' AS integer)", "INVALID_CAST_ARGUMENT"},
	} {
		err := e.readErr(t, tt.query)
		perr, ok := errors.AsType[*trino.Error](err)
		if !ok || perr.Name != tt.name || perr.Type != "USER_ERROR" || errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: the error is %v, want %s that does not wrap dbimp.ErrIncomplete", tt.query, err, tt.name)
		}
	}
	if err := e.readErr(t, "SELECT * FROM dbimp_nosuch"); err == nil {
		t.Error("a table that does not exist gave no error")
	}
}

// TestIntegrationTimeout holds the option WithTimeout, which sets the session
// property query_max_execution_time (D109).
func TestIntegrationTimeout(t *testing.T) {
	e := newEnv(t)
	_, err := e.db.ExecContext(t.Context(), "SELECT count(*) FROM UNNEST(sequence(1, 3000)) AS a(n) CROSS JOIN UNNEST(sequence(1, 3000)) AS b(n) CROSS JOIN UNNEST(sequence(1, 3000)) AS c(n)", trino.WithTimeout(time.Second))
	perr, ok := errors.AsType[*trino.Error](err)
	if !ok || perr.Name != "EXCEEDED_TIME_LIMIT" {
		t.Errorf("a statement past its timeout gave %v, want EXCEEDED_TIME_LIMIT", err)
	}
}

// TestIntegrationContext holds D175: when the context ends while the query runs on
// the server, the driver cancels it there, and the error is the one of the context.
func TestIntegrationContext(t *testing.T) {
	e := newEnv(t)
	mark := marker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := e.readErrCtx(ctx, mark+" SELECT count(*) FROM UNNEST(sequence(1, 3000)) AS a(n) CROSS JOIN UNNEST(sequence(1, 3000)) AS b(n) CROSS JOIN UNNEST(sequence(1, 3000)) AS c(n)")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the error is %v, want context.DeadlineExceeded", err)
	}
	if got := e.waitState(t, mark, "FAILED"); got != "FAILED" {
		t.Errorf("the query is %q after the context ended, want FAILED, because the driver cancels it", got)
	}
}

// TestIntegrationConcurrent holds that two queries run at the same time on one
// sql.DB.
func TestIntegrationConcurrent(t *testing.T) {
	e := newEnv(t)
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			var got int64
			err := e.db.QueryRowContext(t.Context(), "SELECT count(*) FROM UNNEST(sequence(1, ?)) AS t(n)", i*1000+1).Scan(&got)
			if err == nil && got != int64(i*1000+1) {
				err = fmt.Errorf("the count is %d, want %d", got, i*1000+1)
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
