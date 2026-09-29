package pinot

import (
	"context"
	"crypto/rand"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// sqlSyntax is how Pinot writes a literal, a quoted identifier and a
// comment, for the parser for placeholders (D34 and D132). A quote inside a
// literal is written twice (measured).
var sqlSyntax = dbimp.Syntax{Quotes: `'"`, DashComments: true, BlockComments: true}

// conn is one connection. It holds nothing on the server, because Pinot has
// no sessions and no transactions.
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
// which the statement takes out (D109), and a uint64, a decimal and a
// []byte, which the driver writes as literals of their own (D132). A nil
// pointer that implements driver.Valuer becomes nil. It hands every other
// value to the default converter of database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch v := nv.Value.(type) {
	case uint64:
		return nil
	case *apd.Decimal:
		if v == nil {
			nv.Value = nil
		}
		return nil
	case apd.Decimal:
		nv.Value = &v
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

// request is the body of POST /query/sql (measured).
type request struct {
	SQL          string `json:"sql"`
	QueryOptions string `json:"queryOptions"`
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
// end. The Broker takes no write (D128), so a statement that writes fails
// with the error of the server.
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
// when it runs, with its arguments written into its text (D132).
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

// BeginTx satisfies driver.ConnBeginTx. Pinot has no transactions (D20 and
// D133).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: Pinot has none: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the
// credentials.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "SELECT 1", nil)
	return err
}

// query writes the arguments into the statement, sends it, and reads its
// answer up to its first row. The driver names the query, so that it can
// cancel it before its answer arrives (D133).
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	text, err := bind(query, args)
	if err != nil {
		return nil, err
	}
	id := "dbimp-" + strings.ToLower(rand.Text())
	body, err := dbimp.MarshalParams(request{SQL: text, QueryOptions: o.queryOptions(id)}, o.params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	res, err := c.c.query(ctx, body)
	if err != nil {
		// The Broker answers only after the query ends, so only a query
		// whose answer has not arrived can still run (D133).
		if ctx.Err() != nil && o.cancel == CancelKill {
			_ = c.c.stop(ctx, id)
		}
		return nil, err
	}
	return readAnswer(res)
}

// bind writes each argument into query in place of its ? (D132). A query
// with no argument goes as it is. A named argument is refused, because
// Pinot has no named placeholder.
func bind(query string, args []driver.NamedValue) (string, error) {
	if len(args) == 0 {
		return query, nil
	}
	for _, arg := range args {
		if arg.Name != "" {
			return "", fmt.Errorf("binding the argument %s: Pinot has no named placeholder: %w", arg.Name, dbimp.ErrArguments)
		}
	}
	return sqlSyntax.Bind(query, args, literal)
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

// result is the result of a statement that runs with Exec. Pinot changes
// no rows, and has no id of an insert.
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: Pinot takes no insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: Pinot changes no rows: %w", dbimp.ErrNotSupported)
}
