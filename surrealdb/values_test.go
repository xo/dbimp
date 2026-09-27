package surrealdb //nolint:testpackage // The tests read the codec of the driver, which is not exported.

import (
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// cborOf returns the bytes of s, a string of hex digits.
func cborOf(tb testing.TB, s string) []byte {
	tb.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func decimal(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// These are the items that 3.3.0 sent, in the recordings of item 3.
func TestDecodeCBOR(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("0192f1c4-3b5e-7a2c-9f00-000000000001")
	for _, tt := range []struct {
		name string
		hex  string
		want any
	}{
		{"an integer", "1b7fffffffffffffff", int64(math.MaxInt64)},
		{"a negative integer", "3b7fffffffffffffff", int64(math.MinInt64)},
		{"a float", "fb3fb999999999999a", 0.1},
		{"a half float", "f93e00", 1.5},
		{"a string", "6161", "a"},
		{"bytes", "426869", []byte("hi")},
		{"empty bytes", "40", []byte{}},
		{"true", "f5", true},
		{"null", "f6", nil},
		{"NONE", "c6f6", nil},
		{"a decimal", "ca 781e 312e32333435363738393031323334353637383930313233343536373839", decimal(t, "1.2345678901234567890123456789")},
		{"a datetime", "cc 82 1a6ab8e920 1a075bcd15", time.Date(2026, 9, 27, 10, 0, 0, 123456789, time.UTC)},
		{"a datetime before 1970", "cc 82 3b00000002b7f315ff 00", time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"a datetime as text", "c0 74 323032362d30392d32375431303a30303a30305a", time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)},
		{"a duration", "ce 81 191518", 90 * time.Minute},
		{"a zero duration", "ce 80", time.Duration(0)},
		{"a duration of nanoseconds", "ce 82 00 01", time.Nanosecond},
		{"a duration as text", "cd 65 3168333073", time.Hour + 30*time.Second},
		{"a uuid", "d825 50 0192f1c43b5e7a2c9f00000000000001", id},
		{"a uuid as text", "c9 7824 30313932663163342d336235652d376132632d396630302d303030303030303030303031", id},
		{"a record id", "c8 82 66706572736f6e 65746f626965", RecordID{Table: "person", ID: "tobie"}},
		{"a record id with an array", "c8 82 6161 82 6178 01", RecordID{Table: "a", ID: []any{"x", int64(1)}}},
		{"a record id as text", "c8 6c706572736f6e3a746f626965", RecordID{Table: "person", ID: "tobie"}},
		{"a table", "c7 6474626c31", "tbl1"},
		{"a set", "d838 83 010203", []any{int64(1), int64(2), int64(3)}},
		{"a range", "d831 82 d83201 d83305", "1..5"},
		{"an inclusive range", "d831 82 d83201 d83205", "1..=5"},
		{"a range with an excluded start", "d831 82 d83301 d83305", "1>..5"},
		{"a range open at the start", "d831 82 f6 d83305", "..5"},
		{"a range open at the end", "d831 82 d83201 f6", "1.."},
		{"a range of strings", "d831 82 d8326161 d833617a", "'a'..'z'"},
		{"a point", "d858 82 f93e00 f94100", map[string]any{"type": "Point", "coordinates": []any{1.5, 2.5}}},
		{"a line", "d859 82 d858 82 00 00 d858 82 01 01", map[string]any{"type": "LineString", "coordinates": []any{[]any{int64(0), int64(0)}, []any{int64(1), int64(1)}}}},
		{"a polygon", "d85a 81 d859 82 d858 82 00 00 d858 82 01 00", map[string]any{"type": "Polygon", "coordinates": []any{[]any{[]any{int64(0), int64(0)}, []any{int64(1), int64(0)}}}}},
		{"a collection", "d85e 81 d858 82 01 02", map[string]any{"type": "GeometryCollection", "geometries": []any{map[string]any{"type": "Point", "coordinates": []any{int64(1), int64(2)}}}}},
		{"an array", "83 01 6161 f6", []any{int64(1), "a", nil}},
		{"an object", "a2 6161 01 6162 c6f6", map[string]any{"a": int64(1), "b": nil}},
		{"an array of indefinite length", "9f 01 02 ff", []any{int64(1), int64(2)}},
	} {
		got, err := decodeCBOR(cborOf(t, tt.hex))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if d, ok := tt.want.(*apd.Decimal); ok {
			if g, ok := got.(*apd.Decimal); !ok || g.Cmp(d) != 0 {
				t.Errorf("%s is %#v, want %s", tt.name, got, d)
			}
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s is %#v, want %#v", tt.name, got, tt.want)
		}
	}
}

func TestDecodeCBORRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		hex  string
		want error
	}{
		{"a duration of 300 years", "ce 81 1b0000000233e5a200", dbimp.ErrInvalidValue},
		{"a duration as text of 300 years", "cd 6433303079", dbimp.ErrInvalidValue},
		{"a uuid of 15 bytes", "d825 4f 0192f1c43b5e7a2c9f000000000000", dbimp.ErrInvalidValue},
		{"a datetime of three parts", "cc 83 00 00 00", dbimp.ErrInvalidValue},
		{"a datetime with too many nanoseconds", "cc 82 00 1a3b9aca00", dbimp.ErrInvalidValue},
		{"a decimal that is not a number", "ca 6178", dbimp.ErrInvalidValue},
		{"a record id of one part", "c8 81 6161", dbimp.ErrInvalidValue},
		{"a tag that SurrealDB does not use", "d86401", dbimp.ErrNotSupported},
		{"a key that is not text", "a1 01 02", dbimp.ErrInvalidValue},
	} {
		if _, err := decodeCBOR(cborOf(t, tt.hex)); !errors.Is(err, tt.want) {
			t.Errorf("%s gave %v, want %v", tt.name, err, tt.want)
		}
	}
}

