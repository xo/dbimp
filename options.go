package dbimp

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
)

// Option sets one option of a statement or of a transaction of a driver,
// whose options are the struct T (D109). A driver names it as its own type,
// such as type Option = dbimp.Option[options], so that the options of two
// drivers cannot be mixed.
type Option[T any] func(*T)

// optionsKey is the key of the options of the type T in a context.
type optionsKey[T any] struct{}

// WithOptions returns a context that carries opts, after the options that
// ctx carries. Each statement and each transaction that starts with the
// context applies them after the options of the DSN.
func WithOptions[T any](ctx context.Context, opts ...Option[T]) context.Context {
	prev, _ := ctx.Value(optionsKey[T]{}).([]Option[T])
	return context.WithValue(ctx, optionsKey[T]{}, slices.Concat(prev, opts))
}

// IsOption reports whether v is an Option of a driver whose options are T.
// CheckNamedValue of the driver keeps such a value, so that database/sql
// hands it to the statement, and Resolve takes it out there. A connection
// then holds no option between two statements.
func IsOption[T any](v any) bool {
	_, ok := v.(Option[T])
	return ok
}

// Resolve returns dsn, which holds the options of the DSN, with the options
// of ctx and then the Option arguments of args applied to it, in that order,
// so that a later one wins. It returns the other arguments, numbered again
// from 1, as database/sql numbers them when it removes an argument.
func Resolve[T any](ctx context.Context, dsn T, args []driver.NamedValue) (T, []driver.NamedValue) {
	fromCtx, _ := ctx.Value(optionsKey[T]{}).([]Option[T])
	for _, opt := range fromCtx {
		opt(&dsn)
	}
	var rest []driver.NamedValue
	for _, a := range args {
		if opt, ok := a.Value.(Option[T]); ok {
			opt(&dsn)
			continue
		}
		a.Ordinal = len(rest) + 1
		rest = append(rest, a)
	}
	return dsn, rest
}

// Unsupported returns the error of an option that the server of a driver
// cannot honor, so that a caller never believes that it holds (D109).
func Unsupported(option string) error {
	return fmt.Errorf("applying the option %s: the server has no such setting: %w", option, ErrNotSupported)
}

// MarshalParams returns v, which encodes as a JSON object, with the keys of
// params, which WithParameter of a driver sets (D109). A key of params
// replaces the key of v with the same name, as it does in Couchbase (D40).
// The keys of v keep their order, and the keys of params follow in the order
// of their names.
func MarshalParams(v any, params map[string]any) ([]byte, error) {
	buf, err := json.Marshal(v)
	if err != nil || len(params) == 0 {
		return buf, err
	}
	var out bytes.Buffer
	enc := jsontext.NewEncoder(&out)
	dec := jsontext.NewDecoder(bytes.NewReader(buf))
	if _, err := dec.ReadToken(); err != nil {
		return nil, fmt.Errorf("reading the body: %w", err)
	}
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return nil, fmt.Errorf("writing the body: %w", err)
	}
	for dec.PeekKind() != '}' {
		name, err := dec.ReadToken()
		if err != nil {
			return nil, fmt.Errorf("reading the body: %w", err)
		}
		// ReadValue voids the token, so keep the name first.
		k := name.String()
		val, err := dec.ReadValue()
		if err != nil {
			return nil, fmt.Errorf("reading the body: %w", err)
		}
		if _, ok := params[k]; ok {
			continue
		}
		if err := enc.WriteToken(jsontext.String(k)); err != nil {
			return nil, fmt.Errorf("writing the key %q: %w", k, err)
		}
		if err := enc.WriteValue(val); err != nil {
			return nil, fmt.Errorf("writing the key %q: %w", k, err)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(params)) {
		if err := enc.WriteToken(jsontext.String(k)); err != nil {
			return nil, fmt.Errorf("writing the parameter %q: %w", k, err)
		}
		if err := json.MarshalEncode(enc, params[k]); err != nil {
			return nil, fmt.Errorf("writing the parameter %q: %w", k, err)
		}
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return nil, fmt.Errorf("writing the body: %w", err)
	}
	return bytes.TrimSpace(out.Bytes()), nil
}
