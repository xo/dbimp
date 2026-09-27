package dbimptest_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/xo/dbimp"
)

// fake is a small driver built from the root package, for the tests of this
// package. Its server takes {"statement": ...} and answers with
// {"columns": [...], "rows": [[...], ...], "error": ...}.
type fake struct{}

// Open satisfies driver.Driver.
func (fake) Open(string) (driver.Conn, error) {
	return nil, fmt.Errorf("opening without a connector: %w", dbimp.ErrNotSupported)
}

// OpenConnector satisfies driver.DriverContext.
func (fake) OpenConnector(dsn string) (driver.Connector, error) {
	u, err := dbimp.ParseURL("fake", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := dbimp.NewQuery(u); err != nil {
		return nil, err
	}
	t := dbimp.NewTransport(nil)
	return &fakeConnector{
		url:       "http://" + u.Host + "/",
		transport: t,
		client:    dbimp.NewClient(t, false),
	}, nil
}

type fakeConnector struct {
	url       string
	transport *http.Transport
	client    *http.Client
}

func (c *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeConn{c: c}, nil
}

func (c *fakeConnector) Driver() driver.Driver {
	return fake{}
}

func (c *fakeConnector) Close() error {
	c.transport.CloseIdleConnections()
	return nil
}

type fakeConn struct {
	c *fakeConnector
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("preparing: %w", dbimp.ErrNotSupported)
}

func (c *fakeConn) Close() error {
	return nil
}

func (c *fakeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: %w", dbimp.ErrNotSupported)
}

func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: %w", dbimp.ErrNotSupported)
}

func (c *fakeConn) QueryContext(ctx context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	body, err := json.Marshal(map[string]string{"statement": query})
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := dbimp.Send(c.c.client, req)
	if err != nil {
		return nil, err
	}
	if err := dbimp.CheckStatus(res); err != nil {
		return nil, err
	}
	s := dbimp.NewStream(res.Body)
	r, err := newFakeRows(s)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return r, nil
}

type fakeRows struct {
	s    *dbimp.Stream
	cols []string
	rows *dbimp.ArrayRows
	vals []jsontext.Value
}

func newFakeRows(s *dbimp.Stream) (*fakeRows, error) {
	dec := s.Decoder()
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return nil, fmt.Errorf("reading the response: %w", errors.Join(err, dbimp.ErrInvalidValue))
	}
	r := &fakeRows{s: s}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading the response: %w", err)
		}
		switch tok.String() {
		case "columns":
			if err := json.UnmarshalDecode(dec, &r.cols); err != nil {
				return nil, fmt.Errorf("reading the columns: %w", err)
			}
		case "rows":
			if r.rows, err = dbimp.NewArrayRows(dec, len(r.cols)); err != nil {
				return nil, err
			}
			r.vals = make([]jsontext.Value, len(r.cols))
			return r, nil
		default:
			if err := dec.SkipValue(); err != nil {
				return nil, fmt.Errorf("reading the response: %w", err)
			}
		}
	}
	return nil, fmt.Errorf("reading the response: no rows: %w", dbimp.ErrInvalidValue)
}

func (r *fakeRows) Columns() []string {
	return r.cols
}

func (r *fakeRows) Close() error {
	return r.s.Close()
}

func (r *fakeRows) NextRow() error {
	err := r.rows.Next(r.vals)
	if !errors.Is(err, io.EOF) {
		return err
	}
	dec := r.s.Decoder()
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		name := tok.String()
		val, err := dec.ReadValue()
		if err != nil {
			return fmt.Errorf("reading the end of the response: %w", err)
		}
		if name == "error" && !dbimp.IsNull(val) {
			msg, err := dbimp.String(val)
			if err != nil {
				return err
			}
			return fmt.Errorf("the server failed after the rows: %s: %w", msg, dbimp.ErrIncomplete)
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return fmt.Errorf("reading the end of the response: %w", err)
	}
	if err := r.s.End(); err != nil {
		return err
	}
	return io.EOF
}

func (r *fakeRows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	v, err := r.value(i)
	if err != nil {
		return err
	}
	return dbimp.Assign(scanCtx, dest, v)
}

func (r *fakeRows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	for i := range dest {
		v, err := r.value(i)
		if err != nil {
			return err
		}
		dest[i] = v
	}
	return nil
}

func (r *fakeRows) value(i int) (any, error) {
	v := r.vals[i]
	switch {
	case dbimp.IsNull(v):
		return nil, nil
	case v.Kind() == '"':
		return dbimp.String(v)
	}
	return dbimp.Number(v)
}

// openFake opens the fake driver against the server at url.
func openFake(url string) (*sql.DB, error) {
	c, err := fake{}.OpenConnector("fake" + url[len("http"):])
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(c), nil
}
