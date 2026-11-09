package hive

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"

	"github.com/xo/dbimp"
)

// pollMin and pollMax bound the wait between two calls of GetOperationStatus.
const (
	pollMin = 10 * time.Millisecond
	pollMax = 250 * time.Millisecond
)

// uniqueColumnNames is the key of Hive that names a result column with the name
// of its table (docs/HIVE.md, Responses).
const uniqueColumnNames = "hive.resultset.use.unique.column.names"

// conn is one connection, which is one session of HiveServer2.
type conn struct {
	c       *Connector
	session *sessionHandle
	// ctx is the context of Connect without its end, because Close takes none and
	// must still send CloseSession (D235 and hard rule 4).
	ctx context.Context //nolint:containedctx // Close of driver.Conn takes no context (D235).
	// flavor is the lower case name that the server gave: hive or impala.
	flavor  string
	version string
	// bad is true once the server no longer knows the session.
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

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option, which
// the statement takes out (D109), a uint64, and the civil types of the root
// package, which the driver writes as literals itself (D235). A nil pointer that
// implements driver.Valuer becomes nil. It hands every other value to the default
// converter of database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch nv.Value.(type) {
	case uint64, dbimp.Date, dbimp.LocalDateTime:
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

// Prepare satisfies driver.Conn.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent with
// its arguments each time it runs, because the server binds none.
func (c *conn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// BeginTx satisfies driver.ConnBeginTx. Neither flavor has a statement for a
// transaction over this interface (D20 and D235).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: the servers have no transaction over this interface: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. It sends CloseSession, and a connection can close
// twice.
func (c *conn) Close() error {
	return c.close(c.ctx)
}

// IsValid satisfies driver.Validator. A connection whose session the server no
// longer knows is not valid.
func (c *conn) IsValid() bool {
	return !c.bad && c.session != nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the credentials
// and the session.
func (c *conn) Ping(ctx context.Context) error {
	return c.run(ctx, "SELECT 1")
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	o, args := resolve(ctx, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	text, err := bind(query, args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := withTimeout(ctx, o.timeout)
	op, _, err := c.start(ctx, text, o.params, o.fetchSize)
	if err != nil {
		cancel()
		return nil, err
	}
	r, err := c.openRows(ctx, op, o.fetchSize)
	if err != nil {
		cancel()
		c.release(context.WithoutCancel(ctx), op)
		return nil, err
	}
	r.cancel = cancel
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It runs the statement to its end
// and reads no row.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	o, args := resolve(ctx, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	text, err := bind(query, args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := withTimeout(ctx, o.timeout)
	defer cancel()
	op, modified, err := c.start(ctx, text, o.params, o.fetchSize)
	if err != nil {
		return nil, err
	}
	c.release(context.WithoutCancel(ctx), op)
	return result{modified: modified}, nil
}

// withTimeout returns ctx with a deadline of d, or ctx and a cancel that does
// nothing when d is zero.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// run runs a statement to its end and drops its rows.
func (c *conn) run(ctx context.Context, stmt string) error {
	op, _, err := c.start(ctx, stmt, nil, 0)
	if err != nil {
		return err
	}
	c.release(context.WithoutCancel(ctx), op)
	return nil
}

// start sends the statement, and waits until the operation ends. It returns the
// handle, and the number of rows that the statement changed, or -1 when the
// server does not say. When ctx ends first, it cancels and closes the operation
// and returns the error of ctx.
func (c *conn) start(ctx context.Context, text string, conf map[string]string, _ int) (*opHandle, int64, error) {
	if c.session == nil {
		return nil, 0, fmt.Errorf("running a statement: the connection is closed: %w", driver.ErrBadConn)
	}
	op, err := c.c.execute(ctx, c.session, text, c.overlay(conf))
	if err != nil {
		return nil, 0, c.fail(err)
	}
	wait := pollMin
	for {
		st, err := c.c.operationStatus(ctx, op)
		if err != nil {
			c.abandon(context.WithoutCancel(ctx), op)
			return nil, 0, c.fail(err)
		}
		switch st.state {
		case stateFinished:
			return op, st.modified, nil
		case stateCanceled, stateClosed, stateTimedOut:
			c.release(context.WithoutCancel(ctx), op)
			return nil, 0, &Error{Message: fmt.Sprintf("the operation ended in the state %d", st.state)}
		case stateError:
			c.release(context.WithoutCancel(ctx), op)
			return nil, 0, st.st.toError()
		case stateInitialized, stateRunning, statePending, stateUnknown:
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			c.abandon(context.WithoutCancel(ctx), op)
			return nil, 0, fmt.Errorf("waiting for the statement: %w", context.Cause(ctx))
		case <-t.C:
		}
		wait = min(wait*2, pollMax)
	}
}

// overlay returns the confOverlay of a statement: the keys of WithParameter, and
// for Hive the key that makes it name a result column as the statement wrote it.
// Hive names a column of a table "t.id" by default, so that a result of a join
// has unique names, and a tool that takes the names of the columns into an INSERT
// then fails. A caller can set the key again with WithParameter. Impala has no
// such setting, and an unknown server gets nothing it did not ask for.
func (c *conn) overlay(conf map[string]string) map[string]string {
	if c.flavor != flavorHive {
		return conf
	}
	out := map[string]string{uniqueColumnNames: "false"}
	maps.Copy(out, conf)
	return out
}

// fail returns err, and marks the connection as bad when the server no longer
// knows the session.
func (c *conn) fail(err error) error {
	if e, ok := errors.AsType[*Error](err); ok && e.SQLState == "" && e.Code == 0 && e.HTTPStatus == 0 && e.Message == "Invalid SessionHandle" {
		c.bad = true
	}
	return err
}

// abandon cancels and closes an operation that nobody will read, after the
// context of its statement ended (D235). The caller passes the context without its
// end, and the calls have the limit closeTimeout.
func (c *conn) abandon(ctx context.Context, op *opHandle) {
	ctx, cancel := context.WithTimeout(ctx, closeTimeout)
	defer cancel()
	_ = c.c.cancelOperation(ctx, op)
	_ = c.c.closeOperation(ctx, op)
}

// release closes an operation that has ended.
func (c *conn) release(ctx context.Context, op *opHandle) {
	ctx, cancel := context.WithTimeout(ctx, closeTimeout)
	defer cancel()
	_ = c.c.closeOperation(ctx, op)
}

// close sends CloseSession with a limit of closeTimeout, and ends the session.
func (c *conn) close(ctx context.Context) error {
	if c.session == nil {
		return nil
	}
	s := c.session
	c.session = nil
	ctx, cancel := context.WithTimeout(ctx, closeTimeout)
	defer cancel()
	if err := c.c.closeSession(ctx, s); err != nil && !c.bad {
		return fmt.Errorf("closing the session: %w", err)
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

// result is the result of a statement that runs with Exec. Hive says how many
// rows a statement changed in GetOperationStatus, and Impala does not (docs/HIVE.md,
// Responses).
type result struct {
	modified int64
}

// LastInsertId satisfies driver.Result. Neither flavor has such an id.
func (r result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the last insert id: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (r result) RowsAffected() (int64, error) {
	if r.modified < 0 {
		return 0, fmt.Errorf("reading the rows affected: the server does not say: %w", dbimp.ErrNotSupported)
	}
	return r.modified, nil
}
