package cosmos_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/cosmos"
	"github.com/xo/dbimp/dbimptest"
)

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. The bodies are the form of the answer to a
// query (recorded: "a query that selects every document"): an object with the
// array Documents, whose documents are objects.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(cosmos.Name, dsnFor(url, "/db/c", ""))
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "SELECT c.c, c.a, c.b FROM c",
		// The columns are the keys of the first document, in the order that
		// they arrive (D190).
		Columns: dbimptest.ColumnsCase{
			Body: `{"_rid":"r","Documents":[{"c":3,"a":1,"b":2}],"_count":1}`,
			Want: []string{"c", "a", "b"},
		},
		// A null is nil, and a key that a document lacks is nil too (D190).
		Null: dbimptest.NullCase{
			Body:   `{"_rid":"r","Documents":[{"b":1,"a":null}],"_count":1}`,
			Column: 1,
		},
		// A page is one body, so an error after some documents is a body that
		// ends early.
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: `{"_rid":"r","Documents":[{"a":1},{"a":1},{"a":1},`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"_rid":"r","Documents":[`,
			Row:  `{"a":"` + strings.Repeat("x", 64) + `"}`,
			Sep:  ",",
			Tail: `],"_count":1}`,
		},
		Transactions: false,
	})
}
