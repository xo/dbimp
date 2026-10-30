package tablestore_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp/tablestore"
)

// These tests need an instance of Tablestore. TABLESTORE_DSN is the DSN of the
// driver, in the form of D222:
// tablestore://key:secret@instance.region.ots.aliyuncs.com/instance
// with the access key id and the secret of a RAM user that can create, read,
// write and delete tables of the instance (the policy AliyunOTSFullAccess). A test
// skips when TABLESTORE_DSN is empty (hard rule 9). Tablestore is a hosted
// service that dbrun does not start, so a person runs these tests on an account
// that holds the login that dbsetup made for this work (dbmeta D117), and the
// workflow of CI has no job for them (D222). The login is one user, so each test
// runs as that user only, and there is no ordinary user to run it again as.
//
// SQL has no statement that makes a table, only a mapping of a table to a name,
// so each test makes its tables with the CreateTable call of the API, through
// tablestore.Raw, and maps them with CREATE TABLE. A table has the name of this
// run for a prefix, and a test drops the mapping and deletes the table in a
// cleanup. TestMain deletes a table that a test left, and fails if it found one.

// The variable of the environment.
const envDSN = "TABLESTORE_DSN"

// suffix makes the names of this run unique.
var suffix = strings.ToLower(rand.Text()[:8])

// prefix is the start of the name of each table of this run.
var prefix = "dbimp_it_" + suffix

// table returns the name of a table of this run.
func table(name string) string {
	return prefix + "_" + name
}

// dsn returns the DSN of the instance, and skips the test when there is none.
func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv(envDSN)
	if v == "" {
		t.Skipf("%s is empty, so there is no instance to test against", envDSN)
	}
	return v
}

// integrationConfig returns the configuration of the instance.
func integrationConfig(t *testing.T) tablestore.Config {
	t.Helper()
	cfg, err := tablestore.ParseDSN(dsn(t))
	if err != nil {
		t.Fatalf("reading %s: %v", envDSN, err)
	}
	return *cfg
}

// connect returns a database on the instance. Each request opens a connection,
// because the service resets an idle one now and then.
func connect(t *testing.T) *sql.DB {
	t.Helper()
	c := tablestore.NewConnector(integrationConfig(t))
	c.NoKeepAlives()
	db := sql.OpenDB(c)
	t.Cleanup(func() {
		db.Close()
		_ = c.Close()
	})
	return db
}

// retry runs f, and runs it again up to four times when it fails with a reset of
// the connection. It is for the statements that set up and clean up a test, which
// can run twice.
func retry(f func() error) error {
	var err error
	for range 5 {
		if err = f(); err == nil || !strings.Contains(err.Error(), "connection reset") {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

// raw sends an action of the API of Tablestore that the driver does not send, and
// returns its status and its body.
func raw(t *testing.T, action string, body []byte) (int, []byte) {
	t.Helper()
	status, out, err := tablestore.Raw(context.WithoutCancel(t.Context()), integrationConfig(t), action, body)
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return status, out
}

// makeTable makes the table name with a primary key column id of the type
// keyType, and maps it with the columns cols, such as "s MEDIUMTEXT, i BIGINT".
// The id of the mapping is BIGINT for an integer key and VARCHAR(255) for a string
// key. A cleanup drops the mapping and deletes the table. It returns the name.
func makeTable(t *testing.T, db *sql.DB, name string, keyType int64, cols string) string {
	t.Helper()
	full := table(name)
	if status, body := raw(t, "CreateTable", tablestore.CreateTableBody(full, keyType)); status != 200 {
		t.Fatalf("CreateTable %s answered %d %q", full, status, body)
	}
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_ = retry(func() error {
			_, err := db.ExecContext(ctx, "DROP MAPPING TABLE IF EXISTS "+full)
			return err
		})
		_, _, _ = tablestore.Raw(ctx, integrationConfig(t), "DeleteTable", tablestore.DeleteTableBody(full))
	})
	key := "BIGINT"
	if keyType == tablestore.KeyString {
		key = "VARCHAR(255)"
	}
	if cols != "" {
		cols = ", " + cols
	}
	err := retry(func() error {
		_, err := db.ExecContext(t.Context(), fmt.Sprintf("CREATE TABLE %s (id %s%s, PRIMARY KEY(id));", full, key, cols))
		return err
	})
	if err != nil {
		t.Fatalf("mapping the table %s: %v", full, err)
	}
	return full
}

// exec runs a statement that must succeed, and returns its result.
func exec(t *testing.T, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return res
}

// affected returns the count of the rows that the result holds.
func affected(t *testing.T, res sql.Result) int64 {
	t.Helper()
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("reading the rows affected: %v", err)
	}
	return n
}

// rowsOf runs the query and returns its rows, each value as the Go value that
// database/sql scans into an *any.
func rowsOf(t *testing.T, db *sql.DB, query string, args ...any) [][]any {
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
		row := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// TestMain deletes the tables of this run that a test left, and fails when it
// found one, as a cleanup that did not run is a fault of the test.
func TestMain(m *testing.M) {
	code := m.Run()
	if v := os.Getenv(envDSN); v != "" {
		if cfg, err := tablestore.ParseDSN(v); err == nil {
			if left := sweep(*cfg); left > 0 && code == 0 {
				fmt.Fprintf(os.Stderr, "the tests left %d tables with the prefix %s\n", left, prefix)
				code = 1
			}
		}
	}
	os.Exit(code)
}

// sweep deletes each table with the prefix of this run, with its search indexes,
// and returns the count.
func sweep(cfg tablestore.Config) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c := tablestore.NewConnector(cfg)
	c.NoKeepAlives()
	n := sweepTables(ctx, c)
	_ = c.Close()
	return n
}

// sweepTables deletes each table of the instance with the prefix of this run, and
// returns the count.
func sweepTables(ctx context.Context, c *tablestore.Connector) int {
	names, err := c.ListTables(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, name := range names {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		n++
		// The server refuses to delete a table that has a search index (D226).
		indexes, _ := c.ListSearchIndexes(ctx, name)
		for _, ix := range indexes {
			_ = c.DeleteSearchIndex(ctx, name, ix, tablestore.WithIfExists())
		}
		_ = c.DeleteTable(ctx, name, tablestore.WithIfExists())
	}
	return n
}
