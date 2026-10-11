package dbimp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// nopConn is a connection that reads nothing and drops what it is given.
type nopConn struct{}

func (nopConn) Read([]byte) (int, error)    { return 0, nil }
func (nopConn) Write(p []byte) (int, error) { return len(p), nil }
func (nopConn) Close() error                { return nil }

func FuzzWebSocketFrame(f *testing.F) {
	for _, s := range []string{
		"810568656c6c6f",
		"0103" + "68656c" + "8902" + "7878" + "80026c6f",
		"827e0004" + "00010203",
		"880203e8",
		"8800",
		"827f0000000000000003" + "010203",
		"827fffffffffffffffff",
		"c1020000",
		"8180",
	} {
		b, _ := hex.DecodeString(s)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		const limit = 1 << 10
		ws := &WebSocket{
			rwc:        nopConn{},
			br:         bufio.NewReader(bytes.NewReader(in)),
			bw:         bufio.NewWriter(nopConn{}),
			maxMessage: limit,
		}
		for range 8 {
			mt, msg, err := ws.readMessage(t.Context())
			if err != nil {
				return
			}
			if len(msg) > limit {
				t.Fatalf("message of %d bytes is over the limit", len(msg))
			}
			if mt == MessageText && !utf8.Valid(msg) {
				t.Fatalf("text message %x is not UTF-8", msg)
			}
		}
	})
}

