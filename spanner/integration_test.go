package spanner_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/spanner"
)

// These tests need a database. SPANNER_DSN is the DSN of the driver, in the form of
// D191: spanner://host:port/project/instance/database?credential_file=/path/key.json,
// with the key file of a service account that holds roles/spanner.databaseAdmin on the
// database, as dbsetup makes it (D187), or the DSN of the Cloud Spanner emulator,
// spanner://localhost:9020/project/instance/database, which needs no credential. A
// test skips when SPANNER_DSN is empty (hard rule 9). The hosted service is not one
// that dbrun starts, so a person runs these tests with the login that dbsetup made for
// this work, and the workflow of CI has no job for them until dbmeta has the entry of
// the emulator (D187 and D191 item 2). The login is one service account with one role
// on one database and no role on the instance, so each test runs as that account only.
//
// The tests make their tables, indexes, views, sequences, change streams and schemas in
// the database of the DSN, with the name of this run for a prefix. A test drops what it
// makes, even when it fails. TestMain drops what a test left, and fails if it left
// anything. A DDL statement takes seconds on the hosted service, so the tests run one
// after the other.

// envDSN is the variable that holds the DSN.
const envDSN = "SPANNER_DSN"

// suffix makes the names of this run unique. A name of Spanner is lower case letters,
// digits and underscores, and starts with a letter.
var suffix = strings.ToLower(rand.Text()[:8])

// prefix is the start of the name of each object of this run.
var prefix = "dbimp_it_" + suffix

// name returns the name of an object of this run.
func name(n string) string {
	return prefix + "_" + n
}

// dsn returns the DSN of the database, and skips the test when there is none.
func dsn(t testing.TB) string {
	t.Helper()
	v := os.Getenv(envDSN)
	if v == "" {
		t.Skipf("%s is empty, so there is no database to test against", envDSN)
	}
	return v
}

// connect returns a database on the database of the DSN.
func connect(t testing.TB) *sql.DB {
	t.Helper()
	cfg, err := spanner.ParseDSN(dsn(t))
	if err != nil {
		t.Fatalf("reading %s: %v", envDSN, err)
	}
	db := sql.OpenDB(spanner.NewConnector(*cfg))
	// Closing the database closes the connector.
	t.Cleanup(func() { db.Close() })
	return db
}

// exec runs a statement, and fails the test when it fails.
func exec(t testing.TB, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return res
}

// affected runs a statement and returns the count of the rows that it changed.
func affected(t testing.TB, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	n, err := exec(t, db, query, args...).RowsAffected()
	if err != nil {
		t.Fatalf("%s: RowsAffected: %v", query, err)
	}
	return n
}

// ddl runs the statement that makes an object, and registers the statements that
// drop it, which run when the test ends, in the order given, even when the test
// failed. A cleanup that fails fails the test.
func ddl(t testing.TB, db *sql.DB, create string, drops ...string) {
	t.Helper()
	// The drops run after the create, and before the drops that an earlier call of
	// ddl registered, because t.Cleanup runs the last registered first.
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		for _, drop := range drops {
			if _, err := db.ExecContext(ctx, drop); err != nil {
				t.Errorf("cleaning up: %s: %v", drop, err)
			}
		}
	})
	if _, err := db.ExecContext(t.Context(), create); err != nil {
		t.Fatalf("%s: %v", create, err)
	}
}

// rowsOf reads every row of a query into *any values.
func rowsOf(t testing.TB, db *sql.DB, query string, args ...any) [][]any {
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
			t.Fatalf("%s: scanning: %v", query, err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: after %d rows: %v", query, len(out), err)
	}
	return out
}

// failure runs a query to its end, and returns the error that it ended with, from the
// query or from the rows. A test uses it for a statement that the server refuses.
func failure(t testing.TB, db *sql.DB, query string, args ...any) error {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// count returns the count of the rows of a query that selects one number.
func count(t testing.TB, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	rows := rowsOf(t, db, query, args...)
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("%s: read %v, want one number", query, rows)
	}
	n, ok := rows[0][0].(int64)
	if !ok {
		t.Fatalf("%s: read %#v, want an int64", query, rows[0][0])
	}
	return n
}

// TestMain drops each object of this run that a test left, and fails if it found
// one.
func TestMain(m *testing.M) {
	code := m.Run()
	if left := leftovers(); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "the tests left %d objects with the prefix %s, which TestMain dropped: %v\n", len(left), prefix, left)
		code = 1
	}
	os.Exit(code)
}

