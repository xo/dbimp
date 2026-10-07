package trino_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/trino"
)

// exec runs a statement and reads its result to the end.
func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
}

// post returns the requests that are a POST.
func post(reqs []request) []request {
	var out []request
	for _, r := range reqs {
		if r.method == http.MethodPost {
			out = append(out, r)
		}
	}
	return out
}

// TestStatementHeaders holds the headers that the driver sends, for each flavor
// (D175): the user, the source, the catalog, the schema, the time zone, the
// properties of the session, the capabilities for Trino and none for Presto,
// and the secret.
func TestStatementHeaders(t *testing.T) {
	t.Parallel()
	const keys = "source=tester&timezone=Asia%2FJakarta&session.query_max_run_time=5m&timeout=1500ms"
	for _, tt := range []struct {
		flavor string
		prefix string
		other  string
		caps   []string
	}{
		{trino.FlavorTrino, "X-Trino-", "X-Presto-", []string{"PARAMETRIC_DATETIME,NUMBER,VARIANT"}},
		{trino.FlavorPresto, "X-Presto-", "X-Trino-", nil},
	} {
		t.Run(tt.flavor, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, one)
			db := f.open(t, "trino://alice:s3cret@"+f.host()+"/memory/default?flavor="+tt.flavor+"&"+keys)
			exec(t, db, "SELECT 1")
			reqs := f.requests()
			if len(reqs) != 1 || reqs[0].method != http.MethodPost || reqs[0].uri != "/v1/statement" || reqs[0].body != "SELECT 1" {
				t.Fatalf("the requests are %+v, want one POST of the statement", reqs)
			}
			h := reqs[0].header
			for name, want := range map[string][]string{
				"User":                {"alice"},
				"Source":              {"tester"},
				"Catalog":             {"memory"},
				"Schema":              {"default"},
				"Time-Zone":           {"Asia/Jakarta"},
				"Session":             {"query_max_execution_time=1500ms", "query_max_run_time=5m"},
				"Client-Capabilities": tt.caps,
			} {
				if got := h.Values(tt.prefix + name); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
					t.Errorf("%s%s is %q, want %q", tt.prefix, name, got, want)
				}
				if got := h.Values(tt.other + name); len(got) != 0 {
					t.Errorf("%s%s is %q, want none, because the other flavor ignores it", tt.other, name, got)
				}
			}
			if user, pass, ok := parseBasic(h); !ok || user != "alice" || pass != "s3cret" {
				t.Errorf("the credentials are %q, %q, %v, want alice and the secret", user, pass, ok)
			}
			if ct := h.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
				t.Errorf("the content type is %q, want text/plain", ct)
			}
		})
	}
}

// parseBasic returns the user and the password of the basic authentication of
// a request.
func parseBasic(h http.Header) (string, string, bool) {
	r := &http.Request{Header: h}
	return r.BasicAuth()
}

// TestOptionsOfOneStatement holds D109: an option comes from the DSN, then the
// context, then an argument, and a later one wins. A common option that the
// server cannot honor fails with dbimp.ErrNotSupported.
func TestOptionsOfOneStatement(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	db := f.open(t, f.dsn("flavor=trino&timeout=2s&source=dsn"))
	ctx := trino.WithOptions(t.Context(), trino.WithSource("context"), trino.WithTimeZone("UTC"))
	if _, err := db.ExecContext(ctx, "SELECT 1", trino.WithTimeout(1500*time.Millisecond), trino.WithDatabase("tpch"), trino.WithSchema("tiny"),
		trino.WithParameter("query_max_run_time", "1m"), trino.WithParameter("a.b", 7), trino.WithSource("argument")); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1, and the options are not arguments", len(reqs))
	}
	h := reqs[0].header
	for name, want := range map[string]string{"X-Trino-Source": "argument", "X-Trino-Time-Zone": "UTC", "X-Trino-Catalog": "tpch", "X-Trino-Schema": "tiny"} {
		if got := h.Get(name); got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}
	if got, want := h.Values("X-Trino-Session"), []string{"a.b=7", "query_max_execution_time=1500ms", "query_max_run_time=1m"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the session is %q, want %q", got, want)
	}
	for _, tt := range []struct {
		name string
		opt  trino.Option
		want error
	}{
		{"read only", trino.WithReadonly(true), dbimp.ErrNotSupported},
		{"a negative timeout", trino.WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"an empty source", trino.WithSource(""), dbimp.ErrInvalidValue},
		{"a property that is no name", trino.WithParameter("a=b", "c"), dbimp.ErrInvalidValue},
		{"a property with no value", trino.WithParameter("a", nil), dbimp.ErrInvalidValue},
	} {
		before := len(f.requests())
		_, err := db.ExecContext(t.Context(), "SELECT 1", tt.opt)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
		if got := len(f.requests()); got != before {
			t.Errorf("%s: the driver sent a request, and it must send none", tt.name)
		}
	}
	// Read only false and a timeout of zero ask for nothing, so they never fail.
	if _, err := db.ExecContext(t.Context(), "SELECT 1", trino.WithReadonly(false), trino.WithTimeout(0)); err != nil {
		t.Errorf("options that ask for nothing failed: %v", err)
	}
	reqs = f.requests()
	if got := reqs[len(reqs)-1].header.Values("X-Trino-Session"); len(got) != 0 {
		t.Errorf("a timeout of zero sent the session %q, want none", got)
	}
}

