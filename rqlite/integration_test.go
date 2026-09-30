package rqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/rqlite"
)

// These tests need a server. RQLITE_DSN names it for the administrator, and
// RQLITE_ORDINARY_DSN for the ordinary user (D9). A test skips when its DSN
// is empty. rqlite has one database and no namespace, and the dbmeta session
// shares the server, so each table that the tests make starts with a prefix
// of its own, which TestMain drops at the end.

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
	admin      = principal{"administrator", "RQLITE_DSN"}
	ordinary   = principal{"ordinary", "RQLITE_ORDINARY_DSN"}
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
	db, err := sql.Open(rqlite.Name, dsn(t, p))
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

// table returns the name of a table of this run, for p.
func table(p principal, name string) string {
	return prefix + p.name[:1] + "_" + name
}

func TestMain(m *testing.M) {
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, "dropping the tables of the tests:", err)
		code = 1
	}
	os.Exit(code)
}

// cleanup drops each view and each table of this run, as the administrator.
func cleanup() error {
	v := os.Getenv(admin.env)
	if v == "" {
		return nil
	}
	db, err := sql.Open(rqlite.Name, v)
	if err != nil {
		return err
	}
	defer db.Close()
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var errs []error
	for _, kind := range []string{"view", "table"} {
		names, err := column[string](ctx, db, "SELECT name FROM sqlite_schema WHERE type = ? AND name LIKE ?", kind, prefix+"%")
		if err != nil {
			return err
		}
		for _, n := range names {
			if _, err := db.ExecContext(ctx, "DROP "+strings.ToUpper(kind)+" IF EXISTS "+n); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// column runs query, and reads its first column into a []T.
func column[T any](ctx context.Context, db *sql.DB, query string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var v T
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// exec runs stmt, and fails the test on an error.
func exec(t *testing.T, db *sql.DB, stmt string, args ...any) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), stmt, args...)
	if err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
	return res
}

// refused runs stmt, and fails the test unless the server refuses it with an
// error that holds want.
func refused(t *testing.T, db *sql.DB, stmt, want string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), stmt)
	if e, ok := errors.AsType[*rqlite.Error](err); !ok || !strings.Contains(e.Message, want) {
		t.Errorf("%s gave %v, want the error of the server %q", stmt, err, want)
	}
}

// read runs query, and fails the test on an error.
func read[T any](t *testing.T, db *sql.DB, query string, args ...any) []T {
	t.Helper()
	out, err := column[T](t.Context(), db, query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

func TestIntegrationConnect(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		// Both principals read the version of SQLite (step 16).
		if v := read[string](t, db, "SELECT sqlite_version()"); len(v) != 1 || !strings.HasPrefix(v[0], "3.") {
			t.Errorf("the version of SQLite is %q, want 3.x", v)
		}
	})
}

func TestIntegrationErrors(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		refused(t, db, "SELEC 1", `near "SELEC": syntax error`)
		refused(t, db, "SELECT * FROM "+prefix+"nosuch", "no such table")
		refused(t, db, "SELECT abs(-9223372036854775807 - 1)", "integer overflow")
		// An infinity fails the whole answer with HTTP 500 (measured).
		_, err := db.ExecContext(t.Context(), "SELECT 1e999", rqlite.WithReadonly(true))
		if e, ok := errors.AsType[*rqlite.Error](err); !ok || e.HTTPStatus != 500 {
			t.Errorf("an infinity gave %v, want HTTP 500", err)
		}
		cfg, err := rqlite.ParseDSN(dsn(t, p))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Password += "-wrong"
		bad := sql.OpenDB(rqlite.NewConnector(*cfg))
		defer bad.Close()
		if err := bad.PingContext(t.Context()); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
			t.Errorf("a wrong password gave %v, want HTTP 401", err)
		}
	})
}

// TestIntegrationReadonly holds D142: WithReadonly(true) sends a statement
// to /db/query, which refuses a write.
func TestIntegrationReadonly(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		_, err := db.ExecContext(t.Context(), "CREATE TABLE "+table(p, "ro")+" (a)", rqlite.WithReadonly(true))
		if e, ok := errors.AsType[*rqlite.Error](err); !ok || !strings.Contains(e.Message, "attempt to change database via query operation") {
			t.Errorf("a write with WithReadonly gave %v, want the refusal of the server", err)
		}
	})
}

// TestIntegrationRowsAffected holds D142: the counts of the server for a
// statement that counts rows, and 0 for any other.
func TestIntegrationRowsAffected(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table(p, "ra")
		for _, tt := range []struct {
			stmt           string
			affected, last int64
		}{
			{"CREATE TABLE " + tb + " (k INTEGER PRIMARY KEY, v)", 0, 0},
			{"INSERT INTO " + tb + " (v) VALUES (1), (2)", 2, 2},
			{"UPDATE " + tb + " SET v = v + 1", 2, 2},
			// The server gives the counts of the UPDATE for these (measured).
			{"CREATE INDEX " + tb + "_i ON " + tb + " (v)", 0, 0},
			{"SELECT 1", 0, 0},
			{"DELETE FROM " + tb + " WHERE k < 0", 0, 2},
			{"DROP TABLE " + tb, 0, 0},
		} {
			res := exec(t, db, tt.stmt)
			n, _ := res.RowsAffected()
			id, _ := res.LastInsertId()
			if n != tt.affected || id != tt.last {
				t.Errorf("%s gave %d rows and the id %d, want %d and %d", tt.stmt, n, id, tt.affected, tt.last)
			}
		}
	})
}

