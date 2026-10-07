package drill

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
	readonly  bool
	schema    string
	autoLimit int
	params    map[string]any
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout asks the server to stop the statement after d. The REST
// interface of Drill has no such setting, so a statement with a d above zero
// fails with dbimp.ErrNotSupported, and a d of zero asks for nothing. The
// context of the statement sets a limit that the driver keeps itself, and
// then cancels the query on the server (D165).
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks the server to refuse a write. The server runs CREATE
// TABLE AS and has no setting that refuses it, so WithReadonly(true) fails
// the statement with dbimp.ErrNotSupported, and WithReadonly(false) asks for
// nothing (D163 and D165).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any key of the body of POST /query.json by its name,
// such as "options". The value is encoded with json/v2. A key named here
// replaces one that the driver sets itself, as in Couchbase. So "options"
// replaces the options that the driver sends, the verbose option of the
// errors too, and an error before any row then has no message (D165).
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase names the schema of one statement, as the key schema of the
// DSN does, such as "dfs.tmp". It is the schema of a table whose name has
// none, and the driver sends it as defaultSchema (D165).
func WithDatabase(name string) Option {
	return WithSchema(name)
}

// WithSchema sets the schema of one statement, as the key schema of the DSN
// does (D165).
func WithSchema(name string) Option {
	return func(o *options) { o.schema = name }
}

// WithAutoLimit sets the most rows that the server sends for one statement,
// as the key autolimit of the DSN does, and sends it as autoLimit (D165).
// Zero sends none. A result as long as the limit does not say whether rows
// were left out.
func WithAutoLimit(n int) Option {
	return func(o *options) { o.autoLimit = n }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{schema: cfg.Schema, autoLimit: cfg.AutoLimit}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.autoLimit < 0:
		return fmt.Errorf("applying the option WithAutoLimit: %d: %w", o.autoLimit, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	return nil
}

// verboseOption is the session option that makes the server write the message
// of an error before any row (measured).
const verboseOption = "drill.exec.http.rest.errors.verbose"

// request is the body of POST /query.json (measured).
type request struct {
	QueryType     string            `json:"queryType"`
	Query         string            `json:"query"`
	DefaultSchema string            `json:"defaultSchema,omitzero"`
	AutoLimit     int               `json:"autoLimit,omitzero"`
	Options       map[string]string `json:"options"`
}

// request returns the body for the statement query.
func (o options) request(query string) request {
	return request{
		QueryType:     "SQL",
		Query:         query,
		DefaultSchema: o.schema,
		AutoLimit:     o.autoLimit,
		Options:       map[string]string{verboseOption: "true"},
	}
}
