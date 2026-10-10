package athena //nolint:testpackage // The contract answers two of the three calls of a statement itself, which only the package can set.

import (
	"bytes"
	"database/sql"
	"io"
	"maps"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// contractTransport lets the contract run against a fake server that answers
// every request with one body. A statement is three calls, so the transport
// answers StartQueryExecution and GetQueryExecution with the recorded answers of
// a SELECT (recorded: "a select"), and sends GetQueryResults to the fake
// server, whose body is the page of results. For the start it dials the fake
// server first, so that a server that is not there fails the call before it
// reaches the server, as it does for the real service.
type contractTransport struct {
	next  http.RoundTripper
	start *dbimptest.Exchange
	poll  *dbimptest.Exchange
}

// RoundTrip satisfies http.RoundTripper.
func (ct contractTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var ex *dbimptest.Exchange
	switch strings.TrimPrefix(req.Header.Get("X-Amz-Target"), targetPrefix) {
	case "StartQueryExecution":
		conn, err := (&net.Dialer{}).DialContext(req.Context(), "tcp", req.URL.Host)
		if err != nil {
			return nil, err //nolint:wrapcheck // The error is the dial error that http.Client wraps.
		}
		_ = conn.Close()
		ex = ct.start
	case "GetQueryExecution":
		ex = ct.poll
	default:
		return ct.next.RoundTrip(req) //nolint:wrapcheck // http.Client wraps the error.
	}
	_, _ = io.Copy(io.Discard, req.Body)
	h := http.Header{}
	maps.Copy(h, ex.Response.Header)
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(bytes.NewReader(ex.Response.Content())),
		ContentLength: int64(len(ex.Response.Content())),
		Request:       req,
	}, nil
}

// cols are the ColumnInfos of a page whose columns have the type typ, and the
// names names.
func cols(typ string, names ...string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = `{"CaseSensitive":false,"CatalogName":"hive","Label":"` + n + `","Name":"` + n + `","Nullable":"UNKNOWN","Precision":10,"Scale":0,"SchemaName":"","TableName":"","Type":"` + typ + `"}`
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// head is the start of a page of results of integers, up to the header row and
// its comma, which a SELECT sends first and the driver drops (recorded: "a
// select").
func head(names ...string) string {
	return headOf("integer", names...)
}

// headOf is head for columns of the type typ.
func headOf(typ string, names ...string) string {
	header := make([]string, len(names))
	for i, n := range names {
		header[i] = `"` + n + `"`
	}
	return `{"ResultSet":{"ColumnInfos":` + cols(typ, names...) + `,"ResultRows":[{"Data":[` + strings.Join(header, ",") + `]},`
}

// RunContract counts the goroutines of the process, so this test must not run in
// parallel with another. The tests of the pages, of the polls and of the cancel
// of a running query are in pages_test.go and query_test.go.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	start, poll := exchange(t, 17), exchange(t, 18)
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			c := connectorAt(t, testConfig(), url)
			c.client.Transport = contractTransport{next: c.client.Transport, start: start, poll: poll}
			db := sql.OpenDB(c)
			t.Cleanup(func() { _ = c.Close() })
			return db
		},
		Query:       "SELECT b, a, c FROM t",
		ContentType: "application/x-amz-json-1.1",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `{"Data":["2","1","3"]}]}}`,
			Want: []string{"b", "a", "c"},
		},
		// A NULL has no value in its datum, and is null in the form ResultRows
		// (recorded: "the type of CAST(NULL AS integer)").
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `{"Data":["1",null]}]}}`,
			Column: 1,
		},
		// A query that fails does so before the first row (recorded: "an error
		// after some rows"), so the only way that a page ends after some rows is a
		// body that ends early.
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `{"Data":["1"]},{"Data":["1"]},{"Data":["1"]},`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: headOf("varchar", "s"),
			Row:  `{"Data":["xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"]}`,
			Sep:  ",",
			Tail: `]}}`,
		},
		Transactions: false,
	})
}
