package maxcompute //nolint:testpackage // The tests read the options, which are not exported.

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

func TestOptionsResolveInOrder(t *testing.T) {
	t.Parallel()
	cfg := recordedConfig()
	cfg.Settings = map[string]string{"odps.sql.timezone": "UTC", "odps.x": "dsn"}
	ctx := WithOptions(t.Context(), WithParameter("odps.x", "context"), WithTimeout(time.Second))
	args := []driver.NamedValue{
		{Ordinal: 1, Value: 7},
		{Ordinal: 2, Value: WithParameter("odps.x", "argument")},
		{Ordinal: 3, Value: WithDatabase("other")},
		{Ordinal: 4, Value: "text"},
	}
	o, rest := resolve(ctx, &cfg, args)
	if o.settings["odps.x"] != "argument" || o.settings["odps.sql.timezone"] != "UTC" {
		t.Errorf("the settings are %v, want the argument over the context over the DSN (D109)", o.settings)
	}
	if o.project != "other" || o.timeout != time.Second {
		t.Errorf("the project is %q and the timeout %v", o.project, o.timeout)
	}
	if len(rest) != 2 || rest[0].Ordinal != 1 || rest[1].Ordinal != 2 || rest[1].Value != "text" {
		t.Errorf("the other arguments are %v, want two, numbered again", rest)
	}
	if cfg.Settings["odps.x"] != "dsn" {
		t.Error("an option changed the settings of the DSN")
	}
}

func TestOptionsAreChecked(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opt  Option
		want error
	}{
		{"a readonly statement", WithReadonly(true), dbimp.ErrNotSupported},
		{"a negative timeout", WithTimeout(-time.Second), dbimp.ErrInvalidValue},
		{"a huge timeout", WithTimeout(1 << 62), dbimp.ErrInvalidValue},
		{"an empty project", WithDatabase(""), dbimp.ErrInvalidValue},
		{"a project with a slash", WithProject("a/b"), dbimp.ErrInvalidValue},
		{"a setting of nil", WithParameter("odps.a", nil), dbimp.ErrInvalidValue},
		{"a setting of a list", WithParameter("odps.a", []string{"x"}), dbimp.ErrInvalidValue},
		{"a setting with no name", WithParameter("", "x"), dbimp.ErrInvalidValue},
		{"a setting with no value", WithParameter("odps.a", ""), dbimp.ErrInvalidValue},
	} {
		cfg := recordedConfig()
		o, _ := resolve(context.Background(), &cfg, []driver.NamedValue{{Ordinal: 1, Value: tt.opt}})
		if err := o.check(); !errors.Is(err, tt.want) {
			t.Errorf("%s: check gave %v, want %v", tt.name, err, tt.want)
		}
	}
	cfg := recordedConfig()
	o, _ := resolve(t.Context(), &cfg, []driver.NamedValue{
		{Ordinal: 1, Value: WithParameter("odps.n", 5)},
		{Ordinal: 2, Value: WithParameter("odps.b", true)},
		{Ordinal: 3, Value: WithReadonly(false)},
	})
	if err := o.check(); err != nil || o.settings["odps.n"] != "5" || o.settings["odps.b"] != "true" {
		t.Errorf("a number and a bool as settings: %v, %v", o.settings, err)
	}
}
