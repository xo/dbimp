package spanner //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestSplitStatements holds D198: the text is cut at a semicolon that is outside
// a literal, a quoted name and a comment, and a statement that holds nothing is
// dropped.
func TestSplitStatements(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		text string
		want []string
	}{
		{"one", "DROP TABLE a", []string{"DROP TABLE a"}},
		{"trailing semicolon", "DROP TABLE a;\n", []string{"DROP TABLE a"}},
		{"two", "DROP TABLE a; DROP TABLE b", []string{"DROP TABLE a", "DROP TABLE b"}},
		{"empty statements", ";; DROP TABLE a ;\n;  ;DROP TABLE b;;", []string{"DROP TABLE a", "DROP TABLE b"}},
		{"nothing", " ; ;\n", nil},
		{"single quote", "ALTER TABLE a ALTER COLUMN s SET DEFAULT ('x;y'); DROP TABLE b", []string{"ALTER TABLE a ALTER COLUMN s SET DEFAULT ('x;y')", "DROP TABLE b"}},
		{"double quote", `ALTER TABLE a ALTER COLUMN s SET DEFAULT ("x;y"); DROP TABLE b`, []string{`ALTER TABLE a ALTER COLUMN s SET DEFAULT ("x;y")`, "DROP TABLE b"}},
		{"escaped quote", `CREATE TABLE a (s STRING(9) DEFAULT ('it\'s;')) PRIMARY KEY (s); DROP TABLE b`, []string{`CREATE TABLE a (s STRING(9) DEFAULT ('it\'s;')) PRIMARY KEY (s)`, "DROP TABLE b"}},
		{"raw string", `CREATE TABLE a (s STRING(9) DEFAULT (r'\;')) PRIMARY KEY (s); DROP TABLE b`, []string{`CREATE TABLE a (s STRING(9) DEFAULT (r'\;')) PRIMARY KEY (s)`, "DROP TABLE b"}},
		{"triple quote", "CREATE TABLE a (s STRING(9) DEFAULT ('''a;'b''')) PRIMARY KEY (s); DROP TABLE b", []string{"CREATE TABLE a (s STRING(9) DEFAULT ('''a;'b''')) PRIMARY KEY (s)", "DROP TABLE b"}},
		{"backtick", "CREATE TABLE `a;b` (id INT64) PRIMARY KEY (id); DROP TABLE `a;b`", []string{"CREATE TABLE `a;b` (id INT64) PRIMARY KEY (id)", "DROP TABLE `a;b`"}},
		{"line comment", "DROP TABLE a -- one; two\n; DROP TABLE b", []string{"DROP TABLE a -- one; two", "DROP TABLE b"}},
		{"hash comment", "DROP TABLE a # one; two\n; DROP TABLE b", []string{"DROP TABLE a # one; two", "DROP TABLE b"}},
		{"block comment", "DROP /* one; two */ TABLE a; DROP TABLE b", []string{"DROP /* one; two */ TABLE a", "DROP TABLE b"}},
		{"comment after the last", "DROP TABLE a; -- done; really", []string{"DROP TABLE a"}},
		{"comment before", "-- first\nDROP TABLE a; /* second */ DROP TABLE b", []string{"-- first\nDROP TABLE a", "/* second */ DROP TABLE b"}},
	} {
		got, err := splitStatements(tt.text)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: splitStatements(%q) = %q, want %q", tt.name, tt.text, got, tt.want)
		}
	}
	for _, text := range []string{"DROP 'a; DROP TABLE b", "DROP /* a; DROP TABLE b", "DROP `a; b", "SELECT '''a''"} {
		if _, err := splitStatements(text); !errors.Is(err, dbimp.ErrUnterminated) {
			t.Errorf("splitStatements(%q) = %v, want dbimp.ErrUnterminated", text, err)
		}
	}
}

// ddlFake returns a fake server for DDL that answers each update with one
// operation, which is done at its second read, and counts the reads.
func ddlFake(t *testing.T, polls *atomic.Int32) *fake {
	t.Helper()
	return newFake(t, func(c call) (int, string) {
		switch {
		case c.method == http.MethodPatch:
			return http.StatusOK, `{"name":"` + testDB + `/operations/op1"}`
		case c.method == http.MethodGet && strings.HasSuffix(c.path, "/operations/op1"):
			if polls.Add(1) < 2 {
				return http.StatusOK, `{"name":"op1"}`
			}
			return http.StatusOK, `{"name":"op1","done":true,"response":{}}`
		}
		return http.StatusNotFound, `{}`
	})
}

