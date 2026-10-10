package databricks //nolint:testpackage // The tests read the type parser and the decoder, which are not exported.

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestParseType holds that the parser reads type_text as Spark writes it, and
// that the type that it makes has the name, the kind, the precision and the scale
// that the table of types names (measured).
func TestParseType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		text      string
		name      string
		kind      kind
		precision int
		scale     int
	}{
		{"INT", "INT", kindInteger, -1, -1},
		{"TINYINT", "TINYINT", kindInteger, -1, -1},
		{"BIGINT", "BIGINT", kindInteger, -1, -1},
		{"FLOAT", "FLOAT", kindFloat, -1, -1},
		{"DECIMAL(38,18)", "DECIMAL", kindDecimal, 38, 18},
		{"DECIMAL(10, 2)", "DECIMAL", kindDecimal, 10, 2},
		{"DECIMAL", "DECIMAL", kindDecimal, -1, -1},
		{"BOOLEAN", "BOOLEAN", kindBoolean, -1, -1},
		{"STRING", "STRING", kindString, -1, -1},
		{"STRING COLLATE UTF8_BINARY", "STRING", kindString, -1, -1},
		{"VARCHAR(10)", "VARCHAR", kindString, -1, -1},
		{"CHAR(3)", "CHAR", kindString, -1, -1},
		{"BINARY", "BINARY", kindBinary, -1, -1},
		{"DATE", "DATE", kindDate, -1, -1},
		{"TIMESTAMP", "TIMESTAMP", kindTimestamp, -1, -1},
		{"TIMESTAMP_NTZ", "TIMESTAMP_NTZ", kindLocalTimestamp, -1, -1},
		{"INTERVAL YEAR TO MONTH", "INTERVAL", kindYearMonth, -1, -1},
		{"INTERVAL YEAR", "INTERVAL", kindYearMonth, -1, -1},
		{"INTERVAL MONTH", "INTERVAL", kindYearMonth, -1, -1},
		{"INTERVAL DAY TO SECOND", "INTERVAL", kindDaySecond, -1, -1},
		{"INTERVAL HOUR", "INTERVAL", kindDaySecond, -1, -1},
		{"ARRAY<INT>", "ARRAY", kindArray, -1, -1},
		{"ARRAY<VOID>", "ARRAY", kindArray, -1, -1},
		{"ARRAY<INTERVAL YEAR TO MONTH>", "ARRAY", kindArray, -1, -1},
		{"MAP<STRING, INT>", "MAP", kindMap, -1, -1},
		{"MAP<STRING,ARRAY<INT>>", "MAP", kindMap, -1, -1},
		{"STRUCT<a: INT NOT NULL, b: STRING NOT NULL>", "STRUCT", kindStruct, -1, -1},
		{"STRUCT<>", "STRUCT", kindStruct, -1, -1},
		{"ARRAY<STRUCT<a: MAP<STRING, ARRAY<INT>> NOT NULL>>", "ARRAY", kindArray, -1, -1},
		{"VARIANT", "VARIANT", kindVariant, -1, -1},
		{"GEOMETRY(0)", "GEOMETRY", kindString, -1, -1},
		{"GEOGRAPHY(4326)", "GEOGRAPHY", kindString, -1, -1},
		{"VOID", "VOID", kindVoid, -1, -1},
		{"SOMETHING_NEW", "SOMETHING_NEW", kindString, -1, -1},
		{"STRUCT<`a b`: INT, `c``d`: STRING COMMENT 'it''s'>", "STRUCT", kindStruct, -1, -1},
	} {
		got, err := parseType(tt.text)
		if err != nil {
			t.Errorf("parseType(%q): %v", tt.text, err)
			continue
		}
		if got.name != tt.name || got.kind != tt.kind || got.precision != tt.precision || got.scale != tt.scale {
			t.Errorf("parseType(%q) = %s, kind %d, (%d,%d), want %s, kind %d, (%d,%d)", tt.text, got.name, got.kind, got.precision, got.scale, tt.name, tt.kind, tt.precision, tt.scale)
		}
	}
	deep, err := parseType("ARRAY<STRUCT<a: MAP<STRING, ARRAY<INT>> NOT NULL>>")
	if err != nil {
		t.Fatal(err)
	}
	if f := deep.elem.fields; len(f) != 1 || f[0].name != "a" || f[0].typ.kind != kindMap || f[0].typ.elem.kind != kindArray || f[0].typ.elem.elem.kind != kindInteger {
		t.Errorf("the type of the deep array is %+v", deep.elem)
	}
	names, err := parseType("STRUCT<`a b`: INT, `c``d`: STRING>")
	if err != nil {
		t.Fatal(err)
	}
	if len(names.fields) != 2 || names.fields[0].name != "a b" || names.fields[1].name != "c`d" {
		t.Errorf("the fields are %+v, want a b and c`d", names.fields)
	}
}

