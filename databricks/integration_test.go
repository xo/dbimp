package databricks_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databricks"
)

// These tests need a workspace. DATABRICKS_DSN is the DSN of the driver, in the
// form of D193: databricks://token:<personal access token>@<workspace host>/<warehouse id>?catalog=c&schema=s,
// with the token of the login as the password. A test skips when DATABRICKS_DSN is
// empty (hard rule 9). Databricks is a hosted service that dbrun does not start, so
// a person runs these tests on a workspace with a login that owns the schema of
// the DSN (D188 and D193 item 10), and the workflow of CI has no job for them. The
// login is one service principal, so each test runs as that principal only, and
// the manifest of testdata/databricks has no ordinary user.
//
// The tests make their tables, views and functions in the catalog and the schema of
// the DSN, with the name of this run for a prefix. TestMain drops what a test left,
// and fails if it left anything. The warehouse of the free workspace stops after 10
// minutes, and the first statement after it wakes it, which takes about 15 seconds,
// so the first test is slow. The workspace has a daily quota of compute, so a person
// runs the tests once a day at most.

// envDSN is the variable that holds the DSN.
const envDSN = "DATABRICKS_DSN"

// suffix makes the names of this run unique.
var suffix = strings.ToLower(rand.Text()[:8])

// prefix is the start of the name of each table of this run.
var prefix = "dbimp_it_" + suffix

// table returns the name of a table of this run.
func table(name string) string {
	return prefix + "_" + name
}

// dsn returns the DSN of the workspace, and skips the test when there is none.
func dsn(t testing.TB) string {
	t.Helper()
	v := os.Getenv(envDSN)
	if v == "" {
		t.Skipf("%s is empty, so there is no workspace to test against", envDSN)
	}
	return v
}

// integrationConfig returns the configuration of the workspace.
func integrationConfig(t testing.TB) databricks.Config {
	t.Helper()
	cfg, err := databricks.ParseDSN(dsn(t))
	if err != nil {
		t.Fatalf("reading %s: %v", envDSN, err)
	}
	return *cfg
}

// connect returns a database on the workspace.
func connect(t testing.TB) *sql.DB {
	t.Helper()
	db := sql.OpenDB(databricks.NewConnector(integrationConfig(t)))
	// Closing the database closes the connector.
	t.Cleanup(func() { db.Close() })
	return db
}

// TestMain drops each table, view and function of this run that a test left, and
// fails if it found one.
func TestMain(m *testing.M) {
	code := m.Run()
	if left := leftovers(); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "the tests left %d objects with the prefix %s, which TestMain dropped: %v\n", len(left), prefix, left)
		code = 1
	}
	os.Exit(code)
}

// leftovers drops each object of this run, and returns their names. It returns none
// when there is no workspace.
func leftovers() []string {
	v := os.Getenv(envDSN)
	if v == "" {
		return nil
	}
	cfg, err := databricks.ParseDSN(v)
	if err != nil {
		return nil
	}
	db := sql.OpenDB(databricks.NewConnector(*cfg))
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var left []string
	// SHOW TABLES lists the tables and the views, with the name in its second
	// column (measured).
	names, err := firstStrings(ctx, db, "SHOW TABLES LIKE '"+prefix+"*'", 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "looking for the tables that the tests left: %v\n", err)
		return []string{"?"}
	}
	for _, name := range names {
		left = append(left, name)
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+name); err != nil {
			if _, err := db.ExecContext(ctx, "DROP VIEW IF EXISTS "+name); err != nil {
				fmt.Fprintf(os.Stderr, "dropping %s: %v\n", name, err)
			}
		}
	}
	// SHOW USER FUNCTIONS has the full name of each function in its one column.
	funcs, err := firstStrings(ctx, db, "SHOW USER FUNCTIONS LIKE '"+prefix+"*'", 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "looking for the functions that the tests left: %v\n", err)
		return append(left, "?")
	}
	for _, name := range funcs {
		left = append(left, name)
		if _, err := db.ExecContext(ctx, "DROP FUNCTION IF EXISTS "+name); err != nil {
			fmt.Fprintf(os.Stderr, "dropping %s: %v\n", name, err)
		}
	}
	return left
}

// firstStrings returns the text of column col of each row of a statement.
func firstStrings(ctx context.Context, db *sql.DB, q string, col int) ([]string, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		if s, ok := vals[col].(string); ok {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// exec runs a statement, and fails the test if it fails.
func exec(t testing.TB, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return res
}

// drop drops the objects when the test ends, even when it fails. kind is TABLE, VIEW or
// FUNCTION. The context of the test ends before its cleanup runs, so the drop uses
// another.
func drop(t testing.TB, db *sql.DB, kind string, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range names {
			if _, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP "+kind+" IF EXISTS "+name); err != nil {
				t.Errorf("dropping %s: %v", name, err)
			}
		}
	})
}

