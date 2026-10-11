package exasol //nolint:testpackage // The tests read the decoder and the encoder, which are not exported.

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// typesFile is the recorded answer to "select every row of types": ten rows of
// twenty-five columns, in every type (docs/EXASOL.md, Types).
const typesFile = "../testdata/exasol/exasol-2026.2.0-024-websocket-execute.json"

// show writes a value with its Go type, so that a test compares both.
func show(v any) string {
	switch x := v.(type) {
	case []byte:
		return fmt.Sprintf("[]uint8 %x", x)
	case time.Time:
		return "time.Time " + x.Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%T %v", v, v)
}

// recordedTypes returns the columns and the raw values of the recorded answer.
func recordedTypes(t *testing.T) ([]column, [][]jsontext.Value) {
	t.Helper()
	ex, err := dbimptest.ReadWebSocketExchange(typesFile)
	if err != nil {
		t.Fatal(err)
	}
	env, err := decodeEnvelope(ex.Reply.Content())
	if err != nil {
		t.Fatal(err)
	}
	rs := env.ResponseData.Results[0].ResultSet
	cols := columnsOf(rs.Columns)
	rowsOfValues := make([][]jsontext.Value, rs.NumRows)
	for c, data := range rs.Data {
		dec := jsontext.NewDecoder(strings.NewReader(string(data)))
		if _, err := dec.ReadToken(); err != nil {
			t.Fatal(err)
		}
		for r := range rowsOfValues {
			v, err := dec.ReadValue()
			if err != nil {
				t.Fatal(err)
			}
			if rowsOfValues[r] == nil {
				rowsOfValues[r] = make([]jsontext.Value, len(cols))
			}
			rowsOfValues[r][c] = v.Clone()
		}
	}
	return cols, rowsOfValues
}

// TestDecode holds D206 for each type, with the values that the server sent for
// the smallest, the largest and an ordinary row (recorded: "select every row of
// types").
func TestDecode(t *testing.T) {
	t.Parallel()
	cols, data := recordedTypes(t)
	loc := func() (*time.Location, error) { return time.FixedZone("CET", 3600), nil }
	cet := time.FixedZone("CET", 3600)
	day := func(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
	local := func(d dbimp.Date, h, m, s, ns int) dbimp.LocalDateTime {
		return dbimp.LocalDateTime{Date: d, Time: dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: ns}}
	}
	dec := func(s string) *apd.Decimal {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	for _, tt := range []struct {
		row  int
		want map[string]any
		// err names the columns whose value the driver cannot read.
		err []string
	}{
		{0, map[string]any{
			"ID": int64(1), "B": true, "C": "ab   ", "V": "hello", "VA": "ascii", "D1": int64(9007199254740993),
			"D2": dec("1.5"), "D3": dec("12345678901234567890123456789012345"), "D4": dec("1234567890123456789012345.6789012345"),
			"D5": int64(123456789), "D6": int64(7), "D7": dec("1234567890123456789"), "D8": dec("1234567890123.45678"),
			"D9": dec("0.123456789012345678"), "F": 1.5e300, "DT": day(2026, 10, 11),
			"TS": local(day(2026, 10, 11), 12, 34, 56, 123000000), "TS9": local(day(2026, 10, 11), 12, 34, 56, 123456000),
			"TSL": time.Date(2026, 10, 11, 12, 34, 56, 123000000, cet), "IYM": dbimp.Interval{Months: 63},
			"IDS": dbimp.Interval{Days: 2, Nanoseconds: 3*3600e9 + 4*60e9 + 5e9 + 678e6}, "G": "POINT (1 2)", "G2": "POINT (1 2)",
			"H": mustHex("550e8400e29b41d4a716446655440000"), "H2": mustHex("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"),
		}, nil},
		{1, map[string]any{"ID": int64(2), "B": nil, "C": nil, "V": nil, "D1": nil, "D3": nil, "DT": nil, "TS": nil, "TSL": nil, "IYM": nil, "IDS": nil, "G": nil, "H": nil}, nil},
		{2, map[string]any{
			"ID": int64(3), "B": false, "C": nil, "D1": int64(-999999999999999999), "D5": int64(-999999999), "D6": int64(-9),
			"D3": dec("-999999999999999999999999999999999999"), "D9": dec("-0.999999999999999999"), "F": -1.7e308,
			"DT": day(1, 1, 1), "TS": local(day(1, 1, 1), 0, 0, 0, 0), "TSL": time.Date(1, 1, 2, 0, 0, 0, 0, cet),
			"IDS": dbimp.Interval{Days: -999999999, Nanoseconds: -(23*3600e9 + 59*60e9 + 59e9 + 999e6)}, "G": "POINT EMPTY",
			"H": mustHex("00000000000000000000000000000000"),
		}, []string{"IYM"}},
		{3, map[string]any{
			"ID": int64(4), "B": true, "C": "zzzzz", "D1": int64(999999999999999999), "D7": dec("9999999999999999999"), "F": 1.7e308,
			"DT": day(9999, 12, 31), "TS": local(day(9999, 12, 31), 23, 59, 59, 999000000), "TS9": local(day(9999, 12, 31), 23, 59, 59, 999999000),
			"IDS": dbimp.Interval{Days: 999999999, Nanoseconds: 23*3600e9 + 59*60e9 + 59e9 + 999e6},
			"G":   "POLYGON ((0 0, 1 0, 1 1, 0 0))", "G2": "LINESTRING (0 0, 1 1)",
			"H": mustHex("ffffffffffffffffffffffffffffffff"),
		}, []string{"IYM"}},
		{4, map[string]any{"V": "héllo ☃ \"quoted\" \\\\ back\\nslash\n\ttab", "VA": "a'b", "D2": dec("0.01"), "F": 0.1}, nil},
		{5, map[string]any{"D1": int64(0), "D2": dec("0"), "D8": dec("0"), "F": 0.0}, nil},
		{7, map[string]any{"F": 5e-324}, nil},
		{9, map[string]any{"F": 123456789.12345679}, nil},
	} {
		for i, c := range cols {
			got, err := c.decode(data[tt.row][i], loc)
			if slices.Contains(tt.err, c.name) {
				if !errors.Is(err, dbimp.ErrInvalidValue) {
					t.Errorf("row %d, %s: the error is %v, want one that wraps dbimp.ErrInvalidValue (D206)", tt.row, c.name, err)
				}
				continue
			}
			if err != nil {
				t.Errorf("row %d, %s: %v", tt.row, c.name, err)
				continue
			}
			want, ok := tt.want[c.name]
			if !ok {
				continue
			}
			if show(got) != show(want) {
				t.Errorf("row %d, %s: got %s, want %s", tt.row, c.name, show(got), show(want))
			}
		}
	}
}

func mustHex(s string) []byte {
	b := make([]byte, len(s)/2)
	for i := range b {
		if _, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &b[i]); err != nil {
			panic(err)
		}
	}
	return b
}

// TestDecodeKinds holds the rule of D206 for a DECIMAL: its Go type depends on
// the column and not on the value, so a column of DECIMAL(19,0) is a decimal
// even when its value is small, and a column of DECIMAL(18,0) is an int64 (recorded:
// "select decimals by precision").
func TestDecodeKinds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		typ  dataType
		want kind
	}{
		{dataType{Type: "DECIMAL", Precision: 1}, kindInteger},
		{dataType{Type: "DECIMAL", Precision: 18}, kindInteger},
		{dataType{Type: "DECIMAL", Precision: 19}, kindDecimal},
		{dataType{Type: "DECIMAL", Precision: 9, Scale: 2}, kindDecimal},
		{dataType{Type: "DECIMAL", Precision: 36}, kindDecimal},
		{dataType{Type: "decimal", Precision: 5}, kindInteger},
		{dataType{Type: "SOMETHING NEW"}, kindOther},
	} {
		if got := columnOf(wireColumn{DataType: tt.typ}).kind; got != tt.want {
			t.Errorf("columnOf(%+v) is the kind %d, want %d", tt.typ, got, tt.want)
		}
	}
	c := columnOf(wireColumn{DataType: dataType{Type: "SOMETHING NEW"}})
	for raw, want := range map[string]string{`"text"`: "string text", `5`: "int64 5", `null`: "<nil> <nil>"} {
		got, err := c.decode(jsontext.Value(raw), nil)
		if err != nil || show(got) != want {
			t.Errorf("decode(%s) of an unknown type = %s, %v, want %s", raw, show(got), err, want)
		}
	}
}

