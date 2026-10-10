package spanner //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// txHandler answers beginTransaction with the id tx1, a statement with a stream
// of one row of one column, a DML statement with a count of 2, a commit with a
// timestamp and a rollback with nothing.
func txHandler(c call) (int, string) {
	switch c.verb {
	case "beginTransaction":
		return http.StatusOK, `{"id":"dHgx"}`
	case "executeStreamingSql":
		if strings.Contains(string(c.body), "INSERT") || strings.Contains(string(c.body), "UPDATE") {
			return http.StatusOK, dmlStream("2")
		}
		return http.StatusOK, streamOf(intField("a"), `"7"`)
	case "commit":
		return http.StatusOK, `{"commitTimestamp":"2026-10-10T05:55:30.384908Z"}`
	case "rollback":
		return http.StatusOK, `{}`
	}
	return http.StatusNotFound, `{}`
}

// TestTransactionRequests holds D191 item 6: BeginTx calls beginTransaction for
// a transaction that writes, each statement of it names the transaction and a
// seqno that grows, Commit sends commit, and Rollback sends rollback.
func TestTransactionRequests(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tx.ExecContext(t.Context(), "UPDATE t SET a = 1")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 2 {
		t.Errorf("RowsAffected is %d, %v, want 2", n, err)
	}
	var a int64
	if err := tx.QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a); err != nil || a != 7 {
		t.Errorf("the read in the transaction is %d, %v, want 7", a, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, want := f.verbs(), []string{"beginTransaction", "executeStreamingSql", "executeStreamingSql", "commit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the requests are %q, want %q", got, want)
	}
	if got := f.last("beginTransaction"); !reflect.DeepEqual(got, map[string]any{"options": map[string]any{"readWrite": map[string]any{}}}) {
		t.Errorf("the body of beginTransaction is %v, want readWrite", got)
	}
	stmts := f.bodies("executeStreamingSql")
	for i, st := range stmts {
		if !reflect.DeepEqual(st["transaction"], map[string]any{"id": "dHgx"}) || st["seqno"] != string(rune('1'+i)) {
			t.Errorf("statement %d has the transaction %v and the seqno %v, want the id dHgx and the seqno %d", i, st["transaction"], st["seqno"], i+1)
		}
	}
	if got := f.last("commit"); !reflect.DeepEqual(got, map[string]any{"transactionId": "dHgx"}) {
		t.Errorf("the body of commit is %v", got)
	}

	tx, err = db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := f.last("rollback"); !reflect.DeepEqual(got, map[string]any{"transactionId": "dHgx"}) {
		t.Errorf("the body of rollback is %v", got)
	}
}

// TestReadOnlyTransaction holds D191 item 6: a read-only transaction calls
// beginTransaction with readOnly, and its commit and its rollback send nothing,
// because the server refuses the commit of a read-only transaction (recorded:
// "commit a read only transaction").
func TestReadOnlyTransaction(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	for _, end := range []func(*sql.Tx) error{(*sql.Tx).Commit, (*sql.Tx).Rollback} {
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		var a int64
		if err := tx.QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a); err != nil {
			t.Fatal(err)
		}
		if err := end(tx); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := f.verbs(), []string{"beginTransaction", "executeStreamingSql", "beginTransaction", "executeStreamingSql"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	want := map[string]any{"options": map[string]any{"readOnly": map[string]any{"strong": true}}}
	if got := f.last("beginTransaction"); !reflect.DeepEqual(got, want) {
		t.Errorf("the body of beginTransaction is %v, want %v", got, want)
	}
}

// TestIsolationLevels holds that the default and serializable levels send no
// level, repeatable read sends REPEATABLE_READ, and any other level is
// dbimp.ErrNotSupported before any request.
func TestIsolationLevels(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	for _, level := range []sql.IsolationLevel{sql.LevelDefault, sql.LevelSerializable, sql.LevelRepeatableRead} {
		tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: level})
		if err != nil {
			t.Fatalf("%s: %v", level, err)
		}
		_ = tx.Rollback()
		opts, _ := f.last("beginTransaction")["options"].(map[string]any)
		got, has := opts["isolationLevel"]
		if want := level == sql.LevelRepeatableRead; has != want || want && got != "REPEATABLE_READ" {
			t.Errorf("%s sent the isolation level %v, %v", level, got, has)
		}
	}
	n := f.count("beginTransaction")
	for _, level := range []sql.IsolationLevel{sql.LevelReadUncommitted, sql.LevelReadCommitted, sql.LevelSnapshot, sql.LevelLinearizable} {
		if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: level}); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s gave %v, want dbimp.ErrNotSupported", level, err)
		}
	}
	if _, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a read-only transaction with a level gave %v, want dbimp.ErrNotSupported", err)
	}
	if f.count("beginTransaction") != n {
		t.Errorf("the driver sent beginTransaction for a level that it refuses")
	}
}

