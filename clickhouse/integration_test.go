package clickhouse_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/clickhouse"
)

// These tests need a server. CLICKHOUSE_DSN is the DSN of a release as the
// administrator, and CLICKHOUSE_ORDINARY_DSN is the same for the ordinary user
// (D9). The `url` that `dbrun` prints holds the native port, which the driver does
// not speak, so CLICKHOUSE_SECOND_ADDRESS, which is the `secondAddress` of the
// entry, as host:port, replaces the host and the port of each DSN, as the workflow
// of CI sets it. The field `api` is the other way: the url with the scheme http and
// the HTTP port, such as http://user:password@127.0.0.1:56034, which the tests
// read as the DSN clickhouse://user:password@127.0.0.1:56034. A test skips when
// CLICKHOUSE_DSN is empty. The administrator writes in a database of this run, which TestMain
// drops at the end. The ordinary user has the grants that dbrun gives it on one
// database (`dbmeta`), and writes tables there, so each table of a test has the
// name of this run for a prefix, and TestMain drops what a test left and fails if
// it left anything.

// principal is a user of the server, and the variable that holds its DSN.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "CLICKHOUSE_DSN"}
	ordinary   = principal{"ordinary", "CLICKHOUSE_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// suffix makes the names of this run unique.
var suffix = strings.ToLower(rand.Text()[:8])

// prefix is the name of the database of the administrator in this run, and the
// start of the name of each table of this run.
var prefix = "dbimp_it_" + suffix

// release is the server that the tests run on.
type release struct {
	// version is the answer of SELECT version(), such as 25.8.33.6.
	version string
	// major and minor are the first two numbers of the version.
	major, minor int
}

// atLeast reports whether the release is major.minor or later.
func (r release) atLeast(major, minor int) bool {
	return r.major > major || r.major == major && r.minor >= minor
}

// is253 reports whether the release is 25.3, which has no UPDATE statement, no Time
// and no QBit, and refuses to update a column of the type Dynamic or JSON.
func (r release) is253() bool {
	return r.major == 25 && r.minor == 3
}

// serverState is what the tests know of the server, which they find once.
type serverState struct {
	rel release
	// ordinaryDB is the database that the ordinary user can write, or "" when it
	// has none or no ordinary DSN is set.
	ordinaryDB string
	// ordinaryUser is the user of the ordinary DSN.
	ordinaryUser string
}

var (
	stateOnce sync.Once
	state     serverState
	errState  error
)

// dsnOf returns the DSN of a principal, or "" when its variable is empty. The
// variable can hold the url that dbrun prints for the HTTP interface, which has
// the scheme http, and it can hold a DSN of the driver.
func dsnOf(p principal) string {
	v := os.Getenv(p.env)
	if v == "" {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil {
		return v
	}
	if second := os.Getenv("CLICKHOUSE_SECOND_ADDRESS"); second != "" {
		u.Host = second
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		if u.Scheme == "https" {
			q := u.Query()
			q.Set("tls", "true")
			u.RawQuery = q.Encode()
		}
		u.Scheme = clickhouse.Name
	}
	return u.String()
}

// config returns the configuration of a principal in a database.
func config(tb testing.TB, p principal, database string) clickhouse.Config {
	tb.Helper()
	cfg, err := clickhouse.ParseDSN(dsnOf(p))
	if err != nil {
		tb.Fatalf("parsing the DSN in %s: %v", p.env, err)
	}
	cfg.Database = database
	return *cfg
}

// theState finds the release, and makes the database of this run, once.
func theState(tb testing.TB) serverState {
	tb.Helper()
	if dsnOf(admin) == "" {
		tb.Skip("CLICKHOUSE_DSN is empty, so there is no server to test")
	}
	stateOnce.Do(func() {
		db := sql.OpenDB(clickhouse.NewConnector(config(tb, admin, "")))
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(tb.Context()), time.Minute)
		defer cancel()
		if errState = db.QueryRowContext(ctx, "SELECT version()").Scan(&state.rel.version); errState != nil {
			errState = fmt.Errorf("reading the version of the server: %w", errState)
			return
		}
		parts := strings.SplitN(state.rel.version, ".", 3)
		if len(parts) < 2 {
			errState = fmt.Errorf("the version %q has no release", state.rel.version)
			return
		}
		state.rel.major, _ = strconv.Atoi(parts[0])
		state.rel.minor, _ = strconv.Atoi(parts[1])
		if _, errState = db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+prefix); errState != nil {
			errState = fmt.Errorf("making the database %s: %w", prefix, errState)
			return
		}
		if dsnOf(ordinary) == "" {
			return
		}
		cfg := config(tb, ordinary, "")
		state.ordinaryUser = cfg.User
		// The ordinary user writes in the database that it has a grant for.
		rows, err := db.QueryContext(ctx, "SELECT DISTINCT database FROM system.grants WHERE user_name = @u AND access_type = 'CREATE TABLE' AND database IS NOT NULL ORDER BY database", sql.Named("u", cfg.User))
		if err != nil {
			errState = fmt.Errorf("reading the grants of %s: %w", cfg.User, err)
			return
		}
		defer rows.Close()
		for rows.Next() && state.ordinaryDB == "" {
			if errState = rows.Scan(&state.ordinaryDB); errState != nil {
				return
			}
		}
		errState = rows.Err()
	})
	if errState != nil {
		tb.Fatal(errState)
	}
	return state
}

