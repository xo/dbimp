package dbimp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6455 makes the accept key with SHA-1.
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The code in this file is a client for the WebSocket protocol (RFC 6455),
// for a database whose server takes its requests as messages of a WebSocket,
// such as Exasol (D200). It is a client only, and it offers no extension, so
// it never compresses a message. The first version reads and writes whole
// messages, up to a limit. It does not stream a message, so a message that
// is near the limit is in memory once, in the slice that Read returns.

// MessageType is the type of a message of a WebSocket.
type MessageType int

// The types of a data message, as RFC 6455 numbers them.
const (
	MessageText   MessageType = 1
	MessageBinary MessageType = 2
)

// String returns the name of the type.
func (mt MessageType) String() string {
	switch mt {
	case MessageText:
		return "text"
	case MessageBinary:
		return "binary"
	}
	return fmt.Sprintf("MessageType(%d)", int(mt))
}

// CloseCode is the status code of the close frame of a WebSocket.
type CloseCode uint16

// The status codes of RFC 6455 that this package sends or that a server often
// sends.
const (
	CloseNormal        CloseCode = 1000
	CloseGoingAway     CloseCode = 1001
	CloseProtocolError CloseCode = 1002
	CloseUnsupported   CloseCode = 1003
	// CloseNoStatus is the code that Read reports for a close frame that
	// holds no code. It is never sent.
	CloseNoStatus    CloseCode = 1005
	CloseInvalidData CloseCode = 1007
	ClosePolicy      CloseCode = 1008
	CloseTooBig      CloseCode = 1009
	CloseInternal    CloseCode = 1011
)

// The limits of a close frame, and how long Close waits for the reply.
const (
	closeStatusLimit  = 5000
	closeReasonLimit  = 123
	closeDrainTimeout = 5 * time.Second
)

// CloseError is the error that Read returns after the server sent a close
// frame, and that Read and Write return from then on.
type CloseError struct {
	// Code is the status code of the close frame.
	Code CloseCode
	// Reason is the text of the close frame.
	Reason string
}

// Error satisfies the error interface.
func (err *CloseError) Error() string {
	if err.Reason == "" {
		return fmt.Sprintf("websocket closed with code %d", err.Code)
	}
	return fmt.Sprintf("websocket closed with code %d: %s", err.Code, err.Reason)
}

// Error values of the WebSocket client.
const (
	// ErrHandshake is the error for a server that did not accept the upgrade
	// to a WebSocket, or that answered it against RFC 6455.
	ErrHandshake Error = "websocket handshake refused"
	// ErrWebSocketProtocol is the error for a frame that the server sent
	// against RFC 6455.
	ErrWebSocketProtocol Error = "websocket protocol error"
	// ErrMessageTooBig is the error for a message that is larger than the
	// limit of the connection.
	ErrMessageTooBig Error = "websocket message is too big"
	// ErrWebSocketClosed is the error for a call on a connection that this
	// side closed.
	ErrWebSocketClosed Error = "websocket is closed"
)

// DefaultMaxMessage is the default limit for the size of a message that Read
// returns, in bytes.
const DefaultMaxMessage = 64 << 20

// webSocketGUID is the value that RFC 6455 adds to the key of the client to
// make the accept key.
const webSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// The opcodes of RFC 6455.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xa
)

// webSocketConfig holds the options of DialWebSocket.
type webSocketConfig struct {
	client       *http.Client
	tls          *tls.Config
	header       http.Header
	origin       string
	subprotocols []string
	maxMessage   int64
}

// WebSocketOption sets one option of DialWebSocket.
type WebSocketOption func(*webSocketConfig)

// WithWebSocketClient sets the client that sends the request for the upgrade.
// Its transport must speak HTTP/1.1 to the server, because a WebSocket cannot
// start over HTTP/2. By default, DialWebSocket makes a client from
// NewTransport, with HTTP/2 turned off, and it follows no redirect. The
// WithWebSocketTLS option is not used with a client of the caller.
func WithWebSocketClient(c *http.Client) WebSocketOption {
	return func(cfg *webSocketConfig) {
		cfg.client = c
	}
}

