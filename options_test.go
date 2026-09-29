package dbimp_test

import (
	"database/sql/driver"
	"errors"
	"slices"
	"testing"

	"github.com/xo/dbimp"
)

// opts are the options of a driver in the tests.
type opts struct {
	order []string
}

func add(s string) dbimp.Option[opts] {
	return func(o *opts) { o.order = append(o.order, s) }
}

// TestResolve holds the order of D109, the DSN, then the context, then the
// arguments, and the arguments that are left, numbered again from 1.
func TestResolve(t *testing.T) {
	t.Parallel()
	ctx := dbimp.WithOptions(t.Context(), add("ctx1"))
	ctx = dbimp.WithOptions(ctx, add("ctx2"))
	args := []driver.NamedValue{
		{Ordinal: 1, Value: add("arg1")},
		{Ordinal: 2, Value: int64(7)},
		{Ordinal: 3, Value: add("arg2")},
		{Ordinal: 4, Name: "n", Value: "x"},
	}
	got, rest := dbimp.Resolve(ctx, opts{order: []string{"dsn"}}, args)
	if want := []string{"dsn", "ctx1", "ctx2", "arg1", "arg2"}; !slices.Equal(got.order, want) {
		t.Errorf("the order is %q, want %q", got.order, want)
	}
	want := []driver.NamedValue{{Ordinal: 1, Value: int64(7)}, {Ordinal: 2, Name: "n", Value: "x"}}
	if !slices.Equal(rest, want) {
		t.Errorf("the arguments left are %v, want %v", rest, want)
	}
	if !dbimp.IsOption[opts](add("x")) || dbimp.IsOption[opts](int64(7)) {
		t.Error("IsOption does not tell an option from a value")
	}
}

// TestResolveOtherDriver holds that the option of another driver is an
// argument, so that the options of two drivers cannot be mixed.
func TestResolveOtherDriver(t *testing.T) {
	t.Parallel()
	type other struct{}
	opt := dbimp.Option[other](func(*other) {})
	if dbimp.IsOption[opts](opt) {
		t.Error("IsOption took the option of another driver")
	}
	if _, rest := dbimp.Resolve(t.Context(), opts{}, []driver.NamedValue{{Ordinal: 1, Value: opt}}); len(rest) != 1 {
		t.Errorf("Resolve took the option of another driver, and left %v", rest)
	}
}

func TestUnsupported(t *testing.T) {
	t.Parallel()
	if err := dbimp.Unsupported("WithReadonly"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("Unsupported gave %v, want ErrNotSupported", err)
	}
}

// TestMarshalParams holds that a parameter follows the keys of the body, and
// replaces a key of the body with the same name (D109).
func TestMarshalParams(t *testing.T) {
	t.Parallel()
	type body struct {
		A string `json:"a"`
		B int    `json:"b,omitzero"`
		C bool   `json:"c"`
	}
	for _, tt := range []struct {
		params map[string]any
		want   string
	}{
		{nil, `{"a":"x","b":1,"c":true}`},
		{map[string]any{"z": []int{1}, "d": "y"}, `{"a":"x","b":1,"c":true,"d":"y","z":[1]}`},
		{map[string]any{"b": "two"}, `{"a":"x","c":true,"b":"two"}`},
	} {
		got, err := dbimp.MarshalParams(body{A: "x", B: 1, C: true}, tt.params)
		if err != nil || string(got) != tt.want {
			t.Errorf("MarshalParams with %v is %s, %v, want %s", tt.params, got, err, tt.want)
		}
	}
	if _, err := dbimp.MarshalParams(body{}, map[string]any{"f": func() {}}); err == nil {
		t.Error("MarshalParams encoded a func, want an error")
	}
}
