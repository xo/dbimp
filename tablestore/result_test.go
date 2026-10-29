package tablestore //nolint:testpackage // The tests read the columns and the values of the package.

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// decodeAll reads the answer body, and returns the value of each column of each
// row, decoded by the type of its column, and the response.
func decodeAll(t *testing.T, body []byte) ([][]any, *response) {
	t.Helper()
	r, err := readResponse(io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("reading the answer: %v", err)
	}
	var out [][]any
	if r.rs != nil {
		for i := range r.rs.count {
			row := make([]any, len(r.rs.cols))
			for j := range r.rs.cols {
				if row[j], err = r.rs.cols[j].decode(i); err != nil {
					t.Fatalf("decoding the row %d, column %d: %v", i, j, err)
				}
			}
			out = append(out, row)
		}
	}
	if err := r.finish(); err != nil {
		t.Fatalf("reading the end of the answer: %v", err)
	}
	return out, r
}

// TestDecodeEveryDataType decodes a message that has one column of each data
// type, with a NULL in each, and with the layout of the builder, which differs
// from the layout of the server. Each value has the Go type of its column.
func TestDecodeEveryDataType(t *testing.T) {
	t.Parallel()
	cols := []tcol{
		{name: "l", data: dataLong, nulls: []bool{false, true, false}, longs: []int64{-1 << 63, 0, 1<<63 - 1}},
		{name: "b", data: dataBoolean, nulls: []bool{false, false, true}, bools: []bool{true, false, false}},
		{name: "d", data: dataDouble, nulls: []bool{true, false, false}, doubles: []float64{0, 1.5, -2.25}},
		{name: "s", data: dataString, strs: []string{"a", "", "é😀"}},
		{name: "x", data: dataBinary, nulls: []bool{false, true, false}, bins: [][]byte{{0, 255}, nil, {}}},
		{name: "r", data: dataStringRLE, rleArray: []string{"p", "q"}, rleIndex: []int32{0, 0, 1}},
		{name: "dt", data: dataComplex, logic: logicDatetime, encode: dataLong, complex: true, longs: []int64{1793206923000000, 0, -1}},
		{name: "dd", data: dataComplex, logic: logicDate, encode: dataLong, complex: true, longs: []int64{1793232000, 0, -86400}},
		{name: "ts", data: dataComplex, logic: logicTimestamp, encode: dataLong, complex: true, longs: []int64{1793206923000000, 0, 1000000}},
	}
	got, r := decodeAll(t, answer(buildRows(cols, 3)))
	if r.statement != 1 || !r.hasAffected || r.affected != 0 || r.payload != payloadFlatBuffers {
		t.Errorf("the fields after the rows are %d %t %d %d", r.statement, r.hasAffected, r.affected, r.payload)
	}
	if len(got) != 3 {
		t.Fatalf("read %d rows, want 3", len(got))
	}
	for j, want := range [][]any{
		{int64(-1 << 63), nil, int64(1<<63 - 1)},
		{true, false, nil},
		{nil, 1.5, -2.25},
		{"a", "", "é😀"},
		{[]byte{0, 255}, nil, []byte{}},
		{"p", "p", "q"},
		{dbimp.LocalDateTimeOf(mustLocal(t, 1793206923000000)), dbimp.LocalDateTimeOf(mustLocal(t, 0)), dbimp.LocalDateTimeOf(mustLocal(t, -1))},
		{dbimp.Date{Year: 2026, Month: 10, Day: 29}, dbimp.Date{Year: 1970, Month: 1, Day: 1}, dbimp.Date{Year: 1969, Month: 12, Day: 31}},
		{mustInstant(1793206923000000), mustInstant(0), mustInstant(1000000)},
	} {
		for i := range got {
			if !reflect.DeepEqual(got[i][j], want[i]) {
				t.Errorf("column %s, row %d: %#v, want %#v", cols[j].name, i, got[i][j], want[i])
			}
		}
	}
}