// query runs a statement and reads every row into *any.
func query(t testing.TB, db *sql.DB, q string, args ...any) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
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
			t.Fatalf("%s: scanning: %v", q, err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return out
}

// queryError runs a statement, reads its rows to the end, and returns the error that the
// statement or the rows gave.
func queryError(ctx context.Context, db *sql.DB, q string, args ...any) error {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// scalar runs a statement that returns one value.
func scalar(t testing.TB, db *sql.DB, q string, args ...any) any {
	t.Helper()
	rows := query(t, db, q, args...)
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("%s: got %v, want one value", q, rows)
	}
	return rows[0][0]
}

// dec makes a decimal.
func dec(t testing.TB, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("reading %q as a decimal: %v", s, err)
	}
	return d
}

// same reports whether two values are the same: a decimal by its number, a time by its
// instant and its offset, a NaN with a NaN, and a list and a map by their members.
func same(got, want any) bool {
	switch w := want.(type) {
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case time.Time:
		g, ok := got.(time.Time)
		if !ok {
			return false
		}
		_, og := g.Zone()
		_, ow := w.Zone()
		return g.Equal(w) && og == ow
	case float64:
		g, ok := got.(float64)
		return ok && (g == w || math.IsNaN(g) && math.IsNaN(w))
	case []any:
		g, ok := got.([]any)
		return ok && slices.EqualFunc(g, w, same)
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for k, v := range w {
			if gv, ok := g[k]; !ok || !same(gv, v) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// serverError returns err as a *databricks.Error, and fails the test if it is not one.
func serverError(t testing.TB, err error) *databricks.Error {
	t.Helper()
	e, ok := errors.AsType[*databricks.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *databricks.Error", err, err)
	}
	return e
}

// rest sends a request to the REST API of the workspace with the token of the DSN, for
// the facts of the server that the driver does not expose, such as a limit of rows or a
// format of the result. It returns the status and the decoded JSON object of the
// answer, and an empty object for an answer that is no object.
func rest(t testing.TB, method, path string, body any) (int, map[string]any) {
	t.Helper()
	status, _, obj := restRaw(t, method, path, body)
	return status, obj
}

// restRaw is rest, and it returns the headers of the answer too. The transport does
// not decompress, so a test that asks for gzip sees the header Content-Encoding.
func restRaw(t testing.TB, method, path string, body any, headers ...string) (int, http.Header, map[string]any) {
	t.Helper()
	cfg := integrationConfig(t)
	scheme := "https"
	if cfg.Insecure {
		scheme = "http"
	}
	u := scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + path
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, u, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	client := &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{DisableCompression: true, Proxy: http.ProxyFromEnvironment}}
	defer client.CloseIdleConnections()
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	var obj map[string]any
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s %s: reading the answer: %v", method, path, err)
	}
	if strings.Contains(res.Header.Get("Content-Encoding"), "gzip") {
		return res.StatusCode, res.Header, map[string]any{"gzip": true, "bytes": float64(len(data))}
	}
	_ = json.Unmarshal(data, &obj)
	return res.StatusCode, res.Header, obj
}

// stmtBody is the body of a statement for rest, with the warehouse, the catalog and the
// schema of the DSN.
func stmtBody(t testing.TB, statement string, extra map[string]any) map[string]any {
	t.Helper()
	cfg := integrationConfig(t)
	b := map[string]any{"warehouse_id": cfg.Warehouse, "statement": statement, "wait_timeout": "50s"}
	if cfg.Catalog != "" {
		b["catalog"] = cfg.Catalog
	}
	if cfg.Schema != "" {
		b["schema"] = cfg.Schema
	}
	maps.Copy(b, extra)
	return b
}

// state returns the state of the status of an answer of the API.
func state(obj map[string]any) string {
	st, _ := obj["status"].(map[string]any)
	s, _ := st["state"].(string)
	return s
}

// TestIntegrationConnect holds that the login works, that Ping runs, that the version
// answers, and that the catalog and the schema of a statement are those of the DSN or
// of an option. The statement that usql runs for the version is none, because usql
// has no Version statement for Databricks, so the test runs SELECT version() (step
// 16). The login has no ordinary user to compare with (the manifest says so).
func TestIntegrationConnect(t *testing.T) {
	db := connect(t)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	version, ok := scalar(t, db, "SELECT version()").(string)
	if !ok || version == "" {
		t.Errorf("the version is %q", version)
	}
	t.Logf("the workspace runs Databricks SQL %s", version)
	cfg := integrationConfig(t)
	if cfg.Catalog != "" {
		if got := scalar(t, db, "SELECT current_catalog()"); got != cfg.Catalog {
			t.Errorf("the catalog is %v, want %s", got, cfg.Catalog)
		}
	}
	if cfg.Schema != "" {
		if got := scalar(t, db, "SELECT current_schema()"); got != cfg.Schema {
			t.Errorf("the schema is %v, want %s", got, cfg.Schema)
		}
		// Spark calls a schema a database.
		if got := scalar(t, db, "SELECT current_database()"); got != cfg.Schema {
			t.Errorf("the database is %v, want %s", got, cfg.Schema)
		}
	}
	// WithSchema and WithDatabase change one statement, and WithCatalog too.
	if got := scalar(t, db, "SELECT current_schema()", databricks.WithSchema("information_schema")); got != "information_schema" {
		t.Errorf("the schema of the statement is %v, want information_schema", got)
	}
	if got := scalar(t, db, "SELECT current_schema()", databricks.WithDatabase("default")); got != "default" {
		t.Errorf("the database of the statement is %v, want default", got)
	}
	if got := scalar(t, db, "SELECT current_schema()"); cfg.Schema != "" && got != cfg.Schema {
		t.Errorf("the next statement kept the schema %v", got)
	}
}

// beforeRows runs a statement that fails, and returns its error. It fails the test if the
// error does not come from the query itself, before any row.
func beforeRows(t testing.TB, db *sql.DB, q string) error {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
		}
		t.Fatalf("%s: the error came after a row, or there was none: %v", q, rows.Err())
	}
	return err
}

