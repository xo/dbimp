package databricks //nolint:testpackage // The tests read the parameters that the driver builds, which are not exported.

import (
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

// wire writes a parameter as the body of the request holds it.
func wire(t *testing.T, p param) string {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestBind holds D193 item 9: each Go type is the parameter of the type that the
// server kept in the recordings, and the text of its value.
func TestBind(t *testing.T) {
	t.Parallel()
	tm := time.Date(2024, time.January, 2, 10, 4, 5, 123456789, time.FixedZone("", 7*3600))
	for _, tt := range []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, `{"value":null,"type":"VOID"}`},
		{"int64", int64(-42), `{"value":"-42","type":"BIGINT"}`},
		{"the largest int64", int64(math.MaxInt64), `{"value":"9223372036854775807","type":"BIGINT"}`},
		{"float64", 1.5, `{"value":"1.5","type":"DOUBLE"}`},
		{"a float64 with an exponent", 1e300, `{"value":"1e+300","type":"DOUBLE"}`},
		{"NaN", math.NaN(), `{"value":"NaN","type":"DOUBLE"}`},
		{"Infinity", math.Inf(1), `{"value":"Infinity","type":"DOUBLE"}`},
		{"-Infinity", math.Inf(-1), `{"value":"-Infinity","type":"DOUBLE"}`},
		{"string", "héllo", `{"value":"héllo","type":"STRING"}`},
		{"an empty string", "", `{"value":"","type":"STRING"}`},
		{"true", true, `{"value":"true","type":"BOOLEAN"}`},
		{"false", false, `{"value":"false","type":"BOOLEAN"}`},
		{"dbimp.Date", dbimp.Date{Year: 2024, Month: time.January, Day: 2}, `{"value":"2024-01-02","type":"DATE"}`},
		{"the first date", dbimp.Date{Year: 1, Month: time.January, Day: 1}, `{"value":"0001-01-01","type":"DATE"}`},
		{"time.Time in another zone, cut to microseconds", tm, `{"value":"2024-01-02 03:04:05.123456","type":"TIMESTAMP"}`},
		{"time.Time of whole seconds", time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC), `{"value":"2024-01-02 03:04:05","type":"TIMESTAMP"}`},
		{"dbimp.LocalDateTime", local(2024, time.January, 2, 3, 4, 5, 123456000), `{"value":"2024-01-02 03:04:05.123456","type":"TIMESTAMP_NTZ"}`},
		{"a decimal with a scale", dec(t, "1.50"), `{"value":"1.50","type":"DECIMAL(3,2)"}`},
		{"a decimal of 38 digits", dec(t, "12345678901234567890.123456789012345678"), `{"value":"12345678901234567890.123456789012345678","type":"DECIMAL(38,18)"}`},
		{"a decimal of zeros after the point", dec(t, "0.00000"), `{"value":"0.00000","type":"DECIMAL(5,5)"}`},
		{"a decimal with a positive exponent", dec(t, "1E+3"), `{"value":"1000","type":"DECIMAL(4,0)"}`},
		{"a negative decimal", dec(t, "-0.5"), `{"value":"-0.5","type":"DECIMAL(1,1)"}`},
		{"a nil decimal", (*apd.Decimal)(nil), `{"value":null,"type":"VOID"}`},
		{"months", dbimp.Interval{Months: 14}, `{"value":"1-2","type":"INTERVAL YEAR TO MONTH"}`},
		{"negative months", dbimp.Interval{Months: -14}, `{"value":"-1-2","type":"INTERVAL YEAR TO MONTH"}`},
		{"months under a year", dbimp.Interval{Months: 3}, `{"value":"0-3","type":"INTERVAL YEAR TO MONTH"}`},
		{"days and time", dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute + 4*time.Second)}, `{"value":"1 02:03:04","type":"INTERVAL DAY TO SECOND"}`},
		{"a fraction of microseconds", dbimp.Interval{Nanoseconds: int64(1500 * time.Millisecond)}, `{"value":"0 00:00:01.5","type":"INTERVAL DAY TO SECOND"}`},
		{"a negative interval", dbimp.Interval{Days: -1, Nanoseconds: -int64(2*time.Hour + 3*time.Minute + 4*time.Second + 500*time.Millisecond)}, `{"value":"-1 02:03:04.5","type":"INTERVAL DAY TO SECOND"}`},
		{"days and time of two signs", dbimp.Interval{Days: 1, Nanoseconds: -int64(time.Hour)}, `{"value":"0 23:00:00","type":"INTERVAL DAY TO SECOND"}`},
		{"a time of more than a day", dbimp.Interval{Nanoseconds: int64(25 * time.Hour)}, `{"value":"1 01:00:00","type":"INTERVAL DAY TO SECOND"}`},
		{"the largest day to second interval", dbimp.Interval{Days: 106751991, Nanoseconds: int64(4*time.Hour + 54*time.Second + 775807*time.Microsecond)}, `{"value":"106751991 04:00:54.775807","type":"INTERVAL DAY TO SECOND"}`},
		{"the smallest day to second interval", dbimp.Interval{Days: -106751991, Nanoseconds: -int64(4*time.Hour + 54*time.Second + 775808*time.Microsecond)}, `{"value":"-106751991 04:00:54.775808","type":"INTERVAL DAY TO SECOND"}`},
		{"the smallest month interval", dbimp.Interval{Months: math.MinInt32}, `{"value":"-178956970-8","type":"INTERVAL YEAR TO MONTH"}`},
		{"an interval of zero", dbimp.Interval{}, `{"value":"0 00:00:00","type":"INTERVAL DAY TO SECOND"}`},
	} {
		p, err := bind(tt.in)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got := wire(t, p); got != tt.want {
			t.Errorf("%s: the parameter is %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestBindRefuses holds D193 item 9: a Go type that the server refuses as a
// parameter fails with dbimp.ErrArguments, with no request, and a value that
// the server cannot hold fails with dbimp.ErrInvalidValue.
func TestBindRefuses(t *testing.T) {
	t.Parallel()
	huge := apd.New(1, 0)
	huge.Exponent = -39
	nan := &apd.Decimal{Form: apd.NaN}
	for _, tt := range []struct {
		name string
		in   any
		want error
	}{
		{"a byte slice", []byte{1}, dbimp.ErrArguments},
		{"a list", []any{1}, dbimp.ErrArguments},
		{"a map", map[string]any{"a": 1}, dbimp.ErrArguments},
		{"a struct", struct{}{}, dbimp.ErrArguments},
		{"months and days", dbimp.Interval{Months: 1, Days: 1}, dbimp.ErrArguments},
		{"months and time", dbimp.Interval{Months: 1, Nanoseconds: 1000}, dbimp.ErrArguments},
		{"nanoseconds of an interval", dbimp.Interval{Nanoseconds: 1}, dbimp.ErrInvalidValue},
		{"a decimal of 39 digits", huge, dbimp.ErrInvalidValue},
		{"a decimal that is not a number", nan, dbimp.ErrInvalidValue},
		{"a date that is not a day", dbimp.Date{Year: 2024, Month: time.February, Day: 30}, dbimp.ErrInvalidValue},
		{"a date after the year 9999", dbimp.Date{Year: 10000, Month: time.January, Day: 1}, dbimp.ErrInvalidValue},
		{"a time after the year 9999", time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC), dbimp.ErrInvalidValue},
		{"a time before the year 1", time.Date(0, time.December, 31, 0, 0, 0, 0, time.UTC), dbimp.ErrInvalidValue},
		{"a local time after the year 9999", local(10000, time.January, 1, 0, 0, 0, 0), dbimp.ErrInvalidValue},
	} {
		_, err := bind(tt.in)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want %v", tt.name, err, tt.want)
		}
	}
}

// TestBindArgs holds that a named argument has its name in the parameter, a
// numbered argument has none, and the parameters keep the order of the
// arguments.
func TestBindArgs(t *testing.T) {
	t.Parallel()
	got, err := bindArgs([]driver.NamedValue{
		{Ordinal: 1, Value: int64(1)},
		{Ordinal: 2, Name: "b", Value: "x"},
		{Ordinal: 3, Value: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"value":"1","type":"BIGINT"},{"name":"b","value":"x","type":"STRING"},{"value":null,"type":"VOID"}]`
	if string(b) != want {
		t.Errorf("the parameters are %s, want %s", b, want)
	}
	if got, err := bindArgs(nil); got != nil || err != nil {
		t.Errorf("no argument gave %v and %v, want none", got, err)
	}
	_, err = bindArgs([]driver.NamedValue{{Ordinal: 1, Value: int64(1)}, {Ordinal: 2, Value: []byte{1}}})
	if !errors.Is(err, dbimp.ErrArguments) || !strings.Contains(err.Error(), "argument 2") {
		t.Errorf("a byte slice as the second argument gave %v, want an error that names the argument 2", err)
	}
}

// nilValuer is a driver.Valuer that a nil pointer of it must not call.
type nilValuer struct{}

func (*nilValuer) Value() (driver.Value, error) { return "x", nil }

// TestCheckNamedValue holds that the connection keeps an Option, the values that
// the driver binds with a type of their own, and hands every other value to the
// converter of database/sql.
func TestCheckNamedValue(t *testing.T) {
	t.Parallel()
	c := &conn{}
	d := dec(t, "1.5")
	for _, tt := range []struct {
		name string
		in   any
		want any
		skip bool
	}{
		{"an option", WithTimeout(time.Second), nil, false},
		{"a decimal", d, d, false},
		{"a nil decimal", (*apd.Decimal)(nil), nil, false},
		{"a date", dbimp.Date{Year: 2024, Month: time.January, Day: 2}, dbimp.Date{Year: 2024, Month: time.January, Day: 2}, false},
		{"a local date and time", local(2024, time.January, 2, 0, 0, 0, 0), local(2024, time.January, 2, 0, 0, 0, 0), false},
		{"an interval", dbimp.Interval{Months: 1}, dbimp.Interval{Months: 1}, false},
		{"a nil valuer", (*nilValuer)(nil), nil, false},
		{"an int", 5, 5, true},
		{"a string", "x", "x", true},
		{"a time", time.Time{}, time.Time{}, true},
		{"a byte slice", []byte{1}, []byte{1}, true},
	} {
		nv := &driver.NamedValue{Value: tt.in}
		err := c.CheckNamedValue(nv)
		if tt.skip {
			if !errors.Is(err, driver.ErrSkip) {
				t.Errorf("%s: the error is %v, want driver.ErrSkip", tt.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if tt.want != nil && !equal(nv.Value, tt.want) {
			t.Errorf("%s: the value is %#v, want %#v", tt.name, nv.Value, tt.want)
		}
		if tt.name == "a nil valuer" && nv.Value != nil {
			t.Errorf("a nil valuer became %#v, want nil", nv.Value)
		}
	}
	var applied apd.Decimal
	applied.SetInt64(7)
	nv := &driver.NamedValue{Value: applied}
	if err := c.CheckNamedValue(nv); err != nil {
		t.Fatal(err)
	}
	if p, ok := nv.Value.(*apd.Decimal); !ok || p.String() != "7" {
		t.Errorf("a decimal value became %#v, want a pointer", nv.Value)
	}
}