// WithWebSocketTLS sets the TLS configuration for the default client, as
// NewTransport takes it. It is how the caller turns the check of the
// certificate off, or adds a root certificate.
func WithWebSocketTLS(tc *tls.Config) WebSocketOption {
	return func(cfg *webSocketConfig) {
		cfg.tls = tc
	}
}

// WithWebSocketHeader adds header to the request for the upgrade. A header
// that the protocol needs, such as Upgrade, cannot be replaced.
func WithWebSocketHeader(header http.Header) WebSocketOption {
	return func(cfg *webSocketConfig) {
		if cfg.header == nil {
			cfg.header = make(http.Header)
		}
		for k, v := range header {
			cfg.header[k] = slices.Clone(v)
		}
	}
}

// WithWebSocketOrigin sets the Origin header of the request for the upgrade.
// A client that is not a browser sends none by default.
func WithWebSocketOrigin(origin string) WebSocketOption {
	return func(cfg *webSocketConfig) {
		cfg.origin = origin
	}
}

// WithWebSocketSubprotocols offers the subprotocols in the request for the
// upgrade. The server picks at most one, and WebSocket.Subprotocol returns it.
func WithWebSocketSubprotocols(names ...string) WebSocketOption {
	return func(cfg *webSocketConfig) {
		cfg.subprotocols = slices.Clone(names)
	}
}

// WithWebSocketMaxMessage sets the largest message that Read returns, in
// bytes. The default is DefaultMaxMessage.
func WithWebSocketMaxMessage(n int64) WebSocketOption {
	return func(cfg *webSocketConfig) {
		cfg.maxMessage = n
	}
}

// WebSocket is the client side of a WebSocket connection. One goroutine at a
// time can call Read, and one at a time can call Write. A call to Read can
// run with a call to Write, and Close can run at any time.
//
// The context of a call bounds that call only. If it ends, the call closes the
// connection and returns the error of the context, because a call that stops
// half way leaves the stream of frames in a state that nothing can mend.
type WebSocket struct {
	rwc          io.ReadWriteCloser
	br           *bufio.Reader
	maxMessage   int64
	subprotocol  string
	readMu       sync.Mutex
	writeMu      sync.Mutex
	bw           *bufio.Writer
	scratch      []byte
	stateMu      sync.Mutex
	err          error
	closeSent    bool
	closeOnce    sync.Once
	closeRequest bool
}

// DialWebSocket opens a WebSocket to rawurl. The scheme is ws or wss, and
// http and https mean the same. The context bounds the dial and the
// handshake only, and the connection outlives it.
func DialWebSocket(ctx context.Context, rawurl string, opts ...WebSocketOption) (*WebSocket, error) {
	cfg := &webSocketConfig{maxMessage: DefaultMaxMessage}
	for _, opt := range opts {
		opt(cfg)
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("parsing the websocket url: %w", err)
	}
	switch u.Scheme {
	case "ws", "http":
		u.Scheme = "http"
	case "wss", "https":
		u.Scheme = "https"
	default:
		return nil, fmt.Errorf("dialing the websocket: %w: scheme %q", ErrScheme, u.Scheme)
	}
	client := cfg.client
	if client == nil {
		tr := NewTransport(cfg.tls)
		// A nil map and a true ForceAttemptHTTP2 make a TLS server pick h2,
		// and h2 has no upgrade.
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		client = NewClient(tr, false)
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("making the websocket key: %w", err)
	}
	sentKey := base64.StdEncoding.EncodeToString(key[:])
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("making the websocket request: %w", err)
	}
	for k, v := range cfg.header {
		req.Header[k] = slices.Clone(v)
	}
	if cfg.origin != "" {
		req.Header.Set("Origin", cfg.origin)
	}
	if len(cfg.subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(cfg.subprotocols, ", "))
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", sentKey)
	res, err := Send(client, req)
	if err != nil {
		return nil, fmt.Errorf("dialing the websocket: %w", err)
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		if res.StatusCode < 200 || res.StatusCode > 299 {
			err = CheckStatus(res)
		} else {
			res.Body.Close()
			err = fmt.Errorf("status %d", res.StatusCode)
		}
		return nil, fmt.Errorf("dialing the websocket: %w: %w", ErrHandshake, err)
	}
	rwc, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		res.Body.Close()
		return nil, fmt.Errorf("dialing the websocket: %w: the client gave no connection", ErrHandshake)
	}
	ws := &WebSocket{
		rwc:        rwc,
		br:         bufio.NewReader(rwc),
		bw:         bufio.NewWriter(rwc),
		maxMessage: cfg.maxMessage,
	}
	if err := ws.checkHandshake(res.Header, sentKey, cfg.subprotocols); err != nil {
		rwc.Close()
		return nil, fmt.Errorf("dialing the websocket: %w: %w", ErrHandshake, err)
	}
	return ws, nil
}

