package exasol

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

// options are the options of one statement.
type options struct {
	timeout  time.Duration
	readonly bool
	params   map[string]any
	schema   string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout ends the statement after d. When d passes, the driver sends
// abortQuery, so the server stops the statement, and the connection stays
// usable (D211). The error is context.DeadlineExceeded. Zero sets no timeout.
// The key querytimeout of the DSN is the limit that the server keeps itself.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks for a statement that the server refuses to write with. The
// server has no read-only statement or transaction, so WithReadonly(true) fails
// with dbimp.ErrNotSupported. A user with no write privilege is read-only.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any member of the request of the statement by its name,
// such as "attributes". The value is encoded with json/v2. A member named here
// replaces one that the driver sets itself, such as "sqlText".
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the schema of the statement, as WithSchema does. The
// schema is the database of Exasol.
func WithDatabase(name string) Option {
	return WithSchema(name)
}

// WithSchema opens the schema before the statement, if the session has
// another one open. The schema stays open after the statement, because it is
// an attribute of the session, as the path of the DSN is.
func WithSchema(name string) Option {
	return func(o *options) { o.schema = name }
}

// resolve returns the options of one statement, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{schema: cfg.Schema}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	return nil
}