// TestMain drops the database of this run when the tests end, drops each table
// that a test left in the database of the ordinary user, and fails if a test
// left one.
func TestMain(m *testing.M) {
	code := m.Run()
	if dsnOf(admin) != "" && state.rel.version != "" {
		if err := cleanUp(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

// cleanUp drops what the tests left, and reports it.
func cleanUp() error {
	cfg, err := clickhouse.ParseDSN(dsnOf(admin))
	if err != nil {
		return fmt.Errorf("parsing the DSN: %w", err)
	}
	db := sql.OpenDB(clickhouse.NewConnector(*cfg))
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var problems []string
	for _, database := range []string{prefix, state.ordinaryDB} {
		if database == "" {
			continue
		}
		rows, err := db.QueryContext(ctx, "SELECT database, name FROM system.tables WHERE database = @d AND startsWith(name, @p)", sql.Named("d", database), sql.Named("p", prefix))
		if err != nil {
			return fmt.Errorf("listing what the tests left in %s: %w", database, err)
		}
		left, err := readNames(rows)
		if err != nil {
			return fmt.Errorf("listing what the tests left in %s: %w", database, err)
		}
		for _, name := range left {
			_, _ = db.ExecContext(ctx, "DROP DICTIONARY IF EXISTS "+name)
			_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS "+name+" SYNC")
		}
		// Only a test of the ordinary user has a table in the database of the
		// administrator that it made by name, and each test drops its own. The
		// database of this run holds none after the tests.
		if len(left) > 0 {
			problems = append(problems, fmt.Sprintf("the tests left %d tables in %s: %v", len(left), database, left))
		}
	}
	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+prefix+" SYNC"); err != nil {
		return fmt.Errorf("dropping the database %s: %w", prefix, err)
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// readNames reads each row of a result of two columns, a database and a table,
// as database.table, and closes the rows.
func readNames(rows *sql.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d, n string
		if err := rows.Scan(&d, &n); err != nil {
			return nil, err
		}
		out = append(out, d+"."+n)
	}
	return out, rows.Err()
}

// env is what a test of a server uses: the release, the principal, and a
// database in the database that the principal writes.
type env struct {
	release

	p        principal
	db       *sql.DB
	database string
	cfg      clickhouse.Config
}

// eachPrincipal runs a test as the administrator and as the ordinary user, each in
// a subtest. The ordinary user is skipped when no ordinary DSN is set, or when it
// has no database to write.
func eachPrincipal(t *testing.T, f func(t *testing.T, e env)) {
	t.Helper()
	st := theState(t)
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			if p == ordinary && st.ordinaryDB == "" {
				t.Skip("no ordinary user can write a database (CLICKHOUSE_ORDINARY_DSN)")
			}
			f(t, newEnv(t, p))
		})
	}
}

