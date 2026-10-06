package druid //nolint:testpackage // The decoder and the parameters are not exported.

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// TestDecode holds D164 for the forms that the recordings hold, and for the
// values that the driver refuses.
func TestDecode(t *testing.T) {
	t.Parallel()
	jakarta := time.FixedZone("", 7*3600)
	for _, tt := range []struct {
		sql, native, in string
		want            any
	}{
		{"BIGINT", "LONG", `-9223372036854775808`, int64(math.MinInt64)},
		{"INTEGER", "LONG", `3`, int64(3)},
		{"FLOAT", "FLOAT", `3.4E38`, 3.4e38},
		{"REAL", "DOUBLE", `3.3999999521443642E38`, 3.3999999521443642e38},
		{"DOUBLE", "DOUBLE", `"-Infinity"`, math.Inf(-1)},
		{"DECIMAL", "DOUBLE", `1.7976931348623157E308`, math.MaxFloat64},
		{"BOOLEAN", "LONG", `true`, true},
		{"VARCHAR", "STRING", `""`, ""},
		{"VARCHAR", "STRING", `"z"`, "z"},
		{"VARCHAR", "STRING", `"[\"x\",\"y\"]"`, []any{"x", "y"}},
		{"VARCHAR", "STRING", `"[\"x\",null]"`, []any{"x", nil}},
		// One value is the string, so text that is not an array of two or
		// more strings stays a string.
		{"VARCHAR", "STRING", `"[\"x\"]"`, `["x"]`},
		{"VARCHAR", "STRING", `"[1,2]"`, "[1,2]"},
		{"VARCHAR", "STRING", `"[\"x\",\"y\"] "`, `["x","y"] `},
		{"VARCHAR", "STRING", `"[\"x\",\"y\"]]"`, `["x","y"]]`},
		{"CHAR", "STRING", `"x"`, "x"},
		{"TIMESTAMP", "LONG", `"2026-10-01T12:34:56.789Z"`, time.Date(2026, 10, 1, 12, 34, 56, 789e6, time.UTC)},
		{"TIMESTAMP", "LONG", `"2026-10-01T19:34:56.789+07:00"`, time.Date(2026, 10, 1, 19, 34, 56, 789e6, jakarta)},
		{"TIMESTAMP", "LONG", `"0001-01-01T00:00:00.000Z"`, time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"DATE", "LONG", `"2026-10-01T00:00:00.000+07:00"`, dbimp.Date{Year: 2026, Month: 10, Day: 1}},
		{"ARRAY", "ARRAY<STRING>", `"[\"a\",null,\"c\"]"`, []any{"a", nil, "c"}},
		{"ARRAY", "ARRAY<STRING>", `["a",null,"c"]`, []any{"a", nil, "c"}},
		{"ARRAY", "ARRAY<LONG>", `"[-9223372036854775808,9223372036854775807]"`, []any{int64(math.MinInt64), int64(math.MaxInt64)}},
		{"ARRAY", "ARRAY<DOUBLE>", `"[0.1,1.5E300]"`, []any{0.1, 1.5e300}},
		{"ARRAY", "ARRAY<ARRAY<LONG>>", `[[1],[]]`, []any{[]any{int64(1)}, []any{}}},
		{"ARRAY", "ARRAY<STRING>", `"[]"`, []any{}},
		{"OTHER", "COMPLEX<json>", `"{\"k\":[1,\"two\",null,{\"n\":1.5}]}"`, map[string]any{"k": []any{int64(1), "two", nil, map[string]any{"n": 1.5}}}},
		{"OTHER", "COMPLEX<json>", `"[]"`, []any{}},
		{"OTHER", "COMPLEX<json>", `"\"s\""`, "s"},
		{"OTHER", "COMPLEX<HLLSketch>", `"\"AgEHDAMIAQD2fr0F\""`, []byte{2, 1, 7, 12, 3, 8, 1, 0, 246, 126, 189, 5}},
		{"OTHER", "COMPLEX<thetaSketch>", `"\"AQMDAAA6zJM=\""`, []byte{1, 3, 3, 0, 0, 58, 204, 147}},
		{"OTHER", "COMPLEX<other>", `"{\"a\":1}"`, map[string]any{"a": int64(1)}},
		{"NULL", "STRING", `null`, nil},
		{"BIGINT", "LONG", `null`, nil},
		{"SYMBOL", "STRING", `{"x":1}`, map[string]any{"x": int64(1)}},
	} {
		got, err := decode(column{sql: tt.sql, native: tt.native}, jsontext.Value(tt.in))
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%s %s, %s) = %#v, %v, want %#v", tt.sql, tt.native, tt.in, got, err, tt.want)
		}
	}
	nan, err := decode(column{sql: "DOUBLE"}, jsontext.Value(`"NaN"`))
	if f, ok := nan.(float64); err != nil || !ok || !math.IsNaN(f) {
		t.Errorf("decode of \"NaN\" = %v, %v, want NaN", nan, err)
	}
	for _, tt := range []struct{ sql, native, in string }{
		{"BIGINT", "LONG", `1.5`},
		{"BIGINT", "LONG", `"1"`},
		{"DOUBLE", "DOUBLE", `"infinity"`},
		{"BOOLEAN", "LONG", `1`},
		{"VARCHAR", "STRING", `1`},
		{"TIMESTAMP", "LONG", `"2026-10-01 12:34:56"`},
		{"TIMESTAMP", "LONG", `1790858096789`},
		{"DATE", "LONG", `"2026-10-01"`},
		{"ARRAY", "ARRAY<LONG>", `"[1.5]"`},
		{"ARRAY", "ARRAY<LONG>", `1`},
		{"ARRAY", "ARRAY<LONG>", `"["`},
		{"OTHER", "COMPLEX<json>", `"{"`},
		{"OTHER", "COMPLEX<HLLSketch>", `"AgEH"`},
		{"OTHER", "COMPLEX<HLLSketch>", `"\"!!\""`},
	} {
		if got, err := decode(column{sql: tt.sql, native: tt.native}, jsontext.Value(tt.in)); err == nil {
			t.Errorf("decode(%s %s, %s) = %#v, want an error", tt.sql, tt.native, tt.in, got)
		}
	}
}

