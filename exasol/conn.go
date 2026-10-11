package exasol

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection, which owns a session on the server.
type conn struct {
	c  *Connector
	s  *session
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
	_ driver.Validator          = (*conn)(nil)
)

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option, which
// the statement takes out (D109), and the types that the driver writes itself:
// a uint64, an *apd.Decimal, dbimp.Date, dbimp.LocalDateTime and dbimp.Interval
// (D208). A nil pointer that implements driver.Valuer becomes nil. It hands
// every other value to the default converter of database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch nv.Value.(type) {
	case uint64, *apd.Decimal, dbimp.Date, dbimp.LocalDateTime, dbimp.Interval:
		return nil
	}
	if v, ok := nv.Value.(driver.Valuer); ok {
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			nv.Value = nil
			return nil
		}
	}
	return driver.ErrSkip
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	ctx, cancel, err := c.start(ctx, o)
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, o, query, args)
	if err != nil {
		cancel()
		return nil, err
	}
	return c.rowsOf(ctx, cancel, res)
}

// ExecContext satisfies driver.ExecerContext.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	ctx, cancel, err := c.start(ctx, o)
	if err != nil {
		return nil, err
	}
	defer cancel()
	res, err := c.run(ctx, o, query, args)
	if err != nil {
		return nil, err
	}
	return c.resultOf(ctx, res)
}

// execResult is the result of a statement that runs with Exec.
type execResult struct {
	rows int64
	ok   bool
}

// LastInsertId satisfies driver.Result. The server has no last insert id
// (docs/EXASOL.md, Statements).
func (execResult) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the last insert id: the server has none: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. A statement that returns rows has no
// count.
func (r execResult) RowsAffected() (int64, error) {
	if !r.ok {
		return 0, fmt.Errorf("reading the rows affected: the statement returned a result set: %w", dbimp.ErrNotSupported)
	}
	return r.rows, nil
}

// PrepareContext satisfies driver.ConnPrepareContext. It prepares the statement
// on the server, so a syntax error shows now (D208).
func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	o, _ := resolve(ctx, &c.c.cfg, nil)
	ctx, cancel, err := c.start(ctx, o)
	if err != nil {
		return nil, err
	}
	defer cancel()
	p, err := c.create(ctx, o, query)
	if err != nil {
		return nil, err
	}
	return &stmt{c: c, p: p, ctx: ctx}, nil
}

// Prepare satisfies driver.Conn. database/sql calls PrepareContext.
func (c *conn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("preparing a statement without a context: %w", dbimp.ErrNotSupported)
}

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// BeginTx satisfies driver.ConnBeginTx. It turns autocommit off, which starts a
// transaction at the next statement, and Commit and Rollback turn it back on
// (D209). ReadOnly fails, and so does an isolation level other than the
// default, because the server has neither.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	switch {
	case opts.ReadOnly:
		return nil, fmt.Errorf("beginning a read-only transaction: the server has none: %w", dbimp.ErrNotSupported)
	case sql.IsolationLevel(opts.Isolation) != sql.LevelDefault:
		return nil, fmt.Errorf("beginning a transaction with the isolation %s: the server has one level: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported)
	case c.tx != nil:
		return nil, fmt.Errorf("beginning a transaction inside another: %w", dbimp.ErrNotSupported)
	}
	c.s.stateMu.Lock()
	was := c.s.autocommit
	c.s.stateMu.Unlock()
	if was {
		if err := c.setAutocommit(ctx, false); err != nil {
			return nil, fmt.Errorf("beginning a transaction: %w", err)
		}
	}
	c.tx = &tx{c: c, ctx: ctx, restore: was}
	return c.tx, nil
}

// Close satisfies driver.Conn. It closes the WebSocket, and the server ends the
// session, with its statements and its result sets, and stops any statement that
// it runs (recorded: "start a long query and leave"). A transaction that
// database/sql left open ends with it, so the server rolls it back.
func (c *conn) Close() error {
	err := c.s.ws.Close(dbimp.CloseNormal, "")
	c.s.stateMu.Lock()
	if c.s.err == nil {
		c.s.err = fmt.Errorf("the connection is closed: %w", driver.ErrBadConn)
	}
	c.s.stateMu.Unlock()
	return err
}

