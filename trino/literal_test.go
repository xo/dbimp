package trino //nolint:testpackage // The tests read the writer of the literals, which is not exported.

import (
	"database/sql/driver"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// TestLiterals holds the literals that the driver writes for each argument,
// which the servers parse (D34 and D175).
func TestLiterals(t *testing.T) { //nolint:maintidx // One table of the literal of every Go type.
	t.Parallel()
	jakarta := time.FixedZone("", 7*3600)
	for _, tt := range []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "NULL"},
		{"true", true, "TRUE"},
		{"false", false, "FALSE"},
		{"int", int64(-42), "-42"},
		{"the largest int", int64(math.MaxInt64), "9223372036854775807"},
		{"the smallest int", int64(math.MinInt64), "BIGINT '-9223372036854775808'"},
		{"float", 1.5, "DOUBLE '1.5'"},
		{"a float with an exponent", 1e300, "DOUBLE '1e+300'"},
		{"NaN", math.NaN(), "nan()"},
		{"+Inf", math.Inf(1), "infinity()"},
		{"-Inf", math.Inf(-1), "-infinity()"},
		{"string", "it's", "'it''s'"},
		{"a string with a backslash and a newline", "a\\b\nc", "'a\\b\nc'"},
		{"an empty string", "", "''"},
		{"bytes", []byte{0, 0xff}, "X'00ff'"},
		{"empty bytes", []byte{}, "X''"},
		{"a time", time.Date(2026, 10, 1, 12, 34, 56, 123456789, jakarta), "TIMESTAMP '2026-10-01 12:34:56.123456789 +07:00'"},
		{"a time in UTC", time.Date(2026, 10, 1, 12, 34, 56, 0, time.UTC), "TIMESTAMP '2026-10-01 12:34:56.000000000 +00:00'"},
		{"a time west of UTC", time.Date(1, 1, 1, 0, 0, 0, 0, time.FixedZone("", -5*3600-30*60)), "TIMESTAMP '0001-01-01 00:00:00.000000000 -05:30'"},
		{"a decimal", mustDecimal(t, "-1234567890123456789012345678901234567.8"), "DECIMAL '-1234567890123456789012345678901234567.8'"},
		{"a decimal with an exponent", mustDecimal(t, "1E+3"), "DECIMAL '1000'"},
		{"a date", dbimp.Date{Year: 2026, Month: 10, Day: 1}, "DATE '2026-10-01'"},
		{"a negative date", dbimp.Date{Year: -1, Month: 1, Day: 1}, "DATE '-0001-01-01'"},
		{"a local time", dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3, Nanosecond: 4}, "TIME '01:02:03.000000004'"},
		{"an offset time", dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12}, Offset: -19800}, "TIME '12:00:00.000000000 -05:30'"},
		{"a local date time", dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 10, Day: 1}, Time: dbimp.LocalTime{Hour: 12, Nanosecond: 5e8}}, "TIMESTAMP '2026-10-01 12:00:00.500000000'"},
		{"months", dbimp.Interval{Months: 14}, "INTERVAL '1-2' YEAR TO MONTH"},
		{"negative months", dbimp.Interval{Months: -14}, "INTERVAL - '1-2' YEAR TO MONTH"},
		{"days and time", dbimp.Interval{Days: 3, Nanoseconds: 14706789e6}, "INTERVAL '3 04:05:06.789' DAY TO SECOND"},
		{"a negative day", dbimp.Interval{Days: -1}, "INTERVAL - '1 00:00:00.000' DAY TO SECOND"},
		{"days and a negative time", dbimp.Interval{Days: 1, Nanoseconds: -3600e9}, "INTERVAL '0 23:00:00.000' DAY TO SECOND"},
		{"a zero interval", dbimp.Interval{}, "INTERVAL '0 00:00:00.000' DAY TO SECOND"},
		{"a uuid", uuid.UUID{0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78}, "UUID '12345678-1234-5678-1234-567812345678'"},
		{"a list", []any{int64(1), nil, "x"}, "ARRAY[1, NULL, 'x']"},
		{"an empty list", []any{}, "ARRAY[]"},
		{"strings", []string{"a", "b'c"}, "ARRAY['a', 'b''c']"},
		{"ints", []int64{1, 2}, "ARRAY[1, 2]"},
		{"floats", []float64{1.5}, "ARRAY[DOUBLE '1.5']"},
		{"bools", []bool{true}, "ARRAY[TRUE]"},
		{"a nested list", []any{[]any{int64(1)}, []string{"a"}}, "ARRAY[ARRAY[1], ARRAY['a']]"},
		{"a map", map[string]any{"b": int64(2), "a": []any{int64(1)}}, "MAP(ARRAY['a', 'b'], ARRAY[ARRAY[1], 2])"},
		{"an empty map", map[string]any{}, "MAP()"},
	} {
		got, err := (writer{}).literal(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("%s: the literal is %q, %v, want %q", tt.name, got, err, tt.want)
		}
	}
}

