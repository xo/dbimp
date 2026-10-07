package elasticsearch_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/elasticsearch"
)

// head is the start of a page of the first answer, up to the opening bracket
// of rows. The columns cols are each a long (measured).
func head(cols ...string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = `{"name":"` + c + `","type":"long"}`
	}
	return `{"columns":[` + strings.Join(parts, ",") + `],"rows":[`
}

// RunContract counts the goroutines of the process, so this test must not run
// in parallel with another. Each fake server answers every request with one
// page that has no cursor, so the driver sends no request for a next page.
// The tests of the pages and of the cursor are in pages_test.go.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			cfg, err := elasticsearch.ParseDSN(strings.Replace(url, "http://", "elasticsearch://", 1))
			if err != nil {
				t.Fatal(err)
			}
			return sql.OpenDB(elasticsearch.NewConnector(*cfg))
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `[2,1,3]]}`,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `[1,null]]}`,
			Column: 1,
		},
		// The server sends HTTP 200 and the whole page, so an error after
		// some rows is a page that ends where the server stopped (the
		// recorded error comes on a later page: see pages_test.go).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `[1],[1],[1]`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  `[1]`,
			Sep:  ",",
			Tail: `]}`,
		},
		Transactions: false,
	})
}
