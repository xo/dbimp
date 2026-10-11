package exasol_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/exasol"
)

// These tests need a server. They read EXASOL_DSN, which is the DSN of the
// administrator, and skip when it is empty (D9). dbrun makes one user, sys, who
// is the administrator, so the ordinary user is one that the tests make: it has
// CREATE SESSION and the privilege to select in the schema of the test, and
// nothing else (docs/EXASOL.md, Principals). EXASOL_ORDINARY_DSN names a user
// that a caller made, and the tests use it in place of making one.

const (
	envDSN         = "EXASOL_DSN"
	envOrdinaryDSN = "EXASOL_ORDINARY_DSN"
)

// principal is the user that a test runs as.
type principal string

const (
	administrator principal = "administrator"
	ordinary      principal = "ordinary"
)

// adminDSN returns the DSN of the administrator, or skips the test.
func adminDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(envDSN)
	if dsn == "" {
		t.Skipf("%s is empty", envDSN)
	}
	return dsn
}

// openDSN opens dsn, and closes the database with the test.
func openDSN(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open(exasol.Name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// suffix returns a name that no other run of the tests uses, in capitals and
// digits, because the server writes an unquoted name in capitals.
func suffix() string {
	return strings.ToUpper(rand.Text())[:10]
}

// cleanup runs query as the administrator when the test ends, even when the
// context of the test ended.
func cleanup(t *testing.T, adm *sql.DB, query string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := adm.ExecContext(context.WithoutCancel(t.Context()), query); err != nil {
			t.Errorf("cleaning up with %s: %v", query, err)
		}
	})
}

// env is what a test of one principal reads and writes.
type env struct {
	p principal
	// db is the database of the principal, and adm the administrator's.
	db, adm *sql.DB
	// schema is the schema that the test made. The principal can select in
	// it.
	schema string
	// dsn is the DSN of the principal.
	dsn string
}

// forEach runs fn once as the administrator and once as the ordinary user, each
// in a schema of its own that the administrator makes, and drops at the end.
func forEach(t *testing.T, fn func(t *testing.T, e env)) {
	t.Helper()
	adsn := adminDSN(t)
	for _, p := range []principal{administrator, ordinary} {
		t.Run(string(p), func(t *testing.T) {
			adm := openDSN(t, adsn)
			schema := "DBIMP_" + suffix()
			mustExec(t, adm, "CREATE SCHEMA "+schema)
			cleanup(t, adm, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			e := env{p: p, db: adm, adm: adm, schema: schema, dsn: adsn}
			if p == ordinary {
				e.dsn = ordinaryUserDSN(t, adm, adsn, schema)
				e.db = openDSN(t, e.dsn)
			}
			fn(t, e)
		})
	}
}

// ordinaryUserDSN returns the DSN of an ordinary user, who can log in and read
// the schema. It makes the user, and drops it when the test ends, unless a
// caller names one in EXASOL_ORDINARY_DSN.
func ordinaryUserDSN(t *testing.T, adm *sql.DB, adsn, schema string) string {
	t.Helper()
	user := "DBIMP_USER_" + suffix()
	if dsn := os.Getenv(envOrdinaryDSN); dsn != "" {
		mustExec(t, adm, "GRANT SELECT ON SCHEMA "+schema+" TO "+currentUser(t, openDSN(t, dsn)))
		return dsn
	}
	cfg, err := exasol.ParseDSN(adsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.User, cfg.Password = user, "Pw-"+suffix()
	mustExec(t, adm, fmt.Sprintf("CREATE USER %s IDENTIFIED BY \"%s\"", user, cfg.Password))
	cleanup(t, adm, "DROP USER "+user+" CASCADE")
	mustExec(t, adm, "GRANT CREATE SESSION TO "+user)
	mustExec(t, adm, "GRANT SELECT ON SCHEMA "+schema+" TO "+user)
	return cfg.FormatDSN()
}

func currentUser(t *testing.T, db *sql.DB) string {
	t.Helper()
	var u string
	if err := db.QueryRowContext(t.Context(), "SELECT CURRENT_USER").Scan(&u); err != nil {
		t.Fatal(err)
	}
	return u
}

// mustExec runs query, and fails the test on an error.
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return res
}

// forbidden makes sure that the server refuses query for lack of a privilege,
// with the code 42500 (recorded: "create a table without the privilege").
func forbidden(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), query)
	if e, ok := errors.AsType[*exasol.Error](err); !ok || e.Code != "42500" {
		t.Errorf("%s gave %v, want an *exasol.Error with the code 42500", query, err)
	}
}

