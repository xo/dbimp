package arangodb //nolint:testpackage // The tests reach the endpoints of the server through the connector of the driver.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// These tests need a server. ARANGODB_DSN names it for the administrator,
// and ARANGODB_ORDINARY_DSN for the ordinary user (D9). Each DSN names the
// database dbmeta. A test skips when its DSN is empty. Each collection that
// the tests make starts with a prefix of its own, and TestMain drops each one
// at the end.

// prefix starts each collection of this run.
var prefix = "dbimp_it_" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36) + "_"

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin      = principal{"administrator", "ARANGODB_DSN"}
	ordinary   = principal{"ordinary", "ARANGODB_ORDINARY_DSN"}
	principals = []principal{admin, ordinary}
)

// config returns the configuration of the DSN of p, or skips the test when
// its DSN is empty.
func config(t *testing.T, p principal) Config {
	t.Helper()
	dsn := os.Getenv(p.env)
	if dsn == "" {
		t.Skipf("%s is empty, so there is no server to test as the %s user", p.env, p.name)
	}
	cfg, err := ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return *cfg
}

// openWith opens the server as p, with the configuration that f changes.
func openWith(t *testing.T, p principal, f func(*Config)) *sql.DB {
	t.Helper()
	cfg := config(t, p)
	if f != nil {
		f(&cfg)
	}
	db := sql.OpenDB(NewConnector(cfg))
	t.Cleanup(func() { db.Close() })
	return db
}

// forEach runs f as a subtest for each principal.
func forEach(t *testing.T, f func(t *testing.T, p principal, db *sql.DB)) {
	t.Helper()
	for _, p := range principals {
		t.Run(p.name, func(t *testing.T) {
			f(t, p, openWith(t, p, nil))
		})
	}
}

// collection makes a collection of the tests, as the administrator, and
// returns its name.
func collection(t *testing.T, name string, docs ...string) string {
	t.Helper()
	db := openWith(t, admin, nil)
	c := prefix + name
	exec(t, db, "CREATE COLLECTION IF NOT EXISTS "+c)
	for _, d := range docs {
		exec(t, db, "INSERT "+d+" INTO "+c)
	}
	return c
}

// exec runs q, and fails on an error.
func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// column returns the first column of each row of q.
func column(t *testing.T, db *sql.DB, q string, args ...any) []any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	var out []any
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", q, err)
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

// refused runs q, and fails unless the server refuses it with an *Error.
func refused(t *testing.T, db *sql.DB, q string) *Error {
	t.Helper()
	_, err := db.ExecContext(t.Context(), q)
	e, ok := errors.AsType[*Error](err)
	if !ok {
		t.Errorf("%s gave %v, want the refusal of the server", q, err)
	}
	return e
}

// call sends a request to an endpoint of the server as p, for what the
// driver does not do, such as a view.
func call(t *testing.T, p principal, method, path string, body any) error {
	t.Helper()
	c := NewConnector(config(t, p))
	defer c.transport.CloseIdleConnections()
	return c.call(t.Context(), method, api(c.cfg.Database, path), body, nil, "")
}

func TestMain(m *testing.M) {
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// cleanup drops each collection of this run, as the administrator.
func cleanup() error {
	dsn := os.Getenv(admin.env)
	if dsn == "" {
		return nil
	}
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return err
	}
	c := NewConnector(*cfg)
	defer c.transport.CloseIdleConnections()
	// TestMain has no context of its own.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var list struct {
		Result []struct {
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := c.call(ctx, http.MethodGet, api(c.cfg.Database, "collection?excludeSystem=true"), nil, &list, ""); err != nil {
		return err
	}
	var errs []error
	for _, col := range list.Result {
		if strings.HasPrefix(col.Name, prefix) {
			errs = append(errs, c.call(ctx, http.MethodDelete, api(c.cfg.Database, "collection/"+url.PathEscape(col.Name)), nil, nil, ""))
		}
	}
	return errors.Join(errs...)
}

func TestIntegrationConnect(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if err := db.PingContext(t.Context()); err != nil {
			t.Fatal(err)
		}
		same(t, "VERSION()", column(t, db, "RETURN VERSION() >= '3.12'"), true)
	})
}

func TestIntegrationTypes(t *testing.T) {
	c := collection(t, "types", `{_key: "a", s: "é", b: true, i: 9223372036854775807, f: 1.5, arr: [1, "x"], obj: {z: 1}, n: null}`)
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		var (
			s   string
			b   bool
			i   int64
			f   float64
			arr any
			obj any
			n   sql.Null[string]
			m   sql.Null[int64]
		)
		q := `FOR d IN ` + c + ` RETURN {s: d.s, b: d.b, i: d.i, f: d.f, arr: d.arr, obj: d.obj, n: d.n, missing: d.missing}`
		if err := db.QueryRowContext(t.Context(), q).Scan(&s, &b, &i, &f, &arr, &obj, &n, &m); err != nil {
			t.Fatal(err)
		}
		if s != "é" || !b || i != 9223372036854775807 || f != 1.5 || n.Valid || m.Valid {
			t.Errorf("the values are %q %v %d %v %v %v", s, b, i, f, n, m)
		}
		if fmt.Sprint(arr) != "[1 x]" || fmt.Sprint(obj) != "map[z:1]" {
			t.Errorf("the array is %#v and the object %#v", arr, obj)
		}
	})
}

