package hive_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/hive"
)

// ok writes a TStatus of success as the field 1 of a result.
func ok(e *dbimp.ThriftEncoder) {
	e.Field(dbimp.ThriftStruct, 1)
	e.Field(dbimp.ThriftI32, 1)
	e.I32(0)
	e.Stop()
}

// failure writes a TStatus of error as the field 1 of a result.
func failure(e *dbimp.ThriftEncoder, state, msg string) {
	e.Field(dbimp.ThriftStruct, 1)
	e.Field(dbimp.ThriftI32, 1)
	e.I32(3)
	e.Field(dbimp.ThriftString, 3)
	e.String(state)
	e.Field(dbimp.ThriftI32, 4)
	e.I32(1)
	e.Field(dbimp.ThriftString, 5)
	e.String(msg)
	e.Stop()
}

// handle writes a handle identifier as the field id of the struct that is open.
func handle(e *dbimp.ThriftEncoder, id int16) {
	e.Field(dbimp.ThriftStruct, id)
	e.Field(dbimp.ThriftString, 1)
	e.Binary([]byte("0123456789abcdef"))
	e.Field(dbimp.ThriftString, 2)
	e.Binary([]byte("fedcba9876543210"))
	e.Stop()
}

// reply returns the message that answers the call name with a success status
// and the members that more writes.
func reply(name string, more func(*dbimp.ThriftEncoder)) dbimptest.ThriftStep {
	return dbimptest.ThriftStep{Call: name, Reply: dbimptest.ThriftReply(name, func(e *dbimp.ThriftEncoder) {
		ok(e)
		if more != nil {
			more(e)
		}
	})}
}

// columns is the metadata of a result of INT columns with the names.
func columns(names ...string) dbimptest.ThriftStep {
	return reply("GetResultSetMetadata", func(e *dbimp.ThriftEncoder) {
		e.Field(dbimp.ThriftStruct, 2)
		e.Field(dbimp.ThriftList, 1)
		e.List(dbimp.ThriftStruct, len(names))
		for i, n := range names {
			e.Field(dbimp.ThriftString, 1)
			e.String(n)
			e.Field(dbimp.ThriftStruct, 2)
			e.Field(dbimp.ThriftList, 1)
			e.List(dbimp.ThriftStruct, 1)
			e.Field(dbimp.ThriftStruct, 1)
			e.Field(dbimp.ThriftI32, 1)
			e.I32(3)
			e.Stop()
			e.Stop()
			e.Stop()
			e.Field(dbimp.ThriftI32, 3)
			e.I32(int32(i))
			e.Stop()
		}
		e.Stop()
		e.Stop()
	})
}

// page is a page of INT columns. vals[i] is the values of the column i, and
// nulls[i] is the bitmap of its NULLs, or nil for none.
func page(vals [][]int32, nulls [][]byte) dbimptest.ThriftStep {
	return reply("FetchResults", func(e *dbimp.ThriftEncoder) {
		e.Field(dbimp.ThriftBool, 2)
		e.Bool(false)
		e.Field(dbimp.ThriftStruct, 3)
		e.Field(dbimp.ThriftI64, 1)
		e.I64(0)
		e.Field(dbimp.ThriftList, 3)
		e.List(dbimp.ThriftStruct, len(vals))
		for i, col := range vals {
			e.Field(dbimp.ThriftStruct, 4)
			e.Field(dbimp.ThriftList, 1)
			e.List(dbimp.ThriftI32, len(col))
			for _, v := range col {
				e.I32(v)
			}
			e.Field(dbimp.ThriftString, 2)
			var nb []byte
			if nulls != nil {
				nb = nulls[i]
			}
			e.Binary(nb)
			e.Stop()
			e.Stop()
		}
		e.Stop()
		e.Stop()
	})
}

// start is the steps that start a statement with the columns names.
func start(names ...string) []dbimptest.ThriftStep {
	return []dbimptest.ThriftStep{
		reply("ExecuteStatement", func(e *dbimp.ThriftEncoder) {
			e.Field(dbimp.ThriftStruct, 2)
			handle(e, 1)
			e.Field(dbimp.ThriftI32, 2)
			e.I32(0)
			e.Field(dbimp.ThriftBool, 3)
			e.Bool(true)
			e.Stop()
		}),
		reply("GetOperationStatus", func(e *dbimp.ThriftEncoder) {
			e.Field(dbimp.ThriftI32, 2)
			e.I32(2)
		}),
		columns(names...),
	}
}

// RunContract counts the goroutines of the process, so this test must not run in
// parallel with another. A fake server answers each call from a script.
func TestContract(t *testing.T) { //nolint:paralleltest // RunContract counts the goroutines of the process.
	rowsOf := func(n int) [][]int32 {
		col := make([]int32, n)
		for i := range col {
			col[i] = int32(i)
		}
		return [][]int32{col}
	}
	steps := func(names []string, tail ...dbimptest.ThriftStep) []dbimptest.ThriftStep {
		return append(start(names...), tail...)
	}
	dbimptest.RunContract(t, dbimptest.Contract{
		Thrift: &dbimptest.ThriftContract{
			Open: func(t *testing.T, url string) *sql.DB {
				t.Helper()
				db, err := sql.Open(hive.Name, strings.Replace(url, "http://", "hive://u:p@", 1))
				if err != nil {
					t.Fatal(err)
				}
				return db
			},
			Query: "SELECT b, a, c FROM t",
			Common: []dbimptest.ThriftStep{
				reply("OpenSession", func(e *dbimp.ThriftEncoder) {
					e.Field(dbimp.ThriftI32, 2)
					e.I32(9)
					e.Field(dbimp.ThriftStruct, 3)
					handle(e, 1)
					e.Stop()
				}),
				reply("GetInfo", func(e *dbimp.ThriftEncoder) {
					e.Field(dbimp.ThriftStruct, 2)
					e.Field(dbimp.ThriftString, 1)
					e.String("Hive")
					e.Stop()
				}),
				reply("CloseOperation", nil),
				reply("CloseSession", nil),
				reply("CancelOperation", nil),
			},
			Columns: dbimptest.ThriftColumns{
				Steps: steps([]string{"b", "a", "c"}, page([][]int32{{2}, {1}, {3}}, nil)),
				Want:  []string{"b", "a", "c"},
			},
			Null: dbimptest.ThriftNull{
				Steps:  steps([]string{"a", "b"}, page([][]int32{{1}, {0}}, [][]byte{nil, {1}})),
				Column: 1,
			},
			// An error after the rows of a page arrives with the next page, as the
			// status of FetchResults.
			ErrorAfterRows: dbimptest.ThriftErrorCase{
				Steps: steps([]string{"a"}, page(rowsOf(3), nil), dbimptest.ThriftStep{Call: "FetchResults", Reply: dbimptest.ThriftReply("FetchResults", func(e *dbimp.ThriftEncoder) {
					failure(e, "HY000", "the server stopped")
				})}),
				Rows: 3,
			},
			Close:        steps([]string{"a"}, page(rowsOf(3), nil)),
			Cancel:       steps([]string{"a"}, page(rowsOf(1), nil), dbimptest.ThriftStep{Call: "FetchResults", Hold: true}),
			Drop:         "ExecuteStatement",
			Transactions: false,
		},
	})
}