// querier is a database, a connection or a transaction, which run a query.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// read runs query and returns the first column of each row as T.
func read[T any](t *testing.T, db querier, query string, args ...any) []T {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var v T
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// want compares got with want, and fails the test if they differ.
func want[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s is %v, want %v", what, got, want)
	}
}

// TestIntegrationVersion reads the version that the login gave, and the version
// that the metadata holds, as each principal (docs/EXASOL.md, Principals).
func TestIntegrationVersion(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		got := read[string](t, e.db, "SELECT PARAM_VALUE FROM EXA_METADATA WHERE PARAM_NAME = 'databaseProductVersion'")
		if len(got) != 1 || got[0] == "" {
			t.Errorf("the version is %q, want one text", got)
		}
		if err := e.db.PingContext(t.Context()); err != nil {
			t.Errorf("Ping: %v", err)
		}
		if u := currentUser(t, e.db); e.p == administrator && u != "SYS" {
			t.Errorf("the user is %s, want SYS", u)
		}
	})
}

// TestIntegrationLoginRefused holds D197 on the server: a wrong password and a
// wrong user match dbimp.ErrAuthentication, and a missing privilege does not
// (recorded: "login with a wrong password: send the credentials").
func TestIntegrationLoginRefused(t *testing.T) {
	dsn := adminDSN(t)
	cfg, err := exasol.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*exasol.Config){
		"a wrong password": func(c *exasol.Config) { c.Password += "x" },
		"a wrong user":     func(c *exasol.Config) { c.User = "NOSUCHUSER" },
	} {
		t.Run(name, func(t *testing.T) {
			c := *cfg
			mutate(&c)
			err := openDSN(t, c.FormatDSN()).PingContext(t.Context())
			if !errors.Is(err, dbimp.ErrAuthentication) {
				t.Errorf("the login gave %v, want one that matches dbimp.ErrAuthentication", err)
			}
			if x, ok := errors.AsType[*exasol.Error](err); !ok || x.Code != exasol.CodeConnection {
				t.Errorf("the login gave %v, want an *exasol.Error with the code 08004", err)
			}
		})
	}
	t.Run("a missing privilege", func(t *testing.T) {
		forEach(t, func(t *testing.T, e env) {
			if e.p != ordinary {
				return
			}
			_, err := e.db.ExecContext(t.Context(), "CREATE TABLE "+e.schema+".NOPE (A DECIMAL(9,0))")
			if err == nil || errors.Is(err, dbimp.ErrAuthentication) {
				t.Errorf("the statement gave %v, want an error that does not match dbimp.ErrAuthentication", err)
			}
		})
	})
}

// TestIntegrationSchemaOfTheDSN holds D205: the path of the DSN is the schema
// that the session opens at the login.
func TestIntegrationSchemaOfTheDSN(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		cfg, err := exasol.ParseDSN(e.dsn)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Schema = e.schema
		db := openDSN(t, cfg.FormatDSN())
		want(t, "the current schema", read[string](t, db, "SELECT CURRENT_SCHEMA"), []string{e.schema})
	})
}

