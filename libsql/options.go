package libsql

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, then from an argument of
// the statement, and a later one wins.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout   time.Duration
	readonly  bool
	params    map[string]any
	namespace string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout ends the request of the statement after d. The server stops
// a statement when its client leaves (measured), and Hrana has no timeout of
// its own, so the end of the request stops the statement. Zero sets no
// timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly would make the server refuse a write. The server keeps no
// statement or transaction read-only: it took an INSERT inside BEGIN
// TRANSACTION READONLY (measured). So WithReadonly(true) fails with
// dbimp.ErrNotSupported. A token with the claim ro is read-only.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any key of the statement of Hrana by its name, such as
// "want_rows". The value is encoded with json/v2. A key named here replaces
// one that the driver sets itself, such as "sql" or "args".
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the namespace of the statement, as WithNamespace does.
// A namespace is the database of libSQL.
func WithDatabase(name string) Option {
	return WithNamespace(name)
}

// WithNamespace sets the namespace of the statement, sent as x-namespace,
// as the key namespace of the DSN does (D148). A statement of a transaction
// runs in the namespace of the transaction.
func WithNamespace(name string) Option {
	return func(o *options) { o.namespace = name }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{namespace: cfg.Namespace}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	return nil
}
