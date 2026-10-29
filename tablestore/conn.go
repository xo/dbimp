package tablestore

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because each statement
// is its own request and carries its own signature, so it has no transaction and
// no state to reset (D224).
type conn struct {
	c *Connector
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

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option, which
// the statement takes out (D109), and hands every other value to the default
// converter of database/sql, because the driver writes each value of a driver
// type as a literal (D224).
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	return driver.ErrSkip
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the answer to its end,
// without decoding a value, because the count of the rows that the statement
// changed comes after the rows.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.body.Close()
	if err := r.resp.finish(); err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	return r.result(), nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent when
// it runs, with its arguments.
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

// BeginTx satisfies driver.ConnBeginTx. The server refuses BEGIN and START
// TRANSACTION (recorded: "a transaction", "lead: start transaction"), so there
// is none (D20 and D224).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: Tablestore refuses it in SQL: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It sends ListTable, which costs nothing and
// checks the endpoint, the signature and the credentials.
func (c *conn) Ping(ctx context.Context) error {
	res, err := c.c.call(ctx, "ListTable", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		return fmt.Errorf("reading the answer to ListTable: %w", err)
	}
	return nil
}

// query sends the statement with its arguments and reads the answer up to its
// first row.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the statement: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(&c.c.cfg); err != nil {
		return nil, err
	}
	text, err := syntax.Bind(query, args, literal)
	if err != nil {
		return nil, err
	}
	version, token, err := o.request()
	if err != nil {
		return nil, err
	}
	body := appendString(nil, fieldQuery, text)
	body = appendInt(body, fieldVersion, payloadFlatBuffers)
	body = appendInt(body, fieldSQLVersion, version)
	if token != "" {
		body = appendString(body, fieldSearchToken, token)
	}
	res, err := c.c.call(ctx, "SQLQuery", body)
	if err != nil {
		return nil, err
	}
	resp, err := readResponse(res.Body)
	if err != nil {
		_ = res.Body.Close()
		return nil, err
	}
	return newRows(res.Body, resp), nil
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

// result is the result of a statement that runs with Exec.
type result struct {
	// affected is the count of the rows that the statement changed, and known is
	// false when the answer holds none.
	affected int64
	known    bool
}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: Tablestore has no such id: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. It is the count of the answer. An answer
// that holds none has no count, and the error wraps dbimp.ErrNotSupported (D178).
func (r result) RowsAffected() (int64, error) {
	if !r.known {
		return 0, fmt.Errorf("reading the rows affected: the answer of this statement holds no count: %w", dbimp.ErrNotSupported)
	}
	return r.affected, nil
}
