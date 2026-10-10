package bigquery //nolint:testpackage // The integration tests send raw requests with the token of the connector, which only the package can do.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// These tests need a project or an emulator. BIGQUERY_DSN is the DSN of the
// driver, in the form of D189: bigquery://project/dataset?credential_file=/path/key.json
// for the hosted service, with the service account that docs/BIGQUERY.md names, and
// the form that dbrun prints for the emulator, such as
// bigquery://admin@dbmeta/dbmeta?endpoint=http%3A%2F%2F127.0.0.1%3A9050&disable_auth=true.
// A test skips when BIGQUERY_DSN is empty (hard rule 9). BigQuery is a hosted
// service that dbrun does not start, so a person runs these tests on a project that
// holds the account that dbsetup made for this work (dbmeta D117), and the workflow of
// CI runs the emulator only (D189 item 4). The account is one principal, so each test
// runs as that principal only, as the manifest says, and the emulator has one login
// with no password.
//
// The dataset of the DSN holds the tables of the tests, which carry the name of this
// run as a prefix. TestMain drops what a test left, and fails if it left anything.
// A statement of a test names its tables in full, with the project in backticks,
// because a project id has hyphens that GoogleSQL refuses in a name that has no
// backticks (docs/BIGQUERY.md).
//
// A few tests need a bucket of Cloud Storage that the account can read and write.
// BIGQUERY_BUCKET names it, without gs://, and a test that needs it skips when the
// variable is empty.

// The variables that the tests read.
const (
	envDSN    = "BIGQUERY_DSN"
	envBucket = "BIGQUERY_BUCKET"
)

// suffix makes the names of this run unique, in lower case, because the names of
// tables are case sensitive.
var suffix = strings.ToLower(rand.Text()[:8])

// prefix is the start of the name of each table of this run.
var prefix = "dbimp_it_" + suffix

// itDSN returns the DSN of the project, and skips the test when there is none.
func itDSN(t testing.TB) string {
	t.Helper()
	v := os.Getenv(envDSN)
	if v == "" {
		t.Skipf("%s is empty, so there is no project or emulator to test against", envDSN)
	}
	return v
}

// itConfig returns the configuration of the DSN, and skips the test when the DSN
// names no dataset.
// envRelease names the variable that CI sets to the release that dbrun started,
// and unsupportedRelease is the one that the driver does not support.
const (
	envRelease         = "DBIMP_RELEASE"
	unsupportedRelease = "bigquery-0.7.2"
)

func itConfig(t testing.TB) Config {
	t.Helper()
	if os.Getenv(envRelease) == unsupportedRelease {
		t.Skipf("%s is the emulator release %s, which cannot read the result of a jobs.query job, so the driver does not support it (D189 item 4 and D195 item 4)", envRelease, unsupportedRelease)
	}
	cfg, err := ParseDSN(itDSN(t))
	if err != nil {
		t.Fatalf("reading %s: %v", envDSN, err)
	}
	if cfg.Dataset == "" {
		t.Skipf("the DSN in %s names no dataset, so the tests have nowhere to make their tables", envDSN)
	}
	return *cfg
}

// isEmulator reports whether the DSN names an address, which only an emulator
// does.
func isEmulator(cfg Config) bool {
	return cfg.Endpoint != ""
}

// hostedOnly skips the test on the emulator, with the reason that
// docs/BIGQUERY.md gives.
func hostedOnly(t testing.TB, why string) {
	t.Helper()
	if isEmulator(itConfig(t)) {
		t.Skipf("the emulator differs from the service here, so the test needs the service: %s", why)
	}
}

// itConnect returns a database on the project.
func itConnect(t testing.TB) *sql.DB {
	t.Helper()
	return sql.OpenDB(NewConnector(itConfig(t)))
}

// connect returns a database on the project that closes with the test.
func connect(t testing.TB) *sql.DB {
	t.Helper()
	db := itConnect(t)
	// Closing the database closes the connector.
	t.Cleanup(func() { db.Close() })
	return db
}

// table returns the full name of a table of this run, with the project in
// backticks.
func table(t testing.TB, name string) string {
	t.Helper()
	cfg := itConfig(t)
	return "`" + cfg.Project + "`." + cfg.Dataset + "." + prefix + "_" + name
}

