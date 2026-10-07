package clickhouse

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because the HTTP
// interface keeps no state for a client that sends no session (D177).
type conn struct {
	c *Connector
	// version is the answer of SELECT version() when the connection opened, such
	// as 26.9.2.8, or "" when the connection asked none.
	version string
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
// the statement takes out (D109), and the values that the driver binds with a
// type of their own: a uint64 that database/sql would refuse above the range of
// an int64, a decimal, a big integer, the types of the root package, a UUID, an
// address, and a list and a map (D176 and D177). A nil pointer that implements
// driver.Valuer becomes nil. It hands every other value to the default converter
// of database/sql.
func (cn *conn) CheckNamedValue(nv *driver.NamedValue) error {
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
	case *big.Int:
		if v == nil {
			nv.Value = nil
		}
		return nil
	case uint64, dbimp.Date, uuid.UUID, netip.Addr, []any, []string, []int64, []float64, []bool, map[string]any:
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

// QueryContext satisfies driver.QueryerContext.
func (cn *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := cn.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its end, so
// that an error after the rows reaches the caller.
func (cn *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := cn.query(ctx, query, args)
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

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent when
// it runs, with its arguments.
func (cn *conn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &stmt{cn: cn, query: query}, nil
}

// Prepare satisfies driver.Conn.
func (cn *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{cn: cn, query: query}, nil
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (cn *conn) Close() error {
	return nil
}

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (cn *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// BeginTx satisfies driver.ConnBeginTx. The server answers every BEGIN with
// HTTP 501, so it has no transactions (D20 and D177).
func (cn *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: ClickHouse has none: %w", dbimp.ErrNotSupported)
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the user and the
// password. GET /ping needs neither (measured).
func (cn *conn) Ping(ctx context.Context) error {
	_, err := cn.ExecContext(ctx, "SELECT 1", nil)
	return err
}

// readVersion asks the server for its version with SELECT version().
func (cn *conn) readVersion(ctx context.Context) (string, error) {
	r, err := cn.query(ctx, "SELECT version()", nil)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if err := r.NextRow(); err != nil {
		return "", err
	}
	if len(r.cur) != 1 {
		return "", fmt.Errorf("reading the version: %d columns: %w", len(r.cur), dbimp.ErrColumnCount)
	}
	version, ok := r.cur[0].(string)
	if !ok {
		return "", fmt.Errorf("reading the version: %T is not a string: %w", r.cur[0], dbimp.ErrInvalidValue)
	}
	// The answer is read to its end, so that the rows close with no cancel.
	if err := r.NextRow(); !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the version: the answer holds more than one row: %w", dbimp.ErrInvalidValue)
	}
	return version, nil
}

// query sends the statement with its arguments as typed parameters, and reads
// its answer up to its first row. The driver names the query, so that it can
// cancel it when its context ends before the answer is read, or when the caller
// closes the rows early (D176).
func (cn *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the query: %w", err)
	}
	o, args := resolve(ctx, &cn.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	statement, params, err := bind(query, args)
	if err != nil {
		return nil, err
	}
	id := newID()
	q := o.query(id, params)
	if v, ok := o.settings["query_id"]; ok {
		id = v
	}
	w := cn.c.watch(ctx, id)
	res, err := cn.c.send(ctx, statement, q)
	if err != nil {
		w.end()
		return nil, err
	}
	r, err := readAnswer(res, w, cn.version)
	if err != nil {
		w.end()
		return nil, err
	}
	return r, nil
}

// query returns the keys of the query of the request: the database, the settings
// of the driver, the query id, the settings of the options, and the values of the
// parameters (D176).
func (o options) query(id string, params url.Values) url.Values {
	q := url.Values{}
	for _, s := range settings {
		q.Set(s[0], s[1])
	}
	q.Set("query_id", id)
	if o.database != "" {
		q.Set("database", o.database)
	}
	if o.timeout > 0 {
		q.Set("max_execution_time", strconv.FormatFloat(o.timeout.Seconds(), 'f', -1, 64))
	}
	if o.readonly {
		// The settings in the query string do not make readonly=1 refuse the
		// statement, so the settings of the driver go with it (measured).
		q.Set("readonly", "1")
	}
	for name, value := range o.settings {
		q.Set(name, value)
	}
	maps.Copy(q, params)
	return q
}

// stmt is a prepared statement, which runs as its text each time.
type stmt struct {
	cn    *conn
	query string
}

// ensure the interfaces.
var (
	_ driver.StmtQueryContext = (*stmt)(nil)
	_ driver.StmtExecContext  = (*stmt)(nil)
)

func (s *stmt) Close() error { return nil }

func (s *stmt) NumInput() int { return -1 }

func (s *stmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

func (s *stmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

func (s *stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.cn.ExecContext(ctx, s.query, args)
}

func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.cn.QueryContext(ctx, s.query, args)
}

// result is the result of a statement that runs with Exec. The server has no id
// of an insert, and no count of the rows that a statement changed (D176).
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: the server has none: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. X-ClickHouse-Summary holds 0 for an
// update and a delete, so the count of no statement is known (D176).
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: the server gives no count of the rows that a statement changed: %w", dbimp.ErrNotSupported)
}

// version is a version of the server, such as 26.9.2.8, as its major and its
// minor numbers.
type version struct{ major, minor int }

// parseVersion reads the first two numbers of a version, and returns false for
// a text that does not start with two.
func parseVersion(s string) (version, bool) {
	major, rest, _ := strings.Cut(s, ".")
	minor, _, _ := strings.Cut(rest, ".")
	a, err1 := strconv.Atoi(major)
	b, err2 := strconv.Atoi(minor)
	return version{a, b}, err1 == nil && err2 == nil
}

// tagged reports whether the server of version s writes an error after some rows
// in the tagged form, which 26.9 and later do (D177 and measured).
func tagged(s string) bool {
	v, ok := parseVersion(s)
	return ok && (v.major > 26 || v.major == 26 && v.minor >= 9)
}
