package bigquery //nolint:testpackage // The tests read the parameters that the driver builds, which are not exported.

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// args makes the arguments that database/sql hands to the driver.
func args(vals ...any) []driver.NamedValue {
	out := make([]driver.NamedValue, len(vals))
	for i, v := range vals {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
		if n, ok := v.(sql.NamedArg); ok {
			out[i].Name, out[i].Value = n.Name, n.Value
		}
	}
	return out
}

// TestBindArgsModes holds D189 item 8: arguments that are all named go in the mode
// NAMED, arguments with no name go in the mode POSITIONAL, and a mix is refused
// before the service refuses it (recorded: bigquery-152 and bigquery-153).
func TestBindArgsModes(t *testing.T) {
	t.Parallel()
	mode, params, err := bindArgs(args(sql.Named("a", int64(1)), sql.Named("b", "x")))
	if err != nil || mode != modeNamed || len(params) != 2 || params[0].Name != "a" || params[1].Name != "b" {
		t.Errorf("named arguments gave %q, %+v, %v", mode, params, err)
	}
	mode, params, err = bindArgs(args(int64(1), "x"))
	if err != nil || mode != modePositional || len(params) != 2 || params[0].Name != "" {
		t.Errorf("positional arguments gave %q, %+v, %v", mode, params, err)
	}
	mode, params, err = bindArgs(nil)
	if err != nil || mode != "" || params != nil {
		t.Errorf("no argument gave %q, %+v, %v", mode, params, err)
	}
	for _, bad := range [][]driver.NamedValue{
		args(int64(1), sql.Named("b", "x")),
		args(sql.Named("a", int64(1)), "x"),
		args(sql.Named("a", int64(1)), sql.Named("a", int64(2))),
		args(sql.Named("1a", int64(1))),
		args(sql.Named("a b", int64(1))),
		args([]any{1}),
		args(map[string]any{"a": 1}),
		args(struct{}{}),
	} {
		if _, _, err := bindArgs(bad); err == nil {
			t.Errorf("bindArgs(%v) gave no error", bad)
		}
	}
}

// TestBind holds D189 with the parameter type of each Go type, which the
// recordings show, and the text of its value.
func TestBind(t *testing.T) {
	t.Parallel()
	date := dbimp.Date{Year: 2024, Month: time.January, Day: 2}
	for _, tt := range []struct {
		name string
		in   any
		typ  string
		val  string
	}{
		{"int64", int64(-5), "INT64", "-5"},
		{"largest", int64(math.MaxInt64), "INT64", "9223372036854775807"},
		{"float64", 1.5, "FLOAT64", "1.5"},
		{"tenth", 0.1, "FLOAT64", "0.1"},
		{"NaN", math.NaN(), "FLOAT64", "NaN"},
		{"infinity", math.Inf(1), "FLOAT64", "Infinity"},
		{"negative infinity", math.Inf(-1), "FLOAT64", "-Infinity"},
		{"string", "héllo", "STRING", "héllo"},
		{"empty string", "", "STRING", ""},
		{"true", true, "BOOL", "true"},
		{"false", false, "BOOL", "false"},
		{"bytes", []byte("abc"), "BYTES", "YWJj"},
		{"empty bytes", []byte{}, "BYTES", ""},
		{"date", date, "DATE", "2024-01-02"},
		{"first date", dbimp.Date{Year: 1, Month: time.January, Day: 1}, "DATE", "0001-01-01"},
		{"time", dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}, "TIME", "12:34:56.789"},
		{"midnight", dbimp.LocalTime{}, "TIME", "00:00:00"},
		{"datetime", dbimp.LocalDateTime{Date: date, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5, Nanosecond: 123456000}}, "DATETIME", "2024-01-02T03:04:05.123456"},
		{"timestamp", at(123456), "TIMESTAMP", "2024-01-02 03:04:05.123456+00:00"},
		{"timestamp in a zone", time.Date(2024, 1, 2, 10, 4, 5, 0, time.FixedZone("", 7*3600)), "TIMESTAMP", "2024-01-02 03:04:05+00:00"},
		{"first timestamp", time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), "TIMESTAMP", "0001-01-01 00:00:00+00:00"},
		{"last timestamp", time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC), "TIMESTAMP", "9999-12-31 23:59:59.999999+00:00"},
		{"numeric", dec(t, "1.5"), "NUMERIC", "1.5"},
		{"largest numeric", dec(t, "99999999999999999999999999999.999999999"), "NUMERIC", "99999999999999999999999999999.999999999"},
		{"negative numeric", dec(t, "-0.000000001"), "NUMERIC", "-0.000000001"},
		{"scale of ten", dec(t, "0.0000000001"), "BIGNUMERIC", "0.0000000001"},
		{"thirty digits", dec(t, "100000000000000000000000000000"), "BIGNUMERIC", "100000000000000000000000000000"},
		{"largest bignumeric", dec(t, "578960446186580977117854925043439539266.34992332820282019728792003956564819967"), "BIGNUMERIC", "578960446186580977117854925043439539266.34992332820282019728792003956564819967"},
		{"interval", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60 + 6) * int64(time.Second)}, "INTERVAL", "1-2 3 4:5:6"},
		{"interval with a fraction", dbimp.Interval{Nanoseconds: 789 * int64(time.Millisecond)}, "INTERVAL", "0-0 0 0:0:0.789"},
		{"negative interval", dbimp.Interval{Months: -5, Days: -1, Nanoseconds: -((1*60+2)*60 + 3) * int64(time.Second)}, "INTERVAL", "-0-5 -1 -1:2:3"},
		{"null", nil, "STRING", ""},
	} {
		typ, val, err := bind(tt.in)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if typ != tt.typ {
			t.Errorf("%s: the type is %s, want %s", tt.name, typ, tt.typ)
		}
		switch {
		case tt.name == "null":
			if val != nil {
				t.Errorf("a NULL has the value %q, want none", *val)
			}
		case val == nil || *val != tt.val:
			t.Errorf("%s: the value is %v, want %q", tt.name, val, tt.val)
		}
	}
}

