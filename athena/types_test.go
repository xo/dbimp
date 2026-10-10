package athena //nolint:testpackage // The tests decode values with the types of the driver.

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// typeCases are the statements of the recording whose result is one value, with
// the value as show writes it. The number is the file of the StartQueryExecution
// of the statement, and the text of the statement comes from that file, so that
// no test holds an answer that it wrote (the recorded name of each is "the type
// of <expression>").
var typeCases = []struct {
	file int
	want string
}{
	{67, "int64(1)"},
	{70, "int64(1)"},
	{73, "int64(1)"},
	{76, "int64(9223372036854775807)"},
	{79, "float64(1.5)"},
	{82, "float64(1.5)"},
	{85, "*apd.Decimal(12345678.91)"},
	{88, `string("héllo")`},
	{91, `string("ab  ")`},
	{94, "bool(true)"},
	{97, "dbimp.Date(2026-10-10)"},
	{100, "dbimp.LocalTime(12:34:56.123)"},
	{103, "dbimp.LocalDateTime(2026-10-10T12:34:56.123)"},
	{106, "time.Time(2026-10-10T12:34:56.123+07:00 in )"},
	{109, "[]byte(deadbeef)"},
	{112, `string("[1, 2, 3]")`},
	{115, `string("{a=1, b=2}")`},
	{118, `string("{a=1, b=x}")`},
	{121, "map[string]interface {}(map[a:1])"},
	{124, "netip.Addr(2001:db8::1)"},
	{127, "uuid.UUID(12345678-1234-5678-1234-567812345678)"},
	{130, "dbimp.Interval(P1D)"},
	{133, "dbimp.Interval(P1Y)"},
	{136, "nil"},
	{139, "float64(NaN)"},
	{142, "float64(+Inf)"},
	{145, "float64(-Inf)"},
	{148, "dbimp.Date(0001-01-01)"},
	{151, "dbimp.Date(9999-12-31)"},
	{157, `string("POINT (1 2)")`},
	{269, "dbimp.OffsetTime(12:34:56+07:00)"},
	{272, "time.Time(2026-10-10T12:34:56+02:00 in Europe/Paris)"},
	{275, "time.Time(2026-10-10T21:34:56+09:00 in Asia/Tokyo)"},
}

// TestReplayTypes reads one value of each type that the recording holds, through
// Rows.Scan into a *any, and compares the value with its Go type to the type
// table (D135 and D192).
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range typeCases {
		query := queryOf(t, tt.file)
		t.Run(strings.Fields(query)[0]+" "+tt.want, func(t *testing.T) {
			t.Parallel()
			cols, rows, err := read(t, db, query)
			if err != nil {
				t.Fatal(err)
			}
			if len(cols) != 1 || len(rows) != 1 {
				t.Fatalf("%q gave the columns %q and %d rows, want one column and one row", query, cols, len(rows))
			}
			if rows[0][0] != tt.want {
				t.Errorf("%q gave %s, want %s", query, rows[0][0], tt.want)
			}
		})
	}
}

// TestReplayEveryTypeInOneRow reads the row that holds a value of each type
// (recorded: "every type in one row"), and compares each Go type with the type
// table.
func TestReplayEveryTypeInOneRow(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, rows, err := read(t, db, queryOf(t, 160))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want one", len(rows))
	}
	want := []string{
		"int64(1)", "int64(1)", "int64(1)", "int64(9223372036854775807)", "float64(1.5)", "float64(1.5)",
		"*apd.Decimal(12345678.91)", `string("héllo")`, `string("ab  ")`, "bool(true)", "dbimp.Date(2026-10-10)",
		"dbimp.LocalTime(12:34:56.123)", "dbimp.LocalDateTime(2026-10-10T12:34:56.123)",
		"time.Time(2026-10-10T12:34:56.123+07:00 in )", "[]byte(deadbeef)", `string("[1, 2, 3]")`,
		`string("{a=1, b=2}")`, `string("{a=1, b=x}")`, "map[string]interface {}(map[a:1])", "netip.Addr(2001:db8::1)",
		"uuid.UUID(12345678-1234-5678-1234-567812345678)", "dbimp.Interval(P1D)", "dbimp.Interval(P1Y)", "nil",
	}
	for i, w := range want {
		if rows[0][i] != w {
			t.Errorf("column %d is %s, want %s", i, rows[0][i], w)
		}
	}
}

