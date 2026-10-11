package dbimp_test

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // RFC 6455 makes the accept key with SHA-1.
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// wsFrame is a frame that the test server read from the client.
type wsFrame struct {
	fin     bool
	opcode  byte
	masked  bool
	payload []byte
}

// wsConn is the server side of a WebSocket in a test. It is a minimal
// RFC 6455 loop, written apart from the client on purpose.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
}

func (c *wsConn) read(t *testing.T) wsFrame {
	t.Helper()
	f, err := c.tryRead()
	if err != nil {
		t.Errorf("server reading a frame: %v", err)
	}
	return f
}

func (c *wsConn) tryRead() (wsFrame, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return wsFrame{}, err
	}
	f := wsFrame{fin: h[0]&0x80 != 0, opcode: h[0] & 0xf, masked: h[1]&0x80 != 0}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return wsFrame{}, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return wsFrame{}, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	var key [4]byte
	if f.masked {
		if _, err := io.ReadFull(c.br, key[:]); err != nil {
			return wsFrame{}, err
		}
	}
	f.payload = make([]byte, n)
	if _, err := io.ReadFull(c.br, f.payload); err != nil {
		return wsFrame{}, err
	}
	if f.masked {
		for i := range f.payload {
			f.payload[i] ^= key[i&3]
		}
	}
	return f, nil
}

// write sends an unmasked frame.
func (c *wsConn) write(fin bool, opcode byte, payload []byte) {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	h := []byte{b0}
	switch n := len(payload); {
	case n < 126:
		h = append(h, byte(n))
	case n <= 0xffff:
		h = binary.BigEndian.AppendUint16(append(h, 126), uint16(n))
	default:
		h = binary.BigEndian.AppendUint64(append(h, 127), uint64(n))
	}
	_, _ = c.conn.Write(append(h, payload...))
}

func closePayload(code uint16, reason string) []byte {
	return append(binary.BigEndian.AppendUint16(nil, code), reason...)
}

// serverOptions changes how the test server answers the handshake.
type serverOptions struct {
	badAccept bool
	extension string
	protocol  string
	status    int
	// dupAccept sends the Sec-WebSocket-Accept header twice, and dupProtocol sends
	// the Sec-WebSocket-Protocol header twice.
	dupAccept   bool
	dupProtocol bool
}

// newWSServer starts a TLS server, with HTTP/2 on, that upgrades each request
// and then calls serve in the goroutine of the request.
func newWSServer(t *testing.T, so serverOptions, serve func(*testing.T, *wsConn, *http.Request)) *httptest.Server {
	t.Helper()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if so.status != 0 {
			http.Error(w, "no thanks", so.status)
			return
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		if r.Header.Get("Upgrade") != "websocket" || r.Header.Get("Sec-WebSocket-Version") != "13" || key == "" {
			t.Errorf("bad upgrade request headers: %v", r.Header)
		}
		sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec // RFC 6455 makes the accept key with SHA-1.
		accept := base64.StdEncoding.EncodeToString(sum[:])
		if so.badAccept {
			accept = "AAAA"
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("the response writer cannot hijack: HTTP/%d", r.ProtoMajor)
			return
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijacking: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		var sb strings.Builder
		sb.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n")
		if so.extension != "" {
			sb.WriteString("Sec-WebSocket-Extensions: " + so.extension + "\r\n")
		}
		if so.protocol != "" {
			sb.WriteString("Sec-WebSocket-Protocol: " + so.protocol + "\r\n")
		}
		if so.dupProtocol {
			sb.WriteString("Sec-WebSocket-Protocol: " + so.protocol + "\r\n")
		}
		if so.dupAccept {
			sb.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n")
		}
		sb.WriteString("\r\n")
		_, _ = conn.Write([]byte(sb.String()))
		serve(t, &wsConn{conn: conn, br: brw.Reader}, r)
	})
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string {
	return "wss" + strings.TrimPrefix(srv.URL, "https")
}

var insecure = dbimp.WithWebSocketTLS(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // The test server has a self-signed certificate.

// dial dials srv and closes the connection when the test ends.
func dial(t *testing.T, srv *httptest.Server, opts ...dbimp.WebSocketOption) *dbimp.WebSocket {
	t.Helper()
	ws, err := dbimp.DialWebSocket(t.Context(), wsURL(srv), append([]dbimp.WebSocketOption{insecure}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close(dbimp.CloseNormal, "") })
	return ws
}

func TestWebSocketEchoesTextAndBinaryMasked(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		for {
			f, err := c.tryRead()
			if err != nil || f.opcode == 8 {
				return
			}
			if !f.masked {
				t.Errorf("the client sent an unmasked frame")
			}
			c.write(true, f.opcode, f.payload)
		}
	})
	ws := dial(t, srv)
	for _, tc := range []struct {
		mt   dbimp.MessageType
		size int
	}{
		{dbimp.MessageText, 0},
		{dbimp.MessageText, 5},
		{dbimp.MessageBinary, 125},
		{dbimp.MessageBinary, 126},
		{dbimp.MessageText, 70000},
	} {
		in := []byte(strings.Repeat("é", tc.size/2) + strings.Repeat("x", tc.size%2))
		if err := ws.Write(t.Context(), tc.mt, in); err != nil {
			t.Fatal(err)
		}
		mt, out, err := ws.Read(t.Context())
		if err != nil || mt != tc.mt || string(out) != string(in) {
			t.Errorf("echo of %v, %d bytes: got %v, %d bytes, %v", tc.mt, len(in), mt, len(out), err)
		}
	}
}

