package bigquery //nolint:testpackage // The contract opens a connector at a fake server, which only the package can do.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// head is the start of an answer of HTTP 200 with the columns cols, each an
// INTEGER, up to the opening of rows (recorded: bigquery-017). The members after
// the opening, such as the count, come in tail.
func head(cols ...string) string {
	fields := make([]string, len(cols))
	for i, col := range cols {
		fields[i] = `{"name":"` + col + `","type":"INTEGER","mode":"NULLABLE"}`
	}
	return `{"kind":"bigquery#queryResponse","schema":{"fields":[` + strings.Join(fields, ",") + `]},` +
		`"jobReference":{"projectId":"p","jobId":"job_1","location":"US"},"rows":[`
}

// tail is the end of an answer after its rows, with the count of rows that the
// answer holds when total is not empty (recorded: bigquery-017).
func tail(total string) string {
	if total == "" {
		return `],"totalBytesProcessed":"0","jobComplete":true,"cacheHit":false}`
	}
	return `],"totalRows":"` + total + `","jobComplete":true}`
}

// row writes a row of INTEGER values, with null for a value of "".
func row(vals ...string) string {
	cells := make([]string, len(vals))
	for i, v := range vals {
		if v == "" {
			cells[i] = `{"v":null}`
			continue
		}
		cells[i] = `{"v":"` + v + `"}`
	}
	return `{"f":[` + strings.Join(cells, ",") + `]}`
}

// RunContract counts the goroutines of the process, so this test must not run
// in parallel with another test. A fake server answers every request with one
// body.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return open(t, config(), url)
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + row("2", "1", "3") + tail("1"),
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + row("1", "") + tail("1"),
			Column: 1,
		},
		// The service runs the whole statement before it answers, so a result
		// ends after some rows only when it holds fewer rows than totalRows says
		// (D189).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + row("1") + "," + row("2") + "," + row("3") + tail("5"),
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  row("1"),
			Sep:  ",",
			Tail: tail(""),
		},
		Transactions: false,
	})
}
