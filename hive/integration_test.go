package hive_test

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
	"github.com/xo/dbimp/hive"
)

// These tests need a server. HIVE_DSN names it (D9). A test skips when it is
// empty. The servers accept any user, so there is one principal. The tests run on
// Hive and on Impala, and a test that one flavor cannot run skips there, with the
// reason. Each table that the tests make starts with a prefix of its own, which
// each test drops.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each table of this run.
var prefix = "dbimp_it_" + suffix + "_"

// dsn returns the DSN of the server, or skips the test when it is empty.
func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv("HIVE_DSN")
	if v == "" {
		t.Skip("HIVE_DSN is empty, so there is no server to test")
	}
	return v
}

// openServer opens the server.
func openServer(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(hive.Name, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// flavorOf returns "impala" or "hive", from the statement SELECT version().
func flavorOf(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	if err := db.QueryRowContext(t.Context(), "select version()").Scan(&v); err != nil {
		t.Fatalf("select version(): %v", err)
	}
	if strings.HasPrefix(v, "impalad") {
		return "impala"
	}
	return "hive"
}

// version returns the text of SELECT version().
func version(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	if err := db.QueryRowContext(t.Context(), "select version()").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// counter numbers the tables of a run.
var (
	counterMu sync.Mutex
	counter   int
)

// table returns a new table name of this run.
func table() string {
	counterMu.Lock()
	defer counterMu.Unlock()
	counter++
	return prefix + strconv.Itoa(counter)
}

// execAll runs the statements, and fails the test at the first error.
func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.ExecContext(t.Context(), s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// drop drops tables at the end of the test.
func drop(t *testing.T, db *sql.DB, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, n := range names {
			_, _ = db.ExecContext(context.WithoutCancel(t.Context()), "drop table if exists "+n)
		}
	})
}

// drain runs the query and reads every row, and returns the first error.
func drain(ctx context.Context, db *sql.DB, q string) error {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

func TestIntegrationConnect(t *testing.T) {
	db := openServer(t)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	v := version(t, db)
	if v == "" {
		t.Error("select version() is empty")
	}
	t.Logf("%s", strings.SplitN(v, "\n", 2)[0])
}

func TestIntegrationErrors(t *testing.T) {
	db := openServer(t)
	_, err := db.ExecContext(t.Context(), "select nope from "+prefix+"nothing")
	e, ok := errors.AsType[*hive.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *hive.Error", err, err)
	}
	if e.SQLState == "" || e.Message == "" {
		t.Errorf("the error has no SQLSTATE or no message: %+v", e)
	}
	if errors.Is(err, dbimp.ErrAuthentication) {
		t.Error("a statement error matches dbimp.ErrAuthentication")
	}
	if got := e.ErrVerbose(""); !strings.HasPrefix(got, "ERROR:  ") {
		t.Errorf("ErrVerbose() = %q, want the layout of psql", got)
	}
	// The connection still works after an error.
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping after an error: %v", err)
	}
}

func TestIntegrationRowsAffected(t *testing.T) {
	db := openServer(t)
	tbl := table()
	drop(t, db, tbl)
	execAll(t, db, "create table "+tbl+" (id int)")
	res, err := db.ExecContext(t.Context(), "insert into "+tbl+" values (1),(2),(3)")
	if err != nil {
		t.Fatal(err)
	}
	n, err := res.RowsAffected()
	switch flavorOf(t, db) {
	case "hive":
		if err != nil || n != 3 {
			t.Errorf("RowsAffected() = %d, %v, want 3 on Hive", n, err)
		}
	default:
		if !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("RowsAffected() = %d, %v, want dbimp.ErrNotSupported on Impala", n, err)
		}
	}
}

func TestIntegrationPaging(t *testing.T) {
	db := openServer(t)
	const q = "select a.x*100+b.x*10+c.x as n from (select 0 as x union all select 1 union all select 2 union all select 3 union all select 4 union all select 5 union all select 6 union all select 7 union all select 8 union all select 9) a cross join (select 0 as x union all select 1 union all select 2 union all select 3 union all select 4 union all select 5 union all select 6 union all select 7 union all select 8 union all select 9) b cross join (select 0 as x union all select 1 union all select 2 union all select 3 union all select 4 union all select 5 union all select 6 union all select 7 union all select 8 union all select 9) c"
	for _, size := range []int{1, 7, 100, 1000, 5000} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			rows, err := db.QueryContext(hive.WithOptions(t.Context(), hive.WithFetchSize(size)), q)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			seen := map[int64]bool{}
			for rows.Next() {
				var n int64
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
				seen[n] = true
			}
			if err := rows.Err(); err != nil || len(seen) != 1000 {
				t.Errorf("read %d different rows, %v, want 1000", len(seen), err)
			}
		})
	}
}

