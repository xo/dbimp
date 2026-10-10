package cosmos

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"reflect"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because the REST
// API of Cosmos DB has no sessions, and a request carries its own signature.
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

// isNilPointer reports whether v holds a nil pointer.
func isNilPointer(v driver.Valuer) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// queryBody is the body of a query (recorded: "a query that selects every
// document"). The server binds the parameters by name.
type queryBody struct {
	Query      string  `json:"query"`
	Parameters []param `json:"parameters,omitzero"`
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the statement: %w", err)
	}
	o, args := c.c.resolve(ctx, args)
	// A catalog statement is answered from the REST API (D190 item 17).
	if cs, ok, err := parseCatalog(statement, args); ok {
		if err != nil {
			return nil, err
		}
		if err := o.checkCommon(); err != nil {
			return nil, err
		}
		return cs.run(ctx, c.c, o)
	}
	if err := o.check(); err != nil {
		return nil, err
	}
	params, err := parameters(args)
	if err != nil {
		return nil, err
	}
	body, err := dbimp.MarshalParams(queryBody{Query: stripSemicolon(statement), Parameters: params}, o.params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	q := &query{database: o.database, container: o.container, body: body, pageSize: o.pageSize, key: o.key, hasKey: o.hasKey}
	res, err := c.c.docs(ctx, q, "")
	if err != nil {
		return nil, err
	}
	r, err := newRows(ctx, c.c, q, res)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. The driver reads only. The SQL
// of Cosmos DB has no statement that writes, and a write is a request on a
// document, which needs a language of its own that W40 records (D190), so
// Exec fails with dbimp.ErrNotSupported.
func (c *conn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errReadOnly()
}

// errReadOnly returns the error of a statement that writes.
func errReadOnly() error {
	return fmt.Errorf("running a statement with Exec: the driver reads only, and Cosmos DB has no SQL that writes (D190): %w", dbimp.ErrNotSupported)
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, with its arguments.
func (c *conn) PrepareContext(_ context.Context, statement string) (driver.Stmt, error) {
	return &stmt{c: c, query: statement}, nil
}

// Prepare satisfies driver.Conn.
func (c *conn) Prepare(statement string) (driver.Stmt, error) {
	return &stmt{c: c, query: statement}, nil
}

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// BeginTx satisfies driver.ConnBeginTx. A batch of Cosmos DB is atomic inside
// one request only, and it needs a language that writes, so the driver has no
// transaction (D20 and D190).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: a batch of Cosmos DB is atomic only inside one request, and the driver reads only: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It reads the account, GET /, which costs
// little and checks the endpoint and the signature (recorded: "the account").
func (c *conn) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.c.base+"/", nil)
	if err != nil {
		return fmt.Errorf("making the request: %w", err)
	}
	res, err := c.c.do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10)); err != nil {
		return fmt.Errorf("reading the answer to the ping: %w", err)
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
	return nil, errReadOnly()
}

func (s *stmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, fmt.Errorf("running a statement without a context: %w", dbimp.ErrNotSupported)
}

func (s *stmt) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	return nil, errReadOnly()
}

func (s *stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.c.QueryContext(ctx, s.query, args)
}
