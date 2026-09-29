package surrealdb_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"

	_ "github.com/xo/dbimp/surrealdb"
)

// openAt opens the driver against the fake server at an http:// URL, with the
// query of a DSN, such as "?encoding=json".
func openAt(t *testing.T, url, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open("surrealdb", "surrealdb://u:p@"+strings.TrimPrefix(url, "http://")+"/ns/db"+query)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// cbor builds a CBOR body for a fake server.
type cbor struct {
	dbimp.CBOREncoder
}

// pair is a key of an object and its value, which is nil, an int or a
// string.
type pair struct {
	key   string
	value any
}

// String returns the body.
func (c *cbor) String() string {
	return string(c.Bytes())
}

// statement writes the head of a response of the method query, with one
// statement.
func (c *cbor) statement() *cbor {
	c.Map(1)
	c.Text("result")
	c.Array(1)
	return c
}

// object writes an object of pairs, in their order.
func (c *cbor) object(pairs ...pair) {
	c.Map(len(pairs))
	for _, p := range pairs {
		c.Text(p.key)
		switch v := p.value.(type) {
		case nil:
			c.Null()
		case int:
			c.Int(int64(v))
		case string:
			c.Text(v)
		}
	}
}

// entry writes the entry of one statement, with the status status, and a
// result that the function rows writes.
func (c *cbor) entry(status string, rows func(*cbor)) {
	c.Map(2)
	c.Text("result")
	rows(c)
	c.Text("status")
	c.Text(status)
}

// RunContract counts the goroutines of the process, so the contract tests
// must not run in parallel with another test.
func TestContractCBOR(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	var columns, null, errAfter, head, row, tail cbor
	columns.statement().entry("OK", func(c *cbor) {
		c.Array(1)
		c.object(pair{"b", 2}, pair{"a", 1}, pair{"c", 3})
	})
	null.statement().entry("OK", func(c *cbor) {
		c.Array(1)
		c.object(pair{"a", 1}, pair{"b", nil})
	})
	errAfter.statement().entry("ERR", func(c *cbor) {
		c.Array(3)
		c.Int(0)
		c.Int(1)
		c.Int(2)
	})
	head.statement()
	head.Map(2)
	head.Text("result")
	head.Raw([]byte{0x9f})
	row.object(pair{"a", 1})
	tail.Raw([]byte{0xff})
	tail.Text("status")
	tail.Text("OK")
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return openAt(t, url, "")
		},
		Query:          "SELECT * FROM t",
		Columns:        dbimptest.ColumnsCase{Body: columns.String(), Want: []string{"b", "a", "c"}},
		Null:           dbimptest.NullCase{Body: null.String(), Column: 1},
		ErrorAfterRows: dbimptest.ErrorCase{Body: errAfter.String(), Rows: 3},
		Stream:         dbimptest.StreamCase{Head: head.String(), Row: row.String(), Tail: tail.String()},
		ContentType:    "application/cbor",
	})
}

func TestContractJSON(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return openAt(t, url, "?encoding=json")
		},
		Query: "SELECT * FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: `{"result":[{"result":[{"b":2,"a":1,"c":3}],"status":"OK","time":"1ms"}]}`,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   `{"result":[{"result":[{"a":1,"b":null}],"status":"OK","time":"1ms"}]}`,
			Column: 1,
		},
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: `{"result":[{"result":[0,1,2],"status":"ERR","time":"1ms"}]}`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"result":[{"result":[`,
			Row:  `{"a":1}`,
			Sep:  `,`,
			Tail: `],"status":"OK","time":"1ms"}]}`,
		},
	})
}
