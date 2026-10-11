package dbimptest_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

func text(s string) dbimptest.WebSocketFrame { return dbimptest.NewFrame(false, []byte(s), false) }

// writeExchanges records the exchanges into a new folder and returns it.
func writeExchanges(t *testing.T, redact []string, exchanges ...[2]dbimptest.WebSocketFrame) string {
	t.Helper()
	dir := t.TempDir()
	rec := dbimptest.NewRecorder(dir, "demo-1.0", nil)
	rec.RedactMembers(redact...)
	rec.Secret("hunter2")
	for _, ex := range exchanges {
		ctx := dbimptest.WithLabel(t.Context(), 1, dbimptest.Administrator)
		if err := rec.WriteWebSocket(ctx, "main", ex[0], ex[1]); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func dial(t *testing.T, srv *httptest.Server) *dbimp.WebSocket {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	ws, err := dbimp.DialWebSocket(t.Context(), srv.URL, dbimp.WithWebSocketTLS(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close(dbimp.CloseNormal, "") })
	return ws
}

func call(t *testing.T, ws *dbimp.WebSocket, mt dbimp.MessageType, msg []byte) (dbimp.MessageType, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := ws.Write(ctx, mt, msg); err != nil {
		t.Fatal(err)
	}
	got, reply, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return got, reply
}

func TestRecordedWebSocketExchangeIsOneJSONFile(t *testing.T) {
	t.Parallel()
	dir := writeExchanges(t, []string{"password"}, [2]dbimptest.WebSocketFrame{
		text(`{"command":"login","protocolVersion":3}`), text(`{"status":"ok"}`),
	}, [2]dbimptest.WebSocketFrame{
		text(`{"username":"sys","password":"hunter2","attributes":{"password":{"x":1}}}`), text(`{"status":"ok","password":"leak"}`),
	}, [2]dbimptest.WebSocketFrame{
		text(`not json`), {},
	})
	paths, err := filepath.Glob(filepath.Join(dir, "demo-1.0-*.json"))
	if err != nil || len(paths) != 3 {
		t.Fatalf("recorded %v, %v, want three files", paths, err)
	}
	if !strings.HasSuffix(paths[0], "-001-websocket-login.json") {
		t.Errorf("first file is %s, want a name from the command", filepath.Base(paths[0]))
	}
	b, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, leak := range []string{"hunter2", "leak"} {
		if strings.Contains(got, leak) {
			t.Errorf("the file holds %q:\n%s", leak, got)
		}
	}
	if strings.Count(got, "REDACTED") != 3 {
		t.Errorf("the file has %d REDACTED, want 3 (two in the request, one in the reply):\n%s", strings.Count(got, "REDACTED"), got)
	}
	ex, err := dbimptest.ReadWebSocketExchange(paths[2])
	if err != nil {
		t.Fatal(err)
	}
	if ex.Reply.Type != "" || ex.Request.Text != "not json" {
		t.Errorf("exchange = %+v, want text that is not JSON and no reply", ex)
	}
}

func TestReplayWebSocketAnswersByCommand(t *testing.T) {
	dir := writeExchanges(t, []string{"password"},
		[2]dbimptest.WebSocketFrame{text(`{"command":"login","protocolVersion":3}`), text(`{"status":"ok","responseData":{"publicKeyModulus":"c0ffee"}}`)},
		[2]dbimptest.WebSocketFrame{text(`{"username":"sys","password":"hunter2"}`), text(`{"status":"ok","responseData":{"sessionId":7}}`)},
		[2]dbimptest.WebSocketFrame{text(`{"command":"execute","sqlText":"SELECT 1"}`), text(`{"status":"ok","n":1}`)},
		[2]dbimptest.WebSocketFrame{text(`{"command":"execute","sqlText":"SELECT 2"}`), text(`{"status":"ok","n":2}`)},
		[2]dbimptest.WebSocketFrame{{Type: dbimptest.FrameBinary, Binary: []byte{1, 2, 3}}, {Type: dbimptest.FrameBinary, Binary: []byte{9}}},
		[2]dbimptest.WebSocketFrame{text(`{"command":"abortQuery"}`), {}},
		[2]dbimptest.WebSocketFrame{text(`{"command":"disconnect"}`), {Type: dbimptest.FrameClose, Code: 1000, Text: "bye"}},
	)
	srv := dbimptest.ReplayWebSocket(t, dir, nil)
	ws := dial(t, srv)
	_, reply := call(t, ws, dbimp.MessageText, []byte(`{"command":"login","protocolVersion":3}`))
	if !strings.Contains(string(reply), "c0ffee") {
		t.Errorf("login reply = %s", reply)
	}
	// The credentials have no command, and they match the exchange after the login.
	// Their password is not the recorded one, and it does not matter.
	_, reply = call(t, ws, dbimp.MessageText, []byte(`{"username":"sys","password":"another key, another text"}`))
	if !strings.Contains(string(reply), `"sessionId":7`) {
		t.Errorf("credentials reply = %s", reply)
	}
	// The default matching takes the first exchange with the command.
	_, reply = call(t, ws, dbimp.MessageText, []byte(`{"command":"execute","sqlText":"SELECT 2"}`))
	if !strings.Contains(string(reply), `"n":1`) {
		t.Errorf("execute reply = %s, want the first execute", reply)
	}
	mt, reply := call(t, ws, dbimp.MessageBinary, []byte{1, 2, 3})
	if mt != dbimp.MessageBinary || len(reply) != 1 || reply[0] != 9 {
		t.Errorf("binary reply = %v %v", mt, reply)
	}
	// An exchange with no reply sends nothing, and the next one answers.
	if err := ws.Write(t.Context(), dbimp.MessageText, []byte(`{"command":"abortQuery"}`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if _, _, err := ws.Read(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read after a request with no reply: %v, want a timeout", err)
	}
}

func TestReplayWebSocketCloseReply(t *testing.T) {
	dir := writeExchanges(t, nil,
		[2]dbimptest.WebSocketFrame{text(`{"command":"disconnect"}`), {Type: dbimptest.FrameClose, Code: 1000, Text: "bye"}},
	)
	ws := dial(t, dbimptest.ReplayWebSocket(t, dir, nil))
	if err := ws.Write(t.Context(), dbimp.MessageText, []byte(`{"command":"disconnect"}`)); err != nil {
		t.Fatal(err)
	}
	_, _, err := ws.Read(t.Context())
	var ce *dbimp.CloseError
	if !errors.As(err, &ce) || ce.Code != 1000 || ce.Reason != "bye" {
		t.Errorf("err = %v, want a close with the code 1000 and the reason bye", err)
	}
}

func TestReplayWebSocketMatchOnMembers(t *testing.T) {
	dir := writeExchanges(t, nil,
		[2]dbimptest.WebSocketFrame{text(`{"command":"execute","sqlText":"SELECT 1"}`), text(`{"n":1}`)},
		[2]dbimptest.WebSocketFrame{text(`{"command":"execute","sqlText":"SELECT 2"}`), text(`{"n":2}`)},
	)
	ws := dial(t, dbimptest.ReplayWebSocket(t, dir, dbimptest.WebSocketMatchOn("sqlText")))
	_, reply := call(t, ws, dbimp.MessageText, []byte(`{"command":"execute","sqlText":"SELECT 2"}`))
	if string(reply) != `{"n":2}` {
		t.Errorf("reply = %s, want the second exchange", reply)
	}
}

func TestZlibFramesAreRecordedAsTextAndReplayedCompressed(t *testing.T) {
	req, reply := text(`{"command":"execute","sqlText":"SELECT 1"}`), text(`{"status":"ok"}`)
	req.Type, req.Zlib = dbimptest.FrameBinary, true
	reply.Type, reply.Zlib = dbimptest.FrameBinary, true
	dir := writeExchanges(t, nil, [2]dbimptest.WebSocketFrame{req, reply})
	paths, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"zlib": true`) || strings.Contains(string(b), `"binary":`) {
		t.Errorf("the file holds the text and sets zlib:\n%s", b)
	}
	ws := dial(t, dbimptest.ReplayWebSocket(t, dir, nil))
	wire, err := req.Wire()
	if err != nil {
		t.Fatal(err)
	}
	mt, got := call(t, ws, dbimp.MessageBinary, wire)
	frame := dbimptest.NewFrame(mt == dbimp.MessageBinary, got, true)
	if mt != dbimp.MessageBinary || !frame.Zlib || string(frame.Content()) != `{"status":"ok"}` {
		t.Errorf("reply = %v %q, want zlib data of the recorded text", mt, got)
	}
}

func TestLargeFramesStayAsText(t *testing.T) {
	t.Parallel()
	big := `{"data":"` + strings.Repeat("x", 5000) + `"}`
	f := dbimptest.NewFrame(false, []byte(big), false)
	if len(f.JSON) != 0 || f.Text != big {
		t.Errorf("a frame over the limit is in text, got json %d bytes", len(f.JSON))
	}
	s := dbimptest.NewFrame(false, []byte(`{"a": 1}`), false)
	if len(s.JSON) != 0 || s.Text != `{"a": 1}` {
		t.Errorf("a JSON frame that is not compact keeps its exact text, got %+v", s)
	}
	if v := dbimptest.NewFrame(false, []byte(`{"a":1}`), false).JSON; string(v) != `{"a":1}` {
		t.Errorf("a short compact JSON frame is a value, got %s", v)
	}
}

func TestRedactMembers(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`{"a":1,"password":"x","b":[{"password":{"k":[1]}}],"c":1.50}`: `{"a":1,"password":"REDACTED","b":[{"password":"REDACTED"}],"c":1.50}`,
		`not json`:        `not json`,
		`{"a":1} {"b":2}`: `{"a":1} {"b":2}`,
	} {
		if got := dbimptest.RedactMembers(in, []string{"password"}); got != want {
			t.Errorf("RedactMembers(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestHTTPReplaySkipsWebSocketFiles(t *testing.T) {
	dir := writeExchanges(t, nil, [2]dbimptest.WebSocketFrame{text(`{"command":"x"}`), text(`{}`)})
	srv := dbimptest.Replay(t, dir, nil)
	if srv.URL == "" {
		t.Fatal("no server")
	}
}