func TestWebSocketHandshakeFailures(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	idle := func(*testing.T, *wsConn, *http.Request) {}
	for name, so := range map[string]serverOptions{
		"bad accept key":        {badAccept: true},
		"unoffered extension":   {extension: "permessage-deflate"},
		"unoffered subprotocol": {protocol: "other"},
	} {
		srv := newWSServer(t, so, idle)
		_, err := dbimp.DialWebSocket(t.Context(), wsURL(srv), insecure, dbimp.WithWebSocketSubprotocols("mine"))
		if !errors.Is(err, dbimp.ErrHandshake) {
			t.Errorf("%s: got %v, want ErrHandshake", name, err)
		}
	}
	srv := newWSServer(t, serverOptions{status: http.StatusUnauthorized}, idle)
	_, err := dbimp.DialWebSocket(t.Context(), wsURL(srv), insecure)
	var se *dbimp.StatusError
	if !errors.Is(err, dbimp.ErrHandshake) || !errors.As(err, &se) || se.Code != 401 {
		t.Errorf("status 401: got %v", err)
	}
	if _, err := dbimp.DialWebSocket(t.Context(), "ftp://localhost/"); !errors.Is(err, dbimp.ErrScheme) {
		t.Errorf("ftp scheme: got %v", err)
	}
}

func TestWebSocketSubprotocolHeadersAndOrigin(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	got := make(chan http.Header, 1)
	srv := newWSServer(t, serverOptions{protocol: "mine"}, func(_ *testing.T, c *wsConn, r *http.Request) {
		got <- r.Header
		_, _ = c.tryRead()
	})
	ws := dial(t, srv,
		dbimp.WithWebSocketSubprotocols("mine", "yours"),
		dbimp.WithWebSocketOrigin("https://example.com"),
		dbimp.WithWebSocketHeader(http.Header{"X-Extra": {"1"}}))
	h := <-got
	if ws.Subprotocol() != "mine" || h.Get("Origin") != "https://example.com" || h.Get("X-Extra") != "1" || h.Get("Sec-WebSocket-Protocol") != "mine, yours" {
		t.Errorf("subprotocol %q, headers %v", ws.Subprotocol(), h)
	}
}

func TestWebSocketFragmentsWithAPingBetween(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	pong := make(chan wsFrame, 1)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		c.write(false, 1, []byte("hel"))
		c.write(true, 9, []byte("x"))
		c.write(false, 0, []byte("l"))
		c.write(true, 0, []byte("o"))
		pong <- c.read(t)
	})
	ws := dial(t, srv)
	mt, msg, err := ws.Read(t.Context())
	if err != nil || mt != dbimp.MessageText || string(msg) != "hello" {
		t.Fatalf("got %v %q %v", mt, msg, err)
	}
	if f := <-pong; f.opcode != 10 || string(f.payload) != "x" || !f.masked {
		t.Errorf("pong: %+v", f)
	}
}