// TestParseTypeRefuses holds that a type that does not parse is an error that
// wraps dbimp.ErrInvalidValue, and never a panic.
func TestParseTypeRefuses(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"", "<", "ARRAY", "ARRAY<", "ARRAY<INT", "ARRAY<INT>>", "MAP<STRING>", "MAP<STRING INT>", "STRUCT<a INT>", "STRUCT<a: >",
		"STRUCT<a: INT", "STRUCT<`a: INT>", "DECIMAL(", "DECIMAL(10,", "DECIMAL(a)", "DECIMAL(10;2)", "INT)", "ARRAY<INT> x>",
	} {
		if got, err := parseType(text); err == nil || !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseType(%q) = %+v, %v, want an error that wraps dbimp.ErrInvalidValue", text, got, err)
		}
	}
}

// FuzzParseType holds that the parser never panics, whatever the text.
func FuzzParseType(f *testing.F) {
	for _, text := range []string{
		"INT", "DECIMAL(10,2)", "ARRAY<STRUCT<a: MAP<STRING, ARRAY<INT>> NOT NULL>>", "INTERVAL DAY TO SECOND",
		"STRUCT<`a b`: INT COMMENT 'x'>", "MAP<STRING, INT>", "<<<", "ARRAY<", "STRUCT<a:", "`", "'",
	} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		got, err := parseType(text)
		if err != nil {
			return
		}
		// A type that parses has a name, and a value of null decodes to nil.
		if got.name == "" {
			t.Fatalf("parseType(%q) has no name", text)
		}
		if v, err := decode(got, jsontext.Value("null")); v != nil || err != nil {
			t.Fatalf("decode of null with %q gave %v and %v", text, v, err)
		}
	})
}

// decodeText decodes the text s as the value of a column of the type text.
func decodeText(t *testing.T, text, s string) (any, error) {
	t.Helper()
	typ, err := parseType(text)
	if err != nil {
		t.Fatalf("parseType(%q): %v", text, err)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return decode(typ, b)
}

// TestDecode holds D135 and D193: the text of each type is its Go value, for
// forms that the recordings hold and for the edges of each range.
func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		text, in string
		want     any
	}{
		{"INT", "-2147483648", int64(math.MinInt32)},
		{"BIGINT", "9223372036854775807", int64(math.MaxInt64)},
		{"FLOAT", "1.0E-4", 0.0001},
		{"DOUBLE", "-0.0", math.Copysign(0, -1)},
		{"DOUBLE", "1.7976931348623157E308", math.MaxFloat64},
		{"DECIMAL(5,2)", "-12.30", dec(t, "-12.30")},
		{"BOOLEAN", "true", true},
		{"STRING", "", ""},
		{"BINARY", "", []byte{}},
		{"BINARY", "AA==", []byte{0}},
		{"DATE", "0001-01-01", dbimp.Date{Year: 1, Month: time.January, Day: 1}},
		{"TIMESTAMP", "2024-01-02T03:04:05.123Z", utc(2024, time.January, 2, 3, 4, 5, 123000000)},
		{"TIMESTAMP", "2024-01-02T03:04:05Z", utc(2024, time.January, 2, 3, 4, 5, 0)},
		{"TIMESTAMP", "9999-12-31T23:59:59.999Z", utc(9999, time.December, 31, 23, 59, 59, 999000000)},
		{"TIMESTAMP_NTZ", "2024-01-02T03:04:05.123", local(2024, time.January, 2, 3, 4, 5, 123000000)},
		{"VARIANT", `{"a":[1,2.5,"x",null,true]}`, map[string]any{"a": []any{int64(1), 2.5, "x", nil, true}}},
		{"VARIANT", "null", nil},
		{"VARIANT", "9007199254740993", int64(9007199254740993)},
		{"VOID", "x", nil},
		{"ARRAY<INT>", `["1","2"]`, []any{int64(1), int64(2)}},
		{"ARRAY<ARRAY<STRING>>", `[["a"],[]]`, []any{[]any{"a"}, []any{}}},
		{"MAP<STRING, DECIMAL(3,1)>", `{"k":"1.5"}`, map[string]any{"k": dec(t, "1.5")}},
		{"STRUCT<a: INT, b: ARRAY<BINARY>>", `{"a":null,"b":["AA=="]}`, map[string]any{"a": nil, "b": []any{[]byte{0}}}},
		{"STRUCT<`a b`: INT>", `{"a b":"1"}`, map[string]any{"a b": int64(1)}},
		{"ARRAY<VARIANT>", `[{"a":1},"x"]`, []any{map[string]any{"a": int64(1)}, "x"}},
		{"CHAR(3)", "ab ", "ab "},
		{"SOMETHING_NEW", "text", "text"},
	} {
		got, err := decodeText(t, tt.text, tt.in)
		if err != nil {
			t.Errorf("%s %q: %v", tt.text, tt.in, err)
			continue
		}
		if !same(got, tt.want) {
			t.Errorf("%s %q gave %#v (%T), want %#v (%T)", tt.text, tt.in, got, got, tt.want, tt.want)
		}
	}
	// A value that is a JSON number or a JSON boolean, and not a string, is
	// refused: no recording holds one.
	typ, err := parseType("INT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decode(typ, jsontext.Value("1")); err == nil {
		t.Error("a JSON number for an INT column gave no error")
	}
}

