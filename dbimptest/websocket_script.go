package dbimptest

import (
	"bufio"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// WebSocketStep is one step of the script of ServeWebSocketScript: what the
// server does when it reads one request frame.
type WebSocketStep struct {
	// Reply is the frame that the server sends for the request.
	Reply WebSocketFrame
	// Hold makes the server wait for one more frame before it sends Reply, such
	// as the message that cancels a query. The server counts the extra frame,
	// and it closes Held when it starts to wait.
	Hold bool
	// Drop makes the server read the request, send nothing, and close the
	// connection.
	Drop bool
	// Mute makes the server read the request, send nothing, and keep the
	// connection open until the client closes it, as a server that hangs does.
	Mute bool
}

// heldSettle is how long the server waits, after it reads the request of a step
// with Hold, before it closes Held.
const heldSettle = 50 * time.Millisecond

// ScriptedWebSocket is a TLS server that answers the request frames of each
// connection with a script. Unlike ReplayWebSocket, it keeps no recorded
// exchange and matches nothing: step i answers the i-th request frame of a
// connection, whatever it holds. A test uses it for a case that no recording
// holds, such as a reply that waits for a cancel.
type ScriptedWebSocket struct {
	srv *httptest.Server

	mu     sync.Mutex
	frames []WebSocketFrame
	held   chan struct{}
	once   sync.Once
}

// ServeWebSocketScript starts a server that answers each connection with
// script. A request frame after the last step fails the test and closes the
// connection. The server closes with the test.
func ServeWebSocketScript(tb testing.TB, script []WebSocketStep) *ScriptedWebSocket {
	tb.Helper()
	s := &ScriptedWebSocket{held: make(chan struct{})}
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		conns []net.Conn
	)
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, br, err := upgrade(w, r)
		if err != nil {
			tb.Errorf("accepting a websocket in the scripted server: %v", err)
			return
		}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()
		wg.Add(1)
		defer wg.Done()
		defer func() { _ = conn.Close() }()
		s.serve(tb, conn, br, script)
	}))
	s.srv.StartTLS()
	tb.Cleanup(func() {
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
		s.srv.Close()
	})
	return s
}

// Addr returns the host and the port of the server.
func (s *ScriptedWebSocket) Addr() string {
	return s.srv.Listener.Addr().String()
}

// Certificate returns the certificate of the server, which nothing trusts but
// a client that is given it.
func (s *ScriptedWebSocket) Certificate() *x509.Certificate {
	return s.srv.Certificate()
}

// Frames returns the data frames that the server read, in order, from every
// connection.
func (s *ScriptedWebSocket) Frames() []WebSocketFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]WebSocketFrame(nil), s.frames...)
}

// Held returns a channel that closes when the server starts to wait at a step
// with Hold, a moment after it read the request.
func (s *ScriptedWebSocket) Held() <-chan struct{} {
	return s.held
}

// serve answers the frames of one connection.
func (s *ScriptedWebSocket) serve(tb testing.TB, conn net.Conn, br *bufio.Reader, script []WebSocketStep) {
	tb.Helper()
	for _, step := range script {
		if !s.read(conn, br) {
			return
		}
		if step.Hold {
			s.once.Do(func() {
				// The client can still be inside its call to Write when the server has
				// the whole frame. A client that cancels its context at that instant
				// closes the connection, as the guard of WebSocket.Write is built to do
				// until its call returns. So the server waits a moment before it tells
				// the test, and a test that cancels at Held meets a client that is
				// reading.
				time.Sleep(heldSettle)
				close(s.held)
			})
			if !s.read(conn, br) {
				return
			}
		}
		if step.Drop {
			return
		}
		if step.Mute {
			// Read until the client closes the connection.
			for s.read(conn, br) {
			}
			return
		}
		if !s.send(conn, step.Reply) {
			return
		}
	}
	// Any further frame is an error of the driver, which sent a request that
	// the script does not hold.
	if op, msg, err := readClientMessage(br); err == nil && op != opClose && op != opPing {
		tb.Errorf("the scripted server read the frame %q after the last step of its script", msg)
		_ = writeServerFrame(conn, opClose, closePayload(1008, "no step left"))
		return
	} else if err == nil && op == opClose {
		_ = writeServerFrame(conn, opClose, msg)
	}
}

// read reads one request frame, answers a ping and a close frame, and records a
// data frame. It reports false when the connection ended.
func (s *ScriptedWebSocket) read(conn net.Conn, br *bufio.Reader) bool {
	for {
		op, msg, err := readClientMessage(br)
		if err != nil {
			return false
		}
		switch op {
		case opClose:
			_ = writeServerFrame(conn, opClose, msg)
			return false
		case opPing:
			_ = writeServerFrame(conn, opPong, msg)
			continue
		}
		s.mu.Lock()
		s.frames = append(s.frames, NewFrame(op == opBinary, msg, true))
		s.mu.Unlock()
		return true
	}
}

// send writes the reply frame. A close frame closes the connection.
func (s *ScriptedWebSocket) send(conn net.Conn, f WebSocketFrame) bool {
	if f.Type == FrameClose {
		_ = writeServerFrame(conn, opClose, closePayload(f.Code, f.Text))
		return false
	}
	wire, err := f.Wire()
	if err != nil {
		return false
	}
	code := byte(opText)
	if f.Type == FrameBinary {
		code = opBinary
	}
	return writeServerFrame(conn, code, wire) == nil
}
