package druid

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

// conn is one connection. It holds nothing on the server, because the SQL
// API of Druid has no sessions and no transactions.
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
// dbimp.LocalDateTime and a slice, which the driver binds with a type of
// their own (D164). A nil pointer that implements driver.Valuer becomes nil.
// It hands every other value to the default converter of database/sql.
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
	case dbimp.Date, dbimp.LocalDateTime, []any, []string, []int64, []float64, []bool:
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

// request is the body of POST /druid/v2/sql (measured). The driver asks for
// arrayLines with the three rows of the header: the names, the native types
// and the SQL types of the columns (D164).
type request struct {
	Query          string       `json:"query"`
	ResultFormat   string       `json:"resultFormat"`
	Header         bool         `json:"header"`
	TypesHeader    bool         `json:"typesHeader"`
	SQLTypesHeader bool         `json:"sqlTypesHeader"`
	Context        queryContext `json:"context"`
	Parameters     []parameter  `json:"parameters,omitzero"`
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
// end. The SQL API of Druid takes no write (D163), so a statement that
// writes fails with the error of the server.
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

// BeginTx satisfies driver.ConnBeginTx. Druid has no transactions (D20 and
// D164).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: Druid has none: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the
// credentials. GET /status needs more than the privilege to read, so the
// ordinary user cannot ping with it (measured).
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "SELECT 1", nil)
	return err
}

// query sends the statement with its arguments, and reads its answer up to
// its first row. The driver names the query, so that it can cancel it when
// its context ends before the answer is read (D164).
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the query: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	params, err := parameters(args)
	if err != nil {
		return nil, err
	}
	id := "dbimp-" + strings.ToLower(rand.Text())
	body, err := dbimp.MarshalParams(request{
		Query:          query,
		ResultFormat:   "arrayLines",
		Header:         true,
		TypesHeader:    true,
		SQLTypesHeader: true,
		Context:        o.context(id),
		Parameters:     params,
	}, o.params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	w := c.c.watch(ctx, id)
	res, err := c.c.query(ctx, body)
	if err != nil {
		w.end()
		return nil, err
	}
	r, err := readAnswer(res, w)
	if err != nil {
		w.end()
		return nil, err
	}
	return r, nil
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

// result is the result of a statement that runs with Exec. The SQL API of
// Druid changes no rows, and has no id of an insert.
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: the SQL API of Druid takes no insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: the SQL API of Druid changes no rows: %w", dbimp.ErrNotSupported)
}
