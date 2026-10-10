package bigquery //nolint:testpackage // The tests call the decoder, which is not exported.

import (
	"encoding/json/jsontext"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestParseTimestamp holds D189 item 5 and docs/BIGQUERY.md: the driver reads the
// ISO form that it asks for, and the default form of the service and the form of
// the emulator as decimals, so that no digit passes through a float64.
func TestParseTimestamp(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want time.Time
	}{
		{"2024-01-02T03:04:05.123456Z", at(123456)},
		{"2024-01-02T03:04:05Z", at(0)},
		{"0001-01-01T00:00:00Z", time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"9999-12-31T23:59:59.999999Z", time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)},
		{"2024-01-02T10:04:05.5+07:00", at(500000)},
		{"1.704164645123456E9", at(123456)},
		{"1.7041646455E9", at(500000)},
		{"1704164645.123456", at(123456)},
		{"1704164645", at(0)},
		{"0.0", time.Unix(0, 0).UTC()},
		{"-0.5", time.Unix(-1, 500000000).UTC()},
		{"-6.21355968E10", time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"1969-12-31T23:59:59.5Z", time.Unix(-1, 500000000).UTC()},
		// A float64 holds 253402300799.999999 as 253402300800, and the decimal keeps
		// every digit.
		{"253402300799.999999", time.Date(9999, 12, 31, 23, 59, 59, 999999000, time.UTC)},
		// A precision of 12 digits loses the last three.
		{"1704164645.123456789012", at(123457)},
	} {
		got, err := parseTimestamp(tt.in)
		if err != nil || !got.Equal(tt.want) || got.Location() != time.UTC {
			t.Errorf("parseTimestamp(%q) = %v, %v, want %v in UTC", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"", "x", "NaN", "Infinity", "1e999999999", "1e30", "1e-99", "2.534023008E11", "-6.2135596801E10", "2024-13-01T00:00:00Z", "2024-01-02T03:04:05", "1/2", "0x10", "1,5"} {
		if got, err := parseTimestamp(in); err == nil || !isErr(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseTimestamp(%q) = %v, %v, want an error that wraps dbimp.ErrInvalidValue", in, got, err)
		}
	}
}

// TestParseInterval holds D135 with the text of an INTERVAL, in which each part
// carries its own sign (recorded: bigquery-092).
func TestParseInterval(t *testing.T) {
	t.Parallel()
	sec := int64(time.Second)
	for _, tt := range []struct {
		in   string
		want dbimp.Interval
	}{
		{"0-0 1 0:0:0", dbimp.Interval{Days: 1}},
		{"1-2 3 4:5:6.789", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60+6)*sec + 789000000}},
		{"-0-5 0 0:0:0", dbimp.Interval{Months: -5}},
		{"10000-0 0 0:0:0", dbimp.Interval{Months: 120000}},
		{"0-0 0 0:0:0", dbimp.Interval{}},
		{"-1-2 -3 -4:5:6.5", dbimp.Interval{Months: -14, Days: -3, Nanoseconds: -(((4*60+5)*60+6)*sec + 500000000)}},
		{"+1-2 +3 +4:5:6", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60 + 6) * sec}},
		{"0-0 -3660000 0:0:0", dbimp.Interval{Days: -3660000}},
		{"0-0 0 87840000:0:0", dbimp.Interval{Nanoseconds: 87840000 * 3600 * sec}},
		{"0-0 0 0:0:0.000001", dbimp.Interval{Nanoseconds: 1000}},
		{"0-0 0 0:0:0.123456789", dbimp.Interval{Nanoseconds: 123456789}},
	} {
		got, err := parseInterval(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("parseInterval(%q) = %+v, %v, want %+v", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"", "1", "1-2", "1-2 3", "1-2 3 4:5", "a-b c d:e:f", "1-2 3 4:5:6:7", "1-12 0 0:0:0", "10001-0 0 0:0:0", "0-0 3660001 0:0:0", "0-0 0 87840001:0:0", "0-0 0 0:60:0", "0-0 0 0:0:60", "0-0 0 0:0:0.1234567891", "0-0 0 0:0:0.x", "-- 0 0:0:0", "99999999999-0 0 0:0:0", "1-2 3 4:5:6 7"} {
		if got, err := parseInterval(in); err == nil || !isErr(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseInterval(%q) = %+v, %v, want an error that wraps dbimp.ErrInvalidValue", in, got, err)
		}
	}
}

// TestDecode holds D135 and D189: each wire type has one Go type, a number and a
// boolean in JSON read as their text, and a NULL is nil.
func TestDecode(t *testing.T) {
	t.Parallel()
	col := func(wire string) column { return column{name: "c", wire: wire, mode: modeNullable} }
	for _, tt := range []struct {
		wire string
		in   string
		want any
	}{
		{wireInteger, `"-5"`, int64(-5)},
		{wireInteger, `7`, int64(7)},
		{"INT64", `"9223372036854775807"`, int64(math.MaxInt64)},
		{wireFloat, `"1.0E308"`, 1e308},
		{wireFloat, `"4.9E-324"`, 5e-324},
		{wireFloat, `"NaN"`, math.NaN()},
		{wireFloat, `"Infinity"`, math.Inf(1)},
		{wireFloat, `"-Infinity"`, math.Inf(-1)},
		{wireFloat, `"+Inf"`, math.Inf(1)},
		{wireFloat, `1.5`, 1.5},
		{"FLOAT64", `"-0.0"`, math.Copysign(0, -1)},
		{wireNumeric, `"1.5"`, dec(t, "1.5")},
		{wireBigNumeric, `"-578960446186580977117854925043439539266.34992332820282019728792003956564819968"`, dec(t, "-578960446186580977117854925043439539266.34992332820282019728792003956564819968")},
		{wireBoolean, `"true"`, true},
		{wireBoolean, `"false"`, false},
		{wireBoolean, `true`, true},
		{"BOOL", `"false"`, false},
		{wireString, `""`, ""},
		{wireString, `"héllo"`, "héllo"},
		{wireBytes, `""`, []byte{}},
		{wireBytes, `"AP8="`, []byte{0, 0xff}},
		{wireDate, `"0001-01-01"`, dbimp.Date{Year: 1, Month: time.January, Day: 1}},
		{wireTime, `"12:34:56.789000"`, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}},
		{wireDatetime, `"2024-01-02T03:04:05.123456"`, dbimp.LocalDateTime{Date: dbimp.Date{Year: 2024, Month: time.January, Day: 2}, Time: dbimp.LocalTime{Hour: 3, Minute: 4, Second: 5, Nanosecond: 123456000}}},
		{wireTimestamp, `"2024-01-02T03:04:05.123456Z"`, at(123456)},
		{wireJSON, `"{\"a\":[1,2.5,{\"b\":null}]}"`, map[string]any{"a": []any{int64(1), 2.5, map[string]any{"b": nil}}}},
		{wireJSON, `"null"`, nil},
		{wireJSON, `"\"s\""`, "s"},
		{wireJSON, `"12345678901234567890"`, dec(t, "12345678901234567890")},
		{wireGeography, `"POINT(1 2)"`, "POINT(1 2)"},
		{wireInterval, `"1-2 3 4:5:6"`, dbimp.Interval{Months: 14, Days: 3, Nanoseconds: ((4*60+5)*60 + 6) * int64(time.Second)}},
		{wireRange, `"[2024-01-01, UNBOUNDED)"`, "[2024-01-01, UNBOUNDED)"},
		{"NEWTYPE", `"text of a type that the driver does not know"`, "text of a type that the driver does not know"},
		{wireInteger, `null`, nil},
		{wireString, `null`, nil},
		{wireJSON, `null`, nil},
		{wireTimestamp, `null`, nil},
	} {
		c := col(tt.wire)
		if a, ok := aliases[tt.wire]; ok {
			c.wire = a
		}
		got, err := decode(c, jsontext.Value(tt.in))
		if err != nil || !same(got, tt.want) {
			t.Errorf("decode(%s, %s) = %#v, %v, want %#v", tt.wire, tt.in, got, err, tt.want)
		}
	}
}