// TestDecodeRefuses holds D206 for a value of the wrong shape, and for an
// interval that does not fit dbimp.Interval.
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		typ dataType
		raw string
	}{
		{dataType{Type: "BOOLEAN"}, `"true"`},
		{dataType{Type: "DECIMAL", Precision: 9}, `1.5`},
		{dataType{Type: "DECIMAL", Precision: 9}, `"x"`},
		{dataType{Type: "DECIMAL", Precision: 19}, `true`},
		{dataType{Type: "DOUBLE"}, `"1.5"`},
		{dataType{Type: "VARCHAR"}, `5`},
		{dataType{Type: "DATE"}, `"2026-13-01"`},
		{dataType{Type: "TIMESTAMP"}, `"yesterday"`},
		{dataType{Type: "TIMESTAMP WITH LOCAL TIME ZONE"}, `"yesterday"`},
		{dataType{Type: "INTERVAL YEAR TO MONTH"}, `"+000000005-13"`},
		{dataType{Type: "INTERVAL YEAR TO MONTH"}, `"5"`},
		{dataType{Type: "INTERVAL YEAR TO MONTH"}, `"+999999999-11"`},
		{dataType{Type: "INTERVAL YEAR TO MONTH"}, `"-999999999-11"`},
		{dataType{Type: "INTERVAL YEAR TO MONTH"}, `"+178956971-00"`},
		{dataType{Type: "INTERVAL DAY TO SECOND"}, `"+000000002 25:00:00.000000000"`},
		{dataType{Type: "INTERVAL DAY TO SECOND"}, `"2 03:04:05"`},
		{dataType{Type: "HASHTYPE"}, `"xyz"`},
	} {
		c := columnOf(wireColumn{DataType: tt.typ})
		got, err := c.decode(jsontext.Value(tt.raw), func() (*time.Location, error) { return time.UTC, nil })
		if !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("decode(%s) of %s = %v, %v, want an error that wraps dbimp.ErrInvalidValue", tt.raw, tt.typ.Type, got, err)
		}
	}
}

