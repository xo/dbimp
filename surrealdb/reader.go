package surrealdb

import (
	"bufio"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"

	"github.com/xo/dbimp"
)

// kind is the kind of the next value of a response.
type kind int

// The kinds of a value that a reader tells apart.
const (
	kindOther kind = iota
	kindObject
	kindArray
)

// reader reads a response one value at a time, in CBOR or in JSON (D49 and
// D25). The rows walk the response through it, and keep each value that they
// hand to a caller as its raw bytes.
type reader interface {
	// peek returns the kind of the next value, and reads nothing.
	peek() (kind, error)
	// open reads the start of the object or the array that comes next.
	open() error
	// more reports whether the innermost open object or array holds another
	// value, and reads its end if it does not.
	more() (bool, error)
	// key reads the next key of an object.
	key() (string, error)
	// raw reads the next value whole, and returns its bytes.
	raw() ([]byte, error)
	// skip reads the next value whole, and keeps none of it.
	skip() error
	// end reads to the end of the response after its last value.
	end() error
	// decode returns the Go value of bytes that raw returned.
	decode(b []byte) (any, error)
}

// cborReader reads a response in CBOR.
type cborReader struct {
	d *dbimp.CBORDecoder
	// stack holds the objects and the arrays that are open, the innermost last,
	// with the count of the items read of each.
	stack []cborFrame
}

// cborFrame is an object or an array that is open.
type cborFrame struct {
	h dbimp.CBORHead
	n uint64
}

func newCBORReader(body io.Reader) *cborReader {
	return &cborReader{d: dbimp.NewCBORDecoder(bufio.NewReader(body))}
}

// count counts one item of the innermost open object or array.
func (r *cborReader) count() {
	if len(r.stack) > 0 {
		r.stack[len(r.stack)-1].n++
	}
}

func (r *cborReader) peek() (kind, error) {
	h, err := r.d.PeekHead()
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

func (r *cborReader) open() error {
	h, err := r.d.ReadHead()
	if err != nil {
		return err
	}
	if h.Major != dbimp.CBORMap && h.Major != dbimp.CBORArray {
		return fmt.Errorf("reading CBOR major type %d where an object or an array was expected: %w", h.Major, dbimp.ErrInvalidValue)
	}
	r.count()
	r.stack = append(r.stack, cborFrame{h: h})
	return nil
}

func (r *cborReader) more() (bool, error) {
	if len(r.stack) == 0 {
		return false, fmt.Errorf("reading past the end of the response: %w", dbimp.ErrInvalidValue)
	}
	top := r.stack[len(r.stack)-1]
	more, err := r.d.More(top.h, top.n)
	if err != nil {
		return false, err
	}
	if !more {
		r.stack = r.stack[:len(r.stack)-1]
	}
	return more, nil
}

func (r *cborReader) key() (string, error) {
	s, err := r.d.ReadText()
	r.count()
	return s, err
}

func (r *cborReader) raw() ([]byte, error) {
	b, err := r.d.ReadRaw()
	r.count()
	return b, err
}

func (r *cborReader) skip() error {
	err := r.d.Skip()
	r.count()
	return err
}

func (r *cborReader) end() error {
	if _, err := r.d.PeekHead(); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		return fmt.Errorf("reading the end of the response: data after the last value: %w", dbimp.ErrInvalidValue)
	}
	return nil
}

func (r *cborReader) decode(b []byte) (any, error) {
	return decodeCBOR(b)
}

// jsonReader reads a response in JSON, for the key encoding=json (D49).
type jsonReader struct {
	dec *jsontext.Decoder
}

func newJSONReader(body io.Reader) *jsonReader {
	return &jsonReader{dec: jsontext.NewDecoder(body)}
}

func (r *jsonReader) peek() (kind, error) {
	switch r.dec.PeekKind() {
	case '{':
		return kindObject, nil
	case '[':
		return kindArray, nil
	case 0:
		// PeekKind reports an error as the kind 0, and the next read returns
		// it.
		if _, err := r.dec.ReadToken(); err != nil {
			return kindOther, err
		}
	}
	return kindOther, nil
}

func (r *jsonReader) open() error {
	tok, err := r.dec.ReadToken()
	if err != nil {
		return err
	}
	if k := tok.Kind(); k != '{' && k != '[' {
		return fmt.Errorf("reading %v where an object or an array was expected: %w", k, dbimp.ErrInvalidValue)
	}
	return nil
}

func (r *jsonReader) more() (bool, error) {
	switch r.dec.PeekKind() {
	case '}', ']':
		_, err := r.dec.ReadToken()
		return false, err
	case 0:
		_, err := r.dec.ReadToken()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return false, err
	}
	return true, nil
}

func (r *jsonReader) key() (string, error) {
	tok, err := r.dec.ReadToken()
	if err != nil {
		return "", err
	}
	if tok.Kind() != '"' {
		return "", fmt.Errorf("reading %v where a key was expected: %w", tok.Kind(), dbimp.ErrInvalidValue)
	}
	return tok.String(), nil
}

func (r *jsonReader) raw() ([]byte, error) {
	v, err := r.dec.ReadValue()
	if err != nil {
		return nil, err
	}
	return v.Clone(), nil
}

func (r *jsonReader) skip() error {
	return r.dec.SkipValue()
}

func (r *jsonReader) end() error {
	switch _, err := r.dec.ReadToken(); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("reading the end of the response: %w", err)
	}
	return fmt.Errorf("reading the end of the response: data after the last value: %w", dbimp.ErrInvalidValue)
}

func (r *jsonReader) decode(b []byte) (any, error) {
	return dbimp.Any(jsontext.Value(b))
}
