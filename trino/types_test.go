package trino //nolint:testpackage // The tests read the decoder of the types, which is not exported.

import (
	"encoding/json/jsontext"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// sigOf returns the signature of the type that the JSON text of a column holds.
func sigOf(t *testing.T, columns string) []column {
	t.Helper()
	cols, err := readColumns(jsontext.Value(columns))
	if err != nil {
		t.Fatalf("reading the columns %s: %v", columns, err)
	}
	return cols
}

// TestSignatures holds the two forms of a type signature (measured): Trino
// writes each argument as a kind and a value, and Presto writes the types
// again in typeArguments, with the names of the fields of a row in
// literalArguments.
func TestSignatures(t *testing.T) {
	t.Parallel()
	trinoCols := sigOf(t, `[
		{"name":"a","type":"row(a integer, b varchar, c double)","typeSignature":{"rawType":"row","arguments":[
			{"kind":"NAMED_TYPE","value":{"fieldName":{"name":"a"},"typeSignature":{"rawType":"integer","arguments":[]}}},
			{"kind":"NAMED_TYPE","value":{"fieldName":{"name":"b"},"typeSignature":{"rawType":"varchar","arguments":[{"kind":"LONG","value":2147483647}]}}},
			{"kind":"NAMED_TYPE","value":{"fieldName":{"name":"c"},"typeSignature":{"rawType":"double","arguments":[]}}}]}},
		{"name":"b","type":"map(varchar(1), integer)","typeSignature":{"rawType":"map","arguments":[
			{"kind":"TYPE","value":{"rawType":"varchar","arguments":[{"kind":"LONG","value":1}]}},
			{"kind":"TYPE","value":{"rawType":"integer","arguments":[]}}]}},
		{"name":"c","type":"decimal(38, 1)","typeSignature":{"rawType":"decimal","arguments":[{"kind":"LONG","value":38},{"kind":"LONG","value":1}]}},
		{"name":"d","type":"timestamp(3) with time zone","typeSignature":{"rawType":"timestamp with time zone","arguments":[{"kind":"LONG","value":3}]}}]`)
	prestoCols := sigOf(t, `[
		{"name":"a","type":"row(a integer,b varchar,c double)","typeSignature":{"rawType":"row",
			"typeArguments":[{"rawType":"integer","typeArguments":[],"literalArguments":[],"arguments":[]},
				{"rawType":"varchar","typeArguments":[],"literalArguments":[],"arguments":[{"kind":"LONG_LITERAL","value":2147483647}]},
				{"rawType":"double","typeArguments":[],"literalArguments":[],"arguments":[]}],
			"literalArguments":["a","b","c"],
			"arguments":[{"kind":"NAMED_TYPE_SIGNATURE","value":{"fieldName":{"name":"a","delimited":false},"typeSignature":"integer"}},
				{"kind":"NAMED_TYPE_SIGNATURE","value":{"fieldName":{"name":"b","delimited":false},"typeSignature":"varchar"}},
				{"kind":"NAMED_TYPE_SIGNATURE","value":{"fieldName":{"name":"c","delimited":false},"typeSignature":"double"}}]}},
		{"name":"b","type":"map(varchar(1),integer)","typeSignature":{"rawType":"map",
			"typeArguments":[{"rawType":"varchar","typeArguments":[],"literalArguments":[],"arguments":[{"kind":"LONG_LITERAL","value":1}]},
				{"rawType":"integer","typeArguments":[],"literalArguments":[],"arguments":[]}],
			"literalArguments":[],
			"arguments":[{"kind":"TYPE_SIGNATURE","value":{"rawType":"varchar","typeArguments":[],"literalArguments":[],"arguments":[{"kind":"LONG_LITERAL","value":1}]}},
				{"kind":"TYPE_SIGNATURE","value":{"rawType":"integer","typeArguments":[],"literalArguments":[],"arguments":[]}}]}},
		{"name":"c","type":"decimal(38,1)","typeSignature":{"rawType":"decimal","typeArguments":[],"literalArguments":[],"arguments":[{"kind":"LONG_LITERAL","value":38},{"kind":"LONG_LITERAL","value":1}]}},
		{"name":"d","type":"timestamp with time zone","typeSignature":{"rawType":"timestamp with time zone","typeArguments":[],"literalArguments":[],"arguments":[]}}]`)
	for name, cols := range map[string][]column{"trino": trinoCols, "presto": prestoCols} {
		row := cols[0].sig
		if row.raw != "row" || len(row.elems) != 3 || !reflect.DeepEqual(row.names, []string{"a", "b", "c"}) ||
			row.elems[0].raw != "integer" || row.elems[1].raw != "varchar" || row.elems[2].raw != "double" {
			t.Errorf("%s: the row is %+v, want three fields a, b and c", name, row)
		}
		m := cols[1].sig
		if m.raw != "map" || len(m.elems) != 2 || m.elems[0].raw != "varchar" || !reflect.DeepEqual(m.elems[0].nums, []int64{1}) || m.elems[1].raw != "integer" {
			t.Errorf("%s: the map is %+v, want varchar(1) to integer", name, m)
		}
		if dsig := cols[2].sig; dsig.raw != "decimal" || !reflect.DeepEqual(dsig.nums, []int64{38, 1}) {
			t.Errorf("%s: the decimal is %+v, want 38 and 1", name, dsig)
		}
		if got := cols[3].sig.databaseType(); got != "TIMESTAMP WITH TIME ZONE" {
			t.Errorf("%s: the database type is %q", name, got)
		}
	}
	// A column with no signature has the name of its type before the first
	// parenthesis.
	cols := sigOf(t, `[{"name":"x","type":"Decimal(5, 2)"}]`)
	if got := cols[0].sig.raw; got != "decimal" {
		t.Errorf("a column with no signature has the type %q, want decimal", got)
	}
}

// TestColumnTypeDetails holds the length, the precision and the scale of a
// column.
func TestColumnTypeDetails(t *testing.T) {
	t.Parallel()
	r := &rows{cols: sigOf(t, `[
		{"name":"a","type":"varchar(10)","typeSignature":{"rawType":"varchar","arguments":[{"kind":"LONG","value":10}]}},
		{"name":"b","type":"varchar","typeSignature":{"rawType":"varchar","arguments":[{"kind":"LONG","value":2147483647}]}},
		{"name":"c","type":"varbinary","typeSignature":{"rawType":"varbinary","arguments":[]}},
		{"name":"d","type":"decimal(38, 4)","typeSignature":{"rawType":"decimal","arguments":[{"kind":"LONG","value":38},{"kind":"LONG","value":4}]}},
		{"name":"e","type":"bigint","typeSignature":{"rawType":"bigint","arguments":[]}}]`)}
	for i, want := range []struct {
		length int64
		ok     bool
	}{{10, true}, {1<<63 - 1, true}, {1<<63 - 1, true}, {0, false}, {0, false}} {
		if n, ok := r.ColumnTypeLength(i); n != want.length || ok != want.ok {
			t.Errorf("the length of column %d is %d, %v, want %d, %v", i, n, ok, want.length, want.ok)
		}
	}
	for i, want := range []struct {
		p, s int64
		ok   bool
	}{{0, 0, false}, {0, 0, false}, {0, 0, false}, {38, 4, true}, {0, 0, false}} {
		if p, s, ok := r.ColumnTypePrecisionScale(i); p != want.p || s != want.s || ok != want.ok {
			t.Errorf("the precision and the scale of column %d are %d, %d, %v, want %d, %d, %v", i, p, s, ok, want.p, want.s, want.ok)
		}
	}
}

// TestTrimFraction holds D175: a time has up to twelve digits of fraction, and
// the driver drops the digits beyond the ninth only when they are all zero.
func TestTrimFraction(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in, want string
		err      bool
	}{
		{"12:34:56", "12:34:56", false},
		{"12:34:56.789", "12:34:56.789", false},
		{"12:34:56.123456789", "12:34:56.123456789", false},
		{"12:34:56.123456789000", "12:34:56.123456789", false},
		{"12:34:56.123456789001", "", true},
		{"12:34:56.1234567890", "12:34:56.123456789", false},
		{"12:34:56.1234567891", "", true},
		{"2026-10-01 12:34:56.123456789000 Asia/Jakarta", "2026-10-01 12:34:56.123456789 Asia/Jakarta", false},
		{"2026-10-01 12:34:56.000000000000 +05:30", "2026-10-01 12:34:56.000000000 +05:30", false},
		{"12:34:56.123456789000+05:30", "12:34:56.123456789+05:30", false},
		{"12:34:56.123456789500+05:30", "", true},
		{"2026-10-01", "2026-10-01", false},
	} {
		got, err := trimFraction(tt.in)
		if (err != nil) != tt.err || got != tt.want {
			t.Errorf("trimFraction(%q) = %q, %v, want %q, error %v", tt.in, got, err, tt.want, tt.err)
		}
		if err != nil && !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("trimFraction(%q): the error does not wrap dbimp.ErrInvalidValue", tt.in)
		}
	}
}

