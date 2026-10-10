package cosmos

import (
	"context"
	"database/sql/driver"
	"encoding/json/v2"
	"fmt"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, and then from an argument
// of the statement, and a later one wins. The keys tls and insecure of the DSN
// belong to the connection, so they have no option.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout   time.Duration
	readonly  bool
	params    map[string]any
	database  string
	container string
	pageSize  int
	// key is the partition key of the statement, as the JSON text of the
	// header X-Ms-Documentdb-Partitionkey, and hasKey is true when the
	// statement has one.
	key    string
	hasKey bool
	keyErr error
}

// defaults returns the options that the DSN of cfg sets.
func (cfg *Config) defaults() options {
	o := options{database: cfg.Database, container: cfg.Container, pageSize: cfg.PageSize}
	if cfg.PartitionKey != nil {
		o.key, o.hasKey = partitionKey(*cfg.PartitionKey)
	}
	return o
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement. The REST API
// of Cosmos DB has no such setting (not measured, source: Microsoft), so a
// positive value fails the statement with dbimp.ErrNotSupported. The context
// of the statement is the way to bound it. Zero asks for nothing.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. The driver reads only
// (D190), so WithReadonly(true) holds for every statement and changes
// nothing.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any key of the body of the query by its name, such as
// "query" or "parameters". The value is encoded with json/v2. A key named
// here replaces one that the driver sets itself, as in Couchbase.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase names the database of one statement, in place of the database
// of the DSN.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithContainer names the container of one statement, in place of the
// container of the DSN. The SQL of Cosmos DB has no container name in FROM,
// because the path of the request holds it (recorded: "a query that selects
// every document").
func WithContainer(name string) Option {
	return func(o *options) { o.container = name }
}

// WithPartitionKey limits the statement to the documents whose partition key
// is value. The value is encoded with json/v2, so a string, a number, a bool
// and nil, which is null, are the values of a partition key. The server then
// serves an aggregate, TOP, ORDER BY, OFFSET LIMIT and DISTINCT that it
// refuses across partitions (recorded: "a query for one partition key"). The
// DSN key partitionkey sets a string value for every statement.
func WithPartitionKey(value any) Option {
	return func(o *options) {
		buf, err := json.Marshal([]any{value})
		o.key, o.hasKey, o.keyErr = string(buf), err == nil, err
	}
}

// WithPageSize sets the number of documents in a page of the result, which
// the server takes in the header X-Ms-Max-Item-Count. Zero leaves it to the
// server, and -1 lets the server choose the size of each page (recorded: "a
// page of minus one"). A value below -1 fails the statement with
// dbimp.ErrInvalidValue.
func WithPageSize(n int) Option {
	return func(o *options) { o.pageSize = n }
}

// partitionKey returns the header value of a partition key that is the text
// s.
func partitionKey(s string) (string, bool) {
	buf, err := json.Marshal([]string{s})
	if err != nil {
		return "", false
	}
	return string(buf), true
}

// resolve returns the options of one request, those of the DSN, then those of
// the context, and then the Option arguments of args, and the other arguments
// (D109).
func (c *Connector) resolve(ctx context.Context, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, c.cfg.defaults(), args)
}

// check returns an error for an option that the server cannot honor, or
// whose value is not valid.
func (o options) check() error {
	if err := o.checkCommon(); err != nil {
		return err
	}
	if o.database == "" || o.container == "" {
		return fmt.Errorf("sending the statement: it has no database or no container, so name them in the path of the DSN, or with WithDatabase and WithContainer: %w", dbimp.ErrInvalidValue)
	}
	if err := checkName(o.database); err != nil {
		return fmt.Errorf("applying the database: %w", err)
	}
	if err := checkName(o.container); err != nil {
		return fmt.Errorf("applying the container: %w", err)
	}
	return nil
}

// checkCommon returns an error for an option that the server cannot honor, or
// whose value is not valid, and that every statement shares.
func (o options) checkCommon() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.pageSize < -1:
		return fmt.Errorf("applying the option WithPageSize: %d is less than -1: %w", o.pageSize, dbimp.ErrInvalidValue)
	case o.keyErr != nil:
		return fmt.Errorf("applying the option WithPartitionKey: %w: %w", o.keyErr, dbimp.ErrInvalidValue)
	}
	return nil
}
