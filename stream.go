package dbimp

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Stream reads the body of a response one token at a time (D36). It holds
// the body and a jsontext.Decoder that reads from it, and no other buffer,
// because the decoder keeps its own.
//
// A driver reads the rows through Decoder, and calls End after the last
// value of the response. Close before End closes the body and reads nothing
// more, so a large result is never drained. Close after End closes a body
// that is at EOF, so the connection goes back to the pool.
type Stream struct {
	body io.ReadCloser
	dec  *jsontext.Decoder
	once sync.Once
	err  error
}

// NewStream returns a Stream that reads body.
func NewStream(body io.ReadCloser) *Stream {
	return &Stream{
		body: body,
		dec:  jsontext.NewDecoder(body),
	}
}

// Decoder returns the decoder that reads the body.
func (s *Stream) Decoder() *jsontext.Decoder {
	return s.dec
}

// End reads to the end of the body after the last value. It returns an
// error if anything but white space follows that value.
func (s *Stream) End() error {
	switch _, err := s.dec.ReadToken(); {
	case errors.Is(err, io.EOF):
		return nil
	case err != nil:
		return fmt.Errorf("reading the end of the response: %w", err)
	}
	return fmt.Errorf("reading the end of the response: data after the last value: %w", ErrInvalidValue)
}

// Close closes the body. It reads nothing more from it. It can run more than
// once, and returns the same error each time.
func (s *Stream) Close() error {
	s.once.Do(func() {
		s.err = s.body.Close()
	})
	return s.err
}
