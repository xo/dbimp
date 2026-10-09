package snowflake_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/snowflake"
)

// These tests need an account. SNOWFLAKE_DSN is the DSN of the driver, in the form
// of D183: snowflake://user:key@<org>-<account>.snowflakecomputing.com/database/schema,
// with the private key of the user as the password, as the base64url text of its
// PKCS8 DER bytes, and the role and the warehouse as the keys role and warehouse. A
// test skips when SNOWFLAKE_DSN is empty (hard rule 9). Snowflake is a hosted
// service that dbrun does not start, so a person runs these tests on an account
// that holds the login that dbsetup made for this work (dbmeta D117), and the
// workflow of CI has no job for them (D183 item 13). The login is one user with one
// role, so each test runs as that user only.
//
// The tests make their tables in the database and the schema of the DSN, with the
// name of this run for a prefix. TestMain drops what a test left, and fails if it
// left anything. The time zone of the session is UTC for every test, so that a
// timestamp_ltz value and a CURRENT_TIMESTAMP have one zone.

// envDSN is the variable that holds the DSN.
const envDSN = "SNOWFLAKE_DSN"

// suffix makes the names of this run unique.
var suffix = strings.ToUpper(rand.Text()[:8])

// prefix is the start of the name of each table of this run.
var prefix = "DBIMP_IT_" + suffix

// table returns the name of a table of this run.
func table(name string) string {
	return prefix + "_" + strings.ToUpper(name)
}

// dsn returns the DSN of the account, and skips the test when there is none.
func dsn(t testing.TB) string {
	t.Helper()
	v := os.Getenv(envDSN)
	if v == "" {
		t.Skipf("%s is empty, so there is no account to test against", envDSN)
	}
	return v
}

// integrationConfig returns the configuration of the account, with the time zone
// UTC.
func integrationConfig(t testing.TB) snowflake.Config {
	t.Helper()
	cfg, err := snowflake.ParseDSN(dsn(t))
	if err != nil {
		t.Fatalf("reading %s: %v", envDSN, err)
	}
	cfg.TimeZone = "UTC"
	return *cfg
}

// connect returns a database on the account.
func connect(t testing.TB) *sql.DB {
	t.Helper()
	db := sql.OpenDB(snowflake.NewConnector(integrationConfig(t)))
	// Closing the database closes the connector.
	t.Cleanup(func() { db.Close() })
	return db
}

// TestMain drops each table of this run that a test left, and fails if it found
// one.
func TestMain(m *testing.M) {
	code := m.Run()
	if left := leftovers(); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "the tests left %d tables with the prefix %s, which TestMain dropped: %v\n", len(left), prefix, left)
		code = 1
	}
	os.Exit(code)
}

// leftovers drops each table of this run, and returns their names. It returns
// none when there is no account.
func leftovers() []string {
	v := os.Getenv(envDSN)
	if v == "" {
		return nil
	}
	cfg, err := snowflake.ParseDSN(v)
	if err != nil {
		return nil
	}
	db := sql.OpenDB(snowflake.NewConnector(*cfg))
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	names, err := listTables(ctx, db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "looking for the tables that the tests left: %v\n", err)
		return []string{"?"}
	}
	for _, name := range names {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+name); err != nil {
			fmt.Fprintf(os.Stderr, "dropping %s: %v\n", name, err)
		}
	}
	return names
}

// listTables returns the name of each table of this run.
func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SHOW TABLES LIKE '"+prefix+"%'")
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
		// The columns of SHOW TABLES are created_on, name, and others.
		if name, ok := vals[1].(string); ok {
			names = append(names, name)
		}
	}
	return names, rows.Err()
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