func TestWebSocketServerClose(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	echo := make(chan wsFrame, 1)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		c.write(true, 8, closePayload(1001, "going"))
		echo <- c.read(t)
	})
	ws := dial(t, srv)
	_, _, err := ws.Read(t.Context())
	var ce *dbimp.CloseError
	if !errors.As(err, &ce) || ce.Code != dbimp.CloseGoingAway || ce.Reason != "going" {
		t.Fatalf("got %v", err)
	}
	if f := <-echo; f.opcode != 8 || binary.BigEndian.Uint16(f.payload) != 1001 {
		t.Errorf("echo: %+v", f)
	}
	if _, _, err := ws.Read(t.Context()); !errors.As(err, &ce) {
		t.Errorf("second read: %v", err)
	}
	if err := ws.Write(t.Context(), dbimp.MessageText, nil); !errors.As(err, &ce) {
		t.Errorf("write after close: %v", err)
	}
}

func TestWebSocketClientClose(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	got := make(chan wsFrame, 1)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		f := c.read(t)
		got <- f
		c.write(true, 8, f.payload)
	})
	ws := dial(t, srv)
	if err := ws.Close(dbimp.CloseNormal, "bye"); err != nil {
		t.Fatal(err)
	}
	f := <-got
	if f.opcode != 8 || string(f.payload[2:]) != "bye" || binary.BigEndian.Uint16(f.payload) != 1000 {
		t.Errorf("close frame: %+v", f)
	}
	if err := ws.Write(t.Context(), dbimp.MessageText, nil); !errors.Is(err, dbimp.ErrWebSocketClosed) {
		t.Errorf("write after Close: %v", err)
	}
	if err := ws.Close(dbimp.CloseNormal, ""); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestWebSocketMessageOverTheLimit(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	got := make(chan wsFrame, 1)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		c.write(true, 2, make([]byte, 20))
		got <- c.read(t)
	})
	ws := dial(t, srv, dbimp.WithWebSocketMaxMessage(10))
	if _, _, err := ws.Read(t.Context()); !errors.Is(err, dbimp.ErrMessageTooBig) {
		t.Fatalf("got %v", err)
	}
	if f := <-got; f.opcode != 8 || binary.BigEndian.Uint16(f.payload) != 1009 {
		t.Errorf("close frame: %+v", f)
	}
	// A message in two fragments that is over the limit in total.
	srv = newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		c.write(false, 2, make([]byte, 6))
		c.write(true, 0, make([]byte, 6))
		_, _ = c.tryRead()
	})
	ws = dial(t, srv, dbimp.WithWebSocketMaxMessage(10))
	if _, _, err := ws.Read(t.Context()); !errors.Is(err, dbimp.ErrMessageTooBig) {
		t.Fatalf("fragments: got %v", err)
	}
}

func TestWebSocketProtocolErrors(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	for name, send := range map[string]func(*wsConn){
		"continuation first": func(c *wsConn) { c.write(true, 0, []byte("a")) },
		"bad utf-8":          func(c *wsConn) { c.write(true, 1, []byte{0xff}) },
		"reserved bit":       func(c *wsConn) { _, _ = c.conn.Write([]byte{0xc1, 0}) },
		"masked frame":       func(c *wsConn) { _, _ = c.conn.Write([]byte{0x81, 0x81, 1, 2, 3, 4, 5}) },
		"fragmented ping":    func(c *wsConn) { c.write(false, 9, nil) },
		"unknown opcode":     func(c *wsConn) { c.write(true, 3, nil) },
	} {
		srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
			t.Helper()
			send(c)
			_, _ = c.tryRead()
		})
		ws := dial(t, srv)
		if _, _, err := ws.Read(t.Context()); !errors.Is(err, dbimp.ErrWebSocketProtocol) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestWebSocketContextEndClosesTheConnection(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		for {
			if _, err := c.tryRead(); err != nil {
				return
			}
		}
	})
	ws := dial(t, srv)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := ws.Read(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("Read took %v", time.Since(start))
	}
	if err := ws.Write(t.Context(), dbimp.MessageText, []byte("x")); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("write after the end: %v", err)
	}
	ctx2, cancel2 := context.WithCancel(t.Context())
	cancel2()
	if err := ws.Write(ctx2, dbimp.MessageText, nil); err == nil {
		t.Errorf("write with a canceled context succeeded")
	}
}

func TestWebSocketOutlivesTheDialContext(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		f, err := c.tryRead()
		if err == nil {
			c.write(true, f.opcode, f.payload)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	ws, err := dbimp.DialWebSocket(ctx, wsURL(srv), insecure)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close(dbimp.CloseNormal, "") })
	if err := ws.Write(t.Context(), dbimp.MessageText, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := ws.Read(t.Context()); err != nil || string(msg) != "hi" {
		t.Errorf("got %q, %v", msg, err)
	}
}

