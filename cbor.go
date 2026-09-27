package dbimp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// The code in this file reads and writes CBOR (RFC 8949), for a driver whose
// server speaks it, such as SurrealDB (D49). It reads one item at a time, as
// jsontext does, so a driver never holds a whole result in memory (D25). The
// meaning of a tag belongs to the driver, so this code reads and writes tags,
// and gives them no meaning.

// CBORMajor is the major type of a CBOR item.
type CBORMajor byte

// The major types of CBOR, from section 3.1 of RFC 8949.
const (
	CBORUint   CBORMajor = 0
	CBORNegInt CBORMajor = 1
	CBORBytes  CBORMajor = 2
	CBORText   CBORMajor = 3
	CBORArray  CBORMajor = 4
	CBORMap    CBORMajor = 5
	CBORTag    CBORMajor = 6
	// CBORSimple holds false, true, null, undefined, the floats and the
	// break that ends an item of indefinite length.
	CBORSimple CBORMajor = 7
)

// The simple values and the floats of major type 7, by their additional
// information.
const (
	cborFalse     = 20
	cborTrue      = 21
	cborNull      = 22
	cborUndefined = 23
	cborFloat16   = 25
	cborFloat32   = 26
	cborFloat64   = 27
	cborIndef     = 31
)

// maxCBORDepth is the deepest nesting of arrays, maps and tags that the
// decoder reads. A deeper item is ErrInvalidValue, so a hostile response
// cannot exhaust the stack.
const maxCBORDepth = 256

// cborChunk is the most that the decoder allocates at once for a string. A
// string that claims a larger length grows as its bytes arrive, so a length
// that lies costs no memory.
const cborChunk = 64 << 10

// CBORHead is the head of one CBOR item: its major type and its argument.
type CBORHead struct {
	// Major is the major type.
	Major CBORMajor
	// Info is the additional information of the first byte, from 0 to 31.
	Info byte
	// Arg is the argument. It is the value of an integer, the length of a
	// string, an array or a map, the number of a tag, the bits of a float,
	// or the number of a simple value. It is 0 for an item of indefinite
	// length.
	Arg uint64
}

// Indefinite reports whether the item has an indefinite length, which a
// break ends.
func (h CBORHead) Indefinite() bool {
	return h.Info == cborIndef && h.Major >= CBORBytes && h.Major <= CBORMap
}

// Break reports whether the head is the break that ends an item of
// indefinite length.
func (h CBORHead) Break() bool {
	return h.Major == CBORSimple && h.Info == cborIndef
}

// Null reports whether the item is null or undefined.
func (h CBORHead) Null() bool {
	return h.Major == CBORSimple && (h.Info == cborNull || h.Info == cborUndefined)
}

// Float reports whether the item is a float, and returns it.
func (h CBORHead) Float() (float64, bool) {
	if h.Major != CBORSimple {
		return 0, false
	}
	switch h.Info {
	case cborFloat16:
		return float16(uint16(h.Arg)), true //nolint:gosec // G115: the argument of a float of 16 bits holds 16 bits.
	case cborFloat32:
		return float64(math.Float32frombits(uint32(h.Arg))), true //nolint:gosec // G115: the argument of a float of 32 bits holds 32 bits.
	case cborFloat64:
		return math.Float64frombits(h.Arg), true
	}
	return 0, false
}

// Bool reports whether the item is true or false, and returns it.
func (h CBORHead) Bool() (bool, bool) {
	if h.Major != CBORSimple || (h.Info != cborFalse && h.Info != cborTrue) {
		return false, false
	}
	return h.Info == cborTrue, true
}

// float16 returns the float64 of a half-precision float.
func float16(bits uint16) float64 {
	sign := 1.0
	if bits&0x8000 != 0 {
		sign = -1
	}
	exp := int(bits>>10) & 0x1f
	frac := float64(bits & 0x3ff)
	switch exp {
	case 0:
		return sign * math.Ldexp(frac, -24)
	case 0x1f:
		if frac == 0 {
			return math.Inf(int(sign))
		}
		return math.NaN()
	}
	return sign * math.Ldexp(frac+1024, exp-25)
}

// CBORDecoder reads CBOR items from a reader, one at a time.
type CBORDecoder struct {
	r *bufio.Reader
}

// NewCBORDecoder returns a decoder that reads from r.
func NewCBORDecoder(r io.Reader) *CBORDecoder {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	return &CBORDecoder{r: br}
}

// PeekHead returns the head of the next item, and reads nothing.
func (d *CBORDecoder) PeekHead() (CBORHead, error) {
	first, err := d.r.Peek(1)
	if err != nil {
		return CBORHead{}, eof(err)
	}
	n := headSize(first[0])
	if n == 0 {
		return CBORHead{}, fmt.Errorf("reading a CBOR head 0x%02x: %w", first[0], ErrInvalidValue)
	}
	b, err := d.r.Peek(n)
	if err != nil {
		return CBORHead{}, unexpected(err)
	}
	return parseHead(b), nil
}

