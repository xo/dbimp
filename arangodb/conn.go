package arangodb

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds the transaction that is open, if any.
type conn struct {
	c *Connector
	// id and seq name each query of the connection in a comment, for
	// cancel=tag (D90).
	id  string
	seq uint64
	// tx is the open transaction, or nil.
	tx *tx
}

// ensure the interfaces.
var (
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.NamedValueChecker  = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
)

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109), a uint64, a decimal, a slice, an
// array and a map with string keys, which AQL takes as they are, and a
// []byte, which value refuses. A nil pointer that implements driver.Valuer
// becomes nil. It hands every other value to the default converter of
// database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	if v, ok := nv.Value.(driver.Valuer); ok {
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			nv.Value = nil
			return nil
		}
	}
	if checkValue(nv.Value) {
		return nil
	}
	return driver.ErrSkip
}

// QueryContext satisfies driver.QueryerContext. A statement of the DDL of the
// driver runs its HTTP call (D92), and any other statement is AQL.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	o, args, err := c.options(ctx, args)
	if err != nil {
		return nil, err
	}
	d, err := parseDDL(query)
	switch {
	case err != nil:
		return nil, err
	case d != nil:
		if len(args) > 0 {
			return nil, fmt.Errorf("running %s: it takes no arguments: %w", d.verb, dbimp.ErrArguments)
		}
		if err := d.run(ctx, c.c, o.database); err != nil {
			return nil, err
		}
		return &noRows{}, nil
	}
	return c.cursor(ctx, o, query, args)
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its
// end, and RowsAffected is writesExecuted of the query.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	vals := make([]driver.Value, len(r.Columns()))
	for {
		err := r.Next(vals)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if cr, ok := r.(*rows); ok {
		return result{writes: cr.writes}, nil
	}
	return result{}, nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, because the server binds its arguments then.
func (c *conn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// Prepare satisfies driver.Conn.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It reads the version of the database, which
// checks the credentials and the database (measured).
func (c *conn) Ping(ctx context.Context) error {
	return c.c.call(ctx, http.MethodGet, api(c.c.cfg.Database, "version"), nil, nil, "")
}

// trx returns the id of the open transaction, or "".
func (c *conn) trx() string {
	if c.tx == nil {
		return ""
	}
	return c.tx.id
}

// stmt is a prepared statement, which runs as its text each time.
type stmt struct {
	c     *conn
	query string
}

// ensure the interfaces.
var (
	_ driver.StmtQueryContext = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
)

func (s *stmt) Close() error  { return nil }
func (s *stmt) NumInput() int { return -1 }

func (s *stmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

func (s *stmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.c.ExecContext(ctx, s.query, args)
}

func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.c.QueryContext(ctx, s.query, args)
}

// result is the result of a statement that runs with Exec. RowsAffected is
// writesExecuted of the query.
type result struct {
	writes int64
}

// LastInsertId satisfies driver.Result. AQL returns the new key with RETURN
// NEW._key, and has no id to count.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (r result) RowsAffected() (int64, error) {
	return r.writes, nil
}

// noRows is the result of a statement of the DDL, which has no rows.
type noRows struct{}

func (*noRows) Columns() []string         { return []string{} }
func (*noRows) Close() error              { return nil }
func (*noRows) Next([]driver.Value) error { return io.EOF }
