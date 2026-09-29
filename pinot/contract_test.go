package pinot_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/pinot"
)

// head is the start of an answer with the columns cols, each an INT, up to
// its rows, in the order in which the Broker writes the members (measured).
func head(cols ...string) string {
	types := make([]string, len(cols))
	for i := range cols {
		types[i] = `"INT"`
	}
	return `{"resultTable":{"dataSchema":{"columnNames":["` + strings.Join(cols, `","`) + `"],"columnDataTypes":[` + strings.Join(types, ",") + `]},"rows":[`
}

// tail is the end of an answer that the server completed.
const tail = `]},"numRowsResultSet":1,"partialResult":false,"exceptions":[],"requestId":"1"}`

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request with one
// body.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(pinot.Name, strings.Replace(url, "http://", "pinot://", 1))
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `[2,1,3]` + tail,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `[1,null]` + tail,
			Column: 1,
		},
		// A join that stops at maxRowsInJoin sends its rows, and then
		// partialResult true with no exception (measured).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `[1],[1],[1]]},"numRowsResultSet":3,"partialResult":true,"exceptions":[],"requestId":"1"}`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  `[1]`,
			Sep:  `,`,
			Tail: tail,
		},
		Transactions: false,
	})
}
