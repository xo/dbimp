package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// WebSocketSpec names the WebSocket of a script.
type WebSocketSpec struct {
	// Path is the path of the URL to dial, such as "/".
	Path string `json:"path"`
}

// Interrupt is a frame that the recorder sends while a request waits for its
// reply, such as a message that cancels a long query.
type Interrupt struct {
	// After is how long to wait after the request before the frame.
	After string `json:"after"`
	// Frame is the JSON value to send as a text frame.
	Frame jsontext.Value `json:"frame"`
}

// Encrypt replaces the string member Member of the frame with its encryption
// by RSA with the padding of PKCS #1 version 1.5, in Base64. The public key is
// the two kept values, in hex, that Modulus and Exponent name.
type Encrypt struct {
	Member   string `json:"member"`
	Modulus  string `json:"modulus"`
	Exponent string `json:"exponent"`
}

// closeAbnormal is the code that a recorded close reply holds when the server
// ended the connection with no close frame. RFC 6455 reserves it for this.
const closeAbnormal = 1006

// defaultReadTimeout bounds the wait for a reply when a request has no timeout.
const defaultReadTimeout = time.Minute

// wsSessions holds the open sessions of one run of requests.
type wsSessions struct {
	url      string
	tls      *tls.Config
	sessions map[string]*dbimp.WebSocket
}

func (s *wsSessions) get(ctx context.Context, name string) (*dbimp.WebSocket, error) {
	if ws, ok := s.sessions[name]; ok {
		return ws, nil
	}
	ws, err := dbimp.DialWebSocket(ctx, s.url, dbimp.WithWebSocketTLS(s.tls))
	if err != nil {
		return nil, fmt.Errorf("opening the session %s: %w", name, err)
	}
	s.sessions[name] = ws
	return ws, nil
}

// closeAll closes the sessions that are still open. A session that the server
// already ended is not an error of the recording.
func (s *wsSessions) closeAll() {
	for name, ws := range s.sessions {
		if err := ws.Close(dbimp.CloseNormal, ""); err != nil {
			fmt.Printf("closing the session %s: %v\n", name, err)
		}
	}
	clear(s.sessions)
}

// runWebSocket runs a script for a WebSocket: the setup as the administrator,
// the requests of each principal, and the teardown as the administrator.
func runWebSocket(ctx context.Context, rec *dbimptest.Recorder, dir, release string, insecure bool, principals map[string]string, script Script) error {
	rec.RedactMembers(script.Redact...)
	for _, u := range principals {
		if pu, err := url.Parse(u); err == nil && pu.User != nil {
			pass, _ := pu.User.Password()
			rec.Secret(pass)
		}
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		tc.InsecureSkipVerify = true
	}
	runAs := func(p string, phase string) error {
		base, err := url.Parse(principals[p])
		if err != nil {
			return fmt.Errorf("parsing the URL of the %s user: %w", p, err)
		}
		kept := credentials(base)
		dial := *base
		dial.User = nil
		dial.Path = script.WebSocket.Path
		s := &wsSessions{url: dial.String(), tls: tc, sessions: map[string]*dbimp.WebSocket{}}
		var err2 error
		for _, r := range script.Requests {
			if r.Phase != phase || phase == "" && len(r.Principals) > 0 && !slices.Contains(r.Principals, p) {
				continue
			}
			if err := sendFrame(ctx, rec, s, p, r, kept); err != nil {
				err2 = fmt.Errorf("recording item %d, %s, as the %s user: %w", r.Item, r.Name, p, err)
				break
			}
		}
		s.closeAll()
		return err2
	}
	if err := runAs(dbimptest.Administrator, "setup"); err != nil {
		return err
	}
	for _, p := range []string{dbimptest.Administrator, dbimptest.Ordinary} {
		if principals[p] == "" {
			continue
		}
		if err := runAs(p, ""); err != nil {
			return err
		}
	}
	if err := runAs(dbimptest.Administrator, "teardown"); err != nil {
		return err
	}
	if err := rec.WriteManifest(script.Driver); err != nil {
		return err
	}
	return addAbsent(dir, release, script.Absent)
}

