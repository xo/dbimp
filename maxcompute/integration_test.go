package maxcompute_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/maxcompute"
)

// envDSN is the environment variable that holds the DSN of a project of MaxCompute
// for the integration tests (D9). The tests skip when it is empty.
const envDSN = "MAXCOMPUTE_DSN"

// openIntegration opens the project that MAXCOMPUTE_DSN names, and skips the test
// when the variable is empty.
func openIntegration(t testing.TB) *sql.DB {
	t.Helper()
	dsn := os.Getenv(envDSN)
	if dsn == "" {
		t.Skipf("%s is empty, and the test needs a project of MaxCompute (D9)", envDSN)
	}
	db, err := sql.Open(maxcompute.Name, dsn)
	if err != nil {
		t.Fatalf("opening the project: %v", err)
	}
	db.SetConnMaxLifetime(time.Minute)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestIntegrationSmoke(t *testing.T) {
	db := openIntegration(t)
	var a int64
	var b string
	if err := db.QueryRowContext(t.Context(), "SELECT 1 AS a, 'x' AS b").Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	if a != 1 || b != "x" {
		t.Errorf("got %d and %q", a, b)
	}
}

func TestIntegrationPing(t *testing.T) {
	t.Parallel()
	if err := openIntegration(t).PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestIntegrationWrongSecretIsAuthentication holds the refusal of a credential
// against the service (D197): a wrong secret matches dbimp.ErrAuthentication, and a
// project that does not exist does not.
func TestIntegrationWrongSecretIsAuthentication(t *testing.T) {
	t.Parallel()
	dsn := os.Getenv(envDSN)
	if dsn == "" {
		t.Skipf("%s is empty, and the test needs a project of MaxCompute (D9)", envDSN)
	}
	cfg, err := maxcompute.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	wrong := *cfg
	wrong.Secret += "-wrong"
	db := sql.OpenDB(maxcompute.NewConnector(wrong))
	defer db.Close()
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); !errors.Is(err, dbimp.ErrAuthentication) {
		t.Errorf("a wrong secret gave %v, want dbimp.ErrAuthentication", err)
	}
	missing := *cfg
	missing.Project += "_nope"
	db2 := sql.OpenDB(maxcompute.NewConnector(missing))
	defer db2.Close()
	err = db2.PingContext(t.Context())
	if err == nil || errors.Is(err, dbimp.ErrAuthentication) {
		t.Errorf("a project that does not exist gave %v, want an error that is no refusal of the credential", err)
	}
}

func TestIntegrationBindsArguments(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	var a int64
	var b, c string
	var d bool
	if err := db.QueryRowContext(t.Context(), "SELECT ? AS a, ? AS b, ? AS c, ? AS d", int64(-7), `it's a \ "test"`, "?", true).Scan(&a, &b, &c, &d); err != nil {
		t.Fatal(err)
	}
	if a != -7 || b != `it's a \ "test"` || c != "?" || !d {
		t.Errorf("got %d, %q, %q, %t", a, b, c, d)
	}
}

func TestIntegrationErrors(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	err := queryErr(t, db, "SELECT * FROM dbimp_it_missing")
	if e, ok := errors.AsType[*maxcompute.Error](err); !ok || !strings.HasPrefix(e.Message, "ODPS-0130131") {
		t.Errorf("a missing table gave %v, want ODPS-0130131", err)
	}
	_, err = db.ExecContext(t.Context(), "SELEC 1")
	if e, ok := errors.AsType[*maxcompute.Error](err); !ok || e.Code != "ParseError" {
		t.Errorf("a statement that does not parse gave %v, want ParseError", err)
	}
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want dbimp.ErrNotSupported", err)
	}
	res, err := db.ExecContext(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected gave %v, want dbimp.ErrNotSupported", err)
	}
}

func TestIntegrationTextResult(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	rows, err := db.QueryContext(t.Context(), "SHOW TABLES")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if cols, _ := rows.Columns(); len(cols) != 1 || cols[0] != "text" {
		t.Fatalf("the columns are %v, want one named text", cols)
	}
	n := 0
	for rows.Next() {
		n++
	}
	if n != 1 || rows.Err() != nil {
		t.Errorf("a text result has %d rows and the error %v, want one row", n, rows.Err())
	}
}

// TestIntegrationCapOfTheResult shows the silent cap of the server (D220 item 5):
// the driver returns 10,000 rows of a result of 15,000, with no error.
func TestIntegrationCapOfTheResult(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	if got := rowsOf(t, db, ints); got != 10000 {
		t.Errorf("the result has %d rows, want the 10000 of the cap", got)
	}
}

// TestIntegrationTimeZone reads a DATETIME in the zone of the project, and in the
// zone of the setting when the caller gives one (D219 item 2).
func TestIntegrationTimeZone(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	var utc, other time.Time
	const q = "SELECT DATETIME'2026-01-01 00:00:00' AS a"
	if err := db.QueryRowContext(t.Context(), q, maxcompute.WithParameter("odps.sql.timezone", "UTC")).Scan(&utc); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), q, maxcompute.WithParameter("odps.sql.timezone", "Asia/Jakarta")).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if !utc.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("with the zone UTC the instant is %v", utc)
	}
	if want := time.Date(2025, 12, 31, 17, 0, 0, 0, time.UTC); !other.Equal(want) {
		t.Errorf("with the zone Asia/Jakarta the instant is %v, want %v", other, want)
	}
}

// TestIntegrationDeadlineEndsTheStatement shows that the deadline of the context
// stops the wait, ends the instance on the server, and returns the error of the
// context (D36 and D220 item 7).
// slowQuery is a statement that runs for much longer than the tests wait: a join
// of two lists of 300000 equal values, which has 90 billion pairs to count.
const slowQuery = "SELECT COUNT(*) FROM (SELECT posexplode(split(repeat('a,', 300000), ',')) AS (p, v)) a JOIN (SELECT posexplode(split(repeat('a,', 300000), ',')) AS (q, w)) b ON a.v = b.w"

func TestIntegrationDeadlineEndsTheStatement(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, slowQuery)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the error is %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("the statement ended %v after the start, want soon after the deadline", d)
	}
}

func TestIntegrationTimeoutOption(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	_, err := db.ExecContext(t.Context(), slowQuery, maxcompute.WithTimeout(6*time.Second))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
}

func TestIntegrationTwoQueriesAtOnce(t *testing.T) {
	t.Parallel()
	db := openIntegration(t)
	errs := make(chan error, 2)
	for i := range 2 {
		go func() {
			var n int64
			errs <- db.QueryRowContext(t.Context(), "SELECT ? AS n", int64(i)).Scan(&n)
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
