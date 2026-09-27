package dbimp_test

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"reflect"
	"testing"

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
