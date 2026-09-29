package neo4j

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// accept asks for the best version of typed JSON that the server has (D62).
// 5.26.31 answers v1.0, and 2026.09.0 answers v1.2 (measured).
const accept = "application/vnd.neo4j.query.v1.2, application/vnd.neo4j.query.v1.1;q=0.9, application/vnd.neo4j.query;q=0.8"

// contentTypes are the types of a body of typed JSON, by the version that its
// arguments need (D62).
var contentTypes = [...]string{
	version10: "application/vnd.neo4j.query",
	version11: "application/vnd.neo4j.query.v1.1",
	version12: "application/vnd.neo4j.query.v1.2",
}

// conn is one connection. It holds the transaction that is open, if any.
type conn struct {
	c *Connector
	// id names the statements of the connection, in a comment at the end of
	// each or in txMetadata (D67 and D95).
	id string
	// tx is the open transaction, or nil.
	tx *tx
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

// body is the body of a request of the Query API.
type body struct {
	Statement  string                    `json:"statement,omitzero"`
	Parameters map[string]jsontext.Value `json:"parameters,omitzero"`
	AccessMode string                    `json:"accessMode,omitzero"`
	TxMetadata map[string]jsontext.Value `json:"txMetadata,omitzero"`
}

// CheckNamedValue satisfies driver.NamedValueChecker. It calls the Value
// method of a driver.Valuer, and takes each value that the driver can encode
// as typed JSON (D63), so that each keeps its type on the server.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	switch nv.Value.(type) {
	case *apd.Decimal, apd.Decimal:
		// A decimal has a Value method, which writes it as a string. Neo4j
		// has no decimal type, so it is refused, and never sent as a string
		// (D63).
		return fmt.Errorf("binding the argument %s: Neo4j has no decimal type (D63): %w", argName(*nv), dbimp.ErrNotSupported)
	}
	if v, ok := nv.Value.(driver.Valuer); ok {
		if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer && rv.IsNil() {
			nv.Value = nil
		} else {
			val, err := v.Value()
			if err != nil {
				return fmt.Errorf("reading the value of argument %s: %w", argName(*nv), err)
			}
			nv.Value = val
		}
	}
	if _, _, err := encode(nv.Value); err != nil {
		return fmt.Errorf("binding the argument %s: %w", argName(*nv), err)
	}
	return nil
}

// argName returns the name of the parameter of an argument: its name, or its
// ordinal, so that the ordinal n fills $n (D64).
func argName(nv driver.NamedValue) string {
	if nv.Name != "" {
		return nv.Name
	}
	return strconv.Itoa(nv.Ordinal)
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.run(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its
// end.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.run(ctx, query, args)
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

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs a statement, which checks the
// credentials and the database.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "RETURN 1 AS a", nil)
	return err
}

// metadata returns the txMetadata that names the connection (D67). A body
// of typed JSON takes each value of txMetadata in typed JSON too, and the
// server refuses a plain string there (measured on 2026.09.0).
func (c *conn) metadata() map[string]jsontext.Value {
	v, _, _ := encode(c.id)
	return map[string]jsontext.Value{"dbimp": v}
}

// tag returns the comment that names the connection, on a line of its own
// at the end of a statement (D67 and D95). At the end, it leaves each error
// position of the statement where the caller wrote it, except an error at
// the end of the input, which points at the line of the comment (D95).
func (c *conn) tag() string {
	return "\n// dbimp:" + c.id
}

// how returns how a statement of the connection is found on the server to
// stop it: CancelTag, CancelMetadata or "" for none (D67).
func (c *conn) how(ctx context.Context) (string, error) {
	switch c.c.cfg.Cancel {
	case CancelNone:
		return "", nil
	case CancelMetadata:
		if c.tx != nil {
			if c.tx.meta {
				return CancelMetadata, nil
			}
			return CancelTag, nil
		}
		ok, err := c.c.takesMetadata(ctx)
		if err != nil {
			return "", err
		}
		if ok {
			return CancelMetadata, nil
		}
	}
	return CancelTag, nil
}

// run sends one statement, and reads the response up to its rows. A
// statement of an open transaction goes to the transaction (D65).
func (c *conn) run(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if t := c.tx; t != nil && t.ended != nil {
		return nil, fmt.Errorf("running a statement: the transaction ended: %w", t.ended)
	}
	b := body{Statement: query}
	version := version10
	for _, arg := range args {
		v, n, err := encode(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %s: %w", argName(arg), err)
		}
		if b.Parameters == nil {
			b.Parameters = map[string]jsontext.Value{}
		}
		b.Parameters[argName(arg)] = v
		version = max(version, n)
	}
	how, err := c.how(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case how == CancelTag:
		b.Statement = query + c.tag()
	case how == CancelMetadata && c.tx == nil:
		b.TxMetadata = c.metadata()
	}
	path := c.c.queryPath()
	if c.tx != nil {
		path += "/tx/" + c.tx.id
	}
	w := c.watch(ctx, how)
	res, err := c.c.post(ctx, http.MethodPost, path, b, version)
	var r *rows
	if err == nil {
		r, err = readResponse(res, c.tx)
	}
	if err != nil {
		ran, cerr := w.end()
		if ran && c.tx != nil && c.tx.ended == nil {
			c.tx.ended = fmt.Errorf("stopping the statement ended the transaction: %w", err)
		}
		return nil, errors.Join(err, cerr)
	}
	r.w = w
	return r, nil
}

// queryPath returns the path of the Query API for the database of the
// connector.
func (c *Connector) queryPath() string {
	return "/db/" + url.PathEscape(c.cfg.Database) + "/query/v2"
}

// post sends one request with the body b, whose arguments need the version
// version of typed JSON. It returns a response of JSON, with any status. A
// response that is not JSON, such as a page of HTML from a proxy, is a
// *dbimp.StatusError with the start of its body. A status of 2xx is reported
// as HTTP 502, so that such an answer is an error that database/sql sees.
func (c *Connector) post(ctx context.Context, method, path string, b body, version int) (*http.Response, error) {
	var reader io.Reader
	if method != http.MethodDelete {
		buf, err := json.Marshal(b)
		if err != nil {
			return nil, fmt.Errorf("writing the request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Accept", accept)
	if reader != nil {
		req.Header.Set("Content-Type", contentTypes[version])
	}
	dbimp.SetAuth(req, c.cfg.Auth, "Bearer", c.cfg.User, c.cfg.Password)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if ct := res.Header.Get("Content-Type"); ct != "" && !isJSON(ct) {
		if res.StatusCode < http.StatusMultipleChoices {
			res.StatusCode = http.StatusBadGateway
		}
		return nil, dbimp.CheckStatus(res)
	}
	return res, nil
}

// isJSON reports whether a content type is JSON or typed JSON.
func isJSON(contentType string) bool {
	media, _, _ := strings.Cut(contentType, ";")
	media = strings.TrimSpace(media)
	return media == "application/json" || strings.HasPrefix(media, "application/vnd.neo4j.query") &&
		!strings.HasSuffix(media, "+jsonl")
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

// result is the result of a statement that runs with Exec. The counters of
// Neo4j count nodes, relationships and properties apart, and none of them is
// a count of rows (D66).
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: %w", dbimp.ErrNotSupported)
}