// TestIntegrationLargeResult reads a result of 300000 rows by its handle, in
// pages of the size of fetchsize, with and without compression, and reads rows
// of a second statement between two pages (D210).
func TestIntegrationLargeResult(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		for _, tt := range []struct{ name, query string }{
			{"default", ""}, {"small pages", "&fetchsize=16"}, {"no compression", "&compression=false"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				db := openDSN(t, e.dsn+tt.query)
				rows, err := db.QueryContext(t.Context(), "SELECT LEVEL AS N, LPAD('x', 20, 'ab') AS T FROM DUAL CONNECT BY LEVEL <= 300000 ORDER BY 1")
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				var n, sum int64
				for rows.Next() {
					var v int64
					var s string
					if err := rows.Scan(&v, &s); err != nil {
						t.Fatal(err)
					}
					n++
					sum += v
					if v != n || len(s) != 20 {
						t.Fatalf("row %d holds %d and %q", n, v, s)
					}
				}
				if err := rows.Err(); err != nil || n != 300000 || sum != 300000*300001/2 {
					t.Errorf("read %d rows, sum %d, %v, want 300000 rows", n, sum, err)
				}
			})
		}
		t.Run("a second statement between two pages", func(t *testing.T) {
			db := openDSN(t, e.dsn+"&fetchsize=1")
			c := pin(t, db)
			rows, err := c.QueryContext(t.Context(), "SELECT LEVEL FROM DUAL CONNECT BY LEVEL <= 5000 ORDER BY 1")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var n int
			for rows.Next() {
				n++
				if n%1000 == 0 {
					want(t, "the statement between two pages", read[int64](t, c, "SELECT ?", n), []int64{int64(n)})
				}
			}
			if err := rows.Err(); err != nil || n != 5000 {
				t.Errorf("read %d rows, %v, want 5000", n, err)
			}
		})
		t.Run("close before the end", func(t *testing.T) {
			db := openDSN(t, e.dsn)
			c := pin(t, db)
			rows, err := c.QueryContext(t.Context(), "SELECT LEVEL FROM DUAL CONNECT BY LEVEL <= 100000")
			if err != nil {
				t.Fatal(err)
			}
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			if err := rows.Close(); err != nil { //nolint:sqlclosecheck // The test reads the error of Close.
				t.Errorf("Close: %v", err)
			}
			// The handle is closed, so the session has none open, and it works.
			want(t, "the statement after the close", read[int64](t, c, "SELECT 5"), []int64{5})
		})
	})
}

// TestIntegrationIntervalOverflow holds D206: an INTERVAL YEAR TO MONTH that
// does not fit the Months of dbimp.Interval is an error, and the values before
// it are read.
func TestIntegrationIntervalOverflow(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		rows, err := e.db.QueryContext(t.Context(), "SELECT CAST(? AS DECIMAL(9,0)) AS N, INTERVAL '5-3' YEAR(9) TO MONTH AS I FROM DUAL UNION ALL SELECT 2, INTERVAL '999999999-11' YEAR(9) TO MONTH FROM DUAL ORDER BY 1", 1)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		var n int64
		var iv dbimp.Interval
		if err := rows.Scan(&n, &iv); err != nil || iv.Months != 63 {
			t.Errorf("the first row is %d, %v, %v, want 63 months", n, iv, err)
		}
		if rows.Next() {
			t.Error("the row of 11999999999 months was read")
		}
		if err := rows.Err(); !errors.Is(err, dbimp.ErrInvalidValue) || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("the error is %v, want one that wraps dbimp.ErrInvalidValue and dbimp.ErrIncomplete", err)
		}
	})
}

// TestIntegrationConcurrentQueries holds hard rule 12 of docs/DRIVER.md: two
// queries run at the same time on one sql.DB, each on its own session.
func TestIntegrationConcurrentQueries(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for i := range 4 {
			wg.Go(func() {
				var got int64
				if err := e.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM (SELECT LEVEL FROM DUAL CONNECT BY LEVEL <= ?)", 1000*(i+1)).Scan(&got); err != nil || got != int64(1000*(i+1)) {
					errs <- fmt.Errorf("query %d gave %d, %w", i, got, err)
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Error(err)
		}
	})
}