// TestParameters holds D175: the driver names a prepared statement in the
// header, and sends EXECUTE name USING with each argument as a literal.
func TestParameters(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"X-Trino-", "X-Presto-"} {
		flavor := trino.FlavorTrino
		if prefix == "X-Presto-" {
			flavor = trino.FlavorPresto
		}
		t.Run(flavor, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, one)
			db := f.open(t, f.dsn("flavor="+flavor))
			exec(t, db, "SELECT ? + ? FROM t WHERE s = ?", 5, int64(-7), "it's")
			reqs := f.requests()
			if len(reqs) != 1 {
				t.Fatalf("got %d requests, want 1", len(reqs))
			}
			values := reqs[0].header.Values(prefix + "Prepared-Statement")
			if len(values) != 1 {
				t.Fatalf("the prepared statements are %q, want one", values)
			}
			name, text, _ := strings.Cut(values[0], "=")
			if text != "SELECT%20%3F%20%2B%20%3F%20FROM%20t%20WHERE%20s%20%3D%20%3F" {
				t.Errorf("the text is %q, want the statement escaped as a URL, with %%20 for a space", text)
			}
			if want := "EXECUTE " + name + " USING 5, -7, 'it''s'"; reqs[0].body != want {
				t.Errorf("the body is %q, want %q", reqs[0].body, want)
			}
			if _, err := db.ExecContext(t.Context(), "SELECT :a", sql.Named("a", 1)); !errors.Is(err, dbimp.ErrArguments) {
				t.Errorf("a named argument gave %v, want dbimp.ErrArguments", err)
			}
			if _, err := db.ExecContext(t.Context(), "SELECT ?", struct{ A int }{1}); err == nil {
				t.Errorf("a struct is no argument, and the driver gave no error")
			}
		})
	}
}