// bucket returns the bucket of Cloud Storage, and skips the test when there is
// none.
func bucket(t testing.TB) string {
	t.Helper()
	b := os.Getenv(envBucket)
	if b == "" {
		t.Skipf("%s is empty, so there is no bucket to test against", envBucket)
	}
	return strings.TrimPrefix(b, "gs://")
}

// TestMain drops each table of this run that a test left, and fails if it found
// one that it failed to drop.
func TestMain(m *testing.M) {
	code := m.Run()
	if left := leftovers(); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "the tests left %d objects with the prefix %s: %v\n", len(left), prefix, left)
		code = 1
	}
	os.Exit(code)
}

// leftovers drops each table of this run, and returns the name of each object that
// it failed to drop. It returns none when there is no project.
func leftovers() []string {
	if os.Getenv(envRelease) == unsupportedRelease {
		return nil
	}
	v := os.Getenv(envDSN)
	if v == "" {
		return nil
	}
	cfg, err := ParseDSN(v)
	if err != nil || cfg.Dataset == "" {
		return nil
	}
	db := sql.OpenDB(NewConnector(*cfg))
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	found, err := listObjects(ctx, db, cfg)
	if err != nil && isEmulator(*cfg) {
		// The emulator has no INFORMATION_SCHEMA for a project in backticks, so it cannot list.
		return nil
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "looking for the tables that the tests left: %v\n", err)
		return []string{"?"}
	}
	var left []string
	for _, o := range found {
		verb := "DROP TABLE"
		switch o.kind {
		case "VIEW":
			verb = "DROP VIEW"
		case "MATERIALIZED VIEW":
			verb = "DROP MATERIALIZED VIEW"
		case "SNAPSHOT":
			verb = "DROP SNAPSHOT TABLE"
		}
		name := "`" + cfg.Project + "`." + cfg.Dataset + "." + o.name
		if _, err := db.ExecContext(ctx, verb+" IF EXISTS "+name); err != nil {
			// The account of the recorded run cannot delete a snapshot
			// (bigquery.tables.deleteSnapshot), so a person drops it.
			fmt.Fprintf(os.Stderr, "dropping %s: %v\n", name, err)
			left = append(left, o.name)
		}
	}
	return left
}

// object is a table, a view or a snapshot of this run.
type object struct{ name, kind string }

// listObjects returns each object of this run in the dataset.
func listObjects(ctx context.Context, db *sql.DB, cfg *Config) ([]object, error) {
	project := "`" + cfg.Project + "`"
	if isEmulator(*cfg) {
		project = cfg.Project
	}
	rows, err := db.QueryContext(ctx, "SELECT table_name, table_type FROM "+project+"."+cfg.Dataset+".INFORMATION_SCHEMA.TABLES WHERE STARTS_WITH(table_name, @p)", sql.Named("p", prefix))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []object
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.name, &o.kind); err != nil {
			return nil, err
		}
		found = append(found, o)
	}
	return found, rows.Err()
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

// drop runs the statements when the test ends, even when it fails, and drops
// nothing that is not there. The context of the test ends before its cleanup runs,
// so the statements use another.
func drop(t testing.TB, db *sql.DB, stmts ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, stmt := range stmts {
			if _, err := db.ExecContext(context.WithoutCancel(t.Context()), stmt); err != nil {
				t.Errorf("cleaning up with %q: %v", stmt, err)
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

// serverError returns err as a *Error, and fails the test if it is not one.
func serverError(t testing.TB, err error) *Error {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *bigquery.Error", err, err)
	}
	return e
}

// affected checks the count that a statement changed. The emulator sends no
// count, so RowsAffected is an error that wraps dbimp.ErrNotSupported there.
func affected(t testing.TB, db *sql.DB, want int64, q string, args ...any) {
	t.Helper()
	res := exec(t, db, q, args...)
	n, err := res.RowsAffected()
	if isEmulator(itConfig(t)) {
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: RowsAffected on the emulator gave %d, %v, want dbimp.ErrNotSupported", q, n, err)
		}
		return
	}
	if err != nil || n != want {
		t.Errorf("%s: RowsAffected is %d, %v, want %d", q, n, err, want)
	}
}

// TestIntegrationConnect holds that the login works, that Ping runs, and what
// the statement that usql runs for the version gives (step 16). The login has no
// ordinary user to compare with (the manifest says so).
func TestIntegrationConnect(t *testing.T) {
	db := connect(t)
	cfg := itConfig(t)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Logf("the principal is %v", scalar(t, db, "SELECT SESSION_USER()"))
	if !isEmulator(cfg) {
		if got := scalar(t, db, "SELECT @@project_id"); got != cfg.Project {
			t.Errorf("the project is %v, want %s", got, cfg.Project)
		}
	}
	// BigQuery has no statement for a release (recorded: bigquery-229), so the
	// version of usql is an error and not an answer, and the driver has none.
	_, err := db.ExecContext(t.Context(), "SELECT @@version")
	if err == nil {
		t.Error("SELECT @@version gave no error")
	}
	// WithLocation, WithDatabase and WithMaxResults change one statement.
	if got := scalar(t, db, "SELECT 1", WithDatabase(cfg.Dataset), WithMaxResults(10)); got != int64(1) {
		t.Errorf("SELECT 1 with options gave %v", got)
	}
}

// beforeRows runs a statement that fails, and returns its error. It fails the test if
// the statement gives no error, and if the error comes after a row.
func beforeRows(t testing.TB, db *sql.DB, q string) error {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q)
	if err == nil {
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		t.Errorf("%s: the statement gave no error before its rows, and %d rows with the error %v", q, n, rows.Err())
		return nil
	}
	return err
}

