package bigquery

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds nothing on the server, because each request
// is its own job and has no session (D189), so it has no transaction and no
// state to reset.
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
// the statement takes out (D109), a decimal, a dbimp.Date, a dbimp.LocalTime, a
// dbimp.LocalDateTime and a dbimp.Interval, which the driver binds with a type
// of their own, and a list and a map, which bind fails with dbimp.ErrArguments
// (D189). A nil pointer that implements driver.Valuer becomes nil. It hands
// every other value to the default converter of database/sql.
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
	case dbimp.Date, dbimp.LocalTime, dbimp.LocalDateTime, dbimp.Interval, []any, map[string]any:
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

// dataset is the member defaultDataset of the request.
type dataset struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
}

// formatOptions is the member formatOptions of the request. The driver asks for
// timestamps as ISO8601_STRING (D189).
type formatOptions struct {
	TimestampOutputFormat string `json:"timestampOutputFormat"`
}

// request is the body of POST /queries (recorded: bigquery-017). The members
// that hold nothing are left out, and useLegacySql is always written, because
// its default is true (docs/BIGQUERY.md).
type request struct {
	Query           string        `json:"query"`
	UseLegacySQL    bool          `json:"useLegacySql"`
	TimeoutMs       int64         `json:"timeoutMs"`
	JobTimeoutMs    int64         `json:"jobTimeoutMs,omitzero"`
	MaxResults      int           `json:"maxResults,omitzero"`
	DefaultDataset  *dataset      `json:"defaultDataset,omitzero"`
	Location        string        `json:"location,omitzero"`
	ParameterMode   string        `json:"parameterMode,omitzero"`
	QueryParameters []queryParam  `json:"queryParameters,omitzero"`
	FormatOptions   formatOptions `json:"formatOptions"`
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the head of the answer,
// which holds the count of the rows that a statement changed, and it reads no
// row. A result that Exec leaves unread stays at the service, and the driver
// cancels nothing, because the job is done.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := c.query(ctx, query, args)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.result(), nil
}

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, with its arguments.
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

// BeginTx satisfies driver.ConnBeginTx. The service refuses BEGIN TRANSACTION
// alone, and a session across requests is a later release, so a transaction
// cannot span two requests (D20 and D189). A script that holds BEGIN
// TRANSACTION and COMMIT TRANSACTION in one request works, because the driver
// sends the text as it is.
func (c *conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction: the first release has no session across requests, so send BEGIN TRANSACTION and COMMIT TRANSACTION in one script: %w", dbimp.ErrNotSupported)
}

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server.
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1 as a dry run, which checks the
// token, the project and the right to make a job, and which the service does
// not bill (recorded: bigquery-020).
func (c *conn) Ping(ctx context.Context) error {
	ctx = WithOptions(ctx, WithParameter("dryRun", true))
	r, err := c.query(ctx, "SELECT 1", nil)
	if err != nil {
		return err
	}
	return r.Close()
}

// query sends the statement with its arguments, polls the job until it ends, and
// reads its answer up to its first row.
func (c *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the statement: %w", err)
	}
	o, args := resolve(ctx, &c.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	mode, params, err := bindArgs(args)
	if err != nil {
		return nil, err
	}
	req := request{
		Query:           query,
		TimeoutMs:       firstWait,
		JobTimeoutMs:    o.timeoutMs(),
		MaxResults:      o.maxResults,
		Location:        o.location,
		ParameterMode:   mode,
		QueryParameters: params,
		FormatOptions:   formatOptions{TimestampOutputFormat: timestampFormat},
	}
	if o.dataset != "" {
		req.DefaultDataset = &dataset{ProjectID: c.c.cfg.Project, DatasetID: o.dataset}
	}
	body, err := dbimp.MarshalParams(req, o.params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	res, err := c.c.start(ctx, body)
	if err != nil {
		return nil, err
	}
	return c.c.newRows(ctx, res, o)
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
	// affected is the count of the rows that the statement changed, and known is
	// false when the answer holds no count.
	affected int64
	known    bool
}

// LastInsertId satisfies driver.Result.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: BigQuery has no such id: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. It is numDmlAffectedRows of the answer
// (recorded: bigquery-025). A statement whose answer holds no count, such as DDL
// and SELECT, has none, and the error wraps dbimp.ErrNotSupported, as D178 says.
func (r result) RowsAffected() (int64, error) {
	if !r.known {
		return 0, fmt.Errorf("reading the rows affected: the answer of this statement holds no count: %w", dbimp.ErrNotSupported)
	}
	return r.affected, nil
}