// TestDecodeIntervalLimit holds D206: an interval of 2147483647 months fits, and
// one more month does not.
func TestDecodeIntervalLimit(t *testing.T) {
	t.Parallel()
	got, err := parseIntervalYM("+178956970-07")
	if err != nil || got.Months != math.MaxInt32 {
		t.Errorf("parseIntervalYM of the largest = %v, %v, want %d months", got, err, math.MaxInt32)
	}
	if _, err := parseIntervalYM("+178956970-08"); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("parseIntervalYM of one month more = %v, want an error", err)
	}
	got, err = parseIntervalYM("-178956970-08")
	if err != nil || got.Months != math.MinInt32 {
		t.Errorf("parseIntervalYM of the smallest = %v, %v, want %d months", got, err, math.MinInt32)
	}
}

// TestLoadZone holds D206: the server writes the name of the zone in upper case,
// and the driver finds the zone of the database of zones.
func TestLoadZone(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"CET": "CET", "ASIA/TOKYO": "Asia/Tokyo", "AMERICA/NEW_YORK": "America/New_York", "Europe/Berlin": "Europe/Berlin",
		"AMERICA/ARGENTINA/BUENOS_AIRES": "America/Argentina/Buenos_Aires", "UTC": "UTC", "US/PACIFIC": "US/Pacific", "ETC/GMT+5": "Etc/GMT+5",
	} {
		loc, err := loadZone(name)
		if err != nil {
			t.Errorf("loadZone(%q): %v", name, err)
			continue
		}
		if loc.String() != want {
			t.Errorf("loadZone(%q) = %s, want %s", name, loc, want)
		}
	}
	for _, name := range []string{"", "NOWHERE/AT_ALL"} {
		if _, err := loadZone(name); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("loadZone(%q) = %v, want an error", name, err)
		}
	}
}