// TestIntegrationErrors holds D189: an error of the service is an *Error with its
// reason, before any row, and a result that fails halfway gives no row.
func TestIntegrationErrors(t *testing.T) {
	db := connect(t)
	cfg := itConfig(t)
	for _, tt := range []struct {
		name   string
		query  string
		status int
		reason string
		// emulatorAccepts is true when the emulator runs the statement with no error.
		emulatorAccepts bool
	}{
		{"syntax", "SELEC 1", 400, "invalidQuery", false},
		{"no table", "SELECT * FROM " + table(t, "nosuch"), 404, "notFound", false},
		{"overflow", "SELECT CAST(9223372036854775807 AS INT64) + 1 AS v", 400, "invalidQuery", true},
		{"division halfway", "SELECT 1 / (x - 1500) AS v FROM UNNEST(GENERATE_ARRAY(1, 3000)) AS x ORDER BY x", 400, "invalidQuery", false},
		{"array with a NULL", "SELECT [CAST(NULL AS INT64), 1] AS withnull", 400, "invalidQuery", true},
		{"assertion", "ASSERT 1 = 2 AS 'one is not two'", 400, "invalidQuery", true},
		{"update of no table", "UPDATE " + table(t, "nosuch") + " SET x = 1", 404, "notFound", false},
	} {
		if isEmulator(cfg) && tt.emulatorAccepts {
			// The emulator wraps an INT64, allows a NULL element, and ignores ASSERT
			// (docs/BIGQUERY.md, Hosted and emulator).
			continue
		}
		err := beforeRows(t, db, tt.query)
		if err == nil {
			continue
		}
		e := serverError(t, err)
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", tt.name)
		}
		if isEmulator(cfg) {
			// The emulator answers HTTP 400 and jobInternalError for each of them.
			if e.HTTPStatus < 400 {
				t.Errorf("%s: the status is %d", tt.name, e.HTTPStatus)
			}
			continue
		}
		if e.HTTPStatus != tt.status || tt.reason != "" && e.Reason != tt.reason {
			t.Errorf("%s: the error is %+v, want %d %s", tt.name, *e, tt.status, tt.reason)
		}
	}
	if isEmulator(cfg) {
		return
	}
	// A token that is wrong is HTTP 401 with the reason authError (recorded:
	// bigquery-420).
	bad := cfg
	bad.CredentialFile, bad.AccessToken = "", "ya29.not-a-token"
	baddb := sql.OpenDB(NewConnector(bad))
	defer baddb.Close()
	_, err := baddb.ExecContext(t.Context(), "SELECT 1")
	if e := serverError(t, err); e.HTTPStatus != 401 || e.Reason != "authError" {
		t.Errorf("a wrong token gave %+v, want HTTP 401 and authError", *e)
	}
	// A dataset that does not exist is HTTP 403, because the account cannot tell
	// (recorded: bigquery-183).
	_, err = db.ExecContext(t.Context(), "SELECT * FROM `"+cfg.Project+"`.dbimp_nosuchdataset_"+suffix+".t")
	if e := serverError(t, err); e.HTTPStatus != 403 || e.Reason != "accessDenied" {
		t.Errorf("a dataset that does not exist gave %+v, want HTTP 403 and accessDenied", *e)
	}
}

