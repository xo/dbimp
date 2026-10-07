package trino

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// state is what a connection keeps for the server, because the servers keep
// none: a statement that changes the session answers it in the headers of its
// response, and the client sends it back with each later request (measured).
// A value is kept as the server wrote it, so it goes back as it came.
type state struct {
	catalog string
	schema  string
	// session holds the properties of the session, with escaped values.
	session map[string]string
	// prepared holds the statements that PREPARE named, with escaped texts.
	prepared map[string]string
	// tx is the id of the transaction that the connection started, or "".
	tx string
}

// conn is one connection. Its state is on the client, because Trino and
// Presto hold none for a client.
type conn struct {
	c      *Connector
	flavor string
	prefix string
	st     state
}

// ensure the interfaces.
var (
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.NamedValueChecker  = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
	_ driver.SessionResetter    = (*conn)(nil)
)

// ResetSession satisfies driver.SessionResetter. A statement such as USE or
// SET SESSION changes the state that the connection keeps, and database/sql
// hands the connection to the next caller, who must not see it.
func (cn *conn) ResetSession(context.Context) error {
	cn.reset()
	return nil
}

// CheckNamedValue satisfies driver.NamedValueChecker. It keeps an Option,
// which the statement takes out (D109), and the values that the driver writes
// as a literal of their own type: a decimal, the types of the root package, a
// UUID, a list and a map (D175). A nil pointer that implements
// driver.Valuer becomes nil. It hands every other value to the default
// converter of database/sql.
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
	case dbimp.Date, dbimp.LocalTime, dbimp.OffsetTime, dbimp.LocalDateTime, dbimp.Interval,
		uuid.UUID, []any, []string, []int64, []float64, []bool, map[string]any:
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

// ExecContext satisfies driver.ExecerContext. It reads the result to its end,
// so that the count of the rows and the headers of the response arrive.
func (cn *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r, err := cn.query(ctx, query, args)
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

// PrepareContext satisfies driver.ConnPrepareContext. The statement is sent
// when it runs, with its arguments.
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

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the user and
// the password. GET /v1/info needs neither (measured).
func (cn *conn) Ping(ctx context.Context) error {
	_, err := cn.ExecContext(ctx, "SELECT 1", nil)
	return err
}

// BeginTx satisfies driver.ConnBeginTx. It sends START TRANSACTION with the
// header of the transaction set to NONE, which the servers need to start one,
// and keeps the id that they answer in the header Started-Transaction-Id. It
// sends the id with each later statement. COMMIT and ROLLBACK end it. The
// catalog decides whether a write in a transaction works (D175).
func (cn *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	level, err := isolation(sql.IsolationLevel(opts.Isolation))
	if err != nil {
		return nil, err
	}
	statement := "START TRANSACTION"
	var modes []string
	if level != "" {
		modes = append(modes, level)
	}
	if opts.ReadOnly {
		modes = append(modes, "READ ONLY")
	}
	if len(modes) > 0 {
		statement += " " + strings.Join(modes, ", ")
	}
	o, _ := resolve(ctx, &cn.c.cfg, nil)
	if err := o.check(); err != nil {
		return nil, err
	}
	if cn.st.tx != "" {
		return nil, fmt.Errorf("beginning a transaction: the connection has one: %w", dbimp.ErrNotSupported)
	}
	h := cn.headers(o)
	h.Set(cn.prefix+"Transaction-Id", "NONE")
	r, err := cn.start(ctx, statement, h)
	if err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	defer r.Close()
	for {
		if err := r.NextRow(); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("beginning a transaction: %w", err)
		}
	}
	if cn.st.tx == "" {
		return nil, fmt.Errorf("beginning a transaction: the server started none: %w", dbimp.ErrInvalidValue)
	}
	return &tx{cn: cn, ctx: context.WithoutCancel(ctx)}, nil
}

// reset sets the state of the connection to the one that the DSN names.
func (cn *conn) reset() {
	cfg := &cn.c.cfg
	cn.st = state{catalog: cfg.Catalog, schema: cfg.Schema, session: map[string]string{}, prepared: map[string]string{}}
	for name, value := range cfg.Session {
		cn.st.session[name] = escape(value)
	}
}