// sendFrame sends the frame of r, reads the reply and records the exchange.
func sendFrame(ctx context.Context, rec *dbimptest.Recorder, s *wsSessions, p string, r Request, kept map[string]string) error {
	if r.Wait != "" {
		d, err := time.ParseDuration(r.Wait)
		if err != nil {
			return fmt.Errorf("parsing the wait: %w", err)
		}
		time.Sleep(d)
	}
	session := r.Session
	if session == "" {
		session = "main"
	}
	if r.Close {
		ws, ok := s.sessions[session]
		if !ok {
			// The server ended the session before, as it does after a login that failed.
			fmt.Printf("item %d, %s: the session %s is not open\n", r.Item, r.Name, session)
			return nil
		}
		delete(s.sessions, session)
		if err := ws.Close(dbimp.CloseNormal, ""); err != nil {
			fmt.Printf("item %d, %s: closing the session: %v\n", r.Item, r.Name, err)
		}
		return nil
	}
	ctx = dbimptest.WithLabel(ctx, r.Item, p)
	ws, err := s.get(ctx, session)
	if err != nil {
		return err
	}
	req, wire, err := buildFrame(r, kept)
	if err != nil {
		return err
	}
	mt := dbimp.MessageText
	if req.Type == dbimptest.FrameBinary {
		mt = dbimp.MessageBinary
	}
	timeout := defaultReadTimeout
	if r.Timeout != "" {
		if timeout, err = time.ParseDuration(r.Timeout); err != nil {
			return fmt.Errorf("parsing the timeout: %w", err)
		}
	}
	var (
		wg        sync.WaitGroup
		interrupt *time.Timer
		intErr    error
		fired     bool
		mu        sync.Mutex
	)
	if r.Interrupt != nil {
		after, err := time.ParseDuration(r.Interrupt.After)
		if err != nil {
			return fmt.Errorf("parsing the interrupt: %w", err)
		}
		wg.Add(1)
		interrupt = time.AfterFunc(after, func() {
			defer wg.Done()
			mu.Lock()
			fired = true
			mu.Unlock()
			f := dbimptest.WebSocketFrame{Type: dbimptest.FrameText, JSON: slices.Clone(r.Interrupt.Frame)}
			if err := f.JSON.Compact(); err != nil {
				intErr = err
				return
			}
			if err := ws.Write(ctx, dbimp.MessageText, f.JSON); err != nil {
				intErr = err
				return
			}
			intErr = rec.WriteWebSocket(ctx, session, f, dbimptest.WebSocketFrame{})
		})
	}
	if err := ws.Write(ctx, mt, wire); err != nil {
		return fmt.Errorf("sending the frame: %w", err)
	}
	if r.NoReply {
		if err := rec.WriteWebSocket(ctx, session, req, dbimptest.WebSocketFrame{}); err != nil {
			return err
		}
		fmt.Printf("item %d, %s: sent, no reply read\n", r.Item, r.Name)
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rmt, msg, err := ws.Read(rctx)
	var reply dbimptest.WebSocketFrame
	var ce *dbimp.CloseError
	switch {
	case errors.As(err, &ce):
		reply = dbimptest.WebSocketFrame{Type: dbimptest.FrameClose, Code: uint16(ce.Code), Text: ce.Reason}
		delete(s.sessions, session)
	case err != nil && rctx.Err() == nil:
		// The server dropped the connection with no close frame. The exchange
		// keeps that as the code 1006, which RFC 6455 keeps for this case.
		fmt.Printf("item %d, %s: the connection ended with no close frame: %v\n", r.Item, r.Name, err)
		reply = dbimptest.WebSocketFrame{Type: dbimptest.FrameClose, Code: closeAbnormal, Text: "the connection ended with no close frame"}
		delete(s.sessions, session)
	case err != nil && r.Timeout != "":
		// The request names a timeout, so the silence of the server is the
		// result. The exchange has no reply, and the read closed the session.
		fmt.Printf("item %d, %s: no reply in %s, as planned\n", r.Item, r.Name, timeout)
		delete(s.sessions, session)
	case err != nil:
		return fmt.Errorf("reading the reply: %w", err)
	default:
		reply = dbimptest.NewFrame(rmt == dbimp.MessageBinary, msg, r.Zlib)
	}
	if interrupt != nil {
		if interrupt.Stop() {
			return errors.New("the reply came before the interrupt")
		}
		wg.Wait()
		mu.Lock()
		ok := fired
		mu.Unlock()
		if !ok || intErr != nil {
			return fmt.Errorf("sending the interrupt: %w", intErr)
		}
	}
	if err := rec.WriteWebSocket(ctx, session, req, reply); err != nil {
		return err
	}
	if len(r.Capture) > 0 && reply.Type != dbimptest.FrameClose && (reply.Type == dbimptest.FrameText || reply.Zlib) {
		// A reply that is an error holds none of the members, and the frames that
		// use them then fail, as they would for a caller.
		if err := capture(reply.Content(), r.Capture, kept); err != nil {
			fmt.Printf("item %d, %s: %v\n", r.Item, r.Name, err)
		}
	}
	fmt.Printf("item %d, %s: %s, %d bytes\n", r.Item, r.Name, reply.Type, len(reply.Content()))
	return nil
}

// buildFrame returns the frame to record for r and the bytes to send.
func buildFrame(r Request, kept map[string]string) (dbimptest.WebSocketFrame, []byte, error) {
	if r.Text != "" {
		f := dbimptest.WebSocketFrame{Type: dbimptest.FrameText, Text: r.Text}
		return f, []byte(r.Text), nil
	}
	if len(r.Frame) == 0 {
		return dbimptest.WebSocketFrame{}, nil, errors.New("the request has no frame")
	}
	if r.Binary {
		var b64 string
		if err := json.Unmarshal(r.Frame, &b64); err != nil {
			return dbimptest.WebSocketFrame{}, nil, fmt.Errorf("reading the binary frame: %w", err)
		}
		b, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return dbimptest.WebSocketFrame{}, nil, fmt.Errorf("reading the binary frame: %w", err)
		}
		return dbimptest.WebSocketFrame{Type: dbimptest.FrameBinary, Binary: b}, b, nil
	}
	use := kept
	if r.Auth == "wrong" {
		use = maps.Clone(kept)
		use["password"] += "-wrong"
	}
	body := splice(expand(r.Frame, use), use)
	if r.Encrypt != nil {
		var err error
		if body, err = encryptMember(body, *r.Encrypt, kept); err != nil {
			return dbimptest.WebSocketFrame{}, nil, err
		}
	}
	if err := body.Compact(); err != nil {
		return dbimptest.WebSocketFrame{}, nil, fmt.Errorf("compacting the frame: %w", err)
	}
	f := dbimptest.NewFrame(false, body, false)
	if !r.Zlib {
		return f, body, nil
	}
	f.Type, f.Zlib = dbimptest.FrameBinary, true
	wire, err := f.Wire()
	if err != nil {
		return dbimptest.WebSocketFrame{}, nil, err
	}
	return f, wire, nil
}

