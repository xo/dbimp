package exasol_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"encoding/base64"
	"encoding/json/v2"
	"math/big"
	"net"
	"strconv"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/exasol"
)

// rawSession is a session that the test drives frame by frame, for a command of
// the WebSocket API that the driver never sends, such as executeBatch (the
// feature entries of testdata/exasol/features.json that no statement of
// database/sql reaches).
type rawSession struct {
	t  *testing.T
	ws *dbimp.WebSocket
}

// dialRaw opens a WebSocket to the server of the DSN, and logs in with
// protocol, except that the test passes finish to send its own messages from
// the start, such as loginToken.
func dialRaw(t *testing.T, dsn string, protocol int, finish func(r *rawSession, cfg *exasol.Config)) *rawSession {
	t.Helper()
	cfg, err := exasol.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.SkipVerify} //nolint:gosec // The DSN of the test asks for it, for the certificate that dbrun makes.
	ws, err := dbimp.DialWebSocket(t.Context(), "wss://"+net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))+"/", dbimp.WithWebSocketTLS(tc))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close(dbimp.CloseNormal, "") })
	r := &rawSession{t: t, ws: ws}
	if finish != nil {
		finish(r, cfg)
		return r
	}
	key := r.call(map[string]any{"command": "login", "protocolVersion": protocol})
	data, _ := key["responseData"].(map[string]any)
	modulus, _ := data["publicKeyModulus"].(string)
	exponent, _ := data["publicKeyExponent"].(string)
	n, _ := new(big.Int).SetString(modulus, 16)
	e, _ := new(big.Int).SetString(exponent, 16)
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, &rsa.PublicKey{N: n, E: int(e.Int64())}, []byte(cfg.Password)) //nolint:staticcheck // The WebSocket API of Exasol decrypts with PKCS #1 version 1.5 only.
	if err != nil {
		t.Fatal(err)
	}
	reply := r.call(map[string]any{
		"username": cfg.User, "password": base64.StdEncoding.EncodeToString(enc), "useCompression": false,
		"clientName": "dbimp test", "attributes": map[string]any{"autocommit": true},
	})
	if reply["status"] != "ok" {
		t.Fatalf("the login answered %v", reply)
	}
	return r
}

// call sends the frame and returns the answer.
func (r *rawSession) call(frame map[string]any) map[string]any {
	r.t.Helper()
	b, err := json.Marshal(frame)
	if err != nil {
		r.t.Fatal(err)
	}
	if err := r.ws.Write(r.t.Context(), dbimp.MessageText, b); err != nil {
		r.t.Fatal(err)
	}
	_, msg, err := r.ws.Read(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(msg, &out); err != nil {
		r.t.Fatalf("reading the answer %s: %v", msg, err)
	}
	return out
}

// errorCode returns the code of the exception of an answer, or "".
func errorCode(answer map[string]any) string {
	ex, _ := answer["exception"].(map[string]any)
	code, _ := ex["sqlCode"].(string)
	return code
}

// results returns the list of results of an answer.
func results(t *testing.T, answer map[string]any) []any {
	t.Helper()
	data, ok := answer["responseData"].(map[string]any)
	if !ok {
		t.Fatalf("the answer %v has no responseData", answer)
	}
	list, _ := data["results"].([]any)
	return list
}