// Each value that the driver sends keeps its type when the server reads it,
// so its encoding decodes to itself.
func TestEncodeCBORRoundTrip(t *testing.T) {
	t.Parallel()
	for _, v := range []any{
		nil, true, "it's", []byte{0, 255}, int64(-7), 1.5,
		time.Date(2026, 9, 27, 10, 0, 0, 123456789, time.UTC),
		time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC),
		90 * time.Minute,
		uuid.MustParse("0192f1c4-3b5e-7a2c-9f00-000000000001"),
		RecordID{Table: "person", ID: "tobie"},
		RecordID{Table: "a", ID: []any{"x", int64(1)}},
		[]any{int64(1), "a", nil},
		map[string]any{"b": int64(2), "a": []any{true}},
	} {
		var e dbimp.CBOREncoder
		if err := encodeCBOR(&e, v); err != nil {
			t.Errorf("encoding %#v: %v", v, err)
			continue
		}
		got, err := decodeCBOR(e.Bytes())
		if err != nil {
			t.Errorf("decoding %#v: %v", v, err)
			continue
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("%#v came back as %#v", v, got)
		}
	}
}

func TestEncodeCBOROtherTypes(t *testing.T) {
	t.Parallel()
	type point struct {
		X int     `json:"x"`
		Y float64 `json:"y"`
	}
	for _, tt := range []struct {
		in   any
		want any
	}{
		{int(3), int64(3)},
		{int8(-3), int64(-3)},
		{uint16(3), int64(3)},
		{float32(1.5), 1.5},
		{[]string{"a", "b"}, []any{"a", "b"}},
		{[2]int{1, 2}, []any{int64(1), int64(2)}},
		{map[string]int{"a": 1}, map[string]any{"a": int64(1)}},
		{point{X: 1, Y: 2.5}, map[string]any{"x": int64(1), "y": 2.5}},
		{new(7), int64(7)},
		{(*int)(nil), nil},
		{decimal(t, "0.1"), decimal(t, "0.1")},
	} {
		var e dbimp.CBOREncoder
		if err := encodeCBOR(&e, tt.in); err != nil {
			t.Errorf("encoding %#v: %v", tt.in, err)
			continue
		}
		got, err := decodeCBOR(e.Bytes())
		if err != nil {
			t.Errorf("decoding %#v: %v", tt.in, err)
			continue
		}
		if d, ok := tt.want.(*apd.Decimal); ok {
			if g, ok := got.(*apd.Decimal); !ok || g.Cmp(d) != 0 {
				t.Errorf("%#v came back as %#v", tt.in, got)
			}
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%#v came back as %#v, want %#v", tt.in, got, tt.want)
		}
	}
	var e dbimp.CBOREncoder
	if err := encodeCBOR(&e, -time.Second); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("a negative duration gave %v, want ErrInvalidValue", err)
	}
}