// TestSessionState holds the state that a connection keeps for the servers
// (measured): USE, SET SESSION, RESET SESSION, PREPARE and DEALLOCATE answer
// in headers, and the connection sends the state back. ResetSession gives the
// next caller the state of the DSN.
func TestSessionState(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request, body string, _ int) {
		var headers []string
		switch body {
		case "USE tpch.tiny":
			headers = []string{"X-Trino-Set-Catalog", "tpch", "X-Trino-Set-Schema", "tiny"}
		case "SET SESSION query_max_run_time = '5m'":
			headers = []string{"X-Trino-Set-Session", "query_max_run_time=5m"}
		case "SET TIME ZONE 'Asia/Jakarta'":
			headers = []string{"X-Trino-Set-Session", "time_zone_id=Asia%2FJakarta"}
		case "RESET SESSION query_max_run_time":
			headers = []string{"X-Trino-Clear-Session", "query_max_run_time"}
		case "PREPARE s2 FROM SELECT (? + 2)":
			headers = []string{"X-Trino-Added-Prepare", "s2=SELECT+%28%3F+%2B+2%29"}
		case "DEALLOCATE PREPARE s2":
			headers = []string{"X-Trino-Deallocated-Prepare", "s2"}
		}
		writePage(w, page("", bigint("a"), "[[1]]"), headers...)
	})
	db := f.open(t, f.dsn("flavor=trino&session.query_max_rows=10"))
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	last := func() http.Header {
		reqs := post(f.requests())
		return reqs[len(reqs)-1].header
	}
	run := func(query string) http.Header {
		t.Helper()
		if _, err := conn.ExecContext(t.Context(), query); err != nil {
			t.Fatalf("running %q: %v", query, err)
		}
		return last()
	}
	run("SELECT 1")
	if h := last(); h.Get("X-Trino-Catalog") != "" || !reflect.DeepEqual(h.Values("X-Trino-Session"), []string{"query_max_rows=10"}) {
		t.Errorf("the first statement has the headers %v, want the session of the DSN only", h)
	}
	run("USE tpch.tiny")
	h := run("SELECT 2")
	if h.Get("X-Trino-Catalog") != "tpch" || h.Get("X-Trino-Schema") != "tiny" {
		t.Errorf("after USE the catalog and the schema are %q and %q, want tpch and tiny", h.Get("X-Trino-Catalog"), h.Get("X-Trino-Schema"))
	}
	run("SET SESSION query_max_run_time = '5m'")
	run("SET TIME ZONE 'Asia/Jakarta'")
	h = run("SELECT 3")
	if got, want := h.Values("X-Trino-Session"), []string{"query_max_rows=10", "query_max_run_time=5m", "time_zone_id=Asia%2FJakarta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the session is %q, want %q", got, want)
	}
	run("RESET SESSION query_max_run_time")
	run("PREPARE s2 FROM SELECT (? + 2)")
	h = run("SELECT 4")
	if got, want := h.Values("X-Trino-Session"), []string{"query_max_rows=10", "time_zone_id=Asia%2FJakarta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after RESET SESSION the session is %q, want %q", got, want)
	}
	if got, want := h.Values("X-Trino-Prepared-Statement"), []string{"s2=SELECT+%28%3F+%2B+2%29"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the prepared statements are %q, want %q", got, want)
	}
	run("DEALLOCATE PREPARE s2")
	if h := run("SELECT 5"); len(h.Values("X-Trino-Prepared-Statement")) != 0 {
		t.Errorf("after DEALLOCATE the prepared statements are %q, want none", h.Values("X-Trino-Prepared-Statement"))
	}
	// The connection goes back to the pool, and the next caller gets the state
	// of the DSN.
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err = db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	h = run("SELECT 6")
	if h.Get("X-Trino-Catalog") != "" || h.Get("X-Trino-Schema") != "" || !reflect.DeepEqual(h.Values("X-Trino-Session"), []string{"query_max_rows=10"}) {
		t.Errorf("a connection that went back to the pool has the headers %v, want the state of the DSN (SessionResetter)", h)
	}
}

// transactions is the handler of a server that holds transactions: START
// TRANSACTION answers the id in the header of its second page, as the servers
// do (measured), and COMMIT and ROLLBACK clear it.
func transactions(w http.ResponseWriter, r *http.Request, body string, _ int) {
	switch {
	case body == "START TRANSACTION" || strings.HasPrefix(body, "START TRANSACTION "):
		if r.Header.Get("X-Trino-Transaction-Id") != "NONE" {
			http.Error(w, "Client does not support transactions", http.StatusOK)
			return
		}
		writePage(w, page("http://"+r.Host+"/v1/statement/executing/q1/t/0", "", ""))
	case r.Method == http.MethodGet:
		writePage(w, page("", "", ""), "X-Trino-Started-Transaction-Id", "tx-1")
	case body == "COMMIT" || body == "ROLLBACK":
		writePage(w, page("", "", ""), "X-Trino-Clear-Transaction-Id", "true")
	default:
		writePage(w, page("", bigint("a"), "[[1]]"))
	}
}

