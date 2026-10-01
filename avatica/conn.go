package avatica

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"

	"github.com/xo/dbimp"
)

// conn is one connection of Avatica, which openConnection opened (D157).
type conn struct {
	c  *Connector
	id string
	// ctx is the context of Connect without its end, because Close takes
	// none and must still send closeConnection (D157 and hard rule 4).
	ctx context.Context //nolint:containedctx // Close of driver.Conn takes no context (D157).
	// readOnly and isolation are the properties of the connection outside a
	// transaction, which the end of a transaction sets again (D159).
	readOnly  bool
	isolation int
	tx        *tx
	// bad is true once the server no longer knows the connection.
	bad bool
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

// connProps are the properties of a connection that connectionSync sends
// and answers. A property that is nil is not sent, and the server keeps it.
type connProps struct {
	AutoCommit           *bool `json:"autoCommit"`
	ReadOnly             *bool `json:"readOnly"`
	TransactionIsolation *int  `json:"transactionIsolation"`
}

// remarshal decodes the map m, which call returned, into v.
func remarshal(m map[string]any, v any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109), and each type that the driver writes
// itself (D158). A nil pointer that implements driver.Valuer becomes nil. It
// hands every other value to the default converter of database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) || isArg(nv.Value) {
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

// QueryContext satisfies driver.QueryerContext. It runs query, and reads its
// rows in frames (D157).
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.run(ctx, query, args)
}

// ExecContext satisfies driver.ExecerContext. It runs query, and closes any
// rows that it gives.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.run(ctx, query, args)
	if err != nil {
		return nil, err
	}
	n := max(r.updateCount, 0)
	if err := r.Close(); err != nil {
		return nil, err
	}
	return result(n), nil
}

// typedValues returns args as a TypedValue each, or nil for none (D158).
// Avatica has no named parameters, so a named argument is an error.
func typedValues(args []driver.NamedValue) ([]typedValue, error) {
	if len(args) == 0 {
		return nil, nil
	}
	vals := make([]typedValue, len(args))
	for i, a := range args {
		if a.Name != "" {
			return nil, fmt.Errorf("binding the argument %s: Avatica has no named parameters: %w", a.Name, dbimp.ErrArguments)
		}
		v, err := arg(a.Value)
		if err != nil {
			return nil, fmt.Errorf("writing the argument %d: %w", a.Ordinal, err)
		}
		vals[i] = v
	}
	return vals, nil
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

// isolations are the levels of isolation of JDBC for each level of
// database/sql that JDBC names.
var isolations = map[sql.IsolationLevel]int{
	sql.LevelReadUncommitted: 1,
	sql.LevelReadCommitted:   2,
	sql.LevelRepeatableRead:  4,
	sql.LevelSerializable:    8,
}

// BeginTx satisfies driver.ConnBeginTx. It sends connectionSync with
// autoCommit false, and with readOnly and transactionIsolation from opts
// (D159). The transaction is one of the database behind the server.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.tx != nil {
		return nil, fmt.Errorf("beginning a transaction inside another: %w", dbimp.ErrNotSupported)
	}
	p := connProps{AutoCommit: new(false)}
	if opts.ReadOnly != c.readOnly {
		p.ReadOnly = new(opts.ReadOnly)
	}
	if level := sql.IsolationLevel(opts.Isolation); level != sql.LevelDefault {
		iso, ok := isolations[level]
		if !ok {
			return nil, fmt.Errorf("beginning a transaction with the isolation %s: %w", level, dbimp.ErrNotSupported)
		}
		if iso != c.isolation {
			p.TransactionIsolation = new(iso)
		}
	}
	if _, err := c.sync(ctx, p); err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	c.tx = &tx{c: c, ctx: ctx, props: p}
	return c.tx, nil
}

// Close satisfies driver.Conn. It sends closeConnection, which rolls back a
// transaction that database/sql left open.
func (c *conn) Close() error {
	return c.close(c.ctx)
}

// IsValid satisfies driver.Validator. A connection that the server no longer
// knows is not valid.
func (c *conn) IsValid() bool {
	return !c.bad
}

// Ping satisfies driver.Pinger. It sends connectionSync with no change, which
// checks that the server knows the connection.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.sync(ctx, connProps{})
	return err
}

// sync sends connectionSync with p, and returns the properties of the
// connection that the server answers.
func (c *conn) sync(ctx context.Context, p connProps) (connProps, error) {
	props := map[string]any{"connProps": "connPropsImpl", "dirty": true}
	if p.AutoCommit != nil {
		props["autoCommit"] = *p.AutoCommit
	}
	if p.ReadOnly != nil {
		props["readOnly"] = *p.ReadOnly
	}
	if p.TransactionIsolation != nil {
		props["transactionIsolation"] = *p.TransactionIsolation
	}
	m, err := c.call(ctx, map[string]any{"request": "connectionSync", "connectionId": c.id, "connProps": props})
	if err != nil {
		return connProps{}, fmt.Errorf("setting the properties of the connection: %w", err)
	}
	var res struct {
		ConnProps connProps `json:"connProps"`
	}
	if err := remarshal(m, &res); err != nil {
		return connProps{}, fmt.Errorf("reading the properties of the connection: %w", err)
	}
	return res.ConnProps, nil
}

