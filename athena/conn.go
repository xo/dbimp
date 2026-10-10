package athena

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"reflect"
	"slices"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because each query
// is its own request and carries its own signature, so it has no transaction
// and no state to reset (D192).
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
// the statement takes out (D109), and the values that the driver writes as a
// literal of a type of their own: a decimal, a dbimp.Date, a dbimp.LocalTime, a
// dbimp.OffsetTime, a dbimp.LocalDateTime, a dbimp.Interval, a uuid.UUID, a
// netip.Addr, a list and a map (D192). A nil pointer that implements
// driver.Valuer becomes nil. It hands every other value to the default
// converter of database/sql.
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
	case dbimp.Date, dbimp.LocalTime, dbimp.OffsetTime, dbimp.LocalDateTime, dbimp.Interval,
		uuid.UUID, netip.Addr, []any, []string, []int64, []float64, []bool, map[string]any:
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

// maxParameter is the most bytes that an element of ExecutionParameters can
// hold. The service allows 1024 characters, and the driver counts bytes, so a
// value with many multibyte characters goes into the text early, which is safe.
const maxParameter = 1024

// syntax says how the SQL of Athena writes literals, quoted names and comments,
// so that the parser for placeholders skips them (D34).
var syntax = dbimp.Syntax{Quotes: `'"`, DashComments: true, BlockComments: true}

// startRequest is the body of StartQueryExecution (recorded: "a select"). The
// members that hold nothing are left out.
type startRequest struct {
	QueryString           string        `json:"QueryString"`
	WorkGroup             string        `json:"WorkGroup,omitzero"`
	ClientRequestToken    string        `json:"ClientRequestToken"`
	QueryExecutionContext *queryContext `json:"QueryExecutionContext,omitzero"`
	ResultConfiguration   *resultConfig `json:"ResultConfiguration,omitzero"`
	ExecutionParameters   []string      `json:"ExecutionParameters,omitzero"`
}

// queryContext is QueryExecutionContext.
type queryContext struct {
	Database string `json:"Database,omitzero"`
	Catalog  string `json:"Catalog,omitzero"`
}

// resultConfig is ResultConfiguration.
type resultConfig struct {
	OutputLocation string `json:"OutputLocation"`
}

// startBody returns the body of StartQueryExecution for the options o. extra
// holds the keys of WithParameter, which replace the keys of the driver (D109).
func startBody(query, token string, params []string, o options) ([]byte, error) {
	req := startRequest{QueryString: query, WorkGroup: o.workgroup, ClientRequestToken: token, ExecutionParameters: params}
	if o.database != "" || o.catalog != "" {
		req.QueryExecutionContext = &queryContext{Database: o.database, Catalog: o.catalog}
	}
	if o.output != "" {
		req.ResultConfiguration = &resultConfig{OutputLocation: o.output}
	}
	body, err := dbimp.MarshalParams(req, o.params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	return body, nil
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
// changed is UpdateCount, which comes after the rows.
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

// BeginTx satisfies driver.ConnBeginTx. The server refuses START TRANSACTION
// (recorded: "a start transaction"), so there is none (D20 and D192).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: Athena refuses START TRANSACTION: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It sends GetWorkGroup for the workgroup of the
// DSN, or ListWorkGroups with one entry when the DSN names none. Neither starts
// a query, so neither costs a scan, and both check the endpoint, the signature
// and the credentials.
func (c *conn) Ping(ctx context.Context) error {
	if c.c.cfg.WorkGroup != "" {
		return c.c.callJSON(ctx, "GetWorkGroup", map[string]string{"WorkGroup": c.c.cfg.WorkGroup}, nil, false)
	}
	return c.c.callJSON(ctx, "ListWorkGroups", map[string]int{"MaxResults": 1}, nil, false)
}

// query starts the statement with its arguments, polls it until it ends, and
// reads its answer up to its first row. skip makes the rows decode no value,
// for Exec.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue, skip bool) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the statement: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	params, err := literals(args)
	if err != nil {
		return nil, err
	}
	if slices.ContainsFunc(params, func(p string) bool { return len(p) > maxParameter }) {
		// The server refuses a parameter of more than 1024 characters (recorded in
		// the live run of 2026-10-10), so the driver writes every argument into the
		// text of the statement and sends no parameter.
		if query, err = syntax.Bind(query, args, literal); err != nil {
			return nil, err
		}
		params = nil
	}
	body, err := startBody(query, newToken(), params, o)
	if err != nil {
		return nil, err
	}
	id, err := c.c.start(ctx, body)
	if err != nil {
		return nil, err
	}
	q, err := c.c.await(ctx, id)
	if err != nil {
		return nil, err
	}
	res, err := c.c.results(ctx, id, "")
	if err != nil {
		return nil, fmt.Errorf("fetching the first page: %w", err)
	}
	// The first row of a DML result is the header, and a result that changes
	// rows has no row at all (recorded: "a select", "an insert into the
	// iceberg table"), so the first row is dropped only if there is one.
	return newRows(ctx, c.c, id, res, q.QueryExecution.StatementType == "DML", skip)
}

// stmt is a prepared statement, which runs as its text each time. Athena has a
// prepared statement of its own, but the driver does not use it (D192 item 4).
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
	// affected is UpdateCount, and known is false when the answer holds none.
	affected int64
	known    bool
}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: Athena has no such id: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. It is UpdateCount of the answer. A
// statement whose answer holds none, such as DESCRIBE, has no count, and the
// error wraps dbimp.ErrNotSupported, as D178 says.
func (r result) RowsAffected() (int64, error) {
	if !r.known {
		return 0, fmt.Errorf("reading the rows affected: the answer of this statement holds no count: %w", dbimp.ErrNotSupported)
	}
	return r.affected, nil
}
