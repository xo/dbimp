package druid //nolint:testpackage // The contract runs with no cancel, which only the package can set.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// head is the header of an answer in arrayLines with the columns cols, each
// a BIGINT: the names, the native types and the SQL types, one line each
// (measured).
func head(cols ...string) string {
	native := make([]string, len(cols))
	sqlTypes := make([]string, len(cols))
	for i := range cols {
		native[i], sqlTypes[i] = `"LONG"`, `"BIGINT"`
	}
	return `["` + strings.Join(cols, `","`) + "\"]\n[" + strings.Join(native, ",") + "]\n[" + strings.Join(sqlTypes, ",") + "]\n"
}

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request with one
// body, a cancel too, so the driver sends no cancel when a context ends
// (noStop). TestCancel holds the cancel.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			cfg, err := ParseDSN(strings.Replace(url, "http://", "druid://", 1))
			if err != nil {
				t.Fatal(err)
			}
			c := NewConnector(*cfg)
			c.noStop = true
			return sql.OpenDB(c)
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + "[2,1,3]\n\n",
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + "[1,null]\n\n",
			Column: 1,
		},
		// An error after some rows ends the answer with no empty line
		// (measured).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + "[1]\n[1]\n[1]\n",
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  `[1]`,
			Sep:  "\n",
			Tail: "\n\n",
		},
		Transactions: false,
	})
}