// drop drops the tables when the test ends, even when it fails. The context of
// the test ends before its cleanup runs, so the drop uses another.
func drop(t testing.TB, db *sql.DB, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range names {
			if _, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP TABLE IF EXISTS "+name); err != nil {
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

// same reports whether two values are the same: a decimal by its number, a time
// by its instant and its offset, a NaN with a NaN, and a list and a map by their
// members.
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

// serverError returns err as a *snowflake.Error, and fails the test if it is
// not one.
func serverError(t testing.TB, err error) *snowflake.Error {
	t.Helper()
	e, ok := errors.AsType[*snowflake.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *snowflake.Error", err, err)
	}
	return e
}

// TestIntegrationConnect holds that the login works, that Ping runs, and the
// statement that usql runs for the version gives a release (step 16). The login
// has no ordinary user to compare with (the manifest says so).
func TestIntegrationConnect(t *testing.T) {
	db := connect(t)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	version, ok := scalar(t, db, "SELECT CURRENT_VERSION()").(string)
	if !ok || version == "" {
		t.Errorf("the version is %q", version)
	}
	t.Logf("the account runs Snowflake %s", version)
	cfg := integrationConfig(t)
	if cfg.Role != "" {
		if role := scalar(t, db, "SELECT CURRENT_ROLE()"); !strings.EqualFold(fmt.Sprint(role), cfg.Role) {
			t.Errorf("the role is %v, want %s", role, cfg.Role)
		}
	}
	if cfg.Warehouse != "" {
		if wh := scalar(t, db, "SELECT CURRENT_WAREHOUSE()"); !strings.EqualFold(fmt.Sprint(wh), cfg.Warehouse) {
			t.Errorf("the warehouse is %v, want %s", wh, cfg.Warehouse)
		}
	}
	if cfg.Database != "" {
		if got := scalar(t, db, "SELECT CURRENT_DATABASE()"); !strings.EqualFold(fmt.Sprint(got), cfg.Database) {
			t.Errorf("the database is %v, want %s", got, cfg.Database)
		}
	}
	// WithRole, WithDatabase, WithSchema and WithWarehouse change one statement.
	if got := scalar(t, db, "SELECT CURRENT_SCHEMA()", snowflake.WithSchema("INFORMATION_SCHEMA")); fmt.Sprint(got) != "INFORMATION_SCHEMA" {
		t.Errorf("the schema of the statement is %v, want INFORMATION_SCHEMA", got)
	}
	if got := scalar(t, db, "SELECT CURRENT_TIMESTAMP()::STRING", snowflake.WithTimeZone("Asia/Jakarta")); !strings.HasSuffix(fmt.Sprint(got), "+0700") {
		t.Errorf("a statement in Asia/Jakarta gave %v, want an offset of +0700", got)
	}
}

// beforeRows runs a statement that fails, and returns its error. It fails the
// test if the error does not come from the query itself, before any row.
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

// TestIntegrationErrors holds D183: an error of the server is an *snowflake.Error
// with its code and SQLSTATE, before any row, and a statement that reaches its
// timeout is ErrTimeout.
func TestIntegrationErrors(t *testing.T) {
	db := connect(t)
	for _, tt := range []struct {
		query    string
		code     string
		sqlState string
	}{
		{"SELEC 1", "001003", "42000"},
		{"SELECT * FROM " + table("nosuch"), "002003", "42S02"},
		// A statement that fails while it runs answers with a code and a message
		// only, so it has no SQLSTATE (measured, 2026-10-10).
		{"SELECT 1/0", "100051", ""},
		{"SELECT 'abc'::NUMBER", "100038", ""},
	} {
		err := beforeRows(t, db, tt.query)
		e := serverError(t, err)
		if e.Handle == "" {
			t.Errorf("%s: the error has no handle", tt.query)
		}
		if e.Code != tt.code || tt.sqlState != "" && e.SQLState != tt.sqlState {
			t.Errorf("%s: the error is %+v, want the code %s and SQLSTATE %s", tt.query, *e, tt.code, tt.sqlState)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", tt.query)
		}
	}
	// The server runs the whole statement before it sends a row, so a statement
	// that fails halfway gives the error and no row.
	err := beforeRows(t, db, "SELECT 1/(n-5000) FROM (SELECT seq4() AS n FROM TABLE(GENERATOR(ROWCOUNT => 20000))) ORDER BY n")
	if e := serverError(t, err); e.Code != "100051" {
		t.Errorf("the error of a statement that fails halfway is %+v", *e)
	}
	// A statement that reaches its timeout on the server.
	_, err = db.ExecContext(t.Context(), "SELECT SYSTEM$WAIT(20)", snowflake.WithTimeout(2*time.Second))
	if !errors.Is(err, snowflake.ErrTimeout) {
		t.Errorf("a statement of 20 seconds with a timeout of 2 gave %v, want ErrTimeout", err)
	}
	// Two statements need MULTI_STATEMENT_COUNT, which the driver refuses.
	_, err = db.ExecContext(t.Context(), "SELECT 1; SELECT 2")
	if e := serverError(t, err); e.Code != "000008" {
		t.Errorf("two statements gave %+v, want the code 000008", *e)
	}
	// A role that the login does not hold.
	_, err = db.ExecContext(t.Context(), "SELECT 1", snowflake.WithRole("ACCOUNTADMIN"))
	if err == nil {
		t.Skip("the login holds the role ACCOUNTADMIN, so the refusal of a role is not tested")
	}
	if e := serverError(t, err); e.Code != "390186" {
		t.Errorf("a role that the login does not hold gave %+v, want the code 390186", *e)
	}
}

// TestIntegrationTransactions holds D183 item 7: BeginTx fails with
// dbimp.ErrNotSupported, and the server refuses BEGIN alone.
func TestIntegrationTransactions(t *testing.T) {
	db := connect(t)
	if tx, err := db.BeginTx(t.Context(), nil); err == nil {
		_ = tx.Rollback()
		t.Error("BeginTx returned a transaction")
	} else if !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
	}
	_, err := db.ExecContext(t.Context(), "BEGIN")
	if e := serverError(t, err); e.Code != "391911" {
		t.Errorf("BEGIN gave %+v, want the code 391911", *e)
	}
}

// TestIntegrationContext holds D183 item 9: a context that ends while a statement
// runs stops the wait, with the error of the context, and the driver cancels the
// statement.
func TestIntegrationContext(t *testing.T) {
	db := connect(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, "SELECT SYSTEM$WAIT(30)")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a statement of 30 seconds with a deadline of 3 gave %v, want context.DeadlineExceeded", err)
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("the statement took %v to stop, want about 3 seconds", took)
	}
	// The same connection pool still works.
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping after the cancel: %v", err)
	}
}