// TestAbortedCommit holds D191 item 6: an ABORTED answer, HTTP 409 with a
// retryDelay, goes to the caller as an error that wraps ErrAborted, with the
// delay that the server asks for (recorded: "commit the older transaction").
// The driver never runs the transaction again.
func TestAbortedCommit(t *testing.T) {
	t.Parallel()
	aborted := file(t, 324).Response
	if aborted.Status != http.StatusConflict {
		t.Fatalf("the recording has the status %d, want 409", aborted.Status)
	}
	f := newFake(t, func(c call) (int, string) {
		if c.verb == "commit" {
			return aborted.Status, aborted.Body
		}
		return txHandler(c)
	})
	db := f.db()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = tx.Commit()
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("the error is %v, want one that wraps ErrAborted", err)
	}
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.RetryDelay != 117856360*time.Nanosecond || serr.HTTPStatus != http.StatusConflict {
		t.Errorf("the error is %+v, want HTTP 409 and the delay 0.117856360s", serr)
	}
	if f.count("commit") != 1 {
		t.Errorf("the driver sent commit %d times, want 1", f.count("commit"))
	}
}

// TestPrecommitToken holds the commit on a multiplexed session (recorded:
// "commit on the multiplexed session with the precommit token"): the commit
// sends the token with the highest seqNum of the statements, and an answer with
// a new token and no commit timestamp makes the driver send the commit once more
// with that token.
func TestPrecommitToken(t *testing.T) {
	t.Parallel()
	var commits atomic.Int32
	f := newFake(t, func(c call) (int, string) {
		switch c.verb {
		case "executeStreamingSql":
			if strings.Contains(string(c.body), `"seqno":"1"`) {
				return http.StatusOK, `[{"metadata":{"rowType":{}},"stats":{"rowCountExact":"1"},"precommitToken":{"precommitToken":"AAA","seqNum":1},"last":true}]`
			}
			return http.StatusOK, `[{"metadata":{"rowType":{}},"stats":{"rowCountExact":"1"},"precommitToken":{"precommitToken":"BBB","seqNum":2},"last":true}]`
		case "commit":
			if commits.Add(1) == 1 {
				return http.StatusOK, `{"precommitToken":{"precommitToken":"CCC","seqNum":3}}`
			}
			return http.StatusOK, `{"commitTimestamp":"2026-10-10T05:55:58.124462Z"}`
		}
		return txHandler(c)
	})
	db := f.db()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := tx.ExecContext(t.Context(), "UPDATE t SET a = 1"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got := f.bodies("commit")
	if len(got) != 2 {
		t.Fatalf("the driver sent %d commits, want 2", len(got))
	}
	want := []any{
		map[string]any{"precommitToken": "BBB", "seqNum": float64(2)},
		map[string]any{"precommitToken": "CCC", "seqNum": float64(3)},
	}
	for i := range got {
		if !reflect.DeepEqual(got[i]["precommitToken"], want[i]) {
			t.Errorf("commit %d sent the token %v, want %v", i, got[i]["precommitToken"], want[i])
		}
	}
}

// TestCommitWithNoTimestamp holds that a second answer with no commit timestamp
// is an error, so the driver never loops.
func TestCommitWithNoTimestamp(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(c call) (int, string) {
		if c.verb == "commit" {
			return http.StatusOK, `{"precommitToken":{"precommitToken":"CCC","seqNum":3}}`
		}
		return txHandler(c)
	})
	tx, err := f.db().BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("the error is %v, want one that wraps dbimp.ErrInvalidValue", err)
	}
	if n := f.count("commit"); n != 2 {
		t.Errorf("the driver sent commit %d times, want 2", n)
	}
}

