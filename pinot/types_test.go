package pinot //nolint:testpackage // The literals and the decoder are not exported.

import (
	"encoding/json/jsontext"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// TestLiteral holds D132 for each Go type of an argument.
func TestLiteral(t *testing.T) {
	t.Parallel()
	d, _, _ := apd.NewFromString("-12345678901234567890.0123456789")
	for _, tt := range []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{true, "true"},
		{false, "false"},
		{int64(math.MinInt64), "-9223372036854775808"},
		{uint64(math.MaxUint64), "18446744073709551615"},
		{0.1, "0.1"},
		{1e21, "1e+21"},
		{"", "''"},
		{"it's '' ? -- /*", "'it''s '''' ? -- /*'"},
		{d, "CAST('-12345678901234567890.0123456789' AS BIG_DECIMAL)"},
		{time.Date(2023, 11, 15, 3, 43, 20, 123456789, time.FixedZone("x", 5*3600)), "1700001800123"},
		{[]byte{0x00, 0xff}, "hexToBytes('00ff')"},
		{[]byte{}, "hexToBytes('')"},
	} {
		got, err := literal(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("literal(%#v) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
	nan, _, _ := apd.NewFromString("NaN")
	for _, in := range []any{math.NaN(), math.Inf(1), math.Inf(-1), nan} {
		if _, err := literal(in); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("literal(%v) gave %v, want dbimp.ErrInvalidValue, because Pinot has no literal for it", in, err)
		}
	}
	if _, err := literal(int32(1)); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("literal(int32) gave %v, want dbimp.ErrNotSupported", err)
	}
}

// TestDecode holds D130 for the forms that the recordings hold, and for the
// values that the driver refuses.
func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		typ, in string
		want    any
	}{
		{"TIMESTAMP", `"2022-04-15 05:20:00.0"`, time.Date(2022, 4, 15, 5, 20, 0, 0, time.UTC)},
		{"TIMESTAMP", `"2023-11-14 22:13:20.123"`, time.Date(2023, 11, 14, 22, 13, 20, 123e6, time.UTC)},
		{"DOUBLE", `"-Infinity"`, math.Inf(-1)},
		{"JSON", `"null"`, nil},
		{"JSON", `"{\"a\":[1,1.5]}"`, map[string]any{"a": []any{int64(1), 1.5}}},
		{"BYTES", `""`, []byte{}},
		{"TIMESTAMP_ARRAY", `["1970-01-01 00:00:00.0"]`, []any{time.Unix(0, 0).UTC()}},
		{"INT_ARRAY", `[]`, []any{}},
		{"UNKNOWN", `null`, nil},
		{"OBJECT", `{"x":1}`, map[string]any{"x": int64(1)}},
	} {
		got, err := decode(tt.typ, jsontext.Value(tt.in))
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%s, %s) = %#v, %v, want %#v", tt.typ, tt.in, got, err, tt.want)
		}
	}
	for _, tt := range []struct{ typ, in string }{
		{"INT", `1.5`},
		{"LONG", `"1"`},
		{"DOUBLE", `"infinity"`},
		{"BYTES", `"0g"`},
		{"TIMESTAMP", `"2022-04-15T05:20:00Z"`},
		{"TIMESTAMP", `1700000000123`},
		{"INT_ARRAY", `1`},
		{"INT_ARRAY", `[1.5]`},
		{"BOOLEAN", `1`},
		{"JSON", `"{"`},
	} {
		if got, err := decode(tt.typ, jsontext.Value(tt.in)); err == nil {
			t.Errorf("decode(%s, %s) = %#v, want an error", tt.typ, tt.in, got)
		}
	}
}