// decodeAs decodes the JSON text v as a value of the type that raw names.
func decodeAs(raw, v string) (any, error) {
	return (&signature{raw: raw}).decode(jsontext.Value(v))
}

// TestDecodeTimes holds the forms that the servers write a time in.
func TestDecodeTimes(t *testing.T) {
	t.Parallel()
	est := time.FixedZone("", -5*3600)
	for _, tt := range []struct {
		raw, in string
		want    any
	}{
		{typeTime, `"12:34:56"`, ltime(12, 34, 56, 0)},
		{typeTime, `"12:34:56.123456789000"`, ltime(12, 34, 56, 123456789)},
		{typeTime, `"00:00:00.000000000000"`, ltime(0, 0, 0, 0)},
		{typeTimeTZ, `"12:34:56.789+05:30"`, dbimp.OffsetTime{Time: ltime(12, 34, 56, 789e6), Offset: 19800}},
		{typeTimeTZ, `"12:34:56.789 +05:30"`, dbimp.OffsetTime{Time: ltime(12, 34, 56, 789e6), Offset: 19800}},
		{typeTimeTZ, `"12:34:56.789 -05:00"`, dbimp.OffsetTime{Time: ltime(12, 34, 56, 789e6), Offset: -18000}},
		{typeTimeTZ, `"00:00:00.000UTC"`, dbimp.OffsetTime{}},
		{typeTimeTZ, `"12:34:56.789 UTC"`, dbimp.OffsetTime{Time: ltime(12, 34, 56, 789e6)}},
		{typeTimeTZ, `"12:34:56.789000000000 +00:00"`, dbimp.OffsetTime{Time: ltime(12, 34, 56, 789e6)}},
		{typeTimestamp, `"2026-10-01 12:34:56.123456789000"`, ldt(date(2026, 10, 1), ltime(12, 34, 56, 123456789))},
		{typeTimestamp, `"0001-01-01 00:00:00"`, ldt(date(1, 1, 1), ltime(0, 0, 0, 0))},
		{typeTimestTZ, `"2026-10-01 12:34:56.5 UTC"`, time.Date(2026, 10, 1, 12, 34, 56, 5e8, time.UTC)},
		{typeTimestTZ, `"2026-10-01 12:34:56.5 -05:00"`, time.Date(2026, 10, 1, 12, 34, 56, 5e8, est)},
		{typeTimestTZ, `"2026-10-01 12:34:56.5 +00:00"`, time.Date(2026, 10, 1, 12, 34, 56, 5e8, time.UTC)},
		{typeTimestTZ, `"2026-10-01 12:34:56.500000000000 +00:00"`, time.Date(2026, 10, 1, 12, 34, 56, 5e8, time.UTC)},
	} {
		got, err := decodeAs(tt.raw, tt.in)
		if err != nil || !same(got, tt.want) {
			t.Errorf("%s %s = %#v, %v, want %#v", tt.raw, tt.in, got, err, tt.want)
		}
	}
	for _, tt := range []struct{ raw, in string }{
		{typeTime, `"12:34:56.123456789001"`},
		{typeTime, `"25:00:00"`},
		{typeTime, `12`},
		{typeTimeTZ, `"12:34:56.789"`},
		{typeTimestamp, `"2026-13-01 00:00:00"`},
		{typeTimestTZ, `"2026-10-01 12:34:56.5"`},
		{typeTimestTZ, `"2026-10-01 12:34:56.5 Not/AZone"`},
		{typeTimestTZ, `"2026-10-01 12:34:56.5 +zz:00"`},
		{typeTimestTZ, `"2026-10-01 12:34:56.123456789001 UTC"`},
		{typeDate, `"2026-02-30"`},
		{typeDate, `"x"`},
	} {
		if got, err := decodeAs(tt.raw, tt.in); err == nil {
			t.Errorf("%s %s = %#v, want an error, because the value cannot be held", tt.raw, tt.in, got)
		}
	}
}