// TestIntegrationTransactions holds D189 item 3: BeginTx fails with
// dbimp.ErrNotSupported, and the service refuses BEGIN TRANSACTION alone.
func TestIntegrationTransactions(t *testing.T) {
	db := connect(t)
	if tx, err := db.BeginTx(t.Context(), nil); err == nil {
		_ = tx.Rollback()
		t.Error("BeginTx returned a transaction")
	} else if !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
	}
	hostedOnly(t, "the emulator answers HTTP 200 and does nothing for BEGIN TRANSACTION alone")
	_, err := db.ExecContext(t.Context(), "BEGIN TRANSACTION")
	if e := serverError(t, err); !strings.Contains(e.Message, "supported only in scripts or sessions") {
		t.Errorf("BEGIN TRANSACTION alone gave %+v, want the refusal of the service", *e)
	}
}

// TestIntegrationContext holds D36 and D189: a context that ends while a job runs
// stops the wait, with the error of the context, and the driver cancels the job,
// which the service then shows with the reason stopped.
func TestIntegrationContext(t *testing.T) {
	hostedOnly(t, "the emulator reports every job as done, and never stops a query")
	db := connect(t)
	label := "dbimp_ctx_" + suffix
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, slowQuery,
		WithParameter("useQueryCache", false),
		WithParameter("labels", map[string]string{"dbimp_test": label}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a heavy statement with a deadline of 8 seconds gave %v, want context.DeadlineExceeded", err)
	}
	if took := time.Since(start); took > 30*time.Second {
		t.Errorf("the statement took %v to stop, want about 8 seconds", took)
	}
	// The service stops the job a little after it answers the cancel (recorded:
	// bigquery-210 to bigquery-212), so the test reads the job again with a limit
	// on the time, and never sleeps for a fixed time.
	cfg := itConfig(t)
	q := "SELECT state, error_result.reason FROM `region-us`.INFORMATION_SCHEMA.JOBS_BY_PROJECT WHERE creation_time > TIMESTAMP_SUB(CURRENT_TIMESTAMP(), INTERVAL 1 HOUR) AND EXISTS (SELECT 1 FROM UNNEST(labels) AS l WHERE l.key = 'dbimp_test' AND l.value = @label)"
	var state, reason sql.NullString
	deadline := time.Now().Add(90 * time.Second)
	for {
		err := db.QueryRowContext(t.Context(), q, sql.Named("label", label)).Scan(&state, &reason)
		if err == nil && state.String == "DONE" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job of the statement is %v, %v after 90 seconds (the project is %s), want DONE", state, err, cfg.Project)
		}
		time.Sleep(2 * time.Second)
	}
	if reason.String != "stopped" {
		t.Errorf("the job ended with the reason %q, want stopped, which is a canceled job", reason.String)
	}
	// The same connection pool still works.
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping after the cancel: %v", err)
	}
}