// objects are the objects that the tests make, with the query that lists their
// names and the statement that drops one. The order is the order of the drops: a
// change stream, a view, an index and a table go before the sequence and the schema
// that they use.
var objects = []struct {
	query string
	drop  string
}{
	{"SELECT change_stream_name FROM information_schema.change_streams WHERE STARTS_WITH(change_stream_name, @p)", "DROP CHANGE STREAM "},
	{"SELECT table_name FROM information_schema.views WHERE table_schema = '' AND STARTS_WITH(table_name, @p)", "DROP VIEW "},
	{"SELECT index_name FROM information_schema.indexes WHERE table_schema = '' AND index_type = 'SEARCH' AND STARTS_WITH(index_name, @p)", "DROP SEARCH INDEX "},
	{"SELECT index_name FROM information_schema.indexes WHERE table_schema = '' AND index_type = 'INDEX' AND STARTS_WITH(index_name, @p)", "DROP INDEX "},
	{"SELECT table_name FROM information_schema.tables WHERE table_schema = '' AND STARTS_WITH(table_name, @p) ORDER BY parent_table_name IS NULL, table_name DESC", "DROP TABLE "},
	{"SELECT name FROM information_schema.sequences WHERE STARTS_WITH(name, @p)", "DROP SEQUENCE "},
	{"SELECT schema_name FROM information_schema.schemata WHERE STARTS_WITH(schema_name, @p)", "DROP SCHEMA "},
}

// leftovers drops each object of this run, and returns their names. It returns
// none when there is no database.
func leftovers() []string {
	v := os.Getenv(envDSN)
	if v == "" {
		return nil
	}
	cfg, err := spanner.ParseDSN(v)
	if err != nil {
		return nil
	}
	db := sql.OpenDB(spanner.NewConnector(*cfg))
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var left []string
	for _, o := range objects {
		names, err := listNames(ctx, db, o.query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "looking for the objects that the tests left: %v\n", err)
			return []string{"?"}
		}
		for _, n := range names {
			left = append(left, n)
			if _, err := db.ExecContext(ctx, o.drop+n); err != nil {
				fmt.Fprintf(os.Stderr, "dropping %s: %v\n", n, err)
			}
		}
	}
	return left
}

// listNames returns the names that the query lists for this run.
func listNames(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, sql.Named("p", prefix))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// TestIntegrationConnect holds that the driver connects, pings, reports the type
// of each column, and keeps one session for all its connections (D191 item 3).
func TestIntegrationConnect(t *testing.T) {
	db := connect(t)
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(t.Context(), "SELECT 1 AS i, 'x' AS s, [1.5] AS a, CAST(1 AS NUMERIC) AS n")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	defer func() {
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	}()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"INT64", "STRING", "ARRAY", "NUMERIC"}
	for i, ct := range types {
		if ct.DatabaseTypeName() != want[i] {
			t.Errorf("the type of %s is %s, want %s", ct.Name(), ct.DatabaseTypeName(), want[i])
		}
	}
	// Several connections share the session of the connector.
	db.SetMaxOpenConns(4)
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			var n int64
			errs <- db.QueryRowContext(t.Context(), "SELECT 1").Scan(&n)
		}()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// TestIntegrationErrors holds the errors of the server (docs/SPANNER.md, "Errors"):