// TestLiteralErrors holds the values that have no literal, and the values that
// Presto cannot hold. An error never cuts a value without a sign (D175).
func TestLiteralErrors(t *testing.T) {
	t.Parallel()
	ms := dbimp.LocalTime{Hour: 1, Nanosecond: 1e6}
	for _, tt := range []struct {
		name   string
		presto bool
		in     any
		want   error
	}{
		{"a decimal that is NaN", false, mustDecimal(t, "NaN"), dbimp.ErrInvalidValue},
		{"a decimal that is infinite", false, mustDecimal(t, "Infinity"), dbimp.ErrInvalidValue},
		{"a struct", false, struct{}{}, dbimp.ErrNotSupported},
		{"an int", false, 5, dbimp.ErrNotSupported},
		{"an interval of months and days", false, dbimp.Interval{Months: 1, Days: 1}, dbimp.ErrInvalidValue},
		{"an interval finer than a millisecond", false, dbimp.Interval{Nanoseconds: 1}, dbimp.ErrInvalidValue},
		{"a year of five digits", false, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), dbimp.ErrInvalidValue},
		{"an offset with seconds", false, time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", 3601)), dbimp.ErrInvalidValue},
		{"a nested value with no literal", false, []any{struct{}{}}, dbimp.ErrNotSupported},
		{"a map value with no literal", false, map[string]any{"a": 1}, dbimp.ErrNotSupported},
		{"a time finer than a millisecond on Presto", true, dbimp.LocalTime{Nanosecond: 1}, dbimp.ErrInvalidValue},
		{"a timestamp finer than a millisecond on Presto", true, time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC), dbimp.ErrInvalidValue},
		{"a local timestamp finer than a millisecond on Presto", true, dbimp.LocalDateTime{Time: dbimp.LocalTime{Nanosecond: 1}}, dbimp.ErrInvalidValue},
		{"an offset time finer than a millisecond on Presto", true, dbimp.OffsetTime{Time: dbimp.LocalTime{Nanosecond: 1}}, dbimp.ErrInvalidValue},
	} {
		if got, err := (writer{presto: tt.presto}).literal(tt.in); !errors.Is(err, tt.want) {
			t.Errorf("%s: the literal is %q, %v, want an error that wraps %v", tt.name, got, err, tt.want)
		}
	}
	if got, err := (writer{presto: true}).literal(ms); err != nil || got != "TIME '01:00:00.001'" {
		t.Errorf("a time of a whole millisecond on Presto is %q, %v, want its literal", got, err)
	}
}

// TestLiteralsOfTheArguments holds that the literals come in the order of the
// arguments, and that a named argument is refused.
func TestLiteralsOfTheArguments(t *testing.T) {
	t.Parallel()
	got, err := literals([]driver.NamedValue{{Ordinal: 1, Value: int64(1)}, {Ordinal: 2, Value: "b"}}, false)
	if err != nil || got != "1, 'b'" {
		t.Errorf("the literals are %q, %v, want 1, 'b'", got, err)
	}
	if _, err := literals([]driver.NamedValue{{Name: "a", Ordinal: 1, Value: int64(1)}}, false); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a named argument gave %v, want dbimp.ErrArguments", err)
	}
	_, err = literals([]driver.NamedValue{{Ordinal: 1, Value: int64(1)}, {Ordinal: 2, Value: struct{}{}}}, false)
	if !errors.Is(err, dbimp.ErrNotSupported) || !strings.Contains(err.Error(), "argument 2") {
		t.Errorf("an argument with no literal gave %v, want dbimp.ErrNotSupported that names argument 2", err)
	}
}

// TestEscape holds the escape of the headers.
func TestEscape(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"SELECT ? + 1": "SELECT%20%3F%20%2B%201",
		"a=b,c":        "a%3Db%2Cc",
		"é\n":          "%C3%A9%0A",
		"":             "",
		"5m":           "5m",
	} {
		if got := escape(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}

// mustDecimal returns the decimal that text writes.
func mustDecimal(t *testing.T, text string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(text)
	if err != nil {
		t.Fatalf("reading %q as a decimal: %v", text, err)
	}
	return d
}
