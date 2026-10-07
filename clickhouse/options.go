package clickhouse

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, then from an argument of
// the statement, and a later one wins.
type Option = dbimp.Option[options]

// options are the options of one statement.
type options struct {
	timeout  time.Duration
	readonly bool
	database string
	// settings holds the settings that WithParameter set, by name.
	settings map[string]string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement, as the
// setting max_execution_time, which counts seconds with a fraction. The server
// stops a statement that runs longer with the code 159, which is HTTP 408 on
// 26.9, and an error after the rows on 25.3 and 25.8 (measured). Zero sends no
// timeout, and the server uses its own. The DSN has no key for it, because a
// user who cannot change the setting gets an error (D176).
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing, with the setting readonly
// set to 1 (D109). The server then refuses a write with the code 164, and a
// change of a setting in the text of the statement, such as a SETTINGS clause. It
// still accepts the settings in the query string, which are the settings of the
// driver (measured).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a setting of the server for one statement by its name,
// such as "max_threads". The value is written with fmt.Sprint, and a bool is 1
// or 0. A name that the driver sets itself, such as "query_id", takes this
// value in its place. So a caller who replaces "default_format" breaks the
// reading of the answer, and a caller who replaces "query_id" names the query
// that the driver cancels. A name that the server does not know fails the
// statement with the code 115 (measured). WithParameter(name, nil) writes no
// value, and fails the statement with dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.settings == nil {
			o.settings = map[string]string{}
		}
		switch v := value.(type) {
		case nil:
			o.settings[name] = ""
		case bool:
			if v {
				o.settings[name] = "1"
			} else {
				o.settings[name] = "0"
			}
		default:
			o.settings[name] = fmt.Sprint(v)
		}
	}
}

// WithDatabase sets the database of one statement, as the path of the DSN
// does, with the query key database.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// resolve returns the options of one statement, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{database: cfg.Database}, args)
}

// check returns an error for an option whose value the DSN would refuse.
func (o options) check() error {
	if o.timeout < 0 {
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	}
	for name, value := range o.settings {
		if name == "" || strings.ContainsAny(name, "&=#") {
			return fmt.Errorf("applying the option WithParameter: %q is not the name of a setting: %w", name, dbimp.ErrInvalidValue)
		}
		if value == "" {
			return fmt.Errorf("applying the option WithParameter: the setting %q has no value: %w", name, dbimp.ErrInvalidValue)
		}
	}
	return nil
}