// TestIntegrationErrors holds D193: an error of the server is an *databricks.Error with
// its code, its SQLSTATE and its message, before any row, even for a statement that
// fails after the engine read some rows.
func TestIntegrationErrors(t *testing.T) {
	db := connect(t)
	for _, tt := range []struct {
		query    string
		sqlState string
		message  string
	}{
		{"SELEC 1", "42601", "PARSE_SYNTAX_ERROR"},
		{"SELECT * FROM " + table("nosuch"), "42P01", "TABLE_OR_VIEW_NOT_FOUND"},
		{"SELECT nosuch FROM range(1)", "42703", "UNRESOLVED_COLUMN"},
		{"SELECT 1/0", "22012", "DIVIDE_BY_ZERO"},
		{"SELECT CAST('abc' AS INT)", "22018", "CAST_INVALID_INPUT"},
		// The statement fails in the last row, and the server gives the error and no
		// row (measured).
		{"SELECT id, 1 / (CASE WHEN id = 2999 THEN 0 ELSE 1 END) AS q FROM range(3000)", "22012", "DIVIDE_BY_ZERO"},
		{"SELECT raise_error('dbimp raised on purpose')", "P0001", "dbimp raised on purpose"},
		// Two statements in one request are a syntax error (measured).
		{"SELECT 1 AS a; SELECT 2 AS b", "42601", "extra input"},
	} {
		err := beforeRows(t, db, tt.query)
		e := serverError(t, err)
		if e.StatementID == "" || e.Code != "BAD_REQUEST" || e.HTTPStatus != http.StatusOK {
			t.Errorf("%s: the error is %+v, want HTTP 200, BAD_REQUEST and an id", tt.query, *e)
		}
		if e.SQLState != tt.sqlState || !strings.Contains(e.Message, tt.message) {
			t.Errorf("%s: the error is %+v, want the SQLSTATE %s and %q in the message", tt.query, *e, tt.sqlState, tt.message)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", tt.query)
		}
	}
	// A warehouse that the login cannot use gets HTTP 403, and a wrong token too.
	cfg := integrationConfig(t)
	bad := cfg
	bad.Warehouse = "0000000000000000"
	other := sql.OpenDB(databricks.NewConnector(bad))
	defer other.Close()
	_, err := other.ExecContext(t.Context(), "SELECT 1")
	if e := serverError(t, err); e.HTTPStatus != http.StatusForbidden || e.Code != "PERMISSION_DENIED" {
		t.Errorf("a warehouse that the login cannot use gave %+v, want HTTP 403 and PERMISSION_DENIED", *e)
	}
	bad = cfg
	bad.Token = "dapi" + strings.Repeat("0", 32)
	wrong := sql.OpenDB(databricks.NewConnector(bad))
	defer wrong.Close()
	_, err = wrong.ExecContext(t.Context(), "SELECT 1")
	if e := serverError(t, err); e.HTTPStatus != http.StatusForbidden || e.Code != "403" {
		t.Errorf("a wrong token gave %+v, want HTTP 403 and the code 403 as text", *e)
	}
}