// query sends the statement, with its arguments as the literals of EXECUTE,
// and reads its answer up to the columns.
func (cn *conn) query(ctx context.Context, query string, args []driver.NamedValue) (*rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sending the query: %w", err)
	}
	o, args := resolve(ctx, &cn.c.cfg, args)
	if err := o.check(); err != nil {
		return nil, err
	}
	h := cn.headers(o)
	if len(args) > 0 {
		lits, err := literals(args, cn.flavor == FlavorPresto)
		if err != nil {
			return nil, err
		}
		name := "dbimp_" + strings.ToLower(rand.Text())
		h.Add(cn.prefix+"Prepared-Statement", name+"="+escape(query))
		query = "EXECUTE " + name + " USING " + lits
	}
	return cn.start(ctx, query, h)
}

// headers returns the headers of a statement: the user, the source, the
// catalog and the schema, the time zone, the capabilities, the properties of
// the session, the prepared statements that PREPARE named, and the
// transaction.
func (cn *conn) headers(o options) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set(cn.prefix+"User", cn.c.cfg.User)
	h.Set(cn.prefix+"Source", o.source)
	catalog, schema := cn.st.catalog, cn.st.schema
	if o.catalog != "" {
		catalog = o.catalog
	}
	if o.schema != "" {
		schema = o.schema
	}
	if catalog != "" {
		h.Set(cn.prefix+"Catalog", catalog)
	}
	if schema != "" {
		h.Set(cn.prefix+"Schema", schema)
	}
	if o.timeZone != "" {
		h.Set(cn.prefix+"Time-Zone", o.timeZone)
	}
	if caps := cn.c.capabilitiesOf(cn.flavor); caps != "" {
		h.Set(cn.prefix+"Client-Capabilities", caps)
	}
	props := o.properties(cn.st.session)
	for _, name := range slices.Sorted(maps.Keys(props)) {
		h.Add(cn.prefix+"Session", name+"="+props[name])
	}
	for _, name := range slices.Sorted(maps.Keys(cn.st.prepared)) {
		h.Add(cn.prefix+"Prepared-Statement", name+"="+cn.st.prepared[name])
	}
	if cn.st.tx != "" {
		h.Set(cn.prefix+"Transaction-Id", cn.st.tx)
	}
	return h
}

// apply takes the state that a response asks the client to keep (measured):
// the catalog and the schema of USE, the properties of SET SESSION and RESET
// SESSION, the statements of PREPARE and DEALLOCATE, and the transaction of
// START TRANSACTION, COMMIT and ROLLBACK.
func (cn *conn) apply(h http.Header) {
	p := cn.prefix
	for _, v := range h.Values(p + "Set-Catalog") {
		cn.st.catalog = v
	}
	for _, v := range h.Values(p + "Set-Schema") {
		cn.st.schema = v
	}
	for _, v := range h.Values(p + "Set-Session") {
		if name, value, ok := strings.Cut(v, "="); ok {
			cn.st.session[name] = value
		}
	}
	for _, v := range h.Values(p + "Clear-Session") {
		delete(cn.st.session, v)
	}
	for _, v := range h.Values(p + "Added-Prepare") {
		if name, text, ok := strings.Cut(v, "="); ok {
			cn.st.prepared[name] = text
		}
	}
	for _, v := range h.Values(p + "Deallocated-Prepare") {
		delete(cn.st.prepared, v)
	}
	for _, v := range h.Values(p + "Started-Transaction-Id") {
		cn.st.tx = v
	}
	if len(h.Values(p+"Clear-Transaction-Id")) > 0 {
		cn.st.tx = ""
	}
}

