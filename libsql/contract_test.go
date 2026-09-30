package libsql_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/libsql"
)

// head is the start of an answer of a cursor with the columns cols, up to
// its rows, in the form of the server (measured). Its baton is null, so the
// driver sends no close after it.
func head(cols ...string) string {
	cs := make([]string, len(cols))
	for i, c := range cols {
		cs[i] = `{"name":"` + c + `","decltype":"INTEGER"}`
	}
	return "{\"baton\":null,\"base_url\":null}\n{\"type\":\"step_begin\",\"step\":0,\"cols\":[" + strings.Join(cs, ",") + "]}\n"
}

// tail is the end of an answer with no error.
const tail = "{\"type\":\"step_end\",\"affected_row_count\":0,\"last_insert_rowid\":null}\n{\"type\":\"replication_index\",\"replication_index\":1}\n"

// row returns a row of integers.
func row(vals ...string) string {
	vs := make([]string, len(vals))
	for i, v := range vals {
		if v == "null" {
			vs[i] = `{"type":"null"}`
			continue
		}
		vs[i] = `{"type":"integer","value":"` + v + `"}`
	}
	return `{"type":"row","row":[` + strings.Join(vs, ",") + "]}\n"
}

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request with one
// body.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(libsql.Name, strings.Replace(url, "http://", "libsql://", 1)+"?tls=false")
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("b", "a", "c") + row("2", "1", "3") + tail,
			Want: []string{"b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   head("a", "b") + row("1", "null") + tail,
			Column: 1,
		},
		// The cursor sends the rows before an error, then step_error
		// (measured).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head("a") + row("1") + row("1") + row("1") + "{\"type\":\"step_error\",\"step\":0,\"error\":{\"message\":\"SQLite error: integer overflow\",\"code\":\"SQLITE_UNKNOWN\"}}\n",
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("a"),
			Row:  strings.TrimSuffix(row("1"), "\n"),
			Sep:  "\n",
			Tail: "\n" + tail,
		},
		Transactions: true,
		ContentType:  "text/plain",
	})
}
