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

// TestAutocommitDML holds D191 item 6: a DML statement outside a transaction
// runs in a transaction that the driver begins, and the driver commits it. The
// server refuses DML with no transaction (recorded: "a DML statement with no
// transaction").
func TestAutocommitDML(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	res, err := db.ExecContext(t.Context(), "INSERT INTO t (a) VALUES (1)")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 2 {
		t.Errorf("RowsAffected is %d, %v, want 2", n, err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want dbimp.ErrNotSupported", err)
	}
	want := []string{"beginTransaction", "executeStreamingSql", "commit"}
	if got := f.verbs(); !reflect.DeepEqual(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	if st := f.last("executeStreamingSql"); !reflect.DeepEqual(st["transaction"], map[string]any{"id": "dHgx"}) || st["seqno"] != "1" {
		t.Errorf("the statement is %v, want the transaction dHgx and the seqno 1", st)
	}
}

// TestAutocommitDMLFails holds that a DML statement that fails rolls its
// transaction back and returns the error of the server, and that a query
// outside a transaction begins none.
func TestAutocommitDMLFails(t *testing.T) {
	t.Parallel()
	dup := file(t, 113).Response
	f := newFake(t, func(c call) (int, string) {
		if c.verb == "executeStreamingSql" && strings.Contains(string(c.body), "INSERT") {
			return dup.Status, dup.Body
		}
		return txHandler(c)
	})
	db := f.db()
	_, err := db.ExecContext(t.Context(), "INSERT INTO t (a) VALUES (1)")
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.Status != "ALREADY_EXISTS" || serr.HTTPStatus != http.StatusConflict {
		t.Fatalf("the error is %v, want an *Error ALREADY_EXISTS with HTTP 409", err)
	}
	if got, want := f.verbs(), []string{"beginTransaction", "executeStreamingSql", "rollback"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	var a int64
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if f.count("beginTransaction") != 1 {
		t.Errorf("a query began a transaction: %q", f.verbs())
	}
	if st := f.last("executeStreamingSql"); st["transaction"] != nil || st["seqno"] != nil {
		t.Errorf("a query outside a transaction sent %v, want no transaction and no seqno", st)
	}
}

// TestThenReturn holds that INSERT with THEN RETURN, which gives the key that
// LastInsertId cannot, returns its rows, and commits when the caller reads them
// to the end, and when the caller closes the rows early, as QueryRow does.
func TestThenReturn(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(c call) (int, string) {
		if c.verb == "executeStreamingSql" {
			return http.StatusOK, streamOf(intField("id"), `"101","102"`)
		}
		return txHandler(c)
	})
	db := f.db()
	rows, err := db.QueryContext(t.Context(), "INSERT INTO t (a) VALUES (1), (2) THEN RETURN id")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int64{101, 102}) {
		t.Errorf("the ids are %v, want 101 and 102", ids)
	}
	//nolint:sqlclosecheck // The test closes the rows at a chosen point, and then counts the requests.
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := f.verbs(), []string{"beginTransaction", "executeStreamingSql", "commit"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	var id int64
	if err := db.QueryRowContext(t.Context(), "INSERT INTO t (a) VALUES (3) THEN RETURN id").Scan(&id); err != nil || id != 101 {
		t.Fatalf("QueryRow gave %d, %v, want 101", id, err)
	}
	if n := f.count("commit"); n != 2 {
		t.Errorf("the driver sent commit %d times, want 2, because the insert of QueryRow must stay", n)
	}
	if n := f.count("rollback"); n != 0 {
		t.Errorf("the driver sent rollback %d times, want 0", n)
	}
}

// TestThenReturnCommitFails holds that a commit that fails is the error of the
// rows, after the last row (D21).
func TestThenReturnCommitFails(t *testing.T) {
	t.Parallel()
	aborted := file(t, 324).Response
	f := newFake(t, func(c call) (int, string) {
		switch c.verb {
		case "executeStreamingSql":
			return http.StatusOK, streamOf(intField("id"), `"101"`)
		case "commit":
			return aborted.Status, aborted.Body
		}
		return txHandler(c)
	})
	rows, err := f.db().QueryContext(t.Context(), "INSERT INTO t (a) VALUES (1) THEN RETURN id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if n != 1 || !errors.Is(rows.Err(), ErrAborted) {
		t.Errorf("read %d rows and the error %v, want 1 row and an error that wraps ErrAborted", n, rows.Err())
	}
}

// TestStatementKinds holds how the driver sends each kind of statement, by its
// first word, after white space, comments and a hint.
func TestStatementKinds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		want  kind
	}{
		{"SELECT 1", kindQuery},
		{"  select 1", kindQuery},
		{"WITH a AS (SELECT 1) SELECT * FROM a", kindQuery},
		{"(SELECT 1)", kindQuery},
		{"", kindQuery},
		{"INSERT INTO t VALUES (1)", kindDML},
		{"update t SET a = 1", kindDML},
		{"DELETE FROM t WHERE TRUE", kindDML},
		{"MERGE INTO t USING s ON TRUE", kindDML},
		{"-- a comment\nINSERT INTO t VALUES (1)", kindDML},
		{"# a comment\nINSERT INTO t VALUES (1)", kindDML},
		{"/* a comment */ UPDATE t SET a = 1", kindDML},
		{"@{USE_ADDITIONAL_PARALLELISM=TRUE} SELECT 1", kindQuery},
		{"@{PDML_MAX_PARALLELISM=2}\n UPDATE t SET a = 1", kindDML},
		{"CREATE TABLE t (id INT64) PRIMARY KEY (id)", kindDDL},
		{"alter table t add column x INT64", kindDDL},
		{"DROP INDEX i", kindDDL},
		{"RENAME TABLE a TO b", kindDDL},
		{"GRANT SELECT ON TABLE t TO ROLE r", kindDDL},
		{"REVOKE SELECT ON TABLE t FROM ROLE r", kindDDL},
		{"ANALYZE", kindDDL},
		{"-- unfinished", kindQuery},
		{"/* unfinished", kindQuery},
	} {
		if got := classify(tt.query); got != tt.want {
			t.Errorf("classify(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}

// TestOptionsOfOneStatement holds D109: an option comes from the context, then
// from an argument, and a later one wins, and the options that Spanner cannot
// honor fail the statement with dbimp.ErrNotSupported before any request.
func TestOptionsOfOneStatement(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	ctx := WithOptions(t.Context(), WithParameter("queryMode", "PLAN"))
	var a int64
	if err := db.QueryRowContext(ctx, "SELECT a FROM t").Scan(&a); err != nil {
		t.Fatal(err)
	}
	if got := f.last("executeStreamingSql")["queryMode"]; got != "PLAN" {
		t.Errorf("the context sent queryMode %v, want PLAN", got)
	}
	if err := db.QueryRowContext(ctx, "SELECT a FROM t", WithParameter("queryMode", "PROFILE")).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if got := f.last("executeStreamingSql")["queryMode"]; got != "PROFILE" {
		t.Errorf("the argument sent queryMode %v, want PROFILE", got)
	}
	// A parameter replaces a member that the driver sets itself.
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM t", WithParameter("sql", "SELECT 2")).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if got := f.last("executeStreamingSql")["sql"]; got != "SELECT 2" {
		t.Errorf("WithParameter(sql) sent %v, want SELECT 2", got)
	}
	n := f.count("executeStreamingSql")
	for name, opt := range map[string]Option{
		"WithTimeout":   WithTimeout(time.Second),
		"WithTimeout<0": WithTimeout(-time.Second),
		"WithParameter": WithParameter("x", nil),
		"WithDatabase":  WithDatabase("a/b"),
		"empty name":    WithParameter("", 1),
	} {
		_, err := db.ExecContext(t.Context(), "SELECT 1", opt)
		want := dbimp.ErrNotSupported
		if name != "WithTimeout" {
			want = dbimp.ErrInvalidValue
		}
		if !errors.Is(err, want) {
			t.Errorf("%s gave %v, want %v", name, err, want)
		}
	}
	if f.count("executeStreamingSql") != n {
		t.Error("the driver sent a statement for an option that it refuses")
	}
	// Options that ask for nothing do not fail.
	if _, err := db.ExecContext(t.Context(), "SELECT 1", WithTimeout(0), WithReadonly(false)); err != nil {
		t.Errorf("WithTimeout(0) and WithReadonly(false) gave %v", err)
	}
}

// TestReadonlyOption holds that WithReadonly runs the statement in a read-only
// transaction of one use, so a DML statement needs no transaction of its own and
// the server refuses it, and that a DDL statement fails with
// dbimp.ErrNotSupported.
func TestReadonlyOption(t *testing.T) {
	t.Parallel()
	refused := file(t, 111).Response
	f := newFake(t, func(c call) (int, string) {
		if c.verb == "executeStreamingSql" && strings.Contains(string(c.body), "INSERT") {
			return refused.Status, refused.Body
		}
		return txHandler(c)
	})
	db := f.db()
	var a int64
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM t", WithReadonly(true)).Scan(&a); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"singleUse": map[string]any{"readOnly": map[string]any{"strong": true}}}
	if got := f.last("executeStreamingSql")["transaction"]; !reflect.DeepEqual(got, want) {
		t.Errorf("the transaction is %v, want %v", got, want)
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO t (a) VALUES (1)", WithReadonly(true)); err == nil {
		t.Error("an INSERT with WithReadonly gave no error")
	}
	if f.count("beginTransaction") != 0 {
		t.Error("an INSERT with WithReadonly began a transaction")
	}
	if _, err := db.ExecContext(t.Context(), "DROP TABLE t", WithReadonly(true)); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a DDL statement with WithReadonly gave %v, want dbimp.ErrNotSupported", err)
	}
	ctx := WithOptions(t.Context(), WithReadonly(true))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if got := f.last("beginTransaction")["options"]; !reflect.DeepEqual(got, map[string]any{"readOnly": map[string]any{"strong": true}}) {
		t.Errorf("WithReadonly in the context sent the options %v, want readOnly", got)
	}
}

// TestWithDatabase holds that WithDatabase sends the statement to a session of
// the other database, which the driver makes once, and that a transaction runs
// in one database only.
func TestWithDatabase(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	var a int64
	for range 2 {
		if err := db.QueryRowContext(t.Context(), "SELECT a FROM t", WithDatabase("other")).Scan(&a); err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	var paths []string
	for _, c := range f.calls {
		_, tail, _ := strings.Cut(c.path, "/databases/")
		paths = append(paths, c.method+" /databases/"+tail)
	}
	f.mu.Unlock()
	want := []string{
		"GET /databases/dbimp_test",
		"POST /databases/dbimp_test/sessions",
		"GET /databases/other",
		"POST /databases/other/sessions",
		"POST /databases/other/sessions/test-session:executeStreamingSql",
		"POST /databases/other/sessions/test-session:executeStreamingSql",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("the requests are %q, want %q", paths, want)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(t.Context(), "SELECT a FROM t", WithDatabase("other")).Scan(&a); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("WithDatabase in a transaction gave %v, want dbimp.ErrNotSupported", err)
	}
}

// TestDialectRefused holds D191 item 10: a database of the PostgreSQL dialect is
// refused when the driver connects, before it makes a session.
func TestDialectRefused(t *testing.T) {
	t.Parallel()
	f := newFake(t, nil)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, call{method: r.Method, path: r.URL.Path, verb: r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]})
		f.mu.Unlock()
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"databaseDialect":"POSTGRESQL"}`))
		}
	})
	err := f.db().PingContext(t.Context())
	if !errors.Is(err, dbimp.ErrNotSupported) || !strings.Contains(err.Error(), "POSTGRESQL") {
		t.Errorf("the error is %v, want dbimp.ErrNotSupported that names POSTGRESQL", err)
	}
	if f.count("sessions") != 0 {
		t.Error("the driver made a session in a database of the PostgreSQL dialect")
	}
}

// TestSeveralStatementsGoToTheServer holds D191 item 11: the driver sends several
// statements in one request as they are, and the server refuses them (recorded:
// "two statements in one request"), so the driver adds no splitting.
func TestSeveralStatementsGoToTheServer(t *testing.T) {
	t.Parallel()
	refused := file(t, 150).Response
	f := newFake(t, func(c call) (int, string) {
		return refused.Status, refused.Body
	})
	db := f.db()
	err := failure(t, db, "SELECT 1; SELECT 2")
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.HTTPStatus != http.StatusNotImplemented {
		t.Fatalf("the error is %v, want an *Error with HTTP 501", err)
	}
	if got := f.last("executeStreamingSql")["sql"]; got != "SELECT 1; SELECT 2" {
		t.Errorf("the driver sent %v, want the text as it is", got)
	}
}

// TestDDLRequests holds D191 item 8: a DDL statement goes to updateDatabaseDdl
// with its own operation id, the driver polls the operation until it is done,
// and the trailing semicolon is cut.
func TestDDLRequests(t *testing.T) {
	t.Parallel()
	var polls atomic.Int32
	f := newFake(t, func(c call) (int, string) {
		switch {
		case c.method == http.MethodPatch:
			return http.StatusOK, `{"name":"` + testDB + `/operations/op1"}`
		case c.method == http.MethodGet && strings.HasSuffix(c.path, "/operations/op1"):
			if polls.Add(1) < 3 {
				return http.StatusOK, `{"name":"op1"}`
			}
			return http.StatusOK, `{"name":"op1","done":true,"response":{}}`
		}
		return http.StatusNotFound, `{}`
	})
	db := f.db()
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE x (id INT64) PRIMARY KEY (id);\n"); err != nil {
		t.Fatal(err)
	}
	body := f.last("ddl")
	if st, _ := body["statements"].([]any); len(st) != 1 || st[0] != "CREATE TABLE x (id INT64) PRIMARY KEY (id)" {
		t.Errorf("the statements are %v, want the text with no semicolon", body["statements"])
	}
	id, _ := body["operationId"].(string)
	if !strings.HasPrefix(id, "dbimp_") || len(id) < 10 {
		t.Errorf("the operation id is %q, want one that starts with dbimp_", id)
	}
	if n := polls.Load(); n != 3 {
		t.Errorf("the driver read the operation %d times, want 3", n)
	}
	// Query runs a DDL statement too, and returns no rows.
	polls.Store(3)
	rows, err := db.QueryContext(t.Context(), "DROP TABLE x")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	}()
	if cols, _ := rows.Columns(); len(cols) != 0 || rows.Next() {
		t.Errorf("a DDL statement has the columns %v and a row", cols)
	}
	rows.Close()
	if _, err := db.ExecContext(t.Context(), "DROP TABLE x", 1); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a DDL statement with an argument gave %v, want dbimp.ErrArguments", err)
	}
}

// TestDDLReplay holds D191 item 8 with the recorded operations: the operation of
// a column is done at its first read, the operation of a search index is not done
// at its first read and done at its second, an error of the operation is the error
// of the statement, and an error of the call is returned at once.
func TestDDLReplay(t *testing.T) {
	t.Parallel()
	db, s, _ := replayDB(t)
	for _, q := range []string{
		"ALTER TABLE dbimp_t_types ADD COLUMN u UUID",
		"DROP VIEW IF EXISTS dbimp_t_nosuch_v",
	} {
		if _, err := db.ExecContext(t.Context(), q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
	s.mu.Lock()
	s.log = nil
	s.mu.Unlock()
	if _, err := db.ExecContext(t.Context(), "CREATE SEARCH INDEX dbimp_t_si ON dbimp_t_doc (body_tokens)"); err != nil {
		t.Fatal(err)
	}
	if got, want := s.requests(), []string{"PATCH ddl", "GET " + file(t, 191).Request.Path[strings.LastIndex(file(t, 191).Request.Path, "/")+1:], "GET " + file(t, 191).Request.Path[strings.LastIndex(file(t, 191).Request.Path, "/")+1:]}; !reflect.DeepEqual(got, want) {
		t.Errorf("the requests of the search index are %q, want %q", got, want)
	}

	_, err := db.ExecContext(t.Context(), "DROP TABLE dbimp_t_nosuch")
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.Code != 5 || serr.Status != "NOT_FOUND" || !strings.Contains(serr.Message, "Table not found") {
		t.Errorf("the error of the operation is %v, want an *Error with the code 5 and NOT_FOUND", err)
	}
	_, err = db.ExecContext(t.Context(), "CREATE TABL x (id INT64) PRIMARY KEY (id)")
	serr, ok = errors.AsType[*Error](err)
	if !ok || serr.HTTPStatus != http.StatusBadRequest || serr.Status != "INVALID_ARGUMENT" || !strings.Contains(serr.Message, "Syntax error") {
		t.Errorf("the error of the call is %v, want an *Error with HTTP 400", err)
	}
}

// TestDDLCancel holds D191 item 4: when the context ends while the driver waits
// for an operation, it calls operations:cancel on the operation, and the error
// wraps the error of the context (recorded: "cancel the running operation").
func TestDDLCancel(t *testing.T) {
	t.Parallel()
	cancelled := file(t, 202).Response
	f := newFake(t, func(c call) (int, string) {
		switch {
		case c.method == http.MethodPatch:
			return http.StatusOK, `{"name":"` + testDB + `/operations/slow"}`
		case c.method == http.MethodGet:
			return http.StatusOK, `{"name":"slow"}`
		case c.verb == "cancel":
			return cancelled.Status, cancelled.Body
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
		if waits++; waits == 3 {
			cancel()
		}
		return sleep(ctx, d)
	}
	_, err := db.ExecContext(ctx, "CREATE INDEX i ON t (a)")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the error is %v, want context.Canceled", err)
	}
	if n := f.count("cancel"); n != 1 {
		t.Fatalf("the driver sent the cancel %d times, want 1", n)
	}
	if got := f.calls[len(f.calls)-1].path; got != "/v1/"+testDB+"/operations/slow:cancel" {
		t.Errorf("the cancel went to %s, want the operation that the server named", got)
	}
}

// TestDDLCancelBeforeTheAnswer holds that the driver cancels an operation that
// it named, when the context ends before the answer of the first request came,
// so the statement does not run on the server unseen.
func TestDDLCancelBeforeTheAnswer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := newFake(t, func(c call) (int, string) {
		switch {
		case c.method == http.MethodPatch:
			cancel()
			<-c.ctx.Done()
			return http.StatusOK, `{}`
		case c.verb == "cancel":
			return http.StatusOK, `{}`
		}
		return http.StatusNotFound, `{}`
	})
	_, err := f.db().ExecContext(ctx, "CREATE INDEX i ON t (a)")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the error is %v, want context.Canceled", err)
	}
	id, _ := f.last("ddl")["operationId"].(string)
	if got := f.calls[len(f.calls)-1].path; got != "/v1/"+testDB+"/operations/"+id+":cancel" {
		t.Errorf("the cancel went to %s, want the operation with the id %s", got, id)
	}
}