// TestBindIsWrittenAsTheRecordingsShow holds the member names of a parameter:
// a NULL has parameterValue with no value, and a positional parameter has no
// name (recorded: bigquery-133 and bigquery-134).
func TestBindIsWrittenAsTheRecordingsShow(t *testing.T) {
	t.Parallel()
	_, params, err := bindArgs(args(nil, int64(5)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"parameterType":{"type":"STRING"},"parameterValue":{}},{"parameterType":{"type":"INT64"},"parameterValue":{"value":"5"}}]`; !sameJSON(b, []byte(want)) {
		t.Errorf("the parameters are %s, want %s", b, want)
	}
}

// TestBindRefuses holds that a value that the service cannot hold is refused by
// the driver, with an error that wraps dbimp.ErrInvalidValue, or
// dbimp.ErrArguments for a type that the driver does not bind.
func TestBindRefuses(t *testing.T) {
	t.Parallel()
	inf := &apd.Decimal{Form: apd.Infinite}
	for _, tt := range []struct {
		name string
		in   any
		want error
	}{
		{"infinite decimal", inf, dbimp.ErrInvalidValue},
		{"decimal beyond bignumeric", dec(t, "1"+strings.Repeat("0", 39)), dbimp.ErrInvalidValue},
		{"scale beyond bignumeric", dec(t, "0."+strings.Repeat("0", 38)+"1"), dbimp.ErrInvalidValue},
		{"invalid date", dbimp.Date{Year: 2024, Month: time.February, Day: 30}, dbimp.ErrInvalidValue},
		{"year 0", dbimp.Date{Year: 0, Month: time.January, Day: 1}, dbimp.ErrInvalidValue},
		{"nanoseconds of a time", dbimp.LocalTime{Nanosecond: 1}, dbimp.ErrInvalidValue},
		{"nanoseconds of a datetime", dbimp.LocalDateTime{Date: dbimp.Date{Year: 1, Month: 1, Day: 1}, Time: dbimp.LocalTime{Nanosecond: 1}}, dbimp.ErrInvalidValue},
		{"nanoseconds of a timestamp", time.Date(2024, 1, 2, 0, 0, 0, 1, time.UTC), dbimp.ErrInvalidValue},
		{"timestamp of the year 10000", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), dbimp.ErrInvalidValue},
		{"nanoseconds of an interval", dbimp.Interval{Nanoseconds: 1}, dbimp.ErrInvalidValue},
		{"list", []any{1}, dbimp.ErrArguments},
		{"map", map[string]any{"a": 1}, dbimp.ErrArguments},
		{"a type that the driver does not bind", struct{}{}, dbimp.ErrArguments},
	} {
		if _, _, err := bind(tt.in); !isErr(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", tt.name, err, tt.want)
		}
	}
}

// TestCheckNamedValue holds W4: the connection keeps an Option and the values
// that the driver binds with a type of their own, turns a nil pointer into
// nil, and hands every other value to database/sql with driver.ErrSkip.
func TestCheckNamedValue(t *testing.T) {
	t.Parallel()
	c := &conn{}
	for _, v := range []any{
		WithTimeout(time.Second), dec(t, "1"), dbimp.Date{}, dbimp.LocalTime{}, dbimp.LocalDateTime{}, dbimp.Interval{}, []any{}, map[string]any{},
	} {
		nv := &driver.NamedValue{Value: v}
		if err := c.CheckNamedValue(nv); err != nil {
			t.Errorf("CheckNamedValue(%T) = %v, want nil", v, err)
		}
	}
	// A decimal that is not a pointer becomes one.
	nv := &driver.NamedValue{Value: *dec(t, "2.5")}
	if err := c.CheckNamedValue(nv); err != nil {
		t.Fatal(err)
	}
	if d, ok := nv.Value.(*apd.Decimal); !ok || d.String() != "2.5" {
		t.Errorf("a decimal became %#v, want a *apd.Decimal", nv.Value)
	}
	var null *apd.Decimal
	nv = &driver.NamedValue{Value: null}
	if err := c.CheckNamedValue(nv); err != nil || nv.Value != nil {
		t.Errorf("a nil decimal gave %v and %v, want nil", err, nv.Value)
	}
	for _, v := range []any{int64(1), "x", true, 1.5, []byte("x"), time.Time{}, int32(1)} {
		if err := c.CheckNamedValue(&driver.NamedValue{Value: v}); !errors.Is(err, driver.ErrSkip) {
			t.Errorf("CheckNamedValue(%T) = %v, want driver.ErrSkip", v, err)
		}
	}
}
