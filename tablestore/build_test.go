package tablestore //nolint:testpackage // The builder writes the types of the package.

import (
	"encoding/binary"
	"math"
)

// This file writes FlatBuffers rows, so that the tests can build answers that no
// recording holds: a result with a million rows, a result that ends early, and
// columns of every data type. The layout is the simplest one that a reader must
// accept: every table is followed by the tables that it names, and the text of
// the strings comes last. A reader that follows the offsets reads it, as it
// reads the layout of the server.

// tcol is one column of a result that the builder writes.
type tcol struct {
	name   string
	data   dataType
	logic  logicType
	encode dataType
	// complex is true to write the table of the type info, which a COMPLEX
	// column needs and the others of the server have too (recorded).
	complex bool

	nulls   []bool
	longs   []int64
	bools   []bool
	doubles []float64
	strs    []string
	bins    [][]byte
	// rleArray and rleIndex are the two vectors of a column of the type
	// STRING_RLE.
	rleArray []string
	rleIndex []int32
}

// fbw is the message that the builder writes.
type fbw struct {
	b       []byte
	strings []pendingString
}

// pendingString is an offset that the builder patches when it writes the text of
// a string, which comes after every table.
type pendingString struct {
	at   int
	text string
}

func (w *fbw) alloc(n int) int {
	at := len(w.b)
	w.b = append(w.b, make([]byte, n)...)
	return at
}

func (w *fbw) putU16(at int, v int)   { binary.LittleEndian.PutUint16(w.b[at:], uint16(v)) } //nolint:gosec // G115: the builder writes sizes that fit.
func (w *fbw) putU32(at int, v int)   { binary.LittleEndian.PutUint32(w.b[at:], uint32(v)) } //nolint:gosec // G115: the builder writes sizes that fit.
func (w *fbw) putI64(at int, v int64) { binary.LittleEndian.PutUint64(w.b[at:], uint64(v)) } //nolint:gosec // G115: the bits of a signed integer.

// ref writes at the offset to the position target.
func (w *fbw) ref(at, target int) { w.putU32(at, target-at) }

// tbl is a table that the builder wrote. Each field has a slot of 8 bytes.
type tbl struct {
	w    *fbw
	pos  int
	slot map[int]int
}

// table writes a table of nfields fields, of which the fields in present are
// there. Its vtable comes first.
func (w *fbw) table(nfields int, present ...int) *tbl {
	vt := w.alloc(4 + 2*nfields)
	pos := w.alloc(4 + 8*len(present))
	w.putU16(vt, 4+2*nfields)
	w.putU16(vt+2, 4+8*len(present))
	t := &tbl{w: w, pos: pos, slot: map[int]int{}}
	for i, id := range present {
		off := 4 + 8*i
		w.putU16(vt+4+2*id, off)
		t.slot[id] = pos + off
	}
	binary.LittleEndian.PutUint32(w.b[pos:], uint32(int32(pos-vt))) //nolint:gosec // G115: a vtable is before its table, and the distance is small.
	return t
}

func (t *tbl) setU8(id int, v int8)   { t.w.b[t.slot[id]] = byte(v) } //nolint:gosec // G115: the bits of a signed byte.
func (t *tbl) setI64(id int, v int64) { t.w.putI64(t.slot[id], v) }
func (t *tbl) set(id, target int)     { t.w.ref(t.slot[id], target) }

// str makes the field id of the table hold a string, which the builder writes at
// the end.
func (t *tbl) str(id int, s string) {
	t.w.strings = append(t.w.strings, pendingString{at: t.slot[id], text: s})
}

// vecBytes writes a vector of bytes.
func (w *fbw) vecBytes(v []byte) int {
	at := w.alloc(4 + len(v))
	w.putU32(at, len(v))
	copy(w.b[at+4:], v)
	return at
}

// vecI64 writes a vector of 64 bit integers.
func (w *fbw) vecI64(v []int64) int {
	at := w.alloc(4 + 8*len(v))
	w.putU32(at, len(v))
	for i, x := range v {
		w.putI64(at+4+8*i, x)
	}
	return at
}

// vecF64 writes a vector of doubles.
func (w *fbw) vecF64(v []float64) int {
	ints := make([]int64, len(v))
	for i, x := range v {
		ints[i] = int64(math.Float64bits(x)) //nolint:gosec // G115: the bits of a double.
	}
	return w.vecI64(ints)
}

// vecI32 writes a vector of 32 bit integers.
func (w *fbw) vecI32(v []int32) int {
	at := w.alloc(4 + 4*len(v))
	w.putU32(at, len(v))
	for i, x := range v {
		w.putU32(at+4+4*i, int(x))
	}
	return at
}

