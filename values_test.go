package dbimp_test

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

func TestInt64IsExact(t *testing.T) {
	t.Parallel()
	// 2^53 + 1 is the first integer that a float64 cannot hold (D19).
	i, err := dbimp.Int64(jsontext.Value("9007199254740993"))
	if err != nil || i != 9007199254740993 {
		t.Errorf("Int64 = %d, %v, want 9007199254740993", i, err)
	}
	if _, err := dbimp.Int64(jsontext.Value("1.5")); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("Int64 of 1.5 = %v, want ErrInvalidValue", err)
	}
}

func TestNumber(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want any
	}{
		{"42", int64(42)},
		{"-9007199254740993", int64(-9007199254740993)},
		{"1.5", 1.5},
		{"1e3", 1000.0},
	} {
		got, err := dbimp.Number(jsontext.Value(tt.in))
		if err != nil || got != tt.want {
			t.Errorf("Number(%s) = %v (%T), %v, want %v (%T)", tt.in, got, got, err, tt.want, tt.want)
		}
	}
	big := "123456789012345678901234567890"
	got, err := dbimp.Number(jsontext.Value(big))
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := got.(*apd.Decimal); !ok || d.String() != big {
		t.Errorf("Number(%s) = %v (%T), want an exact *apd.Decimal", big, got, got)
	}
}

func TestDecimal(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`12345678901234567890123456789012345678`, `"0.10"`} {
		d, err := dbimp.Decimal(jsontext.Value(in))
		if err != nil {
			t.Errorf("Decimal(%s): %v", in, err)
			continue
		}
		if want := string(jsontext.Value(in)); d.String() != want && `"`+d.String()+`"` != want {
			t.Errorf("Decimal(%s) = %s", in, d)
		}
	}
}

func TestPadScale(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in    string
		scale int64
		want  string
	}{
		{"1.5", 2, "1.50"},
		{"1.50", 2, "1.50"},
		{"1.505", 2, "1.505"},
		{"0", 4, "0.0000"},
		{"-12", 3, "-12.000"},
		{"1E+3", 2, "1000.00"},
		{"0.1", 0, "0.1"},
		{"0.1", -1, "0.1"},
		{"123456789012345678901234567890.5", 3, "123456789012345678901234567890.500"},
	} {
		d, _, err := apd.NewFromString(tt.in)
		if err != nil {
			t.Fatal(err)
		}
		before := d.String()
		got := dbimp.PadScale(d, tt.scale)
		if got.String() != tt.want {
			t.Errorf("PadScale(%s, %d) = %s, want %s", tt.in, tt.scale, got, tt.want)
		}
		if d.String() != before {
			t.Errorf("PadScale(%s, %d) changed its argument to %s", tt.in, tt.scale, d)
		}
	}
	for _, in := range []string{"NaN", "Infinity", "-Infinity"} {
		d, _, err := apd.NewFromString(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := dbimp.PadScale(d, 2); got != d {
			t.Errorf("PadScale(%s, 2) = %s, want the same value", in, got)
		}
	}
	if got := dbimp.PadScale(nil, 2); got != nil {
		t.Errorf("PadScale(nil, 2) = %v, want nil", got)
	}
}

func TestVerbose(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		code     any
		message  string
		position string
		pairs    []any
		want     string
	}{
		{"code and fields", 60, "Table t does not exist.", "", []any{"DETAIL", "UNKNOWN_TABLE", "HTTP STATUS", 404, "EMPTY", "", "ZERO", 0, "WAIT", 2 * time.Second, "FLAG", true, "OFF", false},
			"ERROR:  60: Table t does not exist.\nDETAIL:  UNKNOWN_TABLE\nHTTP STATUS:  404\nWAIT:  2s\nFLAG:  true"},
		{"string code", "42P01", "no table", "", nil, "ERROR:  42P01: no table"},
		{"no code", "", "no table", "", nil, "ERROR:  no table"},
		{"zero code", 0, "no table", "", nil, "ERROR:  no table"},
		{"position", 1, "bad", "LINE 1: select\n        ^", nil, "ERROR:  1: bad\nLINE 1: select\n        ^"},
		{"fields only", nil, "", "", []any{"HTTP STATUS", 400, "STATUS", "fatal"}, "HTTP STATUS:  400\nSTATUS:  fatal"},
		{"nothing", nil, "", "", []any{"CODE", 0}, ""},
	} {
		if got := dbimp.Verbose(tt.code, tt.message, tt.position, tt.pairs...); got != tt.want {
			t.Errorf("%s:\n got: %q\nwant: %q", tt.name, got, tt.want)
		}
	}
}

