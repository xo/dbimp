package snowflake

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because each
// request of the SQL API is its own session (measured), so it has no
// transaction and no state to reset (D183).
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
// dbimp.LocalTime and a dbimp.LocalDateTime, which the driver binds with a
// type of their own, and a list and a map, which bind fails with
// dbimp.ErrArguments (D183). A nil pointer that implements driver.Valuer
// becomes nil. It hands every other value to the default converter of
// database/sql.
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
	case dbimp.Date, dbimp.LocalTime, dbimp.LocalDateTime, []any, map[string]any:
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

// request is the body of POST /api/v2/statements (measured). The members
// that hold nothing are left out.
type request struct {
	Statement  string            `json:"statement"`
	Timeout    int64             `json:"timeout,omitzero"`
	Warehouse  string            `json:"warehouse,omitzero"`
	Role       string            `json:"role,omitzero"`
	Database   string            `json:"database,omitzero"`
	Schema     string            `json:"schema,omitzero"`
	Parameters map[string]string `json:"parameters,omitzero"`
	Bindings   bindings          `json:"bindings,omitzero"`
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, query, args, false)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its end,
// without decoding a value, because the count of the rows that the statement
// changed comes after the rows.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, query, args, true)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				return r.result(), nil
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

// BeginTx satisfies driver.ConnBeginTx. The server refuses BEGIN alone, and
// each request is its own session, so a transaction cannot span two requests
// (D20 and D183).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: the SQL API refuses BEGIN alone and has no session to hold one: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the token and
// the login of the user, and needs no warehouse that costs more than a
// moment (D183).
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "SELECT 1", nil)
	return err
}

// query sends the statement with its arguments, polls it until it ends, and
// reads its answer up to its first row. skip makes the rows read values and
// decode none, for Exec.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue, skip bool) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the statement: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	loc, err := o.location()
	if err != nil {
		return nil, err
	}
	binds, err := bindArgs(args)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(request{
		Statement:  query,
		Timeout:    o.seconds(),
		Warehouse:  o.warehouse,
		Role:       o.role,
		Database:   o.database,
		Schema:     o.schema,
		Parameters: o.parameters(),
		Bindings:   binds,
	})
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	res, err := c.c.start(ctx, body)
	if err != nil {
		return nil, err
	}
	handle := linkHandle(res)
	if res.StatusCode == http.StatusAccepted {
		if handle, err = readHandle(res); err != nil {
			return nil, err
		}
	}
	w := c.c.watch(ctx, handle)
	if res.StatusCode == http.StatusAccepted {
		if res, err = c.c.await(ctx, handle); err != nil {
			w.end()
			return nil, err
		}
	}
	r, err := c.c.readAnswer(ctx, res, w, handle, loc, skip)
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

// result is the result of a statement that runs with Exec.
type result struct {
	// affected is the count of the rows that the statement changed, and
	// known is false when the answer holds no count.
	affected int64
	known    bool
}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: Snowflake has no such id: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. It is the sum of numRowsInserted,
// numRowsUpdated and numRowsDeleted of the answer. A statement whose answer
// holds no count, such as DDL, has none, and the error wraps
// dbimp.ErrNotSupported, as D178 says.
func (r result) RowsAffected() (int64, error) {
	if !r.known {
		return 0, fmt.Errorf("reading the rows affected: the answer of this statement holds no count: %w", dbimp.ErrNotSupported)
	}
	return r.affected, nil
}
