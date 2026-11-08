package dbimp

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// The code in this file reads and writes the binary protocol of Apache
// Thrift, with the strict form of a message, for a driver whose server speaks
// it, such as Hive and Impala (D232). It reads one item at a time, so a driver
// never holds a whole result in memory (D25). The meaning of a field belongs
// to the driver, so this code reads and writes fields and gives them no
// meaning. A reader that finds a field that it does not know calls Skip.

// ThriftType is the type of a Thrift value on the wire.
type ThriftType byte

// The types of the binary protocol. ThriftStop ends a struct.
const (
	ThriftStop   ThriftType = 0
	ThriftBool   ThriftType = 2
	ThriftI8     ThriftType = 3
	ThriftDouble ThriftType = 4
	ThriftI16    ThriftType = 6
	ThriftI32    ThriftType = 8
	ThriftI64    ThriftType = 10
	ThriftString ThriftType = 11
	ThriftStruct ThriftType = 12
	ThriftMap    ThriftType = 13
	ThriftSet    ThriftType = 14
	ThriftList   ThriftType = 15
)

// ThriftMessageType is the type of a message.
type ThriftMessageType byte

// The types of a message.
const (
	// ThriftCall is a request that expects a reply.
	ThriftCall ThriftMessageType = 1
	// ThriftReply is the answer to a call.
	ThriftReply ThriftMessageType = 2
	// ThriftException is the answer to a call that the server could not
	// run. Its body is a struct with a message and a type.
	ThriftException ThriftMessageType = 3
	// ThriftOneway is a request that expects no reply.
	ThriftOneway ThriftMessageType = 4
)

// thriftVersion is the high 16 bits of the first word of a strict message.
const thriftVersion = 0x80010000

// maxThriftDepth is the deepest nesting of structs, lists and maps that the
// decoder skips. A deeper value is ErrInvalidValue, so a hostile response
// cannot exhaust the stack.
const maxThriftDepth = 64

// maxThriftCount is the largest count of elements in a list, a set or a map
// that the decoder accepts. A count over it is ErrInvalidValue.
const maxThriftCount = 1 << 28

// thriftChunk is the most that the decoder allocates at once for a string. A
// string that claims a larger length grows as its bytes arrive, so a length
// that lies costs no memory.
const thriftChunk = 64 << 10

// ThriftDecoder reads Thrift values from a reader, one at a time.
type ThriftDecoder struct {
	r   *bufio.Reader
	buf [8]byte
}

// NewThriftDecoder returns a ThriftDecoder that reads from r.
func NewThriftDecoder(r io.Reader) *ThriftDecoder {
	return &ThriftDecoder{r: bufio.NewReader(r)}
}

// ReadMessage reads the head of a strict message: its name, its type and its
// sequence number.
func (d *ThriftDecoder) ReadMessage() (string, ThriftMessageType, int32, error) {
	v, err := d.ReadI32()
	if err != nil {
		return "", 0, 0, err
	}
	if uint32(v)&0xffff0000 != thriftVersion { //nolint:gosec // G115: the bits are the version.
		return "", 0, 0, fmt.Errorf("reading the version %#x of a message: %w", uint32(v), ErrInvalidValue) //nolint:gosec // G115: the bits are the version.
	}
	name, err := d.ReadString()
	if err != nil {
		return "", 0, 0, err
	}
	seq, err := d.ReadI32()
	if err != nil {
		return "", 0, 0, err
	}
	return name, ThriftMessageType(v & 0xff), seq, nil
}

// ReadField reads the head of a field of a struct. It returns ThriftStop at
// the end of the struct, and then the id is 0.
func (d *ThriftDecoder) ReadField() (ThriftType, int16, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return 0, 0, unexpected(err)
	}
	if ThriftType(b) == ThriftStop {
		return ThriftStop, 0, nil
	}
	id, err := d.ReadI16()
	if err != nil {
		return 0, 0, err
	}
	return ThriftType(b), id, nil
}

// ReadBool reads a bool.
func (d *ThriftDecoder) ReadBool() (bool, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return false, unexpected(err)
	}
	return b != 0, nil
}