// call sends req with the connector, and marks the connection bad when the
// server no longer knows it. The server then ran nothing, so the error is
// driver.ErrBadConn, and database/sql can open another connection (hard rule
// 5).
func (c *conn) call(ctx context.Context, req map[string]any) (map[string]any, error) {
	m, err := c.c.call(ctx, req)
	return m, c.check(err)
}

// check marks the connection bad for the error of a connection that the
// server does not know, and returns driver.ErrBadConn for it.
func (c *conn) check(err error) error {
	if e, ok := errors.AsType[*Error](err); ok && e.Exception == noSuchConnection {
		c.bad = true
		return fmt.Errorf("%w: the server does not know the connection: %w", driver.ErrBadConn, err)
	}
	return err
}

// run runs query with args, and returns its rows, or its count of rows for
// a statement that gives none. A statement with no arguments goes as
// prepareAndExecute (D157). One with arguments goes as prepare and execute
// with a TypedValue for each (D158).
func (c *conn) run(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	o, args := resolve(ctx, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	vals, err := typedValues(args)
	if err != nil {
		return nil, err
	}
	r := &rows{c: c, ctx: ctx, frameSize: o.frameSize}
	var req map[string]any
	if vals == nil {
		if r.stmtID, err = c.createStatement(ctx); err != nil {
			return nil, err
		}
		req = map[string]any{
			"request":             "prepareAndExecute",
			"connectionId":        c.id,
			"statementId":         r.stmtID,
			"sql":                 query,
			"maxRowCount":         -1,
			"maxRowsInFirstFrame": o.frameSize,
		}
	} else {
		var h map[string]any
		if r.stmtID, h, err = c.prepare(ctx, query, len(vals)); err != nil {
			return nil, err
		}
		req = map[string]any{
			"request":         "execute",
			"statementHandle": h,
			"parameterValues": vals,
			"maxRowCount":     -1,
		}
	}
	if err := r.open(merge(req, o.params)); err != nil {
		return nil, err
	}
	return r, nil
}

// createStatement returns the id of a new statement of the connection.
func (c *conn) createStatement(ctx context.Context) (int64, error) {
	m, err := c.call(ctx, map[string]any{"request": "createStatement", "connectionId": c.id})
	if err != nil {
		return 0, fmt.Errorf("creating a statement: %w", err)
	}
	var res struct {
		StatementID int64 `json:"statementId"`
	}
	if err := remarshal(m, &res); err != nil {
		return 0, fmt.Errorf("reading the answer to createStatement: %w", err)
	}
	return res.StatementID, nil
}

// prepare prepares query, and returns the id of its statement and its whole
// handle, which execute takes with its signature (docs/AVATICA.md, Parameters).
// It checks n, the count of the arguments, against the parameters of the
// signature, because the server binds a missing one as NULL with no error
// (D158).
func (c *conn) prepare(ctx context.Context, query string, n int) (int64, map[string]any, error) {
	m, err := c.call(ctx, map[string]any{"request": "prepare", "connectionId": c.id, "sql": query, "maxRowCount": -1})
	if err != nil {
		return 0, nil, fmt.Errorf("preparing the statement: %w", err)
	}
	h, ok := m["statement"].(map[string]any)
	if !ok {
		return 0, nil, fmt.Errorf("reading the answer to prepare: no statement: %w", dbimp.ErrInvalidValue)
	}
	var res struct {
		ID        int64 `json:"id"`
		Signature struct {
			Parameters []jsontext.Value `json:"parameters"`
		} `json:"signature"`
	}
	if err := remarshal(h, &res); err != nil {
		return 0, nil, fmt.Errorf("reading the answer to prepare: %w", err)
	}
	if len(res.Signature.Parameters) != n {
		err := fmt.Errorf("binding %d arguments to a statement of %d parameters: %w", n, len(res.Signature.Parameters), dbimp.ErrArguments)
		return 0, nil, errors.Join(err, c.closeStatement(ctx, res.ID))
	}
	return res.ID, h, nil
}

// closeStatement sends closeStatement for the statement id. It takes ctx
// without its end, and a limit of its own, because the server runs a
// statement to its end when the client leaves (D159).
func (c *conn) closeStatement(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()
	if _, err := c.call(ctx, map[string]any{"request": "closeStatement", "connectionId": c.id, "statementId": id}); err != nil {
		return fmt.Errorf("closing the statement %d: %w", id, err)
	}
	return nil
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

// result is the count of rows of a statement that runs with Exec, from
// updateCount. Avatica gives no id of an inserted row.
type result int64

// LastInsertId satisfies driver.Result. Avatica gives no id of an inserted
// row.
func (r result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of an inserted row: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (r result) RowsAffected() (int64, error) {
	return int64(r), nil
}

// close sends closeConnection with ctx without its end, and a limit of its
// own.
func (c *conn) close(ctx context.Context) error {
	if c.bad {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()
	if _, err := c.c.call(ctx, map[string]any{"request": "closeConnection", "connectionId": c.id}); err != nil {
		return fmt.Errorf("closing the connection: %w", err)
	}
	return nil
}
