package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xo/dbimp/dbimptest"
)

func TestEncryptMemberUsesTheKeptKey(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 1024) //nolint:gosec // A small key makes the test fast.
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{"mod": key.N.Text(16), "exp": big.NewInt(int64(key.E)).Text(16)}
	got, err := encryptMember(jsontext.Value(`{"username":"sys","password":"s3cret","n":1.50}`), Encrypt{Member: "password", Modulus: "mod", Exponent: "exp"}, kept)
	if err != nil {
		t.Fatal(err)
	}
	var obj struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(got, &obj); err != nil || obj.Username != "sys" {
		t.Fatalf("got %s, %v", got, err)
	}
	if !strings.HasSuffix(string(got), `"n":1.50}`) {
		t.Errorf("members changed: %s", got)
	}
	enc, err := base64.StdEncoding.DecodeString(obj.Password)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rsa.DecryptPKCS1v15( //nolint:staticcheck // The server asks for PKCS #1 version 1.5.
		nil, key, enc)
	if err != nil || string(plain) != "s3cret" {
		t.Errorf("decrypted %q, %v, want s3cret", plain, err)
	}
}

func TestRecordAWebSocketScript(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024) //nolint:gosec // A small key makes the test fast.
	if err != nil {
		t.Fatal(err)
	}
	// The in-process server answers from files, as a real server would answer.
	serverDir := t.TempDir()
	srvRec := dbimptest.NewRecorder(serverDir, "fake-1", nil)
	login := `{"status":"ok","responseData":{"publicKeyModulus":"` + key.N.Text(16) + `","publicKeyExponent":"` + big.NewInt(int64(key.E)).Text(16) + `"}}`
	for _, ex := range [][2]string{
		{`{"command":"login","protocolVersion":3}`, login},
		{`{"username":"sys"}`, `{"status":"ok","responseData":{"sessionId":42}}`},
		{`{"command":"execute","sqlText":"SELECT 1"}`, `{"status":"ok","n":1}`},
		{`{"command":"disconnect"}`, ``},
	} {
		reply := dbimptest.NewFrame(false, []byte(ex[1]), false)
		if ex[1] == "" {
			reply = dbimptest.WebSocketFrame{Type: dbimptest.FrameClose, Code: 1000}
		}
		if err := srvRec.WriteWebSocket(t.Context(), "main", dbimptest.NewFrame(false, []byte(ex[0]), false), reply); err != nil {
			t.Fatal(err)
		}
	}
	srv := dbimptest.ReplayWebSocket(t, serverDir, nil)
	dir := t.TempDir()
	script := `{
  "driver": "fake",
  "websocket": {"path": "/"},
  "redact": ["password"],
  "requests": [
    {"item": 1, "name": "key", "frame": {"command": "login", "protocolVersion": 3},
     "capture": {"mod": "responseData.publicKeyModulus", "exp": "responseData.publicKeyExponent"}},
    {"item": 1, "name": "login", "frame": {"username": "{{user}}", "password": "{{password}}"},
     "encrypt": {"member": "password", "modulus": "mod", "exponent": "exp"},
     "capture": {"sid": "responseData.sessionId"}},
    {"item": 2, "name": "select", "frame": {"command": "execute", "sqlText": "SELECT 1"}},
    {"item": 3, "name": "disconnect", "frame": {"command": "disconnect"}}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, dbimptest.RequestsName), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("sys", "hunter2")
	if err := run(t.Context(), dir, "fake-1", "", true, map[string]string{dbimptest.Administrator: u.String()}); err != nil {
		t.Fatal(err)
	}
	m, err := dbimptest.ReadManifest(filepath.Join(dir, dbimptest.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 4 {
		t.Fatalf("manifest has %d entries, want 4", len(m.Entries))
	}
	for _, e := range m.Entries {
		b, err := os.ReadFile(filepath.Join(dir, e.File))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "hunter2") {
			t.Errorf("%s holds the password", e.File)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, m.Entries[1].File))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"password": "REDACTED"`) || !strings.Contains(string(b), `"username": "sys"`) {
		t.Errorf("the login is not recorded as REDACTED with the user:\n%s", b)
	}
	last, err := dbimptest.ReadWebSocketExchange(filepath.Join(dir, m.Entries[3].File))
	if err != nil || last.Reply.Type != dbimptest.FrameClose || last.Reply.Code != 1000 {
		t.Errorf("disconnect = %+v, %v, want a close reply with the code 1000", last, err)
	}
}