func TestPosition(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query        string
		line, column int
		want         string
	}{
		{"select * from nope;", 1, 15, "LINE 1: select * from nope;\n                      ^"},
		{"select a,\n  é, b from t", 2, 6, "LINE 2:   é, b from t\n             ^"},
		{"select a\r\nfrom", 1, 8, "LINE 1: select a\n               ^"},
		{"select 1", 1, 0, "LINE 1: select 1"},
		{"select 1", 2, 1, ""},
		{"select 1", 0, 1, ""},
	} {
		if got := dbimp.Position(tt.query, tt.line, tt.column); got != tt.want {
			t.Errorf("Position(%q, %d, %d):\n got: %q\nwant: %q", tt.query, tt.line, tt.column, got, tt.want)
		}
	}
}

func TestStringAndBool(t *testing.T) {
	t.Parallel()
	if s, err := dbimp.String(jsontext.Value(`"a\"bé"`)); err != nil || s != `a"bé` {
		t.Errorf("String = %q, %v", s, err)
	}
	if b, err := dbimp.Bool(jsontext.Value(`true`)); err != nil || !b {
		t.Errorf("Bool = %v, %v", b, err)
	}
	if !dbimp.IsNull(nil) || !dbimp.IsNull(jsontext.Value("null")) || dbimp.IsNull(jsontext.Value("0")) {
		t.Error("IsNull is wrong for a missing value, null or 0")
	}
}

func TestAny(t *testing.T) {
	t.Parallel()
	got, err := dbimp.Any(jsontext.Value(`{"a":[1,"x",null,true],"b":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": []any{int64(1), "x", nil, true}, "b": int64(9007199254740993)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Any = %#v, want %#v", got, want)
	}
}

// composer is a destination that takes a decimal in parts, as the interface
// decimalCompose of database/sql describes it.
type composer struct{ d apd.Decimal }

func (c *composer) Compose(form byte, negative bool, coefficient []byte, exponent int32) error {
	return c.d.Compose(form, negative, coefficient, exponent)
}

func TestAssignDecimalToCompose(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"1.50", "-0.0010", "123456789012345678901234567890", "NaN", "Infinity"} {
		src, _, err := apd.NewFromString(in)
		if err != nil {
			t.Fatal(err)
		}
		var got composer
		if err := dbimp.Assign(driver.ScanContext{}, &got, src); err != nil {
			t.Errorf("Assign(%s) to a destination with Compose: %v", in, err)
			continue
		}
		if got.d.String() != src.String() {
			t.Errorf("Assign(%s) to a destination with Compose = %s", in, got.d.String())
		}
	}
}

func TestAssign(t *testing.T) {
	t.Parallel()
	d, _, err := apd.NewFromString("1.25")
	if err != nil {
		t.Fatal(err)
	}
	var dec apd.Decimal
	if err := dbimp.Assign(driver.ScanContext{}, &dec, d); err != nil || dec.String() != "1.25" {
		t.Errorf("Assign to *apd.Decimal = %s, %v", dec.String(), err)
	}
	var null sql.Null[apd.Decimal]
	if err := dbimp.Assign(driver.ScanContext{}, &null, d); err != nil || !null.Valid || null.V.String() != "1.25" {
		t.Errorf("Assign to *sql.Null[apd.Decimal] = %+v, %v", null, err)
	}
	var s string
	if err := dbimp.Assign(driver.ScanContext{}, &s, d); err != nil || s != "1.25" {
		t.Errorf("Assign to *string = %q, %v", s, err)
	}
	var a any
	if err := dbimp.Assign(driver.ScanContext{}, &a, d); err != nil || a != d {
		t.Errorf("Assign to *any = %v, %v", a, err)
	}
	if err := dbimp.Assign(driver.ScanContext{}, &s, nil); err == nil {
		t.Error("Assign of NULL to *string returned no error (D8)")
	}
	var ns sql.Null[string]
	if err := dbimp.Assign(driver.ScanContext{}, &ns, nil); err != nil || ns.Valid {
		t.Errorf("Assign of NULL to *sql.Null[string] = %+v, %v", ns, err)
	}
}
