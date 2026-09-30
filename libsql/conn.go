package libsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds a stream on the server only while a
// transaction is open (D150).
type conn struct {
	c  *Connector
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
// which the statement takes out (D109), a uint64, which the driver checks
// against the range of SQLite, and dbimp.Date, dbimp.LocalTime and
// dbimp.LocalDateTime, which go as their text (D152). A nil pointer that
// implements driver.Valuer becomes nil. It hands every other value to the
// default converter of database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch nv.Value.(type) {
	case uint64, dbimp.Date, dbimp.LocalTime, dbimp.LocalDateTime:
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

// QueryContext satisfies driver.QueryerContext. The statement goes to
// /v3/cursor, which streams its rows (D149).
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	stmt, err := c.prepare(o, query, args)
	if err != nil {
		return nil, err
	}
	s, err := c.stream(o)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"baton": s.batonJSON(), "batch": map[string]any{"steps": []any{map[string]any{"stmt": stmt}}}})
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	cancel := context.CancelFunc(func() {})
	if o.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
	}
	res, err := c.c.post(ctx, s, pathCursor, body)
	if err != nil {
		cancel()
		return nil, c.txError(err)
	}
	r := &rows{c: c.c, s: s, tx: c.tx, cancel: cancel}
	if c.tx != nil {
		c.tx.busy = true
	}
	if err := readCursor(ctx, r, res); err != nil {
		return nil, c.txError(err)
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. The statement goes to
// /v3/pipeline, with a close after it outside a transaction (D149).
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	stmt, err := c.prepare(o, query, args)
	if err != nil {
		return nil, err
	}
	s, err := c.stream(o)
	if err != nil {
		return nil, err
	}
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	n, err := c.c.execute(ctx, s, stmt, c.tx == nil)
	if err != nil {
		return nil, c.txError(err)
	}
	return result(n), nil
}

// statement returns the statement of Hrana for query with args: positional
// arguments in args, and named ones in named_args. The server refuses the
// two together, so that is an error before anything is sent (D152).
func statement(query string, args []driver.NamedValue) (map[string]any, error) {
	stmt := map[string]any{"sql": query}
	var pos, named []any
	for _, a := range args {
		v, err := arg(a.Value)
		if err != nil {
			if a.Name != "" {
				return nil, fmt.Errorf("writing the argument %s: %w", a.Name, err)
			}
			return nil, fmt.Errorf("writing the argument %d: %w", a.Ordinal, err)
		}
		if a.Name != "" {
			named = append(named, map[string]any{"name": a.Name, "value": v})
			continue
		}
		pos = append(pos, v)
	}
	if len(pos) > 0 && len(named) > 0 {
		return nil, fmt.Errorf("binding the arguments: the server takes positional and named arguments apart: %w", dbimp.ErrArguments)
	}
	if len(pos) > 0 {
		stmt["args"] = pos
	}
	if len(named) > 0 {
		stmt["named_args"] = named
	}
	return stmt, nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// with its arguments each time it runs.
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

// BeginTx satisfies driver.ConnBeginTx. It sends BEGIN on a new stream
// (D150). The server keeps no transaction read-only, so ReadOnly fails, and
// so does an isolation level other than the default.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	switch {
	case opts.ReadOnly:
		return nil, fmt.Errorf("beginning a read-only transaction: the server takes a write in one: %w", dbimp.ErrNotSupported)
	case sql.IsolationLevel(opts.Isolation) != sql.LevelDefault:
		return nil, fmt.Errorf("beginning a transaction with the isolation %s: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported)
	case c.tx != nil:
		return nil, fmt.Errorf("beginning a transaction inside another: %w", dbimp.ErrNotSupported)
	}
	o, _ := resolve(ctx, &c.c.cfg, nil)
	s := c.c.newStream(o.namespace)
	if _, err := c.c.execute(ctx, s, map[string]any{"sql": "BEGIN"}, false); err != nil {
		return nil, err
	}
	c.tx = &tx{c: c, s: s, ctx: ctx}
	return c.tx, nil
}

// Close satisfies driver.Conn. A connection closes the stream of a
// transaction that database/sql left open.
func (c *conn) Close() error {
	if c.tx == nil || c.tx.expired {
		return nil
	}
	t := c.tx
	c.tx = nil
	return c.c.closeStream(t.ctx, t.s)
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the token.
func (c *conn) Ping(ctx context.Context) error {
	o, _ := resolve(ctx, &c.c.cfg, nil)
	_, err := c.c.execute(ctx, c.c.newStream(o.namespace), map[string]any{"sql": "SELECT 1"}, true)
	return err
}

// prepare checks the options, and returns the statement of Hrana for query
// with args (D152).
func (c *conn) prepare(o options, query string, args []driver.NamedValue) (map[string]any, error) {
	if err := o.check(); err != nil {
		return nil, err
	}
	stmt, err := statement(query, args)
	if err != nil {
		return nil, err
	}
	maps.Copy(stmt, o.params)
	return stmt, nil
}

// stream returns the stream of the transaction, or a new stream in the
// namespace of o. A statement of a transaction waits for no other one,
// because Hrana takes one request at a time on a stream (D150).
func (c *conn) stream(o options) (*stream, error) {
	if c.tx == nil {
		return c.c.newStream(o.namespace), nil
	}
	switch {
	case c.tx.busy:
		return nil, fmt.Errorf("running a statement of the transaction while its last one still reads its rows: %w", dbimp.ErrNotSupported)
	case o.namespace != c.tx.s.namespace:
		return nil, fmt.Errorf("running a statement of the transaction in the namespace %q: %w", o.namespace, dbimp.ErrNotSupported)
	}
	return c.tx.s, nil
}

// txError says that the server rolled back the transaction when its stream
// expired (D150). It is never driver.ErrBadConn, because database/sql would
// run the statement again outside the transaction.
func (c *conn) txError(err error) error {
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != CodeStreamExpired || c.tx == nil {
		return err
	}
	c.tx.expired = true
	return fmt.Errorf("running a statement of the transaction: the stream expired, and the server rolled back the transaction: %w", err)
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

// result is the result of a statement that runs with Exec (D152).
type result counts

// LastInsertId satisfies driver.Result. It is 0 when the server sent null.
func (r result) LastInsertId() (int64, error) {
	return r.lastID, nil
}

// RowsAffected satisfies driver.Result.
func (r result) RowsAffected() (int64, error) {
	return r.affected, nil
}