// a syntax error, a missing table, a duplicate key, an error after the first
// message of a stream, and a message with real line breaks.
func TestIntegrationErrors(t *testing.T) {
	db := connect(t)
	err := failure(t, db, "SELEC 1")
	serr, ok := errors.AsType[*spanner.Error](err)
	if !ok || serr.Status != "INVALID_ARGUMENT" || serr.HTTPStatus != 400 || strings.Contains(serr.Message, `\n`) {
		t.Errorf("a syntax error gave %v, want an *spanner.Error INVALID_ARGUMENT with HTTP 400", err)
	}
	err = failure(t, db, "SELECT * FROM "+name("nosuch"))
	if serr, ok := errors.AsType[*spanner.Error](err); !ok || !strings.Contains(serr.Message, "Table not found") {
		t.Errorf("a missing table gave %v", err)
	}
	tbl := name("err_dup")
	ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL) PRIMARY KEY (id)", "DROP TABLE "+tbl)
	exec(t, db, "INSERT INTO "+tbl+" (id) VALUES (1)")
	_, err = db.ExecContext(t.Context(), "INSERT INTO "+tbl+" (id) VALUES (1)")
	if serr, ok := errors.AsType[*spanner.Error](err); !ok || serr.Status != "ALREADY_EXISTS" || serr.HTTPStatus != 409 {
		t.Errorf("a duplicate key gave %v, want ALREADY_EXISTS with HTTP 409", err)
	}
	// An error after some rows: the server sends the rows, and then the error.
	rows, err := db.QueryContext(t.Context(), "SELECT x, REPEAT('y', 500) AS pad, 1 / (x - 3500) AS v FROM UNNEST(GENERATE_ARRAY(1, 4000)) AS x")
	if err != nil {
		if !errors.Is(err, dbimp.ErrIncomplete) {
			var serr *spanner.Error
			if !errors.As(err, &serr) || serr.Status != "OUT_OF_RANGE" {
				t.Fatalf("the error is %v, want OUT_OF_RANGE", err)
			}
		}
		return
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err == nil || !errors.Is(err, dbimp.ErrIncomplete) && n > 0 {
		t.Errorf("after %d rows the error is %v, want one that wraps dbimp.ErrIncomplete", n, err)
	}
}

// drain runs a query with a context to its end, and returns the error that it ended with.
func drain(ctx context.Context, db *sql.DB, query string) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

// TestIntegrationContext holds that a context that ends stops a query and a DDL
// statement: the error wraps the error of the context, and the statement of a DDL
// call is canceled on the server (D191 item 4).
func TestIntegrationContext(t *testing.T) {
	db := connect(t)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	err := drain(ctx, db, "SELECT x, REPEAT('y', 1000) AS pad FROM UNNEST(GENERATE_ARRAY(1, 2000000)) AS x")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a query that outlived its context gave %v, want context.DeadlineExceeded", err)
	}
	tbl := name("ctx")
	ctx2, cancel2 := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel2()
	_, err = db.ExecContext(ctx2, "CREATE TABLE "+tbl+" (id INT64 NOT NULL) PRIMARY KEY (id)")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a DDL statement that outlived its context gave %v, want context.DeadlineExceeded", err)
	}
	// The cancel can come after the statement ended, so the table can exist or
	// not. Drop it if it does, and wait until the operation of the cancel is done.
	_, _ = db.ExecContext(context.WithoutCancel(t.Context()), "DROP TABLE IF EXISTS "+tbl)
}

// TestIntegrationTransactionRetry holds D191 item 6 by the use that a caller makes
// of ErrAborted: it runs the whole transaction again.
func TestIntegrationTransactionRetry(t *testing.T) {
	db := connect(t)
	tbl := name("retry")
	ddl(t, db, "CREATE TABLE "+tbl+" (id INT64 NOT NULL, n INT64) PRIMARY KEY (id)", "DROP TABLE "+tbl)
	exec(t, db, "INSERT INTO "+tbl+" (id, n) VALUES (1, 0)")
	increment := func() error {
		for range 10 {
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				return err
			}
			var n int64
			if err := tx.QueryRowContext(t.Context(), "SELECT n FROM "+tbl+" WHERE id = 1").Scan(&n); err != nil {
				_ = tx.Rollback()
				if errors.Is(err, spanner.ErrAborted) {
					continue
				}
				return err
			}
			if _, err := tx.ExecContext(t.Context(), "UPDATE "+tbl+" SET n = @n WHERE id = 1", sql.Named("n", n+1)); err != nil {
				_ = tx.Rollback()
				if errors.Is(err, spanner.ErrAborted) {
					continue
				}
				return err
			}
			if err := tx.Commit(); err != nil {
				if errors.Is(err, spanner.ErrAborted) {
					continue
				}
				return err
			}
			return nil
		}
		return errors.New("the transaction aborted ten times")
	}
	errs := make(chan error, 4)
	for range 4 {
		go func() { errs <- increment() }()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if n := count(t, db, "SELECT n FROM "+tbl+" WHERE id = 1"); n != 4 {
		t.Errorf("four transactions that add one gave %d, want 4", n)
	}
}

// sameRows reports whether two lists of rows are equal.
func sameRows(a, b [][]any) bool {
	return reflect.DeepEqual(a, b)
}
