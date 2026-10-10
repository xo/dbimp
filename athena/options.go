package athena

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the DSN,
// then from the context through WithOptions, then from an argument of the
// statement, and a later one wins.
type Option = dbimp.Option[options]

// options are the options of one statement.
type options struct {
	timeout   time.Duration
	readonly  bool
	params    map[string]any
	database  string
	workgroup string
	output    string
	catalog   string
}

// WithOptions returns a context that carries opts. Each statement started with
// the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement. A request of
// Athena has no such setting, because only the workgroup sets the timeout of
// its queries, so a positive value fails the statement with
// dbimp.ErrNotSupported. The context of the statement is the way to bound it,
// and the driver stops the query when the context ends. Zero asks for nothing.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. Athena has no setting
// that makes it refuse a write for one request, so WithReadonly(true) fails the
// statement with dbimp.ErrNotSupported. A policy of IAM that allows reads only
// makes a read-only principal.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any key of the body of StartQueryExecution by its name,
// such as "ResultReuseConfiguration". The value is encoded with json/v2. A key
// named here replaces one that the driver sets itself, such as "QueryString" or
// "ExecutionParameters", as in Couchbase.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the database of the Glue Data Catalog for one statement, as
// the path of the DSN does. It is Database of QueryExecutionContext.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithWorkGroup sets the workgroup of one statement, as the key workgroup of the
// DSN does.
func WithWorkGroup(name string) Option {
	return func(o *options) { o.workgroup = name }
}

// WithOutput sets the S3 location of the results of one statement, as the key
// output of the DSN does. A workgroup that enforces its own location ignores
// it (recorded: "another result configuration").
func WithOutput(location string) Option {
	return func(o *options) { o.output = location }
}

// WithCatalog sets the data catalog of one statement, as the key catalog of the
// DSN does. It is Catalog of QueryExecutionContext.
func WithCatalog(name string) Option {
	return func(o *options) { o.catalog = name }
}

// resolve returns the options of one statement, those of the DSN, then those of
// the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		database:  cfg.Database,
		workgroup: cfg.WorkGroup,
		output:    cfg.Output,
		catalog:   cfg.Catalog,
	}, args)
}

// check returns an error for an option that the server cannot honor, or that has
// a value that the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	for name, value := range o.params {
		if name == "" || value == nil {
			return fmt.Errorf("applying the option WithParameter: the key %q has no name or no value: %w", name, dbimp.ErrInvalidValue)
		}
	}
	return nil
}