// TestBind holds D208: each Go value goes in the JSON form that the server reads
// for the declared type of the parameter (recorded: "execute the prepared insert
// with strings and numbers", "lead: an integer for a double").
func TestBind(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("CEST", 2*3600)
	loc := func() (*time.Location, error) { return zone, nil }
	col := func(typ string, dt dataType) column {
		dt.Type = typ
		return columnOf(wireColumn{DataType: dt})
	}
	d, _, _ := apd.NewFromString("12345678901234567890123456.7890123456")
	nan, _, _ := apd.NewFromString("NaN")
	_ = nan
	date := dbimp.Date{Year: 2026, Month: 10, Day: 11}
	local := dbimp.LocalDateTime{Date: date, Time: dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3, Nanosecond: 456789000}}
	instant := time.Date(2026, 10, 11, 1, 2, 3, 456789000, time.UTC)
	for _, tt := range []struct {
		c    column
		v    any
		want string
	}{
		{col("BOOLEAN", dataType{}), true, `true`},
		{col("DECIMAL", dataType{Precision: 9}), int64(-5), `"-5"`},
		{col("DECIMAL", dataType{Precision: 36}), uint64(math.MaxUint64), `"18446744073709551615"`},
		{col("DECIMAL", dataType{Precision: 10, Scale: 2}), 1.5, `"1.5"`},
		{col("DECIMAL", dataType{Precision: 36, Scale: 10}), d, `"12345678901234567890123456.7890123456"`},
		{col("DECIMAL", dataType{Precision: 36, Scale: 10}), "0.123456789012345678", `"0.123456789012345678"`},
		{col("DOUBLE", dataType{}), 1.0, `1.0`},
		{col("DOUBLE", dataType{}), 2.5, `2.5`},
		{col("DOUBLE", dataType{}), 1.5e300, `1.5e+300`},
		{col("DOUBLE", dataType{}), 1e21, `1e+21`},
		{col("DOUBLE", dataType{}), int64(3), `3.0`},
		{col("DOUBLE", dataType{}), math.SmallestNonzeroFloat64, `5e-324`},
		{col("VARCHAR", dataType{Size: 10}), "héllo \"q\"\n", `"héllo \"q\"\n"`},
		{col("VARCHAR", dataType{Size: 10}), []byte("abc"), `"abc"`},
		{col("VARCHAR", dataType{Size: 10}), int64(12345), `"12345"`},
		{col("VARCHAR", dataType{Size: 10}), 1.5, `"1.5"`},
		{col("VARCHAR", dataType{Size: 10}), true, `"true"`},
		{col("CHAR", dataType{Size: 3}), "ab", `"ab"`},
		{col("DATE", dataType{}), date, `"2026-10-11"`},
		{col("DATE", dataType{}), instant, `"2026-10-11"`},
		{col("TIMESTAMP", dataType{}), local, `"2026-10-11 01:02:03.456789"`},
		{col("TIMESTAMP", dataType{}), instant, `"2026-10-11 01:02:03.456789"`},
		{col("TIMESTAMP", dataType{}), date, `"2026-10-11 00:00:00"`},
		{col("TIMESTAMP WITH LOCAL TIME ZONE", dataType{}), instant, `"2026-10-11 03:02:03.456789"`},
		{col("TIMESTAMP WITH LOCAL TIME ZONE", dataType{}), local, `"2026-10-11 01:02:03.456789"`},
		{col("INTERVAL YEAR TO MONTH", dataType{Precision: 9}), dbimp.Interval{Months: 12*12 + 3}, `"+000000012-03"`},
		{col("INTERVAL YEAR TO MONTH", dataType{Precision: 2}), dbimp.Interval{Months: -(5*12 + 3)}, `"-05-03"`},
		{col("INTERVAL DAY TO SECOND", dataType{Precision: 9, Fraction: 9}), dbimp.Interval{Days: 2, Nanoseconds: 3*3600e9 + 4*60e9 + 5e9 + 678e6}, `"+000000002 03:04:05.678000000"`},
		{col("INTERVAL DAY TO SECOND", dataType{Precision: 3, Fraction: 3}), dbimp.Interval{Days: -2, Nanoseconds: -(3*3600e9 + 4*60e9 + 5e9)}, `"-002 03:04:05.000"`},
		{col("INTERVAL DAY TO SECOND", dataType{Precision: 3, Fraction: 0}), dbimp.Interval{Days: 1, Nanoseconds: -3600e9}, `"+000 23:00:00"`},
		{col("INTERVAL DAY TO SECOND", dataType{Precision: 3, Fraction: 0}), dbimp.Interval{Nanoseconds: 90000e9}, `"+001 01:00:00"`},
		{col("GEOMETRY", dataType{}), "POINT (3 4)", `"POINT (3 4)"`},
		{col("HASHTYPE", dataType{Size: 32}), []byte{0x55, 0x0e, 0x84, 0x00}, `"550e8400"`},
		{col("HASHTYPE", dataType{Size: 32}), "550e8400-e29b-41d4-a716-446655440000", `"550e8400-e29b-41d4-a716-446655440000"`},
		{col("DOUBLE", dataType{}), nil, `null`},
		{col("VARCHAR", dataType{}), nil, `null`},
	} {
		got, err := bindValue(tt.c, tt.v, loc)
		if err != nil {
			t.Errorf("bindValue(%s, %v): %v", tt.c.databaseType(), tt.v, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("bindValue(%s, %v) = %s, want %s", tt.c.databaseType(), tt.v, got, tt.want)
		}
	}
}

