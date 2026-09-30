package rqlite_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/rqlite"
)

// head is the start of an answer with the columns cols, each an INTEGER, up
// to its rows, in the order in which the server writes the members
// (measured).
func head(cols ...string) string {
	types := make([]string, len(cols))
	for i := range cols {
		types[i] = `"integer"`
	}
	return `{"results":[{"columns":["` + strings.Join(cols, `","`) + `"],"types":[` + strings.Join(types, ",") + `],"values":[`
}

// tail is the end of an answer with no error.
const tail = `]}]}`

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request with one
// body.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(rqlite.Name, strings.Replace(url, "http://", "rqlite://", 1))
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
		// The server reads every row before it answers, so it sends no error
		// after the rows of a statement (measured). The form of its result
		// puts error after values, and the driver reads one there as an error
		// after the rows.
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `[1],[1],[1]],"error":"interrupted"}]}`,
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
