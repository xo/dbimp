package cosmos_test

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/cosmos"
)

// TestRecordedErrors holds that an error of the server reaches the caller
// before the first row, as an *Error with the status, the substatus, the code
// and the text that the server wrote, on the hosted account and on the
// emulator (recorded). The driver sends each request once, so HTTP 429 reaches
// the caller too (D8).
func TestRecordedErrors(t *testing.T) {
	t.Parallel()
	type want struct {
		status, sub int
		code        string
		text        string
	}
	for _, tt := range []struct {
		name, server, container, query, dsn string
		want                                want
	}{
		{"a syntax error", hosted, "kv", "SELECT FROM c", "", want{400, 0, "SC1001", "Syntax error, incorrect syntax near 'FROM'."}},
		{"a syntax error", emulator, "kv", "SELECT FROM c", "", want{400, 0, "SC1001", "Syntax error, incorrect syntax near 'FROM'."}},
		{"an unknown function", hosted, "kv", "SELECT ABC(1) AS x FROM c", "", want{400, 0, "SC2005", ""}},
		{"a division by zero", hosted, "kv", "SELECT 1/0 AS x, 1 AS y", "", want{400, 4001, "4001", "numeric value that cannot be represented"}},
		{"an empty statement", hosted, "kv", "", "", want{400, 0, "SC1002", ""}},
		{"two statements", hosted, "kv", "SELECT 1 AS a; SELECT 2 AS b", "", want{400, 0, "SC1010", ""}},
		{"a block comment", hosted, "kv", "SELECT 1 AS a /* comment */", "", want{400, 0, "SC1001", ""}},
		{"INSERT", hosted, "kv", "INSERT INTO c VALUES (1)", "", want{400, 0, "SC1001", ""}},
		{"a container that does not exist", hosted, "nope", "SELECT * FROM c", "", want{404, 0, "NotFound", "Resource Not Found"}},
		{"a container that does not exist", emulator, "nope", "SELECT * FROM c", "", want{404, 1003, "", "Owner resource does not exist"}},
		{"a query across partitions", hosted, "bulk", "SELECT VALUE COUNT(1) FROM c", "", want{400, 1004, "BadRequest", "cross partition query can not be directly served by the gateway"}},
		{"an order by across partitions", hosted, "bulk", "SELECT c.id, c.n FROM c ORDER BY c.n, c.id", "pagesize=2", want{400, 1004, "BadRequest", "cross partition query can not be directly served by the gateway"}},
		{"a group by", hosted, "bulk", "SELECT c.grp, COUNT(1) AS n, AVG(c.n) AS a FROM c GROUP BY c.grp", "", want{400, 0, "BadRequest", "Cross partition query only supports 'VALUE <AggregateFunc>' for aggregates."}},
		{"too many requests", hosted, "bulk", "SELECT * FROM c", "pagesize=5000", want{429, 3200, "TooManyRequests", "Request rate is large."}},
	} {
		t.Run(tt.name+" on "+tt.server, func(t *testing.T) {
			t.Parallel()
			srv := replayServer(t, tt.server)
			dsn := tt.dsn
			db := openFake(t, srv.URL, tt.container, dsn)
			err := drain(t, db, tt.query)
			if err == nil {
				t.Fatalf("the statement %q gave no error", tt.query)
			}
			cerr, ok := errors.AsType[*cosmos.Error](err)
			if !ok {
				t.Fatalf("the error is %v, want an *Error", err)
			}
			if cerr.HTTPStatus != tt.want.status || cerr.SubStatus != tt.want.sub {
				t.Errorf("the status is %d.%d, want %d.%d", cerr.HTTPStatus, cerr.SubStatus, tt.want.status, tt.want.sub)
			}
			if tt.want.code != "" && cerr.Code != tt.want.code {
				t.Errorf("the code is %q, want %q", cerr.Code, tt.want.code)
			}
			if !strings.Contains(cerr.Message, tt.want.text) {
				t.Errorf("the message is %q, want text that holds %q", cerr.Message, tt.want.text)
			}
			if strings.Contains(cerr.Message, "ActivityId") || strings.Contains(cerr.Message, "\n") {
				t.Errorf("the message is %q, which holds the line of the activity", cerr.Message)
			}
			var status *dbimp.StatusError
			if !errors.As(err, &status) || status.Code != tt.want.status {
				t.Errorf("the error does not wrap the *dbimp.StatusError of the response")
			}
		})
	}
}

