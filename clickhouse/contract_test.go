package clickhouse //nolint:testpackage // The contract runs with no cancel and no version, which only the package can set.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// head is the first two lines of an answer with the columns cols, each an
// Int64: the names, and the types (measured).
func head(cols ...string) string {
	types := make([]string, len(cols))
	for i := range cols {
		types[i] = `"Int64"`
	}
	return `["` + strings.Join(cols, `", "`) + "\"]\n[" + strings.Join(types, ", ") + "]\n"
}

// RunContract counts the goroutines of the process, so this test must not run in
// parallel with another. A fake server answers every request with one body, a
// cancel and the version too, so the driver sends no cancel when a context ends
// (noStop), and asks no version (noVersion). The tests of the cancel and of the
// version are in cancel_test.go and replay_test.go.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			cfg, err := ParseDSN(strings.Replace(url, "http://", Name+"://", 1))
			if err != nil {
				t.Fatal(err)
			}
			c := NewConnector(*cfg)
			c.noStop, c.noVersion = true, true
			return sql.OpenDB(c)
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + "[2, 1, 3]\n",
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + "[1, null]\n",
			Column: 1,
		},
		// An error after some rows is the marker, and the text after it
		// (measured on 25.8).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + "[1]\n[1]\n[1]\n" + marker + "\r\nCode: 395. DB::Exception: boom. (FUNCTION_THROW_IF_VALUE_IS_NON_ZERO) (version 25.8.33.6 (official build))\n",
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  `[1]`,
			Sep:  "\n",
			Tail: "\n",
		},
		ContentType:  "text/plain; charset=UTF-8",
		Transactions: false,
	})
}