// headerHasToken reports whether a comma list in a value of the header name
// has the token, with no regard for case.
func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// Subprotocol returns the subprotocol that the server chose, or an empty
// string if it chose none.
func (ws *WebSocket) Subprotocol() string {
	return ws.subprotocol
}

// Read returns the next data message. It answers a ping with a pong, and it
// drops a pong. After the server sent a close frame, it answers the frame and
// returns a *CloseError, and every later call returns the same error. A
// message larger than the limit closes the connection with the code 1009 and
// returns an error that wraps ErrMessageTooBig. A text message that is not UTF-8
// closes it with the code 1007.
func (ws *WebSocket) Read(ctx context.Context) (MessageType, []byte, error) {
	ws.readMu.Lock()
	defer ws.readMu.Unlock()
	if err := ws.failure(); err != nil {
		return 0, nil, err
	}
	stop := context.AfterFunc(ctx, func() { ws.rwc.Close() })
	defer stop()
	mt, msg, err := ws.readMessage()
	if err != nil {
		return 0, nil, ws.fail(ctx, "reading a websocket message", err)
	}
	return mt, msg, nil
}

// parseClose reads the payload of a close frame. It returns a CloseError
// with the code 0 if the payload is not valid.
func parseClose(p []byte) *CloseError {
	switch {
	case len(p) == 0:
		return &CloseError{Code: CloseNoStatus}
	case len(p) == 1:
		return &CloseError{}
	}
	code := CloseCode(binary.BigEndian.Uint16(p))
	if code < 1000 || code >= closeStatusLimit || code == 1004 || code == 1005 || code == 1006 || (code > 1013 && code < 3000) {
		return &CloseError{}
	}
	if !utf8.Valid(p[2:]) {
		return &CloseError{}
	}
	return &CloseError{Code: code, Reason: string(p[2:])}
}

// Write sends one data message in one frame. Control frames that Read sends
// can run between two calls, but never inside one.
func (ws *WebSocket) Write(ctx context.Context, mt MessageType, data []byte) error {
	if mt != MessageText && mt != MessageBinary {
		return fmt.Errorf("writing a websocket message: %w: type %v", ErrInvalidValue, mt)
	}
	if err := ws.failure(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { ws.rwc.Close() })
	defer stop()
	op := byte(opText)
	if mt == MessageBinary {
		op = opBinary
	}
	if err := ws.writeFrame(op, data); err != nil {
		return ws.fail(ctx, "writing a websocket message", err)
	}
	return nil
}

// Close sends a close frame with code and reason, waits at most five seconds
// for the close frame of the server, and closes the connection. The reason
// can be at most 123 bytes. It is safe to call Close more than once and from
// another goroutine.
func (ws *WebSocket) Close(code CloseCode, reason string) error {
	var err error
	ws.closeOnce.Do(func() {
		ws.stateMu.Lock()
		ws.closeRequest = true
		ws.stateMu.Unlock()
		if len(reason) > closeReasonLimit {
			reason = reason[:closeReasonLimit]
		}
		timer := time.AfterFunc(closeDrainTimeout, func() { ws.rwc.Close() })
		defer timer.Stop()
		sendErr := ws.sendClose(code, reason)
		if sendErr == nil {
			ws.drain()
		}
		closeErr := ws.rwc.Close()
		ws.stateMu.Lock()
		if ws.err == nil {
			ws.err = ErrWebSocketClosed
		}
		ws.stateMu.Unlock()
		err = errors.Join(sendErr, closeErr)
		if err != nil {
			err = fmt.Errorf("closing the websocket: %w", err)
		}
	})
	return err
}