// TestTransactions holds D175: BeginTx sends START TRANSACTION with the header
// NONE, keeps the id, sends it with each statement, and COMMIT and ROLLBACK
// end it.
func TestTransactions(t *testing.T) {
	t.Parallel()
	f := newFake(t, transactions)
	db := f.open(t, f.dsn("flavor=trino"))
	tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	exec(t, db, "SELECT 2")
	var bodies, ids []string
	for _, r := range post(f.requests()) {
		bodies = append(bodies, r.body)
		ids = append(ids, r.header.Get("X-Trino-Transaction-Id"))
	}
	if want := []string{"START TRANSACTION ISOLATION LEVEL SERIALIZABLE, READ ONLY", "SELECT 1", "COMMIT", "SELECT 2"}; !reflect.DeepEqual(bodies, want) {
		t.Errorf("the statements are %q, want %q", bodies, want)
	}
	if want := []string{"NONE", "tx-1", "tx-1", ""}; !reflect.DeepEqual(ids, want) {
		t.Errorf("the transaction ids are %q, want %q", ids, want)
	}
	tx, err = db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	reqs := post(f.requests())
	if got := reqs[len(reqs)-1].body; got != "ROLLBACK" {
		t.Errorf("the last statement is %q, want ROLLBACK", got)
	}
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelSnapshot}); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("an isolation level that the servers lack gave %v, want dbimp.ErrNotSupported", err)
	}
}

// TestTransactionRollbackAfterTheContextEnded holds that database/sql rolls a
// transaction back after its context ended, and that the driver sends the
// ROLLBACK then, with the context without its end.
func TestTransactionRollbackAfterTheContextEnded(t *testing.T) {
	t.Parallel()
	f := newFake(t, transactions)
	db := f.open(t, f.dsn("flavor=trino"))
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := db.BeginTx(ctx, nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitFor(t, "the ROLLBACK after the context ended", func() bool {
		for _, r := range post(f.requests()) {
			if r.body == "ROLLBACK" {
				return r.header.Get("X-Trino-Transaction-Id") == "tx-1"
			}
		}
		return false
	})
}

// TestNoTransactionId holds that a server that starts no transaction is an
// error, and never a transaction that is not one (D20).
func TestNoTransactionId(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	db := f.open(t, f.dsn("flavor=trino"))
	tx, err := db.BeginTx(t.Context(), nil)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("BeginTx returned a transaction, and the server named no id")
	}
	if !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("the error is %v, want dbimp.ErrInvalidValue", err)
	}
}

// waitFor polls until ok, for two seconds at most.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("waiting for %s: it did not happen in 2 seconds", what)
}

