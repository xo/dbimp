package tablestore

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/xo/dbimp"
)

// The wire types of protobuf that the driver reads and writes. A message of
// Tablestore uses varints, and fields of bytes, which hold text, bytes and
// messages (D222).
const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

// maxField is the most bytes that the driver accepts for one field of a
// message. It bounds what a broken or hostile server can ask the driver to
// read before the first row.
const maxField = 1 << 30

// The numbers of the fields of the messages that the driver uses
// (otsprotocol/table_store.proto of the SDK). SQLQueryRequest holds the text
// of the statement, the payload version, the version of the SQL and a search
// token. SQLQueryResponse holds the consumed capacity, the rows, the payload
// version, the type of the statement, the consumed capacity of a search, the
// count of the rows that the statement changed and a search token. Error holds
// a code and a text.
const (
	fieldQuery       = 1
	fieldVersion     = 2
	fieldSQLVersion  = 3
	fieldSearchToken = 5

	fieldRows      = 2
	fieldPayload   = 3
	fieldStatement = 4
	fieldAffected  = 6
	fieldNextToken = 8

	fieldErrorCode    = 1
	fieldErrorMessage = 2
)

// payloadFlatBuffers is the payload version that asks for the rows as
// FlatBuffers (D223).
const payloadFlatBuffers = 2

// appendVarint appends v as a varint.
func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

// appendString appends the field number field, holding s.
func appendString(b []byte, field int, s string) []byte {
	b = appendVarint(b, uint64(field)<<3|wireBytes) //nolint:gosec // G115: a field number is positive.
	b = appendVarint(b, uint64(len(s)))
	return append(b, s...)
}

// appendBytes appends the field number field, holding b.
func appendBytes(buf []byte, field int, b []byte) []byte {
	buf = appendVarint(buf, uint64(field)<<3|wireBytes) //nolint:gosec // G115: a field number is positive.
	buf = appendVarint(buf, uint64(len(b)))
	return append(buf, b...)
}

// appendInt appends the field number field, holding v.
func appendInt(b []byte, field int, v int64) []byte {
	b = appendVarint(b, uint64(field)<<3|wireVarint) //nolint:gosec // G115: a field number is positive.
	return appendVarint(b, uint64(v))                //nolint:gosec // G115: protobuf writes a negative int64 as its two's complement.
}

// pbReader reads the fields of one protobuf message from a stream.
type pbReader struct {
	r *bufio.Reader
}

// newPBReader returns a reader for r.
func newPBReader(r io.Reader) pbReader {
	return pbReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// invalid returns an error for a message that is not protobuf.
func invalid(what string, err error) error {
	if err == nil {
		return fmt.Errorf("reading %s: %w", what, dbimp.ErrInvalidValue)
	}
	return fmt.Errorf("reading %s: %w", what, err)
}

// failed wraps err for a read of what, and returns nil for no error.
func failed(what string, err error) error {
	if err == nil {
		return nil
	}
	return invalid(what, unexpected(err))
}

// tag reads the number and the wire type of the next field. At the end of the
// message it returns io.EOF.
func (p pbReader) tag() (int, int, error) {
	v, err := p.varint()
	if err != nil {
		return 0, 0, err
	}
	if v>>3 == 0 || v>>3 > 1<<29 {
		return 0, 0, invalid("a field of the answer", nil)
	}
	return int(v >> 3), int(v & 7), nil
}

// varint reads a varint. It returns io.EOF only when the stream ends before
// its first byte.
func (p pbReader) varint() (uint64, error) {
	var v uint64
	for shift := uint(0); shift < 70; shift += 7 {
		c, err := p.r.ReadByte()
		if err != nil {
			if shift > 0 && errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return 0, err //nolint:wrapcheck // The callers wrap the error, and io.EOF must stay as it is.
		}
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, nil
		}
	}
	return 0, invalid("a varint of the answer", nil)
}

// length reads the length of a field of bytes.
func (p pbReader) length() (int, error) {
	n, err := p.varint()
	if err != nil {
		return 0, invalid("a length of the answer", unexpected(err))
	}
	if n > maxField {
		return 0, invalid("a length of the answer", nil)
	}
	return int(n), nil
}

// unexpected turns io.EOF into io.ErrUnexpectedEOF, because a message that
// ends inside a field is cut.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// skip reads and drops the value of a field of the wire type wire.
func (p pbReader) skip(wire int) error {
	switch wire {
	case wireVarint:
		_, err := p.varint()
		return err
	case wireFixed64:
		return p.discard(8)
	case wireFixed32:
		return p.discard(4)
	case wireBytes:
		n, err := p.length()
		if err != nil {
			return err
		}
		return p.discard(n)
	}
	return invalid("a field of the answer", nil)
}

// discard reads and drops n bytes.
func (p pbReader) discard(n int) error {
	if _, err := io.CopyN(io.Discard, p.r, int64(n)); err != nil {
		return invalid("the answer", unexpected(err))
	}
	return nil
}

// readBytes reads a field of bytes whose length is n.
func (p pbReader) readBytes(n int) ([]byte, error) {
	b := make([]byte, 0, min(n, 64<<10))
	for len(b) < n {
		chunk := min(n-len(b), 64<<10)
		b = append(b, make([]byte, chunk)...)
		if _, err := io.ReadFull(p.r, b[len(b)-chunk:]); err != nil {
			return nil, invalid("the answer", unexpected(err))
		}
	}
	return b, nil
}

// parseError reads the code and the text of an Error message. A body that is
// not protobuf gives empty strings and false.
func parseError(body []byte) (string, string, bool) {
	p := newPBReader(bytes.NewReader(body))
	var code, text string
	for {
		field, wire, err := p.tag()
		if err != nil {
			return code, text, errors.Is(err, io.EOF) && (code != "" || text != "")
		}
		if wire != wireBytes || (field != fieldErrorCode && field != fieldErrorMessage) {
			if p.skip(wire) != nil {
				return "", "", false
			}
			continue
		}
		n, err := p.length()
		if err != nil {
			return "", "", false
		}
		b, err := p.readBytes(n)
		if err != nil {
			return "", "", false
		}
		if field == fieldErrorCode {
			code = string(b)
		} else {
			text = string(b)
		}
	}
}