// TestDecodeIntervals holds the two intervals (measured).
func TestDecodeIntervals(t *testing.T) {
	t.Parallel()
	const hour = int64(3600e9)
	for _, tt := range []struct {
		raw, in string
		want    dbimp.Interval
	}{
		{typeIntervalY, `"1-2"`, dbimp.Interval{Months: 14}},
		{typeIntervalY, `"0-0"`, dbimp.Interval{}},
		{typeIntervalY, `"-1-2"`, dbimp.Interval{Months: -14}},
		{typeIntervalY, `"178956970-7"`, dbimp.Interval{Months: 2147483647}},
		{typeIntervalD, `"3 04:05:06.789"`, dbimp.Interval{Days: 3, Nanoseconds: 4*hour + 5*60e9 + 6789e6}},
		{typeIntervalD, `"-1 00:00:00.000"`, dbimp.Interval{Days: -1}},
		{typeIntervalD, `"-0 00:00:01.500"`, dbimp.Interval{Nanoseconds: -1500e6}},
		{typeIntervalD, `"-2 01:00:00.000"`, dbimp.Interval{Days: -2, Nanoseconds: -hour}},
		{typeIntervalD, `"0 00:00:00.000"`, dbimp.Interval{}},
	} {
		got, err := decodeAs(tt.raw, tt.in)
		if err != nil || got != tt.want {
			t.Errorf("%s %s = %#v, %v, want %#v", tt.raw, tt.in, got, err, tt.want)
		}
	}
	for _, tt := range []struct{ raw, in string }{
		{typeIntervalY, `"1"`}, {typeIntervalY, `"a-b"`}, {typeIntervalY, `"178956971-0"`}, {typeIntervalD, `"3"`},
		{typeIntervalD, `"x 04:05:06"`}, {typeIntervalD, `5`},
	} {
		if got, err := decodeAs(tt.raw, tt.in); err == nil {
			t.Errorf("%s %s = %#v, want an error", tt.raw, tt.in, got)
		}
	}
}