// polling is the handler of a server that answers the POST with a page that has
// a nextUri on another host, then pages of rows, each with a nextUri, and a last
// page. The nextUri names a host that the client must not use (D175).
func polling(elsewhere string, pages int) func(http.ResponseWriter, *http.Request, string, int) {
	return func(w http.ResponseWriter, r *http.Request, _ string, _ int) {
		switch r.Method {
		case http.MethodPost:
			writePage(w, page(elsewhere+"/v1/statement/executing/q1/t0/0", "", ""))
		case http.MethodGet:
			n := pageNumber(r.URL.Path)
			next := ""
			if n < pages {
				next = elsewhere + "/v1/statement/executing/q1/t" + strconv.Itoa(n+1) + "/" + strconv.Itoa(n+1)
			}
			writePage(w, page(next, bigint("a"), "[["+strconv.Itoa(n)+"],["+strconv.Itoa(n*10)+"]]"))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// TestPollsGoToTheHostOfTheDSN holds D175: the driver uses the path and the
// query of each nextUri with the host of the DSN, and sends the secret to that
// host only.
func TestPollsGoToTheHostOfTheDSN(t *testing.T) {
	t.Parallel()
	other := newFake(t, func(http.ResponseWriter, *http.Request, string, int) {})
	f := newFake(t, polling(other.srv.URL, 3))
	db := f.open(t, "trino://trino:s3cret@"+f.host()+"?flavor=trino")
	rows, err := db.QueryContext(t.Context(), "SELECT a")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []int64
	for rows.Next() {
		var a int64
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		got = append(got, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []int64{0, 0, 1, 10, 2, 20, 3, 30}; !reflect.DeepEqual(got, want) {
		t.Errorf("the rows are %v, want %v", got, want)
	}
	if n := len(other.requests()); n != 0 {
		t.Errorf("the host that the server named got %d requests, want none", n)
	}
	for _, r := range f.requests() {
		if user, pass, ok := parseBasic(r.header); !ok || user != "trino" || pass != "s3cret" {
			t.Errorf("%s %s has the credentials %q, %q, %v, want trino and the secret", r.method, r.uri, user, pass, ok)
		}
		if r.header.Get("X-Trino-User") != "trino" {
			t.Errorf("%s %s has no user header", r.method, r.uri)
		}
	}
	if n := len(f.requests()); n != 5 {
		t.Errorf("got %d requests, want a POST and four polls", n)
	}
}

// TestCancel holds D175: the servers never stop a query that nobody polls, so
// the driver sends DELETE to the nextUri when the caller closes the rows
// before the end, and when the context ends, and never when the result was read
// to its end or failed.
func TestCancel(t *testing.T) {
	t.Parallel()
	deletes := func(f *fake) []string {
		var out []string
		for _, r := range f.requests() {
			if r.method == http.MethodDelete {
				out = append(out, r.uri)
			}
		}
		return out
	}
	t.Run("rows closed early", func(t *testing.T) {
		t.Parallel()
		f := newFake(t, polling("http://elsewhere.invalid", 5))
		db := f.open(t, f.dsn("flavor=trino"))
		rows, err := db.QueryContext(t.Context(), "SELECT a")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if got, want := deletes(f), []string{"/v1/statement/executing/q1/t1/1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("the cancels are %q, want %q, the nextUri of the page that was read", got, want)
		}
	})
	t.Run("rows read to the end", func(t *testing.T) {
		t.Parallel()
		f := newFake(t, polling("http://elsewhere.invalid", 2))
		db := f.open(t, f.dsn("flavor=trino"))
		exec(t, db, "SELECT a")
		if got := deletes(f); len(got) != 0 {
			t.Errorf("the cancels are %q, want none for a result that was read", got)
		}
	})
	t.Run("an error", func(t *testing.T) {
		t.Parallel()
		f := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ string, _ int) {
			writePage(w, `{"id":"q1","error":{"message":"boom","errorCode":1,"errorName":"SYNTAX_ERROR","errorType":"USER_ERROR"},"stats":{"state":"FAILED"}}`)
		})
		db := f.open(t, f.dsn("flavor=trino"))
		_, err := db.ExecContext(t.Context(), "SELEC 1")
		if perr, ok := errors.AsType[*trino.Error](err); !ok || perr.Name != "SYNTAX_ERROR" || perr.Message != "boom" {
			t.Errorf("the error is %v, want the error of the page", err)
		}
		if got := deletes(f); len(got) != 0 {
			t.Errorf("the cancels are %q, want none for a query that failed", got)
		}
	})
	t.Run("the context ended", func(t *testing.T) {
		t.Parallel()
		f := newFake(t, polling("http://elsewhere.invalid", 5))
		db := f.open(t, f.dsn("flavor=trino"))
		ctx, cancel := context.WithCancel(t.Context())
		rows, err := db.QueryContext(ctx, "SELECT a")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if !rows.Next() {
			t.Fatalf("reading the first row: %v", rows.Err())
		}
		cancel()
		waitFor(t, "the cancel after the context ended", func() bool { return len(deletes(f)) > 0 })
		for rows.Next() {
		}
		if err := rows.Err(); !errors.Is(err, context.Canceled) {
			t.Errorf("the error is %v, want context.Canceled", err)
		}
		rows.Close()
		if got := deletes(f); len(got) != 1 {
			t.Errorf("the cancels are %q, want one", got)
		}
	})
	t.Run("the context ended before the watch started", func(t *testing.T) {
		t.Parallel()
		// The context ends and the rows close at once, so the function of the
		// watch can run or not. Either way, one cancel goes out.
		for range 20 {
			f := newFake(t, polling("http://elsewhere.invalid", 5))
			db := f.open(t, f.dsn("flavor=trino"))
			ctx, cancel := context.WithCancel(t.Context())
			rows, err := db.QueryContext(ctx, "SELECT a")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			cancel()
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("the rows ended with %v, want nil or context.Canceled", err)
			}
			if got := deletes(f); len(got) != 1 {
				t.Fatalf("the cancels are %q, want one", got)
			}
		}
	})
}

// TestNoRedirect holds D175: the driver follows no redirect, so the secret and
// the user go to the host of the DSN only, and a redirect is an error.
func TestNoRedirect(t *testing.T) {
	t.Parallel()
	other := newFake(t, one)
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ string, _ int) {
		http.Redirect(w, &http.Request{}, other.srv.URL+"/v1/statement", http.StatusTemporaryRedirect)
	})
	db := f.open(t, "trino://trino:s3cret@"+f.host()+"?flavor=trino")
	_, err := db.ExecContext(t.Context(), "SELECT 1")
	if perr, ok := errors.AsType[*trino.Error](err); !ok || perr.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("the error is %v, want one with HTTP 307", err)
	}
	if n := len(other.requests()); n != 0 {
		t.Errorf("the host of the redirect got %d requests, want none", n)
	}
}

