package surrealdb

import (
	"errors"
	"fmt"
	"io"

	"github.com/xo/dbimp"
)

// cborSets reads the result sets of a response in CBOR, with
// dbimp.CBORDecoder (D49 and D108). jsonSets walks the answer in the same
// way, in JSON, and the two are kept alike.
type cborSets struct {
	setState

	d *dbimp.CBORDecoder
	// stack holds the objects and the arrays that are open, the innermost
	// last, with the count of the items read of each.
	stack []cborFrame
}

// cborFrame is an object or an array that is open.
type cborFrame struct {
	h dbimp.CBORHead
	n uint64
}

func newCBORSets(body io.Reader) *cborSets {
	return &cborSets{d: dbimp.NewCBORDecoder(body)}
}

// decode satisfies setReader.
func (s *cborSets) decode(b []byte) (any, error) {
	return decodeCBOR(b)
}

// next satisfies setReader.
func (s *cborSets) next() ([]string, error) {
	if !s.started {
		s.started = true
		statements, err := s.readHead()
		if err != nil {
			return nil, err
		}
		if !statements {
			err := s.readValue()
			return s.cols, err
		}
		return s.startSet()
	}
	if err := s.skipSet(); err != nil {
		return nil, err
	}
	if s.done {
		return nil, io.EOF
	}
	more, err := s.more()
	if err != nil {
		return nil, fmt.Errorf("reading the next statement: %w", err)
	}
	if !more {
		if err := s.finish(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	err = s.readSet()
	return s.cols, err
}

// row satisfies setReader.
func (s *cborSets) row(vals [][]byte) error {
	if s.pending {
		s.pending = false
		copy(vals, s.first)
		return nil
	}
	if !s.stream {
		if err := s.finishSet(); err != nil {
			return err
		}
		return io.EOF
	}
	more, err := s.more()
	if err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	if !more {
		s.stream = false
		if err := s.finishSet(); err != nil {
			return err
		}
		return io.EOF
	}
	return s.readRow(vals)
}

// readHead reads the response up to its result. It reports true for the
// array of the statements of the method query, which it opens, and false
// for the result of another method, such as ping. An error of the RPC call
// itself is the error of the response.
func (s *cborSets) readHead() (bool, error) {
	if err := s.open(); err != nil {
		return false, fmt.Errorf("reading the response: %w", err)
	}
	for {
		more, err := s.more()
		if err != nil {
			return false, fmt.Errorf("reading the response: %w", err)
		}
		if !more {
			return false, fmt.Errorf("reading the response: no result and no error: %w", dbimp.ErrInvalidValue)
		}
		name, err := s.key()
		if err != nil {
			return false, fmt.Errorf("reading the response: %w", err)
		}
		switch name {
		case "error":
			b, err := s.raw()
			if err != nil {
				return false, fmt.Errorf("reading the error of the response: %w", err)
			}
			v, err := s.decode(b)
			if err != nil {
				return false, fmt.Errorf("reading the error of the response: %w", err)
			}
			return false, rpcError(v)
		case "result":
			k, err := s.peek()
			if err != nil {
				return false, fmt.Errorf("reading the result of the response: %w", err)
			}
			if k != kindArray {
				return false, nil
			}
			return true, s.open()
		default:
			if err := s.skip(); err != nil {
				return false, fmt.Errorf("reading %q of the response: %w", name, err)
			}
		}
	}
}

// readValue reads the result of a method other than query, which is one
// value, as one result set of one row.
func (s *cborSets) readValue() error {
	b, err := s.raw()
	if err != nil {
		return fmt.Errorf("reading the result of the response: %w", err)
	}
	s.setColumns([]string{""})
	s.first, s.pending, s.setDone = [][]byte{b}, true, true
	return s.finish()
}

// startSet reads the entry of the first statement up to its rows. A
// response that holds no statement, as the text BEGIN alone does on 2.7,
// has one result set with no columns and no rows.
func (s *cborSets) startSet() ([]string, error) {
	more, err := s.more()
	if err != nil {
		return nil, fmt.Errorf("reading the first statement: %w", err)
	}
	if !more {
		s.cols, s.setDone = []string{}, true
		err := s.finish()
		return s.cols, err
	}
	err = s.readSet()
	return s.cols, err
}

// readSet reads the entry of the next statement up to its rows, which the
// caller has found by more.
func (s *cborSets) readSet() error {
	s.reset()
	if err := s.open(); err != nil {
		return fmt.Errorf("reading a statement: %w", err)
	}
	for {
		more, err := s.more()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if !more {
			s.setDone = true
			if s.cols == nil {
				return fmt.Errorf("reading a statement: no result: %w", dbimp.ErrInvalidValue)
			}
			return s.setError()
		}
		name, err := s.key()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if name != "result" {
			if err := s.readMember(name); err != nil {
				return err
			}
			continue
		}
		if err := s.readResult(); err != nil {
			return err
		}
		if s.stream {
			return nil
		}
	}
}

// readResult reads the result of a statement: the start of its rows if it is
// an array, and the one row that it is otherwise (D52 and D101).
func (s *cborSets) readResult() error {
	k, err := s.peek()
	if err != nil {
		return fmt.Errorf("reading the result of a statement: %w", err)
	}
	switch k {
	case kindArray:
		if err := s.open(); err != nil {
			return err
		}
		more, err := s.more()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		if !more {
			s.cols = []string{}
			return nil
		}
		first, err := s.peek()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		s.stream = true
		s.objects = first == kindObject
		return s.readFirst()
	case kindObject:
		s.objects = true
		return s.readFirst()
	}
	b, err := s.raw()
	if err != nil {
		return fmt.Errorf("reading the result of a statement: %w", err)
	}
	s.setColumns([]string{""})
	s.first, s.pending = [][]byte{b}, true
	return nil
}

// readFirst reads the first row, which gives the columns of the result set.
func (s *cborSets) readFirst() error {
	if !s.objects {
		b, err := s.raw()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		s.setColumns([]string{""})
		s.first, s.pending = [][]byte{b}, true
		return nil
	}
	if err := s.open(); err != nil {
		return fmt.Errorf("reading the first row: %w", err)
	}
	var cols []string
	var vals [][]byte
	for {
		more, err := s.more()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		if !more {
			break
		}
		name, err := s.key()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		b, err := s.raw()
		if err != nil {
			return fmt.Errorf("reading the first row: %w", err)
		}
		cols, vals = append(cols, name), append(vals, b)
	}
	if cols == nil {
		cols = []string{}
	}
	s.setColumns(cols)
	s.first, s.pending = vals, true
	return nil
}

// readRow reads the next row of the rows of an array into vals.
func (s *cborSets) readRow(vals [][]byte) error {
	clear(vals)
	if !s.objects {
		b, err := s.raw()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		vals[0] = b
		return nil
	}
	k, err := s.peek()
	if err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	if k != kindObject {
		return fmt.Errorf("reading a row that is not an object, after a row that is: %w", dbimp.ErrColumnCount)
	}
	if err := s.open(); err != nil {
		return fmt.Errorf("reading a row: %w", err)
	}
	for {
		more, err := s.more()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		if !more {
			return nil
		}
		name, err := s.key()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		i, ok := s.index[name]
		if !ok {
			return fmt.Errorf("reading a row: %q: %w", name, dbimp.ErrExtraColumn)
		}
		if vals[i], err = s.raw(); err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
	}
}

// readMember reads a member of the entry of a statement that is not its
// result: its status, the kind of its error, or a member that the driver
// does not use, such as time.
func (s *cborSets) readMember(name string) error {
	switch name {
	case "status", "kind":
		b, err := s.raw()
		if err != nil {
			return fmt.Errorf("reading %q of a statement: %w", name, err)
		}
		v, err := s.decode(b)
		if err != nil {
			return fmt.Errorf("reading %q of a statement: %w", name, err)
		}
		str, _ := v.(string)
		if name == "kind" {
			s.kind = str
		} else if str != "OK" {
			s.setFailed(str)
		}
		return nil
	}
	if err := s.skip(); err != nil {
		return fmt.Errorf("reading %q of a statement: %w", name, err)
	}
	return nil
}

// setFailed marks the statement as failed, with its result as the message.
func (s *cborSets) setFailed(status string) {
	var msg string
	if s.pending && !s.objects && len(s.first) == 1 {
		if v, err := s.decode(s.first[0]); err == nil {
			msg, _ = v.(string)
		}
	}
	s.fail(status, msg)
}

// finishSet reads the rest of the entry of the current statement after its
// rows, and returns its error.
func (s *cborSets) finishSet() error {
	for !s.setDone {
		more, err := s.more()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if !more {
			s.setDone = true
			break
		}
		name, err := s.key()
		if err != nil {
			return fmt.Errorf("reading a statement: %w", err)
		}
		if err := s.readMember(name); err != nil {
			return err
		}
	}
	return s.setError()
}

// skipSet reads the rest of the current result set, and returns an error
// only if the response cannot be read.
func (s *cborSets) skipSet() error {
	s.pending = false
	for s.stream {
		more, err := s.more()
		if err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
		if !more {
			s.stream = false
			break
		}
		if err := s.skip(); err != nil {
			return fmt.Errorf("reading a row: %w", err)
		}
	}
	if err := s.finishSet(); err != nil && !isServerError(err) {
		return err
	}
	return nil
}

// finish reads the rest of the response after the entry of its last
// statement.
func (s *cborSets) finish() error {
	if s.done {
		return nil
	}
	for {
		more, err := s.more()
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		if !more {
			break
		}
		if _, err := s.key(); err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		if err := s.skip(); err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
	}
	s.done = true
	return s.end()
}

// count counts one item of the innermost open object or array.
func (s *cborSets) count() {
	if len(s.stack) > 0 {
		s.stack[len(s.stack)-1].n++
	}
}

// peek returns the kind of the next value, and reads nothing.
func (s *cborSets) peek() (kind, error) {
	h, err := s.d.PeekHead()
	if err != nil {
		return kindOther, err
	}
	switch h.Major {
	case dbimp.CBORMap:
		return kindObject, nil
	case dbimp.CBORArray:
		return kindArray, nil
	}
	return kindOther, nil
}

// open reads the start of the object or the array that comes next.
func (s *cborSets) open() error {
	h, err := s.d.ReadHead()
	if err != nil {
		return err
	}
	if h.Major != dbimp.CBORMap && h.Major != dbimp.CBORArray {
		return fmt.Errorf("reading CBOR major type %d where an object or an array was expected: %w", h.Major, dbimp.ErrInvalidValue)
	}
	s.count()
	s.stack = append(s.stack, cborFrame{h: h})
	return nil
}

// more reports whether the innermost open object or array holds another
// value, and reads its end if it does not.
func (s *cborSets) more() (bool, error) {
	if len(s.stack) == 0 {
		return false, fmt.Errorf("reading past the end of the response: %w", dbimp.ErrInvalidValue)
	}
	top := s.stack[len(s.stack)-1]
	more, err := s.d.More(top.h, top.n)
	if err != nil {
		return false, err
	}
	if !more {
		s.stack = s.stack[:len(s.stack)-1]
	}
	return more, nil
}

// key reads the next key of an object.
func (s *cborSets) key() (string, error) {
	str, err := s.d.ReadText()
	s.count()
	return str, err
}

// raw reads the next value whole, and returns its bytes.
func (s *cborSets) raw() ([]byte, error) {
	b, err := s.d.ReadRaw()
	s.count()
	return b, err
}

// skip reads the next value whole, and keeps none of it.
func (s *cborSets) skip() error {
	err := s.d.Skip()
	s.count()
	return err
}

// end reads to the end of the response after its last value.
func (s *cborSets) end() error {
	if _, err := s.d.PeekHead(); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		return fmt.Errorf("reading the end of the response: data after the last value: %w", dbimp.ErrInvalidValue)
	}
	return nil
}
