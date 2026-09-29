package databend_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/databend"
)

// These tests need a server. DATABEND_DSN names it for the administrator,
// and DATABEND_ORDINARY_DSN for the ordinary user (D9). A test skips when its
// DSN is empty. Each table that the tests make is in the database dbmeta,
// which both users can write, and starts with a prefix of its own, which
// TestMain drops at the end.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each table of this run.
var prefix = "dbimp_it_" + suffix + "_"

// table returns the name of a table of the tests, in the database dbmeta.
func table(name string) string {
	return "dbmeta." + prefix + name
}

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "DATABEND_DSN"}
	ordinary   = principal{"ordinary", "DATABEND_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// dsn returns the DSN of p, or skips the test when it is empty.
func dsn(t *testing.T, p principal) string {
	t.Helper()
	v := os.Getenv(p.env)
	if v == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	return v
}

// openAs opens the server as p.
func openAs(t *testing.T, p principal) *sql.DB {
	t.Helper()
	db, err := sql.Open(databend.Name, dsn(t, p))
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
			f(t, p, openAs(t, p))
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

// cleanup drops each table of this run, as the administrator.
func cleanup() error {
	v := os.Getenv(admin.env)
	if v == "" {
		return nil
	}
	db, err := sql.Open(databend.Name, v)
	if err != nil {
		return err
	}
	defer db.Close()
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	names, err := strings1(ctx, db, "SELECT name FROM system.tables WHERE database = 'dbmeta' AND engine <> 'VIEW' AND name LIKE ?", prefix+"%")
	if err != nil {
		return err
	}
	var errs []error
	for _, n := range names {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS dbmeta."+n); err != nil {
			errs = append(errs, err)
		}
	}
	for _, stmt := range []string{"DROP VIEW IF EXISTS dbmeta." + prefix + "view", "DROP STAGE IF EXISTS " + prefix + "stage", "DROP SEQUENCE IF EXISTS " + prefix + "seq"} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// strings1 returns the first column of each row of stmt.
func strings1(ctx context.Context, db *sql.DB, stmt string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// exec runs stmt, and fails the test on an error.
func exec(t *testing.T, db interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}, stmt string, args ...any,
) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), stmt, args...)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return res
}

// column returns the first column of each row of stmt.
func column(t *testing.T, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, stmt string, args ...any,
) []any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), stmt, args...)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	defer rows.Close()
	var out []any
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return out
}

// same fails unless got holds want, in order.
func same(t *testing.T, what string, got []any, want ...any) {
	t.Helper()
	if want == nil {
		want = []any{}
	}
	if got == nil {
		got = []any{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s gave %#v, want %#v", what, got, want)
	}
}

// code returns the code of the *databend.Error in err, or 0.
func code(err error) int {
	if e, ok := errors.AsType[*databend.Error](err); ok {
		return e.Code
	}
	return 0
}

// refused runs stmt, and fails unless the server refuses it with the code
// want.
func refused(t *testing.T, db *sql.DB, stmt string, want int) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), stmt)
	if got := code(err); got != want {
		t.Errorf("%s gave %v, want the code %d", stmt, err, want)
	}
}

// release returns the release of the server of db, such as 1.2.948.
func release(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	if err := db.QueryRowContext(t.Context(), "SELECT version()").Scan(&v); err != nil {
		t.Fatal(err)
	}
	v = strings.TrimPrefix(v, "Databend Query v")
	v, _, _ = strings.Cut(v, "-")
	return v
}

func TestIntegrationConnect(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		if v := release(t, db); !strings.HasPrefix(v, "1.2.") {
			t.Errorf("the release is %q, want 1.2.x", v)
		}
	})
}

func TestIntegrationPages(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		got := column(t, db, "SELECT number FROM numbers(30000) ORDER BY number")
		if len(got) != 30000 || got[0] != int64(0) || got[29999] != int64(29999) {
			t.Errorf("read %d rows of the three pages, want 0 to 29999 in order", len(got))
		}
	})
}