// TestSlugOfAnotherFlavor holds that a nextUri with the slug of Presto, on a
// connection that the DSN calls Trino, is an error, because the key flavor is
// wrong.
func TestSlugOfAnotherFlavor(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, r *http.Request, _ string, _ int) {
		writePage(w, page("http://"+r.Host+"/v1/statement/queued/q1/1?slug=x1", "", ""))
	})
	db := f.open(t, f.dsn("flavor=trino"))
	if _, err := db.ExecContext(t.Context(), "SELECT 1"); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("the error is %v, want dbimp.ErrInvalidValue", err)
	}
}

// TestProtocolErrors holds the errors of the protocol: a status that is not 2xx
// and a body of plain text or of HTML (measured).
func TestProtocolErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"trino no user", http.StatusUnauthorized, "Basic authentication or X-Trino-Original-User or X-Trino-User must be sent\n", "Basic authentication or X-Trino-Original-User or X-Trino-User must be sent"},
		{"presto no user", http.StatusBadRequest, "<html><head><title>Error 400 User must be set</title></head><body><h2>HTTP ERROR 400</h2></body></html>", "Error 400 User must be set HTTP ERROR 400"},
		{"an empty body", http.StatusServiceUnavailable, "", "Service Unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ string, _ int) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			db := f.open(t, "trino://trino:s3cret@"+f.host()+"?flavor=trino")
			_, err := db.ExecContext(t.Context(), "SELECT 1")
			perr, ok := errors.AsType[*trino.Error](err)
			if !ok || perr.HTTPStatus != tt.status || perr.Message != tt.want {
				t.Fatalf("the error is %#v, want HTTP %d with %q", err, tt.status, tt.want)
			}
			var serr *dbimp.StatusError
			if !errors.As(err, &serr) || serr.Code != tt.status {
				t.Errorf("the error does not wrap a *dbimp.StatusError of %d", tt.status)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Errorf("the error %q holds the secret", err)
			}
			if errors.Is(err, driver.ErrBadConn) {
				t.Errorf("an answer of the server wraps driver.ErrBadConn, and database/sql would run the statement again (D8)")
			}
		})
	}
}

// TestFlavorFromTheServer holds D175: with no key flavor, the driver asks GET
// /v1/info once, and sends the headers of the product that answered.
func TestFlavorFromTheServer(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		version string
		prefix  string
		flavor  string
	}{
		{"476", "X-Trino-", trino.FlavorTrino},
		{"0.299-7d50721", "X-Presto-", trino.FlavorPresto},
		{"483-SNAPSHOT", "X-Trino-", trino.FlavorTrino},
	} {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, func(w http.ResponseWriter, r *http.Request, _ string, _ int) {
				if r.URL.Path == "/v1/info" {
					writePage(w, `{"nodeVersion":{"version":"`+tt.version+`"},"environment":"docker","coordinator":true,"starting":false,"uptime":"1.00m"}`)
					return
				}
				one(w, r, "", 0)
			})
			cfg, err := trino.ParseDSN(f.dsn(""))
			if err != nil {
				t.Fatal(err)
			}
			c := trino.NewConnector(*cfg)
			db := sql.OpenDB(c)
			defer db.Close()
			if got := c.Flavor(); got != "" {
				t.Errorf("the flavor is %q before any connection, want none", got)
			}
			exec(t, db, "SELECT 1")
			exec(t, db, "SELECT 2")
			if got := c.Flavor(); got != tt.flavor {
				t.Errorf("the flavor is %q, want %q", got, tt.flavor)
			}
			var infos int
			for _, r := range f.requests() {
				if r.uri == "/v1/info" {
					infos++
				}
				if r.method == http.MethodPost && r.header.Get(tt.prefix+"User") != "trino" {
					t.Errorf("the statement has the headers %v, want %sUser", r.header, tt.prefix)
				}
			}
			if infos != 1 {
				t.Errorf("the driver asked for the version %d times, want 1", infos)
			}
		})
	}
	t.Run("the key flavor skips the request", func(t *testing.T) {
		t.Parallel()
		f := newFake(t, one)
		db := f.open(t, f.dsn("flavor=presto"))
		exec(t, db, "SELECT 1")
		for _, r := range f.requests() {
			if r.uri == "/v1/info" {
				t.Errorf("the driver asked for the version, and the DSN names the flavor")
			}
		}
	})
}

