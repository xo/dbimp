package druid

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

// options are the options of one request.
type options struct {
	timeout  time.Duration
	timeZone string
	params   map[string]any
	database string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the query, as timeout in
// the context of the query, as the key timeout of the DSN does. The server
// counts whole milliseconds, so the driver rounds d up to the next
// millisecond. The server stops a query that runs longer with HTTP 504 and
// the category TIMEOUT (measured). Zero sends no timeout, and the server
// uses its own.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. The SQL API of Druid
// takes no write of any kind (D163), so every statement is read-only, and
// the option changes nothing.
func WithReadonly(bool) Option {
	return func(*options) {}
}

// WithParameter sets any key of the body of POST /druid/v2/sql by its name,
// such as "context". The value is encoded with json/v2. A key named here
// replaces one that the driver sets itself, as in Couchbase. So "context"
// replaces the whole context that the driver sends, the id of the query and
// the time zone too, and the driver can then no longer cancel the query by
// its id.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase names the database of one statement. Druid has no database
// to choose (D164), so a statement with it fails with
// dbimp.ErrNotSupported.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithTimeZone sets the time zone of the query, as sqlTimeZone, as the key
// timezone of the DSN does (D164).
func WithTimeZone(name string) Option {
	return func(o *options) { o.timeZone = name }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{timeout: cfg.Timeout, timeZone: cfg.TimeZone}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeZone == "":
		return fmt.Errorf("applying the option WithTimeZone: the time zone is empty: %w", dbimp.ErrInvalidValue)
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	}
	return nil
}

// queryContext is the context of one query (measured).
type queryContext struct {
	// SQLQueryID is the id that the driver gives the query, so that it can
	// cancel the query (D164).
	SQLQueryID string `json:"sqlQueryId"`
	// SQLTimeZone is the time zone of the query.
	SQLTimeZone string `json:"sqlTimeZone"`
	// Timeout is the time that the server gives the query, in milliseconds.
	Timeout int64 `json:"timeout,omitzero"`
}

// context returns the context of the query that the driver named id.
func (o options) context(id string) queryContext {
	qc := queryContext{SQLQueryID: id, SQLTimeZone: o.timeZone}
	if o.timeout > 0 {
		qc.Timeout = int64((o.timeout + time.Millisecond - 1) / time.Millisecond)
	}
	return qc
}