// TestDecodeContainers holds that Presto sends a container as the JSON text in
// a string, and a container inside a container as a string again, and that
// the decoder gives the same values for both forms, by the type of the column
// (D175).
func TestDecodeContainers(t *testing.T) {
	t.Parallel()
	cols := sigOf(t, `[
		{"name":"a","type":"array(array(integer))","typeSignature":{"rawType":"array","typeArguments":[{"rawType":"array","typeArguments":[{"rawType":"integer"}]}]}},
		{"name":"m","type":"map(varchar, array(integer))","typeSignature":{"rawType":"map","typeArguments":[{"rawType":"varchar"},{"rawType":"array","typeArguments":[{"rawType":"integer"}]}]}},
		{"name":"r","type":"row(x integer, y array(date))","typeSignature":{"rawType":"row","literalArguments":["x","y"],"typeArguments":[{"rawType":"integer"},{"rawType":"array","typeArguments":[{"rawType":"date"}]}]}}]`)
	for i, tt := range []struct {
		col           int
		trino, presto string
		want          any
	}{
		{0, `[[1],[2,3]]`, `"[ \"[ 1 ]\", \"[ 2, 3 ]\" ]"`, []any{[]any{int64(1)}, []any{int64(2), int64(3)}}},
		{0, `[[],null]`, `"[ \"[ ]\", null ]"`, []any{[]any{}, nil}},
		{1, `{"k":[1,null]}`, `"{\n  \"k\" : \"[ 1, null ]\"\n}"`, map[string]any{"k": []any{int64(1), nil}}},
		{2, `[7,["2026-10-01"]]`, `"[ 7, \"[ \\\"2026-10-01\\\" ]\" ]"`, []any{int64(7), []any{date(2026, 10, 1)}}},
	} {
		for form, in := range map[string]string{"trino": tt.trino, "presto": tt.presto} {
			got, err := cols[tt.col].sig.decode(jsontext.Value(in))
			if err != nil || !same(got, tt.want) {
				t.Errorf("%s %d: %s = %#v, %v, want %#v", form, i, in, got, err, tt.want)
			}
		}
	}
	for _, tt := range []struct {
		col int
		in  string
	}{
		{0, `"not json"`}, {0, `5`}, {0, `{"a":1}`}, {1, `[1]`}, {2, `[1]`}, {2, `[1,[],3]`}, {0, `[["x"]]`},
	} {
		if got, err := cols[tt.col].sig.decode(jsontext.Value(tt.in)); err == nil {
			t.Errorf("column %d %s = %#v, want an error", tt.col, tt.in, got)
		}
	}
	// A type with no argument for its element is an error.
	if _, err := (&signature{raw: typeArray}).decode(jsontext.Value(`[1]`)); err == nil {
		t.Error("an array with no type for its elements gave no error")
	}
}