// TestBind holds D164 for each Go type of an argument: the type and the
// value of the parameter that the driver sends.
func TestBind(t *testing.T) {
	t.Parallel()
	d, _, _ := apd.NewFromString("-12345678901234567890.0123456789")
	big, _, _ := apd.NewFromString("1E+21")
	for _, tt := range []struct {
		in   any
		want string
	}{
		{nil, `{"type":"VARCHAR","value":null}`},
		{true, `{"type":"BOOLEAN","value":true}`},
		{int64(math.MinInt64), `{"type":"BIGINT","value":-9223372036854775808}`},
		{0.1, `{"type":"DOUBLE","value":0.1}`},
		{"é'x", `{"type":"VARCHAR","value":"é'x"}`},
		{time.Date(2026, 10, 1, 19, 34, 56, 789123456, time.FixedZone("x", 7*3600)), `{"type":"TIMESTAMP","value":1790858096789}`},
		{dbimp.Date{Year: 2026, Month: 10, Day: 1}, `{"type":"DATE","value":"2026-10-01"}`},
		{dbimp.LocalDateTimeOf(time.Date(2026, 10, 1, 12, 34, 56, 789e6, time.UTC)), `{"type":"TIMESTAMP","value":"2026-10-01 12:34:56.789"}`},
		{d, `{"type":"DECIMAL","value":-12345678901234567890.0123456789}`},
		{big, `{"type":"DECIMAL","value":1E+21}`},
		{[]any{"a", nil}, `{"type":"ARRAY","value":["a",null]}`},
		{[]string{"a", "b"}, `{"type":"ARRAY","value":["a","b"]}`},
		{[]int64{1, 2}, `{"type":"ARRAY","value":[1,2]}`},
	} {
		p, err := bind(tt.in)
		if err != nil {
			t.Errorf("bind(%#v): %v", tt.in, err)
			continue
		}
		got, err := json.Marshal(p)
		if err != nil || string(got) != tt.want {
			t.Errorf("bind(%#v) = %s, %v, want %s", tt.in, got, err, tt.want)
		}
	}
	nan, _, _ := apd.NewFromString("NaN")
	for _, tt := range []struct {
		in   any
		want error
	}{
		{math.NaN(), dbimp.ErrInvalidValue},
		{math.Inf(1), dbimp.ErrInvalidValue},
		{nan, dbimp.ErrInvalidValue},
		{[]byte{1}, dbimp.ErrNotSupported},
		{int32(1), dbimp.ErrNotSupported},
	} {
		if _, err := bind(tt.in); !errors.Is(err, tt.want) {
			t.Errorf("bind(%#v) gave %v, want %v", tt.in, err, tt.want)
		}
	}
	if _, err := parameters([]driver.NamedValue{{Name: "id", Ordinal: 1, Value: int64(1)}}); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a named argument gave %v, want dbimp.ErrArguments, because Druid has no named parameter", err)
	}
}