// TestDecodeRefuses holds that a value that does not fit its type is an error
// that wraps dbimp.ErrInvalidValue and names the column.
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		wire string
		in   string
	}{
		{wireInteger, `"1.5"`},
		{wireInteger, `"9223372036854775808"`},
		{wireInteger, `"x"`},
		{wireInteger, `[]`},
		{wireInteger, `{}`},
		{wireFloat, `"one"`},
		{wireNumeric, `"1e"`},
		{wireNumeric, `"NaN"`},
		{wireBoolean, `"1"`},
		{wireBoolean, `"yes"`},
		{wireBytes, `"not base64!"`},
		{wireDate, `"2024-02-30"`},
		{wireTime, `"25:00:00"`},
		{wireDatetime, `"2024-01-02"`},
		{wireTimestamp, `"x"`},
		{wireJSON, `"{"`},
		{wireInterval, `"1-2"`},
	} {
		_, err := decode(column{name: "mycol", wire: tt.wire, mode: modeNullable}, jsontext.Value(tt.in))
		if err == nil || !isErr(err, dbimp.ErrInvalidValue) || !strings.Contains(err.Error(), "mycol") {
			t.Errorf("decode(%s, %s) = %v, want an error that names the column and wraps dbimp.ErrInvalidValue", tt.wire, tt.in, err)
		}
	}
}

