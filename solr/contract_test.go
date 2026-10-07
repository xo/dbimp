package solr_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/solr"
)

// head is the start of an answer with the columns cols, up to the tuple of
// the metadata and the comma after it (measured).
func head(cols ...string) string {
	fields := `"` + strings.Join(cols, `","`) + `"`
	aliases := make([]string, len(cols))
	for i, c := range cols {
		aliases[i] = `"` + c + `":"` + c + `"`
	}
	return `{"result-set":{"docs":[{"isMetadata":true,"fields":[` + fields + `],"aliases":{` + strings.Join(aliases, ",") + `}},`
}

// eof is the end of an answer: the tuple EOF, and the closing of the
// result-set (measured).
const eof = `{"EOF":true,"RESPONSE_TIME":1}]}}`

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. The statement names no table, so the driver
// reads no schema, which a fake server that answers every request with one
// body cannot give.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(solr.Name, strings.Replace(url, "http://", "solr://u:p@", 1)+"/c")
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "SELECT b, a, c",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + `{"b":2,"a":1,"c":3},` + eof,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + `{"a":1,"b":null},` + eof,
			Column: 1,
		},
		// An error after some rows is the tuple EOF with EXCEPTION
		// (measured).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + `{"a":1},{"a":1},{"a":1},{"EXCEPTION":"it failed","EOF":true,"RESPONSE_TIME":1}]}}`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  `{"a":1}`,
			Sep:  ",",
			Tail: "," + eof,
		},
		Transactions: false,
	})
}