// TestIntegrationCancel holds D145: a context that ends stops a long read,
// and the deadline goes to the server as db_timeout.
func TestIntegrationCancel(t *testing.T) {
	long := "WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 3000000000) SELECT count(*) FROM n"
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		start := time.Now()
		_, err := column[int64](ctx, db, long)
		e, isServer := errors.AsType[*rqlite.Error](err)
		if !errors.Is(err, context.DeadlineExceeded) && (!isServer || e.Message != "query timeout") {
			t.Errorf("a long read gave %v, want the end of the context or query timeout", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("a long read ran %v after a deadline of one second", d)
		}
		// 9.4.5 ignores db_timeout on /db/request, so the driver ends the
		// request too (D146), and either error can come first.
		start = time.Now()
		_, err = column[int64](t.Context(), db, long, rqlite.WithTimeout(500*time.Millisecond))
		e, isServer = errors.AsType[*rqlite.Error](err)
		if !errors.Is(err, context.DeadlineExceeded) && (!isServer || e.Message != "query timeout") {
			t.Errorf("a long read with WithTimeout gave %v, want the end of the request or query timeout", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("a long read ran %v after WithTimeout of half a second", d)
		}
		// The next statement answers at once.
		if v := read[int64](t, db, "SELECT 1"); len(v) != 1 || v[0] != 1 {
			t.Errorf("the statement after the cancel gave %v", v)
		}
	})
}

// TestIntegrationTransactions holds D144: BeginTx fails with
// dbimp.ErrNotSupported.
func TestIntegrationTransactions(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("BeginTx gave %v, want %v", err, dbimp.ErrNotSupported)
		}
	})
}

// TestIntegrationScan reads each type through Rows.Scan into its own Go
// type, the path of a caller (dbmeta D93).
func TestIntegrationScan(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		tb := table(p, "scan")
		exec(t, db, "CREATE TABLE "+tb+" (i INTEGER, r REAL, t TEXT, b BLOB, n NUMERIC, bo BOOLEAN, d DATE, dt DATETIME, ts TIMESTAMP)")
		ts := time.Date(2026, time.September, 30, 12, 34, 56, 123456789, time.FixedZone("", 19800))
		exec(t, db, "INSERT INTO "+tb+" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", int64(-1), 0.5, "é", []byte{0, 255}, 12, true,
			dbimp.Date{Year: 2026, Month: time.September, Day: 30}, dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: time.September, Day: 30}, Time: dbimp.LocalTime{Hour: 1}}, ts)
		exec(t, db, "INSERT INTO "+tb+" VALUES (NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)")
		rows, err := db.QueryContext(t.Context(), "SELECT * FROM "+tb+" ORDER BY i IS NULL")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var (
			i  int64
			r  float64
			s  string
			b  []byte
			n  int64
			bo bool
			d  dbimp.Date
			dt dbimp.LocalDateTime
			tv time.Time
		)
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		if err := rows.Scan(&i, &r, &s, &b, &n, &bo, &d, &dt, &tv); err != nil {
			t.Fatal(err)
		}
		if i != -1 || r != 0.5 || s != "é" || string(b) != "\x00\xff" || n != 12 || !bo || d.String() != "2026-09-30" || dt.String() != "2026-09-30T01:00:00" || !tv.Equal(ts) {
			t.Errorf("the row is %v %v %q %v %v %v %v %v %v", i, r, s, b, n, bo, d, dt, tv)
		}
		var (
			ni  sql.Null[int64]
			nr  sql.Null[float64]
			ns  sql.Null[string]
			nb  []byte
			nn  sql.Null[int64]
			nbo sql.Null[bool]
			nd  sql.Null[dbimp.Date]
			ndt sql.Null[dbimp.LocalDateTime]
			nt  sql.Null[time.Time]
		)
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		if err := rows.Scan(&ni, &nr, &ns, &nb, &nn, &nbo, &nd, &ndt, &nt); err != nil {
			t.Fatal(err)
		}
		if ni.Valid || nr.Valid || ns.Valid || nb != nil || nn.Valid || nbo.Valid || nd.Valid || ndt.Valid || nt.Valid {
			t.Error("a row of NULL gave a value")
		}
	})
}

// TestIntegrationConcurrent runs two queries at once on one sql.DB.
func TestIntegrationConcurrent(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Go(func() {
				_, errs[i] = column[int64](t.Context(), db, "WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 100000) SELECT v FROM n")
			})
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Error(err)
		}
	})
}