// TestDDLBatch holds D198: one Exec whose text holds several DDL statements
// sends one updateDatabaseDdl request with all of them, and polls the one
// operation. Quotes and comments hide a semicolon, and empty statements are
// dropped.
func TestDDLBatch(t *testing.T) {
	t.Parallel()
	var polls atomic.Int32
	f := ddlFake(t, &polls)
	const text = "CREATE TABLE a (id INT64, s STRING(9) DEFAULT (';')) PRIMARY KEY (id);\n" +
		";\n" +
		"-- a comment; with a semicolon\n" +
		"CREATE INDEX i ON a (s) /* ; */;\n" +
		"DROP TABLE b;\n"
	if _, err := f.db().ExecContext(t.Context(), text); err != nil {
		t.Fatal(err)
	}
	if n := f.count("ddl"); n != 1 {
		t.Fatalf("the driver sent %d DDL requests, want 1", n)
	}
	want := []string{
		"CREATE TABLE a (id INT64, s STRING(9) DEFAULT (';')) PRIMARY KEY (id)",
		"-- a comment; with a semicolon\nCREATE INDEX i ON a (s) /* ; */",
		"DROP TABLE b",
	}
	got, _ := f.last("ddl")["statements"].([]any)
	var stmts []string
	for _, s := range got {
		stmts = append(stmts, s.(string)) //nolint:forcetypeassert // The test body is JSON text of strings.
	}
	if !reflect.DeepEqual(stmts, want) {
		t.Errorf("the statements are %q, want %q", stmts, want)
	}
	if n := polls.Load(); n != 2 {
		t.Errorf("the driver read the operation %d times, want 2", n)
	}
	if got, want := f.verbs(), []string{"ddl", "op1", "op1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	// Query runs a batch too, and a prepared statement does.
	rows, err := f.db().QueryContext(t.Context(), "DROP TABLE a; DROP TABLE b")
	if err != nil {
		t.Fatalf("Query of a batch: %v", err)
	}
	defer rows.Close()
	if err := rows.Err(); err != nil {
		t.Errorf("the rows of a batch: %v", err)
	}
	if n := f.count("ddl"); n != 2 {
		t.Errorf("the driver sent %d DDL requests, want 2", n)
	}
}

// TestDDLBatchRefusals holds D198: a text that mixes DDL with another kind of
// statement is refused before any request, and several statements that are not DDL
// still go to the server, which refuses them (D191).
func TestDDLBatchRefusals(t *testing.T) {
	t.Parallel()
	var polls atomic.Int32
	f := ddlFake(t, &polls)
	db := f.db()
	for _, text := range []string{
		"CREATE TABLE a (id INT64) PRIMARY KEY (id); INSERT INTO a (id) VALUES (1)",
		"INSERT INTO a (id) VALUES (1); DROP TABLE a",
		"DROP TABLE a; SELECT 1;",
		"SELECT 1; CREATE TABLE a (id INT64) PRIMARY KEY (id)",
	} {
		_, err := db.ExecContext(t.Context(), text)
		if !errors.Is(err, dbimp.ErrNotSupported) || !strings.Contains(err.Error(), "DDL cannot run with other statements") {
			t.Errorf("%q gave %v, want dbimp.ErrNotSupported that says why", text, err)
		}
	}
	if n := f.count("ddl") + f.count("executeStreamingSql") + f.count("beginTransaction"); n != 0 {
		t.Errorf("the driver sent %d requests for texts that it refuses, want 0", n)
	}
	// Several statements that are not DDL go to the server as they did.
	f2 := newFake(t, txHandler)
	var a int64
	if err := f2.db().QueryRowContext(t.Context(), "SELECT 1; SELECT 2").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if got := f2.last("executeStreamingSql")["sql"]; got != "SELECT 1; SELECT 2" {
		t.Errorf("the statement sent is %v, want the text as it was", got)
	}
	// An argument is refused for a batch as for one statement.
	if _, err := db.ExecContext(t.Context(), "DROP TABLE a; DROP TABLE b", 1); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a DDL batch with an argument gave %v, want dbimp.ErrArguments", err)
	}
}

