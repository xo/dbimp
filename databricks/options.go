package databricks

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
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
	catalog  string
	schema   string
	readonly bool
	// params holds the members of the body that WithParameter set, by name.
	params map[string]any
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the longest time that the driver waits for the statement,
// as the key timeout of the DSN does. The time runs from the request to the end
// of the result. When it ends, the driver cancels the statement on the server
// if it knows its id, and the error is context.DeadlineExceeded. Zero waits
// as long as the context allows.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. The Statement Execution
// API has no such setting for one statement, so WithReadonly(true) fails the
// statement with dbimp.ErrNotSupported (D109).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a member of the body of the request by its name, such as
// "query_tags". It replaces the member of the driver with the same name, as
// dbimp.MarshalParams does (D109). The driver refuses a member that changes how
// it sends the statement or how it reads the result: warehouse_id, statement,
// parameters, wait_timeout, on_wait_timeout, row_limit, byte_limit,
// disposition and format. A value of nil fails the statement with
// dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the schema of one statement, because Spark calls a schema
// a database: current_database() answers the schema (measured). It is the same
// as WithSchema, and as the key schema of the DSN.
func WithDatabase(name string) Option {
	return func(o *options) { o.schema = name }
}

// WithSchema sets the schema of one statement, as the key schema of the DSN
// does.
func WithSchema(name string) Option {
	return func(o *options) { o.schema = name }
}

// WithCatalog sets the catalog of one statement, as the key catalog of the DSN
// does.
func WithCatalog(name string) Option {
	return func(o *options) { o.catalog = name }
}

// resolve returns the options of one statement, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		timeout: cfg.Timeout,
		catalog: cfg.Catalog,
		schema:  cfg.Schema,
	}, args)
}

// refused are the members of the body that WithParameter refuses.
var refused = map[string]bool{
	"warehouse_id":    true,
	"statement":       true,
	"parameters":      true,
	"wait_timeout":    true,
	"on_wait_timeout": true,
	"row_limit":       true,
	"byte_limit":      true,
	"disposition":     true,
	"format":          true,
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0 || o.timeout > maxTimeout:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	for name, value := range o.params {
		switch {
		case name == "" || strings.ContainsFunc(name, func(r rune) bool { return !isNameRune(r) }):
			return fmt.Errorf("applying the option WithParameter: %q is not the name of a member of the body: %w", name, dbimp.ErrInvalidValue)
		case value == nil:
			return fmt.Errorf("applying the option WithParameter: the member %q has no value: %w", name, dbimp.ErrInvalidValue)
		case refused[name]:
			return fmt.Errorf("applying the option WithParameter: the member %s changes how the driver sends the statement or reads the result: %w", name, dbimp.ErrNotSupported)
		}
	}
	return nil
}

// isNameRune reports whether r can be in the name of a member of the body.
func isNameRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}