// TestDecodeRefuses holds that a value that does not fit its type is an error
// that wraps dbimp.ErrInvalidValue, and never a wrong value (D135).
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ text, in string }{
		{"INT", "abc"},
		{"INT", "1.5"},
		{"BIGINT", "9223372036854775808"},
		{"INT", ""},
		{"FLOAT", "x"},
		{"DECIMAL(5,2)", "abc"},
		{"DECIMAL(5,2)", "NaN"},
		{"BOOLEAN", "1"},
		{"BOOLEAN", "TRUE"},
		{"BINARY", "***"},
		{"BINARY", "AA"},
		{"DATE", "2024-02-30"},
		{"DATE", "yesterday"},
		{"TIMESTAMP", "2024-01-02 03:04:05"},
		{"TIMESTAMP", "x"},
		{"TIMESTAMP_NTZ", "x"},
		{"INTERVAL YEAR TO MONTH", "1-2"},
		{"INTERVAL YEAR TO MONTH", "INTERVAL '1-x' YEAR TO MONTH"},
		{"INTERVAL DAY TO SECOND", "INTERVAL '1 02:03:04' MINUTE TO SECOND EXTRA"},
		{"INTERVAL DAY TO SECOND", "INTERVAL 5 DAY"},
		{"INTERVAL YEAR TO MONTH", "INTERVAL '178956970-8' YEAR TO MONTH"},
		{"INTERVAL YEAR TO MONTH", "INTERVAL '-178956970-9' YEAR TO MONTH"},
		{"INTERVAL DAY TO SECOND", "INTERVAL '1.5 02' DAY TO HOUR"},
		{"INTERVAL DAY TO SECOND", "INTERVAL '1 02:03:04:05' DAY TO SECOND"},
		{"INTERVAL DAY TO SECOND", "INTERVAL '1 02:03.5:04' DAY TO SECOND"},
		{"VARIANT", "{"},
		{"ARRAY<INT>", "not json"},
		{"ARRAY<INT>", `{"a":"1"}`},
		{"ARRAY<INT>", `[1]`},
		{"ARRAY<INT>", `["x"]`},
		{"MAP<STRING, INT>", `["1"]`},
		{"STRUCT<a: INT>", `{"b":"1"}`},
		{"STRUCT<a: INT>", `[]`},
	} {
		got, err := decodeText(t, tt.text, tt.in)
		if err == nil || !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("%s %q gave %#v and %v, want an error that wraps dbimp.ErrInvalidValue", tt.text, tt.in, got, err)
		}
	}
}