// TestReplayContainersAreText reads an array, a map and a row of values that
// hold a comma and a quote. The text has no escape, so the driver returns it as
// the server wrote it, and does not parse it (recorded: "commas and quotes in an
// array, a map and a row", D192 item 3).
func TestReplayContainersAreText(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, rows, err := read(t, db, queryOf(t, 410))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`string("[a,b, c\"d]")`, `string("{k,1=v\"2}")`, `string("{a=x,y, b=z\"w}")`}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want one", len(rows))
	}
	for i, w := range want {
		if rows[0][i] != w {
			t.Errorf("column %d is %s, want %s", i, rows[0][i], w)
		}
	}
}

// TestDecode decodes the texts that the recording does not hold, from the rules
// of docs/ATHENA.md: the forms that the page names as not measured, and the
// forms of the types that the recording names with a literal only.
func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		wire, text, want string
	}{
		{typeSmallint, "-32768", "int64(-32768)"},
		{typeBigint, "-9223372036854775808", "int64(-9223372036854775808)"},
		{typeDouble, "1E-10", "float64(1e-10)"},
		{typeFloat, "NaN", "float64(NaN)"},
		{typeDecimal, "-0.5", "*apd.Decimal(-0.5)"},
		{typeDecimal, "12345678901234567890123456789012345678", "*apd.Decimal(12345678901234567890123456789012345678)"},
		{typeBoolean, "false", "bool(false)"},
		{typeTime, "12:34:56", "dbimp.LocalTime(12:34:56)"},
		{typeTimeTZ, "12:34:56.123+07:00", "dbimp.OffsetTime(12:34:56.123+07:00)"},
		{typeTimeTZ, "12:34:56Z", "dbimp.OffsetTime(12:34:56Z)"},
		{typeTimestamp, "2026-10-10 01:02:03", "dbimp.LocalDateTime(2026-10-10T01:02:03)"},
		{typeTimestamp, "0001-01-01 00:00:00.000", "dbimp.LocalDateTime(0001-01-01T00:00:00)"},
		{typeTimestTZ, "2026-10-10 12:34:56 +07:00", "time.Time(2026-10-10T12:34:56+07:00 in )"},
		{typeTimestTZ, "2026-10-10 12:34:56.5 -03:30", "time.Time(2026-10-10T12:34:56.5-03:30 in )"},
		{typeTimestTZ, "2026-10-10 12:34:56.123 UTC", "time.Time(2026-10-10T12:34:56.123Z in UTC)"},
		{typeTimestTZ, "2026-10-10 12:34:56 +00:00", "time.Time(2026-10-10T12:34:56Z in UTC)"},
		{typeVarbinary, "", "[]byte()"},
		{typeVarbinary, "00 ff", "[]byte(00ff)"},
		{typeJSON, `[1,"a",null]`, "[]interface {}([1 a <nil>])"},
		{typeIPAddress, "10.0.0.1", "netip.Addr(10.0.0.1)"},
		{typeIntervalDS, "-1 02:03:04.005", "dbimp.Interval(P-1DT-2H-3M-4.005S)"},
		{typeIntervalYM, "-1-2", "dbimp.Interval(P-1Y-2M)"},
		{typeIntervalYM, "10-0", "dbimp.Interval(P10Y)"},
		{typeUnknown, "", "nil"},
		{"hyperloglog", "abc", `string("abc")`},
	} {
		got, err := column{Type: tt.wire}.decode(tt.text)
		if err != nil {
			t.Errorf("decode(%s, %q): %v", tt.wire, tt.text, err)
			continue
		}
		if s := show(got); s != tt.want {
			t.Errorf("decode(%s, %q) = %s, want %s", tt.wire, tt.text, s, tt.want)
		}
	}
}

