package libsql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/libsql"
)

// These tests need a server. LIBSQL_DSN names it for the administrator, whose
// token can write, and LIBSQL_ORDINARY_DSN for the ordinary user, whose token
// can only read (D9). A test skips when its DSN is empty. The server has one
// database, so each table that the tests make starts with a prefix of its
// own, which TestMain drops at the end. The administrator makes every table,
// because the ordinary user can write nothing.

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
	admin      = principal{"administrator", "LIBSQL_DSN"}
	ordinary   = principal{"ordinary", "LIBSQL_ORDINARY_DSN"}
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
	db, err := sql.Open(libsql.Name, dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// forEach runs f as a subtest for each principal, with the database of the
// administrator too, for the writes that the ordinary user cannot make.
func forEach(t *testing.T, f func(t *testing.T, p principal, db, adm *sql.DB)) {
	t.Helper()
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			f(t, p, openAs(t, p), openAs(t, admin))
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
	db, err := sql.Open(libsql.Name, v)
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
// error whose code or message holds want.
func refused(t *testing.T, db *sql.DB, stmt, want string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), stmt)
	if e, ok := errors.AsType[*libsql.Error](err); !ok || !strings.Contains(e.Code+" "+e.Message, want) {
		t.Errorf("%s gave %v, want the error of the server %q", stmt, err, want)
	}
}

// forbidden fails the test unless stmt, as the ordinary user, gets HTTP 403,
// because its token can only read (measured).
func forbidden(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), stmt)
	if e, ok := errors.AsType[*libsql.Error](err); !ok || e.HTTPStatus != 403 {
		t.Errorf("%s as the ordinary user gave %v, want HTTP 403", stmt, err)
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
	forEach(t, func(t *testing.T, _ principal, db, _ *sql.DB) {
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
	forEach(t, func(t *testing.T, p principal, db, _ *sql.DB) {
		refused(t, db, "SELEC 1", "SQL_PARSE_ERROR")
		refused(t, db, "SELECT * FROM "+prefix+"nosuch", "no such table")
		refused(t, db, "SELECT 1; SELECT 2", "SQL_MANY_STATEMENTS")
		if p == ordinary {
			forbidden(t, db, "CREATE TABLE "+table(p, "nope")+" (a)")
		}
		cfg, err := libsql.ParseDSN(dsn(t, p))
		if err != nil {
			t.Fatal(err)
		}
		cfg.Password += "x"
		bad := sql.OpenDB(libsql.NewConnector(*cfg))
		defer bad.Close()
		if err := bad.PingContext(t.Context()); err == nil || !strings.Contains(err.Error(), "The JWT is invalid") {
			t.Errorf("a token that is not valid gave %v, want HTTP 401", err)
		}
	})
}

// TestIntegrationCancel holds D152: a context that ends stops a long
// statement, because the server stops it when its client leaves.
func TestIntegrationCancel(t *testing.T) {
	long := "WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 3000000000) SELECT count(*) FROM n"
	forEach(t, func(t *testing.T, _ principal, db, _ *sql.DB) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		start := time.Now()
		if _, err := column[int64](ctx, db, long); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("a long query gave %v, want the end of the context", err)
		}
		if _, err := column[int64](t.Context(), db, long, libsql.WithTimeout(500*time.Millisecond)); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("a long query with WithTimeout gave %v, want the end of the request", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("two long queries ran %v after their deadlines", d)
		}
		if v := read[int64](t, db, "SELECT 1"); len(v) != 1 || v[0] != 1 {
			t.Errorf("the statement after the cancel gave %v", v)
		}
	})
}

// TestIntegrationLargeResult holds D149: the cursor streams a result larger
// than the 10MB that a pipeline can hold.
func TestIntegrationLargeResult(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db, _ *sql.DB) {
		rows, err := db.QueryContext(t.Context(), "WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v + 1 FROM n WHERE v < 300000) SELECT v, 'padding padding padding padding' FROM n")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var n, last int64
		for rows.Next() {
			var s string
			if err := rows.Scan(&last, &s); err != nil {
				t.Fatal(err)
			}
			n++
		}
		if err := rows.Err(); err != nil || n != 300000 || last != 300000 {
			t.Errorf("read %d rows up to %d and %v, want 300000", n, last, err)
		}
	})
}

