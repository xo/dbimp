package dbimptest

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1" //nolint:gosec // RFC 6455 makes the accept key with SHA-1.
	"encoding/base64"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// TransportWebSocket is the value of the member transport of a file that holds
// a WebSocketExchange.
const TransportWebSocket = "websocket"

// The types of a WebSocketFrame.
const (
	// FrameText is a text frame.
	FrameText = "text"
	// FrameBinary is a binary frame.
	FrameBinary = "binary"
	// FrameClose is a close frame, which only a reply can be.
	FrameClose = "close"
)

// inlineLimit is the size above which a JSON frame is written as text and not
// as a JSON value, so that a large frame stays on few lines of its file.
const inlineLimit = 4096

// WebSocketFrame is one message of a WebSocket, as a file holds it. A text
// frame that is JSON and short is in JSON, and any other text frame is in Text.
// A binary frame is in Binary, which the file holds as base64, unless Zlib is
// set. A close frame holds its code in Code and its reason in Text.
type WebSocketFrame struct {
	Type   string         `json:"type,omitzero"`
	JSON   jsontext.Value `json:"json,omitzero"`
	Text   string         `json:"text,omitzero"`
	Binary []byte         `json:"binary,omitzero"`
	// Zlib is true for a binary frame whose bytes are the zlib form of the
	// text of the frame. The file holds the text, and the replay compresses it.
	Zlib bool `json:"zlib,omitzero"`
	// Code is the status code of a close frame.
	Code uint16 `json:"code,omitzero"`
}

// NewFrame returns the frame for a message that crossed the connection. If
// inflate is true and a binary message is zlib data of valid UTF-8, the frame
// holds the text and sets Zlib.
func NewFrame(binaryMsg bool, msg []byte, inflate bool) WebSocketFrame {
	if binaryMsg {
		if inflate {
			if text, err := inflateText(msg); err == nil {
				f := textFrame(FrameBinary, text)
				f.Zlib = true
				return f
			}
		}
		return WebSocketFrame{Type: FrameBinary, Binary: msg}
	}
	return textFrame(FrameText, string(msg))
}

// Content returns the bytes of the message, as the text of a text frame, the
// bytes of a binary frame before compression, or the reason of a close frame.
func (f WebSocketFrame) Content() []byte {
	switch {
	case len(f.JSON) > 0:
		// The file holds the value with indentation, and the message has none.
		v := f.JSON.Clone()
		if err := v.Compact(); err != nil {
			return f.JSON
		}
		return v
	case len(f.Binary) > 0:
		return f.Binary
	}
	return []byte(f.Text)
}

// Wire returns the bytes that cross the connection for the message: the zlib
// form of the content if Zlib is set, and the content otherwise.
func (f WebSocketFrame) Wire() ([]byte, error) {
	if !f.Zlib {
		return f.Content(), nil
	}
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(f.Content()); err != nil {
		return nil, fmt.Errorf("compressing a frame: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("compressing a frame: %w", err)
	}
	return buf.Bytes(), nil
}

// isZero reports whether the frame holds nothing, which a reply that the server
// did not send holds.
func (f WebSocketFrame) isZero() bool {
	return f.Type == ""
}

// textFrame returns a frame of the type typ that holds text, as a JSON value
// if text is short JSON.
func textFrame(typ, text string) WebSocketFrame {
	f := WebSocketFrame{Type: typ}
	if len(text) <= inlineLimit {
		v := jsontext.Value(text)
		if c := v.Clone(); c.Compact() == nil && c.IsValid() && bytes.Equal(c, v) {
			f.JSON = c
			return f
		}
	}
	f.Text = text
	return f
}

func inflateText(b []byte) (string, error) {
	zr, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(out) {
		return "", errors.New("the zlib data is not UTF-8")
	}
	return string(out), nil
}

// WebSocketExchange is one request frame to a real server and the reply
// frame, as a file under testdata/<driver>/ holds it.
type WebSocketExchange struct {
	// Transport is TransportWebSocket.
	Transport string `json:"transport"`
	// Session names the session that the exchange belongs to.
	Session string `json:"session,omitzero"`
	// Request is the frame that the client sent.
	Request WebSocketFrame `json:"request"`
	// Reply is the frame that the server sent, or the zero frame if it sent
	// none.
	Reply WebSocketFrame `json:"reply,omitzero"`
}

// ReadWebSocketExchange reads the exchange in the file at path.
func ReadWebSocketExchange(path string) (*WebSocketExchange, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading a websocket exchange: %w", err)
	}
	var ex WebSocketExchange
	if err := json.Unmarshal(b, &ex, json.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("reading the websocket exchange %s: %w", path, err)
	}
	if ex.Transport != TransportWebSocket {
		return nil, fmt.Errorf("reading the websocket exchange %s: the transport is %q", path, ex.Transport)
	}
	return &ex, nil
}

