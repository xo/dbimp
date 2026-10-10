package spanner //nolint:testpackage // The contract runs with a session that the connector holds, which only the package can set.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// head is the start of a stream of one message with the columns cols, each an
// INT64, up to the opening of the list of values (recorded: "a statement on the
// stream").
func head(cols ...string) string {
	fields := make([]string, len(cols))
	for i, col := range cols {
		fields[i] = `{"name":"` + col + `","type":{"code":"INT64"}}`
	}
	return `[{"metadata":{"rowType":{"fields":[` + strings.Join(fields, ",") + `]}},"values":[`
}

// tail is the end of a stream of one message.
const tail = `],"last":true}]`

// TestContract runs the contract of every driver. RunContract counts the
// goroutines of the process, so this test must not run in parallel with another.
// A fake server answers every request with one body, so the connector holds its
// session, and no test of the contract sends a request for one.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return open(t, config(), url, true)
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `"2","1","3"` + tail,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `"1",null` + tail,
			Column: 1,
		},
		// An error after some rows is an element that holds error, with HTTP 200
		// (recorded: "a division by zero in row 3500 of 4000, unsorted, on the
		// stream").
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `"1","2","3"` + `]},{"error":{"code":400,"message":"division by zero","status":"OUT_OF_RANGE"}}]`,
			Rows: 3,
		},
		// Each row is a message of its own. The server sends the last value of a
		// message with chunkedValue after it, so the driver holds back that value
		// until the message ends (D191 item 11), and a stream that stops in the
		// middle of a message would hold the first row for ever. The server never
		// does that, because it writes a whole message at a time.
		Stream: dbimptest.StreamCase{
			Head: head("a") + `"0"]},`,
			Row:  `{"values":["1"]}`,
			Sep:  ",",
			Tail: `]`,
		},
		Transactions: true,
	})
}
