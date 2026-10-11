package exasol_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/exasol"
)

// TestOptions holds D109 for the options of the driver: each one reaches the
// request of the statement, and one that the server cannot honor fails before
// anything is sent.
func TestOptions(t *testing.T) {
	t.Parallel()
	one := result(0, 1, []string{"N"}, "[[1]]")
	t.Run("WithParameter", func(t *testing.T) {
		t.Parallel()
		srv := scripted(t, one)
		db := open(t, srv.Addr(), "")
		attrs := map[string]any{"resultSetMaxRows": 3}
		if _, _, err := scan(t, db, "SELECT N FROM T", exasol.WithParameter("attributes", attrs)); err != nil {
			t.Fatal(err)
		}
		f := member(t, srv.Frames()[2])
		if f["command"] != "execute" || f["sqlText"] != "SELECT N FROM T" || !reflect.DeepEqual(f["attributes"], map[string]any{"resultSetMaxRows": 3.0}) {
			t.Errorf("the request is %v, want execute with the attributes", f)
		}
	})
	t.Run("WithParameter replaces a member of the driver", func(t *testing.T) {
		t.Parallel()
		srv := scripted(t, one)
		db := open(t, srv.Addr(), "")
		if _, _, err := scan(t, db, "SELECT N FROM T", exasol.WithParameter("sqlText", "SELECT 2")); err != nil {
			t.Fatal(err)
		}
		if f := member(t, srv.Frames()[2]); f["sqlText"] != "SELECT 2" {
			t.Errorf("the request is %v, want the statement of the option", f)
		}
	})
	t.Run("WithOptions", func(t *testing.T) {
		t.Parallel()
		srv := scripted(t, ok, one)
		db := open(t, srv.Addr(), "")
		ctx := exasol.WithOptions(t.Context(), exasol.WithDatabase("OTHER"))
		rows, err := db.QueryContext(ctx, "SELECT N FROM T")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		f := member(t, srv.Frames()[2])
		if f["command"] != "setAttributes" || !reflect.DeepEqual(f["attributes"], map[string]any{"currentSchema": "OTHER"}) {
			t.Errorf("the first request is %v, want setAttributes with the schema OTHER", f)
		}
	})
	t.Run("the schema of the DSN is open already", func(t *testing.T) {
		t.Parallel()
		srv := scripted(t, one)
		db, err := sql.Open(exasol.Name, "exasol://sys:exasol@"+srv.Addr()+"/MINE?validateservercertificate=false")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if _, _, err := scan(t, db, "SELECT N FROM T", exasol.WithSchema("MINE")); err != nil {
			t.Fatal(err)
		}
		if got := commands(t, srv); !reflect.DeepEqual(got, []string{"execute"}) {
			t.Errorf("the driver sent %q, want execute and no setAttributes", got)
		}
	})
	t.Run("WithReadonly", func(t *testing.T) {
		t.Parallel()
		srv := scripted(t)
		db := open(t, srv.Addr(), "")
		if _, err := db.ExecContext(t.Context(), "DELETE FROM T", exasol.WithReadonly(true)); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("WithReadonly(true) gave %v, want %v", err, dbimp.ErrNotSupported)
		}
		if n := len(srv.Frames()); n > 2 {
			t.Errorf("the driver sent %d frames after the login for an option that fails, want none", n-2)
		}
	})
	t.Run("WithReadonly(false)", func(t *testing.T) {
		t.Parallel()
		srv := scripted(t, one)
		db := open(t, srv.Addr(), "")
		if _, _, err := scan(t, db, "SELECT N FROM T", exasol.WithReadonly(false)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("WithTimeout below zero", func(t *testing.T) {
		t.Parallel()
		srv := scripted(t)
		db := open(t, srv.Addr(), "")
		if _, err := db.ExecContext(t.Context(), "SELECT 1", exasol.WithTimeout(-time.Second)); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("WithTimeout(-1s) gave %v, want %v", err, dbimp.ErrInvalidValue)
		}
	})
	t.Run("an option on a statement with arguments", func(t *testing.T) {
		t.Parallel()
		prepared := text(`{"status":"ok","responseData":{"statementHandle":2,"parameterData":{"numColumns":1,"columns":[{"name":"","dataType":{"type":"VARCHAR","size":10,"characterSet":"UTF8"}}]},"results":[{"resultType":"rowCount","rowCount":0}],"numResults":1}}`)
		srv := scripted(t, prepared, one, ok)
		db := open(t, srv.Addr(), "")
		if _, _, err := scan(t, db, "SELECT N FROM T WHERE V = ?", "a", exasol.WithTimeout(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if got := commands(t, srv); !reflect.DeepEqual(got, []string{"createPreparedStatement", "executePreparedStatement", "closePreparedStatement"}) {
			t.Errorf("the driver sent %q, want the prepared statement and its close", got)
		}
	})
	t.Run("WithTimeout in the context", func(t *testing.T) {
		t.Parallel()
		script := append(loginSteps(t), dbimptest.WebSocketStep{Hold: true, Reply: aborted()})
		srv := dbimptest.ServeWebSocketScript(t, script)
		db := open(t, srv.Addr(), "")
		ctx := exasol.WithOptions(t.Context(), exasol.WithTimeout(50*time.Millisecond))
		if _, err := db.ExecContext(ctx, "SELECT COUNT(*) FROM BIG"); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("the error is %v, want context.DeadlineExceeded", err)
		}
	})
}