// isWebSocketFile reports whether the bytes of a file are a WebSocketExchange.
func isWebSocketFile(b []byte) bool {
	var probe struct {
		Transport string `json:"transport"`
	}
	return json.Unmarshal(b, &probe) == nil && probe.Transport == TransportWebSocket
}

// WebSocketTurn is what a WebSocketMatch compares.
type WebSocketTurn struct {
	// Got is the request frame that the fake server received.
	Got WebSocketFrame
	// Prev is the request frame that came before Got on the same connection,
	// or the zero frame.
	Prev WebSocketFrame
	// Exchange is the exchange that is tried.
	Exchange *WebSocketExchange
	// Before is the exchange before Exchange in its session, or nil.
	Before *WebSocketExchange
}

// WebSocketMatch reports whether the exchange of the turn answers the frame.
type WebSocketMatch func(t WebSocketTurn) bool

// frameKey returns the key of a request frame: the string member command of a
// JSON object, "after:" and the command of the frame before it if there is no
// such member, or the bytes of a frame that is not a JSON object.
func frameKey(f, prev WebSocketFrame) string {
	if cmd, ok := command(f); ok {
		return cmd
	}
	if _, ok := members(f); ok {
		cmd, _ := command(prev)
		return "after:" + cmd
	}
	return "raw:" + string(f.Content())
}

// members returns the members of f if it is a JSON object.
func members(f WebSocketFrame) (map[string]jsontext.Value, bool) {
	if f.Type == FrameClose || f.isZero() {
		return nil, false
	}
	var m map[string]jsontext.Value
	if err := json.Unmarshal(f.Content(), &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// command returns the string member command of f.
func command(f WebSocketFrame) (string, bool) {
	m, ok := members(f)
	if !ok {
		return "", false
	}
	var cmd string
	if v, ok := m["command"]; ok && json.Unmarshal(v, &cmd) == nil {
		return cmd, true
	}
	return "", false
}

// DefaultWebSocketMatch matches the key of the frame that arrived with the
// key of the request of the exchange. The key is the string member command of a
// JSON object. A frame with no such member takes the key of the frame before
// it, with the prefix "after:", as the message with the credentials of a login
// has none. A frame that is not a JSON object has its bytes as its key.
func DefaultWebSocketMatch(t WebSocketTurn) bool {
	var before WebSocketFrame
	if t.Before != nil {
		before = t.Before.Request
	}
	return frameKey(t.Got, t.Prev) == frameKey(t.Exchange.Request, before)
}

// WebSocketMatchOn returns a WebSocketMatch that applies DefaultWebSocketMatch
// and also requires each of the named members of the two requests to be equal,
// such as sqlText. A member that both lack is equal.
func WebSocketMatchOn(names ...string) WebSocketMatch {
	return func(t WebSocketTurn) bool {
		if !DefaultWebSocketMatch(t) {
			return false
		}
		got, _ := members(t.Got)
		want, _ := members(t.Exchange.Request)
		for _, name := range names {
			if !sameValue(got[name], want[name]) {
				return false
			}
		}
		return true
	}
}

func sameValue(a, b jsontext.Value) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	return sameBody(a, b)
}

// ReplayWebSocket starts a TLS server that accepts a WebSocket on any path.
// For each request frame that arrives, it sends the reply of the first exchange
// in dir that match accepts, in the order of the file names. If match is nil,
// it uses DefaultWebSocketMatch. A request that no exchange matches fails the
// test and closes the connection. An exchange with no reply sends nothing,
// and a reply that is a close frame closes the connection. The server answers
// a ping and the close of the client. ReplayWebSocket reads every file in dir
// that holds a WebSocketExchange. It closes the server and its connections when
// the test ends. A client trusts srv.Certificate().
func ReplayWebSocket(t *testing.T, dir string, match WebSocketMatch) *httptest.Server {
	t.Helper()
	if match == nil {
		match = DefaultWebSocketMatch
	}
	exchanges, befores := readWebSocketExchanges(t, dir)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		conns []net.Conn
	)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, br, err := upgrade(w, r)
		if err != nil {
			t.Errorf("accepting a websocket in the fake server: %v", err)
			return
		}
		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()
		wg.Add(1)
		defer wg.Done()
		defer func() { _ = conn.Close() }()
		serveWebSocket(t, dir, conn, br, exchanges, befores, match)
	}))
	srv.StartTLS()
	t.Cleanup(func() {
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
		srv.Close()
	})
	return srv
}

