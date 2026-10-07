package drill_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/drill"
)

// These tests need a server. DRILL_DSN names the server for the
// administrator, and DRILL_ORDINARY_DSN for the ordinary user (D9). Each is the
// url of dbrun, which is drill://user:pass@host:port, and the tests read it as
// the DSN of the driver. The server runs CREATE TABLE AS and refuses INSERT,
// UPDATE and DELETE (D163), so each test makes its tables with CREATE TABLE AS
// in dfs.tmp, reads them, and drops them. A test skips when a variable that it
// needs is empty. Each table of the tests starts with a prefix of its own,
// which TestMain drops at the end.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each table of this run.
var prefix = "dbimp_it_" + suffix + "_"

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "DRILL_DSN"}
	ordinary   = principal{"ordinary", "DRILL_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// toDSN returns the URL of dbrun as the DSN of the driver. A URL whose scheme
// is already drill stays as it is.
func toDSN(v string) string {
	switch {
	case strings.HasPrefix(v, "http://"):
		return "drill://" + strings.TrimPrefix(v, "http://")
	case strings.HasPrefix(v, "https://"):
		return "drill://" + strings.TrimPrefix(v, "https://") + "?tls=true"
	}
	return v
}

// dsn returns the DSN of p, or skips the test when it is empty.
func dsn(t *testing.T, p principal) string {
	t.Helper()
	v := os.Getenv(p.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	return toDSN(v)
}

// openAs opens the server as p, with the query of a DSN.
func openAs(t *testing.T, p principal, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open(drill.Name, dsn(t, p)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// forEach runs f as a subtest for each principal.
func forEach(t *testing.T, f func(t *testing.T, p principal, db *sql.DB)) {
	t.Helper()
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			f(t, p, openAs(t, p, ""))
		})
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, "dropping the tables of the tests:", err)
		code = 1
	}
	os.Exit(code)
}

// cleanup drops each table of this run, and makes sure that none is left.
func cleanup() error {
	v := os.Getenv(admin.env)
	if v == "" {
		return nil
	}
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open(drill.Name, toDSN(v))
	if err != nil {
		return err
	}
	defer db.Close()
	left, err := tables(ctx, db)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range left {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS dfs.tmp.`"+name+"`"); err != nil {
			errs = append(errs, err)
		}
	}
	if left, err = tables(ctx, db); err != nil {
		errs = append(errs, err)
	} else if len(left) > 0 {
		errs = append(errs, fmt.Errorf("the tables %v are left", left))
	}
	return errors.Join(errs...)
}

// tables returns the names of the tables of this run in dfs.tmp.
func tables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SHOW FILES IN dfs.tmp")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if name, _ := vals[0].(string); strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names, rows.Err()
}

// name returns a table name for the test, with the prefix of this run and the
// principal, so that two principals never share a table. The name has no
// character that needs a quote.
func name(p principal, what string) string {
	return prefix + p.name[:3] + "_" + what
}

// exec runs a statement, and fails the test if it fails.
func exec(t *testing.T, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return res
}

// ctas makes the table dfs.tmp.<table> from a query, and drops it when the test
// ends, even if it fails (step 14a). A table whose name has a slash is a
// subdirectory of a table that is a directory, and the cleanup drops the whole
// directory, because the server cannot drop a directory that holds no file
// (measured).
func ctas(t *testing.T, db *sql.DB, table, query string, args ...any) {
	t.Helper()
	drop(t.Context(), t, db, table)
	exec(t, db, "CREATE TABLE dfs.tmp.`"+table+"` AS "+query, args...)
	t.Cleanup(func() {
		// The context of the test ends before its cleanup runs.
		dir, _, _ := strings.Cut(table, "/")
		drop(context.WithoutCancel(t.Context()), t, db, dir)
	})
}

// drop drops the table dfs.tmp.<table> if it exists.
func drop(ctx context.Context, t *testing.T, db *sql.DB, table string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS dfs.tmp.`"+table+"`"); err != nil {
		t.Errorf("dropping %s: %v", table, err)
	}
}

// all runs a query and returns its rows, with each value read into a *any.
func all(t *testing.T, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, query string, args ...any) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// queryErr runs a query, reads its rows to the end, and returns its error.
func queryErr(ctx context.Context, db *sql.DB, query string, args ...any) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// refused runs a statement that the server refuses, and returns the error of
// Drill, or fails the test if the statement ran or failed in another way.
func refused(t *testing.T, db *sql.DB, query string, args ...any) *drill.Error {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query, args...)
	e, ok := errors.AsType[*drill.Error](err)
	if !ok {
		t.Fatalf("%s gave %v, want an error of the server", query, err)
	}
	return e
}

// raw sends requests to the REST interface of the server with no driver, for
// what the driver hides: the request with no cookie, the profile of a query,
// and the answers that the driver does not read.
type raw struct {
	base       string
	user, pass string
	client     *http.Client
}