func TestWebSocketRefusedOverPlainHTTP(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer plain.Close()
	_, err := dbimp.DialWebSocket(t.Context(), "ws"+strings.TrimPrefix(plain.URL, "http"))
	var se *dbimp.StatusError
	if !errors.As(err, &se) || se.Code != 403 {
		t.Errorf("got %v", err)
	}
}

// TestWebSocketCloseAfterACleanCloseReturnsNil holds that a connection that ended
// with a close frame of the server is closed with no error, so that a deferred
// Close does not report a normal shutdown, and that Close is idempotent.
func TestWebSocketCloseAfterACleanCloseReturnsNil(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		c.write(true, 8, closePayload(1000, "done"))
		_, _ = c.tryRead()
	})
	ws := dial(t, srv)
	var ce *dbimp.CloseError
	if _, _, err := ws.Read(t.Context()); !errors.As(err, &ce) || ce.Code != dbimp.CloseNormal {
		t.Fatalf("got %v", err)
	}
	for i := range 2 {
		if err := ws.Close(dbimp.CloseNormal, ""); err != nil {
			t.Errorf("Close call %d after a clean close: %v", i+1, err)
		}
	}
}

// TestWebSocketNoDataFrameAfterTheCloseFrame holds RFC 6455 section 5.5.1: a
// frame that is not a close frame never follows the close frame.
func TestWebSocketNoDataFrameAfterTheCloseFrame(t *testing.T) {
	t.Parallel()
	c, s := net.Pipe()
	t.Cleanup(func() { _ = s.Close() })
	ws := dbimp.NewTestWebSocket(c)
	opcodes := make(chan byte, 8)
	go func() {
		br := bufio.NewReader(s)
		for {
			var h [2]byte
			if _, err := io.ReadFull(br, h[:]); err != nil {
				close(opcodes)
				return
			}
			n := int(h[1] & 0x7f)
			rest := make([]byte, 4+n)
			if _, err := io.ReadFull(br, rest); err != nil {
				close(opcodes)
				return
			}
			opcodes <- h[0] & 0xf
		}
	}()
	if err := ws.SendCloseForTest(dbimp.CloseNormal, ""); err != nil {
		t.Fatal(err)
	}
	if op := <-opcodes; op != 8 {
		t.Fatalf("the first frame has the opcode %d, want 8", op)
	}
	err := ws.Write(t.Context(), dbimp.MessageText, []byte("late"))
	if !errors.Is(err, dbimp.ErrWebSocketClosed) {
		t.Errorf("Write after the close frame: %v, want ErrWebSocketClosed", err)
	}
	_ = c.Close()
	for op := range opcodes {
		t.Errorf("a frame with the opcode %d followed the close frame", op)
	}
}

// TestWebSocketWriteDuringCloseGetsTheClosedError holds that a Write and a Read
// that start while Close waits for the reply of the server return at once, and
// that they do not cut the handshake short.
func TestWebSocketWriteDuringCloseGetsTheClosedError(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	release := make(chan struct{})
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		f := c.read(t)
		<-release
		c.write(true, 8, f.payload)
		_, _ = c.tryRead()
	})
	ws := dial(t, srv)
	closed := make(chan error, 1)
	go func() { closed <- ws.Close(dbimp.CloseNormal, "") }()
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err = ws.Write(t.Context(), dbimp.MessageText, []byte("x")); err != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !errors.Is(err, dbimp.ErrWebSocketClosed) {
		t.Fatalf("Write during Close: %v, want ErrWebSocketClosed", err)
	}
	start := time.Now()
	if _, _, err := ws.Read(t.Context()); !errors.Is(err, dbimp.ErrWebSocketClosed) || time.Since(start) > time.Second {
		t.Errorf("Read during Close: %v after %v, want ErrWebSocketClosed at once", err, time.Since(start))
	}
	close(release)
	if err := <-closed; err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestWebSocketControlFrameDoesNotCountAgainstTheLimit holds that a ping inside a
