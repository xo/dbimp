package avatica //nolint:testpackage // The contract opens the driver through the fake transport of fake_test.go.

import (
	"database/sql"
	"maps"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// head is the start of an answer to prepareAndExecute with a column of
// INTEGER for each of cols, up to its rows, in the form of the server
// (measured).
func head(done bool, cols ...string) string {
	cs := make([]string, len(cols))
	for i, c := range cols {
		cs[i] = `{"ordinal":` + string(rune('0'+i)) + `,"nullable":1,"label":"` + c + `","columnName":"` + c + `","type":{"type":"scalar","id":4,"name":"INTEGER","rep":"PRIMITIVE_INT"}}`
	}
	d := "false"
	if done {
		d = "true"
	}
	return `{"response":"executeResults","missingStatement":false,"results":[{"response":"resultSet","connectionId":"c","statementId":1,"ownStatement":true,"signature":{"columns":[` +
		strings.Join(cs, ",") + `],"sql":null,"parameters":[],"cursorFactory":{"style":"LIST","clazz":null,"fieldNames":null},"statementType":null},"firstFrame":{"offset":0,"done":` + d + `,"rows":[`
}

// tail is the end of an answer after its rows.
const tail = `]},"updateCount":-1}]}`

// fetchError is the answer of the server to a fetch that fails, with HTTP
// 500 (measured).
const fetchError = `{"response":"error","exceptions":["java.sql.SQLDataException: data exception: division by zero"],"errorMessage":"Error -1 (00000) : Error while executing SQL \"...\": data exception: division by zero","errorCode":-1,"sqlState":"00000","severity":"ERROR"}`

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. A fake server answers every request that
// runs a statement with one body, and the fake transport answers the rest.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	answers := maps.Clone(setupAnswers)
	answers["fetch"] = fetchError
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			return openLocal(t, url, answers)
		},
		Query: "SELECT B, A, C FROM T",
		Columns: dbimptest.ColumnsCase{
			Body: head(true, "B", "A", "C") + `[2,1,3]` + tail,
			Want: []string{"B", "A", "C"},
		},
		Null: dbimptest.NullCase{
			Body:   head(true, "A", "B") + `[1,null]` + tail,
			Column: 1,
		},
		// The first frame holds some rows, and the fetch of the next one
		// fails (D157).
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: head(false, "A") + `[1],[2],[3]` + tail,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: head(true, "A"),
			Row:  `[1]`,
			Sep:  ",",
			Tail: tail,
		},
		Transactions: true,
	})
}