// newRaw returns the raw client of the DSN of p.
func newRaw(t *testing.T, p principal) *raw {
	t.Helper()
	cfg, err := drill.ParseDSN(dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	return &raw{
		base:   scheme + "://" + cfg.Host + ":" + strconv.Itoa(cfg.Port),
		user:   cfg.User,
		pass:   cfg.Password,
		client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

// do sends one request, with body as JSON or no body for nil, and returns the
// status and the body of the answer.
func (r *raw) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, r.base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth(r.user, r.pass)
	res, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, b
}

// reply is the answer to a statement, as the server writes it.
type reply struct {
	QueryID string           `json:"queryId"`
	Columns []string         `json:"columns"`
	Meta    []string         `json:"metadata"`
	Rows    []map[string]any `json:"rows"`
	State   string           `json:"queryState"`
	Message string           `json:"errorMessage"`
}

// query sends a statement with no cookie, and returns its answer.
func (r *raw) query(t *testing.T, query string) reply {
	t.Helper()
	body := map[string]any{"queryType": "SQL", "query": query, "options": map[string]string{"drill.exec.http.rest.errors.verbose": "true"}}
	status, b := r.do(t, http.MethodPost, "/query.json", body)
	var a reply
	if status != http.StatusOK || json.Unmarshal(b, &a) != nil {
		t.Fatalf("the statement %q gave HTTP %d and %s", query, status, b)
	}
	return a
}

// TestIntegrationVersion holds that both principals read the version, that
// Ping works, and that the version has the form of a release.
func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		rows := all(t, db, "SELECT version FROM sys.version")
		if v, _ := rows[0][0].(string); !regexp.MustCompile(`^\d+\.\d+\.\d+`).MatchString(v) {
			t.Errorf("the version is %v, want the form 1.22.0", rows[0][0])
		}
		viaDrillbits := all(t, db, "SELECT MIN(version) AS version FROM sys.drillbits")
		if viaDrillbits[0][0] != rows[0][0] {
			t.Errorf("sys.drillbits gave %v, want %v", viaDrillbits[0][0], rows[0][0])
		}
		if err := db.PingContext(t.Context()); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})
}

// TestIntegrationErrors holds the errors of the server: an error before any
// row has its message, an error after some rows has its message from the
// profile, and a wrong password is drill.ErrLogin.
func TestIntegrationErrors(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, err := db.ExecContext(t.Context(), "SELEC 1")
		if e, ok := errors.AsType[*drill.Error](err); !ok || e.QueryID == "" || e.Message == "" {
			t.Errorf("a syntax error gave %v, want a *drill.Error with a message and a query id", err)
		}
		err = queryErr(t.Context(), db, "SELECT * FROM dfs.tmp.`"+name(p, "nope")+"`")
		if e, ok := errors.AsType[*drill.Error](err); !ok || e.Kind != "VALIDATION ERROR" && !strings.Contains(e.Message, "not found") {
			t.Errorf("an unknown table gave %v, want an error that says that the table was not found", err)
		}
		// The first file holds 300 rows of numbers, and the second a text
		// that is not a number, so the query fails after 300 rows.
		a, b := name(p, "multi")+"/a", name(p, "multi")+"/b"
		ctas(t, db, a, "SELECT employee_id AS id, '1' AS s FROM cp.`employee.json` LIMIT 300")
		ctas(t, db, b, "SELECT employee_id + 10000 AS id, 'x' AS s FROM cp.`employee.json` LIMIT 5")
		rows, err := db.QueryContext(t.Context(), "SELECT id, CAST(s AS INT) AS v FROM dfs.tmp.`"+name(p, "multi")+"`")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		err = rows.Err()
		e, ok := errors.AsType[*drill.Error](err)
		if !ok || !errors.Is(err, dbimp.ErrIncomplete) || !strings.Contains(e.Message, "x") {
			t.Errorf("an error after %d rows gave %v, want a *drill.Error from the profile that wraps dbimp.ErrIncomplete", n, err)
		}
		if n != 300 {
			t.Errorf("read %d rows before the error, want 300", n)
		}
		// A wrong password.
		cfg, err := drill.ParseDSN(dsn(t, p))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Password += "x"
		bad := sql.OpenDB(drill.NewConnector(*cfg))
		defer bad.Close()
		if _, err := bad.ExecContext(t.Context(), "SELECT 1"); !errors.Is(err, drill.ErrLogin) {
			t.Errorf("a wrong password gave %v, want drill.ErrLogin", err)
		}
		if err := bad.PingContext(t.Context()); !errors.Is(err, drill.ErrLogin) {
			t.Errorf("Ping with a wrong password gave %v, want drill.ErrLogin", err)
		}
	})
}

// TestIntegrationPrincipals holds what the ordinary user cannot do: it cannot
// change a system option, and cannot read the storage plugins, while the
// administrator can (recorded: "alter system" and "the storage plugins").
func TestIntegrationPrincipals(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, err := db.ExecContext(t.Context(), "ALTER SYSTEM SET `exec.query.max_rows` = 0")
		status, _ := newRaw(t, p).do(t, http.MethodGet, "/storage.json", nil)
		if p == admin {
			if err != nil {
				t.Errorf("ALTER SYSTEM as the administrator gave %v", err)
			}
			if _, err := db.ExecContext(t.Context(), "ALTER SYSTEM RESET `exec.query.max_rows`"); err != nil {
				t.Errorf("ALTER SYSTEM RESET as the administrator gave %v", err)
			}
			if status != http.StatusOK {
				t.Errorf("GET /storage.json as the administrator gave HTTP %d, want 200", status)
			}
			return
		}
		if e, ok := errors.AsType[*drill.Error](err); !ok || e.Kind != "PERMISSION ERROR" {
			t.Errorf("ALTER SYSTEM as the ordinary user gave %v, want a PERMISSION ERROR", err)
		}
		if status != http.StatusInternalServerError {
			t.Errorf("GET /storage.json as the ordinary user gave HTTP %d, want 500", status)
		}
	})
}
