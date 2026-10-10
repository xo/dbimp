package spanner

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// conn is one connection. It holds the transaction that BeginTx made, and
// nothing else on the server, because every connection of a connector shares the
// multiplexed session (D191 item 3).
type conn struct {
	c  *Connector
	tx *txn
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
// the statement takes out (D109), and the values that the driver binds with a
// type of its own: a float32, a decimal, a dbimp.Date, a dbimp.Interval, a UUID, JSON text,
// a map, which is a JSON value, and a slice, which is an ARRAY. A nil pointer
// that implements driver.Valuer becomes nil. It hands every other value to the
// default converter of database/sql (W4).
func (c *conn) CheckNamedValue(nv *driver.NamedValue) error {
	if dbimp.IsOption[options](nv.Value) {
		return nil
	}
	switch nv.Value.(type) {
	case float32, *apd.Decimal, apd.Decimal, dbimp.Date, dbimp.Interval, uuid.UUID, jsontext.Value, map[string]any:
		v, err := canon(nv.Value)
		nv.Value = v
		return err
	}
	rv := reflect.ValueOf(nv.Value)
	if rv.IsValid() && rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() != reflect.Uint8 {
		a, err := makeArray(rv)
		if err != nil {
			return err
		}
		nv.Value = a
		return nil
	}
	if _, ok := nv.Value.(driver.Valuer); ok && rv.Kind() == reflect.Pointer && rv.IsNil() {
		nv.Value = nil
		return nil
	}
	return driver.ErrSkip
}

// kind is the kind of a statement, which decides how the driver sends it.
type kind int

const (
	// kindQuery is any statement that reads. It runs in a read-only
	// transaction of one use.
	kindQuery kind = iota
	// kindDML is INSERT, UPDATE, DELETE and MERGE. It needs a transaction that
	// writes (recorded: "a DML statement with no transaction"), so the driver
	// begins one and commits it, unless the caller is in a transaction.
	kindDML
	// kindDDL goes to updateDatabaseDdl, because executeSql refuses it
	// (recorded: "a CREATE TABLE statement sent to executeSql").
	kindDDL
)

// classify returns the kind of a statement from its first word, after the
// white space, the comments and the hint before it.
func classify(query string) kind {
	s := skipLeading(query)
	end := 0
	for end < len(s) && (s[end] >= 'a' && s[end] <= 'z' || s[end] >= 'A' && s[end] <= 'Z') {
		end++
	}
	switch strings.ToUpper(s[:end]) {
	case "CREATE", "ALTER", "DROP", "RENAME", "GRANT", "REVOKE", "ANALYZE":
		return kindDDL
	case "INSERT", "UPDATE", "DELETE", "MERGE":
		return kindDML
	}
	return kindQuery
}

// skipLeading returns s without the white space, the comments (--, # and
// /* */) and the statement hint (@{...}) that start it.
func skipLeading(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n\f\v")
		switch {
		case strings.HasPrefix(s, "--"), strings.HasPrefix(s, "#"):
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return ""
			}
			s = s[i+1:]
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s[2:], "*/")
			if i < 0 {
				return ""
			}
			s = s[2+i+2:]
		case strings.HasPrefix(s, "@{"):
			i := strings.IndexByte(s, '}')
			if i < 0 {
				return ""
			}
			s = s[i+1:]
		default:
			return s
		}
	}
}

// plan is a statement with its options, ready to send.
type plan struct {
	o        options
	args     []driver.NamedValue
	database string
	kind     kind
}

// QueryContext satisfies driver.QueryerContext.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	p, err := c.plan(ctx, query, args)
	if err != nil {
		return nil, err
	}
	if p.kind == kindDDL {
		if err := c.runDDL(ctx, p, query); err != nil {
			return nil, err
		}
		return emptyRows{}, nil
	}
	r, err := c.run(ctx, p, query, false)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ExecContext satisfies driver.ExecerContext. It reads the result to its end
// without decoding a value, because the count of the rows that the statement
// changed comes in the last message.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	p, err := c.plan(ctx, query, args)
	if err != nil {
		return nil, err
	}
	if p.kind == kindDDL {
		if err := c.runDDL(ctx, p, query); err != nil {
			return nil, err
		}
		return result{}, nil
	}
	r, err := c.run(ctx, p, query, true)
	if err != nil {
		return nil, err
	}
	for {
		if err := r.NextRow(); err != nil {
			if isEOF(err) {
				break
			}
			_ = r.Close()
			return nil, err
		}
	}
	if err := r.Close(); err != nil {
		return nil, err
	}
	n, ok := r.affected()
	return result{affected: n, known: ok}, nil
}

