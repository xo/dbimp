package surrealdb

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds no state, because each request of HTTP
// stands alone (D54).
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
	_ driver.Validator          = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
)

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109). It refuses any other argument that
// has no name, because SurrealQL has named parameters only (D50). It takes
// a value of any type, and the request encodes it by the rules of D53. It
// returns driver.ErrSkip for a driver.Valuer, so that database/sql calls it.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	if nv.Name == "" {
		return fmt.Errorf("binding argument %d: SurrealQL has named parameters only, so pass sql.Named(name, value): %w", nv.Ordinal, dbimp.ErrArguments)
	}
	if _, ok := nv.Value.(driver.Valuer); ok {
		return driver.ErrSkip
	}
	return nil
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.query(ctx, query, args)
}

// ExecContext satisfies driver.ExecerContext. It reads the result of every
// statement, and returns the error of the first one that failed (D55).
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := r.drain(); err != nil {
		return nil, err
	}
	return result{}, nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, because the server binds its arguments then.
func (c *conn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// Prepare satisfies driver.Conn.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{c: c, query: query}, nil
}

// Begin satisfies driver.Conn. A transaction of SurrealDB lives in the text
// of one request, so the driver has none (D54).
func (c *conn) Begin() (driver.Tx, error) {
	return nil, errNoTx()
}

// BeginTx satisfies driver.ConnBeginTx. It returns dbimp.ErrNotSupported for
// every option, so that a caller with ReadOnly or an isolation level gets the
// same error as any other (D54).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, errNoTx()
}

// errNoTx returns the error of a transaction, which SurrealDB keeps in the
// text of one request (D54).
func errNoTx() error {
	return fmt.Errorf("beginning a transaction: a transaction of SurrealDB lives in one request: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close.
func (c *conn) Close() error {
	return nil
}

// IsValid satisfies driver.Validator. A connection holds no state.
func (c *conn) IsValid() bool {
	return true
}

// Ping satisfies driver.Pinger. It calls the RPC method ping, with the
// credentials that every request sends.
func (c *conn) Ping(ctx context.Context) error {
	o, _ := resolve(ctx, &c.c.cfg, nil)
	r, err := c.send(ctx, o, rpcRequest{Method: "ping", Params: []any{}})
	if err != nil {
		return err
	}
	defer r.Close()
	return r.drain()
}

// rpcRequest is the body of a request to /rpc.
type rpcRequest struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

// query sends query with its arguments to the RPC method query (D50).
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	params := []any{query}
	if len(args) > 0 {
		vars := make(map[string]any, len(args))
		for _, arg := range args {
			vars[arg.Name] = arg.Value
		}
		params = append(params, vars)
	}
	return c.send(ctx, o, rpcRequest{Method: "query", Params: params})
}

// send sends one request to /rpc with the options o, and reads the response
// up to the rows of its first statement.
func (c *conn) send(ctx context.Context, o options, body rpcRequest) (*rows, error) {
	cfg := &c.c.cfg
	buf, contentType, err := c.encode(body, o.params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.c.base+"/rpc", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	// net/http writes each header in its canonical form, such as
	// Surreal-Ns, and the server reads a header of any case.
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", contentType)
	req.Header.Set("Surreal-Ns", o.namespace)
	req.Header.Set("Surreal-Db", o.database)
	switch cfg.Auth {
	case AuthNamespace:
		req.Header.Set("Surreal-Auth-Ns", cfg.Namespace)
	case AuthDatabase:
		req.Header.Set("Surreal-Auth-Ns", cfg.Namespace)
		req.Header.Set("Surreal-Auth-Db", cfg.Database)
	}
	if cfg.User != "" || cfg.Password != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}
	res, err := dbimp.Send(c.c.client, req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 && strings.HasPrefix(res.Header.Get("Content-Type"), contentJSON) {
		return nil, refusal(res)
	}
	if err := dbimp.CheckStatus(res); err != nil {
		return nil, fmt.Errorf("sending the query: %w", err)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, contentType) {
		// A response in another form, such as a page of HTML from a proxy,
		// becomes a *dbimp.StatusError with the start of its body. Its
		// status is reported as HTTP 502, so that database/sql sees an error.
		res.StatusCode = http.StatusBadGateway
		return nil, fmt.Errorf("reading a response of the content type %q: %w", got, dbimp.CheckStatus(res))
	}
	var sets setReader
	if contentType == contentCBOR {
		sets = newCBORSets(res.Body)
	} else {
		sets = newJSONSets(res.Body)
	}
	return readResponse(res.Body, sets)
}

// refusal returns the error of a request that the server refused with a
// body of JSON, such as HTTP 400 for a regex, which CBOR cannot hold. A parse
// error in /rpc is HTTP 200, with the code -32000. The body holds a code,
// and the message in information (recorded on each release). A body in
// another form stays the *dbimp.StatusError that CheckStatus returns.
func refusal(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	var se *dbimp.StatusError
	if !errors.As(err, &se) {
		return fmt.Errorf("sending the query: %w", err)
	}
	var body struct {
		Code        int    `json:"code"`
		Information string `json:"information"`
	}
	if json.Unmarshal([]byte(se.Body), &body) != nil || body.Information == "" {
		return fmt.Errorf("sending the query: %w", se)
	}
	if body.Code == 0 {
		body.Code = se.Code
	}
	return &ResponseError{HTTPStatus: se.Code, Errs: []Error{{Code: body.Code, Msg: body.Information}}}
}

// The content types of the encodings (D49).
const (
	contentCBOR = "application/cbor"
	contentJSON = "application/json"
)

// encode returns the body of a request in the encoding of the connector, and
// its content type. Each key of extra, which WithParameter sets, follows the
// keys of body, and replaces a key of body with the same name (D109).
func (c *conn) encode(body rpcRequest, extra map[string]any) ([]byte, string, error) {
	if c.c.cfg.Encoding == EncodingJSON {
		buf, err := dbimp.MarshalParams(body, extra)
		if err != nil {
			return nil, "", fmt.Errorf("writing the request: %w", err)
		}
		return buf, contentJSON, nil
	}
	_, method := extra["method"]
	_, params := extra["params"]
	var e dbimp.CBOREncoder
	e.Map(len(extra) + b2i(!method) + b2i(!params))
	if !method {
		e.Text("method")
		e.Text(body.Method)
	}
	if !params {
		e.Text("params")
		e.Array(len(body.Params))
		for _, p := range body.Params {
			if err := encodeCBOR(&e, p); err != nil {
				return nil, "", fmt.Errorf("writing the request: %w", err)
			}
		}
	}
	for _, k := range slices.Sorted(maps.Keys(extra)) {
		e.Text(k)
		if err := encodeCBOR(&e, extra[k]); err != nil {
			return nil, "", fmt.Errorf("writing the parameter %q: %w", k, err)
		}
	}
	return e.Bytes(), contentCBOR, nil
}

// b2i returns 1 for true and 0 for false.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
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

// result is the result of a statement that runs with Exec. The server sends
// no count of the records that a statement changed (D55).
type result struct{}

// LastInsertId satisfies driver.Result. A record id is not an integer.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: the server sends no count: %w", dbimp.ErrNotSupported)
}
