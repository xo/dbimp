package exasol

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"fmt"

	"github.com/xo/dbimp"
)

// prepared is a statement that the server prepared: its handle, and the
// columns that describe its parameters, in the form that the server sent them,
// which executePreparedStatement sends back.
type prepared struct {
	handle int64
	params []column
	raw    jsontext.Value
}

// create prepares the statement on the server, which parses it and says what
// type each placeholder has (recorded: "prepare an insert with every kind of
// column").
func (c *conn) create(ctx context.Context, o options, query string) (*prepared, error) {
	env, err := c.s.command(ctx, createRequest{Command: cmdCreate, SQLText: query}, o.params)
	if err != nil {
		return nil, err
	}
	p := &prepared{handle: env.ResponseData.StatementHandle}
	if pd := env.ResponseData.ParameterData; pd != nil && pd.NumColumns > 0 {
		var ws []wireColumn
		if err := unmarshalColumns(pd.Columns, &ws); err != nil {
			c.release(ctx, p.handle)
			return nil, err
		}
		p.params, p.raw = columnsOf(ws), pd.Columns
	}
	return p, nil
}

// executePrepared binds args to the parameters and runs the statement.
func (c *conn) executePrepared(ctx context.Context, o options, p *prepared, args []driver.NamedValue) (*envelope, error) {
	data, err := bind(p.params, args, c.s.location)
	if err != nil {
		return nil, err
	}
	if data == nil {
		data = []jsontext.Value{}
	}
	req := executePreparedRequest{
		Command:    cmdExecutePrep,
		Handle:     p.handle,
		NumColumns: len(p.params),
		NumRows:    min(len(p.params), 1),
		Columns:    p.raw,
		Data:       data,
	}
	return c.s.command(ctx, req, o.params)
}

// release closes the prepared statement. The server frees it with the session
// if the request fails, so a failure is not an error of the statement that
// used it.
func (c *conn) release(ctx context.Context, handle int64) {
	ctx, cancel := closeContext(ctx)
	defer cancel()
	_, _ = c.s.call(ctx, closePreparedRequest{Command: cmdClosePrep, Handle: handle})
}

// stmt is a prepared statement of database/sql.
type stmt struct {
	c *conn
	p *prepared
	// ctx is the context of PrepareContext, which Close takes without its end,
	// because Close of driver.Stmt takes none (D208 and hard rule 4).
	ctx    context.Context //nolint:containedctx // Close of driver.Stmt takes no context (D208).
	closed bool
}

// ensure the interfaces.
var (
	_ driver.StmtQueryContext = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
)

// Close satisfies driver.Stmt.
func (s *stmt) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.c.release(s.ctx, s.p.handle)
	return nil
}

// NumInput satisfies driver.Stmt. It is -1, because database/sql counts the
// options among the arguments, and the driver counts the others itself.
func (s *stmt) NumInput() int { return -1 }

// Exec satisfies driver.Stmt. database/sql calls ExecContext.
func (s *stmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

// Query satisfies driver.Stmt. database/sql calls QueryContext.
func (s *stmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

// ExecContext satisfies driver.StmtExecContext.
func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	o, args := resolve(ctx, &s.c.c.cfg, args)
	ctx, cancel, err := s.c.start(ctx, o)
	if err != nil {
		return nil, err
	}
	defer cancel()
	env, err := s.c.executePrepared(ctx, o, s.p, args)
	if err != nil {
		return nil, err
	}
	res, err := firstResult(env)
	if err != nil {
		return nil, err
	}
	return s.c.resultOf(ctx, res)
}

// QueryContext satisfies driver.StmtQueryContext.
func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	o, args := resolve(ctx, &s.c.c.cfg, args)
	ctx, cancel, err := s.c.start(ctx, o)
	if err != nil {
		return nil, err
	}
	env, err := s.c.executePrepared(ctx, o, s.p, args)
	if err != nil {
		cancel()
		return nil, err
	}
	res, err := firstResult(env)
	if err != nil {
		cancel()
		return nil, err
	}
	return s.c.rowsOf(ctx, cancel, res)
}
