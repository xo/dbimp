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
// from an argument of the statement, and a later one wins. The options
// start empty. The keys of the DSN that can change for one statement are the
// database and the database role (D198).
type Option = dbimp.Option[options]

// options are the options of one statement.
type options struct {
	timeout  time.Duration
	database string
	// role is the database role of the session. Empty keeps the role of the DSN
	// (D198).
	role     string
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

// WithDatabaseRole sets the database role of one statement, as the key
// database_role of the DSN does. The driver keeps one session for each pair of a
// database and a role, because the server fixes the role when it makes the
// session. An empty name keeps the role of the DSN. A statement of a transaction
// runs as the role of the transaction, so WithDatabaseRole with another role
// fails the statement with dbimp.ErrNotSupported (D109 and D198).
func WithDatabaseRole(name string) Option {
	return func(o *options) { o.role = name }
}

// with returns tg with the database and the role of o, where o sets them.
func (tg target) with(o options) target {
	if o.database != "" {
		tg.database = o.database
	}
	if o.role != "" {
		tg.role = o.role
	}
	return tg
}

// maxRoleLen is the longest name of a database role.
const maxRoleLen = 128

// validRole returns an error when name is not a database role that Spanner
// accepts: 1 to 128 letters, digits and underscores. The empty name is the
// absence of a role, and it is valid.
func validRole(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > maxRoleLen {
		return fmt.Errorf("the database role %q is longer than %d characters: %w", name, maxRoleLen, dbimp.ErrInvalidValue)
	}
	for i := range len(name) {
		c := name[i]
		if c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return fmt.Errorf("the database role %q holds a character that is not a letter, a digit or an underscore: %w", name, dbimp.ErrInvalidValue)
		}
	}
	return nil
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
	if err := validRole(o.role); err != nil {
		return fmt.Errorf("applying the option WithDatabaseRole: %w", err)
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
