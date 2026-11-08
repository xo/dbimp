package hive

import (
	"context"
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/xo/dbimp"
)

// defaultFetchSize is the number of rows that one FetchResults asks for (D235).
const defaultFetchSize = 1000

// Option sets an option of one statement (D109). An option comes from the DSN,
// then from the context through WithOptions, then from an argument of the
// statement, and a later one wins.
type Option = dbimp.Option[options]

// options are the options of one statement.
type options struct {
	timeout   time.Duration
	readonly  bool
	database  string
	params    map[string]string
	fetchSize int
}

// WithOptions returns a context that carries opts. Each statement started with
// the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the driver gives the statement, from its start
// to the end of its rows. When the time ends, the driver sends CancelOperation
// and the statement fails with context.DeadlineExceeded. When the context has a
// deadline too, the shorter of the two goes. Zero sets no timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly would send the statement on a read-only connection. Neither
// flavor has such a setting, so a statement with WithReadonly(true) fails with
// dbimp.ErrNotSupported.
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a key of confOverlay of ExecuteStatement by its name, such
// as "hive.exec.parallel" on Hive, with the text of value. The server applies it
// to this statement only.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]string{}
		}
		o.params[name] = fmt.Sprint(value)
	}
}

// WithDatabase would choose a database for one statement. The database is a state
// of the session, which another statement of the connection would see, so a
// statement with it fails with dbimp.ErrNotSupported. The DSN names the database
// of the session.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithFetchSize sets the number of rows that each FetchResults asks for. The
// default is 1000.
func WithFetchSize(n int) Option {
	return func(o *options) { o.fetchSize = n }
}

// resolve returns the options of one statement, those of the context, then the
// Option arguments of args, and the other arguments (D109).
func resolve(ctx context.Context, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{fetchSize: defaultFetchSize}, args)
}

// check returns an error for an option that the server cannot honor, or whose
// value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	case o.fetchSize < 1:
		return fmt.Errorf("applying the option WithFetchSize: %d: %w", o.fetchSize, dbimp.ErrInvalidValue)
	}
	return nil
}