// ReadHead reads the head of the next item. For a string, an array, a map or
// a tag, the content follows, and the caller reads it.
func (d *CBORDecoder) ReadHead() (CBORHead, error) {
	h, err := d.PeekHead()
	if err != nil {
		return h, err
	}
	_, err = d.r.Discard(headSize(byte(h.Major)<<5 | h.Info))
	return h, err
}

// ReadString reads the content of the byte string or the text string whose
// head is h, including each chunk of one of indefinite length.
func (d *CBORDecoder) ReadString(h CBORHead) ([]byte, error) {
	if h.Major != CBORBytes && h.Major != CBORText {
		return nil, fmt.Errorf("reading a string from CBOR major type %d: %w", h.Major, ErrInvalidValue)
	}
	if !h.Indefinite() {
		return d.readN(nil, h.Arg)
	}
	var out []byte
	for {
		c, err := d.ReadHead()
		if err != nil {
			return nil, err
		}
		if c.Break() {
			return out, nil
		}
		if c.Major != h.Major || c.Indefinite() {
			return nil, fmt.Errorf("reading a chunk of a CBOR string: %w", ErrInvalidValue)
		}
		if out, err = d.readN(out, c.Arg); err != nil {
			return nil, err
		}
	}
}

// ReadText reads the next item, which must be a text string.
func (d *CBORDecoder) ReadText() (string, error) {
	h, err := d.ReadHead()
	if err != nil {
		return "", err
	}
	if h.Major != CBORText {
		return "", fmt.Errorf("reading CBOR major type %d as text: %w", h.Major, ErrInvalidValue)
	}
	b, err := d.ReadString(h)
	return string(b), err
}

// More reports whether the container whose head is h has another item, when
// n items of it were read. For an item of indefinite length, it reads the
// break that ends it.
func (d *CBORDecoder) More(h CBORHead, n uint64) (bool, error) {
	if !h.Indefinite() {
		size := h.Arg
		if h.Major == CBORMap {
			size *= 2
		}
		return n < size, nil
	}
	next, err := d.PeekHead()
	if err != nil {
		return false, unexpected(err)
	}
	if next.Break() {
		_, err := d.r.Discard(1)
		return false, err
	}
	return true, nil
}

// ReadRaw reads the next item whole, and returns its bytes, which the caller
// owns.
func (d *CBORDecoder) ReadRaw() ([]byte, error) {
	return d.raw(nil, 0)
}

// Skip reads the next item whole, and keeps none of it.
func (d *CBORDecoder) Skip() error {
	_, err := d.raw(nil, 0)
	return err
}

// raw appends the next item to dst.
func (d *CBORDecoder) raw(dst []byte, depth int) ([]byte, error) {
	if depth > maxCBORDepth {
		return nil, fmt.Errorf("reading CBOR nested deeper than %d: %w", maxCBORDepth, ErrInvalidValue)
	}
	h, err := d.PeekHead()
	if err != nil {
		if depth > 0 {
			err = unexpected(err)
		}
		return nil, err
	}
	n := headSize(byte(h.Major)<<5 | h.Info)
	head, _ := d.r.Peek(n)
	dst = append(dst, head...)
	if _, err := d.r.Discard(n); err != nil {
		return nil, err
	}
	switch h.Major {
	case CBORBytes, CBORText:
		if !h.Indefinite() {
			return d.readN(dst, h.Arg)
		}
		for {
			c, err := d.PeekHead()
			if err != nil {
				return nil, unexpected(err)
			}
			if c.Break() {
				_, err := d.r.Discard(1)
				return append(dst, 0xff), err
			}
			if c.Major != h.Major || c.Indefinite() {
				return nil, fmt.Errorf("reading a chunk of a CBOR string: %w", ErrInvalidValue)
			}
			if dst, err = d.raw(dst, depth+1); err != nil {
				return nil, err
			}
		}
	case CBORArray, CBORMap, CBORTag:
		for i := uint64(0); ; i++ {
			more := i < 1
			if h.Major != CBORTag {
				if more, err = d.More(h, i); err != nil {
					return nil, err
				}
				if !more && h.Indefinite() {
					dst = append(dst, 0xff)
				}
			}
			if !more {
				return dst, nil
			}
			if dst, err = d.raw(dst, depth+1); err != nil {
				return nil, err
			}
		}
	case CBORSimple:
		if h.Break() {
			return nil, fmt.Errorf("reading a CBOR break outside an item of indefinite length: %w", ErrInvalidValue)
		}
	}
	return dst, nil
}

// readN appends n bytes of content to dst. It grows dst as the bytes arrive,
// so a length that lies costs no memory.
func (d *CBORDecoder) readN(dst []byte, n uint64) ([]byte, error) {
	for n > 0 {
		step := min(n, cborChunk)
		start := len(dst)
		dst = append(dst, make([]byte, step)...)
		if _, err := io.ReadFull(d.r, dst[start:]); err != nil {
			return nil, unexpected(err)
		}
		n -= step
	}
	return dst, nil
}

