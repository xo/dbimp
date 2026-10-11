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
	"strings"
	"testing"
	"time"

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