func TestIntegrationErrors(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		refused(t, db, "SELEC 1", 1005)
		refused(t, db, "SELECT * FROM dbmeta.nosuch", 1025)
		refused(t, db, "SELECT 1; SELECT 2", 1005)
		if p == ordinary {
			refused(t, db, "CREATE DATABASE "+prefix+"db", 1063)
		}
		rows, err := db.QueryContext(t.Context(), "SELECT number AS n FROM numbers(20000) UNION ALL SELECT to_uint8(300 + sleep(2)) AS n", databend.WithParameter("pagination", map[string]any{"max_rows_per_page": 10000, "wait_time_secs": 1}))
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); code(err) != 1006 || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("an error after %d rows gave %v, want the code 1006, which wraps dbimp.ErrIncomplete", n, err)
		}
		cfg, err := databend.ParseDSN(dsn(t, p))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Password += "-wrong"
		bad := sql.OpenDB(databend.NewConnector(*cfg))
		defer bad.Close()
		if err := bad.PingContext(t.Context()); code(err) != 5100 {
			t.Errorf("a wrong password gave %v, want the code 5100", err)
		}
	})
}

// running returns the number of queries whose text is text, that the server
// still runs.
func running(t *testing.T, db *sql.DB, text string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM system.processes WHERE extra_info = ?", text).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// waitGone fails unless no query with the text text runs within 3 seconds.
func waitGone(t *testing.T, db *sql.DB, text string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); running(t, db, text) > 0; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the query %q still runs 3 seconds after the driver stopped it (D123)", text)
		}
	}
}

// closeEarly runs text, reads its first row, and closes its rows before the
// end.
func closeEarly(t *testing.T, db *sql.DB, text string) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), text)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("no first row: %v", rows.Err())
	}
	if err := rows.Close(); err != nil {
		t.Errorf("closing the rows early: %v", err)
	}
}

// TestIntegrationCancel holds D123: the driver kills a query whose context
// ends, and one whose rows close before the end.
func TestIntegrationCancel(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		for i, how := range []string{"context", "close"} {
			t.Run(how, func(t *testing.T) {
				// The text is unique to the run, so the test finds its own
				// query in system.processes, which holds no comment.
				// A query of close sends its first page at once, and one of
				// context sends none before the context ends.
				text := fmt.Sprintf("SELECT number FROM numbers(1000000000000) WHERE number > %d", len(p.name)*10+i+int(time.Now().UnixNano()%1e6))
				ctx, cancel := context.WithCancel(t.Context())
				if how == "context" {
					text = fmt.Sprintf("SELECT sum(number) FROM numbers(1000000000000) WHERE number > %d", len(p.name)*10+i+int(time.Now().UnixNano()%1e6))
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
				}
				defer cancel()
				if how == "context" {
					if _, err := db.ExecContext(ctx, text); !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("the slow query gave %v, want context.DeadlineExceeded", err)
					}
				} else {
					closeEarly(t, db, text)
				}
				waitGone(t, db, text)
			})
		}
	})
}

