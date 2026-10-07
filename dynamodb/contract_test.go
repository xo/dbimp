package dynamodb_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/dynamodb"
)

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. The bodies are the form of ExecuteStatement
// (recorded: "a statement"): an object with the array Items, whose items are
// objects of typed values.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(dynamodb.Name, strings.Replace(url, "http://", "dynamodb://key:secret@", 1)+"?region=us-east-1&tls=false")
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query:       "SELECT b, a, c FROM t",
		ContentType: "application/x-amz-json-1.0",
		// The server sends the members of an item in an order of its own
		// (recorded: "a projection"), so the columns are the order of the
		// statement.
		Columns: dbimptest.ColumnsCase{
			Body: `{"Items":[{"c":{"N":"3"},"a":{"N":"1"},"b":{"N":"2"}}]}`,
			Want: []string{"b", "a", "c"},
		},
		// An attribute that the item lacks is nil, as NULL is (D169).
		Null: dbimptest.NullCase{
			Body:   `{"Items":[{"b":{"N":"1"},"a":{"NULL":true}}]}`,
			Column: 1,
		},
		// A page is one body, so an error after some items is a body that ends
		// early.
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: `{"Items":[{"a":{"N":"1"}},{"a":{"N":"1"}},{"a":{"N":"1"}},`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"Items":[`,
			Row:  `{"a":{"S":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}}`,
			Sep:  ",",
			Tail: `]}`,
		},
		Transactions: false,
	})
}