// checkHandshake checks the headers of the response of the server.
func (ws *WebSocket) checkHandshake(h http.Header, sentKey string, offered []string) error {
	if !strings.EqualFold(strings.TrimSpace(h.Get("Upgrade")), "websocket") {
		return errors.New("the Upgrade header is not websocket")
	}
	if !headerHasToken(h, "Connection", "upgrade") {
		return errors.New("the Connection header has no upgrade")
	}
	sum := sha1.Sum([]byte(sentKey + webSocketGUID)) //nolint:gosec // RFC 6455 makes the accept key with SHA-1.
	if h.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		return errors.New("the Sec-WebSocket-Accept header is wrong")
	}
	if len(h.Values("Sec-WebSocket-Extensions")) > 0 {
		return errors.New("the server chose an extension that the client did not offer")
	}
	if sub := h.Get("Sec-WebSocket-Protocol"); sub != "" {
		if !slices.Contains(offered, sub) {
			return fmt.Errorf("the server chose the subprotocol %q that the client did not offer", sub)
		}
		ws.subprotocol = sub
	}
	return nil
}

// readMessage reads frames until it has one whole data message.
func (ws *WebSocket) readMessage() (MessageType, []byte, error) {
	var (
		mt      MessageType
		msg     []byte
		started bool
	)
	for {
		f, err := readFrame(ws.br, ws.maxMessage-int64(len(msg)))
		if err != nil {
			var code CloseCode
			switch {
			case errors.Is(err, ErrMessageTooBig):
				code = CloseTooBig
			case errors.Is(err, ErrWebSocketProtocol):
				code = CloseProtocolError
			}
			if code != 0 {
				ws.abort(code)
			}
			return 0, nil, err
		}
		switch f.opcode {
		case opPing:
			if err := ws.writeFrame(opPong, f.payload); err != nil {
				return 0, nil, err
			}
		case opPong:
		case opClose:
			ce := parseClose(f.payload)
			if ce.Code == 0 {
				ws.abort(CloseProtocolError)
				return 0, nil, fmt.Errorf("%w: bad close frame", ErrWebSocketProtocol)
			}
			if ce.Code != CloseNoStatus {
				ws.abort(ce.Code)
			} else {
				ws.abort(CloseNormal)
			}
			return 0, nil, ce
		case opText, opBinary:
			if started {
				ws.abort(CloseProtocolError)
				return 0, nil, fmt.Errorf("%w: a data frame inside a fragmented message", ErrWebSocketProtocol)
			}
			started = true
			mt = MessageType(f.opcode)
			msg = f.payload
		case opContinuation:
			if !started {
				ws.abort(CloseProtocolError)
				return 0, nil, fmt.Errorf("%w: a continuation with no message", ErrWebSocketProtocol)
			}
			msg = append(msg, f.payload...)
		}
		if (f.opcode == opText || f.opcode == opBinary || f.opcode == opContinuation) && f.fin {
			if mt == MessageText && !utf8.Valid(msg) {
				ws.abort(CloseInvalidData)
				return 0, nil, fmt.Errorf("%w: a text message that is not UTF-8", ErrWebSocketProtocol)
			}
			return mt, msg, nil
		}
	}
}

// drain reads and drops frames until the close frame of the server, or until
// the stream breaks. It waits for a call to Read that is already running.
func (ws *WebSocket) drain() {
	ws.readMu.Lock()
	defer ws.readMu.Unlock()
	for {
		f, err := readFrame(ws.br, ws.maxMessage)
		if err != nil || f.opcode == opClose {
			return
		}
	}
}

// failure returns the error that ended the connection, or nil.
func (ws *WebSocket) failure() error {
	ws.stateMu.Lock()
	defer ws.stateMu.Unlock()
	return ws.err
}

// fail records err as the end of the connection, closes it, and returns the
// error for the caller. If the context ended, it returns the error of the
// context.
func (ws *WebSocket) fail(ctx context.Context, what string, err error) error {
	ws.rwc.Close()
	ws.stateMu.Lock()
	defer ws.stateMu.Unlock()
	if ws.closeRequest {
		err = ErrWebSocketClosed
	} else if cerr := ctx.Err(); cerr != nil {
		err = cerr
	}
	if ws.err == nil {
		ws.err = err
	}
	return fmt.Errorf("%s: %w", what, err)
}