// newEnv opens a database on the server for a principal. The database closes with
// the test.
func newEnv(t *testing.T, p principal) env {
	t.Helper()
	st := theState(t)
	database := prefix
	if p == ordinary {
		database = st.ordinaryDB
	}
	cfg := config(t, p, database)
	db := sql.OpenDB(clickhouse.NewConnector(cfg))
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	return env{release: st.rel, p: p, db: db, database: database, cfg: cfg}
}

// admin reports whether the principal is the administrator.
func (e env) admin() bool { return e.p == admin }

// name returns the name of a table of this run: the prefix, an underscore and
// name, and the database before them.
func (e env) name(name string) string {
	return e.database + "." + prefix + "_" + name
}

// table makes a table, and drops it when the test ends. ddl holds one %s, which is
// the name of the table. The table is dropped first if it exists.
func (e env) table(t *testing.T, name, ddl string) string {
	t.Helper()
	full := e.name(name)
	e.dropAtEnd(t, full)
	e.exec(t, fmt.Sprintf(ddl, full))
	return full
}

// dropAtEnd drops a table, a view or a dictionary now, if it exists, and again
// when the test ends. The context of the test ends before its cleanup runs.
func (e env) dropAtEnd(t *testing.T, full string) {
	t.Helper()
	drop := func() {
		ctx := context.WithoutCancel(t.Context())
		_, _ = e.db.ExecContext(ctx, "DROP DICTIONARY IF EXISTS "+full)
		if _, err := e.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+full+" SYNC"); err != nil {
			t.Errorf("dropping %s: %v", full, err)
		}
	}
	drop()
	t.Cleanup(drop)
}

// exec runs a statement and fails the test with its error.
func (e env) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := e.db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
}

// refusal runs a statement that the server must refuse, and returns the error of
// the server. A statement that the server accepts, or that fails for a reason
// other than the answer of the server, fails the test.
func (e env) refusal(t *testing.T, query string, args ...any) *clickhouse.Error {
	t.Helper()
	_, err := e.db.ExecContext(t.Context(), query, args...)
	if err == nil {
		t.Fatalf("%q ran, and the server must refuse it", query)
	}
	cerr, ok := errors.AsType[*clickhouse.Error](err)
	if !ok {
		t.Fatalf("%q failed with %v, want the error of the server", query, err)
	}
	return cerr
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

// scalar reads the one value of a query.
func (e env) scalar(t *testing.T, query string, args ...any) any {
	t.Helper()
	rows := e.rows(t, query, args...)
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("%q gave %v, want one value", query, rows)
	}
	return rows[0][0]
}

// count reads the count of a query that selects one number.
func (e env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	v := e.scalar(t, query, args...)
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil {
		t.Fatalf("%q gave %v, want a number", query, v)
	}
	return n
}

// raw is the answer to a request that a test sends with net/http, to see what the
// server says to a client that is not the driver.
type raw struct {
	status int
	header http.Header
	body   []byte
}

