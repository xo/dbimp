package maxcompute

import (
	"context"
	"database/sql/driver"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, then from an argument of
// the statement, and a later one wins.
type Option = dbimp.Option[options]

// maxTimeout is the most time that an option names for the timeout of a
// statement. It keeps the sum of a time and a deadline inside the range of a
// time.Duration.
const maxTimeout = 24 * 365 * time.Hour

// options are the options of one statement.
type options struct {
	timeout  time.Duration
	project  string
	readonly bool
	// settings are the settings of the server for the statement: those of the
	// DSN, and then those that WithParameter set.
	settings map[string]string
	// invalid holds the error of a setting whose value cannot be text.
	invalid error
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the longest time that the driver waits for the statement,
// from the request to the end of the result. When it ends, the driver cancels
// the statement on the server, and the error is context.DeadlineExceeded.
// Zero waits as long as the context allows.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. The server has no such
// setting for one statement, so WithReadonly(true) fails the statement with
// dbimp.ErrNotSupported (D109).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a setting of the server for one statement, by its name,
// such as "odps.sql.timezone". It replaces the setting of the DSN with the same
// name (D109 and D220 item 3). The value is a string, a bool or a number, and
// the driver sends its text. A value of nil fails the statement with
// dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		o.settings = maps.Clone(o.settings)
		if o.settings == nil {
			o.settings = map[string]string{}
		}
		var text string
		switch v := value.(type) {
		case string:
			text = v
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			text = fmt.Sprint(v)
		default:
			o.invalid = fmt.Errorf("applying the option WithParameter: the setting %q has a value of %T, and the driver sends a string, a bool or a number: %w", name, value, dbimp.ErrInvalidValue)
			return
		}
		o.settings[name] = text
	}
}

// WithDatabase sets the project of one statement, because a project is the
// database of MaxCompute. It is the same as WithProject.
func WithDatabase(name string) Option {
	return func(o *options) { o.project = name }
}

// WithProject sets the project of one statement, in place of the project of the
// DSN.
func WithProject(name string) Option {
	return func(o *options) { o.project = name }
}

// resolve returns the options of one statement, those of the DSN, then those of
// the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		project:  cfg.Project,
		settings: cfg.Settings,
	}, args)
}

// check returns an error for an option that the server cannot honor, or whose
// value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.invalid != nil:
		return o.invalid
	case o.timeout < 0 || o.timeout > maxTimeout:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	case o.project == "" || strings.ContainsFunc(o.project, func(r rune) bool { return r < ' ' || r == '/' || r == '?' || r == '#' }):
		return fmt.Errorf("applying the option WithDatabase: %q is not the name of a project: %w", o.project, dbimp.ErrInvalidValue)
	}
	for name, value := range o.settings {
		if name == "" || value == "" {
			return fmt.Errorf("applying the option WithParameter: the setting %q has no name or no value: %w", name, dbimp.ErrInvalidValue)
		}
	}
	return nil
}
