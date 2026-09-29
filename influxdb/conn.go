package influxdb

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds no state on the server, only the dialect
// that it speaks and the major release of the server (D78).
type conn struct {
	c *Connector
	// dialect is SQL or InfluxQL.
	dialect string
	// major is the major release of the server: from GET /ping with sqlmode
	// prefer or require, and from the key version otherwise.
	major int
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

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps a uint64 and
// a decimal, which the default converter of database/sql refuses or turns
// into text, and hands every other value to that converter.
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	switch v := nv.Value.(type) {
	case uint64:
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

// paramName returns the name of the parameter of an argument: its name, or
// its ordinal, so that the ordinal n fills $n, in SQL and in InfluxQL
// (measured, D111).
func paramName(nv driver.NamedValue) string {
	if nv.Name != "" {
		return nv.Name
	}
	return strconv.Itoa(nv.Ordinal)
}

// params returns the arguments as a JSON object of parameters, or nil for no
// argument. A value is null, a boolean, a number or a string (measured). A
// time is a string in RFC 3339.
func params(args []driver.NamedValue) (jsontext.Value, error) {
	if len(args) == 0 {
		return nil, nil
	}
	var b bytes.Buffer
	enc := jsontext.NewEncoder(&b)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return nil, err
	}
	for _, arg := range args {
		name := paramName(arg)
		if err := enc.WriteToken(jsontext.String(name)); err != nil {
			return nil, fmt.Errorf("writing argument %s: %w", name, err)
		}
		v, err := value(arg.Value)
		if err != nil {
			return nil, fmt.Errorf("binding the argument %s: %w", name, err)
		}
		if err := enc.WriteValue(v); err != nil {
			return nil, fmt.Errorf("writing argument %s: %w", name, err)
		}
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, err
	}
	return jsontext.Value(bytes.TrimSpace(b.Bytes())), nil
}

// value returns the JSON value of one argument. A decimal keeps every digit
// of its text.
func value(v any) (jsontext.Value, error) {
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
		return quote(v)
	case time.Time:
		return quote(v.Format(time.RFC3339Nano))
	case *apd.Decimal:
		if v.Form != apd.Finite {
			return nil, fmt.Errorf("JSON has no %s: %w", v, dbimp.ErrInvalidValue)
		}
		return jsontext.Value(v.Text('f')), nil
	case []byte:
		return nil, fmt.Errorf("InfluxDB has no binary parameter: %w", dbimp.ErrNotSupported)
	}
	return nil, fmt.Errorf("a parameter of %T: %w", v, dbimp.ErrNotSupported)
}

// quote returns s as a JSON string.
func quote(s string) (jsontext.Value, error) {
	b, err := jsontext.AppendQuote(nil, s)
	if err != nil {
		return nil, fmt.Errorf("writing a string: %w", err)
	}
	return b, nil
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	ins, err := parseInsert(query)
	switch {
	case err != nil:
		return nil, err
	case ins != nil:
		if err := c.write(ctx, ins, args); err != nil {
			return nil, err
		}
		return noRows{}, nil
	}
	p, err := params(args)
	if err != nil {
		return nil, err
	}
	if c.dialect == SQL {
		return c.querySQL(ctx, query, p)
	}
	return c.queryInfluxQL(ctx, query, p)
}

// ExecContext satisfies driver.ExecerContext. It reads every result to its
// end, and returns the first error.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := drain(r); err != nil {
		return nil, err
	}
	return result{}, nil
}

// drain reads every row of every result set of r.
func drain(r driver.Rows) error {
	vals := make([]driver.Value, len(r.Columns()))
	for {
		err := r.Next(vals)
		switch {
		case err == nil:
			continue
		case !errors.Is(err, io.EOF):
			return err
		}
		next, ok := r.(driver.RowsNextResultSet)
		if !ok || !next.HasNextResultSet() {
			return nil
		}
		if err := next.NextResultSet(); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		vals = make([]driver.Value, len(r.Columns()))
	}
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

// BeginTx satisfies driver.ConnBeginTx. InfluxDB has no transactions (D20).
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: InfluxDB has none: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs a statement, which checks the
// credentials and the database, as GET /ping does not on InfluxDB 1 and 2
// (measured).
func (c *conn) Ping(ctx context.Context) error {
	q := "SHOW MEASUREMENTS"
	if c.dialect == SQL {
		q = "SELECT 1 AS a"
	}
	_, err := c.ExecContext(ctx, q, nil)
	return err
}

// send sends a request to path, with the credentials of the connector, and
// returns a response of JSON with a 2xx status. Any other response is an
// error, and its body is closed.
func (c *Connector) send(ctx context.Context, major int, path, contentType string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	c.auth(req, major)
	res, err := dbimp.Send(c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	if err := checkJSON(res); err != nil {
		return nil, err
	}
	return res, nil
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

// result is the result of a statement that runs with Exec. InfluxDB counts
// no rows for a statement.
type result struct{}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result.
func (result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("reading the rows affected: %w", dbimp.ErrNotSupported)
}