func readWebSocketExchanges(t *testing.T, dir string) ([]*WebSocketExchange, []*WebSocketExchange) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("finding the exchanges in %s: %v", dir, err)
	}
	slices.Sort(paths)
	var exchanges []*WebSocketExchange
	for _, path := range paths {
		if base := filepath.Base(path); base == ManifestName || base == RequestsName || base == FeaturesName {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !isWebSocketFile(b) {
			continue
		}
		ex, err := ReadWebSocketExchange(path)
		if err != nil {
			t.Fatal(err)
		}
		exchanges = append(exchanges, ex)
	}
	// befores[i] is the exchange before exchanges[i] in its session.
	befores := make([]*WebSocketExchange, len(exchanges))
	last := map[string]*WebSocketExchange{}
	for i, ex := range exchanges {
		befores[i] = last[ex.Session]
		last[ex.Session] = ex
	}
	return exchanges, befores
}

// serveWebSocket answers the frames of one connection.
func serveWebSocket(t *testing.T, dir string, conn net.Conn, br *bufio.Reader, exchanges, befores []*WebSocketExchange, match WebSocketMatch) {
	t.Helper()
	var prev WebSocketFrame
	for {
		op, msg, err := readClientMessage(br)
		if err != nil {
			return
		}
		switch op {
		case opClose:
			_ = writeServerFrame(conn, opClose, msg)
			return
		case opPing:
			_ = writeServerFrame(conn, opPong, msg)
			continue
		}
		got := NewFrame(op == opBinary, msg, true)
		i := -1
		for j, ex := range exchanges {
			if match(WebSocketTurn{Got: got, Prev: prev, Exchange: ex, Before: befores[j]}) {
				i = j
				break
			}
		}
		prev = got
		if i < 0 {
			t.Errorf("no websocket exchange in %s matches the frame %s", dir, msg)
			_ = writeServerFrame(conn, opClose, closePayload(1008, "no exchange matches"))
			return
		}
		reply := exchanges[i].Reply
		switch reply.Type {
		case "":
		case FrameClose:
			if reply.Code == 1006 {
				// The recording ended with no close frame, so the server ends the same way.
				return
			}
			_ = writeServerFrame(conn, opClose, closePayload(reply.Code, reply.Text))
			return
		default:
			wire, err := reply.Wire()
			if err != nil {
				t.Errorf("making a reply of the fake server: %v", err)
				return
			}
			code := byte(opText)
			if reply.Type == FrameBinary {
				code = opBinary
			}
			if err := writeServerFrame(conn, code, wire); err != nil {
				return
			}
		}
	}
}

// The opcodes of RFC 6455 that the fake server uses.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xa
)

const webSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// upgrade answers the request for a WebSocket and returns the connection.
func upgrade(w http.ResponseWriter, r *http.Request) (net.Conn, *bufio.Reader, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if r.Header.Get("Upgrade") != "websocket" || key == "" {
		http.Error(w, "websocket upgrade needed", http.StatusBadRequest)
		return nil, nil, errors.New("the request is not a websocket upgrade")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return nil, nil, errors.New("the response cannot be hijacked")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("hijacking the connection: %w", err)
	}
	sum := sha1.Sum([]byte(key + webSocketGUID)) //nolint:gosec // RFC 6455 makes the accept key with SHA-1.
	_, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n",
		base64.StdEncoding.EncodeToString(sum[:]))
	if err == nil {
		err = rw.Flush()
	}
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("writing the handshake: %w", err)
	}
	return conn, rw.Reader, nil
}