// streamRequest is the body of executeStreamingSql (recorded).
type streamRequest struct {
	SQL         string              `json:"sql"`
	Transaction any                 `json:"transaction,omitzero"`
	Seqno       string              `json:"seqno,omitzero"`
	Params      map[string]any      `json:"params,omitzero"`
	ParamTypes  map[string]wireType `json:"paramTypes,omitzero"`
}

// selector is the member transaction of a request that names a transaction.
type selector struct {
	ID string `json:"id"`
}

// readOnlyOnce returns the member transaction of a request that runs in a
// read-only transaction of one use, which no statement can write in (recorded:
// "a DML statement in a read only transaction").
func readOnlyOnce() jsontext.Value {
	return jsontext.Value(`{"singleUse":{"readOnly":{"strong":true}}}`)
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

// Close satisfies driver.Conn. A connection holds nothing to close on the
// server, and a transaction that is still open ends with the session of its
// server (D191 item 3).
func (c *conn) Close() error {
	return nil
}

// Ping satisfies driver.Pinger. It runs SELECT 1, which checks the token, the
// session and the login, and costs little.
func (c *conn) Ping(ctx context.Context) error {
	_, err := c.ExecContext(ctx, "SELECT 1", nil)
	return err
}

// Begin satisfies driver.Conn. database/sql calls BeginTx.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("beginning a transaction without a context: %w", dbimp.ErrNotSupported)
}