func TestBindRefuses(t *testing.T) {
	t.Parallel()
	col := func(typ string, dt dataType) column {
		dt.Type = typ
		return columnOf(wireColumn{DataType: dt})
	}
	nan, _, _ := apd.NewFromString("NaN")
	for _, tt := range []struct {
		c    column
		v    any
		want error
	}{
		{col("BOOLEAN", dataType{}), "true", dbimp.ErrNotSupported},
		{col("BOOLEAN", dataType{}), int64(1), dbimp.ErrNotSupported},
		{col("DECIMAL", dataType{Precision: 9}), math.NaN(), dbimp.ErrInvalidValue},
		{col("DECIMAL", dataType{Precision: 9}), math.Inf(1), dbimp.ErrInvalidValue},
		{col("DECIMAL", dataType{Precision: 9}), nan, dbimp.ErrInvalidValue},
		{col("DECIMAL", dataType{Precision: 9}), "abc", dbimp.ErrInvalidValue},
		{col("DECIMAL", dataType{Precision: 9}), true, dbimp.ErrNotSupported},
		{col("DOUBLE", dataType{}), math.NaN(), dbimp.ErrInvalidValue},
		{col("DOUBLE", dataType{}), math.Inf(-1), dbimp.ErrInvalidValue},
		{col("DOUBLE", dataType{}), "x", dbimp.ErrInvalidValue},
		{col("VARCHAR", dataType{}), []byte{0xff}, dbimp.ErrInvalidValue},
		{col("VARCHAR", dataType{}), struct{}{}, dbimp.ErrNotSupported},
		{col("DATE", dataType{}), int64(1), dbimp.ErrNotSupported},
		{col("TIMESTAMP", dataType{}), true, dbimp.ErrNotSupported},
		{col("INTERVAL YEAR TO MONTH", dataType{Precision: 9}), dbimp.Interval{Months: 1, Days: 1}, dbimp.ErrInvalidValue},
		{col("INTERVAL DAY TO SECOND", dataType{Precision: 9}), dbimp.Interval{Months: 1}, dbimp.ErrInvalidValue},
		{col("INTERVAL DAY TO SECOND", dataType{Precision: 9}), int64(1), dbimp.ErrNotSupported},
		{col("HASHTYPE", dataType{}), int64(1), dbimp.ErrNotSupported},
		{col("GEOMETRY", dataType{}), int64(1), dbimp.ErrNotSupported},
	} {
		if _, err := bindValue(tt.c, tt.v, func() (*time.Location, error) { return time.UTC, nil }); !errors.Is(err, tt.want) {
			t.Errorf("bindValue(%s, %v) = %v, want %v", tt.c.databaseType(), tt.v, err, tt.want)
		}
	}
}