// fragmented message, near the limit, is not a message that is too big.
func TestWebSocketControlFrameDoesNotCountAgainstTheLimit(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		c.write(false, 2, make([]byte, 9))
		c.write(true, 9, []byte("12345"))
		c.write(true, 0, make([]byte, 1))
		_, _ = c.tryRead()
	})
	ws := dial(t, srv, dbimp.WithWebSocketMaxMessage(10))
	mt, msg, err := ws.Read(t.Context())
	if err != nil || mt != dbimp.MessageBinary || len(msg) != 10 {
		t.Errorf("got %v, %d bytes, %v, want a message of 10 bytes", mt, len(msg), err)
	}
}

// TestWebSocketTruncatedFrameIsAnUnexpectedEOF holds that a stream that ends
// inside a frame is io.ErrUnexpectedEOF, and that only the end at the start of a
// frame is io.EOF.
func TestWebSocketTruncatedFrameIsAnUnexpectedEOF(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	for name, tc := range map[string]struct {
		wire []byte
		want error
	}{
		"cut header":         {[]byte{0x81}, io.ErrUnexpectedEOF},
		"cut length":         {[]byte{0x82, 0x7e, 0x01}, io.ErrUnexpectedEOF},
		"no length":          {[]byte{0x82, 0x7e}, io.ErrUnexpectedEOF},
		"cut payload":        {[]byte{0x81, 0x05, 'a', 'b'}, io.ErrUnexpectedEOF},
		"no payload":         {[]byte{0x81, 0x05}, io.ErrUnexpectedEOF},
		"end at a boundary":  {nil, io.EOF},
		"end after a frame":  {[]byte{0x81, 0x01, 'a'}, io.EOF},
		"cut control frame":  {[]byte{0x89, 0x03, 'a'}, io.ErrUnexpectedEOF},
		"cut extended frame": {append([]byte{0x82, 0x7f}, 0, 0, 0, 0), io.ErrUnexpectedEOF},
	} {
		srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
			t.Helper()
			if len(tc.wire) > 0 {
				_, _ = c.conn.Write(tc.wire)
			}
		})
		ws := dial(t, srv)
		var err error
		for range 3 {
			if _, _, err = ws.Read(t.Context()); err != nil {
				break
			}
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", name, err, tc.want)
		}
		if errors.Is(tc.want, io.EOF) && errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%s: a clean end is an unexpected EOF: %v", name, err)
		}
	}
}

// TestWebSocketCloseReasonIsCutAtACharacter holds that a reason that is longer
// than 123 bytes is cut where a character starts, so that the close frame holds
// UTF-8 (RFC 6455 section 5.5.1).
func TestWebSocketCloseReasonIsCutAtACharacter(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	got := make(chan wsFrame, 1)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		f := c.read(t)
		got <- f
		c.write(true, 8, f.payload)
	})
	ws := dial(t, srv)
	if err := ws.Close(dbimp.CloseNormal, strings.Repeat("é", 100)); err != nil {
		t.Fatal(err)
	}
	f := <-got
	reason := f.payload[2:]
	if f.opcode != 8 || len(reason) > 123 || !utf8.Valid(reason) || len(reason) != 122 {
		t.Errorf("the close frame has a reason of %d bytes, valid %t, want 122 bytes of UTF-8", len(reason), utf8.Valid(reason))
	}
}

// TestWebSocketHeadersAreCanonicalAndTheProtocolOnesAreIgnored holds that the
// header names of the caller are canonical, so that the headers that the protocol
// needs cannot be replaced or repeated, whatever the case of their names.
func TestWebSocketHeadersAreCanonicalAndTheProtocolOnesAreIgnored(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	got := make(chan http.Header, 1)
	srv := newWSServer(t, serverOptions{}, func(_ *testing.T, c *wsConn, r *http.Request) {
		got <- r.Header
		_, _ = c.tryRead()
	})
	dial(t, srv, dbimp.WithWebSocketHeader(http.Header{
		"sec-websocket-extensions": {"permessage-deflate"},
		"upgrade":                  {"other"},
		"connection":               {"close"},
		"sec-websocket-key":        {"AAAAAAAAAAAAAAAAAAAAAA=="},
		"sec-websocket-version":    {"8"},
		"sec-websocket-protocol":   {"x"},
		"x-lower":                  {"1"},
	}))
	h := <-got
	if v := h.Values("Sec-WebSocket-Extensions"); len(v) != 0 {
		t.Errorf("the client sent the extensions %v", v)
	}
	for name, want := range map[string]string{"Upgrade": "websocket", "Sec-Websocket-Version": "13"} {
		if v := h.Values(name); len(v) != 1 || v[0] != want {
			t.Errorf("%s is %v, want [%s]", name, v, want)
		}
	}
	if v := h.Values("Sec-WebSocket-Key"); len(v) != 1 || v[0] == "AAAAAAAAAAAAAAAAAAAAAA==" {
		t.Errorf("Sec-Websocket-Key is %v", v)
	}
	if v := h.Values("Sec-WebSocket-Protocol"); len(v) != 0 {
		t.Errorf("the client sent the subprotocols %v", v)
	}
	if h.Get("X-Lower") != "1" {
		t.Errorf("the header x-lower is %q, want 1", h.Get("X-Lower"))
	}
}

