package tablestore

import (
	"errors"
	"fmt"
	"io"

	"github.com/xo/dbimp"
)

// The data types of a column in the FlatBuffers rows (sql.fbs of the SDK).
// COMPLEX is a column whose logic type says what it holds, such as a date, and
// whose encode type says how it is stored.
type dataType int8

const (
	dataNone      dataType = 0
	dataLong      dataType = 1
	dataBoolean   dataType = 2
	dataDouble    dataType = 3
	dataString    dataType = 4
	dataBinary    dataType = 5
	dataStringRLE dataType = 6
	dataComplex   dataType = 7
)

// The logic types of a COMPLEX column.
type logicType int8

const (
	logicNone     logicType = 0
	logicDatetime logicType = 1
	logicDate     logicType = 2
	logicTime     logicType = 3
)

// The numbers of the fields of the tables of the rows, in the order of
// sql.fbs. A FlatBuffers table numbers its fields from 0 in the order that the
// schema declares them.
const (
	rowsColumns  = 0
	rowsRowCount = 1

	colName    = 0
	colType    = 1
	colValues  = 2
	colComplex = 3

	valNulls   = 0
	valLongs   = 1
	valBools   = 2
	valDoubles = 3
	valStrings = 4
	valBinary  = 5
	valRLE     = 6

	bytesValue = 0

	rleArray = 0
	rleIndex = 1

	complexLogic  = 0
	complexEncode = 1
)

// column is one column of a result. It names the column, gives its type, and
// finds its values in the rows. A column finds the vectors of its values the
// first time that a row asks for one, so that a row of the first column does
// not read the whole message.
type column struct {
	name   string
	data   dataType
	logic  logicType
	encode dataType

	values   table
	hasVals  bool
	prepared bool
	nulls    vector
	vals     vector
	index    vector
}

// resultSet is the rows of one answer. It holds the columns and the count of
// the rows, and it reads each value from the buffer when a row asks for it.
type resultSet struct {
	buf   *buffer
	cols  []column
	count int
}

// parseRows reads the columns of the rows in buf. It reads the root table, the
// vector of the columns, and the name and the type of each one.
func parseRows(buf *buffer) (*resultSet, error) {
	rs := &resultSet{buf: buf}
	if buf.size == 0 {
		return rs, nil
	}
	root, err := buf.root()
	if err != nil {
		return nil, err
	}
	count, err := root.int64(rowsRowCount)
	if err != nil {
		return nil, err
	}
	if count < 0 || count > int64(buf.size) {
		return nil, invalid("the count of the rows", nil)
	}
	rs.count = int(count)
	cols, err := root.vector(rowsColumns)
	if err != nil {
		return nil, err
	}
	// A vector of n tables takes 4n bytes, so a count that the message cannot hold
	// is a broken message, and the driver never allocates for it.
	if cols.n > (buf.size-cols.start)/4 {
		return nil, invalid("the columns of the rows", nil)
	}
	rs.cols = make([]column, cols.n)
	for i := range rs.cols {
		at, err := cols.element(i, 4)
		if err != nil {
			return nil, err
		}
		tp, err := buf.target(at)
		if err != nil {
			return nil, err
		}
		if err := rs.cols[i].read(table{b: buf, pos: tp}); err != nil {
			return nil, err
		}
	}
	return rs, nil
}

// read reads the name and the types of the column in t.
func (c *column) read(t table) error {
	var err error
	if c.name, err = t.str(colName); err != nil {
		return err
	}
	tp, err := t.int8(colType, 0)
	if err != nil {
		return err
	}
	c.data = dataType(tp)
	if info, ok, err := t.child(colComplex); err != nil {
		return err
	} else if ok {
		lg, err := info.int8(complexLogic, 0)
		if err != nil {
			return err
		}
		en, err := info.int8(complexEncode, 0)
		if err != nil {
			return err
		}
		c.logic, c.encode = logicType(lg), dataType(en)
	}
	c.values, c.hasVals, err = t.child(colValues)
	return err
}

// prepare finds the vectors of the values of the column.
func (c *column) prepare() error {
	if c.prepared {
		return nil
	}
	c.prepared = true
	if !c.hasVals {
		return nil
	}
	var err error
	if c.nulls, err = c.values.vector(valNulls); err != nil {
		return err
	}
	switch c.storage() {
	case dataLong:
		c.vals, err = c.values.vector(valLongs)
	case dataBoolean:
		c.vals, err = c.values.vector(valBools)
	case dataDouble:
		c.vals, err = c.values.vector(valDoubles)
	case dataString:
		c.vals, err = c.values.vector(valStrings)
	case dataBinary:
		c.vals, err = c.values.vector(valBinary)
	case dataStringRLE:
		var rle table
		var ok bool
		if rle, ok, err = c.values.child(valRLE); err == nil && ok {
			if c.vals, err = rle.vector(rleArray); err == nil {
				c.index, err = rle.vector(rleIndex)
			}
		}
	}
	return err
}

// storage returns the data type that holds the values of the column: the data
// type of a plain column, and the encode type of a COMPLEX one.
func (c *column) storage() dataType {
	if c.data == dataComplex {
		return c.encode
	}
	return c.data
}

