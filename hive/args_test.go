package hive //nolint:testpackage // The test reads the literals of the driver, which are not exported.

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

func TestBind(t *testing.T) {
	t.Parallel()
	d := dbimp.Date{Year: 2026, Month: time.January, Day: 2}
	dt := dbimp.LocalDateTime{Date: d, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5, Nanosecond: 120000000}}
	dec, _, err := apd.NewFromString("1.50")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		query string
		args  []any
		want  string
	}{
		{"select ?", []any{int64(1)}, "select 1"},
		{"select ?, ?", []any{"it's a \\ test", nil}, `select 'it\'s a \\ test', NULL`},
		{"select ? -- a ?\n, '?', `?`", []any{true}, "select true -- a ?\n, '?', `?`"},
		{"select ? /* ? */", []any{[]byte{0, 255}}, "select cast(unhex('00ff') as binary) /* ? */"},
		{"select ?", []any{d}, "select cast('2026-01-02' as date)"},
		{"select ?", []any{dt}, "select cast('2026-01-02 03:04:05.12' as timestamp)"},
		{"select ?", []any{dec}, "select 1.50"},
		{"select ?", []any{1.5}, "select 1.5"},
		{"select 1", nil, "select 1"},
	} {
		args := make([]driver.NamedValue, len(tt.args))
		for i, a := range tt.args {
			args[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
		}
		got, err := bind(tt.query, args)
		if err != nil || got != tt.want {
			t.Errorf("bind(%q, %v) = %q, %v, want %q", tt.query, tt.args, got, err, tt.want)
		}
	}
}

func TestBindRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  error
	}{
		{"select ?", nil, dbimp.ErrArguments},
		{"select 1", []driver.NamedValue{{Ordinal: 1, Value: int64(1)}}, dbimp.ErrArguments},
		{"select 'abc", nil, dbimp.ErrUnterminated},
		{"select ?", []driver.NamedValue{{Ordinal: 1, Value: struct{}{}}}, dbimp.ErrNotSupported},
	} {
		if _, err := bind(tt.query, tt.args); !errors.Is(err, tt.want) {
			t.Errorf("bind(%q, %v) = %v, want %v", tt.query, tt.args, err, tt.want)
		}
	}
}
