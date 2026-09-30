package libsql //nolint:testpackage // The tests read the decoder and the encoder, which are not exported.

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestDecode holds D140 and D147: a value has the Go type of the affinity of
// its column, and a value of another storage class keeps its own Go type.
// Each value is one that the server sent (recorded: "every type").
func TestDecode(t *testing.T) {
	t.Parallel()
	col := columnOf
	day := dbimp.Date{Year: 2026, Month: time.October, Day: 1}
	for _, tt := range []struct {
		c    column
		v    string
		want any
	}{
		{col("INTEGER"), `{"type":"integer","value":"-9223372036854775808"}`, int64(math.MinInt64)},
		{col("INTEGER"), `{"type":"float","value":1.5}`, 1.5},
		{col("INTEGER"), `{"type":"null"}`, nil},
		{col("REAL"), `{"type":"float","value":-1.7976931348623157E+308}`, -math.MaxFloat64},
		{col("REAL"), `{"type":"text","value":"abc"}`, "abc"},
		{col("REAL"), `{"type":"integer","value":"2"}`, 2.0},
		{col(""), `{"type":"float","value":null}`, math.Inf(1)},
		{col("TEXT"), `{"type":"text","value":"é'\"\\ x"}`, "é'\"\\ x"},
		{col("TEXT"), `{"type":"integer","value":"42"}`, int64(42)},
		{col("BLOB"), `{"type":"blob","base64":"AP8"}`, []byte{0, 0xff}},
		{col("BLOB"), `{"type":"blob","base64":"AP8="}`, []byte{0, 0xff}},
		{col("BLOB"), `{"type":"blob","base64":""}`, []byte{}},
		{col("BLOB"), `{"type":"text","value":"text in a blob"}`, "text in a blob"},
		{col("NUMERIC"), `{"type":"float","value":1.25}`, 1.25},
		{col("NUMERIC"), `{"type":"integer","value":"12"}`, int64(12)},
		{col("BOOLEAN"), `{"type":"integer","value":"1"}`, true},
		{col("BOOLEAN"), `{"type":"integer","value":"0"}`, false},
		{col("BOOLEAN"), `{"type":"integer","value":"2"}`, true},
		{col("DATE"), `{"type":"text","value":"2026-10-01"}`, day},
		{col("DATE"), `{"type":"text","value":"not a date"}`, "not a date"},
		{col("DATE"), `{"type":"integer","value":"42"}`, int64(42)},
		{col("DATETIME"), `{"type":"text","value":"2026-10-01 12:34:56.789"}`, dbimp.LocalDateTime{Date: day, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}}},
		{col("DATETIME"), `{"type":"text","value":"yesterday"}`, "yesterday"},
		{col("DATETIME"), `{"type":"text","value":"2026-10-01T12:00:00+05:30"}`, "2026-10-01T12:00:00+05:30"},
		{col("DATETIME"), `{"type":"integer","value":"1727699696"}`, int64(1727699696)},
		{col("TIMESTAMP"), `{"type":"text","value":"2026-10-01 12:34:56"}`, time.Date(2026, time.October, 1, 12, 34, 56, 0, time.UTC)},
		{col("TIMESTAMP"), `{"type":"text","value":"x"}`, "x"},
		{col("TIMESTAMP"), `{"type":"float","value":1.5}`, 1.5},
		{col("F32_BLOB(3)"), `{"type":"blob","base64":"AACAPwAAAEAAAEBA"}`, dbimp.Vector[float32]{1, 2, 3}},
		{col("F32_BLOB(3)"), `{"type":"blob","base64":"AQ"}`, []byte{1}},
		{col("ANY"), `{"type":"text","value":"any"}`, "any"},
	} {
		got, err := decode(tt.c, jsontext.Value(tt.v))
		if err != nil {
			t.Errorf("decode(%+v, %s): %v", tt.c, tt.v, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%+v, %s) = %#v (%T), want %#v (%T)", tt.c, tt.v, got, got, tt.want, tt.want)
		}
	}
}

