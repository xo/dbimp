package influxdb_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/influxdb"
)

// series writes one answer of InfluxQL with one series, whose rows are
// values, and whose series holds "partial":true when partial is true.
func series(cols, values string, partial bool) string {
	tail := "}]}]}"
	if partial {
		tail = `,"partial":true}],"partial":true}]}`
	}
	return `{"results":[{"statement_id":0,"series":[{"name":"m","columns":` + cols + `,"values":` + values + tail
}

// RunContract counts the goroutines of the process, so this test must not
// run in parallel with another. The contract runs InfluxQL on InfluxDB 1,
// with sqlmode=disable, because a fake server answers every request with one
// body, and SQL sends DESCRIBE first (D80). The TestReplaySQL tests hold SQL.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			db, err := sql.Open(influxdb.Name, strings.Replace(url, "http://", "influxdb://", 1)+"/db?sqlmode=disable&version=1")
			if err != nil {
				t.Fatal(err)
			}
			return db
		},
		Query: "SELECT * FROM m",
		Columns: dbimptest.ColumnsCase{
			Body: series(`["b","a","c"]`, `[[2,1,3]]`, false),
			Want: []string{"measurement", "b", "a", "c"},
		},
		Null: dbimptest.NullCase{
			Body:   series(`["a","b"]`, `[[1,null]]`, false),
			Column: 2,
		},
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: series(`["y"]`, `[[1],[1],[1]]`, true) + "\n" +
				`{"results":[{"statement_id":0,"error":"a failure after the first chunk"}]}`,
			Rows: 3,
		},
		Stream: dbimptest.StreamCase{
			Head: `{"results":[{"statement_id":0,"series":[{"name":"m","columns":["a"],"values":[`,
			Row:  `[1]`,
			Sep:  `,`,
			Tail: `]}]}]}`,
		},
	})
}
