package rqlite

import (
	"context"
	"database/sql/driver"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
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
	params    map[string]string
	database  string
	level     string
	freshness time.Duration
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement, as the key
// db_timeout. The server stops a read that runs longer with the error
// `query timeout`, and a write with `execute timeout`, and keeps nothing of
// the write (measured). When the context has a deadline too, the shorter of
// the two goes (D145). The driver also ends the request at that time,
// because 9.4.5 ignores db_timeout for a read on /db/request, and a read
// stops when its client leaves (D146). Zero sets no timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly sends the statement to /db/query, which runs it on a
// read-only connection, so the server refuses a write with the error
// `attempt to change database via query operation` (measured and D142). A
// user with only the permission query can run a statement with it.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any key of the URL of the request by its name, such as
// "timings" or "qualify_columns", with the text of value. The server reads a
// flag by its presence, so nil and "" send the key alone. A key named here
// replaces one that the driver sets itself, such as "db_timeout" or "level".
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]string{}
		}
		if value == nil {
			o.params[name] = ""
			return
		}
		o.params[name] = fmt.Sprint(value)
	}
}

// WithDatabase would choose a database. rqlite has one database (D141), so a
// statement with it fails with dbimp.ErrNotSupported.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithLevel sets the read consistency of a query, as the key level of the
// DSN does (D141).
func WithLevel(level string) Option {
	return func(o *options) { o.level = level }
}

// WithFreshness sets how old the data of a read with LevelNone can be, as the
// key freshness of the DSN does (D141).
func WithFreshness(d time.Duration) Option {
	return func(o *options) { o.freshness = d }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{level: cfg.Level, freshness: cfg.Freshness}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.freshness < 0:
		return fmt.Errorf("applying the option WithFreshness: %v: %w", o.freshness, dbimp.ErrInvalidValue)
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	case !slices.Contains(levels, o.level):
		return fmt.Errorf("applying the option WithLevel: %q: %w", o.level, dbimp.ErrInvalidValue)
	}
	return nil
}

// keys returns the keys of the URL of one request: blob_array, which every
// request sends so that a BLOB differs from text (D142), the level and the
// freshness, db_timeout from the option and the deadline of ctx (D145), and
// the keys of WithParameter, which replace the others.
func (o options) keys(ctx context.Context) url.Values {
	keys := url.Values{"blob_array": {""}}
	if o.level != "" {
		keys.Set("level", o.level)
	}
	if o.freshness > 0 {
		keys.Set("freshness", o.freshness.String())
	}
	if d := o.deadline(ctx); d > 0 {
		keys.Set("db_timeout", strconv.FormatInt(int64((d+time.Millisecond-1)/time.Millisecond), 10)+"ms")
	}
	for k, v := range o.params {
		keys.Set(k, v)
	}
	return keys
}

// deadline returns the time that the server gives the statement: the
// shorter of the timeout of the option and the time left before the
// deadline of ctx, or zero for none (D145).
func (o options) deadline(ctx context.Context) time.Duration {
	d := o.timeout
	if dl, ok := ctx.Deadline(); ok {
		if left := time.Until(dl); d == 0 || left < d {
			d = max(left, time.Millisecond)
		}
	}
	return d
}

// sortedKeys returns the keys of keys in order, so that a request is the
// same each time.
func sortedKeys(keys url.Values) []string {
	return slices.Sorted(maps.Keys(keys))
}