func TestIntegrationShapes(t *testing.T) {
	c := collection(t, "shapes", `{_key: "a", x: 1}`, `{_key: "b", y: 2}`)
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		rows, err := db.QueryContext(t.Context(), "FOR d IN "+c+" SORT d._key RETURN d")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			n++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		// Two documents with different attributes are one column (D89).
		if !slices.Equal(cols, []string{""}) || n != 2 {
			t.Errorf("the documents gave the columns %q and %d rows", cols, n)
		}
		// A projection whose later row has a new key is an error (D89).
		_, err = db.ExecContext(t.Context(), "FOR d IN "+c+" SORT d._key RETURN d._key == 'a' ? {x: d.x} : {y: d.y}")
		if err == nil {
			t.Error("a row with a key that the first row lacks gave no error")
		}
	})
}

func TestIntegrationBatches(t *testing.T) {
	c := collection(t, "batches")
	exec(t, openWith(t, admin, nil), "FOR i IN 1..2500 INSERT {_key: TO_STRING(i), n: i} INTO "+c)
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		got := column(t, db, "FOR d IN "+c+" SORT d.n RETURN d.n")
		if len(got) != 2500 || got[2499] != int64(2500) {
			t.Errorf("2500 rows in batches of 1000 gave %d rows", len(got))
		}
		// Close before the end returns no error. It deletes the cursor on the
		// server, which TestIntegrationCloseInALaterBatch holds (D90).
		rows, err := db.QueryContext(t.Context(), "FOR d IN "+c+" RETURN d.n")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		if err := rows.Close(); err != nil {
			t.Errorf("closing the rows before their end: %v", err)
		}
	})
}

// TestIntegrationCloseInALaterBatch closes the rows in the middle of the
// second batch, with more than 1 MiB of it left. The driver knows the id of
// the cursor from the first batch, so it deletes the cursor, and a drop of
// the collection does not wait for its ttl (D90).
func TestIntegrationCloseInALaterBatch(t *testing.T) {
	c := collection(t, "later")
	db := openWith(t, admin, nil)
	exec(t, db, "FOR i IN 1..2500 INSERT {_key: TO_STRING(i), n: i, pad: CONCAT_SEPARATOR('', FOR j IN 1..2000 RETURN 'x')} INTO "+c)
	rows, err := db.QueryContext(t.Context(), "FOR d IN "+c+" SORT d.n RETURN d")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for range 1001 {
		if !rows.Next() {
			t.Fatalf("the rows ended before the second batch: %v", rows.Err())
		}
	}
	if err := rows.Close(); err != nil {
		t.Errorf("closing the rows in the second batch: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "DROP COLLECTION "+c); err != nil {
		t.Errorf("dropping the collection after the close: %v, want no wait for the ttl of the cursor", err)
	}
}

// TestIntegrationRollbackAfterTheContext ends the context of a transaction.
// database/sql then rolls it back, and the driver aborts it on the server,
// so it holds its collections no longer (D91).
func TestIntegrationRollbackAfterTheContext(t *testing.T) {
	c := collection(t, "rollback")
	db := openWith(t, admin, nil)
	ctx, cancel := context.WithCancel(t.Context())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT {_key: 'a'} INTO "+c); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("rolling back after the context ended: %v", err)
	}
	dctx, dcancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer dcancel()
	if _, err := db.ExecContext(dctx, "DROP COLLECTION "+c); err != nil {
		t.Errorf("dropping a collection of the transaction: %v, want no wait for its idle timeout", err)
	}
}

func TestIntegrationCancel(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		for _, q := range []string{
			"RETURN SLEEP(5)",
			// The list of running queries keeps the first 4096 bytes of a
			// long text, so its comment goes at the start (D99).
			"LET s = SLEEP(5) LET pad = '" + strings.Repeat("x", 6000) + "' RETURN s",
		} {
			cancelled(t, p, db, q)
		}
	})
}

// cancelled runs q with a context that ends after 300ms, and fails unless the
// driver killed q on the server (D90).
func cancelled(t *testing.T, p principal, db *sql.DB, q string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := db.ExecContext(ctx, q)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a query past its deadline gave %v, want context.DeadlineExceeded", err)
	}
	// The driver killed the query by its comment, so none runs (D90).
	c := NewConnector(config(t, p))
	defer c.transport.CloseIdleConnections()
	var current []struct {
		Query string `json:"query"`
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if err := c.call(t.Context(), http.MethodGet, api(c.cfg.Database, "query/current"), nil, &current, ""); err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(current, func(q struct {
			Query string `json:"query"`
		}) bool {
			return strings.Contains(q.Query, "SLEEP(5)")
		}) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the query still runs on the server 3 seconds after its context ended")
		}
	}
}