// readClientMessage reads one message or control frame that the client sent,
// and joins fragments.
func readClientMessage(br *bufio.Reader) (byte, []byte, error) {
	var (
		msg  []byte
		kind byte
	)
	for {
		var h [2]byte
		if _, err := io.ReadFull(br, h[:]); err != nil {
			return 0, nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0xf
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(br, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(br, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		var mask [4]byte
		if h[1]&0x80 != 0 {
			if _, err := io.ReadFull(br, mask[:]); err != nil {
				return 0, nil, err
			}
		}
		if n > 1<<30 {
			return 0, nil, errors.New("the frame is too long")
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(br, p); err != nil {
			return 0, nil, err
		}
		for i := range p {
			p[i] ^= mask[i&3]
		}
		switch op {
		case opClose, opPing, opPong:
			return op, p, nil
		case opText, opBinary:
			kind = op
			msg = p
		case opContinuation:
			msg = append(msg, p...)
		}
		if fin {
			return kind, msg, nil
		}
	}
}

func closePayload(code uint16, reason string) []byte {
	if code == 0 {
		return nil
	}
	p := binary.BigEndian.AppendUint16(nil, code)
	return append(p, reason...)
}

// writeServerFrame writes one unmasked frame with the final bit set.
func writeServerFrame(w io.Writer, op byte, p []byte) error {
	hdr := []byte{0x80 | op}
	switch l := len(p); {
	case l < 126:
		hdr = append(hdr, byte(l))
	case l <= 0xffff:
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(l))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(l))
	}
	_, err := w.Write(append(hdr, p...))
	return err
}

// RedactMembers returns text with the value of each member that is named in
// names written as REDACTED, at any depth. It returns text as it is if text is
// not JSON.
func RedactMembers(text string, names []string) string {
	if len(names) == 0 {
		return text
	}
	dec := jsontext.NewDecoder(bytes.NewReader([]byte(text)))
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	if err := redactValue(dec, enc, names); err != nil {
		return text
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return text
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func redactValue(dec *jsontext.Decoder, enc *jsontext.Encoder, names []string) error {
	switch dec.PeekKind() {
	case '{':
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		if err := enc.WriteToken(tok); err != nil {
			return err
		}
		for dec.PeekKind() != '}' {
			name, err := dec.ReadToken()
			if err != nil {
				return err
			}
			if err := enc.WriteToken(name); err != nil {
				return err
			}
			if slices.Contains(names, name.String()) {
				if _, err := dec.ReadValue(); err != nil {
					return err
				}
				if err := enc.WriteToken(jsontext.String(redacted)); err != nil {
					return err
				}
				continue
			}
			if err := redactValue(dec, enc, names); err != nil {
				return err
			}
		}
	case '[':
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		if err := enc.WriteToken(tok); err != nil {
			return err
		}
		for dec.PeekKind() != ']' {
			if err := redactValue(dec, enc, names); err != nil {
				return err
			}
		}
	default:
		v, err := dec.ReadValue()
		if err != nil {
			return err
		}
		return enc.WriteValue(v)
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	return enc.WriteToken(tok)
}

// RedactMembers adds names to the members that the Recorder writes as REDACTED
// in the frames of a WebSocket, at any depth, such as the password of a login.
func (r *Recorder) RedactMembers(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.members = append(r.members, names...)
}

// WriteWebSocket writes one exchange of a WebSocket into the directory of
// the Recorder. The frame reply can be the zero frame for a request that the
// server did not answer. It redacts the passwords that Secret names and the
// members that RedactMembers names, and it labels the exchange as RoundTrip
// does. The frames that it gets hold the message as sent, so a frame of text or
// of JSON is redacted and a binary frame is not.
func (r *Recorder) WriteWebSocket(ctx context.Context, session string, req, reply WebSocketFrame) error {
	req, reply = r.redactFrame(req), r.redactFrame(reply)
	ex := WebSocketExchange{Transport: TransportWebSocket, Session: session, Request: req, Reply: reply}
	cmd, _ := command(req)
	l, _ := ctx.Value(labelKey{}).(label)
	return r.write(&ex, "websocket "+cmd, l)
}

// redactFrame returns f with each secret and each named member hidden.
func (r *Recorder) redactFrame(f WebSocketFrame) WebSocketFrame {
	if f.Type != FrameText && !f.Zlib {
		return f
	}
	r.mu.Lock()
	names := slices.Clone(r.members)
	r.mu.Unlock()
	text := string(r.hide(f.Content()))
	text = RedactMembers(text, names)
	out := textFrame(f.Type, text)
	out.Zlib = f.Zlib
	return out
}
