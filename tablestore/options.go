package tablestore

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
	timeout  time.Duration
	readonly bool
	params   map[string]any
	database string
}

// WithOptions returns a context that carries opts. Each statement started with
// the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement. The server
// accepts the hint MAX_EXECUTION_TIME in the text, but a test did not show that
// it stops a statement, so a positive value fails the statement with
// dbimp.ErrNotSupported. The context of the statement is the way to bound it.
// Zero asks for nothing.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. Tablestore has no setting
// that makes it refuse a write for one request, so WithReadonly(true) fails the
// statement with dbimp.ErrNotSupported. A policy that allows reads only makes a
// read-only principal.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// The names that WithParameter accepts, which are members of SQLQueryRequest.
const (
	paramSQLVersion  = "sql_version"
	paramSearchToken = "search_token"
)

// WithParameter sets a member of SQLQueryRequest by its name. The driver sets
// sql_version (an integer, 1 by default) and search_token (a string, empty by
// default), and a value here replaces them. The member version is not open to
// the caller, because the driver reads FlatBuffers only (D223). Any other name
// fails the statement with dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the instance of one statement, as the path of the DSN does.
// The host of the DSN names the instance too, so a name that differs from the
// instance of the DSN fails the statement with dbimp.ErrNotSupported.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// resolve returns the options of one statement, those of the DSN, then those of
// the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{database: cfg.Instance}, args)
}

// check returns an error for an option that the server cannot honor, or that has
// a value that the DSN refuses.
func (o options) check(cfg *Config) error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	case o.database != cfg.Instance:
		return dbimp.Unsupported("WithDatabase")
	}
	_, _, err := o.request()
	return err
}

// request returns the sql_version and the search token of the statement. It
// returns an error for a name that WithParameter does not accept, and for a
// value of another type than the member has.
func (o options) request() (int64, string, error) {
	version, token := int64(1), ""
	for name, value := range o.params {
		switch name {
		case paramSQLVersion:
			switch v := value.(type) {
			case int:
				version = int64(v)
			case int64:
				version = v
			default:
				return 0, "", fmt.Errorf("applying the option WithParameter: %q takes an integer, and the value has the type %T: %w", name, value, dbimp.ErrInvalidValue)
			}
		case paramSearchToken:
			v, ok := value.(string)
			if !ok {
				return 0, "", fmt.Errorf("applying the option WithParameter: %q takes a string, and the value has the type %T: %w", name, value, dbimp.ErrInvalidValue)
			}
			token = v
		default:
			return 0, "", fmt.Errorf("applying the option WithParameter: the name %q is not %s or %s: %w", name, paramSQLVersion, paramSearchToken, dbimp.ErrInvalidValue)
		}
	}
	return version, token, nil
}
