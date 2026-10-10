package snowflake //nolint:testpackage // The tests call the functions that decode a value, which are not exported.

import (
	"encoding/json/jsontext"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// col returns a column of the wire type, with the precision and the scale.
func col(wire string, precision, scale int64) column {
	return column{wire: wire, precision: precision, scale: scale, length: -1, nullable: true}
}

// quoted writes s as a JSON string.
func quoted(s string) jsontext.Value {
	b, err := jsontext.AppendQuote(nil, s)
	if err != nil {
		panic(err)
	}
	return b
}

// TestDecode holds D183 item 4 for the edges of each type that the recordings
// do not show.
func TestDecode(t *testing.T) {
	t.Parallel()
	local := func(sec, nanos int64) dbimp.LocalDateTime { return dbimp.LocalDateTimeOf(time.Unix(sec, nanos).UTC()) }
	for _, tt := range []struct {
		name string
		c    column
		in   string
		want any
	}{
		{"an integer of 18 digits", col(wireFixed, 18, 0), "-999999999999999999", int64(-999999999999999999)},
		{"an integer of 1 digit", col(wireFixed, 1, 0), "0", int64(0)},
		{"a number with a scale is a decimal", col(wireFixed, 10, 2), "0.10", dec(t, "0.10")},
		{"a number of 19 digits is a decimal", col(wireFixed, 19, 0), "9223372036854775807", dec(t, "9223372036854775807")},
		{"a number with no precision is a decimal", col(wireFixed, -1, -1), "5", dec(t, "5")},
		{"a negative decimal", col(wireFixed, 38, 0), "-12345678901234567890123456789012345678", dec(t, "-12345678901234567890123456789012345678")},
		{"a float", col(wireReal, -1, -1), "-1.5e300", -1.5e300},
		{"an empty text", col(wireText, -1, -1), "", ""},
		{"text of a number", col(wireText, -1, -1), "42", "42"},
		{"empty binary", col(wireBinary, -1, -1), "", []byte{}},
		{"lower case binary", col(wireBinary, -1, -1), "deadbeef", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"false", col(wireBoolean, -1, -1), "false", false},
		{"a boolean as 1", col(wireBoolean, -1, -1), "1", true},
		{"a boolean as 0", col(wireBoolean, -1, -1), "0", false},
		{"the first date", col(wireDate, -1, -1), "-719162", dbimp.Date{Year: 1, Month: time.January, Day: 1}},
		{"the last date", col(wireDate, -1, -1), "2932896", dbimp.Date{Year: 9999, Month: time.December, Day: 31}},
		{"the epoch", col(wireDate, -1, -1), "0", dbimp.Date{Year: 1970, Month: time.January, Day: 1}},
		{"midnight", col(wireTime, 0, 9), "0.000000000", dbimp.LocalTime{}},
		{"the last time", col(wireTime, 0, 9), "86399.999999999", dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999}},
		{"a time of a scale of 3", col(wireTime, 0, 3), "3600.5", dbimp.LocalTime{Hour: 1, Nanosecond: 500000000}},
		{"a timestamp with no fraction", col(wireTimestampNTZ, 0, 0), "1791549296", local(1791549296, 0)},
		{"a timestamp before 1970", col(wireTimestampNTZ, 0, 9), "-1.500000000", local(-2, 500000000)},
		{"a whole negative timestamp", col(wireTimestampNTZ, 0, 9), "-1.000000000", local(-1, 0)},
		{"the first timestamp", col(wireTimestampNTZ, 0, 9), "-62135596800.000000000", dbimp.LocalDateTime{Date: dbimp.Date{Year: 1, Month: time.January, Day: 1}}},
		{"the last timestamp", col(wireTimestampNTZ, 0, 9), "253402300799.999999999", dbimp.LocalDateTime{
			Date: dbimp.Date{Year: 9999, Month: time.December, Day: 31},
			Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999999999},
		}},
		{"an instant", col(wireTimestampLTZ, 0, 9), "1791524096.123456789", time.Unix(1791524096, 123456789).UTC()},
		{"an instant with a zone", col(wireTimestampTZ, 0, 9), "1791524096.123456789 1860", time.Unix(1791524096, 123456789).In(time.FixedZone("", 7*3600))},
		{"an instant at UTC", col(wireTimestampTZ, 0, 9), "0.000000000 1440", time.Unix(0, 0).In(time.FixedZone("", 0))},
		{"an instant west of UTC", col(wireTimestampTZ, 0, 0), "86400 1020", time.Unix(86400, 0).In(time.FixedZone("", -7*3600))},
		{"an empty object", col(wireObject, -1, -1), "{}", map[string]any{}},
		{"an empty array", col(wireArray, -1, -1), "[]", []any{}},
		{"a null in an array", col(wireArray, -1, -1), "[\n  undefined,\n  null\n]", []any{nil, nil}},
		{"a variant text that holds the word undefined", col(wireVariant, -1, -1), `"undefined"`, "undefined"},
		{"an object that holds the word in a string", col(wireObject, -1, -1), "{\n  \"a\": \"x undefined \\\" undefined\"\n}", map[string]any{"a": `x undefined " undefined`}},
		{"an object with undefined in a list", col(wireObject, -1, -1), "{\n  \"a\": [\n    1,\n    undefined\n  ]\n}", map[string]any{"a": []any{int64(1), nil}}},
		{"a vector of integers", col(wireVector, -1, -1), "[1,2,3]", dbimp.Vector[float64]{1, 2, 3}},
		{"an empty vector", col(wireVector, -1, -1), "[]", dbimp.Vector[float64]{}},
		{"a geography", col(wireGeography, -1, -1), `{"type":"Point","coordinates":[1.5,2]}`, map[string]any{"type": "Point", "coordinates": []any{1.5, int64(2)}}},
	} {
		got, err := decode(tt.c, quoted(tt.in), time.UTC)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !equal(got, tt.want) {
			t.Errorf("%s: got %#v, want %#v", tt.name, got, tt.want)
		}
	}
	// A NULL is nil for every type, and a JSON null too.
	for _, wire := range []string{wireFixed, wireReal, wireText, wireBinary, wireBoolean, wireDate, wireTime, wireTimestampNTZ, wireTimestampLTZ, wireTimestampTZ, wireVariant, wireObject, wireArray, wireGeography, wireGeometry, wireVector, "unknown"} {
		if got, err := decode(col(wire, 10, 0), jsontext.Value("null"), time.UTC); got != nil || err != nil {
			t.Errorf("a NULL of %s is %v, %v, want nil", wire, got, err)
		}
	}
	// A float of the special texts.
	for text, check := range map[string]func(float64) bool{
		"NaN":  math.IsNaN,
		"inf":  func(f float64) bool { return math.IsInf(f, 1) },
		"-inf": func(f float64) bool { return math.IsInf(f, -1) },
	} {
		got, err := decode(col(wireReal, -1, -1), quoted(text), time.UTC)
		if f, ok := got.(float64); err != nil || !ok || !check(f) {
			t.Errorf("the float %q is %v, %v", text, got, err)
		}
	}
}

