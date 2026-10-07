package trino_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/trino"
)

// head is the start of one page of an answer, up to the opening bracket of its
// member data. The columns cols are each a bigint (measured).
func head(cols ...string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = `{"name":"` + c + `","type":"bigint","typeSignature":{"rawType":"bigint","arguments":[]}}`
	}
	return `{"id":"20261006_232422_00019_x3v5n","columns":[` + strings.Join(parts, ",") + `],"data":[`
}

// RunContract counts the goroutines of the process, so this test must not run
// in parallel with another. Each fake server answers every request with one
// page that has no nextUri, so the driver sends nothing to cancel. The tests
// of the pages and of the cancel are in pages_test.go.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			cfg, err := trino.ParseDSN(strings.Replace(url, "http://", "trino://trino@", 1) + "?flavor=trino")
			if err != nil {
				t.Fatal(err)
			}
			return sql.OpenDB(trino.NewConnector(*cfg))
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `[2,1,3]],"stats":{"state":"FINISHED"}}`,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `[1,null]],"stats":{"state":"FINISHED"}}`,
			Column: 1,
		},
		// An error comes in the same page, after the data (measured).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `[1],[1],[1]],"stats":{"state":"FAILED"},"error":{"message":"boom","errorCode":8,"errorName":"DIVISION_BY_ZERO","errorType":"USER_ERROR"}}`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  `[1]`,
			Sep:  ",",
			Tail: `],"stats":{"state":"FINISHED"}}`,
		},
		Transactions: true,
	})
}
