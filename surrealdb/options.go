package surrealdb

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, then from an argument of
// the statement, and a later one wins. The driver has no transaction (D54),
// so no option applies to BeginTx.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout   time.Duration
	readonly  bool
	params    map[string]any
	namespace string
	database  string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout would set the time that the server gives the request. The RPC
// method query takes the text and the variables only, and the server has a
// timeout for all requests only (the RPC manual). So a statement with it
// fails with dbimp.ErrNotSupported (D109). The TIMEOUT clause of SurrealQL
// limits one statement.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly would make the server refuse a write. The RPC protocol has no
// read-only mode (the RPC manual), so a statement with it fails with
// dbimp.ErrNotSupported (D109).
func WithReadonly(v bool) Option {
	return func(o *options) { o.readonly = v }
}

// WithParameter sets any key of the body of the RPC request by its name,
// such as "id". The value is encoded as a variable is (D53). A key named
// here replaces one that the driver sets itself, as in Couchbase. The server
// ignores a key that it does not know (measured by hand on 2.7.0 and 3.3.0).
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithNamespace sets the namespace of the statement, which the header
// Surreal-NS names, as the first segment of the path of the DSN does. The
// credentials stay those of the DSN.
func WithNamespace(name string) Option {
	return func(o *options) { o.namespace = name }
}

// WithDatabase sets the database of the statement, which the header
// Surreal-DB names, as the second segment of the path of the DSN does. The
// credentials stay those of the DSN.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{namespace: cfg.Namespace, database: cfg.Database}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	return nil
}
