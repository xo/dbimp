package dynamodb

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// context through WithOptions, and then from an argument of the statement,
// and a later one wins. The DSN holds no option, because its two keys are
// keys of the connection (D169).
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout  time.Duration
	readonly bool
	params   map[string]any
	database string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement. DynamoDB
// has no such setting, because each request reads one page and ends fast
// (D169), so a positive value fails the statement with dbimp.ErrNotSupported.
// The context of the statement is the way to bound it. Zero asks for
// nothing.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. DynamoDB has no
// setting that makes it refuse a write for one request, so WithReadonly(true)
// fails the statement with dbimp.ErrNotSupported. A policy of IAM that allows
// reads only makes a read-only principal.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any key of the body of ExecuteStatement by its name,
// such as "ConsistentRead", "Limit" or "ReturnConsumedCapacity". The value
// is encoded with json/v2. A key named here replaces one that the driver
// sets itself, such as "Statement" or "Parameters", as in Couchbase. The
// driver sends the keys again with each page of a result, so "Limit" limits
// each page, and not the result.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase names the database of one statement. DynamoDB has no database
// to choose (D169), so a statement with it fails with dbimp.ErrNotSupported.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// resolve returns the options of one request, those of the context, and then
// the Option arguments of args, and the other arguments (D109).
func resolve(ctx context.Context, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value is not valid.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	}
	return nil
}
