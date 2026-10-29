package maxcompute //nolint:testpackage // The tests read the literals of the driver, which are not exported.

import (
	"database/sql/driver"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// utc is the zone of the tests of a time.
func utc() (*time.Location, error) { return time.UTC, nil }

func TestBindWritesLiterals(t *testing.T) {
	t.Parallel()
	d, _, _ := apd.NewFromString("-12.50")
	for _, tt := range []struct {
		name  string
		query string
		args  []any
		want  string
	}{
		{"no argument", "SELECT 1", nil, "SELECT 1"},
		{"an integer and a float", "SELECT ?, ?", []any{int64(-5), 1.5}, "SELECT -5, 1.5D"},
		{"a float with an exponent", "SELECT ?", []any{1e300}, "SELECT 1e300D"},
		{"a bool and a NULL", "SELECT ?, ?", []any{true, nil}, "SELECT true, NULL"},
		{"a string", "SELECT ?", []any{`a'b\c`}, `SELECT 'a\'b\\c'`},
		{"bytes", "SELECT ?", []any{[]byte{0, 255, 16}}, "SELECT unhex('00FF10')"},
		{"a time", "SELECT ?", []any{time.Date(2026, 10, 29, 1, 2, 3, 4, time.UTC)}, "SELECT TIMESTAMP'2026-10-29 01:02:03.000000004'"},
		{"a date", "SELECT ?", []any{dbimp.Date{Year: 2026, Month: 10, Day: 29}}, "SELECT DATE'2026-10-29'"},
		{"a local time", "SELECT ?", []any{dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 10, Day: 29}, Time: dbimp.LocalTime{Hour: 1}}}, "SELECT TIMESTAMP_NTZ'2026-10-29 01:00:00'"},
		{"a decimal", "SELECT ?", []any{d}, "SELECT -12.50BD"},
		{"months", "SELECT ?", []any{dbimp.Interval{Months: 14}}, "SELECT INTERVAL '1-2' YEAR TO MONTH"},
		{"days", "SELECT ?", []any{dbimp.Interval{Days: 1, Nanoseconds: 3723 * int64(time.Second)}}, "SELECT INTERVAL '1 01:02:03.000000000' DAY TO SECOND"},
		{"a placeholder in a literal and a comment", "SELECT '?', \"?\", `?` -- ?\n, /* ? */ ?", []any{int64(1)}, "SELECT '?', \"?\", `?` -- ?\n, /* ? */ 1"},
		{"a name", "SELECT @a, @a", []any{namedArg("a", int64(2))}, "SELECT 2, 2"},
	} {
		args := make([]driver.NamedValue, len(tt.args))
		for i, a := range tt.args {
			args[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
			if n, ok := a.(named); ok {
				args[i] = driver.NamedValue{Ordinal: i + 1, Name: n.name, Value: n.value}
			}
		}
		got, err := bindArgs(tt.query, args, utc)
		if err != nil || got != tt.want {
			t.Errorf("%s: bindArgs = %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
}

// named is an argument with a name.
type named struct {
	name  string
	value any
}

func namedArg(name string, value any) named { return named{name: name, value: value} }

func TestBindRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		args  []any
		want  error
	}{
		{"NaN", "SELECT ?", []any{math.NaN()}, dbimp.ErrInvalidValue},
		{"an infinity", "SELECT ?", []any{math.Inf(1)}, dbimp.ErrInvalidValue},
		{"a list", "SELECT ?", []any{[]any{1}}, dbimp.ErrArguments},
		{"a map", "SELECT ?", []any{map[string]any{}}, dbimp.ErrArguments},
		{"an interval of months and days", "SELECT ?", []any{dbimp.Interval{Months: 1, Days: 1}}, dbimp.ErrArguments},
		{"a missing argument", "SELECT ?, ?", []any{int64(1)}, dbimp.ErrArguments},
		{"an argument left over", "SELECT ?", []any{int64(1), int64(2)}, dbimp.ErrArguments},
		{"a literal that does not end", "SELECT 'a ?", []any{int64(1)}, dbimp.ErrUnterminated},
	} {
		args := make([]driver.NamedValue, len(tt.args))
		for i, a := range tt.args {
			args[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
		}
		if _, err := bindArgs(tt.query, args, utc); !errors.Is(err, tt.want) {
			t.Errorf("%s: bindArgs gave %v, want %v", tt.name, err, tt.want)
		}
	}
}
