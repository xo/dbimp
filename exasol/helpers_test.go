package exasol_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/exasol"
)

// testdata holds the exchanges that step 6 recorded from a real server.
const testdata = "../testdata/exasol"

// recorded returns the exchange that the file with the number n holds, such as
// exasol-2026.2.0-024-websocket-execute.json for 24.
func recorded(tb testing.TB, n int) *dbimptest.WebSocketExchange {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join(testdata, fmt.Sprintf("exasol-2026.2.0-%03d-*.json", n)))
	if err != nil || len(paths) != 1 {
		tb.Fatalf("finding the exchange %d: %v, %d files", n, err, len(paths))
	}
	ex, err := dbimptest.ReadWebSocketExchange(paths[0])
	if err != nil {
		tb.Fatal(err)
	}
	return ex
}

// loginSteps are the two steps of a login, with the answers that the server
// gave: the public key, and the session (recorded: "login with protocol 3").
func loginSteps(tb testing.TB) []dbimptest.WebSocketStep {
	tb.Helper()
	return []dbimptest.WebSocketStep{{Reply: recorded(tb, 1).Reply}, {Reply: recorded(tb, 2).Reply}}
}

// text returns a text frame.
func text(s string) dbimptest.WebSocketFrame {
	return dbimptest.WebSocketFrame{Type: dbimptest.FrameText, Text: s}
}

// open opens the driver against a fake server at addr. The key query adds to
// the DSN, such as "&compression=false".
func open(t *testing.T, addr, query string) *sql.DB {
	t.Helper()
	db, err := sql.Open(exasol.Name, "exasol://sys:exasol@"+addr+"?validateservercertificate=false"+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// replay opens the driver against the recorded exchanges. A statement matches
// the exchange with its text, and a fetch the one with its handle and its
// position.
func replay(t *testing.T, query string) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayWebSocket(t, testdata, dbimptest.WebSocketMatchOn("sqlText", "statementHandle", "resultSetHandle", "startPosition", "resultSetHandles"))
	return open(t, srv.Listener.Addr().String(), query)
}
