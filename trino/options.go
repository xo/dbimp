package trino

import (
	"context"
	"database/sql/driver"
	"fmt"
	"maps"
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
	timeZone string
	source   string
	readonly bool
	catalog  string
	schema   string
	// session holds the properties that this statement sets, over those of
	// the connection.
	session map[string]string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement, as the
// session property query_max_execution_time, as the key timeout of the DSN
// does. The server counts whole milliseconds, so the driver rounds d up to
// the next millisecond. The server stops a statement that runs longer with
// the error EXCEEDED_TIME_LIMIT (measured). Zero sends no timeout, and the
// server uses its own.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. Neither server has a
// setting for one statement. A transaction can be read only, which BeginTx
// sets from sql.TxOptions, so WithReadonly(true) fails the statement with
// dbimp.ErrNotSupported (D109).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a property of the session for one statement by its name,
// such as "query_max_run_time", as the keys session.<name> of the DSN do. The
// servers take no body of keys, so the property of the session is the setting
// that a caller names. The value is written with fmt.Sprint. A property named
// here replaces the one of the connection, and a name that the server does
// not know fails the statement with INVALID_SESSION_PROPERTY (measured).
// WithParameter(name, nil) writes no value, and fails the statement with
// dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.session == nil {
			o.session = map[string]string{}
		}
		if value == nil {
			o.session[name] = ""
			return
		}
		o.session[name] = fmt.Sprint(value)
	}
}

// WithDatabase sets the catalog of one statement, as the first part of the
// path of the DSN does. Neither server has a database, and the catalog is the
// nearest name (D175).
func WithDatabase(name string) Option {
	return func(o *options) { o.catalog = name }
}

// WithSchema sets the schema of one statement, as the second part of the path
// of the DSN does.
func WithSchema(name string) Option {
	return func(o *options) { o.schema = name }
}

// WithTimeZone sets the time zone of one statement, as the key timezone of the
// DSN does.
func WithTimeZone(name string) Option {
	return func(o *options) { o.timeZone = name }
}

// WithSource sets the source of one statement, as the key source of the DSN
// does.
func WithSource(name string) Option {
	return func(o *options) { o.source = name }
}

// resolve returns the options of one statement, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{timeout: cfg.Timeout, timeZone: cfg.TimeZone, source: cfg.Source}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	case o.source == "":
		return fmt.Errorf("applying the option WithSource: the source is empty: %w", dbimp.ErrInvalidValue)
	}
	for name, value := range o.session {
		if err := checkProperty(name); err != nil {
			return fmt.Errorf("applying the option WithParameter: %w", err)
		}
		if value == "" {
			return fmt.Errorf("applying the option WithParameter: the property %q has no value: %w", name, dbimp.ErrInvalidValue)
		}
	}
	return nil
}

// properties returns the properties of the session of a statement: those of
// the connection, then those of the options, then the timeout.
func (o options) properties(conn map[string]string) map[string]string {
	out := maps.Clone(conn)
	if out == nil {
		out = map[string]string{}
	}
	for name, value := range o.session {
		out[name] = escape(value)
	}
	if o.timeout > 0 {
		out[timeoutProperty] = escape(fmt.Sprintf("%dms", (o.timeout+time.Millisecond-1)/time.Millisecond))
	}
	return out
}

// timeoutProperty is the property of the session that limits the time of a
// statement (measured).
const timeoutProperty = "query_max_execution_time"