// BeginTx satisfies driver.ConnBeginTx. It calls beginTransaction, for a
// transaction that writes or one that only reads. The isolation levels are the
// default, which is serializable, and repeatable read. The context of BeginTx
// lives as long as the transaction, as database/sql defines it, and the
// transaction keeps it for its commit (D191 item 6, and D45 for the rule).
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.tx != nil {
		return nil, fmt.Errorf("beginning a transaction: one is open: %w", dbimp.ErrNotSupported)
	}
	level := ""
	switch sql.IsolationLevel(opts.Isolation) {
	case sql.LevelDefault, sql.LevelSerializable:
	case sql.LevelRepeatableRead:
		level = "REPEATABLE_READ"
	default:
		return nil, fmt.Errorf("beginning a transaction with the isolation %s: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported)
	}
	o, _ := resolve(ctx, nil)
	if err := o.check(); err != nil {
		return nil, err
	}
	database := c.c.cfg.Database
	if o.database != "" {
		database = o.database
	}
	readOnly := opts.ReadOnly || o.readonly
	if readOnly && level != "" {
		return nil, fmt.Errorf("beginning a read-only transaction with the isolation %s: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported)
	}
	beginParams, commitParams := o.split()
	t, err := c.c.begin(ctx, database, readOnly, level, beginParams)
	if err != nil {
		if serr, ok := errors.AsType[*Error](err); ok && level != "" && serr.Status == "INVALID_ARGUMENT" {
			return nil, fmt.Errorf("beginning a transaction with the isolation %s: the server refused it: %w: %w", sql.IsolationLevel(opts.Isolation), dbimp.ErrNotSupported, err)
		}
		return nil, err
	}
	t.commitParams = commitParams
	c.tx = t
	return &tx{c: c, t: t, ctx: ctx}, nil
}

// plan resolves the options of a statement, and chooses its database.
func (c *conn) plan(ctx context.Context, query string, args []driver.NamedValue) (plan, error) {
	if err := ctx.Err(); err != nil {
		return plan{}, fmt.Errorf("sending the statement: %w", context.Cause(ctx))
	}
	o, args := resolve(ctx, args)
	if err := o.check(); err != nil {
		return plan{}, err
	}
	p := plan{o: o, args: args, database: c.c.cfg.Database, kind: classify(query)}
	if o.database != "" {
		p.database = o.database
	}
	if c.tx != nil {
		if o.database != "" && o.database != c.tx.db {
			return plan{}, fmt.Errorf("applying the option WithDatabase: the transaction runs in the database %s: %w", c.tx.db, dbimp.ErrNotSupported)
		}
		p.database = c.tx.db
	}
	return p, nil
}

// runDDL sends a DDL statement, and waits until the server is done with it.
func (c *conn) runDDL(ctx context.Context, p plan, query string) error {
	switch {
	case c.tx != nil:
		return fmt.Errorf("running a DDL statement: it cannot run in a transaction: %w", dbimp.ErrNotSupported)
	case p.o.readonly:
		return fmt.Errorf("running a DDL statement: the option WithReadonly asks that the statement write nothing: %w", dbimp.ErrNotSupported)
	case len(p.args) > 0:
		return fmt.Errorf("running a DDL statement: it takes no argument: %w", dbimp.ErrArguments)
	}
	text := strings.TrimRight(strings.TrimSpace(query), "; \t\r\n")
	return c.c.ddl(ctx, p.database, text)
}

// run sends a query or a DML statement and returns its rows, read up to the
// metadata. A DML statement outside a transaction runs in a transaction that
// the driver begins, and the rows commit it when the caller closes them or
// reads them to the end (D191 item 6).
func (c *conn) run(ctx context.Context, p plan, query string, skip bool) (*rows, error) {
	b, err := bindArgs(query, p.args)
	if err != nil {
		return nil, err
	}
	req := streamRequest{SQL: b.text, Params: b.params, ParamTypes: b.paramTypes}
	t := c.tx
	var finish func(ok bool) error
	switch {
	case t != nil:
	case p.o.readonly:
		req.Transaction = readOnlyOnce()
	case p.kind == kindDML:
		if t, err = c.c.begin(ctx, p.database, false, "", nil); err != nil {
			return nil, err
		}
		finish = func(ok bool) error { return c.c.finish(ctx, t, ok) }
	}
	if t != nil {
		req.Transaction = selector{ID: t.id}
		req.Seqno = t.nextSeq()
	}
	body, err := dbimp.MarshalParams(req, p.o.params)
	if err != nil {
		if finish != nil {
			_ = finish(false)
		}
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	res, err := c.c.stream(ctx, p.database, body)
	if err != nil {
		if finish != nil {
			_ = finish(false)
		}
		return nil, err
	}
	r, err := newRows(ctx, res, t, skip, finish)
	if err != nil {
		if finish != nil {
			_ = finish(false)
		}
		return nil, err
	}
	return r, nil
}

// txn is a transaction of the server.
type txn struct {
	db       string
	id       string
	readOnly bool
	// commitParams are the members of the commit that WithParameter set.
	commitParams map[string]any

	seq atomic.Int64
	mu  sync.Mutex
	pre precommit
	// hasPre is true once an answer held the member precommitToken, also with
	// no token in it, as the emulator answers on a multiplexed session. The
	// commit then sends the member, and the emulator commits only when it comes
	// (D196).
	hasPre bool
}

// nextSeq returns the number of the next statement of the transaction, as the
// text that seqno holds. The server wants it to grow for each statement
// (recorded: "a DML statement with no seqno").
func (t *txn) nextSeq() string {
	return strconv.FormatInt(t.seq.Add(1), 10)
}

// setPrecommit keeps the precommit token with the highest seqNum. A commit on
// a multiplexed session sends it. A nil transaction keeps nothing.
func (t *txn) setPrecommit(p precommit) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hasPre = true
	if p.Token != "" && p.SeqNum >= t.pre.SeqNum {
		t.pre = p
	}
}

// precommitToken returns the token to send, or nil.
func (t *txn) precommitToken() *precommit {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.hasPre {
		return nil
	}
	p := t.pre
	return &p
}

// begin calls beginTransaction in the session of the database. The id of the
// transaction is base64 text (recorded: "begin a read write transaction").
func (c *Connector) begin(ctx context.Context, database string, readOnly bool, level string, params map[string]any) (*txn, error) {
	opts := map[string]any{"readWrite": map[string]any{}}
	if readOnly {
		opts = map[string]any{"readOnly": map[string]any{"strong": true}}
	}
	if level != "" {
		opts["isolationLevel"] = level
	}
	body, err := dbimp.MarshalParams(struct {
		Options map[string]any `json:"options"`
	}{opts}, params)
	if err != nil {
		return nil, fmt.Errorf("writing the request: %w", err)
	}
	var ans struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, database, "beginTransaction", body, &ans); err != nil {
		return nil, fmt.Errorf("beginning a transaction: %w", err)
	}
	if ans.ID == "" {
		return nil, fmt.Errorf("beginning a transaction: the answer has no id: %w", dbimp.ErrInvalidValue)
	}
	return &txn{db: database, id: ans.ID, readOnly: readOnly}, nil
}

