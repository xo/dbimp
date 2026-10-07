package dynamodb //nolint:testpackage // The test reads the encoding of an argument, which the package does not export.

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

// dec returns the decimal of text.
func dec(t *testing.T, text string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestAttribute holds the typed value of each Go type of an argument. The
// body of "a parameter of each type" is a recording (D169).
func TestAttribute(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, `{"NULL":true}`},
		{"string", "é'\"\\ x", `{"S":"é'\"\\ x"}`},
		{"empty string", "", `{"S":""}`},
		{"true", true, `{"BOOL":true}`},
		{"false", false, `{"BOOL":false}`},
		{"bytes", []byte{0, 0xff}, `{"B":"AP8="}`},
		{"empty bytes", []byte{}, `{"B":""}`},
		{"int64", int64(-42), `{"N":"-42"}`},
		{"int", 42, `{"N":"42"}`},
		{"uint64", uint64(math.MaxUint64), `{"N":"18446744073709551615"}`},
		{"float64", 1.5, `{"N":"1.5"}`},
		{"float32", float32(0.25), `{"N":"0.25"}`},
		{"decimal", dec(t, "1.50"), `{"N":"1.50"}`},
		{"decimal of 38 digits", dec(t, "12345678901234567890123456789012345678"), `{"N":"12345678901234567890123456789012345678"}`},
		{"decimal with an exponent", dec(t, "1E-130"), `{"N":"0.` + zeros(129) + `1"}`},
		{"nil decimal", (*apd.Decimal)(nil), `{"NULL":true}`},
		{"list", []any{"a", int64(1), nil, []any{}}, `{"L":[{"S":"a"},{"N":"1"},{"NULL":true},{"L":[]}]}`},
		{"map", map[string]any{"k": int64(1)}, `{"M":{"k":{"N":"1"}}}`},
		{"empty map", map[string]any{}, `{"M":{}}`},
		{"string set", Set{"a", "b"}, `{"SS":["a","b"]}`},
		{"number set", Set{dec(t, "1"), int64(2), 2.5}, `{"NS":["1","2","2.5"]}`},
		{"binary set", Set{[]byte{1}}, `{"BS":["AQ=="]}`},
	} {
		got, err := attribute(tt.in)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		b, err := json.Marshal(got, json.Deterministic(true))
		if err != nil || string(b) != tt.want {
			t.Errorf("%s: attribute = %s, %v, want %s", tt.name, b, err, tt.want)
		}
	}
}

// zeros returns n zeros.
func zeros(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

// TestAttributeRefuses holds that a value with no type in DynamoDB is an
// error, and never a guess.
func TestAttributeRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   any
		want error
	}{
		{"a time", time.Now(), dbimp.ErrNotSupported},
		{"a date", dbimp.Date{Year: 2026, Month: 10, Day: 1}, dbimp.ErrNotSupported},
		{"NaN", math.NaN(), dbimp.ErrInvalidValue},
		{"infinity", math.Inf(1), dbimp.ErrInvalidValue},
		{"a decimal that is NaN", &apd.Decimal{Form: apd.NaN}, dbimp.ErrInvalidValue},
		{"an empty set", Set{}, dbimp.ErrInvalidValue},
		{"a set of two kinds", Set{"a", int64(1)}, dbimp.ErrInvalidValue},
		{"a set of two kinds, numbers first", Set{int64(1), "a"}, dbimp.ErrInvalidValue},
		{"a set of booleans", Set{true}, dbimp.ErrInvalidValue},
		{"a set of lists", Set{[]any{}}, dbimp.ErrInvalidValue},
		{"a list with a time", []any{time.Now()}, dbimp.ErrNotSupported},
		{"a map with a time", map[string]any{"k": time.Now()}, dbimp.ErrNotSupported},
		{"a struct", struct{}{}, dbimp.ErrNotSupported},
	} {
		if _, err := attribute(tt.in); !errors.Is(err, tt.want) {
			t.Errorf("%s: attribute = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// TestParameters holds D169: the driver refuses a count of arguments that is
// not the count of the placeholders, because the server takes too many with
// no error (recorded: "too many parameters"), and a named argument.
func TestParameters(t *testing.T) {
	t.Parallel()
	arg := func(ordinal int, v any) driver.NamedValue { return driver.NamedValue{Ordinal: ordinal, Value: v} }
	got, err := parameters("SELECT pk FROM t WHERE pk = ? AND n = ?", []driver.NamedValue{arg(1, "t1"), arg(2, int64(42))})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if want := `[{"S":"t1"},{"N":"42"}]`; string(b) != want {
		t.Errorf("parameters = %s, want %s (recorded: \"positional parameters\")", b, want)
	}
	if got, err := parameters("SELECT pk FROM t WHERE pk = '?'", nil); err != nil || got != nil {
		t.Errorf("parameters of a question mark in a string = %v, %v, want none", got, err)
	}
	for _, tt := range []struct {
		name  string
		query string
		args  []driver.NamedValue
		want  error
	}{
		{"too few", "SELECT pk FROM t WHERE pk = ? AND n = ?", []driver.NamedValue{arg(1, "t1")}, dbimp.ErrArguments},
		{"too many", "SELECT pk FROM t WHERE pk = ?", []driver.NamedValue{arg(1, "t1"), arg(2, int64(42))}, dbimp.ErrArguments},
		{"arguments for no placeholder", "SELECT pk FROM t", []driver.NamedValue{arg(1, "t1")}, dbimp.ErrArguments},
		{"a named argument", "SELECT pk FROM t WHERE pk = ?", []driver.NamedValue{{Name: "p", Ordinal: 1, Value: "t1"}}, dbimp.ErrNotSupported},
		{"a value with no type", "SELECT pk FROM t WHERE pk = ?", []driver.NamedValue{arg(1, time.Now())}, dbimp.ErrNotSupported},
		{"an unterminated literal", "SELECT pk FROM t WHERE pk = 'x", nil, dbimp.ErrUnterminated},
	} {
		if _, err := parameters(tt.query, tt.args); !errors.Is(err, tt.want) {
			t.Errorf("%s: parameters = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// TestCheckNamedValue holds which values the driver keeps, and which it hands
// to the converter of database/sql (W4).
func TestCheckNamedValue(t *testing.T) {
	t.Parallel()
	c := &conn{}
	for _, v := range []any{Set{"a"}, []any{}, map[string]any{}, dec(t, "1"), WithReadonly(false)} {
		nv := driver.NamedValue{Value: v}
		if err := c.CheckNamedValue(&nv); err != nil {
			t.Errorf("CheckNamedValue(%T) = %v, want nil", v, err)
		}
	}
	for _, v := range []any{"s", int64(1), 1.5, true, []byte("b"), time.Now(), 7} {
		nv := driver.NamedValue{Value: v}
		if err := c.CheckNamedValue(&nv); !errors.Is(err, driver.ErrSkip) {
			t.Errorf("CheckNamedValue(%T) = %v, want driver.ErrSkip", v, err)
		}
	}
	nv := driver.NamedValue{Value: apd.Decimal{}}
	if err := c.CheckNamedValue(&nv); err != nil {
		t.Fatal(err)
	}
	if _, ok := nv.Value.(*apd.Decimal); !ok {
		t.Errorf("CheckNamedValue of an apd.Decimal left a %T, want *apd.Decimal", nv.Value)
	}
	nv = driver.NamedValue{Value: (*apd.Decimal)(nil)}
	if err := c.CheckNamedValue(&nv); err != nil || nv.Value != nil {
		t.Errorf("CheckNamedValue of a nil decimal = %v, %v, want nil", nv.Value, err)
	}
}
