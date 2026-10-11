package exasol_test

import (
	"database/sql"
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// result returns the answer of execute for a result set of the columns cols,
// which are all DECIMAL(9,0), with the rows that data holds, as columns of
// values. A handle of 0 leaves the handle out. numRows is the number of rows
// of the whole result.
func result(handle, numRows int, cols []string, data string) dbimptest.WebSocketFrame {
	defs := make([]string, len(cols))
	for i, c := range cols {
		defs[i] = `{"name":"` + c + `","dataType":{"type":"DECIMAL","precision":9,"scale":0}}`
	}
	cs := strings.Join(defs, ",")
	h, inMessage := "", numRows
	if handle != 0 {
		h = fmt.Sprintf(`"resultSetHandle":%d,`, handle)
		inMessage = 0
	}
	d := ""
	if data != "" {
		d = `,"data":` + data
		var columns [][]any
		if err := json.Unmarshal([]byte(data), &columns); err == nil && len(columns) > 0 && handle != 0 {
			inMessage = len(columns[0])
		}
	}
	return text(fmt.Sprintf(`{"status":"ok","responseData":{"results":[{"resultType":"resultSet","resultSet":{%s"numColumns":%d,"numRows":%d,"numRowsInMessage":%d,"columns":[%s]%s}}],"numResults":1}}`,
		h, len(cols), numRows, inMessage, cs, d))
}

// failure returns the answer for an error of the server.
func failure(code, message string) dbimptest.WebSocketFrame {
	return text(`{"status":"error","exception":{"text":"` + message + ` (Session: 1)","sqlCode":"` + code + `"}}`)
}

var ok = text(`{"status":"ok"}`)

// RunContract counts the goroutines of the process, so this test must not run
// in parallel with another. A scripted server answers each frame of a
// connection with its next step.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		WebSocket: &dbimptest.WebSocketContract{
			Open: func(t *testing.T, addr string) *sql.DB {
				t.Helper()
				return open(t, addr, "")
			},
			Query: "SELECT B, A, C FROM T",
			Login: loginSteps(t),
			Columns: dbimptest.WebSocketColumns{
				Steps: []dbimptest.WebSocketStep{{Reply: result(0, 1, []string{"B", "A", "C"}, "[[2],[1],[3]]")}},
				Want:  []string{"B", "A", "C"},
			},
			Null: dbimptest.WebSocketNull{
				Steps:  []dbimptest.WebSocketStep{{Reply: result(0, 1, []string{"A", "B"}, "[[1],[null]]")}},
				Column: 1,
			},
			// The server computes a whole result before it answers, so an error
			// comes with a fetch (recorded: "error in a late row of a large
			// result"), and the driver closes the handle after it.
			ErrorAfterRows: dbimptest.WebSocketErrorCase{
				Steps: []dbimptest.WebSocketStep{
					{Reply: result(7, 6, []string{"A"}, "[[1,2,3]]")},
					{Reply: failure("42000", "Resultset handle 7 is unknown")},
					{Reply: ok},
				},
				Rows: 3,
			},
			Close: []dbimptest.WebSocketStep{
				{Reply: result(7, 100, []string{"A"}, "[[1,2]]")},
				{Reply: ok},
			},
			Cancel: []dbimptest.WebSocketStep{
				{Reply: result(7, 100, []string{"A"}, "[[1]]")},
				{Hold: true, Reply: failure("R0003", "Client requested execution abort.")},
				{Reply: ok},
			},
			Transactions: true,
		},
	})
}
