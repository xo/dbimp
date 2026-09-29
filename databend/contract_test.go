package databend_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/databend"
	"github.com/xo/dbimp/dbimptest"
)

// head is the start of an answer of one page with the columns cols, up to
// its rows, in the order in which the server writes the members (measured).
func head(cols ...string) string {
	var schema []string
	for _, c := range cols {
		schema = append(schema, `{"name":"`+c+`","type":"Nullable(Int32)"}`)
	}
	return `{"id":"q1","node_id":"n1","state":"Succeeded","session":{"database":"default","txn_state":"AutoCommit"},"error":null,"has_result_set":true,"schema":[` + strings.Join(schema, ",") + `],"data":[`
}

// tail is the end of an answer of one page, with no next page.
const tail = `],"affect":null,"settings":{"timezone":"UTC"},"next_uri":null,"final_uri":null,"kill_uri":"/v1/query/q1/kill"}`

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request with one
// body, so the driver kills nothing (cancel=none), and each answer has no
// next page.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(databend.Name, strings.Replace(url, "http://", "databend://", 1)+"/default?cancel=none")
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `["2","1","3"]` + tail,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `["1",null]` + tail,
			Column: 1,
		},
		// The body ends after three rows, as when the server closes it.
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `["1"],["1"],["1"],`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  `["1"]`,
			Sep:  `,`,
			Tail: tail,
		},
		Transactions: true,
	})
}
