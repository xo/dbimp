package couchbase_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"

	_ "github.com/xo/dbimp/couchbase"
)

// openAt opens the driver against the fake server at an http:// URL.
func openAt(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := sql.Open("couchbase", "couchbase://u:p@"+strings.TrimPrefix(url, "http://")+"/")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestContract(t *testing.T) {
	t.Parallel()
	dbimptest.RunContract(t, dbimptest.Contract{
		Open:  openAt,
		Query: "SELECT 1",
		Columns: dbimptest.ColumnsCase{
			Body: `{"requestID":"x","signature":{"b":"number","a":"number","c":"number"},"results":[{"b":2,"a":1,"c":3}],"status":"success"}`,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   `{"signature":{"a":"number","b":"json"},"results":[{"a":1}],"status":"success"}`,
			Column: 1,
		},
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: `{"signature":"json","results":[0,1,2],"errors":[{"code":5011,"msg":"Abort: boom"}],"status":"fatal"}`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"signature":{"a":"number"},"results":[`,
			Row:  `{"a":1}`,
			Sep:  `,`,
			Tail: `],"status":"success"}`,
		},
		Transactions: true,
	})
}
