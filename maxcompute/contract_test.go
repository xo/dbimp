package maxcompute //nolint:testpackage // The contract answers two of the three calls of a statement itself, which only the package can set.

import (
	"database/sql"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// contractTransport lets the contract run against a fake server that answers every
// request with one body. A statement is three calls, so the transport answers the
// creation and the poll with the recorded answers of a SELECT (recorded: "select
// 1"), and sends the read of the result to the fake server, whose body is the
// result. It answers the creation of EXPLAIN OUTPUT with the recorded error of a
// statement that does not parse, so that the driver sends the statement itself and
// reads its columns as strings (D219). For the creation it dials the fake server
// first, so that a server that is not there fails the call before it reaches the
// server, as it does for the real service.
type contractTransport struct {
	next    http.RoundTripper
	create  *dbimptest.Exchange
	refused *dbimptest.Exchange
	status  *dbimptest.Exchange
}

// RoundTrip satisfies http.RoundTripper.
func (ct contractTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var ex *dbimptest.Exchange
	switch {
	case req.Method == http.MethodPost:
		conn, err := (&net.Dialer{}).DialContext(req.Context(), "tcp", req.URL.Host)
		if err != nil {
			return nil, err //nolint:wrapcheck // The error is the dial error that http.Client wraps.
		}
		_ = conn.Close()
		body, _ := io.ReadAll(req.Body)
		ex = ct.create
		if strings.Contains(string(body), explainPrefix) {
			ex = ct.refused
		}
	case strings.Contains(req.URL.RawQuery, "taskstatus"):
		ex = ct.status
	default:
		return ct.next.RoundTrip(req) //nolint:wrapcheck // http.Client wraps the error.
	}
	return respond(req, ex), nil
}

// refusedExchange returns the recorded answer of a statement that does not
// parse, which is HTTP 400 with the code ParseError (recorded: "a syntax error").
func refusedExchange(tb testing.TB) *dbimptest.Exchange {
	tb.Helper()
	for n := 1; n < exchangeCount; n++ {
		ex := exchange(tb, n)
		if ex.Response.Status == http.StatusBadRequest && strings.Contains(string(ex.Response.Content()), "<Code>ParseError</Code>") && strings.Contains(string(ex.Request.Content()), "SELEC 1") {
			return ex
		}
	}
	tb.Fatal("no recorded statement that does not parse")
	return nil
}

// RunContract counts the goroutines of the process, so this test must not run in
// parallel with another. The tests of the flow of a statement are in
// replay_test.go and query_test.go.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	f := recordedFlow(t, "SELECT 1 AS a, 'x' AS b", "Success")
	refused := refusedExchange(t)
	head, tail := resultParts(t, f.result)
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			c := connectorAt(t, recordedConfig(), url)
			c.client.Transport = contractTransport{next: c.client.Transport, create: f.create, refused: refused, status: f.status}
			db := sql.OpenDB(c)
			t.Cleanup(func() { _ = c.Close() })
			return db
		},
		Query:       "SELECT z, y, x, b, a",
		ContentType: "application/xml",
		Columns: dbimptest.ColumnsCase{
			Body: head + "\"z\",\"y\",\"x\",\"b\",\"a\"\r\n1,2,3,4,5\r\n" + tail,
			Want: []string{"z", "y", "x", "b", "a"},
		},
		// A NULL is the cell "\N" in quotes (recorded: "type tinyint: low, high and
		// NULL").
		Null: dbimptest.NullCase{
			Body:   head + "\"a\",\"b\"\r\n1,\"\\N\"\r\n" + tail,
			Column: 1,
		},
		// A result that the server cut ends inside its character data, so the only
		// way that a result ends after some rows is a body that ends early.
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head + "\"a\"\r\n1\r\n1\r\n1\r\n",
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head + "\"s\"\r\n",
			Row:  "\"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\"\r\n",
			Sep:  "",
			Tail: tail,
		},
		Transactions: false,
	})
}