// encryptMember replaces the string member of body that e names with the
// Base64 form of its encryption.
func encryptMember(body jsontext.Value, e Encrypt, kept map[string]string) (jsontext.Value, error) {
	var obj map[string]jsontext.Value
	if err := json.Unmarshal(body, &obj, json.RejectUnknownMembers(false)); err != nil {
		return nil, fmt.Errorf("reading the frame to encrypt: %w", err)
	}
	var plain string
	if err := json.Unmarshal(obj[e.Member], &plain); err != nil {
		return nil, fmt.Errorf("reading the member %s to encrypt: %w", e.Member, err)
	}
	mod, ok := new(big.Int).SetString(kept[e.Modulus], 16)
	if !ok {
		return nil, fmt.Errorf("encrypting %s: the kept value %q is not a hex number", e.Member, e.Modulus)
	}
	exp, ok := new(big.Int).SetString(kept[e.Exponent], 16)
	if !ok || !exp.IsInt64() {
		return nil, fmt.Errorf("encrypting %s: the kept value %q is not a hex number", e.Member, e.Exponent)
	}
	// The login of the protocol that the script records takes this padding, so the recorder must send it.
	enc, err := rsa.EncryptPKCS1v15( //nolint:staticcheck // The server asks for PKCS #1 version 1.5.
		rand.Reader, &rsa.PublicKey{N: mod, E: int(exp.Int64())}, []byte(plain))
	if err != nil {
		return nil, fmt.Errorf("encrypting %s: %w", e.Member, err)
	}
	q, err := jsontext.AppendQuote(nil, base64.StdEncoding.EncodeToString(enc))
	if err != nil {
		return nil, err
	}
	// Replace the value in place, so that the members keep their order.
	return replaceMember(body, e.Member, q)
}

// replaceMember returns the JSON object body with the value of the top level
// member name replaced by value, and the members in their order.
func replaceMember(body jsontext.Value, name string, value jsontext.Value) (jsontext.Value, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(body))
	var out bytes.Buffer
	enc := jsontext.NewEncoder(&out)
	tok, err := dec.ReadToken()
	if err != nil || tok.Kind() != '{' {
		return nil, errors.New("the frame is not a JSON object")
	}
	if err := enc.WriteToken(tok); err != nil {
		return nil, err
	}
	for dec.PeekKind() != '}' {
		n, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		key := n.String()
		if err := enc.WriteToken(n); err != nil {
			return nil, err
		}
		v, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		if key == name {
			v = value
		}
		if err := enc.WriteValue(v); err != nil {
			return nil, err
		}
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// splice writes each kept value in place of the string "{{#name}}" without
// quotes, so that a kept number, such as a handle, is a JSON number.
func splice(body jsontext.Value, kept map[string]string) jsontext.Value {
	s := string(body)
	for name, v := range kept {
		s = strings.ReplaceAll(s, `"{{#`+name+`}}"`, v)
	}
	return jsontext.Value(s)
}