// TestDecodeTimestampKeepsItsOffset holds D147: a TIMESTAMP keeps the offset
// that its text names, and its nine digits (recorded: "every type").
func TestDecodeTimestampKeepsItsOffset(t *testing.T) {
	t.Parallel()
	got, err := decode(columnOf("TIMESTAMP"), jsontext.Value(`{"type":"text","value":"2026-10-01T12:34:56.123456789+05:30"}`))
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := got.(time.Time)
	if _, off := ts.Zone(); !ok || off != 5*3600+30*60 || ts.Nanosecond() != 123456789 {
		t.Errorf("decode gave %v, want 12:34:56.123456789 at +05:30", got)
	}
}

func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for _, v := range []string{`{"type":"integer","value":"x"}`, `{"type":"blob","base64":"!!"}`, `{"type":"vector"}`, `[]`} {
		if _, err := decode(columnOf("INTEGER"), jsontext.Value(v)); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("decode(%s) = %v, want %v", v, err, dbimp.ErrInvalidValue)
		}
	}
}

// TestStatement holds D152: each Go value goes as the typed value of Hrana
// that binds as its storage class, and named arguments go in named_args.
func TestStatement(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, time.October, 1, 12, 0, 0, 5, time.FixedZone("", 19800))
	pos := func(vals ...any) []driver.NamedValue {
		args := make([]driver.NamedValue, len(vals))
		for i, v := range vals {
			args[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
		}
		return args
	}
	for _, tt := range []struct {
		args []driver.NamedValue
		want string
	}{
		{nil, `{"sql":"q"}`},
		{pos(int64(1), 1.5, "a", []byte{0, 0xff}, nil, 1.0), `{"args":[{"type":"integer","value":"1"},{"type":"float","value":1.5},{"type":"text","value":"a"},{"base64":"AP8","type":"blob"},{"type":"null"},{"type":"float","value":1}],"sql":"q"}`},
		{pos(true, false, uint64(math.MaxInt64)), `{"args":[{"type":"integer","value":"1"},{"type":"integer","value":"0"},{"type":"integer","value":"9223372036854775807"}],"sql":"q"}`},
		{pos(ts, dbimp.Date{Year: 2026, Month: 10, Day: 1}), `{"args":[{"type":"text","value":"2026-10-01T12:00:00.000000005+05:30"},{"type":"text","value":"2026-10-01"}],"sql":"q"}`},
		{[]driver.NamedValue{{Name: "a", Ordinal: 1, Value: int64(1)}, {Name: "b", Ordinal: 2, Value: "x"}}, `{"named_args":[{"name":"a","value":{"type":"integer","value":"1"}},{"name":"b","value":{"type":"text","value":"x"}}],"sql":"q"}`},
	} {
		stmt, err := statement("q", tt.args)
		if err != nil {
			t.Errorf("statement(%v): %v", tt.args, err)
			continue
		}
		got, _ := json.Marshal(stmt, json.Deterministic(true))
		if string(got) != tt.want {
			t.Errorf("statement(%v) = %s, want %s", tt.args, got, tt.want)
		}
	}
}

func TestStatementRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args []driver.NamedValue
		want error
	}{
		{[]driver.NamedValue{{Ordinal: 1, Value: uint64(math.MaxInt64 + 1)}}, dbimp.ErrInvalidValue},
		{[]driver.NamedValue{{Ordinal: 1, Value: math.NaN()}}, dbimp.ErrInvalidValue},
		{[]driver.NamedValue{{Ordinal: 1, Value: math.Inf(1)}}, dbimp.ErrInvalidValue},
		{[]driver.NamedValue{{Ordinal: 1, Value: dbimp.Interval{Days: 1}}}, dbimp.ErrNotSupported},
		{[]driver.NamedValue{{Ordinal: 1, Value: int64(1)}, {Name: "b", Ordinal: 2, Value: int64(2)}}, dbimp.ErrArguments},
	} {
		if _, err := statement("q", tt.args); !errors.Is(err, tt.want) {
			t.Errorf("statement(%v) = %v, want %v", tt.args, err, tt.want)
		}
	}
}
