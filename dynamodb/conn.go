package dynamodb

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because the API of
// DynamoDB has no sessions, and a request carries its own signature.
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
// which the statement takes out (D109), and the values that the driver binds
// with a type of its own: a decimal, a Set, a list and a map (D169). A nil
// pointer that implements driver.Valuer becomes nil. It hands every other
// value to the default converter of database/sql.
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
	case Set, []any, map[string]any:
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

// statementRequest is the body of ExecuteStatement (recorded: "a statement").
type statementRequest struct {
	Statement  string `json:"Statement"`
	Parameters []any  `json:"Parameters,omitzero"`
	NextToken  string `json:"NextToken,omitzero"`
}

// statementBody returns the body of ExecuteStatement. extra holds the keys of
// WithParameter, which replace the keys of the driver (D109).
func statementBody(query string, params []any, token string, extra map[string]any) ([]byte, error) {
	body, err := dbimp.MarshalParams(statementRequest{Statement: query, Parameters: params, NextToken: token}, extra)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	return body, nil
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its
// end, so that an error on a later page reaches the caller (D21). DynamoDB
// gives no count of the items that a write changed (recorded: "crud:
// update"), so the result has no RowsAffected.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				return result{}, nil
			}
			return nil, err
		}
	}
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

// BeginTx satisfies driver.ConnBeginTx. DynamoDB runs a transaction only as
// one request of reads only or of writes only, which database/sql cannot
// express (D20 and D169).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: DynamoDB runs one only as a single request of reads or of writes: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It sends ListTables with a limit of one
// table, which costs little and checks the endpoint, the signature and the
// credentials.
func (c *conn) Ping(ctx context.Context) error {
	res, err := c.c.call(ctx, "ListTables", []byte(`{"Limit":1}`))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10)); err != nil {
		return fmt.Errorf("reading the answer to the ping: %w", err)
	}
	return nil
}

// query sends the statement with its arguments, and reads its answer up to
// its first item.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the statement: %w", err)
	}
	o, args := resolve(ctx, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	params, err := parameters(query, args)
	if err != nil {
		return nil, err
	}
	body, err := statementBody(query, params, "", o.params)
	if err != nil {
		return nil, err
	}
	res, err := c.c.call(ctx, "ExecuteStatement", body)
	if err != nil {
		return nil, err
	}
	return newRows(ctx, c.c, query, params, o.params, res)
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

// result is the result of a statement that runs with Exec. UPDATE and DELETE
// answer an empty list of items, and no count (recorded: "crud: update"), so
// no count is known (D169).
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: DynamoDB gives none: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: DynamoDB gives no count of the items that a write changed: %w", dbimp.ErrNotSupported)
}