// TestIntegrationTransactions holds D121 and D122.
func TestIntegrationTransactions(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table("tx_" + p.name)
		exec(t, db, "CREATE OR REPLACE TABLE "+tb+" (k INT)")
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		exec(t, tx, "INSERT INTO "+tb+" VALUES (1)")
		same(t, "the count in the transaction", column(t, tx, "SELECT count(*) FROM "+tb), int64(1))
		same(t, "the count outside the transaction", column(t, db, "SELECT count(*) FROM "+tb), int64(0))
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		same(t, "the count after the rollback", column(t, db, "SELECT count(*) FROM "+tb), int64(0))
		tx, err = db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		exec(t, tx, "INSERT INTO "+tb+" VALUES (2)")
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		same(t, "the count after the commit", column(t, db, "SELECT count(*) FROM "+tb), int64(1))
		tx, err = db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		exec(t, tx, "INSERT INTO "+tb+" VALUES (3)")
		exec(t, tx, "CREATE OR REPLACE TABLE "+tb+"_ddl (k INT)")
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (4)"); err == nil {
			t.Error("a statement after a DDL statement committed the transaction gave no error (D121)")
		}
		if err := tx.Commit(); err == nil {
			t.Error("a Commit after a DDL statement committed the transaction gave no error (D121)")
		}
		same(t, "the rows after the DDL", column(t, db, "SELECT k FROM "+tb+" ORDER BY k"), int64(2), int64(3))
		// Any error of a statement ends the transaction on the server: one
		// before the statement runs, and one while it runs (measured, D122).
		for _, stmt := range []string{"SELECT * FROM dbmeta.nosuch", "SELECT to_uint8(300)"} {
			tx, err = db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			exec(t, tx, "INSERT INTO "+tb+" VALUES (6)")
			if _, err := tx.ExecContext(t.Context(), stmt); err == nil {
				t.Fatalf("%s gave no error", stmt)
			}
			if _, err := tx.ExecContext(t.Context(), "SELECT 1"); err == nil {
				t.Errorf("a statement after %s in the transaction gave no error", stmt)
			}
			if err := tx.Commit(); err == nil {
				t.Errorf("a Commit after %s gave no error (D122)", stmt)
			}
			same(t, "the rows after the failed transaction", column(t, db, "SELECT count(*) FROM "+tb+" WHERE k = 6"), int64(0))
		}
	})
}

// TestIntegrationRollbackAfterTheContext ends the context of a transaction.
// database/sql then rolls it back, and the driver sends the rollback, so the
// write is gone (D122).
func TestIntegrationRollbackAfterTheContext(t *testing.T) {
	db := openAs(t, admin)
	tb := table("rollback")
	exec(t, db, "CREATE OR REPLACE TABLE "+tb+" (k INT)")
	ctx, cancel := context.WithCancel(t.Context())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+tb+" VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rolling back after the context ended: %v", err)
	}
	same(t, "the rows after the rollback", column(t, db, "SELECT count(*) FROM "+tb), int64(0))
}

// TestIntegrationOptions holds the options of one statement on the server
// (D109 and D117).
func TestIntegrationOptions(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		start := time.Now()
		_, err := db.ExecContext(t.Context(), "SELECT sum(number) FROM numbers(1000000000000)", databend.WithTimeout(500*time.Millisecond))
		if code(err) != 1043 {
			t.Errorf("the slow query with WithTimeout gave %v, want the code 1043", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("the slow query with WithTimeout of 500ms ended after %v", d)
		}
		same(t, "the database with WithDatabase", column(t, db, "SELECT database()", databend.WithDatabase("dbmeta")), "dbmeta")
		same(t, "the timezone with WithTimezone", column(t, db, "SELECT timezone()", databend.WithTimezone("Asia/Tokyo")), "Asia/Tokyo")
		ts := column(t, db, "SELECT '2026-09-29 12:00:00'::TIMESTAMP", databend.WithTimezone("Asia/Tokyo"))
		if got, ok := ts[0].(time.Time); !ok || got.UTC().Hour() != 3 {
			t.Errorf("a Timestamp in Asia/Tokyo is %v, want 03:00 in UTC", ts)
		}
		if _, err := db.ExecContext(t.Context(), "SELECT 1", databend.WithReadonly(true)); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithReadonly gave %v, want dbimp.ErrNotSupported", err)
		}
	})
}

// TestIntegrationSession holds D122: USE and SET stay on one connection, and
// database/sql resets them before it hands the connection to another caller.
func TestIntegrationSession(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		db.SetMaxOpenConns(1)
		conn, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		exec(t, conn, "SET timezone = 'Asia/Tokyo'")
		same(t, "the timezone after SET", column(t, conn, "SELECT timezone()"), "Asia/Tokyo")
		exec(t, conn, "USE dbmeta")
		same(t, "the database after USE", column(t, conn, "SELECT database()"), "dbmeta")
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		if got := column(t, db, "SELECT timezone()"); reflect.DeepEqual(got, []any{"Asia/Tokyo"}) {
			t.Errorf("the next caller got the timezone %v of SET, want the session of the DSN", got)
		}
	})
}