// TestTooManyRequests holds D8 and D190: the driver sends a request that got
// HTTP 429 once, and never again. The error holds the wait that the server
// asks for in X-Ms-Retry-After-Ms, which the caller can use, because the text
// "no changes were made" says that the request did not run (recorded: "lead:
// a burst of reads, number 1").
func TestTooManyRequests(t *testing.T) {
	t.Parallel()
	srv := replayServer(t, hosted)
	c := openCounted(t, srv.URL, "bulk", "pagesize=5000")
	err := drain(t, c.db, "SELECT * FROM c")
	cerr, ok := errors.AsType[*cosmos.Error](err)
	if !ok || cerr.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("the error is %v, want an *Error of HTTP 429", err)
	}
	if cerr.RetryAfter != 287*time.Millisecond {
		t.Errorf("the wait is %v, want 287ms", cerr.RetryAfter)
	}
	if n := c.n.Load(); n != 1 {
		t.Errorf("the driver sent %d requests, want 1", n)
	}
	if want := "cosmos: 429.3200 TooManyRequests: Request rate is large."; !strings.HasPrefix(cerr.Error(), want) {
		t.Errorf("the error says %q, want it to start with %q", cerr.Error(), want)
	}
}

// TestAnErrorHoldsNoKey holds D94: no error that the driver returns holds the
// key.
func TestAnErrorHoldsNoKey(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	db := openFake(t, srv.URL, "c", "")
	err := drain(t, db, "SELECT 1")
	if err == nil || strings.Contains(err.Error(), "dsZQi3") || !strings.Contains(err.Error(), "401") {
		t.Errorf("the error is %v, want HTTP 401 and no key", err)
	}
	// A server that is not there: the request did not reach it, so database/sql
	// can try again, and the error holds no key.
	db2, err := sql.Open(cosmos.Name, "cosmos://x:"+escapedKey+"@127.0.0.1:1/db/c?tls=false")
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err := drain(t, db2, "SELECT 1"); err == nil || strings.Contains(err.Error(), "dsZQi3") {
		t.Errorf("the error is %v, want an error with no key", err)
	}
}

// TestErrorBodies holds the forms of an error body that the driver reads:
// text that is no JSON, an empty body, and an object with no message.
func TestErrorBodies(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		status int
		body   string
		want   string
	}{
		{500, "Database query failed: PostgresError", "Database query failed: PostgresError"},
		{500, "", "Internal Server Error"},
		{502, "<html>bad gateway</html>", "Bad Gateway"},
		{400, `{"code":"BadRequest"}`, "Bad Request"},
		{400, `{"code":"BadRequest","message":"Message: {\"Errors\":[\"Parameter names should be in the format\"]}\r\nActivityId: a"}`, "Parameter names should be in the format"},
		{400, `{"code":"BadRequest","message":"Message: not JSON\r\nActivityId: a"}`, "not JSON"},
		{403, `{"errors":[{"code":"X","message":"first"},{"code":"Y","message":"second"}]}`, "first"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte(tt.body))
		}))
		t.Cleanup(srv.Close)
		err := drain(t, openFake(t, srv.URL, "c", ""), "SELECT 1")
		cerr, ok := errors.AsType[*cosmos.Error](err)
		if !ok || cerr.HTTPStatus != tt.status || !strings.Contains(cerr.Message, tt.want) {
			t.Errorf("the body %q gave %v, want an *Error of %d with %q", tt.body, err, tt.status, tt.want)
		}
	}
}