// TestTransactionOptions holds that the options of the context reach the
// transaction: WithParameter("options", ...) replaces the options of
// beginTransaction, and the members of the commit go in the commit.
func TestTransactionOptions(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	ctx := WithOptions(t.Context(),
		WithParameter("options", map[string]any{"readWrite": map[string]any{"readLockMode": "OPTIMISTIC"}, "excludeTxnFromChangeStreams": true}),
		WithParameter("maxCommitDelay", "0.05s"),
		WithParameter("returnCommitStats", true),
	)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wantBegin := map[string]any{"options": map[string]any{"readWrite": map[string]any{"readLockMode": "OPTIMISTIC"}, "excludeTxnFromChangeStreams": true}}
	if got := f.last("beginTransaction"); !reflect.DeepEqual(got, wantBegin) {
		t.Errorf("the body of beginTransaction is %v, want %v", got, wantBegin)
	}
	wantCommit := map[string]any{"transactionId": "dHgx", "maxCommitDelay": "0.05s", "returnCommitStats": true}
	if got := f.last("commit"); !reflect.DeepEqual(got, wantCommit) {
		t.Errorf("the body of commit is %v, want %v", got, wantCommit)
	}
}

// TestNoTransactionInsideATransaction holds that a second BeginTx on one
// connection is an error, and that a DDL statement in a transaction is
// dbimp.ErrNotSupported, before any request.
func TestNoTransactionInsideATransaction(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.BeginTx(t.Context(), nil); err == nil {
		t.Error("a second transaction on the connection did not fail")
	}
	if _, err := tx.ExecContext(t.Context(), "CREATE TABLE x (id INT64) PRIMARY KEY (id)"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("a DDL statement in a transaction gave %v, want dbimp.ErrNotSupported", err)
	}
	if f.count("ddl") != 0 {
		t.Error("the driver sent a DDL statement from a transaction")
	}
	_ = tx.Rollback()
}

// TestRollbackAfterTheContextEnds holds that the rollback of a transaction
// whose context ended is still sent, with a context that does not end (D67).
func TestRollbackAfterTheContextEnds(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	ctx, cancel := context.WithCancel(t.Context())
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	// database/sql rolls back when the context of BeginTx ends, so wait until it
	// did.
	deadline := time.Now().Add(5 * time.Second)
	for f.count("rollback") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.count("rollback") != 1 {
		t.Errorf("the driver sent rollback %d times after the context ended, want 1", f.count("rollback"))
	}
	if err := tx.Commit(); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("Commit after the end of the context gave %v, want sql.ErrTxDone", err)
	}
}

// TestLostSessionInATransaction holds that a transaction whose session is gone
// gets an error that wraps ErrSessionNotFound and driver.ErrBadConn, and that
// the connector then makes a new session for the next statement.
func TestLostSessionInATransaction(t *testing.T) {
	t.Parallel()
	gone := file(t, 376).Response
	var lost atomic.Bool
	lost.Store(true)
	f := newFake(t, func(c call) (int, string) {
		if c.verb == "executeStreamingSql" && lost.CompareAndSwap(true, false) {
			return gone.Status, gone.Body
		}
		return txHandler(c)
	})
	db := f.db()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(t.Context(), "UPDATE t SET a = 1")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("the error is %v, want one that wraps ErrSessionNotFound", err)
	}
	_ = tx.Rollback()
	var a int64
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM t").Scan(&a); err != nil || a != 7 {
		t.Errorf("the statement after the lost session is %d, %v, want 7", a, err)
	}
}