// TestDecodeNoRows reads an answer that has no rows at all, such as the answer
// to DROP, and an answer whose rows have columns and no row (recorded: "a result
// with no rows").
func TestDecodeNoRows(t *testing.T) {
	t.Parallel()
	got, r := decodeAll(t, answerTail(nil, 5, 0))
	if len(got) != 0 || r.rs != nil {
		t.Errorf("an answer with no rows field gave %d rows", len(got))
	}
	got, r = decodeAll(t, answer(buildRows([]tcol{{name: "id", data: dataLong}}, 0)))
	if len(got) != 0 || len(r.rs.cols) != 1 || r.rs.cols[0].name != "id" {
		t.Errorf("a result with no rows gave %d rows and the columns %v", len(got), r.rs.cols)
	}
}

// TestDecodeRecordedColumns reads the columns of the recorded answers, and checks
// each one against the type that step 6 measured (recorded: "select the dates as
// int", "select every row", "select 1").
func TestDecodeRecordedColumns(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		n    int
		want string
	}{
		{8, "a:BIGINT b:MEDIUMTEXT"},
		{20, "id:BIGINT s:MEDIUMTEXT i:BIGINT d:DOUBLE b:BOOL x:MEDIUMBLOB"},
		{113, "id:BIGINT dt:DATETIME ts:TIMESTAMP d:DATE"},
		{199, "a:DATETIME"},
	} {
		ex := exchange(t, tt.n)
		r, err := readResponse(io.NopCloser(bytes.NewReader(ex.Response.Content())))
		if err != nil {
			t.Fatalf("exchange %d: %v", tt.n, err)
		}
		var got string
		for i := range r.rs.cols {
			if got != "" {
				got += " "
			}
			got += r.rs.cols[i].name + ":" + r.rs.cols[i].databaseType()
		}
		if got != tt.want {
			t.Errorf("exchange %d: the columns are %q, want %q", tt.n, got, tt.want)
		}
	}
}

// TestDecodeRejectsBrokenAnswers reads answers that a broken server can send.
// Each one is an error that wraps dbimp.ErrInvalidValue or io.ErrUnexpectedEOF,
// and none is a panic.
func TestDecodeRejectsBrokenAnswers(t *testing.T) {
	t.Parallel()
	good := answer(buildRows([]tcol{{name: "a", data: dataLong, longs: []int64{1, 2}}, {name: "s", data: dataString, strs: []string{"x", "y"}}}, 2))
	// A cut inside the field of the rows is an error. A cut after it only leaves out
	// the fields that follow, which an answer can leave out.
	for n := 1; n < len(good)-len(answerTail(nil, 1, 0)); n++ {
		body := good[:n]
		r, err := readResponse(io.NopCloser(bytes.NewReader(body)))
		if err != nil {
			continue
		}
		// A cut answer can read its columns, and then fail at a value or at the end.
		failed := r.finish() != nil
		if r.rs != nil {
			for i := range r.rs.count {
				for j := range r.rs.cols {
					if _, err := r.rs.cols[j].decode(i); err != nil {
						failed = true
					}
				}
			}
		}
		if !failed {
			t.Errorf("the answer cut at %d of %d bytes read without an error", n, len(good))
		}
	}
	for name, body := range map[string][]byte{
		"a length past the limit":  appendVarint(appendVarint(nil, fieldRows<<3|wireBytes), maxField+1),
		"a tag of field zero":      {0x00},
		"a varint that never ends": bytes.Repeat([]byte{0xff}, 12),
		"a row count too large":    answer(buildRows([]tcol{{name: "a", data: dataLong, longs: []int64{1}}}, 1<<30)),
	} {
		r, err := readResponse(io.NopCloser(bytes.NewReader(body)))
		if err == nil && r.rs != nil && r.rs.count > 0 {
			_, err = r.rs.cols[0].value(r.rs.count - 1)
		}
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if !errors.Is(err, dbimp.ErrInvalidValue) && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%s: the error is %v, want one that wraps dbimp.ErrInvalidValue or io.ErrUnexpectedEOF", name, err)
		}
	}
}

// mustLocal returns the wall clock of the instant of micros microseconds, in the
// zone of the server.
func mustLocal(t *testing.T, micros int64) time.Time {
	t.Helper()
	return time.UnixMicro(micros).In(serverZone)
}

// mustInstant returns the instant of micros microseconds, in UTC.
func mustInstant(micros int64) time.Time {
	return time.UnixMicro(micros).UTC()
}
