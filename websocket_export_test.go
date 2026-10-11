package dbimp

import (
	"bufio"
	"io"
	"net"
)

// NewTestWebSocket makes a WebSocket over c, with no handshake, for a test that
// holds the frames that it writes.
func NewTestWebSocket(c net.Conn) *WebSocket {
	var rwc io.ReadWriteCloser = c
	return &WebSocket{
		rwc:        rwc,
		br:         bufio.NewReader(rwc),
		bw:         bufio.NewWriter(rwc),
		maxMessage: DefaultMaxMessage,
	}
}

// SendCloseForTest sends a close frame as Close does, and nothing more.
func (ws *WebSocket) SendCloseForTest(code CloseCode, reason string) error {
	return ws.sendClose(code, reason)
}
