package databend

import (
	"context"
	"database/sql/driver"
	"fmt"
	"math"
	"slices"
	"strconv"
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
	timezone string
}

// WithOptions returns a context that carries opts. Each statement and each
// transaction started with the context applies them after the options of
// the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement, as the
// setting max_execute_time_in_seconds, which counts whole seconds, so the
// driver rounds d up to the next second. The server stops a statement that
// runs longer with the code 1043 (measured).
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly would make the server refuse a write. The server has no
// read-only mode for one statement, so a statement with it fails with
// dbimp.ErrNotSupported (D117).
func WithReadonly(v bool) Option {
	return func(o *options) { o.readonly = v }
}

// WithParameter sets any key of the body of POST /v1/query by its name, such
// as "pagination". The value is encoded with json/v2. A key named here
// replaces one that the driver sets itself, as in Couchbase.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the database of the statement, which the session of the
// request names, as the path of the DSN does. The next statement of the
// connection returns to the database of its session.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithCancel sets how the driver stops the statement on the server,
// CancelKill or CancelNone, as the key cancel of the DSN does (D123).
func WithCancel(how string) Option {
	return func(o *options) { o.cancel = how }
}

// WithTimezone sets the timezone of the statement, as the key timezone of
// the DSN does.
func WithTimezone(name string) Option {
	return func(o *options) { o.timezone = name }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109). The database and the timezone start empty, which leaves those of
// the session of the connection.
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{cancel: cfg.Cancel}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	case !slices.Contains(cancels, o.cancel):
		return fmt.Errorf("applying the option WithCancel: %q: %w", o.cancel, dbimp.ErrInvalidValue)
	}
	return nil
}

// settings returns the settings of the session that the options set for one
// statement.
func (o options) settings() map[string]string {
	s := map[string]string{}
	if o.timeout > 0 {
		s["max_execute_time_in_seconds"] = strconv.FormatInt(int64(math.Ceil(o.timeout.Seconds())), 10)
	}
	if o.timezone != "" {
		s["timezone"] = o.timezone
	}
	return s
}
