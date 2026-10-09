package snowflake //nolint:testpackage // The tests call the functions that bind and decode a value, which are not exported.

import (
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// binds returns the bindings of args as the body writes them.
func binds(t *testing.T, args ...driver.NamedValue) string {
	t.Helper()
	b, err := bindArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(struct {
		B bindings `json:"bindings"`
	}{b})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestBindingsOrder holds that the body writes the bindings in the order of the
// arguments, with their numbers or their names as keys (D183).
func TestBindingsOrder(t *testing.T) {
	t.Parallel()
	got := binds(t,
		driver.NamedValue{Ordinal: 1, Value: int64(1)},
		driver.NamedValue{Ordinal: 2, Value: "b"},
		driver.NamedValue{Name: "named", Ordinal: 3, Value: nil},
		driver.NamedValue{Ordinal: 10, Value: true},
	)
	want := `{"bindings":{"1":{"type":"FIXED","value":"1"},"2":{"type":"TEXT","value":"b"},"named":{"type":"TEXT","value":null},"10":{"type":"BOOLEAN","value":"true"}}}`
	if got != want {
		t.Errorf("the bindings are\n%s\nwant\n%s", got, want)
	}
	if b, err := bindArgs(nil); err != nil || b != nil {
		t.Errorf("no arguments gave %v, %v, want no bindings", b, err)
	}
}

// TestBind holds D183 item 6: each Go type is the type of the binding that it
// names, with the text that the recordings show, and a value that the types of
// the server cannot hold fails.
func TestBind(t *testing.T) {
	t.Parallel()
	dec := func(s string) *apd.Decimal {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for _, tt := range []struct {
		name string
		in   any
		typ  string
		want string
	}{
		{"int64 min", int64(math.MinInt64), "FIXED", "-9223372036854775808"},
		{"float", 1.5, "REAL", "1.5"},
		{"float exponent", 1e300, "REAL", "1e+300"},
		{"float NaN", math.NaN(), "REAL", "NaN"},
		{"float +Inf", math.Inf(1), "REAL", "Infinity"},
		{"float -Inf", math.Inf(-1), "REAL", "-Infinity"},
		{"empty string", "", "TEXT", ""},
		{"false", false, "BOOLEAN", "false"},
		{"empty bytes", []byte{}, "BINARY", ""},
		{"bytes", []byte{0x0a, 0xff}, "BINARY", "0AFF"},
		{"date", dbimp.Date{Year: 1970, Month: time.January, Day: 1}, "DATE", "0"},
		{"date before 1970", dbimp.Date{Year: 1900, Month: time.January, Day: 1}, "DATE", "-2208988800000"},
		{"date at the end", dbimp.Date{Year: 9999, Month: time.December, Day: 31}, "DATE", "253402214400000"},
		{"time", dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}, "TIME", "86399999999999"},
		{"midnight", dbimp.LocalTime{}, "TIME", "0"},
		{"local date and time", dbimp.LocalDateTime{Date: dbimp.Date{Year: 1970, Month: time.January, Day: 1}, Time: dbimp.LocalTime{Nanosecond: 1}}, "TIMESTAMP_NTZ", "1"},
		{"local date and time before 1970", dbimp.LocalDateTime{Date: dbimp.Date{Year: 1969, Month: time.December, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}}, "TIMESTAMP_NTZ", "-1"},
		{"local date and time at the end", dbimp.LocalDateTime{Date: dbimp.Date{Year: 9999, Month: time.December, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}}, "TIMESTAMP_NTZ", "253402300799999999999"},
		{"instant at +07:00", time.Unix(1791549296, 123456789).In(time.FixedZone("", 7*3600)), "TIMESTAMP_TZ", "1791549296123456789 1860"},
		{"instant at -03:30", time.Unix(0, 0).In(time.FixedZone("", -(3*3600 + 1800))), "TIMESTAMP_TZ", "0 1230"},
		{"instant in UTC", time.Unix(-1, 0).UTC(), "TIMESTAMP_TZ", "-1000000000 1440"},
		{"decimal", dec("12345678.91"), "FIXED", "12345678.91"},
		{"decimal with an exponent", dec("1E+3"), "FIXED", "1000"},
		{"decimal of 38 digits", dec("-99999999999999999999999999999999999999"), "FIXED", "-99999999999999999999999999999999999999"},
	} {
		got, err := bind(tt.in)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got.Type != tt.typ || got.Value == nil || *got.Value != tt.want {
			v := "<null>"
			if got.Value != nil {
				v = *got.Value
			}
			t.Errorf("%s: the binding is %s %q, want %s %q", tt.name, got.Type, v, tt.typ, tt.want)
		}
	}
	if got, err := bind(nil); err != nil || got.Type != "TEXT" || got.Value != nil {
		t.Errorf("a NULL is %+v, %v, want a TEXT of null", got, err)
	}
}

// TestBindRefuses holds D183 item 6: a variant is not a type of a binding, so a
// list or a map fails with dbimp.ErrArguments, and so does a value that
// has no type of a binding.
func TestBindRefuses(t *testing.T) {
	t.Parallel()
	nan := apd.New(0, 0)
	nan.Form = apd.NaN
	inf := apd.New(0, 0)
	inf.Form = apd.Infinite
	for _, tt := range []struct {
		name string
		in   any
		want error
	}{
		{"a list", []any{int64(1)}, dbimp.ErrArguments},
		{"a map", map[string]any{"a": int64(1)}, dbimp.ErrArguments},
		{"a struct", struct{}{}, dbimp.ErrArguments},
		{"a decimal NaN", nan, dbimp.ErrInvalidValue},
		{"a decimal infinity", inf, dbimp.ErrInvalidValue},
		{"an invalid date", dbimp.Date{Year: 2026, Month: 13, Day: 1}, dbimp.ErrInvalidValue},
		{"an invalid time", dbimp.LocalTime{Hour: 24}, dbimp.ErrInvalidValue},
		{"an invalid date and time", dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 1, Day: 1}, Time: dbimp.LocalTime{Minute: 60}}, dbimp.ErrInvalidValue},
	} {
		if _, err := bind(tt.in); !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
	}
}

