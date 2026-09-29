package pinot

import (
	"context"
	"database/sql/driver"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, then from an argument of
// the statement, and a later one wins.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout  time.Duration
	params   map[string]any
	database string
	cancel   string
	engine   string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the query, as the query
// option timeoutMs, which counts whole milliseconds, so the driver rounds d
// up to the next millisecond. The server stops a query that runs longer with
// the code 250 (measured). Zero leaves the timeout of the server, which is
// 10 seconds by default.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. The Broker takes no
// write of any kind (D128), so every statement is read-only, and the option
// changes nothing.
func WithReadonly(bool) Option {
	return func(*options) {}
}

// WithParameter sets any key of the body of POST /query/sql by its name, such
// as "trace". The value is encoded with json/v2. A key named here replaces
// one that the driver sets itself, as in Couchbase. So "queryOptions"
// replaces every query option that the driver sends, enableNullHandling
// and the id of the query too.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase would choose a database. Pinot has tables and no databases
// (D129), so a statement with it fails with dbimp.ErrNotSupported.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithCancel sets how the driver stops the query on the server when its
// context ends, CancelKill or CancelNone, as the key cancel of the DSN does
// (D133).
func WithCancel(how string) Option {
	return func(o *options) { o.cancel = how }
}

// WithEngine sets the engine that runs the query, EngineMulti or
// EngineSingle, as the key engine of the DSN does (D131).
func WithEngine(name string) Option {
	return func(o *options) { o.engine = name }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{cancel: cfg.Cancel, engine: cfg.Engine}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	case !slices.Contains(cancels, o.cancel):
		return fmt.Errorf("applying the option WithCancel: %q: %w", o.cancel, dbimp.ErrInvalidValue)
	case !slices.Contains(engines, o.engine):
		return fmt.Errorf("applying the option WithEngine: %q: %w", o.engine, dbimp.ErrInvalidValue)
	}
	return nil
}

// queryOptions returns the query options of one query: the engine, null
// handling (D130), the id that the driver gave the query (D133), and the
// timeout, joined with ";" as the Broker reads them.
func (o options) queryOptions(id string) string {
	opts := []string{
		"useMultistageEngine=" + strconv.FormatBool(o.engine == EngineMulti),
		"enableNullHandling=true",
		"clientQueryId=" + id,
	}
	if o.timeout > 0 {
		ms := (o.timeout + time.Millisecond - 1) / time.Millisecond
		opts = append(opts, "timeoutMs="+strconv.FormatInt(int64(ms), 10))
	}
	return strings.Join(opts, ";")
}
