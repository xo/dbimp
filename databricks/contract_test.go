package databricks //nolint:testpackage // The contract opens a connector on a fake server, which only the package can do.

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// arrayStart is the text of the answer that opens the rows (measured).
const arrayStart = `"data_array":[`

// split cuts the recorded answer n at the start of its rows. The head ends with
// the opening of data_array, and the tail starts with the end of data_array.
func split(tb testing.TB, n int) (string, string) {
	tb.Helper()
	body := string(exchange(tb, n).Response.Content())
	i := strings.Index(body, arrayStart)
	j := strings.LastIndex(body, "]")
	if i < 0 || j < i {
		tb.Fatalf("the recorded answer %d has no rows", n)
	}
	i += len(arrayStart)
	// The tail is the end of data_array and the closing of result and of the
	// answer.
	rest := body[j:]
	return body[:i], rest
}

// RunContract counts the goroutines of the process, so this test must not run in
// parallel with another. The answers are the recorded answers of the server, cut
// where the rows start, so that the driver decodes the real form (step 11).
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	// The answer 136 holds 3000 rows of the columns id and s. The contract
	// changes row_count, which the driver checks at the end of the rows.
	head, tail := split(t, 136)
	threeRows := strings.ReplaceAll(head, `"row_count":3000`, `"row_count":3`)
	short := strings.ReplaceAll(head, `"row_count":3000`, `"row_count":5`)
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return open(t, recordedConfig(), url)
		},
		Query: "SELECT z, y, x, b, a",
		Columns: dbimptest.ColumnsCase{
			Body: string(exchange(t, 57).Response.Content()),
			Want: []string{"z", "y", "x", "b", "a"},
		},
		Null: dbimptest.NullCase{
			Body:   string(exchange(t, 92).Response.Content()),
			Column: 1,
		},
		// A result that holds fewer rows than row_count names ends after some
		// rows with an error (D21).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: short + `["1","row-1"],["2","row-2"],["3","row-3"]` + tail,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: threeRows,
			Row:  `["1","row-1"]`,
			Sep:  ",",
			Tail: tail,
		},
		Transactions: false,
	})
}