// TestDecodeRefuses holds that a value that the driver cannot decode is an
// error and never its text (D135), and that a type that the driver has no Go
// type for is dbimp.ErrNotSupported.
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		c    column
		in   jsontext.Value
		want error
	}{
		{"an integer with a fraction", col(wireFixed, 10, 0), quoted("1.5"), dbimp.ErrInvalidValue},
		{"an integer too large", col(wireFixed, 18, 0), quoted("9223372036854775808"), dbimp.ErrInvalidValue},
		{"a decimal that is text", col(wireFixed, 10, 2), quoted("abc"), dbimp.ErrInvalidValue},
		{"a decimal infinity", col(wireFixed, 38, 0), quoted("Infinity"), dbimp.ErrInvalidValue},
		{"a float that is text", col(wireReal, -1, -1), quoted("abc"), dbimp.ErrInvalidValue},
		{"a value that is a number", col(wireText, -1, -1), jsontext.Value("5"), dbimp.ErrInvalidValue},
		{"binary that is not hex", col(wireBinary, -1, -1), quoted("xyz"), dbimp.ErrInvalidValue},
		{"binary of an odd length", col(wireBinary, -1, -1), quoted("abc"), dbimp.ErrInvalidValue},
		{"a boolean that is text", col(wireBoolean, -1, -1), quoted("yes"), dbimp.ErrInvalidValue},
		{"a date out of range", col(wireDate, -1, -1), quoted("2932897"), dbimp.ErrInvalidValue},
		{"a date that is text", col(wireDate, -1, -1), quoted("2026-10-09"), dbimp.ErrInvalidValue},
		{"a time that is negative", col(wireTime, 0, 9), quoted("-1.0"), dbimp.ErrInvalidValue},
		{"a time of a day", col(wireTime, 0, 9), quoted("86400"), dbimp.ErrInvalidValue},
		{"a timestamp of ten digits of fraction", col(wireTimestampNTZ, 0, 9), quoted("1.0123456789"), dbimp.ErrInvalidValue},
		{"a timestamp with a sign in the fraction", col(wireTimestampNTZ, 0, 9), quoted("1.-5"), dbimp.ErrInvalidValue},
		{"a timestamp out of range", col(wireTimestampNTZ, 0, 9), quoted("253402300800"), dbimp.ErrInvalidValue},
		{"a timestamp below the range", col(wireTimestampNTZ, 0, 9), quoted("-62135596801"), dbimp.ErrInvalidValue},
		{"an empty timestamp", col(wireTimestampLTZ, 0, 9), quoted(""), dbimp.ErrInvalidValue},
		{"a timestamp with no offset", col(wireTimestampTZ, 0, 9), quoted("1791524096.123456789"), dbimp.ErrInvalidValue},
		{"a timestamp with a bad offset", col(wireTimestampTZ, 0, 9), quoted("1791524096 x"), dbimp.ErrInvalidValue},
		{"a timestamp with an offset out of range", col(wireTimestampTZ, 0, 9), quoted("1791524096 2881"), dbimp.ErrInvalidValue},
		{"an object that is not JSON", col(wireObject, -1, -1), quoted("{a"), dbimp.ErrInvalidValue},
		{"a vector that is not an array", col(wireVector, -1, -1), quoted("1,2"), dbimp.ErrInvalidValue},
		{"a vector of text", col(wireVector, -1, -1), quoted(`["a"]`), dbimp.ErrInvalidValue},
		{"a vector that is cut", col(wireVector, -1, -1), quoted("[1,"), dbimp.ErrInvalidValue},
		{"a type that is not known", col("map", -1, -1), quoted("{}"), dbimp.ErrNotSupported},
	} {
		got, err := decode(tt.c, tt.in, time.UTC)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: got %#v, %v, want an error that wraps %v", tt.name, got, err, tt.want)
		}
	}
}

// TestUndefinedAsNull holds the rewrite of undefined, which the server writes
// for a NULL inside an array.
func TestUndefinedAsNull(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`[1,undefined]`:                       `[1,null]`,
		`"undefined"`:                         `"undefined"`,
		`["undefined",undefined]`:             `["undefined",null]`,
		`["a\"undefined",undefined]`:          `["a\"undefined",null]`,
		`["a\\",undefined]`:                   `["a\\",null]`,
		`{"undefined":undefined}`:             `{"undefined":null}`,
		`[undefinedundefined]`:                `[nullnull]`,
		`no word here`:                        `no word here`,
		``:                                    ``,
		`"` + "\\":                            `"` + "\\",
		`undefined`:                           `null`,
		`[1, 2, undefined, "x", [undefined]]`: `[1, 2, null, "x", [null]]`,
	} {
		if got := undefinedAsNull(in); got != want {
			t.Errorf("undefinedAsNull(%q) = %q, want %q", in, got, want)
		}
	}
}