// TestCheckNamedValue holds that the connection keeps the values that the driver
// binds with a type of their own, converts a nil pointer to NULL, and hands every
// other value to database/sql.
func TestCheckNamedValue(t *testing.T) {
	t.Parallel()
	c := &conn{}
	d, _, _ := apd.NewFromString("1.5")
	for _, tt := range []struct {
		name string
		in   any
		want error
		out  any
	}{
		{"an option", WithRole("x"), nil, nil},
		{"a decimal", d, nil, d},
		{"a nil decimal", (*apd.Decimal)(nil), nil, nil},
		{"a date", dbimp.Date{Year: 2026, Month: 1, Day: 1}, nil, dbimp.Date{Year: 2026, Month: 1, Day: 1}},
		{"a time", dbimp.LocalTime{}, nil, dbimp.LocalTime{}},
		{"a list", []any{1}, nil, nil},
		{"a map", map[string]any{}, nil, nil},
		{"an int", 5, driver.ErrSkip, nil},
		{"a string", "s", driver.ErrSkip, nil},
		{"a vector", dbimp.Vector[float64]{1}, driver.ErrSkip, nil},
	} {
		nv := &driver.NamedValue{Value: tt.in}
		err := c.CheckNamedValue(nv)
		if err != nil && !errors.Is(err, tt.want) || err == nil && tt.want != nil {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
		if tt.out != nil && nv.Value != tt.out {
			t.Errorf("%s: the value is %v, want %v", tt.name, nv.Value, tt.out)
		}
	}
	nv := &driver.NamedValue{Value: (*apd.Decimal)(nil)}
	if err := c.CheckNamedValue(nv); err != nil || nv.Value != nil {
		t.Errorf("a nil decimal became %v, %v, want nil", nv.Value, err)
	}
}
