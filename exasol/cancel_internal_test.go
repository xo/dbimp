package exasol

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
)

// TestAbortGrace holds D211: if the server does not answer an aborted
// statement, the driver closes the session after the grace period, so a hung
// server cannot hang the caller, and the connection is not used again.
func TestAbortGrace(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	ex1, err := dbimptest.ReadWebSocketExchange("../testdata/exasol/exasol-2026.2.0-001-websocket-login.json")
	if err != nil {
		t.Fatal(err)
	}
	ex2, err := dbimptest.ReadWebSocketExchange("../testdata/exasol/exasol-2026.2.0-002-websocket.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := dbimptest.ServeWebSocketScript(t, []dbimptest.WebSocketStep{{Reply: ex1.Reply}, {Reply: ex2.Reply}, {Hold: true, Mute: true}})
	db := sql.OpenDB(NewConnector(Config{Host: "127.0.0.1", Port: portOf(t, srv.Addr()), User: "sys", Password: "exasol", SkipVerify: true}))
	t.Cleanup(func() { db.Close() })
	c, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Raw(func(dc any) error {
		cn, ok := dc.(*conn)
		if !ok {
			return fmt.Errorf("the connection is %T, want *conn", dc)
		}
		cn.s.grace = 100 * time.Millisecond
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-srv.Held():
			cancel()
		case <-ctx.Done():
		}
	}()
	start := time.Now()
	_, err = c.ExecContext(ctx, "SELECT COUNT(*) FROM BIG")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want one that wraps context.Canceled", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the statement took %v, want about the grace period of 100ms", d)
	}
	var valid bool
	_ = c.Raw(func(dc any) error {
		if cn, ok := dc.(*conn); ok {
			valid = cn.IsValid()
		}
		return nil
	})
	if valid {
		t.Error("the session is still valid after the server did not answer the abort")
	}
}

// portOf returns the port of the address host:port.
func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