// rawClient reads the body as the server sends it, with no decompression.
var rawClient = &http.Client{
	Transport: &http.Transport{DisableCompression: true},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// send sends a request to the server of the principal, with its user and its
// password in a Basic header, and returns the answer. The method is POST with the
// body, or GET with none. The keys of query go in the query string.
func (e env) send(t *testing.T, method, path string, query url.Values, header http.Header, body []byte) raw {
	t.Helper()
	target := "http://" + e.cfg.Host + ":" + strconv.Itoa(e.cfg.Port) + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(req.Header, header)
	// A request that carries the user in its own headers sends no Basic header,
	// because the server refuses both (measured).
	if req.Header.Get("X-Clickhouse-User") == "" && req.Header.Get("Authorization") == "" {
		req.SetBasicAuth(e.cfg.User, e.cfg.Password)
	}
	res, err := rawClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("reading the answer to %s %s: %v", method, path, err)
	}
	return raw{status: res.StatusCode, header: res.Header, body: b}
}

// post sends a statement as the body of POST /, in the database of the
// environment, with the keys of query.
func (e env) post(t *testing.T, statement string, query url.Values) raw {
	t.Helper()
	q := url.Values{"database": {e.database}}
	maps.Copy(q, query)
	return e.send(t, http.MethodPost, "/", q, nil, []byte(statement))
}

