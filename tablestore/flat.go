package tablestore

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/xo/dbimp"
)

// buffer is a FlatBuffers message that arrives from a stream. The driver reads
// it when a read asks for the bytes, so a caller that closes the rows early
// leaves the rest of the stream unread (D36). It keeps the bytes that it read,
// because FlatBuffers refers to any place in the message.
type buffer struct {
	r    io.Reader
	data []byte
	size int
}

// newBuffer returns a buffer for a message of size bytes that r holds.
func newBuffer(r io.Reader, size int) *buffer {
	return &buffer{r: r, size: size}
}

// bytes returns the n bytes at pos. It reads from the stream up to the end of
// them, and it fails for a place outside the message.
func (b *buffer) bytes(pos, n int) ([]byte, error) {
	if pos < 0 || n < 0 || pos > b.size || n > b.size-pos {
		return nil, invalid("a value of the rows", nil)
	}
	end := pos + n
	for len(b.data) < end {
		chunk := min(end-len(b.data), 1<<20)
		old := len(b.data)
		b.data = append(b.data, make([]byte, chunk)...)
		if _, err := io.ReadFull(b.r, b.data[old:]); err != nil {
			b.data = b.data[:old]
			return nil, invalid("the rows", unexpected(err))
		}
	}
	return b.data[pos:end], nil
}

// drain reads and drops what the driver did not read of the message, so that
// the fields that follow it can be read.
func (b *buffer) drain() error {
	rest := int64(b.size - len(b.data))
	if rest <= 0 {
		return nil
	}
	if _, err := io.CopyN(io.Discard, b.r, rest); err != nil {
		return invalid("the rows", unexpected(err))
	}
	b.size = len(b.data)
	return nil
}

func (b *buffer) u8(pos int) (uint8, error) {
	v, err := b.bytes(pos, 1)
	if err != nil {
		return 0, err
	}
	return v[0], nil
}

func (b *buffer) u16(pos int) (int, error) {
	v, err := b.bytes(pos, 2)
	if err != nil {
		return 0, err
	}
	return int(binary.LittleEndian.Uint16(v)), nil
}

func (b *buffer) u32(pos int) (int, error) {
	v, err := b.bytes(pos, 4)
	if err != nil {
		return 0, err
	}
	return int(binary.LittleEndian.Uint32(v)), nil
}

func (b *buffer) i32(pos int) (int, error) {
	v, err := b.bytes(pos, 4)
	if err != nil {
		return 0, err
	}
	return int(int32(binary.LittleEndian.Uint32(v))), nil //nolint:gosec // G115: the type of the field is a signed 32 bit integer.
}

func (b *buffer) i64(pos int) (int64, error) {
	v, err := b.bytes(pos, 8)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(v)), nil //nolint:gosec // G115: the type of the field is a signed 64 bit integer.
}

func (b *buffer) f64(pos int) (float64, error) {
	v, err := b.i64(pos)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(uint64(v)), nil //nolint:gosec // G115: the bits of a double are the bits of the integer.
}

// table is a table of a FlatBuffers message, at the position pos. A table
// starts with the offset of its vtable, and the vtable holds the offset of
// each field, or 0 for a field that the message leaves out.
type table struct {
	b   *buffer
	pos int
}

// root returns the root table of the message.
func (b *buffer) root() (table, error) {
	off, err := b.u32(0)
	if err != nil {
		return table{}, err
	}
	return table{b: b, pos: off}, nil
}

// field returns the position of the field number id, and false when the table
// leaves it out.
func (t table) field(id int) (int, bool, error) {
	soff, err := t.b.i32(t.pos)
	if err != nil {
		return 0, false, err
	}
	vt := t.pos - soff
	size, err := t.b.u16(vt)
	if err != nil {
		return 0, false, err
	}
	entry := 4 + 2*id
	if entry+2 > size {
		return 0, false, nil
	}
	off, err := t.b.u16(vt + entry)
	if err != nil || off == 0 {
		return 0, false, err
	}
	return t.pos + off, true, nil
}

// target returns the position that the offset at pos points to.
func (b *buffer) target(pos int) (int, error) {
	off, err := b.u32(pos)
	if err != nil {
		return 0, err
	}
	return pos + off, nil
}

// child returns the table that the field number id holds.
func (t table) child(id int) (table, bool, error) {
	pos, ok, err := t.field(id)
	if err != nil || !ok {
		return table{}, false, err
	}
	at, err := t.b.target(pos)
	if err != nil {
		return table{}, false, err
	}
	return table{b: t.b, pos: at}, true, nil
}

// vector is a vector of a FlatBuffers message: the position of its first
// element and its length.
type vector struct {
	start, n int
}

// vector returns the vector that the field number id holds. A vector that the
// table leaves out has no element.
func (t table) vector(id int) (vector, error) {
	pos, ok, err := t.field(id)
	if err != nil || !ok {
		return vector{}, err
	}
	at, err := t.b.target(pos)
	if err != nil {
		return vector{}, err
	}
	n, err := t.b.u32(at)
	if err != nil {
		return vector{}, err
	}
	if n > t.b.size {
		return vector{}, invalid("a vector of the rows", nil)
	}
	return vector{start: at + 4, n: n}, nil
}

// int8 returns the byte field number id, or def when the table leaves it out.
func (t table) int8(id int, def int8) (int8, error) {
	pos, ok, err := t.field(id)
	if err != nil || !ok {
		return def, err
	}
	v, err := t.b.u8(pos)
	return int8(v), err //nolint:gosec // G115: the type of the field is a signed byte.
}

// int64 returns the field number id, or 0 when the table leaves it out.
func (t table) int64(id int) (int64, error) {
	pos, ok, err := t.field(id)
	if err != nil || !ok {
		return 0, err
	}
	return t.b.i64(pos)
}

// str returns the string that the field number id holds, and "" when the table
// leaves it out.
func (t table) str(id int) (string, error) {
	pos, ok, err := t.field(id)
	if err != nil || !ok {
		return "", err
	}
	return t.b.stringAt(pos)
}

// stringAt returns the string that the offset at pos points to.
func (b *buffer) stringAt(pos int) (string, error) {
	at, err := b.target(pos)
	if err != nil {
		return "", err
	}
	n, err := b.u32(at)
	if err != nil {
		return "", err
	}
	s, err := b.bytes(at+4, n)
	if err != nil {
		return "", err
	}
	return string(s), nil
}

// bytes returns the bytes of the vector of bytes that the field number id
// holds.
func (t table) bytes(id int) ([]byte, error) {
	v, err := t.vector(id)
	if err != nil || v.n == 0 {
		return []byte{}, err
	}
	b, err := t.b.bytes(v.start, v.n)
	if err != nil {
		return nil, err
	}
	return append([]byte{}, b...), nil
}

// element returns the position of the element i of a vector whose elements are
// size bytes long.
func (v vector) element(i, size int) (int, error) {
	if i < 0 || i >= v.n {
		return 0, fmt.Errorf("reading the element %d of a vector of %d: %w", i, v.n, dbimp.ErrInvalidValue)
	}
	return v.start + i*size, nil
}
