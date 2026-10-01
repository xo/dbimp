package avatica_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/avatica"
)

// These tests need a server. AVATICA_DSN names it for the administrator, and
// AVATICA_ORDINARY_DSN for the ordinary user, which the Phoenix Query Server
// does not have (D9). A test skips when its DSN is empty. The tables of a
// run start with a prefix of their own, and the administrator makes each
// one. A test that holds for one flavor only skips on the other.

// suffix makes the names of this run unique.
var suffix = strconv.FormatInt(time.Now().UnixNano()%1e9, 36)

// prefix starts each table of this run.
var prefix = strings.ToUpper("DBIMP_IT_" + suffix + "_")

// principal is a user that the tests run as.
type principal struct {
	name string
	env  string
}

var (
	admin    = principal{"administrator", "AVATICA_DSN"}
	ordinary = principal{"ordinary", "AVATICA_ORDINARY_DSN"}
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
	db, err := sql.Open(avatica.Name, dsn(t, p))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// The flavors of the database behind the server (docs/AVATICA.md, Flavors).
const (
	hsqldb  = "hsqldb"
	phoenix = "phoenix"
)

// flavor returns the flavor of the database of db. VALUES is a statement of
// HSQLDB that Phoenix refuses (measured).
func flavor(t *testing.T, db *sql.DB) string {
	t.Helper()
	var n int64
	if err := db.QueryRowContext(t.Context(), "VALUES (1)").Scan(&n); err != nil {
		return phoenix
	}
	return hsqldb
}

// only skips the test unless the database of db is of the flavor f.
func only(t *testing.T, db *sql.DB, f string) {
	t.Helper()
	if got := flavor(t, db); got != f {
		t.Skipf("the server runs %s, and this test is for %s", got, f)
	}
}

// table returns the name of a table of this run.
func table(name string) string {
	return prefix + strings.ToUpper(name)
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

// drop drops each object in a cleanup, which runs even when the test fails.
// Each one is a kind and a name, such as "TABLE T".
func drop(t *testing.T, db *sql.DB, objects ...string) {
	t.Helper()
	t.Cleanup(func() {
		// The context of the test ends before its cleanup runs.
		ctx := context.WithoutCancel(t.Context())
		for _, o := range objects {
			if _, err := db.ExecContext(ctx, "DROP "+o); err != nil {
				t.Errorf("dropping %s: %v", o, err)
			}
		}
	})
}

// read runs query, and reads its first column into a []T.
func read[T any](t *testing.T, db *sql.DB, query string, args ...any) []T {
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
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// dec returns the decimal s.
func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// serverError reports whether err is an error of the server whose message
// holds want.
func serverError(err error, want string) bool {
	e, ok := errors.AsType[*avatica.Error](err)
	return ok && strings.Contains(e.Message+" "+e.Exception, want)
}

func TestIntegrationPing(t *testing.T) {
	for _, p := range []principal{admin, ordinary} {
		t.Run(p.name, func(t *testing.T) {
			if err := openAs(t, p).PingContext(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestIntegrationOrdinaryUser holds what the ordinary user of HSQLDB can do:
// read the table that dbmeta granted, and nothing that it did not grant
// (recorded: "a readable table" and "a table that the user was not
// granted").
func TestIntegrationOrdinaryUser(t *testing.T) {
	adm := openAs(t, admin)
	db := openAs(t, ordinary)
	tb := table("ordinary")
	exec(t, adm, "CREATE TABLE "+tb+" (K INTEGER PRIMARY KEY)")
	drop(t, adm, "TABLE "+tb)
	if got := read[int64](t, db, "SELECT COUNT(*) FROM DBMETA.READABLE"); len(got) != 1 {
		t.Errorf("the readable table gave %v, want one count", got)
	}
	for _, stmt := range []string{"SELECT K FROM " + tb, "INSERT INTO " + tb + " VALUES (1)"} {
		if _, err := db.ExecContext(t.Context(), stmt); !serverError(err, "user lacks privilege") {
			t.Errorf("%s as the ordinary user gave %v, want the refusal of the server", stmt, err)
		}
	}
}

// TestIntegrationWrongPassword holds that a wrong password fails the first
// statement, and is no driver.ErrBadConn for the statement (recorded: "a
// wrong password").
func TestIntegrationWrongPassword(t *testing.T) {
	cfg, err := avatica.ParseDSN(dsn(t, ordinary))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Password += "-wrong"
	db := sql.OpenDB(avatica.NewConnector(*cfg))
	defer db.Close()
	if err := db.PingContext(t.Context()); err == nil {
		t.Error("a wrong password gave no error")
	}
}

// TestIntegrationErrors holds that an error of the server reaches the
// caller with its message (recorded: "a syntax error" and "an unknown
// table").
func TestIntegrationErrors(t *testing.T) {
	db := openAs(t, admin)
	for _, stmt := range []string{"SELEC 1", "SELECT * FROM " + table("nosuch")} {
		_, err := db.ExecContext(t.Context(), stmt)
		if e, ok := errors.AsType[*avatica.Error](err); !ok || e.Message == "" || e.HTTPStatus != 500 {
			t.Errorf("%s gave %v, want an *avatica.Error of HTTP 500 with its message", stmt, err)
		}
	}
}

// TestIntegrationErrorInResult holds that HSQLDB computes the whole result
// before it sends the first frame, so an error in any row of it arrives
// before the first row, and does not wrap dbimp.ErrIncomplete (measured).
func TestIntegrationErrorInResult(t *testing.T) {
	db := openAs(t, admin)
	only(t, db, hsqldb)
	ctx := avatica.WithOptions(t.Context(), avatica.WithFrameSize(3))
	_, err := db.ExecContext(ctx, "SELECT CASE WHEN V < 5 THEN V ELSE 1 / (V - 5) END FROM UNNEST(SEQUENCE_ARRAY(1, 6, 1)) AS T(V)")
	if !serverError(err, "division by zero") || errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the query gave %v, want the division by zero before any row", err)
	}
}

// TestIntegrationManyFrames holds D157 for a result of many frames.
func TestIntegrationManyFrames(t *testing.T) {
	db := openAs(t, admin)
	only(t, db, hsqldb)
	ctx := avatica.WithOptions(t.Context(), avatica.WithFrameSize(100))
	rows, err := db.QueryContext(ctx, "SELECT V FROM UNNEST(SEQUENCE_ARRAY(1, 2500, 1)) AS T(V)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n, v int64
	for rows.Next() {
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		if n++; v != n {
			t.Fatalf("row %d holds %d", n, v)
		}
	}
	if err := rows.Err(); err != nil || n != 2500 {
		t.Errorf("read %d rows and %v, want 2500", n, err)
	}
}

// TestIntegrationCancel holds D159: the context of a query stops the read,
// and the connection runs the next statement.
func TestIntegrationCancel(t *testing.T) {
	db := openAs(t, admin)
	only(t, db, hsqldb)
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(t.Context())
	ctx = avatica.WithOptions(ctx, avatica.WithFrameSize(10))
	rows, err := db.QueryContext(ctx, "SELECT V FROM UNNEST(SEQUENCE_ARRAY(1, 1000, 1)) AS T(V)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	cancel()
	for rows.Next() {
	}
	if err := rows.Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("the read after the end of the context gave %v, want context.Canceled", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got := read[int64](t, db, "VALUES (1)"); len(got) != 1 {
		t.Errorf("the next statement gave %v, want 1", got)
	}
}

// TestIntegrationTimeout holds D159: the deadline of the context ends the
// request, though the server runs the statement to its end.
func TestIntegrationTimeout(t *testing.T) {
	db := openAs(t, admin)
	only(t, db, hsqldb)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := db.ExecContext(ctx, "SELECT COUNT(*) FROM UNNEST(SEQUENCE_ARRAY(1, 3000, 1)) AS A(X), UNNEST(SEQUENCE_ARRAY(1, 3000, 1)) AS B(Y)")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
		t.Errorf("the statement gave %v after %v, want context.DeadlineExceeded at once", err, time.Since(start))
	}
}

// TestIntegrationParameters holds D158: each Go type binds as its
// TypedValue, a decimal keeps every digit on HSQLDB, and a count of
// arguments that is not the count of the parameters is an error.
func TestIntegrationParameters(t *testing.T) {
	db := openAs(t, admin)
	only(t, db, hsqldb)
	var s string
	err := db.QueryRowContext(t.Context(), "VALUES (CAST(CAST(? AS DECIMAL(38,10)) AS VARCHAR(50)))", dec(t, "1234567890123456789012345678.0123456789")).Scan(&s)
	if err != nil || s != "1234567890123456789012345678.0123456789" {
		t.Errorf("the decimal read back as %q and %v, want every digit", s, err)
	}
	if err := db.QueryRowContext(t.Context(), "VALUES (CAST(? AS VARCHAR(20)))", "é 日本 🙂").Scan(&s); err != nil || s != "é 日本 🙂" {
		t.Errorf("the text read back as %q and %v, want é 日本 🙂", s, err)
	}
	if _, err := db.ExecContext(t.Context(), "VALUES (CAST(? AS INTEGER), CAST(? AS INTEGER))", 1); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("one argument for two parameters gave %v, want dbimp.ErrArguments", err)
	}
}

// TestIntegrationPhoenixText holds D158 on Phoenix: text outside ASCII
// survives, because the driver escapes it.
func TestIntegrationPhoenixText(t *testing.T) {
	db := openAs(t, admin)
	only(t, db, phoenix)
	tb := table("text")
	exec(t, db, "CREATE TABLE "+tb+" (K INTEGER NOT NULL PRIMARY KEY, V VARCHAR)")
	drop(t, db, "TABLE IF EXISTS "+tb)
	exec(t, db, "UPSERT INTO "+tb+" VALUES (1, ?)", "é 日本 🙂")
	exec(t, db, "UPSERT INTO "+tb+" VALUES (2, 'ü')")
	if got := read[string](t, db, "SELECT V FROM "+tb+" ORDER BY K"); fmt.Sprint(got) != "[é 日本 🙂 ü]" {
		t.Errorf("the text read back as %q, want é 日本 🙂 and ü", got)
	}
}
