package tablestore //nolint:testpackage // The tests read the parser and the literals, which the package keeps unexported.

import (
	"database/sql/driver"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestLiteral holds how each Go value is written into a statement, because the
// server binds no argument (recorded: "a question mark parameter").
func TestLiteral(t *testing.T) {
	t.Parallel()
	zone := time.Date(2026, 10, 29, 1, 2, 3, 456, serverZone)
	for _, tt := range []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{true, "TRUE"},
		{false, "FALSE"},
		{int64(-1 << 63), "-9223372036854775808"},
		{int64(0), "0"},
		{1.5, "1.5"},
		{1e300, "1e+300"},
		{math.Copysign(0, -1), "-0"},
		{"", "''"},
		{"it's", "'it''s'"},
		{`a\b`, `'a\\b'`},
		{"a\x00b", `'a\0b'`},
		{"é😀", "'é😀'"},
		{[]byte{0, 0xff, 0x10}, "X'00ff10'"},
		{[]byte{}, "X''"},
		{zone, "'2026-10-29 01:02:03'"},
		{zone.UTC(), "'2026-10-29 01:02:03'"},
	} {
		got, err := literal(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("literal(%#v) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []any{math.NaN(), math.Inf(1), math.Inf(-1), struct{}{}, []int{1}} {
		if _, err := literal(in); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("literal(%#v) gave %v, want dbimp.ErrNotSupported", in, err)
		}
	}
}

// TestBind holds that the parser for placeholders skips a ? in a literal, a quoted
// name and a comment, and that @@version is no placeholder (D34).
func TestBind(t *testing.T) {
	t.Parallel()
	args := func(v ...any) []driver.NamedValue {
		out := make([]driver.NamedValue, len(v))
		for i, x := range v {
			out[i] = driver.NamedValue{Ordinal: i + 1, Value: x}
		}
		return out
	}
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  string
	}{
		{"SELECT ?, ?", args(int64(1), "x"), "SELECT 1, 'x'"},
		{"SELECT '?' , \"?\", `?`, ?", args(int64(1)), "SELECT '?' , \"?\", `?`, 1"},
		{"SELECT ? -- ?\n, ?", args(int64(1), int64(2)), "SELECT 1 -- ?\n, 2"},
		{"SELECT ? /* ? */ # ?\n", args(int64(1)), "SELECT 1 /* ? */ # ?\n"},
		{"SELECT @@version", nil, "SELECT @@version"},
		{`SELECT 'a\'?'`, nil, `SELECT 'a\'?'`},
		{"SELECT ?", args(nil), "SELECT NULL"},
		{"SELECT 1", nil, "SELECT 1"},
	} {
		got, err := syntax.Bind(tt.query, tt.args, literal)
		if err != nil || got != tt.want {
			t.Errorf("Bind(%q) = %q, %v, want %q", tt.query, got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  error
	}{
		{"SELECT ?", nil, dbimp.ErrArguments},
		{"SELECT 1", args(int64(1)), dbimp.ErrArguments},
		{"SELECT 'x", nil, dbimp.ErrUnterminated},
		{"SELECT ?", args(math.NaN()), dbimp.ErrNotSupported},
	} {
		if _, err := syntax.Bind(tt.query, tt.args, literal); !errors.Is(err, tt.want) {
			t.Errorf("Bind(%q) gave %v, want %v", tt.query, err, tt.want)
		}
	}
}
