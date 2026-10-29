package tablestore //nolint:testpackage // The fuzz test reads the answer with the reader of the package.

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// FuzzReadResponse reads an answer that a server can send, and decodes every
// value of it. The reader must return an error for a broken answer, and never
// panic or allocate for a size that the answer does not hold.
func FuzzReadResponse(f *testing.F) {
	paths, err := filepath.Glob(testdata + "/tablestore-*.json")
	if err != nil {
		f.Fatal(err)
	}
	for _, path := range paths {
		ex, err := dbimptest.ReadExchange(path)
		if err != nil {
			f.Fatal(err)
		}
		if ex.Response.Status == 200 {
			f.Add(ex.Response.Content())
		}
	}
	f.Add(answer(buildRows([]tcol{
		{name: "l", data: dataLong, nulls: []bool{false, true}, longs: []int64{1, 2}},
		{name: "x", data: dataBinary, bins: [][]byte{{1}, {2}}},
		{name: "r", data: dataStringRLE, rleArray: []string{"p"}, rleIndex: []int32{0, 0}},
		{name: "t", data: dataComplex, logic: logicTimestamp, encode: dataLong, complex: true, longs: []int64{1, 2}},
	}, 2)))
	f.Fuzz(func(t *testing.T, body []byte) {
		r, err := readResponse(io.NopCloser(bytes.NewReader(body)))
		if err != nil {
			return
		}
		if r.rs != nil {
			for i := range min(r.rs.count, 64) {
				for j := range r.rs.cols {
					_, _ = r.rs.cols[j].decode(i)
				}
			}
		}
		_ = r.finish()
	})
}

// FuzzParseError reads the body of an error response, which can be protobuf, XML
// or any text, and returns an error value for each.
func FuzzParseError(f *testing.F) {
	f.Add([]byte("\n\x13OTSParameterInvalid\x12\x05hello"))
	f.Add([]byte("<?xml version=\"1.0\"?><Error><Code>C</Code><Message>M</Message></Error>"))
	f.Add([]byte("bad gateway"))
	f.Fuzz(func(t *testing.T, body []byte) {
		e := newError(&dbimp.StatusError{Code: 400, Body: string(body)})
		if e.Error() == "" || e.HTTPStatus != 400 {
			t.Fatal("an error with no text or no status")
		}
	})
}