// equalValue compares a value that the driver returned with the value that the
// test wants, and the Go type of each (D135). A time is equal when it is the same
// instant, a decimal and a big integer when they compare equal, a float with the
// NaN and the sign of zero, and a netip.Addr with ==.
func equalValue(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case time.Time:
		g, ok := got.(time.Time)
		return ok && g.Equal(w)
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case *big.Int:
		g, ok := got.(*big.Int)
		return ok && g.Cmp(w) == 0
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) || (g == nil) != (w == nil) {
			return false
		}
		for i := range w {
			if !equalValue(g[i], w[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok || !equalValue(gv, wv) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// poll runs cond until it is true, for at most 30 seconds, and fails the test
// when it is not. It never sleeps for a fixed time (step 14a).
func poll(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen in 30 seconds", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// versionPattern is the form of the answer of SELECT version().
var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)

// TestIntegrationVersion holds D177: the statement that the driver runs when a
// connection opens gives the version of the release, which `usql` and `dbmeta`
// read with the same statement, as each principal.
func TestIntegrationVersion(t *testing.T) {
	eachPrincipal(t, func(t *testing.T, e env) {
		v, ok := e.scalar(t, "SELECT version()").(string)
		if !ok || !versionPattern.MatchString(v) || v != e.version {
			t.Errorf("SELECT version() gave %v, want the version %q", v, e.version)
		}
		if err := e.db.PingContext(t.Context()); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})
}

// TestIntegrationErrors holds D176 and D177 for the errors that come before any
// row: the code, the name and the status, and that no error holds the password.
func TestIntegrationErrors(t *testing.T) {
	eachPrincipal(t, func(t *testing.T, e env) {
		for _, tt := range []struct {
			query  string
			code   int
			name   string
			status int
		}{
			{"SELECT * FROM " + e.database + ".nosuch_" + suffix, 60, "UNKNOWN_TABLE", http.StatusNotFound},
			{"SELEC 1", 62, "SYNTAX_ERROR", http.StatusBadRequest},
			{"SELECT intDiv(1, 0)", 153, "ILLEGAL_DIVISION", http.StatusInternalServerError},
			{"SELECT nosuchfunction(1)", 46, "UNKNOWN_FUNCTION", http.StatusNotFound},
		} {
			cerr := e.refusal(t, tt.query)
			if cerr.Code != tt.code || cerr.HTTPStatus != tt.status || cerr.Name != tt.name {
				t.Errorf("%q: the error is %+v, want the code %d, %s and HTTP %d", tt.query, cerr, tt.code, tt.name, tt.status)
			}
			if _, ok := errors.AsType[*dbimp.StatusError](cerr); !ok {
				t.Errorf("%q: the error wraps no *dbimp.StatusError", tt.query)
			}
		}
		// A database that does not exist.
		_, err := e.db.QueryContext(t.Context(), "SELECT 1", clickhouse.WithDatabase("nosuch_"+suffix)) //nolint:rowserrcheck,sqlclosecheck // The statement fails, and no rows come.
		if cerr, ok := errors.AsType[*clickhouse.Error](err); !ok || cerr.Code != 81 || cerr.HTTPStatus != http.StatusNotFound {
			t.Errorf("a database that does not exist gave %v, want the code 81 and HTTP 404", err)
		}
		// A setting that the server does not know.
		_, err = e.db.ExecContext(t.Context(), "SELECT 1", clickhouse.WithParameter("no_such_setting", 1))
		if cerr, ok := errors.AsType[*clickhouse.Error](err); !ok || cerr.Code != 115 {
			t.Errorf("a setting that does not exist gave %v, want the code 115", err)
		}
	})
}

// TestIntegrationWrongPassword holds that a wrong password is an error of the
// server, and that the error holds no password: HTTP 401 and the code 194 for the
// user default, and HTTP 403 and the code 516 for another user (measured).
func TestIntegrationWrongPassword(t *testing.T) {
	st := theState(t)
	for _, p := range principals {
		if p == ordinary && st.ordinaryDB == "" {
			continue
		}
		cfg := config(t, p, "")
		cfg.Password = "wrong-" + suffix
		db := sql.OpenDB(clickhouse.NewConnector(cfg))
		err := db.PingContext(t.Context())
		db.Close()
		cerr, ok := errors.AsType[*clickhouse.Error](err)
		if !ok {
			t.Errorf("%s: a wrong password gave %v, want the error of the server", p.name, err)
			continue
		}
		want, wantCode := http.StatusForbidden, 516
		if cfg.User == "default" {
			want, wantCode = http.StatusUnauthorized, 194
		}
		if cerr.HTTPStatus != want || cerr.Code != wantCode {
			t.Errorf("%s: the error is %+v, want HTTP %d and the code %d", p.name, cerr, want, wantCode)
		}
		if strings.Contains(err.Error(), cfg.Password) {
			t.Errorf("%s: the error %q holds the password", p.name, err)
		}
	}
}

// TestIntegrationOrdinaryUser holds the privileges of the ordinary user that
// dbrun makes: it cannot read system.users, and a read of what it has no grant
// for is a refusal with the code 497, which is HTTP 500 on 25.3 and 25.8 and
// HTTP 403 on 26.8 and 26.9 (measured).
func TestIntegrationOrdinaryUser(t *testing.T) {
	st := theState(t)
	if st.ordinaryDB == "" {
		t.Skip("no ordinary user can write a database (CLICKHOUSE_ORDINARY_DSN)")
	}
	e := newEnv(t, ordinary)
	cerr := e.refusal(t, "SELECT name FROM system.users")
	wantStatus := http.StatusInternalServerError
	if e.atLeast(26, 8) {
		wantStatus = http.StatusForbidden
	}
	if cerr.Code != 497 || cerr.HTTPStatus != wantStatus {
		t.Errorf("a read of system.users gave %+v, want the code 497 and HTTP %d", cerr, wantStatus)
	}
	// The ordinary user reads its own queries.
	if n := e.count(t, "SELECT count() FROM system.processes WHERE query_id != ''"); n < 1 {
		t.Errorf("the ordinary user sees %d queries in system.processes, want its own", n)
	}
	if cerr := e.refusal(t, "CREATE DATABASE "+prefix+"_x"); cerr.Code != 497 {
		t.Errorf("a create of a database gave %+v, want the code 497", cerr)
	}
	e.exec(t, "SELECT 1")
	// The ordinary user cannot use a table of the administrator.
	adm := newEnv(t, admin)
	tbl := adm.table(t, "private", "CREATE TABLE %s (a Int32) ENGINE = Memory")
	_, err := e.db.ExecContext(t.Context(), "SELECT * FROM "+tbl)
	if cerr, ok := errors.AsType[*clickhouse.Error](err); !ok || (cerr.Code != 497 && cerr.Code != 60) {
		t.Errorf("a read of a table in a database with no grant gave %v, want the code 497 or 60", err)
	}
}

// TestIntegrationLargeResults holds that a result of 20000 rows arrives whole and
// in order, as each principal, with the types of the columns, and that
// Rows.ColumnTypes names them.
func TestIntegrationLargeResults(t *testing.T) {
	eachPrincipal(t, func(t *testing.T, e env) {
		rows, err := e.db.QueryContext(t.Context(), "SELECT number AS n, toString(number) AS s, toBool(number % 7 = 0) AS b, toDecimal64(number, 3) AS d, if(number % 10 = 0, NULL, number) AS nl FROM numbers(20000)")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cts, err := rows.ColumnTypes()
		if err != nil {
			t.Fatal(err)
		}
		var names, nullable []string
		for _, ct := range cts {
			n, _ := ct.Nullable()
			names = append(names, ct.DatabaseTypeName()+" "+ct.ScanType().String())
			nullable = append(nullable, strconv.FormatBool(n))
		}
		wantNames := []string{"UINT64 uint64", "STRING string", "BOOL bool", "DECIMAL *apd.Decimal", "UINT64 uint64"}
		if !reflect.DeepEqual(names, wantNames) {
			t.Errorf("the column types are %q, want %q", names, wantNames)
		}
		if got := strings.Join(nullable, ","); got != "false,false,false,false,true" {
			t.Errorf("the nullable flags are %s, want false, false, false, false and true", got)
		}
		if p, s, ok := cts[3].DecimalSize(); !ok || p != 18 || s != 3 {
			t.Errorf("the decimal size is %d, %d, %v, want 18, 3", p, s, ok)
		}
		n := 0
		for rows.Next() {
			var (
				a  uint64
				s  string
				b  bool
				d  apd.Decimal
				nl sql.Null[uint64]
			)
			if err := rows.Scan(&a, &s, &b, &d, &nl); err != nil {
				t.Fatal(err)
			}
			if a != uint64(n) || s != strconv.Itoa(n) || b != (n%7 == 0) || d.Cmp(apd.New(int64(n), 0)) != 0 || nl.Valid != (n%10 != 0) || (nl.Valid && nl.V != uint64(n)) {
				t.Fatalf("row %d is %d, %q, %v, %v and %v", n, a, s, b, &d, nl)
			}
			n++
		}
		if err := rows.Err(); err != nil || n != 20000 {
			t.Errorf("read %d rows and %v, want 20000", n, err)
		}
	})
}

// TestIntegrationErrorAfterRows holds D176: an error after some rows reaches the
// caller after exactly those rows, with the code of the server, and wraps
// dbimp.ErrIncomplete. The statement writes about 20 MiB before the error, so the
// status of HTTP is 200 by then, and the marker carries the error, with the tag on
// 26.8 and 26.9 (measured).
func TestIntegrationErrorAfterRows(t *testing.T) {
	eachPrincipal(t, func(t *testing.T, e env) {
		rows, err := e.db.QueryContext(t.Context(), "SELECT number, throwIf(number = 1500000) FROM numbers(2000000)",
			clickhouse.WithParameter("max_block_size", 10000), clickhouse.WithParameter("output_format_parallel_formatting", 0))
		if err != nil {
			t.Fatalf("the error came before any row: %v", err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		err = rows.Err()
		cerr, ok := errors.AsType[*clickhouse.Error](err)
		if !ok || cerr.Code != 395 || cerr.HTTPStatus != http.StatusOK || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("the error is %v, want the code 395 with HTTP 200, wrapped with dbimp.ErrIncomplete", err)
		}
		if n == 0 || n > 1500000 {
			t.Errorf("read %d rows before the error, want some, and at most 1500000", n)
		}
	})
}

// TestIntegrationCancel holds D176: a query whose context ends is gone from the
// server soon after, whether rows flow or not, and so is a query whose rows the
// caller closes early. The test names each query, and reads system.processes,
// which the ordinary user can read for its own queries.
func TestIntegrationCancel(t *testing.T) {
	eachPrincipal(t, func(t *testing.T, e env) {
		running := func(id string) bool {
			return e.count(t, "SELECT count() FROM system.processes WHERE query_id = ?", id) > 0
		}
		for _, tt := range []struct {
			name  string
			query string
			flows bool
		}{
			{"rows flow", "SELECT sleepEachRow(0.2) FROM numbers(300)", true},
			{"no row flows", "SELECT sum(sleepEachRow(0.2)) FROM numbers(300)", false},
			{"before the first byte", "SELECT sleep(3) + sleep(3) + sleep(3)", false},
		} {
			id := "dbimp-it-" + suffix + "-" + strings.ReplaceAll(tt.name, " ", "-")
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			start := time.Now()
			rows, err := e.db.QueryContext(ctx, tt.query, clickhouse.WithParameter("query_id", id), clickhouse.WithParameter("max_block_size", 1))
			if err == nil {
				for rows.Next() {
				}
				err = rows.Err()
				rows.Close() //nolint:sqlclosecheck // The rows end here, to see what the cancel does after the read.
			}
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) {
				t.Errorf("%s: the error is %v, want context.DeadlineExceeded", tt.name, err)
			}
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("%s: the call took %v to end, want about 1 second", tt.name, d)
			}
			poll(t, tt.name+": the query to be gone", func() bool { return !running(id) })
		}
		// Rows that the caller closes early.
		id := "dbimp-it-" + suffix + "-close"
		rows, err := e.db.QueryContext(t.Context(), "SELECT sleepEachRow(0.2) FROM numbers(300)", clickhouse.WithParameter("query_id", id), clickhouse.WithParameter("max_block_size", 1))
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatalf("no first row: %v", rows.Err())
		}
		if !running(id) {
			t.Fatal("the query is not running after its first row")
		}
		if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test closes the rows before the end, to see what Close sends.
			t.Fatal(err)
		}
		// Close sends KILL QUERY SYNC before it returns, so the query is gone.
		poll(t, "the query to be gone after Close", func() bool { return !running(id) })
		// The connection of the pool is fine for the next statement.
		if got := e.scalar(t, "SELECT 1"); got != int64(1) {
			t.Errorf("SELECT 1 gave %#v after the cancel", got)
		}
	})
}

