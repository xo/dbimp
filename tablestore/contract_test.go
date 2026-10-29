package tablestore //nolint:testpackage // The contract builds a connector for the fake server, which only the package can do.

import (
	"bytes"
	"database/sql"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

// streamStrings builds a result of one column of n strings of 1019 bytes each,
// which take 1024 bytes in the message, and returns the start of the answer up
// to the first string, one string, and the end of the answer. The answer is the
// head, then the string n times, then the tail, so that the contract can stream
// it. The check below makes sure that the three parts make the message.
func streamStrings(t *testing.T, n int) (string, string, string) {
	t.Helper()
	text := string(bytes.Repeat([]byte("x"), 1019))
	strs := make([]string, n)
	for i := range strs {
		strs[i] = text
	}
	msg := buildRows([]tcol{{name: "s", data: dataString, strs: strs}}, n)
	first := len(msg) - n*1024
	prefix := appendVarint(nil, fieldRows<<3|wireBytes)
	prefix = appendVarint(prefix, uint64(len(msg)))
	head := string(prefix) + string(msg[:first])
	row := string(msg[first : first+1024])
	tail := string(answerTail(nil, 1, 0))
	if got := head + string(bytes.Repeat([]byte(row), n)) + tail; got != string(answer(msg)) {
		t.Fatal("the head, the strings and the tail do not make the answer")
	}
	return head, row, tail
}

// TestContract runs the contract of every driver against fake servers that answer
// with bodies in the form of the answer of Tablestore: a protobuf message that
// holds FlatBuffers rows. RunContract counts the goroutines of the process, so
// this test must not run in parallel with another. The tests of the cancel of a
// statement and of the version are in query_test.go.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	const stream = (64 << 20) / 1024
	columns := string(answer(buildRows([]tcol{
		{name: "b", data: dataLong, longs: []int64{2}},
		{name: "a", data: dataLong, longs: []int64{1}},
		{name: "c", data: dataLong, longs: []int64{3}},
	}, 1)))
	null := string(answer(buildRows([]tcol{
		{name: "a", data: dataLong, longs: []int64{1}},
		{name: "b", data: dataLong, nulls: []bool{true}, longs: []int64{0}},
	}, 1)))
	// A body that ends early: the rows are ten strings, and the body stops
	// after the third one, because a statement of Tablestore answers in one
	// piece and the only way to fail after a row is a cut answer.
	strs := make([]string, 10)
	for i := range strs {
		strs[i] = "xxx"
	}
	msg := buildRows([]tcol{{name: "s", data: dataString, strs: strs}}, 10)
	prefix := appendVarint(appendVarint(nil, fieldRows<<3|wireBytes), uint64(len(msg)))
	cut := string(prefix) + string(msg[:len(msg)-7*8])
	head, row, tail := streamStrings(t, stream)
	dbimptest.RunContract(t, dbimptest.Contract{
		Open: func(t *testing.T, url string) *sql.DB {
			t.Helper()
			c := connectorAt(t, testConfig(), url)
			db := sql.OpenDB(c)
			t.Cleanup(func() { _ = c.Close() })
			return db
		},
		Query:       "SELECT b, a, c FROM t",
		ContentType: "application/x-protobuf",
		Columns:     dbimptest.ColumnsCase{Body: columns, Want: []string{"b", "a", "c"}},
		Null:        dbimptest.NullCase{Body: null, Column: 1},
		ErrorAfterRows: dbimptest.ErrorCase{
			Body: cut,
			Rows: 3,
		},
		Stream:       dbimptest.StreamCase{Head: head, Row: row, Sep: "", Tail: tail},
		Transactions: false,
	})
}
