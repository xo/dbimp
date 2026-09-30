package rqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because each
// request stands alone, and the driver has no transactions (D144).
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
// which the statement takes out (D109), a uint64, which the driver checks
// against the range of SQLite, and dbimp.Date, dbimp.LocalTime and
// dbimp.LocalDateTime, which go as their text (D143). A nil pointer that
// implements driver.Valuer becomes nil. It hands every other value to the
// default converter of database/sql.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch nv.Value.(type) {
	case uint64, dbimp.Date, dbimp.LocalTime, dbimp.LocalDateTime:
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

// QueryContext satisfies driver.QueryerContext. The statement goes to
// /db/request, so that a write with RETURNING returns its rows, or to
// /db/query with WithReadonly(true) (D142).
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.run(ctx, pathRequest, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. The statement goes to
// /db/execute, or to /db/query with WithReadonly(true) (D142). It reads the
// answer to its end.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.run(ctx, pathExecute, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if !errors.Is(err, io.EOF) {
				return nil, err
			}
			break
		}
	}
	if !countsRows(query) {
		return result{}, nil
	}
	return result{lastInsertID: r.lastInsertID, rowsAffected: r.rowsAffected}, nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// with its arguments each time it runs, because the server keeps no
// prepared statement.
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

// BeginTx satisfies driver.ConnBeginTx. A BEGIN of rqlite stays open on the
// one write connection that every client of the server shares, so the driver
// has no transactions (D20 and D144).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: rqlite shares one write connection among every client: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1 on /db/query, which checks
// the credentials, and needs only the permission query.
func (c *conn) Ping(ctx context.Context) error {
	r, err := c.run(ctx, pathQuery, "SELECT 1", nil)
	if err != nil {
		return err
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// run sends query with args to path, or to /db/query with WithReadonly(true),
// and reads the answer up to its first row. WithTimeout bounds the request
// too, because 9.4.5 ignores db_timeout for a read on /db/request, and a
// disconnect stops a read (measured and D146). The rows end that bound when
// they close.
func (c *conn) run(ctx context.Context, path, query string, args []driver.NamedValue) (*rows, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	b, err := body(query, args)
	if err != nil {
		return nil, err
	}
	if o.readonly {
		path = pathQuery
	}
	keys := o.keys(ctx)
	cancel := context.CancelFunc(func() {})
	if o.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
	}
	res, err := c.c.send(ctx, path, keys, b)
	if err != nil {
		cancel()
		return nil, err
	}
	r, err := readAnswer(res)
	if err != nil {
		cancel()
		return nil, err
	}
	r.cancel = cancel
	return r, nil
}

// countsRows reports whether the server counts the rows of query: a
// statement whose first word, after any space and comment, is INSERT,
// UPDATE, DELETE, REPLACE or WITH. For any other statement, the server gives
// the counts of the last write before it on its connection (measured), so
// the driver gives 0 (D142).
func countsRows(query string) bool {
	switch strings.ToUpper(firstWord(query)) {
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "WITH":
		return true
	}
	return false
}

// firstWord returns the first word of query, after any space, comment of
// the form -- or /* */, and opening parenthesis.
func firstWord(query string) string {
	for {
		query = strings.TrimLeft(query, " \t\r\n\f(")
		switch {
		case strings.HasPrefix(query, "--"):
			i := strings.IndexByte(query, '\n')
			if i < 0 {
				return ""
			}
			query = query[i+1:]
		case strings.HasPrefix(query, "/*"):
			i := strings.Index(query[2:], "*/")
			if i < 0 {
				return ""
			}
			query = query[i+4:]
		default:
			end := strings.IndexFunc(query, func(r rune) bool {
				return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
			})
			if end < 0 {
				return query
			}
			return query[:end]
		}
	}
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

// result is the result of a statement that runs with Exec. The server leaves
// out a count that is zero (measured), so a count that is missing is 0.
type result struct {
	lastInsertID int64
	rowsAffected int64
}

// LastInsertId satisfies driver.Result.
func (r result) LastInsertId() (int64, error) {
	return r.lastInsertID, nil
}

// RowsAffected satisfies driver.Result.
func (r result) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}