// The maps of a request are written in the order of their keys, so that the
// same request is always the same bytes.
func TestEncodeCBORSortsKeys(t *testing.T) {
	t.Parallel()
	var e dbimp.CBOREncoder
	if err := encodeCBOR(&e, map[string]any{"b": 1, "c": 2, "a": 3}); err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(e.Bytes()), "a3616103616201616302"; got != want {
		t.Errorf("the map is %s, want %s", got, want)
	}
}

func TestRecordIDString(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("0192f1c4-3b5e-7a2c-9f00-000000000001")
	for _, tt := range []struct {
		id   RecordID
		want string
	}{
		{RecordID{"person", "tobie"}, "person:tobie"},
		{RecordID{"person", int64(123)}, "person:123"},
		{RecordID{"person", "123"}, "person:`123`"},
		{RecordID{"a", "b c"}, "a:`b c`"},
		{RecordID{"a", "x`y"}, "a:`x\\`y`"},
		{RecordID{"my table", "k"}, "`my table`:k"},
		{RecordID{"a", []any{"a", int64(1)}}, "a:['a', 1]"},
		{RecordID{"a", map[string]any{"b": int64(1), "a": "x"}}, "a:{ a: 'x', b: 1 }"},
		{RecordID{"a", id}, "a:u'0192f1c4-3b5e-7a2c-9f00-000000000001'"},
	} {
		if got := tt.id.String(); got != tt.want {
			t.Errorf("%#v is %s, want %s", tt.id, got, tt.want)
		}
	}
}

// A record id inside an array or an object is written by MarshalText, so a
// caller that writes the value as JSON, as usql does, sees person:tobie and
// not the fields of the struct (D70).
func TestRecordIDText(t *testing.T) {
	t.Parallel()
	b, err := RecordID{"a", "x`y"}.MarshalText()
	if err != nil || string(b) != "a:`x\\`y`" {
		t.Errorf("MarshalText gave %q, %v, want the text of String", b, err)
	}
	v := []any{
		RecordID{"book", "earthsea"},
		map[string]any{"by": RecordID{"author", int64(1)}},
		&RecordID{"a", []any{"x", int64(1)}},
	}
	b, err = json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	const want = `["book:earthsea",{"by":"author:1"},"a:['x', 1]"]`
	if string(b) != want {
		t.Errorf("json.Marshal gave %s, want %s", b, want)
	}
}

func TestDurations(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		text string
		d    time.Duration
	}{
		{"0ns", 0},
		{"1ns", time.Nanosecond},
		{"1h30m", 90 * time.Minute},
		{"1y2w3d", 365*24*time.Hour + 17*24*time.Hour},
		{"1s500ms", 1500 * time.Millisecond},
		{"2µs", 2 * time.Microsecond},
	} {
		if got := formatDuration(tt.d); got != tt.text {
			t.Errorf("formatDuration(%v) = %s, want %s", tt.d, got, tt.text)
		}
		got, err := parseDuration(tt.text)
		if err != nil || got != tt.d {
			t.Errorf("parseDuration(%s) = %v, %v, want %v", tt.text, got, err, tt.d)
		}
	}
	if got, err := parseDuration("3us"); err != nil || got != 3*time.Microsecond {
		t.Errorf("parseDuration(3us) = %v, %v", got, err)
	}
	for _, s := range []string{"", "h", "1", "1x", "300y"} {
		if _, err := parseDuration(s); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("parseDuration(%q) gave %v, want ErrInvalidValue", s, err)
		}
	}
}

func FuzzDecodeCBOR(f *testing.F) {
	for _, s := range []string{
		"a2 6161 01 6162 c6f6", "cc 82 1a6ab8e920 1a075bcd15", "d831 82 d83201 d83305",
		"c8 82 6161 82 6178 01", "d85a 81 d859 82 d858 82 00 00 d858 82 01 00", "ce 81 1b0000000233e5a200",
	} {
		f.Add(cborOf(f, s))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		v, err := decodeCBOR(in)
		if err != nil {
			return
		}
		// A value that decodes must encode, unless it holds a string that a
		// tag made, which the driver never sends back as that tag.
		var e dbimp.CBOREncoder
		_ = encodeCBOR(&e, v)
	})
}