// TestIntegrationConcurrent holds that many queries run at the same time on one
// database, each with its own answer.
func TestIntegrationConcurrent(t *testing.T) {
	eachPrincipal(t, func(t *testing.T, e env) {
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Go(func() {
				var s string
				var n int64
				if err := e.db.QueryRowContext(t.Context(), "SELECT toString(?) AS s, sleep(0.05) + ? AS n", int64(i), int64(i)).Scan(&s, &n); err != nil {
					t.Errorf("query %d: %v", i, err)
					return
				}
				if s != strconv.Itoa(i) || n != int64(i) {
					t.Errorf("query %d gave %q and %d", i, s, n)
				}
			})
		}
		wg.Wait()
	})
}

// TestIntegrationTimeout holds the option WithTimeout: the server stops a statement
// that runs past it with the code 159, which is HTTP 408 on 26.8 and 26.9 and an error
// after the rows on 25.3 and 25.8 (measured).
func TestIntegrationTimeout(t *testing.T) {
	eachPrincipal(t, func(t *testing.T, e env) {
		_, err := e.db.ExecContext(t.Context(), "SELECT sleep(3) + sleep(3)", clickhouse.WithTimeout(time.Second))
		cerr, ok := errors.AsType[*clickhouse.Error](err)
		if !ok || cerr.Code != 159 {
			t.Errorf("a statement that runs past its timeout gave %v, want the code 159", err)
		}
	})
}