// ReadI8 reads a byte as an int8.
func (d *ThriftDecoder) ReadI8() (int8, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return 0, unexpected(err)
	}
	return int8(b), nil //nolint:gosec // G115: Thrift names the byte as signed.
}

// ReadI16 reads an i16.
func (d *ThriftDecoder) ReadI16() (int16, error) {
	if _, err := io.ReadFull(d.r, d.buf[:2]); err != nil {
		return 0, unexpected(err)
	}
	return int16(binary.BigEndian.Uint16(d.buf[:2])), nil //nolint:gosec // G115: the bits are the value.
}

// ReadI32 reads an i32.
func (d *ThriftDecoder) ReadI32() (int32, error) {
	if _, err := io.ReadFull(d.r, d.buf[:4]); err != nil {
		return 0, unexpected(err)
	}
	return int32(binary.BigEndian.Uint32(d.buf[:4])), nil //nolint:gosec // G115: the bits are the value.
}

// ReadI64 reads an i64.
func (d *ThriftDecoder) ReadI64() (int64, error) {
	if _, err := io.ReadFull(d.r, d.buf[:8]); err != nil {
		return 0, unexpected(err)
	}
	return int64(binary.BigEndian.Uint64(d.buf[:8])), nil //nolint:gosec // G115: the bits are the value.
}

// ReadDouble reads a double.
func (d *ThriftDecoder) ReadDouble() (float64, error) {
	if _, err := io.ReadFull(d.r, d.buf[:8]); err != nil {
		return 0, unexpected(err)
	}
	return math.Float64frombits(binary.BigEndian.Uint64(d.buf[:8])), nil
}

// ReadBinary reads a string of bytes.
func (d *ThriftDecoder) ReadBinary() ([]byte, error) {
	n, err := d.ReadI32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("reading a string of length %d: %w", n, ErrInvalidValue)
	}
	return d.readN(nil, uint64(n))
}