// TestDecodeStructs holds D189 item 7: a STRUCT is a map by the names of its
// members, a struct that repeats a name or has none is refused with an error
// that names the column, and an ARRAY that is NULL is an empty list.
func TestDecodeStructs(t *testing.T) {
	t.Parallel()
	cols, err := newColumns([]schemaField{
		{Name: "s", Type: "RECORD", Mode: "NULLABLE", Fields: []schemaField{
			{Name: "a", Type: "INTEGER", Mode: "NULLABLE"},
			{Name: "inner", Type: "STRUCT", Mode: "REPEATED", Fields: []schemaField{{Name: "x", Type: "STRING"}}},
			{Name: "ts", Type: "TIMESTAMP"},
		}},
		{Name: "arr", Type: "INTEGER", Mode: "REPEATED"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := decode(cols[0], jsontext.Value(`{"f":[{"v":"1"},{"v":[{"v":{"f":[{"v":"p"}]}},{"v":{"f":[{"v":null}]}}]},{"v":"2024-01-02T03:04:05Z"}]}`))
	want := map[string]any{"a": int64(1), "inner": []any{map[string]any{"x": "p"}, map[string]any{"x": nil}}, "ts": at(0)}
	if err != nil || !same(got, want) {
		t.Errorf("a struct is %#v, %v, want %#v", got, err, want)
	}
	for _, in := range []string{`{"f":[{"v":"1"}]}`, `{"f":[]}`, `[]`, `"x"`} {
		if got, err := decode(cols[0], jsontext.Value(in)); err == nil {
			t.Errorf("a struct of %s gave %#v, want an error", in, got)
		}
	}
	for _, in := range []string{`null`, `[]`, `[{"v":"1"},{"v":"2"}]`, `[{"v":null}]`} {
		got, err := decode(cols[1], jsontext.Value(in))
		if err != nil {
			t.Errorf("an array of %s gave %v", in, err)
		}
		if l, ok := got.([]any); !ok || l == nil {
			t.Errorf("an array of %s is %#v, want a list that is not nil", in, got)
		}
	}
	for name, fields := range map[string][]schemaField{
		"a repeated name": {{Name: "x", Type: "INTEGER"}, {Name: "x", Type: "INTEGER"}},
		"no name":         {{Name: "", Type: "INTEGER"}},
	} {
		_, err := newColumns([]schemaField{{Name: "bad", Type: "RECORD", Fields: fields}}, "")
		if err == nil || !isErr(err, dbimp.ErrNotSupported) || !strings.Contains(err.Error(), "column bad") {
			t.Errorf("%s: the error is %v, want one that names the column bad", name, err)
		}
	}
	// The error names the path of a column in a struct.
	_, err = newColumns([]schemaField{{Name: "outer", Type: "RECORD", Fields: []schemaField{{Name: "in", Type: "RECORD", Fields: []schemaField{{Name: "x"}, {Name: "x"}}}}}}, "")
	if err == nil || !strings.Contains(err.Error(), "outer.in") {
		t.Errorf("the error is %v, want one that names outer.in", err)
	}
}

// TestRowsRefuse holds D18 and D189 with answers that break a rule: a row with
// the wrong number of values, rows before the schema, a value that does not fit,
// and a count that is not a number.
func TestRowsRefuse(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		body string
		want error
	}{
		{"too many values", head("a") + row("1", "2") + tail(""), dbimp.ErrColumnCount},
		{"too few values", head("a", "b") + row("1") + tail(""), dbimp.ErrColumnCount},
		{"rows before the schema", `{"rows":[{"f":[{"v":"1"}]}],"schema":{"fields":[{"name":"a","type":"INTEGER"}]},"jobComplete":true}`, dbimp.ErrInvalidValue},
		{"more rows than totalRows", head("a") + row("1") + "," + row("2") + tail("1"), dbimp.ErrInvalidValue},
		{"a count that is not a number", head("a") + row("1") + `],"totalRows":"many"}`, dbimp.ErrInvalidValue},
		{"a row that is not an object", head("a") + `[1]` + tail(""), dbimp.ErrInvalidValue},
		{"a value that does not fit", head("a") + row("x") + tail(""), dbimp.ErrInvalidValue},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = io.WriteString(w, tt.body)
		}))
		t.Cleanup(srv.Close)
		db := open(t, config(), srv.URL)
		_, _, err := readAllContext(t, t.Context(), db, "SELECT a")
		if !isErr(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", tt.name, err, tt.want)
		}
	}
}