// TestIntegrationTransactions holds D150: a transaction lives on a stream,
// a read outside it does not see its writes, and Commit and Rollback end it.
func TestIntegrationTransactions(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db, adm *sql.DB) {
		tb := table(p, "tx")
		exec(t, adm, "CREATE TABLE "+tb+" (k INTEGER PRIMARY KEY)")
		if p == ordinary {
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (1)"); err == nil {
				t.Error("a write in a transaction of the ordinary user gave no error")
			}
			_ = tx.Rollback()
			return
		}
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (1)"); err != nil {
			t.Fatal(err)
		}
		var in, out int64
		if err := tx.QueryRowContext(t.Context(), "SELECT count(*) FROM "+tb).Scan(&in); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+tb).Scan(&out); err != nil {
			t.Fatal(err)
		}
		if in != 1 || out != 0 {
			t.Errorf("the transaction saw %d rows and a read outside it %d, want 1 and 0", in, out)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := read[int64](t, db, "SELECT count(*) FROM "+tb); n[0] != 0 {
			t.Errorf("the rollback left %d rows", n[0])
		}
		tx, err = db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (2)"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if n := read[int64](t, db, "SELECT k FROM "+tb); len(n) != 1 || n[0] != 2 {
			t.Errorf("the commit left %v, want 2", n)
		}
		if _, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true}); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("a read-only transaction gave %v, want %v", err, dbimp.ErrNotSupported)
		}
	})
}

// TestIntegrationStreamExpired holds D150: a transaction whose stream has no
// request for more than 10 seconds is rolled back, and its next statement
// says so.
func TestIntegrationStreamExpired(t *testing.T) {
	p := admin
	db := openAs(t, p)
	tb := table(p, "exp")
	exec(t, db, "CREATE TABLE "+tb+" (k INTEGER PRIMARY KEY)")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	// The server expires a stream after 10 seconds with no request
	// (measured). Nothing else can show it.
	time.Sleep(12 * time.Second)
	_, err = tx.ExecContext(t.Context(), "INSERT INTO "+tb+" VALUES (2)")
	if e, ok := errors.AsType[*libsql.Error](err); !ok || e.Code != libsql.CodeStreamExpired || errors.Is(err, driver.ErrBadConn) {
		t.Errorf("a statement after the stream expired gave %v, want STREAM_EXPIRED", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Errorf("Rollback after the stream expired gave %v", err)
	}
	if n := read[int64](t, db, "SELECT count(*) FROM "+tb); n[0] != 0 {
		t.Errorf("the expired transaction left %d rows", n[0])
	}
}

// TestIntegrationScan reads each type through Rows.Scan into its own Go
// type, the path of a caller (dbmeta D93).
func TestIntegrationScan(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db, adm *sql.DB) {
		tb := table(p, "scan")
		exec(t, adm, "CREATE TABLE "+tb+" (i INTEGER, r REAL, t TEXT, b BLOB, n NUMERIC, bo BOOLEAN, d DATE, dt DATETIME, ts TIMESTAMP, v F32_BLOB(2))")
		ts := time.Date(2026, time.October, 1, 12, 34, 56, 123456789, time.FixedZone("", 19800))
		exec(t, adm, "INSERT INTO "+tb+" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, vector32('[1.5,2]'))", int64(-1), 0.5, "é", []byte{0, 255}, 12, true,
			dbimp.Date{Year: 2026, Month: time.October, Day: 1}, dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: time.October, Day: 1}, Time: dbimp.LocalTime{Hour: 1}}, ts)
		exec(t, adm, "INSERT INTO "+tb+" VALUES (NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)")
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
			v  dbimp.Vector[float32]
		)
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		if err := rows.Scan(&i, &r, &s, &b, &n, &bo, &d, &dt, &tv, &v); err != nil {
			t.Fatal(err)
		}
		if i != -1 || r != 0.5 || s != "é" || string(b) != "\x00\xff" || n != 12 || !bo || d.String() != "2026-10-01" || dt.String() != "2026-10-01T01:00:00" || !tv.Equal(ts) || len(v) != 2 || v[0] != 1.5 {
			t.Errorf("the row is %v %v %q %v %v %v %v %v %v %v", i, r, s, b, n, bo, d, dt, tv, v)
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
			nv  []byte
		)
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		if err := rows.Scan(&ni, &nr, &ns, &nb, &nn, &nbo, &nd, &ndt, &nt, &nv); err != nil {
			t.Fatal(err)
		}
		if ni.Valid || nr.Valid || ns.Valid || nb != nil || nn.Valid || nbo.Valid || nd.Valid || ndt.Valid || nt.Valid || nv != nil {
			t.Error("a row of NULL gave a value")
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	})
}

// TestIntegrationConcurrent runs two queries at once on one sql.DB.
func TestIntegrationConcurrent(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db, _ *sql.DB) {
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
