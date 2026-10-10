package spanner

import (
	"context"
	"database/sql/driver"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement or of one transaction (D109). An
// option comes from the DSN, then from the context through WithOptions, then
// from an argument of the statement, and a later one wins. The DSN of this
// driver has no key that can change for one statement, so the options start
// empty.
type Option = dbimp.Option[options]

// options are the options of one statement.
type options struct {
	timeout  time.Duration
	database string
	readonly bool
	// params holds the members of the body that WithParameter set, by name.
	params map[string]any
}

// WithOptions returns a context that carries opts. Each statement and each
// transaction started with the context applies them.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout asks for a time that the server gives the statement. Spanner
// has no such setting for a request of the REST API, because the deadline
// belongs to the client (docs/SPANNER.md, "Cancellation and timeouts"), so a
// positive value fails the statement with dbimp.ErrNotSupported. The context
// of the statement is the way to bound it. Zero asks for nothing.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. The driver runs the
// statement in a read-only transaction that has no commit, so the server
// refuses a DML statement with HTTP 400 (recorded: "a DML statement in a read
// only transaction"). A DDL statement fails with dbimp.ErrNotSupported. In
// BeginTx, WithReadonly(true) makes the transaction read-only, as
// sql.TxOptions does.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a member of the body of the request by its name, for a
// setting that the driver has no option for. It replaces a member that the
// driver sets itself. For a statement it sets a member of executeStreamingSql,
// such as queryMode, queryOptions, requestOptions, directedReadOptions or
// transaction. For a transaction it sets a member of beginTransaction, such as
// options, and the members maxCommitDelay, returnCommitStats and requestOptions
// of the commit. A value of nil fails the statement with dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the database of one statement, as the last part of the
// path of the DSN does. The name is the id of a database of the project and
// the instance of the DSN, such as "other". The driver keeps one session for
// each database. A statement of a transaction runs in the database of the
// transaction, so WithDatabase with another database fails the statement
// with dbimp.ErrNotSupported (D109).
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// resolve returns the options of one statement, those of the context, and
// then the Option arguments of args, and the other arguments (D109).
func resolve(ctx context.Context, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value is not valid.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case strings.Contains(o.database, "/"):
		return fmt.Errorf("applying the option WithDatabase: %q is not the id of a database: %w", o.database, dbimp.ErrInvalidValue)
	}
	for name, value := range o.params {
		switch {
		case name == "":
			return fmt.Errorf("applying the option WithParameter: the name is empty: %w", dbimp.ErrInvalidValue)
		case value == nil:
			return fmt.Errorf("applying the option WithParameter: the parameter %q has no value: %w", name, dbimp.ErrInvalidValue)
		}
	}
	return nil
}

// commitKeys are the members of the commit that WithParameter can set for a
// transaction. The other members go in the body of beginTransaction.
var commitKeys = map[string]bool{
	"maxCommitDelay":    true,
	"returnCommitStats": true,
	"requestOptions":    true,
}

// split returns the parameters for beginTransaction and the parameters for the
// commit.
func (o options) split() (map[string]any, map[string]any) {
	begin, commit := map[string]any{}, map[string]any{}
	for name, value := range maps.All(o.params) {
		if commitKeys[name] {
			commit[name] = value
			continue
		}
		begin[name] = value
	}
	return begin, commit
}
