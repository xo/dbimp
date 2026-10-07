package opensearch

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
	timeout   time.Duration
	fetchSize int
	params    map[string]any
	database  string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout asks the server to stop the statement after d. The SQL plugin
// has no such setting: wait_for_completion_timeout does not work, and the
// plugin has no way to cancel a statement (measured). A statement with a
// positive timeout fails with dbimp.ErrNotSupported, so that a caller never
// believes that it holds. The context of the statement bounds the request
// instead.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. SQL in OpenSearch takes
// no write of any kind (D163), so every statement is read-only, and the
// option changes nothing.
func WithReadonly(bool) Option {
	return func(*options) {}
}

// WithParameter sets any key of the body of POST /_plugins/_sql by its name,
// such as "filter", which holds a query of the Query DSL, or "fetch_size". The
// value is encoded with json/v2. A key named here replaces one that the driver
// sets itself, as in Couchbase, so "fetch_size" replaces the page size of the
// driver, and 0 turns paging off. The key applies to the first request of a
// statement and not to the request for each next page, which sends only the
// cursor. A statement with "filter" runs in the legacy engine of the server,
// whose types, errors and pages differ (measured).
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase names the database of one statement. OpenSearch has no
// database to choose, so a statement with WithDatabase fails with
// dbimp.ErrNotSupported (D168).
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithFetchSize sets the rows of a page of a plain SELECT, as fetch_size, as
// the key fetch_size of the DSN does.
func WithFetchSize(n int) Option {
	return func(o *options) { o.fetchSize = n }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{fetchSize: cfg.FetchSize}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.fetchSize < 1:
		return fmt.Errorf("applying the option WithFetchSize: %d: %w", o.fetchSize, dbimp.ErrInvalidValue)
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	}
	return nil
}

// request is the body of POST /_plugins/_sql (measured). The first request of
// a statement holds the query and, for a plain SELECT, the page size. The
// request for each next page holds the cursor only.
type request struct {
	Query     string `json:"query,omitzero"`
	Cursor    string `json:"cursor,omitzero"`
	FetchSize int    `json:"fetch_size,omitzero"`
}
