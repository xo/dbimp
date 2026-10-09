package snowflake

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
	timeout   time.Duration
	timeZone  string
	database  string
	schema    string
	role      string
	warehouse string
	readonly  bool
	// params holds the parameters that WithParameter set, by name.
	params map[string]any
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout sets the time that the server gives the statement, as timeout
// in the body, as the key timeout of the DSN does. The server counts whole
// seconds, so the driver rounds d up to the next second. The server stops a
// statement that runs longer with HTTP 408 and the code 000630 (measured).
// Zero sends no timeout, and the server uses its own.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. Snowflake has no such
// setting for one statement, so WithReadonly(true) fails the statement with
// dbimp.ErrNotSupported (D109).
func WithReadonly(readonly bool) Option {
	return func(o *options) { o.readonly = readonly }
}

// WithParameter sets a parameter of the session of one statement by its
// name, such as "QUERY_TAG", in parameters of the body. The value is written
// as text with fmt.Sprint. The server reads a parameter in the same way in
// each statement, and the parameter ends with the statement, because each
// request is its own session (measured).
//
// A parameter that changes the text of a value is refused with
// dbimp.ErrNotSupported, because the driver reads each value in the form that
// docs/SNOWFLAKE.md names: DATE_OUTPUT_FORMAT, TIME_OUTPUT_FORMAT,
// TIMESTAMP_OUTPUT_FORMAT, TIMESTAMP_LTZ_OUTPUT_FORMAT,
// TIMESTAMP_NTZ_OUTPUT_FORMAT, TIMESTAMP_TZ_OUTPUT_FORMAT,
// BINARY_OUTPUT_FORMAT, GEOGRAPHY_OUTPUT_FORMAT and GEOMETRY_OUTPUT_FORMAT.
// MULTI_STATEMENT_COUNT is refused too, because the driver does not read the
// child statements that a request of several statements makes (D183).
// WithParameter("TIMEZONE", v) replaces the time zone of the DSN. A value
// of nil fails the statement with dbimp.ErrInvalidValue.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase sets the database of one statement, as the path of the DSN
// does.
func WithDatabase(name string) Option {
	return func(o *options) { o.database = name }
}

// WithSchema sets the schema of one statement, as the path of the DSN does.
func WithSchema(name string) Option {
	return func(o *options) { o.schema = name }
}

// WithRole sets the role of one statement, as the key role of the DSN does.
func WithRole(name string) Option {
	return func(o *options) { o.role = name }
}

// WithWarehouse sets the warehouse of one statement, as the key warehouse of
// the DSN does.
func WithWarehouse(name string) Option {
	return func(o *options) { o.warehouse = name }
}

// WithTimeZone sets the time zone of the session of one statement, as the
// parameter TIMEZONE, as the key timezone of the DSN does. A timestamp_ltz
// value of the statement takes this zone.
func WithTimeZone(name string) Option {
	return func(o *options) { o.timeZone = name }
}

// resolve returns the options of one statement, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{
		timeout:   cfg.Timeout,
		timeZone:  cfg.TimeZone,
		database:  cfg.Database,
		schema:    cfg.Schema,
		role:      cfg.Role,
		warehouse: cfg.Warehouse,
	}, args)
}

// refused are the parameters that WithParameter refuses, in upper case. The
// first nine change the text of a value, and the last one starts several
// statements.
var refused = map[string]bool{
	"DATE_OUTPUT_FORMAT":          true,
	"TIME_OUTPUT_FORMAT":          true,
	"TIMESTAMP_OUTPUT_FORMAT":     true,
	"TIMESTAMP_LTZ_OUTPUT_FORMAT": true,
	"TIMESTAMP_NTZ_OUTPUT_FORMAT": true,
	"TIMESTAMP_TZ_OUTPUT_FORMAT":  true,
	"BINARY_OUTPUT_FORMAT":        true,
	"GEOGRAPHY_OUTPUT_FORMAT":     true,
	"GEOMETRY_OUTPUT_FORMAT":      true,
	"MULTI_STATEMENT_COUNT":       true,
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN would refuse.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.readonly:
		return dbimp.Unsupported("WithReadonly")
	}
	for name, value := range o.params {
		switch {
		case name == "" || strings.ContainsFunc(name, func(r rune) bool { return !isNameRune(r) }):
			return fmt.Errorf("applying the option WithParameter: %q is not the name of a parameter: %w", name, dbimp.ErrInvalidValue)
		case value == nil:
			return fmt.Errorf("applying the option WithParameter: the parameter %q has no value: %w", name, dbimp.ErrInvalidValue)
		case refused[strings.ToUpper(name)]:
			return fmt.Errorf("applying the option WithParameter: the parameter %s changes how the driver reads a value: %w", name, dbimp.ErrNotSupported)
		}
	}
	return nil
}

// isNameRune reports whether r can be in the name of a parameter.
func isNameRune(r rune) bool {
	return r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// location returns the zone of a timestamp_ltz value: the time zone of the
// statement, and time.Local when it names none. The name Local is time.Local.
func (o options) location() (*time.Location, error) {
	if o.timeZone == "" {
		return time.Local, nil //nolint:gosmopolitan // D183 item 15: a value follows the system or TZ when no zone is named.
	}
	loc, err := time.LoadLocation(o.timeZone)
	if err != nil {
		return nil, fmt.Errorf("applying the time zone %q: %w", o.timeZone, dbimp.ErrInvalidValue)
	}
	return loc, nil
}

// seconds returns the timeout of the statement in whole seconds, rounded up.
func (o options) seconds() int64 {
	return int64((o.timeout + time.Second - 1) / time.Second)
}

// parameters returns the parameters of the body: the time zone, and each
// parameter that WithParameter set, which replaces one of the same name.
func (o options) parameters() map[string]string {
	if o.timeZone == "" && len(o.params) == 0 {
		return nil
	}
	out := map[string]string{}
	if o.timeZone != "" {
		out["TIMEZONE"] = o.timeZone
	}
	for name, value := range o.params {
		for k := range out {
			if strings.EqualFold(k, name) {
				delete(out, k)
			}
		}
		out[name] = fmt.Sprint(value)
	}
	return out
}
