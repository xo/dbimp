package arangodb_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/arangodb"
	"github.com/xo/dbimp/dbimptest"
)

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request with one
// body, so the driver sends no tag (cancel=none), and the error after some
// rows is a body that ends before its end, because the error of a stream
// arrives with a later fetch (D90), which the replay tests hold.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(arangodb.Name, strings.Replace(url, "http://", "arangodb://", 1)+"/db?cancel=none")
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "FOR u IN c RETURN u",
		Columns: dbimptest.ColumnsCase{
			Body: `{"result":[{"b":2,"a":1,"c":3}],"hasMore":false,"cached":false,"error":false,"code":201}`,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   `{"result":[{"a":1,"b":null}],"hasMore":false,"error":false,"code":201}`,
			Column: 1,
		},
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: `{"result":[1,1,1],`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"result":[`,
			// A row that ends at its last byte, as each row of a real batch
			// does, because more of the batch follows it.
			Row:  `{"a":1}`,
			Sep:  `,`,
			Tail: `],"hasMore":false,"error":false,"code":201}`,
		},
		Transactions: true,
	})
}