// ReadString reads a string of text.
func (d *ThriftDecoder) ReadString() (string, error) {
	b, err := d.ReadBinary()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ReadList reads the head of a list or a set. It returns the type of the
// elements and their count.
func (d *ThriftDecoder) ReadList() (ThriftType, int, error) {
	b, err := d.r.ReadByte()
	if err != nil {
		return 0, 0, unexpected(err)
	}
	n, err := d.ReadI32()
	if err != nil {
		return 0, 0, err
	}
	if n < 0 || n > maxThriftCount {
		return 0, 0, fmt.Errorf("reading a list of %d elements: %w", n, ErrInvalidValue)
	}
	return ThriftType(b), int(n), nil
}

// ReadMap reads the head of a map. It returns the types of the keys and of the
// values, and the count of pairs.
func (d *ThriftDecoder) ReadMap() (ThriftType, ThriftType, int, error) {
	k, err := d.r.ReadByte()
	if err != nil {
		return 0, 0, 0, unexpected(err)
	}
	v, err := d.r.ReadByte()
	if err != nil {
		return 0, 0, 0, unexpected(err)
	}
	n, err := d.ReadI32()
	if err != nil {
		return 0, 0, 0, err
	}
	if n < 0 || n > maxThriftCount {
		return 0, 0, 0, fmt.Errorf("reading a map of %d pairs: %w", n, ErrInvalidValue)
	}
	return ThriftType(k), ThriftType(v), int(n), nil
}

// Skip reads and drops one value of type t.
func (d *ThriftDecoder) Skip(t ThriftType) error {
	return d.skip(t, 0)
}

func (d *ThriftDecoder) skip(t ThriftType, depth int) error {
	if depth > maxThriftDepth {
		return fmt.Errorf("skipping a value nested deeper than %d: %w", maxThriftDepth, ErrInvalidValue)
	}
	var err error
	switch t {
	case ThriftBool, ThriftI8:
		_, err = d.r.ReadByte()
		err = unexpected(err)
	case ThriftI16:
		_, err = d.ReadI16()
	case ThriftI32:
		_, err = d.ReadI32()
	case ThriftI64, ThriftDouble:
		_, err = d.ReadI64()
	case ThriftString:
		_, err = d.ReadBinary()
	case ThriftStruct:
		for {
			ft, _, err := d.ReadField()
			if err != nil {
				return err
			}
			if ft == ThriftStop {
				return nil
			}
			if err := d.skip(ft, depth+1); err != nil {
				return err
			}
		}
	case ThriftMap:
		k, v, n, err := d.ReadMap()
		if err != nil {
			return err
		}
		for range n {
			if err := d.skip(k, depth+1); err != nil {
				return err
			}
			if err := d.skip(v, depth+1); err != nil {
				return err
			}
		}
	case ThriftList, ThriftSet:
		et, n, err := d.ReadList()
		if err != nil {
			return err
		}
		for range n {
			if err := d.skip(et, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("skipping a value of type %d: %w", t, ErrInvalidValue)
	}
	return err
}

// readN reads n bytes. A length that is large grows the result a chunk at a
// time, so that a length that lies costs no memory.
func (d *ThriftDecoder) readN(dst []byte, n uint64) ([]byte, error) {
	for n > 0 {
		c := min(n, thriftChunk)
		start := len(dst)
		dst = append(dst, make([]byte, c)...)
		if _, err := io.ReadFull(d.r, dst[start:]); err != nil {
			return nil, unexpected(err)
		}
		n -= c
	}
	if dst == nil {
		dst = []byte{}
	}
	return dst, nil
}

// ThriftEncoder writes Thrift values into a buffer.
type ThriftEncoder struct {
	b []byte
}

// Bytes returns what the encoder wrote.
func (e *ThriftEncoder) Bytes() []byte {
	return e.b
}

// Message writes the head of a strict message.
func (e *ThriftEncoder) Message(name string, t ThriftMessageType, seq int32) {
	e.I32(int32(thriftVersion | uint32(t))) //nolint:gosec // G115: the bits are the version.
	e.String(name)
	e.I32(seq)
}

// Field writes the head of a field of a struct.
func (e *ThriftEncoder) Field(t ThriftType, id int16) {
	e.b = append(e.b, byte(t))
	e.I16(id)
}

// Stop ends a struct.
func (e *ThriftEncoder) Stop() {
	e.b = append(e.b, byte(ThriftStop))
}

// Bool writes a bool.
func (e *ThriftEncoder) Bool(v bool) {
	if v {
		e.b = append(e.b, 1)
		return
	}
	e.b = append(e.b, 0)
}

// I8 writes an int8.
func (e *ThriftEncoder) I8(v int8) {
	e.b = append(e.b, byte(v)) //nolint:gosec // G115: the bits are the value.
}

// I16 writes an i16.
func (e *ThriftEncoder) I16(v int16) {
	e.b = binary.BigEndian.AppendUint16(e.b, uint16(v)) //nolint:gosec // G115: the bits are the value.
}

// I32 writes an i32.
func (e *ThriftEncoder) I32(v int32) {
	e.b = binary.BigEndian.AppendUint32(e.b, uint32(v)) //nolint:gosec // G115: the bits are the value.
}

// I64 writes an i64.
func (e *ThriftEncoder) I64(v int64) {
	e.b = binary.BigEndian.AppendUint64(e.b, uint64(v)) //nolint:gosec // G115: the bits are the value.
}

// Double writes a double.
func (e *ThriftEncoder) Double(v float64) {
	e.b = binary.BigEndian.AppendUint64(e.b, math.Float64bits(v))
}

// Binary writes a string of bytes.
func (e *ThriftEncoder) Binary(v []byte) {
	e.I32(int32(len(v))) //nolint:gosec // G115: a request is never 2 GiB.
	e.b = append(e.b, v...)
}

// String writes a string of text.
func (e *ThriftEncoder) String(v string) {
	e.I32(int32(len(v))) //nolint:gosec // G115: a request is never 2 GiB.
	e.b = append(e.b, v...)
}

// List writes the head of a list of n elements of type t.
func (e *ThriftEncoder) List(t ThriftType, n int) {
	e.b = append(e.b, byte(t))
	e.I32(int32(n)) //nolint:gosec // G115: a request is never 2 GiB.
}

// Map writes the head of a map of n pairs.
func (e *ThriftEncoder) Map(k, v ThriftType, n int) {
	e.b = append(e.b, byte(k), byte(v))
	e.I32(int32(n)) //nolint:gosec // G115: a request is never 2 GiB.
}