// start sends the statement as the body of POST /v1/statement, and reads the
// answer up to the columns. If the connection can have reached the server, a
// later request never wraps driver.ErrBadConn, because database/sql would run
// the statement again.
func (cn *conn) start(ctx context.Context, statement string, h http.Header) (*rows, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cn.c.base+"/v1/statement", strings.NewReader(statement))
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header = h
	dbimp.SetAuth(req, dbimp.AuthBasic, "", cn.c.cfg.User, cn.c.cfg.Password)
	res, err := dbimp.Send(cn.c.client, req)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	cn.apply(res.Header)
	r := newRows(ctx, cn, res.Body)
	if err := r.open(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

// poll sends GET to the nextUri of a page, with the user and the secret, and
// returns a response with a 2xx status. It sends no request again, and the
// error never wraps driver.ErrBadConn, because the statement reached the
// server.
func (cn *conn) poll(ctx context.Context, next string) (*http.Response, error) {
	target, err := cn.c.target(cn.flavor, next)
	if err != nil {
		return nil, err
	}
	res, err := cn.do(ctx, http.MethodGet, target)
	if err != nil {
		return nil, fmt.Errorf("reading the next page: %w", err)
	}
	if err := checkStatus(res); err != nil {
		return nil, err
	}
	cn.apply(res.Header)
	return res, nil
}

// do sends a request with the user and the secret, once.
func (cn *conn) do(ctx context.Context, method, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	req.Header.Set(cn.prefix+"User", cn.c.cfg.User)
	dbimp.SetAuth(req, dbimp.AuthBasic, "", cn.c.cfg.User, cn.c.cfg.Password)
	res, err := cn.c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending the request: %w", err)
	}
	return res, nil
}

// stop sends DELETE to the nextUri of a query, which cancels it, with ctx
// without its end and the limit of a stop, because ctx can have ended. The
// servers answer HTTP 204, and the same for a query that has ended or that
// does not exist (measured).
func (cn *conn) stop(ctx context.Context, next string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	target, err := cn.c.target(cn.flavor, next)
	if err != nil {
		return err
	}
	res, err := cn.do(ctx, http.MethodDelete, target)
	if err != nil {
		return fmt.Errorf("cancelling the query: %w", err)
	}
	if err := checkStatus(res); err != nil {
		return fmt.Errorf("cancelling the query: %w", err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, maxAnswer)); err != nil {
		return fmt.Errorf("reading the answer to the cancel: %w", err)
	}
	return nil
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

// result is the result of a statement that runs with Exec.
type result struct {
	count int64
	known bool
}

// LastInsertId satisfies driver.Result. The servers have no id of an insert.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: the servers have none: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. A statement that writes answers
// updateCount, and a statement of DDL answers none (measured).
func (r result) RowsAffected() (int64, error) {
	if !r.known {
		return 0, fmt.Errorf("reading the rows affected: the server gave no count: %w", dbimp.ErrNotSupported)
	}
	return r.count, nil
}

// txTimeout bounds COMMIT and ROLLBACK, which run with the context of the
// transaction without its end, because database/sql rolls a transaction back
// after its context ended.
const txTimeout = 30 * time.Second

// tx is a transaction that the server holds, which START TRANSACTION began.
// It keeps the context of BeginTx without its end, because Commit and
// Rollback take none (D175).
type tx struct {
	cn  *conn
	ctx context.Context //nolint:containedctx // D175: database/sql gives Commit and Rollback no context of their own.
}

// isolation returns the clause of START TRANSACTION for a level, which the
// servers name as SQL does (measured).
func isolation(level sql.IsolationLevel) (string, error) {
	switch level {
	case sql.LevelDefault:
		return "", nil
	case sql.LevelReadUncommitted:
		return "ISOLATION LEVEL READ UNCOMMITTED", nil
	case sql.LevelReadCommitted:
		return "ISOLATION LEVEL READ COMMITTED", nil
	case sql.LevelRepeatableRead:
		return "ISOLATION LEVEL REPEATABLE READ", nil
	case sql.LevelSerializable:
		return "ISOLATION LEVEL SERIALIZABLE", nil
	}
	return "", fmt.Errorf("beginning a transaction at the level %v: %w", level, dbimp.ErrNotSupported)
}

// Commit satisfies driver.Tx.
func (t *tx) Commit() error {
	return t.end("COMMIT")
}

// Rollback satisfies driver.Tx.
func (t *tx) Rollback() error {
	return t.end("ROLLBACK")
}

// end runs COMMIT or ROLLBACK. The connection forgets the transaction in
// either case, because one that failed to end is lost, and the server drops it.
func (t *tx) end(statement string) error {
	ctx, cancel := context.WithTimeout(t.ctx, txTimeout)
	defer cancel()
	defer func() { t.cn.st.tx = "" }()
	if _, err := t.cn.ExecContext(ctx, statement, nil); err != nil {
		return fmt.Errorf("ending the transaction with %s: %w", statement, err)
	}
	return nil
}