// sendClose writes a close frame once. It ignores a second request.
func (ws *WebSocket) sendClose(code CloseCode, reason string) error {
	ws.stateMu.Lock()
	sent := ws.closeSent
	ws.closeSent = true
	ws.stateMu.Unlock()
	if sent {
		return nil
	}
	p := make([]byte, 2, 2+len(reason))
	binary.BigEndian.PutUint16(p, uint16(code))
	p = append(p, reason...)
	return ws.writeFrame(opClose, p)
}

// abort sends a close frame for an error that this side found. It is a best
// effort: the caller already holds the error that explains the close, and a
// failed write adds nothing to it.
func (ws *WebSocket) abort(code CloseCode) {
	_ = ws.sendClose(code, "")
}

// writeFrame writes one masked frame with the final bit set.
func (ws *WebSocket) writeFrame(opcode byte, payload []byte) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()
	var hdr [14]byte
	hdr[0] = 0x80 | opcode
	n := 2
	switch l := len(payload); {
	case l < 126:
		hdr[1] = 0x80 | byte(l)
	case l <= 0xffff:
		hdr[1] = 0x80 | 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(l))
		n += 2
	default:
		hdr[1] = 0x80 | 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(l))
		n += 8
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("making the mask: %w", err)
	}
	copy(hdr[n:], mask[:])
	n += 4
	if _, err := ws.bw.Write(hdr[:n]); err != nil {
		return err
	}
	if ws.scratch == nil {
		ws.scratch = make([]byte, 32<<10)
	}
	for i := 0; i < len(payload); {
		chunk := ws.scratch[:min(len(ws.scratch), len(payload)-i)]
		for j := range chunk {
			chunk[j] = payload[i+j] ^ mask[(i+j)&3]
		}
		if _, err := ws.bw.Write(chunk); err != nil {
			return err
		}
		i += len(chunk)
	}
	return ws.bw.Flush()
}

// frame is one frame that a server sent.
type frame struct {
	fin     bool
	opcode  byte
	payload []byte
}

// readFrame reads one frame from r. It refuses a frame whose payload is
// longer than limit, before it reads the payload, and it never allocates
// more than the bytes that arrive. A server frame must not be masked, and the
// client offers no extension, so no reserved bit can be set.
func readFrame(r io.Reader, limit int64) (frame, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:2]); err != nil {
		return frame{}, fmt.Errorf("reading a frame header: %w", err)
	}
	f := frame{fin: hdr[0]&0x80 != 0, opcode: hdr[0] & 0x0f}
	if hdr[0]&0x70 != 0 {
		return frame{}, fmt.Errorf("%w: a reserved bit is set", ErrWebSocketProtocol)
	}
	if hdr[1]&0x80 != 0 {
		return frame{}, fmt.Errorf("%w: the server masked a frame", ErrWebSocketProtocol)
	}
	var length uint64
	switch l := hdr[1] & 0x7f; l {
	case 126:
		if _, err := io.ReadFull(r, hdr[:2]); err != nil {
			return frame{}, fmt.Errorf("reading a frame length: %w", err)
		}
		length = uint64(binary.BigEndian.Uint16(hdr[:2]))
	case 127:
		if _, err := io.ReadFull(r, hdr[:8]); err != nil {
			return frame{}, fmt.Errorf("reading a frame length: %w", err)
		}
		length = binary.BigEndian.Uint64(hdr[:8])
		if length>>63 != 0 {
			return frame{}, fmt.Errorf("%w: the length has the top bit set", ErrWebSocketProtocol)
		}
	default:
		length = uint64(l)
	}
	switch f.opcode {
	case opContinuation, opText, opBinary:
	case opClose, opPing, opPong:
		if !f.fin || length > 125 {
			return frame{}, fmt.Errorf("%w: a control frame that is fragmented or longer than 125 bytes", ErrWebSocketProtocol)
		}
	default:
		return frame{}, fmt.Errorf("%w: unknown opcode %d", ErrWebSocketProtocol, f.opcode)
	}
	if limit < 0 || length > uint64(limit) {
		return frame{}, ErrMessageTooBig
	}
	var buf bytes.Buffer
	n, err := io.CopyN(&buf, r, int64(length))
	if err != nil {
		return frame{}, fmt.Errorf("reading a frame payload after %d of %d bytes: %w", n, length, err)
	}
	f.payload = buf.Bytes()
	return f, nil
}