// TestWebSocketCloseReasonThatIsNotUTF8Is1007 holds RFC 6455 section 7.4.1: a close
// reason that is not UTF-8 gets the code 1007.
func TestWebSocketCloseReasonThatIsNotUTF8Is1007(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	got := make(chan wsFrame, 1)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		c.write(true, 8, append(closePayload(1000, ""), 0xff, 0xfe))
		got <- c.read(t)
	})
	ws := dial(t, srv)
	if _, _, err := ws.Read(t.Context()); !errors.Is(err, dbimp.ErrWebSocketProtocol) {
		t.Fatalf("got %v", err)
	}
	if f := <-got; f.opcode != 8 || binary.BigEndian.Uint16(f.payload) != 1007 {
		t.Errorf("close frame: %+v, want the code 1007", f)
	}
}

// TestWebSocketRefusesARepeatedHandshakeHeader holds that a response with the
// Sec-WebSocket-Accept or the Sec-WebSocket-Protocol header twice is refused.
func TestWebSocketRefusesARepeatedHandshakeHeader(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	idle := func(*testing.T, *wsConn, *http.Request) {}
	for name, so := range map[string]serverOptions{
		"accept twice":   {dupAccept: true},
		"protocol twice": {protocol: "mine", dupProtocol: true},
	} {
		srv := newWSServer(t, so, idle)
		_, err := dbimp.DialWebSocket(t.Context(), wsURL(srv), insecure, dbimp.WithWebSocketSubprotocols("mine"))
		if !errors.Is(err, dbimp.ErrHandshake) {
			t.Errorf("%s: got %v, want ErrHandshake", name, err)
		}
	}
}

// TestWebSocketContextEndedAfterAReadDoesNotCloseTheConnection holds that a
// context that ends after a call returned does not close the connection, however
// often it is tried.
func TestWebSocketContextEndedAfterAReadDoesNotCloseTheConnection(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		for {
			f, err := c.tryRead()
			if err != nil || f.opcode == 8 {
				return
			}
			c.write(true, f.opcode, f.payload)
		}
	})
	ws := dial(t, srv)
	var wg sync.WaitGroup
	for i := range 200 {
		ctx, cancel := context.WithCancel(t.Context())
		if err := ws.Write(ctx, dbimp.MessageText, []byte("ping")); err != nil {
			t.Fatalf("round %d: Write: %v", i, err)
		}
		_, msg, err := ws.Read(ctx)
		if err != nil || string(msg) != "ping" {
			t.Fatalf("round %d: Read: %q, %v", i, msg, err)
		}
		wg.Go(cancel)
		wg.Wait()
	}
}

// cancelAfterWrite is a connection that ends a context just after it wrote a
// whole frame, and before the call that wrote it returns.
type cancelAfterWrite struct {
	net.Conn

	cancel context.CancelFunc
}

func (c *cancelAfterWrite) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
		time.Sleep(50 * time.Millisecond)
	}
	return n, err
}

// TestWebSocketContextEndedAfterTheFrameWentOutKeepsTheConnection holds that a
// Write that sent its whole frame returns nil, and that the connection works for
// the next call, when the context ends before the call returns.
func TestWebSocketContextEndedAfterTheFrameWentOutKeepsTheConnection(t *testing.T) {
	t.Parallel()
	c, s := net.Pipe()
	t.Cleanup(func() { _ = s.Close(); _ = c.Close() })
	go func() { _, _ = io.Copy(io.Discard, s) }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ws := dbimp.NewTestWebSocket(&cancelAfterWrite{Conn: c, cancel: cancel})
	if err := ws.Write(ctx, dbimp.MessageText, []byte("one")); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := ws.Write(t.Context(), dbimp.MessageText, []byte("two")); err != nil {
		t.Errorf("second Write after the context ended: %v", err)
	}
}

