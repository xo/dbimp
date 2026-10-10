package bigquery

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the DSN,
// then from the context through WithOptions, then from an argument of the
// statement, and a later one wins.
type Option = dbimp.Option[options]

// options are the options of one statement.
type options struct {
	timeout    time.Duration
	dataset    string
	location   string
	maxResults int
	readonly   bool
	// params holds the members of the request that WithParameter set, by name.
	params map[string]any
}

// WithOptions returns a context that carries opts. Each statement started with
// the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the service gives the job, as jobTimeoutMs, as
// the key timeout of the DSN does. The service tries to stop a job that runs
// longer, and it can fail to. The driver rounds d up to the next millisecond.
// Zero sends none, and the service uses its own limit.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. BigQuery has no such
// setting for one statement, so WithReadonly(true) fails the statement with
// dbimp.ErrNotSupported (D109).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a member of the request of one statement by its name, such
// as "labels", "maximumBytesBilled", "dryRun", "useQueryCache", "requestId" or
// "createSession". The value is written as JSON. It replaces the member of the
// same name that the driver writes, as dbimp.MarshalParams does (D109).
//
// A member that changes how the driver binds or reads a value is refused with
// dbimp.ErrNotSupported: query, queryParameters, parameterMode, useLegacySql,
// formatOptions and queryResultsFormat. The members defaultDataset, location,
// maxResults and jobTimeoutMs have an option of their own, and WithParameter
// replaces them too. A value of nil fails the statement with
// dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the default dataset of one statement, as the path of the
// DSN does. BigQuery calls the database a dataset.
func WithDatabase(name string) Option {
	return func(o *options) { o.dataset = name }
}

// WithLocation sets the location of the job of one statement, as the key
// location of the DSN does.
func WithLocation(name string) Option {
	return func(o *options) { o.location = name }
}

// WithMaxResults sets the most rows in a page of the result of one statement,
// as the key max_results of the DSN does. Zero leaves the size to the service.
func WithMaxResults(n int) Option {
	return func(o *options) { o.maxResults = n }
}

// resolve returns the options of one statement, those of the DSN, then those of
// the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		timeout:    cfg.Timeout,
		dataset:    cfg.Dataset,
		location:   cfg.Location,
		maxResults: cfg.MaxResults,
	}, args)
}

// refused are the members of the request that WithParameter refuses, because
// the driver reads and binds by them.
var refused = map[string]bool{
	"query":              true,
	"queryParameters":    true,
	"parameterMode":      true,
	"useLegacySql":       true,
	"formatOptions":      true,
	"queryResultsFormat": true,
}

// check returns an error for an option that the service cannot honor, or whose
// value that the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0 || o.timeout > maxTimeout:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.maxResults < 0 || o.maxResults > maxMaxResults:
		return fmt.Errorf("applying the option WithMaxResults: %d: %w", o.maxResults, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	for name, value := range o.params {
		switch {
		case name == "" || strings.ContainsFunc(name, func(r rune) bool { return !isNameRune(r) }):
			return fmt.Errorf("applying the option WithParameter: %q is not the name of a member of the request: %w", name, dbimp.ErrInvalidValue)
		case value == nil:
			return fmt.Errorf("applying the option WithParameter: the member %q has no value: %w", name, dbimp.ErrInvalidValue)
		case refused[name]:
			return fmt.Errorf("applying the option WithParameter: the member %s changes how the driver binds or reads a value: %w", name, dbimp.ErrNotSupported)
		}
	}
	return nil
}

// isNameRune reports whether r can be in the name of a member of the request.
func isNameRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// timeoutMs returns the timeout of the job in whole milliseconds, rounded up.
func (o options) timeoutMs() int64 {
	return int64((o.timeout + time.Millisecond - 1) / time.Millisecond)
}