// TestIntegrationParameters binds each Go type that the driver names, as a named
// parameter and as a positional one, and reads the value back in the Go type of
// the column (D189 item 8).
func TestIntegrationParameters(t *testing.T) {
	db := connect(t)
	cfg := itConfig(t)
	at := time.Date(2026, 10, 9, 12, 34, 56, 123456000, time.FixedZone("", 7*3600))
	date := dbimp.Date{Year: 2026, Month: time.October, Day: 9}
	clock := dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123456000}
	for _, tt := range []struct {
		name string
		arg  any
		want any
	}{
		{"int64", int64(-42), int64(-42)},
		{"float64", 1.5, 1.5},
		{"string", "héllo", "héllo"},
		{"bool", true, true},
		{"[]byte", []byte{0xDE, 0xAD}, []byte{0xDE, 0xAD}},
		{"dbimp.Date", date, date},
		{"dbimp.LocalTime", clock, clock},
		{"dbimp.LocalDateTime", dbimp.LocalDateTime{Date: date, Time: clock}, dbimp.LocalDateTime{Date: date, Time: clock}},
		{"time.Time", at, at.UTC()},
		{"*apd.Decimal", dec(t, "12345678.91"), dec(t, "12345678.91")},
		{"dbimp.Interval", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60 + 6) * int64(time.Second)}, dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60 + 6) * int64(time.Second)}},
	} {
		if isEmulator(cfg) && tt.name != "int64" && tt.name != "string" && tt.name != "bool" || isEmulator(cfg) && tt.name == "[]byte" {
			// The emulator reads most positional parameters in another type.
			continue
		}
		// The emulator reads a bare positional ? as an INT64 (recorded: the parameter requests).
		if !isEmulator(cfg) {
			if got := scalar(t, db, "SELECT ? AS a", tt.arg); !same(got, tt.want) {
				t.Errorf("positional %s: got %#v (%T), want %#v (%T)", tt.name, got, got, tt.want, tt.want)
			}
		}
		if got := scalar(t, db, "SELECT @a AS a", sql.Named("a", tt.arg)); !same(got, tt.want) {
			t.Errorf("named %s: got %#v (%T), want %#v (%T)", tt.name, got, got, tt.want, tt.want)
		}
	}
	// A NULL has no type, so the statement casts it.
	if got := scalar(t, db, "SELECT CAST(? AS INT64)", nil); got != nil {
		t.Errorf("a NULL gave %#v, want nil", got)
	}
	// A parameter in a LIMIT, and a statement that uses a parameter twice.
	if got := query(t, db, "SELECT x FROM UNNEST(GENERATE_ARRAY(1, 10)) AS x ORDER BY x LIMIT @n", sql.Named("n", int64(3))); len(got) != 3 {
		t.Errorf("LIMIT @n gave %d rows, want 3", len(got))
	}
	if got := scalar(t, db, "SELECT @a + @a", sql.Named("a", int64(21))); got != int64(42) {
		t.Errorf("a parameter used twice gave %v, want 42", got)
	}
	// A list and a map are not bound.
	if _, err := db.ExecContext(t.Context(), "SELECT ?", []any{int64(1)}); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a list gave %v, want dbimp.ErrArguments", err)
	}
	// A mix of named and positional arguments is refused before the service sees
	// it.
	if _, err := db.ExecContext(t.Context(), "SELECT ?, @a", int64(1), sql.Named("a", int64(2))); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a mix gave %v, want dbimp.ErrArguments", err)
	}
	if !isEmulator(cfg) {
		// The service refuses a parameter that the statement does not name, with
		// invalidQuery (recorded: bigquery-151).
		_, err := db.ExecContext(t.Context(), "SELECT @a AS a", sql.Named("b", int64(5)))
		if e := serverError(t, err); e.Reason != "invalidQuery" {
			t.Errorf("a parameter that the statement does not name gave %+v", *e)
		}
	}
}

// TestIntegrationTimestampForm holds D189 item 5 and open question 1 of
// docs/BIGQUERY.md: the driver asks for timestamps as ISO8601_STRING, and this
// test checks the two ends of the range. If the ISO form loses a digit at the year
// 9999, this test fails, and the driver must switch to useInt64Timestamp, which is
// exact, after Ken is told.
func TestIntegrationTimestampForm(t *testing.T) {
	hostedOnly(t, "the emulator writes a timestamp as seconds and ignores the form that the driver asks for")
	db := connect(t)
	for _, tt := range []struct {
		lit  string
		want time.Time
	}{
		{"0001-01-01 00:00:00+00", time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"1969-12-31 23:59:59.999999+00", time.Date(1969, 12, 31, 23, 59, 59, 999999000, time.UTC)},
		{"1970-01-01 00:00:00.000001+00", time.Date(1970, 1, 1, 0, 0, 0, 1000, time.UTC)},
		{"2024-01-02 03:04:05.123456+00", time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)},
		{"9999-12-31 23:59:59.999999+00", time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)},
	} {
		got := scalar(t, db, "SELECT TIMESTAMP '"+tt.lit+"'")
		if !same(got, tt.want) {
			t.Errorf("TIMESTAMP %q is %#v, want %v: the ISO form lost a digit, so the driver must use useInt64Timestamp (D189 item 5)", tt.lit, got, tt.want)
		}
	}
	// A float64 loses the last digits of this value, and the driver keeps
	// them.
	if got := scalar(t, db, "SELECT CAST(9223372036854775807 AS INT64)"); got != int64(math.MaxInt64) {
		t.Errorf("the largest INT64 is %v", got)
	}
}