// TestIntegrationBindings binds each Go type that the driver names, and reads the
// value back in the Go type of the column (D183 item 6).
func TestIntegrationBindings(t *testing.T) {
	db := connect(t)
	at := time.Date(2026, 10, 9, 12, 34, 56, 123456789, time.FixedZone("", 7*3600))
	for _, tt := range []struct {
		name  string
		query string
		arg   any
		want  any
	}{
		{"int64", "SELECT ?::NUMBER(18,0)", int64(-42), int64(-42)},
		{"float64", "SELECT ?::FLOAT", 1.5, 1.5},
		{"string", "SELECT ?::VARCHAR", "héllo", "héllo"},
		{"bool", "SELECT ?::BOOLEAN", true, true},
		{"[]byte", "SELECT ?::BINARY", []byte{0xDE, 0xAD}, []byte{0xDE, 0xAD}},
		{"dbimp.Date", "SELECT ?::DATE", dbimp.Date{Year: 2026, Month: time.October, Day: 9}, dbimp.Date{Year: 2026, Month: time.October, Day: 9}},
		{"dbimp.LocalTime", "SELECT ?::TIME(9)", dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456789}},
		{"dbimp.LocalDateTime", "SELECT ?::TIMESTAMP_NTZ(9)", dbimp.LocalDateTimeOf(at), dbimp.LocalDateTimeOf(at)},
		{"time.Time", "SELECT ?::TIMESTAMP_TZ(9)", at, at},
		{"*apd.Decimal", "SELECT ?::NUMBER(38,10)", dec(t, "12345678.91"), dec(t, "12345678.91")},
		{"NULL", "SELECT ?::VARCHAR", nil, nil},
	} {
		if got := scalar(t, db, tt.query, tt.arg); !same(got, tt.want) {
			t.Errorf("%s: got %#v (%T), want %#v (%T)", tt.name, got, got, tt.want, tt.want)
		}
	}
	// A variant is not a type of a binding.
	_, err := db.ExecContext(t.Context(), "SELECT ?", map[string]any{"a": 1})
	if !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a map gave %v, want dbimp.ErrArguments", err)
	}
	// A named placeholder, and a numbered one used twice.
	if got := scalar(t, db, "SELECT :a + :a", sql.Named("a", int64(21))); !same(got, dec(t, "42")) && got != int64(42) {
		t.Errorf("a named placeholder gave %v, want 42", got)
	}
}
