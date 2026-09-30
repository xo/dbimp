package databend

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds its session, which each response returns
// (D122), and the transaction that is open, if any (D121).
type conn struct {
	c *Connector
	// sess is the session of the connection, or nil for the session of the
	// DSN, and node the node of the last response.
	sess session
	node string
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

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109), and a uint64 and a decimal, which
// the default converter of database/sql refuses or turns into text. A nil
// pointer that implements driver.Valuer becomes nil. It hands every other
// value to that converter.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch v := nv.Value.(type) {
	case uint64, dbimp.Date, dbimp.LocalTime, dbimp.LocalDateTime, dbimp.OffsetTime, dbimp.Interval,
		dbimp.Vector[int8], dbimp.Vector[int16], dbimp.Vector[int32], dbimp.Vector[int64], dbimp.Vector[float32], dbimp.Vector[float64]:
		// Each of the types of D138 has a Value method, which writes ISO
		// 8601. paramValue writes an Interval in the form of the server.
		return nil
	case *apd.Decimal:
		if v == nil {
			nv.Value = nil
		}
		return nil
	case apd.Decimal:
		nv.Value = &v
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

// request is the body of POST /v1/query (measured).
type request struct {
	SQL     string         `json:"sql"`
	Session session        `json:"session"`
	Params  jsontext.Value `json:"params,omitzero"`
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its
// end.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	res, err := c.exec(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return res, nil
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

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the
// credentials and the database of the session.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.exec(ctx, "SELECT 1", nil)
	return err
}

// current returns the session of the connection.
func (c *conn) current() session {
	if c.sess == nil {
		c.sess = newSession(&c.c.cfg)
	}
	return c.sess
}

// query sends one statement, and reads its answer up to its columns.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	if t := c.tx; t != nil && t.ended != nil {
		return nil, fmt.Errorf("running a statement: the transaction ended: %w", t.ended)
	}
	params, err := bindParams(args)
	if err != nil {
		return nil, err
	}
	prev := c.current()
	settings := o.settings()
	sent := prev.with(o.database, settings)
	body, err := dbimp.MarshalParams(request{SQL: query, Session: sent, Params: params}, o.params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	// The driver names the query, so that it can kill it before its first
	// answer arrives (D123).
	id := strings.ToLower(rand.Text())
	h := http.Header{"X-Databend-Query-Id": {id}}
	// A transaction lives on one node, which need_sticky names (measured).
	if c.node != "" && string(prev["need_sticky"]) == "true" {
		h.Set("X-Databend-Sticky-Node", c.node)
	}
	r := &rows{c: c, ctx: ctx, id: id, cancel: o.cancel, header: h, prev: prev, database: o.database, settings: settings, sent: sent}
	res, err := c.c.do(ctx, http.MethodPost, "/v1/query", body, h)
	if err != nil {
		if ctx.Err() != nil && o.cancel == CancelKill {
			_ = c.c.stop(ctx, r.killURI(), h)
		}
		return nil, err
	}
	if err := r.open(res); err != nil {
		if ctx.Err() != nil && o.cancel == CancelKill && !r.ended {
			_ = c.c.stop(ctx, r.killURI(), h)
		}
		return nil, err
	}
	if r.p.nodeID != "" {
		h.Set("X-Databend-Sticky-Node", r.p.nodeID)
	}
	return r, nil
}

// exec runs a statement and reads its result to its end. RowsAffected is the
// sum of the columns of the first row whose names start with "number of
// rows", such as "number of rows inserted", which a statement that changes
// rows returns (measured).
func (c *conn) exec(ctx context.Context, query string, args []driver.NamedValue) (result, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return result{}, err
	}
	defer r.Close()
	cols := r.Columns()
	res := result{}
	for first := true; ; first = false {
		err := r.NextRow()
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return result{}, err
		}
		if !first {
			continue
		}
		for i, name := range cols {
			if !strings.HasPrefix(name, "number of rows") {
				continue
			}
			// The count is a UInt64, which is a uint64 (D138).
			switch n := r.cur[i].(type) {
			case uint64:
				if n > math.MaxInt64 {
					return result{}, fmt.Errorf("reading the rows affected: %d is more than an int64: %w", n, dbimp.ErrInvalidValue)
				}
				res.affected += int64(n)
				res.counted = true
			case int64:
				res.affected += n
				res.counted = true
			}
		}
	}
}