// headSize returns the size of the head whose first byte is b, or 0 if b
// starts no head.
func headSize(b byte) int {
	info := b & 0x1f
	switch {
	case info < 24:
		return 1
	case info <= 27:
		return 1 + 1<<(info-24)
	case info == cborIndef:
		switch CBORMajor(b >> 5) {
		case CBORBytes, CBORText, CBORArray, CBORMap, CBORSimple:
			return 1
		}
	}
	return 0
}

// parseHead reads the head in b, which holds exactly its bytes.
func parseHead(b []byte) CBORHead {
	h := CBORHead{Major: CBORMajor(b[0] >> 5), Info: b[0] & 0x1f}
	switch {
	case h.Info < 24:
		h.Arg = uint64(h.Info)
	case h.Info == 24:
		h.Arg = uint64(b[1])
	case h.Info == 25:
		h.Arg = uint64(binary.BigEndian.Uint16(b[1:]))
	case h.Info == 26:
		h.Arg = uint64(binary.BigEndian.Uint32(b[1:]))
	case h.Info == 27:
		h.Arg = binary.BigEndian.Uint64(b[1:])
	}
	return h
}

// eof keeps io.EOF, which marks the end of the input between two items.
func eof(err error) error {
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return err
}

// unexpected turns io.EOF inside an item into io.ErrUnexpectedEOF.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

// CBOREncoder appends CBOR items to a buffer.
type CBOREncoder struct {
	buf bytes.Buffer
}

// Bytes returns what the encoder holds.
func (e *CBOREncoder) Bytes() []byte {
	return e.buf.Bytes()
}

// Head appends the head of an item of major type m with the argument arg, in
// its shortest form.
func (e *CBOREncoder) Head(m CBORMajor, arg uint64) {
	top := byte(m) << 5
	switch {
	case arg < 24:
		e.buf.WriteByte(top | byte(arg))
	case arg <= math.MaxUint8:
		e.buf.Write([]byte{top | 24, byte(arg)})
	case arg <= math.MaxUint16:
		e.buf.WriteByte(top | 25)
		e.buf.Write(binary.BigEndian.AppendUint16(nil, uint16(arg)))
	case arg <= math.MaxUint32:
		e.buf.WriteByte(top | 26)
		e.buf.Write(binary.BigEndian.AppendUint32(nil, uint32(arg)))
	default:
		e.buf.WriteByte(top | 27)
		e.buf.Write(binary.BigEndian.AppendUint64(nil, arg))
	}
}

// Int appends an integer.
func (e *CBOREncoder) Int(i int64) {
	if i < 0 {
		e.Head(CBORNegInt, uint64(-(i + 1))) //nolint:gosec // G115: -(i+1) is not negative when i is.
		return
	}
	e.Head(CBORUint, uint64(i))
}

// Uint appends an unsigned integer.
func (e *CBOREncoder) Uint(u uint64) {
	e.Head(CBORUint, u)
}

// Float appends a float64.
func (e *CBOREncoder) Float(f float64) {
	e.buf.WriteByte(byte(CBORSimple)<<5 | cborFloat64)
	e.buf.Write(binary.BigEndian.AppendUint64(nil, math.Float64bits(f)))
}

// Bool appends true or false.
func (e *CBOREncoder) Bool(b bool) {
	if b {
		e.buf.WriteByte(byte(CBORSimple)<<5 | cborTrue)
		return
	}
	e.buf.WriteByte(byte(CBORSimple)<<5 | cborFalse)
}

// Null appends null.
func (e *CBOREncoder) Null() {
	e.buf.WriteByte(byte(CBORSimple)<<5 | cborNull)
}

// Text appends a text string.
func (e *CBOREncoder) Text(s string) {
	e.Head(CBORText, uint64(len(s)))
	e.buf.WriteString(s)
}

// ByteString appends a byte string.
func (e *CBOREncoder) ByteString(b []byte) {
	e.Head(CBORBytes, uint64(len(b)))
	e.buf.Write(b)
}

// Array appends the head of an array of n items, which the caller appends.
func (e *CBOREncoder) Array(n int) {
	e.Head(CBORArray, uint64(n)) //nolint:gosec // G115: a count is not negative.
}

// Map appends the head of a map of n pairs, which the caller appends.
func (e *CBOREncoder) Map(n int) {
	e.Head(CBORMap, uint64(n)) //nolint:gosec // G115: a count is not negative.
}

// Tag appends the tag num, whose item the caller appends.
func (e *CBOREncoder) Tag(num uint64) {
	e.Head(CBORTag, num)
}

// Raw appends an item that is already encoded.
func (e *CBOREncoder) Raw(b []byte) {
	e.buf.Write(b)
}
