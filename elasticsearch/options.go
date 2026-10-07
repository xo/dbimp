package elasticsearch

import (
	"context"
	"database/sql/driver"
	"fmt"
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
	fetchSize int
	timeZone  string
	leniency  bool
	catalog   string
	params    map[string]any
	database  string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement, as
// request_timeout. The server counts milliseconds, so the driver rounds d up
// to the next millisecond. The server stops a statement that runs longer
// with search_timeout_exception, and HTTP 504 on 8.19.22 and HTTP 429 on
// 9.4.6 and 9.5.3 (measured). Zero sends no timeout, and the server uses its
// own.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. SQL in Elasticsearch
// takes no write of any kind (D163), so every statement is read-only, and
// the option changes nothing.
func WithReadonly(bool) Option {
	return func(*options) {}
}

// WithParameter sets any key of the body of POST /_sql by its name, such as
// "filter" or "runtime_mappings". The value is encoded with json/v2. A key
// named here replaces one that the driver sets itself, as in Couchbase, so
// "params" replaces the arguments of the statement. The key applies to the
// first request of a statement and not to the request for each next page,
// which sends only the cursor and the keys of the driver.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase names the database of one statement. Elasticsearch has no
// database to choose, and its catalog is a cluster, which WithCatalog sets
// (D167). A statement with WithDatabase fails with dbimp.ErrNotSupported.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithFetchSize sets the rows of a page, as fetch_size, as the key
// fetch_size of the DSN does.
func WithFetchSize(n int) Option {
	return func(o *options) { o.fetchSize = n }
}

// WithTimeZone sets the time zone of the statement, as time_zone, as the key
// time_zone of the DSN does.
func WithTimeZone(name string) Option {
	return func(o *options) { o.timeZone = name }
}

// WithFieldMultiValueLeniency sets field_multi_value_leniency, as the key of
// the DSN does. When it is true, a field that holds several values gives its
// first value, and the other values are lost with no sign (D167).
func WithFieldMultiValueLeniency(on bool) Option {
	return func(o *options) { o.leniency = on }
}

// WithCatalog sets the cluster of the statement, as catalog, as the key
// catalog of the DSN does. The name of the local cluster works, and the name
// of a cluster that is not configured fails with HTTP 404 (measured).
func WithCatalog(name string) Option {
	return func(o *options) { o.catalog = name }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		fetchSize: cfg.FetchSize,
		timeZone:  cfg.TimeZone,
		leniency:  cfg.FieldMultiValueLeniency,
		catalog:   cfg.Catalog,
	}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.fetchSize < 1:
		return fmt.Errorf("applying the option WithFetchSize: %d: %w", o.fetchSize, dbimp.ErrInvalidValue)
	case o.timeZone == "":
		return fmt.Errorf("applying the option WithTimeZone: the time zone is empty: %w", dbimp.ErrInvalidValue)
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	}
	return nil
}

// request is the body of POST /_sql (measured). The first request of a
// statement holds the query, and the request for each next page holds the
// cursor.
type request struct {
	Query          string `json:"query,omitzero"`
	Cursor         string `json:"cursor,omitzero"`
	FetchSize      int    `json:"fetch_size,omitzero"`
	TimeZone       string `json:"time_zone,omitzero"`
	Leniency       bool   `json:"field_multi_value_leniency,omitzero"`
	Catalog        string `json:"catalog,omitzero"`
	RequestTimeout string `json:"request_timeout,omitzero"`
	Params         []any  `json:"params,omitzero"`
}

// requestTimeout returns the value of request_timeout for the timeout of the
// options, and empty for none.
func (o options) requestTimeout() string {
	if o.timeout <= 0 {
		return ""
	}
	return strconv.FormatInt(int64((o.timeout+time.Millisecond-1)/time.Millisecond), 10) + "ms"
}

// first returns the first request of a statement.
func (o options) first(query string, params []any) request {
	return request{
		Query:          query,
		FetchSize:      o.fetchSize,
		TimeZone:       o.timeZone,
		Leniency:       o.leniency,
		Catalog:        o.catalog,
		RequestTimeout: o.requestTimeout(),
		Params:         params,
	}
}

// next returns the request for the next page of the cursor.
func (o options) next(cursor string) request {
	return request{
		Cursor:         cursor,
		Leniency:       o.leniency,
		RequestTimeout: o.requestTimeout(),
	}
}
