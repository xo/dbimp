package exasol

import (
	"bytes"
	"compress/zlib"
	"errors"
	"testing"

	"github.com/xo/dbimp"
)

// TestDecodeBoundsTheInflatedAnswerByTheLimit holds that an answer that inflates
// to more than the limit of the session is too big, and not to more than twice
// that limit (D217).
func TestDecodeBoundsTheInflatedAnswerByTheLimit(t *testing.T) {
	t.Parallel()
	const limit = 1000
	deflate := func(n int) []byte {
		body := append([]byte(`{"status":"ok","pad":"`), bytes.Repeat([]byte{'a'}, n-24)...)
		body = append(body, '"', '}')
		var buf bytes.Buffer
		zw := zlib.NewWriter(&buf)
		_, _ = zw.Write(body)
		_ = zw.Close()
		return buf.Bytes()
	}
	s := &session{maxData: limit}
	if _, err := s.decode(dbimp.MessageBinary, deflate(limit)); errors.Is(err, dbimp.ErrMessageTooBig) {
		t.Errorf("an answer of the limit: %v", err)
	}
	if _, err := s.decode(dbimp.MessageBinary, deflate(limit+1)); !errors.Is(err, dbimp.ErrMessageTooBig) {
		t.Errorf("an answer of the limit and one byte: got %v, want ErrMessageTooBig", err)
	}
}

// TestInflateGrowsOnlyAsBytesArrive holds that the buffer of an answer does not
// double: it holds the answer and one step, so the peak is near the size of the
// answer, as the limit of the session means.
func TestInflateGrowsOnlyAsBytesArrive(t *testing.T) {
	t.Parallel()
	const size = 20 << 20
	src := bytes.Repeat([]byte{'a'}, size)
	got, err := inflate(bytes.NewReader(src), 64<<20)
	if err != nil || len(got) != size {
		t.Fatalf("got %d bytes, %v, want %d", len(got), err, size)
	}
	// The capacity of a doubling buffer would be 32 MiB for 20 MiB.
	if c := cap(got); c > size+maxInflateStep {
		t.Errorf("the buffer holds %d bytes for an answer of %d, want at most %d", c, size, size+maxInflateStep)
	}
	if _, err := inflate(bytes.NewReader(src), size-1); !errors.Is(err, dbimp.ErrMessageTooBig) {
		t.Errorf("an answer over the limit: got %v, want ErrMessageTooBig", err)
	}
	if got, err := inflate(bytes.NewReader(nil), 10); err != nil || len(got) != 0 {
		t.Errorf("an empty answer: %d bytes, %v", len(got), err)
	}
}