// TestDecodeRefuses makes sure that a text that does not fit its type is an
// error that wraps dbimp.ErrInvalidValue, and not a zero value (D8).
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ wire, text string }{
		{typeInteger, "1.5"},
		{typeBigint, "9223372036854775808"},
		{typeDouble, "one"},
		{typeDecimal, "NaN"},
		{typeBoolean, "1"},
		{typeDate, "2026-13-01"},
		{typeTime, "25:00:00"},
		{typeTimeTZ, "12:34:56"},
		{typeTimestamp, "2026-10-10"},
		{typeTimestTZ, "2026-10-10 12:34:56"},
		{typeTimestTZ, "2026-10-10 12:34:56 Mars/Olympus"},
		{typeTimestTZ, "2026-10-10 12:34:56 "},
		{typeVarbinary, "de a"},
		{typeJSON, "{a"},
		{typeIPAddress, "1.2.3"},
		{typeUUID, "1234"},
		{typeIntervalDS, "1 99:00:00"},
		{typeIntervalDS, "1"},
		{typeIntervalYM, "1"},
		{typeIntervalYM, "1-x"},
	} {
		if got, err := (column{Type: tt.wire}).decode(tt.text); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("decode(%s, %q) = %v, %v, want dbimp.ErrInvalidValue", tt.wire, tt.text, got, err)
		}
	}
}

// TestNaNIsNaN holds that the text NaN is the float NaN, which is not equal to
// itself (recorded: "the type of nan()").
func TestNaNIsNaN(t *testing.T) {
	t.Parallel()
	got, err := column{Type: typeDouble}.decode("NaN")
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := got.(float64); !ok || !math.IsNaN(f) {
		t.Errorf("got %v, want NaN", got)
	}
}

// TestInstantKeepsItsZone makes sure that a time with a named zone is in that
// zone, and that an offset is a fixed zone with no name (D192 item 7).
func TestInstantKeepsItsZone(t *testing.T) {
	t.Parallel()
	got, err := parseInstant("2026-10-10 12:34:56.123 Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	if got.Location().String() != "Europe/Paris" {
		t.Errorf("the zone is %s, want Europe/Paris", got.Location())
	}
	if _, off := got.Zone(); off != 2*3600 {
		t.Errorf("the offset is %d, want 7200", off)
	}
	want := time.Date(2026, 10, 10, 10, 34, 56, 123e6, time.UTC)
	if !got.Equal(want) {
		t.Errorf("the instant is %s, want %s", got.UTC(), want)
	}
	got, err = parseInstant("2026-10-10 12:34:56 +07:00")
	if err != nil {
		t.Fatal(err)
	}
	if _, off := got.Zone(); off != 7*3600 || got.Location().String() != "" {
		t.Errorf("the zone is %q at %d, want a fixed zone at 25200", got.Location(), off)
	}
}

// TestScanTypeIsTheGoType holds that the scan type of a column is the type of
// the value that Rows.Next returns, for each type of the table (D135 and D136).
func TestScanTypeIsTheGoType(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ wire, text string }{
		{typeTinyint, "1"}, {typeSmallint, "1"}, {typeInteger, "1"}, {typeBigint, "1"},
		{typeFloat, "1.5"}, {typeDouble, "1.5"}, {typeDecimal, "1.5"}, {typeVarchar, "a"}, {typeChar, "a"},
		{typeString, "a"}, {typeBoolean, "true"}, {typeDate, "2026-10-10"}, {typeTime, "12:34:56"},
		{typeTimeTZ, "12:34:56+07:00"}, {typeTimestamp, "2026-10-10 12:34:56"},
		{typeTimestTZ, "2026-10-10 12:34:56 +07:00"}, {typeVarbinary, "de ad"}, {typeIPAddress, "::1"},
		{typeUUID, "12345678-1234-5678-1234-567812345678"}, {typeIntervalDS, "1 00:00:00.000"},
		{typeIntervalYM, "1-0"}, {typeGeometry, "POINT (1 2)"}, {typeArray, "[1]"}, {typeMap, "{a=1}"},
		{typeRow, "{a=1}"},
	} {
		col := column{Type: c.wire}
		v, err := col.decode(c.text)
		if err != nil {
			t.Errorf("%s: %v", c.wire, err)
			continue
		}
		if got, want := reflect.TypeOf(v), col.scanType(); got != want {
			t.Errorf("%s: the value is a %s, and the scan type is %s", c.wire, got, want)
		}
	}
	// A value of an unknown or JSON column is the decoded value, whose scan type
	// is any.
	for _, wire := range []string{typeJSON, typeUnknown} {
		if got := (column{Type: wire}).scanType().String(); got != "interface {}" {
			t.Errorf("%s: the scan type is %s, want interface {}", wire, got)
		}
	}
}