func TestIntegrationErrors(t *testing.T) {
	forEach(t, func(t *testing.T, _ principal, db *sql.DB) {
		if e := refused(t, db, "RETUR 1"); e != nil && e.Num != 1501 {
			t.Errorf("a statement that does not parse gave %d, want 1501", e.Num)
		}
		bdb := openWith(t, admin, func(cfg *Config) { cfg.Batch = 1 })
		rows, err := bdb.QueryContext(t.Context(), "FOR i IN 1..4 RETURN ASSERT(i < 3, 'too big')")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			n++
		}
		if e, ok := errors.AsType[*Error](rows.Err()); !ok || e.Num != 1593 || n == 0 {
			t.Errorf("an error after %d rows gave %v, want some rows and then error 1593", n, rows.Err())
		}
	})
}

func TestIntegrationTransactions(t *testing.T) {
	c := collection(t, "tx")
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		key := "k" + p.name
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "INSERT {_key: @k} INTO "+c, sql.Named("k", key)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		same(t, "after the rollback", column(t, db, "RETURN DOCUMENT(@@c, @k) == null", sql.Named("c", c), sql.Named("k", key)), true)
		tx, err = db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(t.Context(), "INSERT {_key: @k} INTO "+c, sql.Named("k", key)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		same(t, "after the commit", column(t, db, "RETURN DOCUMENT(@@c, @k)._key", sql.Named("c", c), sql.Named("k", key)), key)
		ro, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Rollback()
		if _, err := ro.ExecContext(t.Context(), "INSERT {} INTO "+c); err == nil {
			t.Error("a write in a read-only transaction gave no error")
		}
	})
}

func TestIntegrationDDL(t *testing.T) {
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		c := prefix + "ddl_" + p.name
		exec(t, db, "CREATE COLLECTION "+c)
		exec(t, db, "CREATE COLLECTION IF NOT EXISTS "+c)
		exec(t, db, "CREATE UNIQUE INDEX u ON "+c+" (a)")
		exec(t, db, "CREATE INDEX IF NOT EXISTS u ON "+c+" (a)")
		exec(t, db, "INSERT {a: 1} INTO "+c)
		if e := refused(t, db, "INSERT {a: 1} INTO "+c); e != nil && e.Num != 1210 {
			t.Errorf("a duplicate of a unique index gave %d, want 1210", e.Num)
		}
		exec(t, db, "CREATE GEO INDEX g ON "+c+" (loc)")
		exec(t, db, "CREATE INVERTED INDEX v ON "+c+" (t)")
		exec(t, db, "CREATE TTL INDEX x ON "+c+" (exp) EXPIRE AFTER 60")
		exec(t, db, "DROP INDEX u ON "+c)
		exec(t, db, "DROP INDEX IF EXISTS u ON "+c)
		exec(t, db, "INSERT {a: 1} INTO "+c)
		exec(t, db, "DROP COLLECTION "+c)
		exec(t, db, "DROP COLLECTION IF EXISTS "+c)
		ec := prefix + "edge_" + p.name
		exec(t, db, "CREATE COLLECTION "+ec+" EDGE")
		exec(t, db, "INSERT {_from: 'a/1', _to: 'a/2'} INTO "+ec)
		exec(t, db, "DROP COLLECTION "+ec)
	})
}

// TestIntegrationOptions holds the options of one statement and one
// transaction on the server (D109).
func TestIntegrationOptions(t *testing.T) {
	c := collection(t, "options")
	forEach(t, func(t *testing.T, p principal, db *sql.DB) {
		const current = "RETURN CURRENT_DATABASE()"
		same(t, "the database", column(t, db, current), "dbmeta")
		if p == admin {
			same(t, "the database with WithDatabase", column(t, db, current, WithDatabase("_system")), "_system")
		} else {
			_, err := db.ExecContext(t.Context(), current, WithDatabase("_system"))
			if e, ok := errors.AsType[*Error](err); !ok || e.HTTPStatus != http.StatusUnauthorized {
				t.Errorf("the ordinary user in _system gave %v, want HTTP 401", err)
			}
		}
		same(t, "a query with WithParameter", column(t, db, "RETURN 1", WithParameter("ttl", 30)), int64(1))
		start := time.Now()
		_, err := db.ExecContext(t.Context(), "FOR i IN 1..100000000 FILTER i < 0 RETURN i", WithTimeout(300*time.Millisecond))
		if e, ok := errors.AsType[*Error](err); !ok || e.Num != 1500 {
			t.Errorf("the slow query with WithTimeout gave %v, want the error 1500", err)
		}
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("the slow query with WithTimeout of 300ms ended after %v", d)
		}
		if _, err := db.ExecContext(t.Context(), "RETURN 1", WithReadonly(true)); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithReadonly outside a transaction gave %v, want dbimp.ErrNotSupported", err)
		}
		tx, err := db.BeginTx(WithOptions(t.Context(), WithReadonly(true)), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(t.Context(), "INSERT {} INTO "+c); err == nil {
			t.Error("a write in a transaction with WithReadonly gave no error")
		}
	})
}