// TestDDLBatchError holds D198: the error of the operation is the error of the
// Exec, and a context that ends cancels the one operation.
func TestDDLBatchError(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(c call) (int, string) {
		if c.method == http.MethodPatch {
			return http.StatusOK, `{"name":"` + testDB + `/operations/op1","done":true,"error":{"code":5,"message":"Table not found: b"}}`
		}
		return http.StatusNotFound, `{}`
	})
	_, err := f.db().ExecContext(t.Context(), "DROP TABLE a; DROP TABLE b")
	if serr, ok := errors.AsType[*Error](err); !ok || serr.Code != 5 || !strings.Contains(serr.Message, "Table not found") {
		t.Errorf("the error is %v, want an *Error with the code 5", err)
	}

	f = newFake(t, func(c call) (int, string) {
		switch {
		case c.method == http.MethodPatch:
			return http.StatusOK, `{"name":"` + testDB + `/operations/slow"}`
		case c.method == http.MethodGet:
			return http.StatusOK, `{"name":"slow"}`
		case c.verb == "cancel":
			return http.StatusOK, `{}`
		}
		return http.StatusNotFound, `{}`
	})
	c := connector(config(), f.srv.URL, false)
	db := sql.OpenDB(c)
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waits := 0
	c.wait = func(ctx context.Context, d time.Duration) error {
		if waits++; waits == 2 {
			cancel()
		}
		return sleep(ctx, d)
	}
	_, err = db.ExecContext(ctx, "CREATE INDEX i ON t (a); CREATE INDEX j ON t (b)")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the error is %v, want context.Canceled", err)
	}
	if n, m := f.count("ddl"), f.count("cancel"); n != 1 || m != 1 {
		t.Errorf("the driver sent %d DDL requests and %d cancels, want 1 and 1", n, m)
	}
}

// TestDatabaseRole holds D198: a session carries the database role of the DSN as
// creatorRole, the option WithDatabaseRole makes a session of its own, and a
// session with no role has no such member.
func TestDatabaseRole(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	cfg := config()
	cfg.DatabaseRole = "reader"
	db := open(t, cfg, f.srv.URL, false)
	var a int64
	for range 2 {
		if err := db.QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a); err != nil {
			t.Fatal(err)
		}
	}
	bodies := f.bodies("sessions")
	if len(bodies) != 1 {
		t.Fatalf("the driver made %d sessions, want 1", len(bodies))
	}
	if got, want := bodies[0], map[string]any{"session": map[string]any{"multiplexed": true, "creatorRole": "reader"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("the session request is %v, want %v", got, want)
	}
	// One more session for the role of a statement, made once.
	for range 2 {
		if err := db.QueryRowContext(t.Context(), "SELECT a FROM t", WithDatabaseRole("writer")).Scan(&a); err != nil {
			t.Fatal(err)
		}
	}
	bodies = f.bodies("sessions")
	if len(bodies) != 2 {
		t.Fatalf("the driver made %d sessions, want 2", len(bodies))
	}
	if got := bodies[1]["session"].(map[string]any)["creatorRole"]; got != "writer" { //nolint:forcetypeassert // The test body is known.
		t.Errorf("the second session has the role %v, want writer", got)
	}
	// The role of the option also reaches a transaction, which keeps it.
	tx, err := db.BeginTx(WithOptions(t.Context(), WithDatabaseRole("writer")), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(t.Context(), "SELECT a FROM t", WithDatabaseRole("other")).Scan(&a); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("WithDatabaseRole in a transaction gave %v, want dbimp.ErrNotSupported", err)
	}
	if err := tx.QueryRowContext(t.Context(), "SELECT a FROM t", WithDatabaseRole("writer")).Scan(&a); err != nil {
		t.Errorf("the same role in a transaction gave %v", err)
	}
	if n := len(f.bodies("sessions")); n != 2 {
		t.Errorf("the driver made %d sessions, want 2", n)
	}

	// A connector with no role sends no member.
	f2 := newFake(t, txHandler)
	if err := open(t, config(), f2.srv.URL, false).QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if got, want := f2.bodies("sessions")[0], map[string]any{"session": map[string]any{"multiplexed": true}}; !reflect.DeepEqual(got, want) {
		t.Errorf("the session request is %v, want %v", got, want)
	}
	// A role that is not valid fails the statement before any request.
	n := f2.count("executeStreamingSql")
	for _, role := range []string{"a b", "a-b", `a"b`, strings.Repeat("r", 129)} {
		if err := open(t, config(), f2.srv.URL, true).QueryRowContext(t.Context(), "SELECT a FROM t", WithDatabaseRole(role)).Scan(&a); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("WithDatabaseRole(%q) gave %v, want dbimp.ErrInvalidValue", role, err)
		}
	}
	if f2.count("executeStreamingSql") != n {
		t.Error("the driver sent a statement for a role that it refuses")
	}
}
