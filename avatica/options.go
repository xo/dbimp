package avatica

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

// defaultFrameSize is the count of rows in each frame that the driver asks
// for (D157).
const defaultFrameSize = 1000

// options are the options of one request.
type options struct {
	timeout   time.Duration
	readonly  bool
	params    map[string]any
	database  string
	frameSize int
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout would bound the time of the statement on the server. Avatica
// has none, and the server runs a query to its end when its client leaves
// (measured), so a timeout above zero fails with dbimp.ErrNotSupported. The
// context of the statement still stops its request and its read (D159).
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly would make the server refuse a write of one statement.
// readOnly is a property of the connection of Avatica, which one statement
// cannot change, so WithReadonly(true) fails with dbimp.ErrNotSupported.
// BeginTx with ReadOnly makes a transaction read-only (D159).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets any key of the request that runs the statement by its
// name, such as "maxRowCount". The value is encoded with json/v2. A key
// named here replaces one that the driver sets itself.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase would choose the schema of one statement. The schema is a
// property of the connection of Avatica, which one statement cannot change,
// so a statement with it fails with dbimp.ErrNotSupported. The SQL can name
// the schema of each table.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithFrameSize sets the count of rows of each frame that the driver asks
// for, 1000 by default (D157).
func WithFrameSize(rows int) Option {
	return func(o *options) { o.frameSize = rows }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{frameSize: defaultFrameSize}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	case o.database != "":
		return dbimp.Unsupported("WithDatabase")
	case o.frameSize < 1:
		return fmt.Errorf("applying the option WithFrameSize: %d: %w", o.frameSize, dbimp.ErrInvalidValue)
	}
	return nil
}