// value returns the value of the column in the row i: nil for a NULL, and an
// int64, float64, bool, string or []byte otherwise. It fails with an error that
// wraps dbimp.ErrInvalidValue when the message holds no such value.
func (c *column) value(i int) (any, error) {
	if err := c.prepare(); err != nil {
		return nil, err
	}
	if i < c.nulls.n {
		at, err := c.nulls.element(i, 1)
		if err != nil {
			return nil, err
		}
		null, err := c.values.b.u8(at)
		if err != nil {
			return nil, err
		}
		if null != 0 {
			return nil, nil
		}
	}
	if i >= c.vals.n && c.storage() != dataStringRLE {
		return nil, fmt.Errorf("reading the column %s: the row %d has no value: %w", c.name, i, dbimp.ErrInvalidValue)
	}
	b := c.values.b
	switch c.storage() {
	case dataLong:
		at, err := c.vals.element(i, 8)
		if err != nil {
			return nil, err
		}
		return b.i64(at)
	case dataBoolean:
		at, err := c.vals.element(i, 1)
		if err != nil {
			return nil, err
		}
		v, err := b.u8(at)
		return v != 0, err
	case dataDouble:
		at, err := c.vals.element(i, 8)
		if err != nil {
			return nil, err
		}
		return b.f64(at)
	case dataString:
		at, err := c.vals.element(i, 4)
		if err != nil {
			return nil, err
		}
		return b.stringAt(at)
	case dataBinary:
		at, err := c.vals.element(i, 4)
		if err != nil {
			return nil, err
		}
		tp, err := b.target(at)
		if err != nil {
			return nil, err
		}
		return table{b: b, pos: tp}.bytes(bytesValue)
	case dataStringRLE:
		return c.rle(i)
	}
	return nil, fmt.Errorf("reading the column %s: the data type %d: %w", c.name, c.storage(), dbimp.ErrNotSupported)
}

// rle returns the value of a string column that is run-length encoded: the
// element of the array that the index names for the row i.
func (c *column) rle(i int) (any, error) {
	b := c.values.b
	at, err := c.index.element(i, 4)
	if err != nil {
		return nil, err
	}
	k, err := b.i32(at)
	if err != nil {
		return nil, err
	}
	pos, err := c.vals.element(k, 4)
	if err != nil {
		return nil, err
	}
	return b.stringAt(pos)
}

// response is the answer to one SQLQuery. The driver reads its fields in order,
// up to the field that holds the rows, and it reads the fields after the rows
// when the rows end (D21 and D36).
type response struct {
	body io.Closer
	pb   pbReader
	rs   *resultSet

	statement int64
	payload   int64
	affected  int64
	// hasAffected is true when the answer holds the count of the rows that the
	// statement changed.
	hasAffected bool
	next        string
	done        bool
}

// readResponse reads the answer from body up to its rows, or to its end when it
// has none. It does not close body.
func readResponse(body io.ReadCloser) (*response, error) {
	r := &response{body: body, pb: newPBReader(body)}
	for {
		field, wire, err := r.pb.tag()
		if errors.Is(err, io.EOF) {
			r.done = true
			return r, nil
		}
		if err != nil {
			return nil, invalid("the answer", err)
		}
		if field == fieldRows && wire == wireBytes {
			n, err := r.pb.length()
			if err != nil {
				return nil, err
			}
			rs, err := parseRows(newBuffer(r.pb.r, n))
			if err != nil {
				return nil, err
			}
			r.rs = rs
			return r, nil
		}
		if err := r.field(field, wire); err != nil {
			return nil, err
		}
	}
}

// field reads one field that is not the rows.
func (r *response) field(field, wire int) error {
	switch {
	case field == fieldPayload && wire == wireVarint:
		v, err := r.pb.varint()
		r.payload = int64(v) //nolint:gosec // G115: the payload version is a small number.
		return failed("the payload version", err)
	case field == fieldStatement && wire == wireVarint:
		v, err := r.pb.varint()
		r.statement = int64(v) //nolint:gosec // G115: the type of a statement is a small number.
		return failed("the type of the statement", err)
	case field == fieldAffected && wire == wireVarint:
		v, err := r.pb.varint()
		r.affected, r.hasAffected = int64(v), true //nolint:gosec // G115: a count of rows is not near 2^63.
		return failed("the rows affected", err)
	case field == fieldNextToken && wire == wireBytes:
		n, err := r.pb.length()
		if err != nil {
			return err
		}
		b, err := r.pb.readBytes(n)
		r.next = string(b)
		return err
	}
	return r.pb.skip(wire)
}

// finish reads what is left of the answer: the rest of the rows, and the fields
// after them. It returns an error when the answer holds rows in another payload
// than FlatBuffers.
func (r *response) finish() error {
	if r.done {
		return nil
	}
	if r.rs != nil {
		if err := r.rs.buf.drain(); err != nil {
			return err
		}
	}
	for {
		field, wire, err := r.pb.tag()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return invalid("the answer", err)
		}
		if err := r.field(field, wire); err != nil {
			return err
		}
	}
	r.done = true
	if r.rs != nil && r.rs.count > 0 && r.payload != 0 && r.payload != payloadFlatBuffers {
		return fmt.Errorf("reading the rows: the server sent the payload %d, and the driver reads FlatBuffers only: %w", r.payload, dbimp.ErrNotSupported)
	}
	return nil
}
