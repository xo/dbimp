package couchbase

import (
	"context"
	"maps"
	"time"
)

// Option sets an option of one statement or one transaction (D40). An option
// comes from the DSN, then from the context through WithOptions, then from
// an argument of the statement, and a later one wins. BeginTx takes no
// argument, so an option of a transaction comes from the DSN or through
// WithOptions only.
type Option func(*options)

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

// optionsKey is the key of the options in a context.
type optionsKey struct{}

// WithOptions returns a context that carries opts. Each statement and each
// transaction started with the context applies them after the options of
// the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	prev, _ := ctx.Value(optionsKey{}).([]Option)
	return context.WithValue(ctx, optionsKey{}, append(append([]Option(nil), prev...), opts...))
}

// resolve returns the options of one request: those of the DSN, then those of
// the context, then extra.
func resolve(ctx context.Context, cfg *Config, extra []Option) options {
	o := options{
		queryContext:    cfg.QueryContext,
		scanConsistency: cfg.ScanConsistency,
		timeout:         cfg.Timeout,
		durability:      cfg.Durability,
		txTimeout:       cfg.TxTimeout,
	}
	fromCtx, _ := ctx.Value(optionsKey{}).([]Option)
	for _, opt := range fromCtx {
		opt(&o)
	}
	for _, opt := range extra {
		opt(&o)
	}
	return o
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
