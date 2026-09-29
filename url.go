package dbimp

import (
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ParseURL parses dsn with net/url, and returns an error if its scheme is
// not name (D27 and D35). A driver registers one name, so it takes one
// scheme. An error never holds the DSN, because the DSN can hold a password.
func ParseURL(name, dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		if uerr, ok := errors.AsType[*url.Error](err); ok {
			err = uerr.Err
		}
		return nil, fmt.Errorf("parsing the dsn: %w", err)
	}
	switch {
	case u.Scheme != name:
		return nil, fmt.Errorf("parsing the dsn: scheme %q is not %q: %w", u.Scheme, name, ErrScheme)
	case u.Opaque != "":
		return nil, fmt.Errorf("parsing the dsn: %s:%s has no host: %w", u.Scheme, u.Opaque, ErrInvalidValue)
	}
	// net/url takes a host such as "::" and reads it as the host ":", which
	// no URL can write back. A host with a colon is an IPv6 address, so it
	// must parse as one.
	if h := u.Hostname(); strings.Contains(h, ":") {
		if _, err := netip.ParseAddr(h); err != nil {
			return nil, fmt.Errorf("parsing the dsn: the host %q is not an IPv6 address: %w", h, ErrInvalidValue)
		}
	}
	return u, nil
}

// Query holds the keys of the query of a DSN. NewQuery refuses a key that is
// unknown or repeated, so a value that Query returns is the only value of
// its key.
type Query struct {
	values url.Values
}

// NewQuery reads the query of u. It returns an error for a key that is not
// in known, and for a key that appears more than once (D27).
func NewQuery(u *url.URL, known ...string) (Query, error) {
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Query{}, fmt.Errorf("parsing the query of the dsn: %w", err)
	}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		switch {
		case !slices.Contains(known, key):
			return Query{}, fmt.Errorf("parsing the query of the dsn: %q: %w", key, ErrUnknownKey)
		case len(values[key]) > 1:
			return Query{}, fmt.Errorf("parsing the query of the dsn: %q: %w", key, ErrRepeatedKey)
		}
	}
	return Query{values: values}, nil
}

// Has reports whether the query holds key.
func (q Query) Has(key string) bool {
	return q.values.Has(key)
}

// String returns the value of key, or def if the query does not hold key.
func (q Query) String(key, def string) string {
	if !q.values.Has(key) {
		return def
	}
	return q.values.Get(key)
}

// Bool returns the value of key as a bool, or def if the query does not
// hold key. It takes the forms that strconv.ParseBool takes.
func (q Query) Bool(key string, def bool) (bool, error) {
	if !q.values.Has(key) {
		return def, nil
	}
	v, err := strconv.ParseBool(q.values.Get(key))
	if err != nil {
		return false, fmt.Errorf("parsing key %q: %w: %w", key, ErrInvalidValue, err)
	}
	return v, nil
}

// Int returns the value of key as an int, or def if the query does not hold
// key.
func (q Query) Int(key string, def int) (int, error) {
	if !q.values.Has(key) {
		return def, nil
	}
	v, err := strconv.Atoi(q.values.Get(key))
	if err != nil {
		return 0, fmt.Errorf("parsing key %q: %w: %w", key, ErrInvalidValue, err)
	}
	return v, nil
}

// Duration returns the value of key as a time.Duration, or def if the query
// does not hold key. It takes the forms that time.ParseDuration takes.
func (q Query) Duration(key string, def time.Duration) (time.Duration, error) {
	if !q.values.Has(key) {
		return def, nil
	}
	v, err := time.ParseDuration(q.values.Get(key))
	if err != nil {
		return 0, fmt.Errorf("parsing key %q: %w: %w", key, ErrInvalidValue, err)
	}
	return v, nil
}
