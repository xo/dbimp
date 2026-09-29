package couchbase

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds the txid of an open transaction, and the
// options that the arguments of the next statement carry.
type conn struct {
	c *Connector

	// txid is the id of the open transaction, or "".
	txid string
	// txReadonly makes each statement of the transaction read only.
	txReadonly bool
}

// ensure the interfaces.
var (
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.NamedValueChecker  = (*conn)(nil)
	_ driver.SessionResetter    = (*conn)(nil)
	_ driver.Validator          = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
)

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out of its arguments (D109), and takes any other
// value, which send encodes with json/v2. It returns driver.ErrSkip for a
// driver.Valuer, so that database/sql calls it.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	if _, ok := nv.Value.(driver.Valuer); ok {
		return driver.ErrSkip
	}
	return nil
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	b, err := c.statement(ctx, query, args)
	if err != nil {
		return nil, err
	}
	r, err := c.send(ctx, b)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its
// end, and returns the count of mutations that the server reports.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	b, err := c.statement(ctx, query, args)
	if err != nil {
		return nil, err
	}
	r, err := c.send(ctx, b)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := r.drain(); err != nil {
		return nil, err
	}
	return result(r.mutations), nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, because the query service binds its arguments then.
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

// Close satisfies driver.Conn. A connection holds nothing to close.
func (c *conn) Close() error {
	return nil
}

// IsValid satisfies driver.Validator.
func (c *conn) IsValid() bool {
	return true
}

// ResetSession satisfies driver.SessionResetter. It rolls back a transaction
// that is still open, and returns driver.ErrBadConn if that fails, so that
// database/sql drops the connection rather than hand the transaction to
// another caller (D41).
func (c *conn) ResetSession(ctx context.Context) error {
	if c.txid == "" {
		return nil
	}
	if err := c.endTx(ctx, "ROLLBACK WORK"); err != nil {
		return fmt.Errorf("rolling back a transaction left open: %w: %w", driver.ErrBadConn, err)
	}
	return nil
}

// Ping satisfies driver.Pinger. It runs a statement, because the endpoint
// /admin/ping answers without credentials, so it would not check them.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "SELECT RAW 1", nil)
	return err
}

// statement returns the body of a request for query, with its arguments and
// its options.
func (c *conn) statement(ctx context.Context, query string, args []driver.NamedValue) (map[string]any, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	b := map[string]any{"statement": query}
	var positional []any
	for _, arg := range args {
		if arg.Name == "" {
			positional = append(positional, arg.Value)
			continue
		}
		b["$"+arg.Name] = arg.Value
	}
	if len(positional) > 0 {
		b["args"] = positional
	}
	if c.txid != "" {
		b["txid"] = c.txid
		o.readonly = o.readonly || c.txReadonly
	}
	o.body(b)
	return b, nil
}

// send sends one request, and reads the response up to its rows.
func (c *conn) send(ctx context.Context, body map[string]any) (*rows, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.c.base+"/query/service", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg := c.c.cfg; cfg.User != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}
	res, err := dbimp.Send(c.c.client, req)
	if err != nil {
		return nil, err
	}
	if res.Header.Get("Content-Type") != "" && !isJSON(res.Header.Get("Content-Type")) {
		// A response that is not JSON, such as a page of HTML from a proxy,
		// becomes a *dbimp.StatusError with the start of its body.
		return nil, dbimp.CheckStatus(forceError(res))
	}
	return readResponse(res)
}

// forceError makes a response that is not JSON an error, even with a status
// of 2xx, which it reports as HTTP 502, so that database/sql sees an error.
func forceError(res *http.Response) *http.Response {
	if res.StatusCode < 300 {
		res.StatusCode = http.StatusBadGateway
	}
	return res
}

func isJSON(contentType string) bool {
	return len(contentType) >= 16 && contentType[:16] == "application/json"
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
type result int64

// LastInsertId satisfies driver.Result. A document key is not an integer, so
// the query service has none.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result, from metrics.mutationCount.
func (r result) RowsAffected() (int64, error) {
	return int64(r), nil
}
