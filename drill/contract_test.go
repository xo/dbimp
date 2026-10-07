package drill //nolint:testpackage // The contract runs with no cancel and no profile, which only the package can set.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// head is the start of an answer with the columns cols, each a BIGINT, as
// the server writes it: each member and each row starts a new line
// (measured).
func head(cols ...string) string {
	types := make([]string, len(cols))
	for i := range cols {
		types[i] = `"BIGINT"`
	}
	return `{"queryId":"15414a6c-aa54-92d7-fbe4-920a25c41b97"` + "\n" +
		`,"columns":["` + strings.Join(cols, `","`) + `"]` + "\n" +
		`,"metadata":[` + strings.Join(types, ",") + `]` + "\n" +
		`,"attemptedAutoLimit":0` + "\n" +
		`,"rows":[` + "\n"
}

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request with one
// body, a cancel and a profile too, so the driver sends no cancel and reads
// no profile when a query ends early (noStop). TestCancel holds the cancel.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			cfg, err := ParseDSN(strings.Replace(url, "http://", "drill://", 1))
			if err != nil {
				t.Fatal(err)
			}
			c := NewConnector(*cfg)
			c.noStop = true
			return sql.OpenDB(c)
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `{"b":2,"a":1,"c":3}` + "\n]\n" + `,"queryState":"COMPLETED"` + "\n}\n",
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `{"a":1,"b":null}` + "\n]\n" + `,"queryState":"COMPLETED"` + "\n}\n",
			Column: 1,
		},
		// An error after some rows ends the array of rows, then sends
		// FAILED with no message (measured).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `{"a":1}` + "\n" + `,{"a":1}` + "\n" + `,{"a":1}` + "\n]\n" + `,"queryState":"FAILED"` + "\n}\n",
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a") + `{"a":1}` + "\n",
			Row:  `,{"a":1}`,
			Sep:  "\n",
			Tail: "\n]\n" + `,"queryState":"COMPLETED"` + "\n}\n",
		},
		Transactions: false,
	})
}