// TestWebSocketLateContextEndKeepsTheConnectionOverTCP holds, over a real TLS
// connection whose body has no deadline, that a call that succeeded is never
// followed by a failure of the next call with a new context, when the context of
// the first call ends at the moment of the call (D217). A call that fails
// because its context ended is allowed, and the test dials again.
func TestWebSocketLateContextEndKeepsTheConnectionOverTCP(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
		t.Helper()
		for {
			f, err := c.tryRead()
			if err != nil || f.opcode == 8 {
				return
			}
			c.write(true, f.opcode, f.payload)
		}
	})
	ws := dial(t, srv)
	redials := 0
	for i := range 1000 {
		ctx, cancel := context.WithCancel(t.Context())
		var wg sync.WaitGroup
		wg.Go(func() {
			// Spin for 0 to 300 microseconds, which is about the time of one round.
			for end := time.Now().Add(time.Duration(i%300) * time.Microsecond); time.Now().Before(end); {
				runtime.Gosched()
			}
			cancel()
		})
		err := ws.Write(ctx, dbimp.MessageText, []byte("hi"))
		if err == nil {
			var msg []byte
			if _, msg, err = ws.Read(ctx); err == nil && string(msg) != "hi" {
				t.Fatalf("round %d: Read returned %q", i, msg)
			}
		}
		wg.Wait()
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("round %d: %v", i, err)
			}
			// The call that the context interrupted may leave a half frame, and the
			// connection may be closed, so this round ends with a new connection.
			_ = ws.Close(dbimp.CloseNormal, "")
			ws = dial(t, srv)
			redials++
			continue
		}
		// Every call of this round succeeded, so the next calls must too.
		if err := ws.Write(t.Context(), dbimp.MessageText, []byte("ok")); err != nil {
			t.Fatalf("round %d: Write after a successful round: %v", i, err)
		}
		if _, msg, err := ws.Read(t.Context()); err != nil || string(msg) != "ok" {
			t.Fatalf("round %d: Read after a successful round: %q, %v", i, msg, err)
		}
	}
	t.Logf("%d rounds ended with a context error", redials)
}

// TestWebSocketCloseCodes holds the bytes that Close puts on the wire: no code
// for CloseNoStatus, and an error, with nothing sent, for a code that an
// endpoint cannot send.
func TestWebSocketCloseCodes(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	for _, code := range []dbimp.CloseCode{0, 999, 1004, 1006, 1015, 1016, 2999, 5000, 65535} {
		first := make(chan wsFrame, 1)
		srv := newWSServer(t, serverOptions{}, func(_ *testing.T, c *wsConn, _ *http.Request) {
			f, _ := c.tryRead()
			first <- f
			_, _ = c.tryRead()
		})
		ws := dial(t, srv)
		if err := ws.Close(code, "x"); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("Close(%d): got %v, want ErrInvalidValue", code, err)
		}
		if err := ws.Write(t.Context(), dbimp.MessageText, []byte("still open")); err != nil {
			t.Errorf("Write after Close(%d) was refused: %v", code, err)
		}
		if f := <-first; f.opcode != 1 || string(f.payload) != "still open" {
			t.Errorf("Close(%d): the first frame of the server was %+v", code, f)
		}
	}
	for code, want := range map[dbimp.CloseCode][]byte{
		dbimp.CloseNoStatus: {},
		1000:                {0x03, 0xe8, 'r'},
		1012:                {0x03, 0xf4, 'r'},
		1014:                {0x03, 0xf6, 'r'},
		3000:                {0x0b, 0xb8, 'r'},
		4999:                {0x13, 0x87, 'r'},
	} {
		got := make(chan wsFrame, 1)
		srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
			t.Helper()
			f := c.read(t)
			got <- f
			c.write(true, 8, f.payload)
		})
		ws := dial(t, srv)
		if err := ws.Close(code, "r"); err != nil {
			t.Errorf("Close(%d): %v", code, err)
		}
		f := <-got
		if f.opcode != 8 || !f.masked || string(f.payload) != string(want) {
			t.Errorf("Close(%d): frame %+v, want a masked close frame with payload % x", code, f, want)
		}
	}
}

