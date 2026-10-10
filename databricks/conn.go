package databricks

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// columnAffected is the name of the first column of the answer of a statement
// that changes rows: INSERT, UPDATE, DELETE and MERGE (measured).
const columnAffected = "num_affected_rows"

// conn is one connection. It holds nothing on the server, because each request
// of the API is its own session (measured), so it has no transaction and no
// state to reset (D193 item 5).
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

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109), a decimal, a dbimp.Date, a
// dbimp.LocalDateTime and a dbimp.Interval, which the driver binds with a type
// of their own, and a list and a map, which bind fails with dbimp.ErrArguments
// (D193). A nil pointer that implements driver.Valuer becomes nil. It hands
// every other value to the default converter of database/sql, which makes an
// int64, a float64, a bool, a string, a []byte and a time.Time.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch v := nv.Value.(type) {
	case *apd.Decimal:
		if v == nil {
			nv.Value = nil
		}
		return nil
	case apd.Decimal:
		nv.Value = &v
		return nil
	case dbimp.Date, dbimp.LocalDateTime, dbimp.Interval, []any, map[string]any:
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

// request is the body of POST /api/2.0/sql/statements (measured). The members
// that hold nothing are left out. The driver sends no row_limit, byte_limit,
// disposition or format, so the server answers INLINE with JSON_ARRAY, its
// default, and cuts no result.
type request struct {
	Warehouse   string  `json:"warehouse_id"`
	Statement   string  `json:"statement"`
	Catalog     string  `json:"catalog,omitzero"`
	Schema      string  `json:"schema,omitzero"`
	WaitTimeout string  `json:"wait_timeout"`
	Parameters  []param `json:"parameters,omitzero"`
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the count of the rows
// that the statement changed, which is the one row of the answer.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	res, err := r.result()
	if err != nil {
		return nil, err
	}
	return res, nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, with its arguments.
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

// BeginTx satisfies driver.ConnBeginTx. The API has no session and no
// transaction across requests: BEGIN TRANSACTION in one request and COMMIT in
// the next fails with NO_ACTIVE_TRANSACTION (D20 and D193 item 5).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: the Statement Execution API has no session to hold one: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the token and
// the warehouse, and wakes a warehouse that stopped, which takes about 15
// seconds (measured).
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "SELECT 1", nil)
	return err
}

// query sends the statement with its arguments, polls it until it ends, and
// reads its answer up to its first row.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the statement: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	params, err := bindArgs(args)
	if err != nil {
		return nil, err
	}
	cancel := func() {}
	if o.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
	}
	body, err := dbimp.MarshalParams(request{
		Warehouse:   c.c.cfg.Warehouse,
		Statement:   query,
		Catalog:     o.catalog,
		Schema:      o.schema,
		WaitTimeout: waitFor(ctx),
		Parameters:  params,
	}, o.params)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	r, err := c.c.run(ctx, body)
	if err != nil {
		cancel()
		return nil, err
	}
	r.cancel = cancel
	return r, nil
}

// waitFor returns the wait that the request sends. The server holds the request
// for the wait, and the driver knows the id of the statement only from the
// answer. So when the context ends sooner than the longest wait, the driver asks
// for no wait, and polls, so that it holds the id and can cancel the statement
// when the context ends (D36 and D193 item 8).
func waitFor(ctx context.Context) string {
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < waitTimeout {
		return "0s"
	}
	return "50s"
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
	// affected is the count of the rows that the statement changed, and
	// known is false when the answer holds no count.
	affected int64
	known    bool
}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: Databricks has no such id: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. It is num_affected_rows of the answer.
// A statement whose answer holds no count, such as DDL and CREATE TABLE AS
// SELECT, has none, and the error wraps dbimp.ErrNotSupported, as D178 says.
func (r result) RowsAffected() (int64, error) {
	if !r.known {
		return 0, fmt.Errorf("reading the rows affected: the answer of this statement holds no count: %w", dbimp.ErrNotSupported)
	}
	return r.affected, nil
}