// vecBools writes a vector of booleans.
func (w *fbw) vecBools(v []bool) int {
	b := make([]byte, len(v))
	for i, x := range v {
		if x {
			b[i] = 1
		}
	}
	return w.vecBytes(b)
}

// vecStrings writes a vector of strings, whose text the builder writes at the
// end, in the order of the vector.
func (w *fbw) vecStrings(v []string) int {
	at := w.alloc(4 + 4*len(v))
	w.putU32(at, len(v))
	for i, s := range v {
		w.strings = append(w.strings, pendingString{at: at + 4 + 4*i, text: s})
	}
	return at
}

// vecTables writes a vector of n offsets to tables, which patchTable fills in
// when the tables exist. A table comes after the vector that names it, because
// an offset points forward.
func (w *fbw) vecTables(n int) int {
	at := w.alloc(4 + 4*n)
	w.putU32(at, n)
	return at
}

// patchTable makes the element i of the vector at vec name the table at target.
func (w *fbw) patchTable(vec, i, target int) {
	w.ref(vec+4+4*i, target)
}

// finish writes the text of the strings, each one with its length first and its
// NUL after it, aligned to 4 bytes, and patches the offsets.
func (w *fbw) finish() []byte {
	for _, p := range w.strings {
		at := w.alloc((4 + len(p.text) + 1 + 3) &^ 3)
		w.putU32(at, len(p.text))
		copy(w.b[at+4:], p.text)
		w.ref(p.at, at)
	}
	w.strings = nil
	return w.b
}

// buildRows returns the FlatBuffers message of a result with the columns cols and
// rowCount rows. rowCount is 0 for a result with no row.
func buildRows(cols []tcol, rowCount int) []byte {
	w := &fbw{}
	w.alloc(4)
	root := w.table(2, 0, 1)
	w.putU32(0, root.pos)
	root.setI64(1, int64(rowCount))
	vec := w.vecTables(len(cols))
	root.set(0, vec)
	built := make([]*tbl, len(cols))
	for i := range cols {
		present := []int{0, 1, 2}
		if cols[i].complex {
			present = append(present, 3)
		}
		built[i] = w.table(4, present...)
		w.patchTable(vec, i, built[i].pos)
	}
	for i, c := range cols {
		t := built[i]
		t.str(0, c.name)
		t.setU8(1, int8(c.data))
		t.set(2, w.values(c))
		if c.complex {
			info := w.table(2, 0, 1)
			info.setU8(0, int8(c.logic))
			info.setU8(1, int8(c.encode))
			t.set(3, info.pos)
		}
	}
	return w.finish()
}

// values writes the table ColumnValues of c, with the vectors that its data
// type needs.
func (w *fbw) values(c tcol) int {
	storage := c.data
	if storage == dataComplex {
		storage = c.encode
	}
	present := []int{}
	if c.nulls != nil {
		present = append(present, valNulls)
	}
	switch storage {
	case dataLong:
		present = append(present, valLongs)
	case dataBoolean:
		present = append(present, valBools)
	case dataDouble:
		present = append(present, valDoubles)
	case dataString:
		present = append(present, valStrings)
	case dataBinary:
		present = append(present, valBinary)
	case dataStringRLE:
		present = append(present, valRLE)
	}
	t := w.table(7, present...)
	if c.nulls != nil {
		t.set(valNulls, w.vecBools(c.nulls))
	}
	switch storage {
	case dataLong:
		t.set(valLongs, w.vecI64(c.longs))
	case dataBoolean:
		t.set(valBools, w.vecBools(c.bools))
	case dataDouble:
		t.set(valDoubles, w.vecF64(c.doubles))
	case dataString:
		t.set(valStrings, w.vecStrings(c.strs))
	case dataBinary:
		vec := w.vecTables(len(c.bins))
		t.set(valBinary, vec)
		for i, b := range c.bins {
			item := w.table(1, bytesValue)
			w.patchTable(vec, i, item.pos)
			item.set(bytesValue, w.vecBytes(b))
		}
	case dataStringRLE:
		rle := w.table(2, rleArray, rleIndex)
		rle.set(rleArray, w.vecStrings(c.rleArray))
		rle.set(rleIndex, w.vecI32(c.rleIndex))
		t.set(valRLE, rle.pos)
	}
	return t.pos
}

// answer returns the body of an answer to a SELECT that holds the rows msg, in
// the form of the server: the rows, the payload version, the type of the
// statement and a count of 0 rows changed.
func answer(msg []byte) []byte {
	body := appendBytes(nil, fieldRows, msg)
	return answerTail(body, 1, 0)
}

// answerTail appends the fields that follow the rows.
func answerTail(body []byte, statement, affected int64) []byte {
	body = appendInt(body, fieldPayload, payloadFlatBuffers)
	body = appendInt(body, fieldStatement, statement)
	return appendInt(body, fieldAffected, affected)
}
