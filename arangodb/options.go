package arangodb

import (
	"context"
	"database/sql/driver"
	"fmt"
	"slices"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement or one transaction (D109). An
// option comes from the DSN, then from the context through WithOptions, then
// from an argument of the statement, and a later one wins. BeginTx takes no
// argument, so an option of a transaction comes from the DSN or through
// WithOptions only.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout  time.Duration
	readonly bool
	params   map[string]any
	database string
	batch    int
	cancel   string
}

// WithOptions returns a context that carries opts. Each statement and each
// transaction started with the context applies them after the options of
// the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the query, as
// options.maxRuntime, in seconds with a fraction. The server kills a query
// that runs longer, with HTTP 410 and the error 1500 (measured by hand on
// 3.12.12).
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly makes the transaction of BeginTx read-only, as the ReadOnly of
// sql.TxOptions does: it names no collection for write (D91). The server has
// no read-only mode for one query, so a statement with WithReadonly fails
// with dbimp.ErrNotSupported, unless it runs in a read-only transaction.
func WithReadonly(v bool) Option {
	return func(o *options) { o.readonly = v }
}

// WithParameter sets any key of the body of POST /_api/cursor by its name,
// such as "ttl" or "memoryLimit". The value is encoded with json/v2. A key
// named here replaces one that the driver sets itself, as in Couchbase.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the database of the statement or the transaction, which
// the path of the request names. A statement of a transaction runs in the
// database of the transaction, so another database there fails with
// dbimp.ErrNotSupported.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithBatch sets the batchSize of the cursor, as the key batch of the DSN
// does.
func WithBatch(n int) Option {
	return func(o *options) { o.batch = n }
}

// WithCancel sets how the driver stops the query on the server when its
// context ends, CancelTag or CancelNone, as the key cancel of the DSN does
// (D90).
func WithCancel(how string) Option {
	return func(o *options) { o.cancel = how }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109). database is the database without WithDatabase: that of the DSN, or
// that of the transaction for a statement in one.
func resolve(ctx context.Context, cfg *Config, database string, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{database: database, batch: cfg.Batch, cancel: cfg.Cancel}, args)
}

// options returns the options of one statement, and the other arguments
// (D109). A statement of a transaction runs in the database of the
// transaction. The server has no read-only mode for one query, so WithReadonly
// holds only in a read-only transaction.
func (c *conn) options(ctx context.Context, args []driver.NamedValue) (options, []driver.NamedValue, error) {
	database := c.c.cfg.Database
	if c.tx != nil {
		database = c.tx.database
	}
	o, args := resolve(ctx, &c.c.cfg, database, args)
	switch {
	case c.tx != nil && o.database != c.tx.database:
		return o, nil, fmt.Errorf("running a statement in the database %q: the transaction is in %q: %w", o.database, c.tx.database, dbimp.ErrNotSupported)
	case o.readonly && (c.tx == nil || !c.tx.readonly):
		return o, nil, dbimp.Unsupported("WithReadonly")
	}
	if err := o.check(); err != nil {
		return o, nil, err
	}
	return o, args, nil
}

// check returns an error for an option whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.batch < 1:
		return fmt.Errorf("applying the option WithBatch: %d: %w", o.batch, dbimp.ErrInvalidValue)
	case !slices.Contains(cancels, o.cancel):
		return fmt.Errorf("applying the option WithCancel: %q: %w", o.cancel, dbimp.ErrInvalidValue)
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	}
	return nil
}