// commitRequest is the body of commit.
type commitRequest struct {
	TransactionID string     `json:"transactionId"`
	Precommit     *precommit `json:"precommitToken,omitzero"`
}

// commit commits the transaction. A transaction that only reads sends nothing,
// because the server refuses its commit (recorded: "commit a read only
// transaction"). The answer to a commit on a multiplexed session can hold a
// new precommit token and no commit timestamp. The documentation of the
// CommitResponse says that the commit then goes again with that token (not
// measured), so the driver sends it once more.
func (t *txn) commit(ctx context.Context, c *Connector) error {
	if t.readOnly {
		return nil
	}
	for attempt := 0; ; attempt++ {
		body, err := dbimp.MarshalParams(commitRequest{TransactionID: t.id, Precommit: t.precommitToken()}, t.commitParams)
		if err != nil {
			return fmt.Errorf("writing the request: %w", err)
		}
		var ans struct {
			CommitTimestamp string     `json:"commitTimestamp"`
			Precommit       *precommit `json:"precommitToken"`
		}
		if err := c.post(ctx, t.db, "commit", body, &ans); err != nil {
			return fmt.Errorf("committing the transaction: %w", err)
		}
		if ans.CommitTimestamp != "" {
			return nil
		}
		if ans.Precommit == nil || attempt > 0 {
			return fmt.Errorf("committing the transaction: the answer has no commit timestamp: %w", dbimp.ErrInvalidValue)
		}
		t.setPrecommit(*ans.Precommit)
	}
}

// rollback rolls the transaction back, with ctx without its end and the limit
// of a stop, because ctx can have ended. A transaction that only reads sends
// nothing.
func (t *txn) rollback(ctx context.Context, c *Connector) error {
	if t.readOnly {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	body, err := json.Marshal(struct {
		TransactionID string `json:"transactionId"`
	}{t.id})
	if err != nil {
		return fmt.Errorf("writing the request: %w", err)
	}
	if err := c.post(ctx, t.db, "rollback", body, nil); err != nil {
		return fmt.Errorf("rolling the transaction back: %w", err)
	}
	return nil
}

// finish ends a transaction that the driver began for one statement. It
// commits when ok is true and the context did not end, and it rolls back
// otherwise.
func (c *Connector) finish(ctx context.Context, t *txn, ok bool) error {
	if !ok {
		return t.rollback(ctx, c)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(fmt.Errorf("committing the transaction: %w", context.Cause(ctx)), t.rollback(ctx, c))
	}
	return t.commit(ctx, c)
}

// tx is a transaction that BeginTx made. It keeps the context of BeginTx,
// which database/sql defines as the lifetime of the transaction, so that Commit
// and Rollback, which take no context, send their requests with it (D45).
type tx struct {
	c   *conn
	t   *txn
	ctx context.Context //nolint:containedctx // database/sql gives Commit no context, and D45 allows it for a transaction.
}

// Commit satisfies driver.Tx. An answer ABORTED is an error that wraps
// ErrAborted, and the caller runs the whole transaction again (D191 item 6).
func (x *tx) Commit() error {
	x.c.tx = nil
	return x.t.commit(x.ctx, x.c.c)
}

// Rollback satisfies driver.Tx.
func (x *tx) Rollback() error {
	x.c.tx = nil
	return x.t.rollback(x.ctx, x.c.c)
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
	// affected is the count of the rows that the statement changed, and known
	// is false when the answer holds no count.
	affected int64
	known    bool
}

// LastInsertId satisfies driver.Result. Spanner has no such id, and
// INSERT ... THEN RETURN returns the key.
func (result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("reading the id of the last insert: Spanner has no such id, so use THEN RETURN: %w", dbimp.ErrNotSupported)
}

// RowsAffected satisfies driver.Result. It is rowCountExact of the answer. A
// statement whose answer holds no count, such as a query and a DDL statement,
// has none, and the error wraps dbimp.ErrNotSupported, as D178 says.
func (r result) RowsAffected() (int64, error) {
	if !r.known {
		return 0, fmt.Errorf("reading the rows affected: the answer of this statement holds no count: %w", dbimp.ErrNotSupported)
	}
	return r.affected, nil
}
