package neo4j

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
	cancel   string
}

// WithOptions returns a context that carries opts. Each statement and each
// transaction started with the context applies them after the options of
// the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement, as
// maxExecutionTime, which counts whole seconds, so the driver rounds d up to
// the next second (measured by hand on 2026.09.0). The server stops a
// statement that runs longer, and the error arrives after its rows
// (recorded). 5.26.31 ignores maxExecutionTime (recorded), so on a release
// older than 2026.04 the statement fails with dbimp.ErrNotSupported (D109).
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly makes the server refuse a statement that writes, with
// accessMode READ (measured). In a transaction, it applies to BeginTx.
func WithReadonly(v bool) Option {
	return func(o *options) { o.readonly = v }
}

// WithParameter sets any key of the body of the request by its name, such as
// "includeCounters" or "impersonatedUser". The value is encoded with json/v2.
// A key named here replaces one that the driver sets itself, as in Couchbase.
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

// WithCancel sets how the driver stops the statement on the server when its
// context ends: CancelTag, CancelMetadata or CancelNone, as the key cancel of
// the DSN does (D67).
func WithCancel(how string) Option {
	return func(o *options) { o.cancel = how }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109). database is the database without WithDatabase: that of the DSN, or
// that of the transaction for a statement in one.
func resolve(ctx context.Context, cfg *Config, database string, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{database: database, cancel: cfg.Cancel}, args)
}

// check returns an error for an option whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case !slices.Contains(cancels, o.cancel):
		return fmt.Errorf("applying the option WithCancel: %q: %w", o.cancel, dbimp.ErrInvalidValue)
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	}
	return nil
}
