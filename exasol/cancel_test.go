package exasol_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/exasol"
)

// aborted is the answer of the server to a statement that abortQuery ended
// (recorded: "abort a running query").
func aborted() dbimptest.WebSocketFrame {
	return failure("R0003", "Client requested execution abort.")
}

// RunContract counts goroutines, and so does CheckGoroutines, so these tests do
// not run in parallel.
func TestCancelKeepsTheConnection(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	script := append(loginSteps(t),
		dbimptest.WebSocketStep{Hold: true, Reply: aborted()},
		dbimptest.WebSocketStep{Reply: result(0, 1, []string{"N"}, "[[7]]")},
	)
	srv := dbimptest.ServeWebSocketScript(t, script)
	db := open(t, srv.Addr(), "")
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		select {
		case <-srv.Held():
			cancel()
		case <-ctx.Done():
		}
	}()
	_, err := db.ExecContext(ctx, "SELECT COUNT(*) FROM BIG")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want one that wraps context.Canceled (D211)", err)
	}
	if e, ok := errors.AsType[*exasol.Error](err); !ok || e.Code != exasol.CodeAborted {
		t.Errorf("the error is %v, want one that holds the *exasol.Error with the code R0003", err)
	}
	// The answer of the aborted statement was read, so the next statement gets
	// its own answer on the same connection (D211).
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT 7").Scan(&n); err != nil || n != 7 {
		t.Errorf("the statement after the cancel gave %d, %v, want 7", n, err)
	}
	if open := db.Stats().OpenConnections; open != 1 {
		t.Errorf("%d connections are open, want the one that the cancel kept", open)
	}
	frames := srv.Frames()
	if len(frames) != 5 {
		t.Fatalf("the driver sent %d frames, want 5", len(frames))
	}
	if string(frames[3].Content()) != `{"command":"abortQuery"}` {
		t.Errorf("the frame after the statement is %s, want abortQuery", frames[3].Content())
	}
}

// TestTimeoutOption holds D211: WithTimeout ends the statement with abortQuery,
// and the error is context.DeadlineExceeded.
func TestTimeoutOption(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	script := append(loginSteps(t),
		dbimptest.WebSocketStep{Hold: true, Reply: aborted()},
		dbimptest.WebSocketStep{Reply: result(0, 1, []string{"N"}, "[[7]]")},
	)
	srv := dbimptest.ServeWebSocketScript(t, script)
	db := open(t, srv.Addr(), "")
	db.SetMaxOpenConns(1)
	start := time.Now()
	_, err := db.ExecContext(t.Context(), "SELECT COUNT(*) FROM BIG", exasol.WithTimeout(50*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the error is %v, want one that wraps context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the statement took %v, want about 50ms", d)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT 7").Scan(&n); err != nil || n != 7 {
		t.Errorf("the statement after the timeout gave %d, %v, want 7", n, err)
	}
}

// TestCancelBeforeTheRequest holds D8: a context that ended before the
// statement sends nothing.
func TestCancelBeforeTheRequest(t *testing.T) {
	t.Parallel()
	srv := scripted(t)
	db := open(t, srv.Addr(), "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.ExecContext(ctx, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("the error is %v, want context.Canceled", err)
	}
	if n := len(srv.Frames()); n > 2 {
		t.Errorf("the driver sent %d frames for a context that ended, want none after the login", n-2)
	}
}