// TestBindCounts holds D208: the number of arguments must be the number of
// placeholders, and the server takes no named parameter (recorded: "prepare with
// a named parameter").
func TestBindCounts(t *testing.T) {
	t.Parallel()
	cols := []column{columnOf(wireColumn{DataType: dataType{Type: "VARCHAR"}})}
	loc := func() (*time.Location, error) { return time.UTC, nil }
	if _, err := bind(cols, nil, loc); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("bind with no argument = %v, want %v", err, dbimp.ErrArguments)
	}
	two := []driver.NamedValue{{Ordinal: 1, Value: "a"}, {Ordinal: 2, Value: "b"}}
	if _, err := bind(cols, two, loc); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("bind with two arguments = %v, want %v", err, dbimp.ErrArguments)
	}
	if _, err := bind(cols, []driver.NamedValue{{Name: "p", Ordinal: 1, Value: "a"}}, loc); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("bind with a named argument = %v, want %v", err, dbimp.ErrNotSupported)
	}
	got, err := bind(cols, two[:1], loc)
	if err != nil || len(got) != 1 || string(got[0]) != `["a"]` {
		t.Errorf("bind of one argument = %s, %v, want [\"a\"]", got, err)
	}
}

// TestFormatIntervalRoundTrip makes sure that what the driver writes for an
// interval is what it reads.
func TestFormatIntervalRoundTrip(t *testing.T) {
	t.Parallel()
	for _, iv := range []dbimp.Interval{{Months: 63}, {Months: -63}, {Months: 0}, {Months: math.MaxInt32}, {Months: math.MinInt32}} {
		s, err := formatIntervalYM(iv, 9)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseIntervalYM(s)
		if err != nil || got != iv {
			t.Errorf("year to month %v wrote %s and read %v, %v", iv, s, got, err)
		}
	}
	for _, iv := range []dbimp.Interval{{Days: 2, Nanoseconds: 3e9}, {Days: -2, Nanoseconds: -3e9}, {Days: 999999999, Nanoseconds: nanosPerDay - 1e6}, {}, {Nanoseconds: -1e6}} {
		s, err := formatIntervalDS(iv, 9, 9)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseIntervalDS(s)
		if err != nil || got != iv {
			t.Errorf("day to second %v wrote %s and read %v, %v", iv, s, got, err)
		}
	}
}

// TestTypesFileIsRecorded makes sure that the recording that the tests read is
// there, so a rename shows here and not as a confusing failure.
func TestTypesFileIsRecorded(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(filepath.Clean(typesFile)); err != nil {
		t.Fatal(err)
	}
}
