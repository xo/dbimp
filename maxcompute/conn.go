package maxcompute

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// explainPrefix starts the statement that gives the types of a result (D219).
const explainPrefix = "EXPLAIN OUTPUT "

// codeParse is the code of the error of a statement that does not parse
// (recorded: "a syntax error").
const codeParse = "ParseError"

// conn is one connection. It holds nothing on the server, because each statement
// is its own instance (recorded), so it has no transaction and no state to reset
// (D220 item 4).
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
// the statement takes out (D109), a decimal, a dbimp.Date, a dbimp.LocalDateTime
// and a dbimp.Interval, which the driver writes as literals of their own, and a
// list and a map, which the binding refuses with dbimp.ErrArguments. A nil
// pointer that implements driver.Valuer becomes nil. It hands every other value
// to the default converter of database/sql, which makes an int64, a float64, a
// bool, a string, a []byte and a time.Time.
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

// QueryContext satisfies driver.QueryerContext. It sends EXPLAIN OUTPUT for the
// types of the columns, and then the statement (D219 item 1). A statement that is
// not a query has no types, so the driver sends it as it is and returns its text
// as one column (D219 item 4).
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	o, text, err := c.prepare(ctx, query, args)
	if err != nil {
		return nil, err
	}
	cancel := func() {}
	if o.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
	}
	types, err := c.explain(ctx, o, text)
	if err != nil {
		cancel()
		return nil, err
	}
	t, err := c.c.run(ctx, o.project, text, o.settings)
	if err != nil {
		cancel()
		return nil, err
	}
	r, err := c.c.newRows(ctx, o, t, types, cancel)
	if err != nil {
		_ = t.close()
		cancel()
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It sends the statement and reads
// nothing of its result: the server tells no count of rows (recorded: "insert
// values", "update the transactional table"), so RowsAffected is an error.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	o, text, err := c.prepare(ctx, query, args)
	if err != nil {
		return nil, err
	}
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	t, err := c.c.run(ctx, o.project, text, o.settings)
	if err != nil {
		return nil, err
	}
	// The body is closed without a read of the rows, so a large result is never
	// drained (D36).
	if err := t.close(); err != nil {
		return nil, err
	}
	return result{}, nil
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

// BeginTx satisfies driver.ConnBeginTx. The server has no transaction: BEGIN and
// COMMIT do not parse (recorded: "a transaction statement"), and UPDATE, DELETE
// and MERGE run on a transactional table only (D20 and D220 item 4).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: MaxCompute has no transaction: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It reads the project, which checks the key and
// the project and costs no query (recorded: "the project").
func (c *conn) Ping(ctx context.Context) error {
	res, err := c.c.get(ctx, "/projects/"+url.PathEscape(c.c.cfg.Project), "")
	if err != nil {
		return err
	}
	if err := checkStatus(res, http.StatusOK); err != nil {
		return fmt.Errorf("reading the project: %w", err)
	}
	return res.Body.Close() //nolint:wrapcheck // The error is the one of the body.
}

// prepare resolves the options of the statement, checks them, and writes the
// arguments into its text.
func (c *conn) prepare(ctx context.Context, query string, args []driver.NamedValue) (options, string, error) {
	if err := ctx.Err(); err != nil {
		return options{}, "", fmt.Errorf("sending the statement: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return options{}, "", err
	}
	text, err := bindArgs(query, args, func() (*time.Location, error) {
		return c.c.zone(ctx, o.project, o.settings)
	})
	if err != nil {
		return options{}, "", err
	}
	return o, text, nil
}

// explain returns the types of the columns of the statement, in order. It
// returns nil and no error when the server refuses the statement as a statement
// that EXPLAIN OUTPUT does not take, such as DDL or SHOW, and the driver sends the
// statement itself then. Any other failure, such as a refused secret or a
// network error, is returned.
func (c *conn) explain(ctx context.Context, o options, text string) ([]*typ, error) {
	body := strings.TrimRight(strings.TrimSpace(text), "; \t\r\n")
	t, err := c.c.run(ctx, o.project, explainPrefix+body, o.settings)
	if err != nil {
		if isStatementError(err) {
			return nil, nil
		}
		return nil, err
	}
	out, err := t.readText()
	if cerr := t.finish(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	types, err := parseTypes(out)
	if errors.Is(err, errNoTypes) {
		return nil, nil
	}
	return types, err
}

// isStatementError reports whether err says that the server did not take the
// statement: a statement that does not parse, or a task that failed.
func isStatementError(err error) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && (e.state == stateFailed || e.HTTPStatus == http.StatusBadRequest && e.Code == codeParse)
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
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: MaxCompute has no such id: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. The result of a statement that changes
// rows is empty text, and holds no count (recorded: "insert values"), so the
// error wraps dbimp.ErrNotSupported, as D178 says.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: the server gives no count: %w", dbimp.ErrNotSupported)
}