// TestDecodeScalars holds the edges of each scalar type.
func TestDecodeScalars(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		raw, in string
		want    any
	}{
		{typeBigint, `-9223372036854775808`, int64(-9223372036854775808)},
		{typeInteger, `null`, nil},
		{typeUnknown, `null`, nil},
		{typeDouble, `"NaN"`, nil},
		{typeVarchar, `"é"`, "é"},
		{typeChar, `"ab   "`, "ab   "},
		{typeVarbinary, `""`, []byte{}},
		{"hyperloglog", `"AAE="`, []byte{0, 1}},
		{typeJSON, `"null"`, nil},
		{typeJSON, `"[1,{\"a\":2.5}]"`, []any{int64(1), map[string]any{"a": 2.5}}},
		{typeVariant, `{"a":[1]}`, map[string]any{"a": []any{int64(1)}}},
		{"somethingnew", `"x"`, "x"},
		{"somethingnew", `[1]`, []any{int64(1)}},
	} {
		got, err := decodeAs(tt.raw, tt.in)
		if tt.raw == typeDouble {
			if f, ok := got.(float64); err != nil || !ok || f == f {
				t.Errorf("%s %s = %#v, %v, want NaN", tt.raw, tt.in, got, err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s %s = %#v, %v, want %#v", tt.raw, tt.in, got, err, tt.want)
		}
	}
	for _, tt := range []struct{ raw, in string }{
		{typeBigint, `"1"`}, {typeBigint, `1.5`}, {typeBoolean, `1`}, {typeVarchar, `1`}, {typeDouble, `"x"`}, {typeDecimal, `true`},
		{typeDecimal, `"1.5.5"`}, {typeVarbinary, `"***"`}, {typeJSON, `"{"`}, {typeUUID, `"zz"`}, {typeUUID, `1`},
	} {
		if got, err := decodeAs(tt.raw, tt.in); err == nil {
			t.Errorf("%s %s = %#v, want an error", tt.raw, tt.in, got)
		}
	}
	got, err := decodeAs(typeDecimal, `"1234567890123456789012345678901234567.8"`)
	if d, ok := got.(*apd.Decimal); err != nil || !ok || d.String() != "1234567890123456789012345678901234567.8" {
		t.Errorf("a decimal of 38 digits is %v, %v", got, err)
	}
}

// TestLocation holds the zones that a timestamp names.
func TestLocation(t *testing.T) {
	t.Parallel()
	for _, zone := range []string{"UTC", "+00:00", "-00:00"} {
		if loc, err := location(zone); err != nil || loc != time.UTC {
			t.Errorf("location(%q) = %v, %v, want UTC", zone, loc, err)
		}
	}
	loc, err := location("+05:30")
	if err != nil {
		t.Fatal(err)
	}
	if _, off := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Zone(); off != 19800 {
		t.Errorf("the offset of +05:30 is %d, want 19800", off)
	}
	for _, zone := range []string{"", "Not/AZone", "+", "+5", "+25:00"} {
		if _, err := location(zone); err == nil {
			t.Errorf("location(%q) gave no error", zone)
		} else if !strings.Contains(err.Error(), "zone") {
			t.Errorf("location(%q): the error %v does not name the zone", zone, err)
		}
	}
}
