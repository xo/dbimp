package snowflake //nolint:testpackage // The contract runs with no cancel, which only the package can set.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// head is the start of an answer of HTTP 200 with the columns cols, each a
// fixed of precision 10 and scale 0, up to the opening of data (measured). The
// partitions are the counts of rows that the metadata names, and none leaves
// the member out.
func head(partitions string, cols ...string) string {
	types := make([]string, len(cols))
	for i, col := range cols {
		types[i] = `{"name":"` + col + `","type":"fixed","precision":10,"scale":0,"nullable":true}`
	}
	part := ""
	if partitions != "" {
		part = `"partitionInfo":[` + partitions + `],`
	}
	return `{"resultSetMetaData":{"numRows":0,"format":"jsonv2",` + part + `"rowType":[` + strings.Join(types, ",") + `]},"data":[`
}

// tail is the end of an answer after its rows (measured).
const tail = `],"code":"090001","sqlState":"00000","statementHandle":"01c79a10-0001-ac43-0000-d07900024c1a","message":"Statement executed successfully."}`

// RunContract counts the goroutines of the process, so this test must not run
// in parallel with another. A fake server answers every request with one
// body, so the driver sends no cancel when a context ends (noStop). The tests
// of the cancel are in cancel_test.go.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return open(t, config(), url, false)
		},
		Query: "SELECT b, a, c FROM t",
		Columns: dbimptest.ColumnsCase{
			Body: head("", "B", "A", "C") + `["2","1","3"]` + tail,
			Want: []string{"B", "A", "C"},
		},
		Null: dbimptest.NullCase{
			Body:   head("", "A", "B") + `["1",null]` + tail,
			Column: 1,
		},
		// The only way that a result ends after some rows is a partition that
		// holds fewer rows than the metadata names (D183).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head(`{"rowCount":5}`, "A") + `["1"],["2"],["3"]` + tail,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head("", "A"),
			Row:  `["1"]`,
			Sep:  ",",
			Tail: tail,
		},
		Transactions: false,
	})
}
