package dbimp

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"testing"
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
			mt, msg, err := ws.readMessage()
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