// bindParams returns the arguments as params: a JSON array of the positional
// arguments, which fill each ? in order, or a JSON object of the named ones,
// which fill each :name (D120). It returns nil for no argument.
func bindParams(args []driver.NamedValue) (jsontext.Value, error) {
	if len(args) == 0 {
		return nil, nil
	}
	named := args[0].Name != ""
	var b bytes.Buffer
	b.WriteByte("[{"[boolIndex(named)])
	for i, arg := range args {
		if (arg.Name != "") != named {
			return nil, fmt.Errorf("binding the arguments: params is an array or an object, so the arguments are all named or all positional: %w", dbimp.ErrArguments)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		if named {
			k, err := jsontext.AppendQuote(nil, arg.Name)
			if err != nil {
				return nil, fmt.Errorf("binding the argument %s: %w", arg.Name, err)
			}
			b.Write(k)
			b.WriteByte(':')
		}
		v, err := paramValue(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %s: %w", argName(arg), err)
		}
		b.Write(v)
	}
	b.WriteByte("]}"[boolIndex(named)])
	return b.Bytes(), nil
}

// boolIndex returns 1 for true and 0 for false.
func boolIndex(v bool) int {
	if v {
		return 1
	}
	return 0
}

// argName returns the name of an argument, or its ordinal.
func argName(nv driver.NamedValue) string {
	if nv.Name != "" {
		return nv.Name
	}
	return strconv.Itoa(nv.Ordinal)
}

// paramValue returns the JSON of one argument (D120). A uint64 keeps every
// digit as a number. A Date, a LocalTime and a LocalDateTime are the text of
// ISO 8601, and an Interval is the text of the server (D138). A decimal is a string, which the server casts with every
// digit, where it reads a number with a fraction as a Float64 (D124). A time
// is a string that the server casts, in UTC, with its offset.
func paramValue(v any) (jsontext.Value, error) {
	switch v := v.(type) {
	case nil:
		return jsontext.Value("null"), nil
	case bool:
		return jsontext.Value(strconv.FormatBool(v)), nil
	case int64:
		return jsontext.Value(strconv.FormatInt(v, 10)), nil
	case uint64:
		return jsontext.Value(strconv.FormatUint(v, 10)), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("JSON has no %v: %w", v, dbimp.ErrInvalidValue)
		}
		return jsontext.Value(strconv.FormatFloat(v, 'g', -1, 64)), nil
	case string:
		return jsontext.AppendQuote(nil, v)
	case time.Time:
		return jsontext.AppendQuote(nil, v.UTC().Format("2006-01-02 15:04:05.999999 +00:00"))
	case *apd.Decimal:
		if v.Form != apd.Finite {
			return nil, fmt.Errorf("JSON has no %s: %w", v, dbimp.ErrInvalidValue)
		}
		return jsontext.AppendQuote(nil, v.Text('f'))
	case []byte:
		return nil, fmt.Errorf("params has no form for a binary value: %w", dbimp.ErrNotSupported)
	case dbimp.Date, dbimp.LocalTime, dbimp.LocalDateTime, dbimp.OffsetTime:
		// The server casts the text of ISO 8601 (measured).
		return jsontext.AppendQuote(nil, fmt.Sprint(v))
	case dbimp.Vector[int8], dbimp.Vector[int16], dbimp.Vector[int32], dbimp.Vector[int64], dbimp.Vector[float32], dbimp.Vector[float64]:
		// A vector is a JSON array of its numbers, which the server casts to
		// a Vector (measured on 1.2.948, D139). JSON has no NaN.
		if !finite(v) {
			return nil, fmt.Errorf("JSON has no NaN or infinity, which the vector %v holds: %w", v, dbimp.ErrInvalidValue)
		}
		return jsontext.Value(fmt.Sprint(v)), nil
	case dbimp.Interval:
		s, err := formatInterval(v)
		if err != nil {
			return nil, err
		}
		return jsontext.AppendQuote(nil, s)
	}
	return nil, fmt.Errorf("a parameter of %T: %w", v, dbimp.ErrNotSupported)
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
	affected int64
	counted  bool
}

// LastInsertId satisfies driver.Result. Databend has no id of an insert.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. A statement whose result names no
// count, such as REPLACE INTO, has none (D125).
func (r result) RowsAffected() (int64, error) {
	if !r.counted {
		return 0, fmt.Errorf("reading the rows affected: the server sent no count: %w", dbimp.ErrNotSupported)
	}
	return r.affected, nil
}

// finite reports whether every number of a vector of floats is finite. A
// vector of integers always is.
func finite(v any) bool {
	var fs []float64
	switch x := v.(type) {
	case dbimp.Vector[float32]:
		for _, f := range x {
			fs = append(fs, float64(f))
		}
	case dbimp.Vector[float64]:
		fs = x
	}
	for _, f := range fs {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return false
		}
	}
	return true
}