// tcpPair returns the two ends of a real TCP connection.
func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	var d net.Dialer
	client, err := d.DialContext(t.Context(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted
	if server == nil {
		t.Fatal("accepting the connection failed")
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

// bodyConn is the connection as the body of an upgraded response shows it: it
// reads, writes and closes, and it has no deadline. It counts the closes, and
// it runs after after each read or write that moved bytes.
type bodyConn struct {
	c      net.Conn
	closed atomic.Int32
	noop   bool
	after  func()
}

func (b *bodyConn) Read(p []byte) (int, error) {
	n, err := b.c.Read(p)
	if n > 0 && b.after != nil {
		f := b.after
		b.after = nil
		f()
	}
	return n, err
}

func (b *bodyConn) Write(p []byte) (int, error) {
	n, err := b.c.Write(p)
	if err == nil && b.after != nil {
		f := b.after
		b.after = nil
		f()
	}
	return n, err
}

func (b *bodyConn) Close() error {
	b.closed.Add(1)
	if b.noop {
		return nil
	}
	return b.c.Close()
}

// newBodyWebSocket makes a WebSocket over the client end of a TCP pair, in the
// way that DialWebSocket does: the rwc has no deadline, and conn is the TCP
// connection that the dial recorded.
func newBodyWebSocket(t *testing.T) (*WebSocket, *bodyConn, net.Conn) {
	t.Helper()
	c, s := tcpPair(t)
	b := &bodyConn{c: c}
	return &WebSocket{
		rwc:        b,
		conn:       c,
		br:         bufio.NewReader(b),
		bw:         bufio.NewWriter(b),
		maxMessage: DefaultMaxMessage,
	}, b, s
}

// TestLateContextEndAfterAReadKeepsTheConnection holds that a context that
// ends after the bytes of a message arrived, and before Read returns, does not
// break the next Read, when the connection has no write deadline for its body.
func TestLateContextEndAfterAReadKeepsTheConnection(t *testing.T) {
	t.Parallel()
	ws, b, s := newBodyWebSocket(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b.after = func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := s.Write([]byte{0x81, 0x02, 'h', 'i'}); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := ws.Read(ctx); err != nil || string(msg) != "hi" {
		t.Fatalf("first Read: %q, %v", msg, err)
	}
	if _, err := s.Write([]byte{0x81, 0x02, 'y', 'o'}); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := ws.Read(t.Context()); err != nil || string(msg) != "yo" {
		t.Fatalf("second Read: %q, %v", msg, err)
	}
	if n := b.closed.Load(); n != 0 {
		t.Errorf("the connection was closed %d times", n)
	}
}

// TestLateContextEndAfterAWriteKeepsTheConnection holds the same for Write,
// whose connection body has no SetWriteDeadline.
func TestLateContextEndAfterAWriteKeepsTheConnection(t *testing.T) {
	t.Parallel()
	ws, b, s := newBodyWebSocket(t)
	go func() { _, _ = io.Copy(io.Discard, s) }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b.after = func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	}
	if err := ws.Write(ctx, MessageText, []byte("one")); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := ws.Write(t.Context(), MessageText, []byte("two")); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if n := b.closed.Load(); n != 0 {
		t.Errorf("the connection was closed %d times", n)
	}
}

// TestDeadlineInterruptsABlockedRead holds that the end of a context interrupts
// a Read that waits for the network, with the deadline of the connection and
// not with a close: the body that the test uses ignores Close.
func TestDeadlineInterruptsABlockedRead(t *testing.T) {
	t.Parallel()
	ws, b, _ := newBodyWebSocket(t)
	b.noop = true
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, _, err := ws.Read(ctx)
		errc <- err
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("got %v, want the error of the context", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the blocked Read was not interrupted")
	}
}

// TestDeadlineInterruptsABlockedWrite holds the same for a Write to a server
// that never reads, with a message that is larger than the buffers of the
// sockets.
func TestDeadlineInterruptsABlockedWrite(t *testing.T) {
	t.Parallel()
	ws, b, _ := newBodyWebSocket(t)
	b.noop = true
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- ws.Write(ctx, MessageBinary, make([]byte, 128<<20)) }()
	select {
	case err := <-errc:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("got %v, want the error of the context", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the blocked Write was not interrupted")
	}
	if err := ws.Write(t.Context(), MessageText, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a Write after the interrupted one: %v", err)
	}
}

// memoryOf returns the bytes that f allocated.
func memoryOf(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestFrameThatDeclaresMoreThanItSendsHoldsLittleMemory holds that a frame that
// declares 60 MiB and sends one byte allocates one step, and not the length.
func TestFrameThatDeclaresMoreThanItSendsHoldsLittleMemory(t *testing.T) { //nolint:paralleltest // The test measures the allocations of the process.
	wire := append([]byte{0x82, 127}, binary.BigEndian.AppendUint64(nil, 60<<20)...)
	wire = append(wire, 'x')
	ws := &WebSocket{
		rwc:        nopConn{},
		br:         bufio.NewReader(bytes.NewReader(wire)),
		bw:         bufio.NewWriter(nopConn{}),
		maxMessage: DefaultMaxMessage,
	}
	var err error
	n := memoryOf(func() { _, _, err = ws.readMessage(t.Context()) })
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v, want io.ErrUnexpectedEOF", err)
	}
	if n > 3<<20 {
		t.Errorf("the reader allocated %d bytes for 1 byte of payload", n)
	}
}

// TestFragmentsJoinInOneBuffer holds that a message of many fragments comes out
// whole, that the cost of a fragment is linear, and that the memory stays near
// the size of the message.
func TestFragmentsJoinInOneBuffer(t *testing.T) { //nolint:paralleltest // The test measures the allocations of the process.
	const frames, size = 200_000, 3
	var wire []byte
	for i := range frames {
		fin := byte(0)
		op := byte(opContinuation)
		if i == 0 {
			op = opBinary
		}
		if i == frames-1 {
			fin = 0x80
		}
		wire = append(wire, fin|op, size, byte(i), byte(i>>8), byte(i>>16))
	}
	ws := &WebSocket{
		rwc:        nopConn{},
		br:         bufio.NewReader(bytes.NewReader(wire)),
		bw:         bufio.NewWriter(nopConn{}),
		maxMessage: DefaultMaxMessage,
	}
	var (
		msg []byte
		err error
	)
	n := memoryOf(func() { _, msg, err = ws.readMessage(t.Context()) })
	if err != nil || len(msg) != frames*size {
		t.Fatalf("got %d bytes, %v", len(msg), err)
	}
	for _, i := range []int{0, 1, 70_000, frames - 1} {
		if msg[i*size] != byte(i) || msg[i*size+1] != byte(i>>8) || msg[i*size+2] != byte(i>>16) { //nolint:gosec // The test keeps the low byte of each part.
			t.Errorf("fragment %d is wrong: % x", i, msg[i*size:i*size+size])
		}
	}
	if n > 8*uint64(len(msg)) {
		t.Errorf("the reader allocated %d bytes for a message of %d", n, len(msg))
	}
}

// emptyFrames returns a text frame with no final bit, and n-1 more empty
// continuation frames, the last one final.
func emptyFrames(n int) []byte {
	wire := make([]byte, 0, 2*n)
	wire = append(wire, opText, 0)
	for i := 1; i < n; i++ {
		fin := byte(0)
		if i == n-1 {
			fin = 0x80
		}
		wire = append(wire, fin|opContinuation, 0)
	}
	return wire
}

// closeFrameOf returns the code of the first close frame in the bytes that a
// WebSocket wrote, which the client masks.
func closeFrameOf(t *testing.T, wire []byte) CloseCode {
	t.Helper()
	if len(wire) < 8 || wire[0] != 0x80|opClose || wire[1] != 0x80|2 {
		t.Fatalf("no close frame with a code in % x", wire)
	}
	return CloseCode(binary.BigEndian.Uint16([]byte{wire[6] ^ wire[2], wire[7] ^ wire[3]}))
}

// TestTooManyFramesIsTooBig holds that a message of more frames than the limit
// closes the connection with 1009, and that a message at the limit is read.
func TestTooManyFramesIsTooBig(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		frames    int
		maxFrames int
		want      bool
	}{
		{"at a small limit", 100, 100, true},
		{"over a small limit", 101, 100, false},
		{"at the default limit", defaultMaxFrames, 0, true},
		{"over the default limit", defaultMaxFrames + 1, 0, false},
	} {
		var out bytes.Buffer
		ws := &WebSocket{
			rwc:        nopConn{},
			br:         bufio.NewReader(bytes.NewReader(emptyFrames(tc.frames))),
			bw:         bufio.NewWriter(&out),
			maxMessage: DefaultMaxMessage,
			maxFrames:  tc.maxFrames,
		}
		_, msg, err := ws.readMessage(t.Context())
		switch {
		case tc.want && (err != nil || len(msg) != 0):
			t.Errorf("%s: got %q, %v", tc.name, msg, err)
		case !tc.want && !errors.Is(err, ErrMessageTooBig):
			t.Errorf("%s: got %v, want ErrMessageTooBig", tc.name, err)
		case !tc.want && closeFrameOf(t, out.Bytes()) != CloseTooBig:
			t.Errorf("%s: close frame % x, want the code 1009", tc.name, out.Bytes())
		}
	}
}
