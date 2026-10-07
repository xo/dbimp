package solr

import (
	"context"
	"database/sql/driver"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xo/dbimp"
)

// Option sets an option of one statement (D109). An option comes from the
// DSN, then from the context through WithOptions, then from an argument of
// the statement, and a later one wins.
type Option = dbimp.Option[options]

// options are the options of one request.
type options struct {
	timeout    time.Duration
	params     map[string]any
	collection string
}

// WithOptions returns a context that carries opts. Each statement started
// with the context applies them after the options of the DSN.
func WithOptions(ctx context.Context, opts ...Option) context.Context {
	return dbimp.WithOptions(ctx, opts...)
}

// WithTimeout cannot give the server a time for the statement. The server has
// no setting that honors one: timeAllowed cut nothing (measured), so a
// statement with a timeout other than zero fails with dbimp.ErrNotSupported.
// The deadline of the context stops the request.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithReadonly asks that the statement write nothing. The SQL of Solr takes
// no write of any kind (D163), so every statement is read-only, and the
// option changes nothing.
func WithReadonly(bool) Option {
	return func(*options) {}
}

// WithParameter sets a field of the form of POST /solr/<collection>/sql by
// its name, such as "numWorkers". A string is sent as it is, a []string as
// one field for each element, and any other value as its text. A field named
// here replaces one that the driver sets itself, such as "includeMetadata"
// or "aggregationMode". So WithParameter("aggregationMode", "map_reduce")
// turns on the mode that the DSN refuses, and WithParameter("includeMetadata",
// "false") makes the answer name no column before its rows.
func WithParameter(name string, value any) Option {
	return func(o *options) {
		if o.params == nil {
			o.params = map[string]any{}
		}
		o.params[name] = value
	}
}

// WithDatabase names the collection whose /sql handler takes the statement,
// in place of the collection of the DSN (D166). FROM still names the table.
func WithDatabase(name string) Option {
	return func(o *options) { o.collection = name }
}

// resolve returns the options of one request, those of the DSN, then those
// of the context, then the Option arguments of args, and the other arguments
// (D109).
func resolve(ctx context.Context, cfg *Config, args []driver.NamedValue) (options, []driver.NamedValue) {
	return dbimp.Resolve(ctx, options{collection: cfg.Collection}, args)
}

// check returns an error for an option that the server cannot honor, or
// whose value the DSN refuses.
func (o options) check() error {
	switch {
	case o.timeout < 0:
		return fmt.Errorf("applying the option WithTimeout: %v: %w", o.timeout, dbimp.ErrInvalidValue)
	case o.timeout > 0:
		return dbimp.Unsupported("WithTimeout")
	case o.collection == "":
		return fmt.Errorf("choosing the collection: the DSN has no path and the statement has no WithDatabase: %w", dbimp.ErrInvalidValue)
	}
	return nil
}

// form returns the form of POST /solr/<collection>/sql for stmt (measured).
// The driver asks for the first tuple that names the columns, and sends the
// aggregationMode of the DSN.
func (o options) form(stmt, mode string) url.Values {
	v := url.Values{
		"stmt":            {stmt},
		"includeMetadata": {"true"},
		"aggregationMode": {mode},
	}
	for _, name := range slices.Sorted(maps.Keys(o.params)) {
		v.Del(name)
		switch p := o.params[name].(type) {
		case string:
			v.Set(name, p)
		case []string:
			v[name] = slices.Clone(p)
		case int:
			v.Set(name, strconv.Itoa(p))
		case bool:
			v.Set(name, strconv.FormatBool(p))
		default:
			v.Set(name, strings.TrimSpace(fmt.Sprint(p)))
		}
	}
	return v
}