// TestIntegrationTransactions holds D193 item 5: BeginTx fails with
// dbimp.ErrNotSupported, and a transaction does not last across requests.
func TestIntegrationTransactions(t *testing.T) {
	db := connect(t)
	if tx, err := db.BeginTx(t.Context(), nil); err == nil {
		_ = tx.Rollback()
		t.Error("BeginTx returned a transaction")
	} else if !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
	}
	exec(t, db, "BEGIN TRANSACTION")
	_, err := db.ExecContext(t.Context(), "COMMIT")
	if e := serverError(t, err); !strings.Contains(e.Message, "NO_ACTIVE_TRANSACTION") {
		t.Errorf("COMMIT gave %+v, want NO_ACTIVE_TRANSACTION", *e)
	}
}

// TestIntegrationContext holds D36 and D193 item 8: a context that ends while a
// statement runs stops the wait, with the error of the context, and the driver
// cancels the statement on the server. The statement is long, so it ends only by its
// cancel.
func TestIntegrationContext(t *testing.T) {
	db := connect(t)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, heavy)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a long statement with a deadline of 8 seconds gave %v, want context.DeadlineExceeded", err)
	}
	if took := time.Since(start); took > 30*time.Second {
		t.Errorf("the statement took %v to stop, want about 8 seconds", took)
	}
	// WithTimeout is the same through the option.
	_, err = db.ExecContext(t.Context(), heavy, databricks.WithTimeout(5*time.Second))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a long statement with WithTimeout gave %v, want context.DeadlineExceeded", err)
	}
	// The same connection pool still works.
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping after the cancel: %v", err)
	}
}

// heavy is a statement that runs for much longer than any test waits, so that only a
// cancel ends it.
const heavy = "SELECT sum(hash(a.id, b.id)) FROM range(1000000) a CROSS JOIN range(1000000) b"

// TestIntegrationParameters binds each Go type that the driver names, and reads the
// value back in the Go type of the column (D193 item 9).
func TestIntegrationParameters(t *testing.T) {
	db := connect(t)
	at := time.Date(2026, 10, 9, 12, 34, 56, 123456789, time.FixedZone("", 7*3600))
	for _, tt := range []struct {
		name  string
		query string
		arg   any
		want  any
	}{
		{"int64", "SELECT CAST(? AS BIGINT)", int64(-42), int64(-42)},
		{"float64", "SELECT CAST(? AS DOUBLE)", 1.5, 1.5},
		{"string", "SELECT CAST(? AS STRING)", "héllo", "héllo"},
		{"bool", "SELECT CAST(? AS BOOLEAN)", true, true},
		{"dbimp.Date", "SELECT CAST(? AS DATE)", dbimp.Date{Year: 2026, Month: time.October, Day: 9}, dbimp.Date{Year: 2026, Month: time.October, Day: 9}},
		{"dbimp.LocalDateTime", "SELECT CAST(? AS TIMESTAMP_NTZ)", dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: time.October, Day: 9}, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123000000}}, dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: time.October, Day: 9}, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123000000}}},
		{"time.Time", "SELECT CAST(? AS TIMESTAMP)", at, time.Date(2026, 10, 9, 5, 34, 56, 123000000, time.UTC)},
		{"*apd.Decimal", "SELECT CAST(? AS DECIMAL(38,10))", dec(t, "12345678.91"), dec(t, "12345678.91")},
		{"dbimp.Interval", "SELECT CAST(? AS INTERVAL YEAR TO MONTH)", dbimp.Interval{Months: 14}, dbimp.Interval{Months: 14}},
		{"NULL", "SELECT CAST(? AS STRING)", nil, nil},
	} {
		if got := scalar(t, db, tt.query, tt.arg); !same(got, tt.want) {
			t.Errorf("%s: got %#v (%T), want %#v (%T)", tt.name, got, got, tt.want, tt.want)
		}
	}
	// A NULL has no Go type to name its type, so the driver sends the type VOID, and
	// a column of a number takes it too.
	tbl := table("null_param")
	exec(t, db, "CREATE TABLE "+tbl+" (a INT, b STRING, c DATE)")
	drop(t, db, "TABLE", tbl)
	exec(t, db, "INSERT INTO "+tbl+" VALUES (?, ?, ?)", nil, nil, nil)
	if rows := query(t, db, "SELECT a, b, c FROM "+tbl); len(rows) != 1 || rows[0][0] != nil || rows[0][1] != nil || rows[0][2] != nil {
		t.Errorf("a row of NULL parameters read back as %v", rows)
	}
	// A byte slice, a list and a map have no parameter type that the server takes.
	for name, arg := range map[string]any{"a byte slice": []byte{1}, "a list": []any{1}, "a map": map[string]any{"a": 1}} {
		if _, err := db.ExecContext(t.Context(), "SELECT ?", arg); !errors.Is(err, dbimp.ErrArguments) {
			t.Errorf("%s gave %v, want dbimp.ErrArguments", name, err)
		}
	}
}