// Ping satisfies driver.Pinger. It sends getAttributes, which costs the server
// nothing.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.s.call(ctx, request{Command: cmdGetAttribute})
	return err
}

// IsValid satisfies driver.Validator. A session that the server ended, or that
// the driver closed after a fault, is not valid, so database/sql drops the
// connection.
func (c *conn) IsValid() bool {
	return c.s.failure() == nil
}

// start checks the options, applies the timeout of the statement, and opens the
// schema that the options name if another one is open.
func (c *conn) start(ctx context.Context, o options) (context.Context, context.CancelFunc, error) {
	if err := o.check(); err != nil {
		return nil, nil, err
	}
	cancel := context.CancelFunc(func() {})
	if o.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
	}
	if err := c.openSchema(ctx, o); err != nil {
		cancel()
		return nil, nil, err
	}
	return ctx, cancel, nil
}

// run sends the statement. A statement with no arguments goes in execute. One
// with arguments is prepared, executed and closed, because the server binds the
// arguments (D208).
func (c *conn) run(ctx context.Context, o options, query string, args []driver.NamedValue) (result, error) {
	if len(args) == 0 {
		env, err := c.s.command(ctx, executeRequest{Command: cmdExecute, SQLText: query}, o.params)
		if err != nil {
			return result{}, err
		}
		return firstResult(env)
	}
	p, err := c.create(ctx, o, query)
	if err != nil {
		return result{}, err
	}
	defer c.release(ctx, p.handle)
	env, err := c.executePrepared(ctx, o, p, args)
	if err != nil {
		return result{}, err
	}
	return firstResult(env)
}

// firstResult returns the one result of an answer. The server runs one
// statement for each request, so there is one (recorded: "two statements with a
// semicolon").
func firstResult(env *envelope) (result, error) {
	if len(env.ResponseData.Results) == 0 {
		return result{}, fmt.Errorf("reading the answer: it holds no result: %w", dbimp.ErrInvalidValue)
	}
	return env.ResponseData.Results[0], nil
}

// rowsOf returns the rows of a result. A result with a count and no rows has no
// column and no row.
func (c *conn) rowsOf(ctx context.Context, cancel context.CancelFunc, res result) (driver.Rows, error) {
	if res.ResultType != resultSetType || res.ResultSet == nil {
		cancel()
		return noRows{}, nil
	}
	return c.newRows(ctx, cancel, res.ResultSet)
}

// resultOf returns the result of a statement run with Exec. A statement that
// returns a result set gives no count, and its handle closes.
func (c *conn) resultOf(ctx context.Context, res result) (driver.Result, error) {
	if res.ResultType == rowCountType {
		return execResult{rows: res.RowCount, ok: true}, nil
	}
	if res.ResultSet != nil && res.ResultSet.Handle != 0 {
		r := &rows{c: c, handle: res.ResultSet.Handle}
		if err := r.release(ctx); err != nil {
			return nil, err
		}
	}
	return execResult{}, nil
}

// openSchema opens the schema of the options if the session has another one
// open (D109).
func (c *conn) openSchema(ctx context.Context, o options) error {
	c.s.stateMu.Lock()
	cur := c.s.schema
	c.s.stateMu.Unlock()
	if o.schema == "" || o.schema == cur {
		return nil
	}
	_, err := c.s.call(ctx, attributeRequest{Command: cmdSetAttribute, Attributes: map[string]any{"currentSchema": o.schema}})
	if err != nil {
		return fmt.Errorf("opening the schema %q: %w", o.schema, err)
	}
	c.s.stateMu.Lock()
	c.s.schema = o.schema
	c.s.stateMu.Unlock()
	return nil
}

// setAutocommit sets the attribute autocommit.
func (c *conn) setAutocommit(ctx context.Context, on bool) error {
	_, err := c.s.call(ctx, attributeRequest{Command: cmdSetAttribute, Attributes: map[string]any{"autocommit": on}})
	if err != nil {
		return fmt.Errorf("setting autocommit to %t: %w", on, err)
	}
	c.s.stateMu.Lock()
	c.s.autocommit = on
	c.s.stateMu.Unlock()
	return nil
}