// TestWebSocketServerCloseWithRegisteredCodes holds that a close frame with a
// code that IANA registered, 1012 to 1014, is a close of the server and not a
// protocol error, and that a reserved code still is one.
func TestWebSocketServerCloseWithRegisteredCodes(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	for code, ok := range map[uint16]bool{
		1000: true, 1011: true, 1012: true, 1013: true, 1014: true, 3000: true, 4999: true,
		1004: false, 1005: false, 1006: false, 1015: false, 1016: false, 2999: false, 5000: false,
	} {
		srv := newWSServer(t, serverOptions{}, func(t *testing.T, c *wsConn, _ *http.Request) {
			t.Helper()
			c.write(true, 8, closePayload(code, "r"))
			_, _ = c.tryRead()
		})
		ws := dial(t, srv)
		_, _, err := ws.Read(t.Context())
		var ce *dbimp.CloseError
		switch {
		case ok && (!errors.As(err, &ce) || ce.Code != dbimp.CloseCode(code)):
			t.Errorf("code %d: got %v, want a close of the server", code, err)
		case !ok && !errors.Is(err, dbimp.ErrWebSocketProtocol):
			t.Errorf("code %d: got %v, want a protocol error", code, err)
		}
	}
}

// TestWebSocketTCPEndIsEOFAndNotACloseError pins the error of a server that
// closes the TCP connection at a frame boundary without a close frame: it wraps
// io.EOF, it is not a *CloseError, and every later call returns the same error.
func TestWebSocketTCPEndIsEOFAndNotACloseError(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(*testing.T, *wsConn, *http.Request) {})
	ws := dial(t, srv)
	for range 2 {
		_, _, err := ws.Read(t.Context())
		var ce *dbimp.CloseError
		if !errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &ce) {
			t.Fatalf("got %v, want an error that wraps io.EOF and no CloseError", err)
		}
	}
}

// TestWebSocketHostHeaderSetsTheHost holds that a Host header of the caller
// sets the host of the request, and that the request does not carry it twice.
func TestWebSocketHostHeaderSetsTheHost(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	type seen struct {
		host  string
		hosts []string
	}
	got := make(chan seen, 1)
	srv := newWSServer(t, serverOptions{}, func(_ *testing.T, c *wsConn, r *http.Request) {
		got <- seen{host: r.Host, hosts: r.Header.Values("Host")}
		_, _ = c.tryRead()
	})
	dial(t, srv, dbimp.WithWebSocketHeader(http.Header{"host": {"db.example.com"}}))
	if s := <-got; s.host != "db.example.com" || len(s.hosts) != 0 {
		t.Errorf("host %q, Host headers %q, want db.example.com and none", s.host, s.hosts)
	}
}

// TestWebSocketProxyOption holds that the default client uses no proxy until
// WithWebSocketProxy sets one. The proxy function sees the request of the dial.
func TestWebSocketProxyOption(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	srv := newWSServer(t, serverOptions{}, func(_ *testing.T, c *wsConn, _ *http.Request) {
		_, _ = c.tryRead()
	})
	var calls int
	dial(t, srv, dbimp.WithWebSocketProxy(func(r *http.Request) (*url.URL, error) {
		calls++
		if r.URL.Scheme != "https" {
			t.Errorf("proxy function got the scheme %q", r.URL.Scheme)
		}
		return nil, nil //nolint:nilnil // No proxy for this request.
	}))
	if calls != 1 {
		t.Errorf("the proxy function ran %d times, want 1", calls)
	}
}

// TestWebSocketFailedDialLeavesNoIdleConnection holds that a dial that fails
// after the server answered closes the idle connection of its own transport.
// The server stays up, so only the client can end the goroutines of the
// connection.
func TestWebSocketFailedDialLeavesNoIdleConnection(t *testing.T) { //nolint:paralleltest // The test counts the goroutines of the process.
	srv := newWSServer(t, serverOptions{status: http.StatusForbidden}, func(*testing.T, *wsConn, *http.Request) {})
	before := runtime.NumGoroutine()
	for range 10 {
		if _, err := dbimp.DialWebSocket(t.Context(), wsURL(srv), insecure); !errors.Is(err, dbimp.ErrHandshake) {
			t.Fatalf("got %v", err)
		}
	}
	for deadline := time.Now().Add(5 * time.Second); runtime.NumGoroutine() > before+2; {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines after ten failed dials, %d before", runtime.NumGoroutine(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