func TestIntegrationCancel(t *testing.T) {
	db := openServer(t)
	var q string
	if flavorOf(t, db) == "impala" {
		q = "select sleep(30000)"
	} else {
		var b strings.Builder
		b.WriteString("select count(*) from ")
		for i := range 11 {
			if i > 0 {
				b.WriteString(" cross join ")
			}
			b.WriteString("(select 0 as x union all select 1 union all select 2 union all select 3 union all select 4 union all select 5 union all select 6 union all select 7 union all select 8 union all select 9) t" + strconv.Itoa(i))
		}
		q = b.String()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	err := drain(ctx, db, q)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("the statement ended after %v, want about 3s", d)
	}
	// The connection is usable afterwards.
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping after a cancel: %v", err)
	}
}

func TestIntegrationTimeoutOption(t *testing.T) {
	db := openServer(t)
	if flavorOf(t, db) != "impala" {
		t.Skip("select sleep() is a function of Impala")
	}
	ctx := hive.WithOptions(t.Context(), hive.WithTimeout(2*time.Second))
	err := drain(ctx, db, "select sleep(30000)")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want context.DeadlineExceeded", err)
	}
}

func TestIntegrationTransactions(t *testing.T) {
	db := openServer(t)
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx() = %v, want dbimp.ErrNotSupported", err)
	}
}

func TestIntegrationScan(t *testing.T) {
	db := openServer(t)
	var (
		i  int64
		f  float64
		s  string
		ns sql.Null[string]
		b  bool
	)
	err := db.QueryRowContext(t.Context(), "select 1, 2.5, 'x', cast(null as string), true").Scan(&i, &f, &s, &ns, &b)
	if err != nil || i != 1 || f != 2.5 || s != "x" || ns.Valid || !b {
		t.Errorf("scan = %d %v %q %+v %v, %v", i, f, s, ns, b, err)
	}
	var decimal string
	if err := db.QueryRowContext(t.Context(), "select cast(1.50 as decimal(10,2))").Scan(&decimal); err != nil || decimal != "1.50" {
		t.Errorf("a decimal scanned into a string = %q, %v, want 1.50 with its scale (D228)", decimal, err)
	}
}

func TestIntegrationArguments(t *testing.T) {
	db := openServer(t)
	var got string
	err := db.QueryRowContext(t.Context(), "select ?", "it's a \\ test 'quoted'").Scan(&got)
	if err != nil || got != "it's a \\ test 'quoted'" {
		t.Errorf("a string argument came back as %q, %v", got, err)
	}
	var n int64
	if err := db.QueryRowContext(t.Context(), "select ? + ?", int64(2), int64(3)).Scan(&n); err != nil || n != 5 {
		t.Errorf("two arguments gave %d, %v, want 5", n, err)
	}
	if _, err := db.ExecContext(t.Context(), "select ?"); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a missing argument gave %v, want dbimp.ErrArguments", err)
	}
}

func TestIntegrationConcurrent(t *testing.T) {
	db := openServer(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Go(func() {
			var n int64
			if err := db.QueryRowContext(t.Context(), fmt.Sprintf("select %d", i)).Scan(&n); err != nil || n != int64(i) {
				errs <- fmt.Errorf("select %d = %d, %w", i, n, err)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestIntegrationDatabaseInDSN(t *testing.T) {
	base := dsn(t)
	cfg, err := hive.ParseDSN(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database = "default"
	db, err := sql.Open(hive.Name, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("a DSN with the database default: %v", err)
	}
	cfg.Database = prefix + "nothing"
	db2, err := sql.Open(hive.Name, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err := db2.PingContext(t.Context()); err == nil {
		t.Error("a DSN with a database that does not exist gave no error")
	}
}
