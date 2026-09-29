package influxdb

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"slices"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, then from an argument of
// the statement, and a later one wins. InfluxDB has no transactions (D20), so
// no option applies to BeginTx.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout  time.Duration
	readonly bool
	params   map[string]any
	database string
	rp       string
	chunked  string
	describe string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout would set the time that the server gives the statement. No
// release has a timeout for one request: InfluxDB 1 and 2 have one for the
// whole server, and the manual of InfluxDB 3 names none. So a statement with
// it fails with dbimp.ErrNotSupported (D109).
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly would make the server refuse a write. GET /query runs a write
// on InfluxDB 1 with only a warning (measured by hand on 1.13.1), and no
// release has another read-only mode. So a statement with it fails with
// dbimp.ErrNotSupported (D109).
func WithReadonly(v bool) Option {
	return func(o *options) { o.readonly = v }
}

// WithParameter sets any key of the body of the request by its name, such as
// "epoch" for InfluxQL. For SQL, the value is encoded with json/v2. For
// InfluxQL, whose body is a form, a string is the value as it is, and any
// other value is its JSON text. A key named here replaces one that the driver
// sets itself, as in Couchbase. An INSERT sends no body of keys, so it
// ignores WithParameter.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the database of the statement, sent as db, as the path
// of the DSN does. An INSERT with INTO writes to the database of INTO.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithRetentionPolicy sets the retention policy of the statement, sent as
// rp, as the key rp of the DSN does.
func WithRetentionPolicy(name string) Option {
	return func(o *options) { o.rp = name }
}

// WithChunked sets whether InfluxQL asks for its answer in chunks,
// ChunkedPrefer or ChunkedDisable, as the key chunked of the DSN does (D83).
func WithChunked(how string) Option {
	return func(o *options) { o.chunked = how }
}

// WithDescribe sets where SQL reads its columns, DescribeAlways or
// DescribeDisable, as the key describe of the DSN does (D80).
func WithDescribe(how string) Option {
	return func(o *options) { o.describe = how }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		database: cfg.Database,
		rp:       cfg.RetentionPolicy,
		chunked:  cfg.Chunked,
		describe: cfg.Describe,
	}, args)
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
	case !slices.Contains(chunkeds, o.chunked):
		return fmt.Errorf("applying the option WithChunked: %q: %w", o.chunked, dbimp.ErrInvalidValue)
	case !slices.Contains(describes, o.describe):
		return fmt.Errorf("applying the option WithDescribe: %q: %w", o.describe, dbimp.ErrInvalidValue)
	}
	return nil
}

// formValue returns the value of a key of a form for WithParameter: a string
// as it is, and any other value as its JSON text. The caller wraps the error
// with the name of the key.
func formValue(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}