// TestRowsAffected holds that Exec returns the updateCount that the server
// sent.
func TestRowsAffected(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(w http.ResponseWriter, _ *http.Request, _ string, _ int) {
		writePage(w, `{"id":"q1","columns":[`+bigint("rows")+`],"data":[[3]],"stats":{"state":"FINISHED"},"updateType":"INSERT","updateCount":3}`)
	})
	db := f.open(t, f.dsn("flavor=trino"))
	res, err := db.ExecContext(t.Context(), "INSERT INTO t SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 3 {
		t.Errorf("the rows affected are %d, %v, want 3", n, err)
	}
}

// TestPing holds that Ping sends a statement, which checks the user.
func TestPing(t *testing.T) {
	t.Parallel()
	f := newFake(t, one)
	db := f.open(t, f.dsn("flavor=trino"))
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if reqs := f.requests(); len(reqs) != 1 || reqs[0].body != "SELECT 1" {
		t.Errorf("the requests are %+v, want one POST of SELECT 1", reqs)
	}
}

// pageNumber returns the number at the end of the path of a poll.
func pageNumber(path string) int {
	n, _ := strconv.Atoi(path[strings.LastIndexByte(path, '/')+1:])
	return n
}

// TestCloseInATransaction holds that rows that close in a transaction take the last
// pages of a query that has ended, and send no cancel, because a cancel aborts the
// transaction (measured). A query that has more than a few small pages is cancelled.
func TestCloseInATransaction(t *testing.T) {
	t.Parallel()
	deletes := func(f *fake) int {
		n := 0
		for _, r := range f.requests() {
			if r.method == http.MethodDelete {
				n++
			}
		}
		return n
	}
	// handler answers the transaction, and then a result with last pages after the
	// first row.
	handler := func(pages int, big bool) func(http.ResponseWriter, *http.Request, string, int) {
		return func(w http.ResponseWriter, r *http.Request, body string, n int) {
			switch {
			case r.Method == http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			case body == "SELECT 1" && r.Method == http.MethodPost:
				writePage(w, page("http://"+r.Host+"/v1/statement/executing/q2/t/0", bigint("a"), "[[1]]"))
			case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/q2/"):
				p := pageNumber(r.URL.Path)
				next := ""
				if p+1 < pages {
					next = "http://" + r.Host + "/v1/statement/executing/q2/t/" + strconv.Itoa(p+1)
				}
				data := ""
				if big {
					data = "[[" + strings.Repeat("1,", 40000) + "1]]"
				}
				writePage(w, page(next, bigint("a"), data))
			default:
				transactions(w, r, body, n)
			}
		}
	}
	for _, tt := range []struct {
		name    string
		pages   int
		big     bool
		deletes int
	}{
		{"a query that ended", 2, false, 0},
		{"a page that is too large", 2, true, 1},
		{"too many pages", 8, false, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, handler(tt.pages, tt.big))
			db := f.open(t, f.dsn("flavor=trino"))
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			var a int64
			if err := tx.QueryRowContext(t.Context(), "SELECT 1").Scan(&a); err != nil || a != 1 {
				t.Fatalf("QueryRow gave %d, %v, want 1", a, err)
			}
			if got := deletes(f); got != tt.deletes {
				t.Errorf("the cancels are %d, want %d", got, tt.deletes)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