// TestParseInterval holds the forms of an interval that the server writes, with
// the field of each (measured for the first nine).
func TestParseInterval(t *testing.T) {
	t.Parallel()
	hms := int64(2*time.Hour + 3*time.Minute + 4*time.Second)
	for _, tt := range []struct {
		in   string
		want dbimp.Interval
	}{
		{"INTERVAL '1-2' YEAR TO MONTH", dbimp.Interval{Months: 14}},
		{"INTERVAL '-1-2' YEAR TO MONTH", dbimp.Interval{Months: -14}},
		{"INTERVAL '5' YEAR", dbimp.Interval{Months: 60}},
		{"INTERVAL '3' MONTH", dbimp.Interval{Months: 3}},
		{"INTERVAL '1 02:03:04.123456' DAY TO SECOND", dbimp.Interval{Days: 1, Nanoseconds: hms + 123456000}},
		{"INTERVAL '-1 02:03:04.5' DAY TO SECOND", dbimp.Interval{Days: -1, Nanoseconds: -(hms + 500000000)}},
		{"INTERVAL '5' DAY", dbimp.Interval{Days: 5}},
		{"INTERVAL '10' HOUR", dbimp.Interval{Nanoseconds: int64(10 * time.Hour)}},
		{"INTERVAL '30' MINUTE", dbimp.Interval{Nanoseconds: int64(30 * time.Minute)}},
		{"INTERVAL '01.5' SECOND", dbimp.Interval{Nanoseconds: 1500000000}},
		{"INTERVAL '1 02' DAY TO HOUR", dbimp.Interval{Days: 1, Nanoseconds: int64(2 * time.Hour)}},
		{"INTERVAL '1 02:03' DAY TO MINUTE", dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute)}},
		{"INTERVAL '02:03' HOUR TO MINUTE", dbimp.Interval{Nanoseconds: int64(2*time.Hour + 3*time.Minute)}},
		{"INTERVAL '02:03:04.25' HOUR TO SECOND", dbimp.Interval{Nanoseconds: hms + 250000000}},
		{"INTERVAL '03:04.5' MINUTE TO SECOND", dbimp.Interval{Nanoseconds: int64(3*time.Minute + 4*time.Second + 500*time.Millisecond)}},
		{"INTERVAL -'1 02:03:04' DAY TO SECOND", dbimp.Interval{Days: -1, Nanoseconds: -hms}},
		{"INTERVAL '-5' YEAR", dbimp.Interval{Months: -60}},
		{"INTERVAL '-178956970-8' YEAR TO MONTH", dbimp.Interval{Months: math.MinInt32}},
		{"INTERVAL '178956970-7' YEAR TO MONTH", dbimp.Interval{Months: math.MaxInt32}},
		{"INTERVAL '-2147483648' MONTH", dbimp.Interval{Months: math.MinInt32}},
		{"INTERVAL '-106751991 04:00:54.775808' DAY TO SECOND", dbimp.Interval{Days: -106751991, Nanoseconds: -int64(4*time.Hour + 54*time.Second + 775808*time.Microsecond)}},
		{"INTERVAL '106751991 04:00:54.775807' DAY TO SECOND", dbimp.Interval{Days: 106751991, Nanoseconds: int64(4*time.Hour + 54*time.Second + 775807*time.Microsecond)}},
	} {
		got, err := parseInterval(tt.in)
		if err != nil {
			t.Errorf("%s: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

// TestScanTypes holds that each kind has the scan type that the table of types
// names, and that the database type of a column is the name of its type.
func TestScanTypes(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		text string
		scan string
		db   string
	}{
		{"INT", "int64", "INT"},
		{"DECIMAL(10,2)", "*apd.Decimal", "DECIMAL"},
		{"BINARY", "[]uint8", "BINARY"},
		{"TIMESTAMP", "time.Time", "TIMESTAMP"},
		{"TIMESTAMP_NTZ", "dbimp.LocalDateTime", "TIMESTAMP_NTZ"},
		{"INTERVAL DAY TO SECOND", "dbimp.Interval", "INTERVAL"},
		{"ARRAY<INT>", "[]interface {}", "ARRAY"},
		{"MAP<STRING, INT>", "map[string]interface {}", "MAP"},
		{"STRUCT<a: INT>", "map[string]interface {}", "STRUCT"},
		{"VARIANT", "interface {}", "VARIANT"},
		{"VOID", "interface {}", "VOID"},
		{"GEOMETRY(0)", "string", "GEOMETRY"},
	} {
		c, err := newColumn(manifestColumn{Name: "c", TypeText: tt.text})
		if err != nil {
			t.Errorf("%s: %v", tt.text, err)
			continue
		}
		r := &rows{cols: []column{c}}
		if got := r.ColumnTypeScanType(0).String(); got != tt.scan {
			t.Errorf("%s: the scan type is %s, want %s", tt.text, got, tt.scan)
		}
		if got := r.ColumnTypeDatabaseTypeName(0); got != tt.db {
			t.Errorf("%s: the database type is %s, want %s", tt.text, got, tt.db)
		}
	}
	// A manifest with no type_text names the type in type_name, whose names for
	// the integers and for NULL differ (measured).
	for name, want := range map[string]string{"BYTE": "TINYINT", "SHORT": "SMALLINT", "LONG": "BIGINT", "NULL": "VOID", "INT": "INT"} {
		c, err := newColumn(manifestColumn{Name: "c", TypeName: name})
		if err != nil || c.databaseType() != want {
			t.Errorf("type_name %s gave %v and %v, want %s", name, c.databaseType(), err, want)
		}
	}
	r := &rows{cols: []column{{typ: &typ{kind: kindDecimal, name: "DECIMAL", precision: 10, scale: 2}}, {typ: &typ{kind: kindString, name: "STRING", precision: -1, scale: -1}}}}
	if p, s, ok := r.ColumnTypePrecisionScale(0); !ok || p != 10 || s != 2 {
		t.Errorf("the precision and the scale are %d, %d, %v, want 10, 2, true", p, s, ok)
	}
	if _, _, ok := r.ColumnTypePrecisionScale(1); ok {
		t.Error("a string column has a precision")
	}
}
