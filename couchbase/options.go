package couchbase

import (
	"context"
	"database/sql/driver"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement or one transaction (D40 and D109).
// An option comes from the DSN, then from the context through WithOptions,
// then from an argument of the statement, and a later one wins. BeginTx
// takes no argument, so an option of a transaction comes from the DSN or
// through WithOptions only.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	queryContext    string
	scanConsistency string
	timeout         time.Duration
	durability      string
	txTimeout       time.Duration
	readonly        bool
	params          map[string]any
}

// WithQueryContext sets the bucket and the scope of a collection that a
// statement names alone, such as "default:dbmeta._default".
func WithQueryContext(v string) Option {
	return func(o *options) { o.queryContext = v }
}

// WithDatabase sets the bucket and the scope of a collection that a
// statement names alone, as WithQueryContext does. It is the name that every
// driver gives the option (D109). The query service has no database, and
// query_context is the setting nearest to one.
func WithDatabase(name string) Option {
	return WithQueryContext(name)
}

// WithScanConsistency sets the scan consistency, "not_bounded" or
// "request_plus". A read that must see an earlier write sends
// "request_plus", because Couchbase updates an index after a write.
func WithScanConsistency(v string) Option {
	return func(o *options) { o.scanConsistency = v }
}

// WithTimeout sets the timeout that the server enforces for the statement.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly makes the server refuse a statement that writes.
func WithReadonly(v bool) Option {
	return func(o *options) { o.readonly = v }
}

// WithDurability sets the durability of a transaction, such as "none" or
// "majority" (D43). It applies to BeginTx, through WithOptions. A statement
// that runs as a transaction of its own takes
// WithParameter("durability_level", v).
func WithDurability(v string) Option {
	return func(o *options) { o.durability = v }
}

// WithTransactionTimeout sets how long a transaction can last before the
// server ends it. It applies to BeginTx, through WithOptions.
func WithTransactionTimeout(d time.Duration) Option {
	return func(o *options) { o.txTimeout = d }
}

// WithParameter sets any parameter of the request by its name, such as
// "profile" or "client_context_id", as the SDKs of Couchbase do with their
// raw options. The value is encoded with json/v2. A parameter named here
// replaces one that the driver sets itself.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithOptions returns a context that carries opts. Each statement and each
// transaction started with the context applies them after the options of
// the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		queryContext:    cfg.QueryContext,
		scanConsistency: cfg.ScanConsistency,
		timeout:         cfg.Timeout,
		durability:      cfg.Durability,
		txTimeout:       cfg.TxTimeout,
	}, args)
}

// body adds the options that apply to a statement to a request body.
func (o options) body(b map[string]any) {
	if o.queryContext != "" {
		b["query_context"] = o.queryContext
	}
	if o.scanConsistency != "" {
		b["scan_consistency"] = o.scanConsistency
	}
	if o.timeout != 0 {
		b["timeout"] = o.timeout.String()
	}
	if o.readonly {
		b["readonly"] = true
	}
	maps.Copy(b, o.params)
}

// check returns an error for an option whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.scanConsistency != "" && !slices.Contains(scanConsistencies, o.scanConsistency):
		return fmt.Errorf("applying the option WithScanConsistency: %q: %w", o.scanConsistency, dbimp.ErrInvalidValue)
	case o.durability != "" && !slices.Contains(durabilities, o.durability):
		return fmt.Errorf("applying the option WithDurability: %q: %w", o.durability, dbimp.ErrInvalidValue)
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.txTimeout < 0:
		return fmt.Errorf("applying the option WithTransactionTimeout: %v: %w", o.txTimeout, dbimp.ErrInvalidValue)
	}
	return nil
}
