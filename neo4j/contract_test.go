package neo4j_test

import (
	"database/sql"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// typed writes one value of typed JSON.
func typed(typ, value string) string {
	return `{"$type":"` + typ + `","_value":` + value + `}`
}

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. The fake servers of the contract answer every
// request with one body, so the driver sends nothing to stop a query when its
// context ends (cancel=none). TestCancel tests that.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	one := typed("Integer", `"1"`)
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return open(t, url, "?cancel=none")
		},
		Query: "RETURN 1 AS a",
		Columns: dbimptest.ColumnsCase{
			Body: `{"data":{"fields":["b","a","c"],"values":[[` + typed("Integer", `"2"`) + `,` + one + `,` + typed("Integer", `"3"`) + `]]},"bookmarks":["FB:x"]}`,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   `{"data":{"fields":["a","b"],"values":[[` + one + `,` + typed("Null", "null") + `]]}}`,
			Column: 1,
		},
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: `{"data":{"fields":["y"],"values":[[` + one + `],[` + one + `],[` + one + `]]},` +
				`"errors":[{"code":"Neo.ClientError.Statement.ArithmeticError","message":"/ by zero"}]}`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"data":{"fields":["a"],"values":[`,
			Row:  `[` + one + `]`,
			Sep:  `,`,
			Tail: `]},"bookmarks":["FB:x"]}`,
		},
		Transactions: true,
		ContentType:  "application/vnd.neo4j.query",
	})
}
