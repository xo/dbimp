package neo4j //nolint:testpackage // These tests read the parsers and the encoder, which are not exported.

import (
	"encoding/json/jsontext"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

func TestParseForms(t *testing.T) {
	t.Parallel()
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skipf("the system has no zone Europe/Oslo: %v", err)
	}
	for _, tt := range []struct {
		typ, text string
		want      any
	}{
		{"Date", "2026-09-27", Date(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))},
		{"Date", "+999999999-12-31", Date(time.Date(999999999, 12, 31, 0, 0, 0, 0, time.UTC))},
		{"Date", "-0001-06-01", Date(time.Date(-1, 6, 1, 0, 0, 0, 0, time.UTC))},
		{"LocalTime", "12:50:35.556123456", LocalTime(time.Date(0, 1, 1, 12, 50, 35, 556123456, time.UTC))},
		{"LocalTime", "12:00", LocalTime(time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC))},
		{"LocalDateTime", "2026-09-27T10:00:00.1", LocalDateTime(time.Date(2026, 9, 27, 10, 0, 0, 1e8, time.UTC))},
		{"OffsetDateTime", "1600-01-01T00:00:00Z", time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"Duration", "P1Y2M3DT4H5M6.007S", Duration{Months: 14, Days: 3, Seconds: 14706, Nanos: 7e6}},
		{"Duration", "P-1Y-2M-3DT-5.000000007S", Duration{Months: -14, Days: -3, Seconds: -6, Nanos: 999999993}},
		{"Duration", "PT-0.5S", Duration{Seconds: -1, Nanos: 5e8}},
		{"Duration", "P2W", Duration{Days: 14}},
		{"Point", "SRID=7203;POINT (1.5 2.5)", Point{SRID: 7203, X: 1.5, Y: 2.5, Dims: 2}},
		{"Point", "SRID=4979;POINT Z (10.7 59.9 3.0)", Point{SRID: 4979, X: 10.7, Y: 59.9, Z: 3, Dims: 3}},
	} {
		got, err := parseText(tt.typ, tt.text)
		if err != nil {
			t.Errorf("parsing the %s %q: %v", tt.typ, tt.text, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("the %s %q is %#v, want %#v", tt.typ, tt.text, got, tt.want)
		}
	}
	z, err := parseText("ZonedDateTime", "2026-09-27T10:00:00+02:00[Europe/Oslo]")
	if zt, ok := z.(time.Time); err != nil || !ok || !zt.Equal(time.Date(2026, 9, 27, 10, 0, 0, 0, oslo)) || zt.Location().String() != oslo.String() {
		t.Errorf("the ZonedDateTime is %v and %v, want 10:00 in Europe/Oslo", z, err)
	}
	// A zone that the system does not have keeps its name and its offset
	// (D63).
	z, err = parseText("ZonedDateTime", "2026-09-27T10:00:00+05:00[Dbimp/Nowhere]")
	if zt, ok := z.(time.Time); err != nil || !ok || zt.Location().String() != "Dbimp/Nowhere" || zt.Hour() != 10 {
		t.Errorf("the ZonedDateTime in an unknown zone is %v and %v", z, err)
	}
}

func TestParseRefusesForms(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ typ, text string }{
		{"Date", "2026-13-01"},
		{"Date", "2026-02-30"},
		{"Date", "26-09-27"},
		{"LocalTime", "25:00:00"},
		{"LocalTime", "12:00:00.1234567891"},
		{"Time", "12:00:00"},
		{"LocalDateTime", "2026-09-27 10:00:00"},
		{"OffsetDateTime", "2026-09-27T10:00:00"},
		{"ZonedDateTime", "2026-09-27T10:00:00+02:00"},
		{"Duration", "P"},
		{"Duration", "PT1.5M"},
		{"Duration", "P1D1Y"},
		{"Duration", "1D"},
		{"Point", "POINT (1 2)"},
		{"Point", "SRID=7203;POINT Z (1 2)"},
		{"Float", "Inf"},
	} {
		if _, err := parseText(tt.typ, tt.text); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("parsing the %s %q gave %v, want dbimp.ErrInvalidValue", tt.typ, tt.text, err)
		}
	}
}

func TestEncode(t *testing.T) {
	t.Parallel()
	oslo, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		t.Skipf("the system has no zone Europe/Oslo: %v", err)
	}
	type myInt int
	s := "p"
	for _, tt := range []struct {
		in      any
		want    string
		version int
	}{
		{nil, `{"$type":"Null","_value":null}`, 0},
		{true, `{"$type":"Boolean","_value":true}`, 0},
		{int64(math.MaxInt64), `{"$type":"Integer","_value":"9223372036854775807"}`, 0},
		{myInt(7), `{"$type":"Integer","_value":"7"}`, 0},
		{uint64(7), `{"$type":"Integer","_value":"7"}`, 0},
		{0.1, `{"$type":"Float","_value":"0.1"}`, 0},
		{float32(0.1), `{"$type":"Float","_value":"0.1"}`, 0},
		{math.Inf(-1), `{"$type":"Float","_value":"-Infinity"}`, 0},
		{"héllo", `{"$type":"String","_value":"héllo"}`, 0},
		{&s, `{"$type":"String","_value":"p"}`, 0},
		{[]byte{0xde, 0xad, 0xbe, 0xef}, `{"$type":"Base64","_value":"3q2+7w=="}`, 0},
		{[]any{1, "a", nil}, `{"$type":"List","_value":[{"$type":"Integer","_value":"1"},{"$type":"String","_value":"a"},{"$type":"Null","_value":null}]}`, 0},
		{[]float32{1.5}, `{"$type":"List","_value":[{"$type":"Float","_value":"1.5"}]}`, 0},
		{map[string]int{"k": 1}, `{"$type":"Map","_value":{"k":{"$type":"Integer","_value":"1"}}}`, 0},
		{Date(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)), `{"$type":"Date","_value":"2026-09-27"}`, 0},
		{LocalTime(time.Date(0, 1, 1, 12, 0, 0, 5e8, time.UTC)), `{"$type":"LocalTime","_value":"12:00:00.5"}`, 0},
		{Time(time.Date(0, 1, 1, 12, 0, 0, 0, time.FixedZone("", -5*3600-30*60))), `{"$type":"Time","_value":"12:00:00-05:30"}`, 0},
		{LocalDateTime(time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)), `{"$type":"LocalDateTime","_value":"2026-09-27T10:00:00"}`, 0},
		{time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC), `{"$type":"OffsetDateTime","_value":"2026-09-27T10:00:00Z"}`, 0},
		{time.Date(2026, 9, 27, 10, 0, 0, 0, time.FixedZone("", 7200)), `{"$type":"OffsetDateTime","_value":"2026-09-27T10:00:00+02:00"}`, 0},
		{time.Date(2026, 9, 27, 10, 0, 0, 0, oslo), `{"$type":"ZonedDateTime","_value":"2026-09-27T10:00:00+02:00[Europe/Oslo]"}`, 0},
		{Duration{Months: 14, Days: 3, Seconds: 14706, Nanos: 7e6}, `{"$type":"Duration","_value":"P1Y2M3DT4H5M6.007S"}`, 0},
		{90 * time.Minute, `{"$type":"Duration","_value":"PT1H30M"}`, 0},
		{Point{SRID: 4979, X: 1, Y: 2, Z: 3, Dims: 3}, `{"$type":"Point","_value":"SRID=4979;POINT Z (1 2 3)"}`, 0},
		{Vector{Coordinates: []float32{1.5, 2}}, `{"$type":"Vector","_value":{"coordinatesType":"FLOAT32","coordinates":["1.5","2"]}}`, 1},
		{Vector{Coordinates: []int8{1, -2}}, `{"$type":"Vector","_value":{"coordinatesType":"INT8","coordinates":["1","-2"]}}`, 1},
		{uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"), `{"$type":"UUID","_value":"550e8400-e29b-41d4-a716-446655440000"}`, 2},
		{[]any{uuid.UUID{}}, `{"$type":"List","_value":[{"$type":"UUID","_value":"00000000-0000-0000-0000-000000000000"}]}`, 2},
	} {
		got, version, err := encode(tt.in)
		if err != nil {
			t.Errorf("encoding %#v: %v", tt.in, err)
			continue
		}
		if string(got) != tt.want || version != tt.version {
			t.Errorf("encoding %#v gave %s at version %d, want %s at version %d", tt.in, got, version, tt.want, tt.version)
		}
	}
}

func TestEncodeRefuses(t *testing.T) {
	t.Parallel()
	for _, in := range []any{
		uint64(math.MaxUint64), Node{}, Relationship{}, Path{}, apd.New(1, 0), *apd.New(1, 0),
		struct{}{}, map[int]int{1: 1}, Vector{Coordinates: []string{"a"}}, make(chan int),
	} {
		if _, _, err := encode(in); err == nil {
			t.Errorf("encoding %T gave no error", in)
		}
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want any
	}{
		{`{"$type":"Null","_value":null}`, nil},
		{`{"$type":"Integer","_value":"-9223372036854775808"}`, int64(math.MinInt64)},
		{`{"$type":"Float","_value":"1.0E300"}`, 1e300},
		{`{"_value":"x","$type":"String"}`, "x"},
		{`{"$type":"Base64","_value":"3q2+7w=="}`, []byte{0xde, 0xad, 0xbe, 0xef}},
		{`{"$type":"List","_value":[]}`, []any{}},
		{`{"$type":"Map","_value":{}}`, map[string]any{}},
		{`{"$type":"Vector","_value":{"coordinatesType":"INT64","coordinates":["9223372036854775807"]}}`, Vector{Coordinates: []int64{math.MaxInt64}}},
	} {
		got, err := decode(jsontext.Value(tt.in))
		if err != nil {
			t.Errorf("decoding %s: %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decoding %s gave %#v, want %#v", tt.in, got, tt.want)
		}
	}
	for _, in := range []string{
		`{"$type":"Unsupported","_value":"x"}`,
		`{"$type":"Dbimp","_value":1}`,
		`{"$type":"Integer","_value":"1.5"}`,
		`{"$type":"Vector","_value":{"coordinatesType":"INT8","coordinates":["300"]}}`,
		`{"$type":"Vector","_value":{"coordinatesType":"BIT","coordinates":[]}}`,
		`{"$type":"Path","_value":[{"$type":"Integer","_value":"1"}]}`,
		`1`,
		`{"$type":"Integer"}`,
	} {
		if _, err := decode(jsontext.Value(in)); err == nil {
			t.Errorf("decoding %s gave no error", in)
		}
	}
}

// FuzzParseDuration holds that a duration that the driver reads writes itself
// in a form that the driver reads back as the same duration.
func FuzzParseDuration(f *testing.F) {
	for _, s := range []string{"P1Y2M3DT4H5M6.007S", "P-1Y-2M-3DT-5.000000007S", "PT-0.5S", "PT0S", "P3D", "PT-1H-1M-40S"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := parseDuration(s)
		if err != nil {
			return
		}
		again, err := parseDuration(d.String())
		if err != nil || again != d {
			t.Fatalf("%q is %+v, which writes %q, which reads back as %+v and %v", s, d, d.String(), again, err)
		}
	})
}

// FuzzParseDate holds the same for a date, a time and a date and time.
func FuzzParseDate(f *testing.F) {
	for _, s := range []string{"2026-09-27", "+999999999-12-31", "-0001-06-01", "12:00", "12:50:35.556123456", "12:00:00.5-05:30", "2026-01-01T00:00:00+01:30:15"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, typ := range []string{"Date", "LocalTime", "Time", "LocalDateTime", "OffsetDateTime"} {
			v, err := parseText(typ, s)
			if err != nil {
				continue
			}
			text, _, err := encode(v)
			if err != nil {
				t.Fatalf("the %s %q reads as %v, which does not encode: %v", typ, s, v, err)
			}
			again, err := decode(text)
			if err != nil {
				t.Fatalf("the %s %q encodes as %s, which does not decode: %v", typ, s, text, err)
			}
			a, b := timeOf(v), timeOf(again)
			_, ao := a.Zone()
			_, bo := b.Zone()
			if !a.Equal(b) || ao != bo || reflect.TypeOf(v) != reflect.TypeOf(again) {
				t.Fatalf("the %s %q reads as %v, and back as %v", typ, s, v, again)
			}
		}
	})
}

// timeOf returns the time.Time of a temporal value.
func timeOf(v any) time.Time {
	switch x := v.(type) {
	case Date:
		return time.Time(x)
	case LocalTime:
		return time.Time(x)
	case Time:
		return time.Time(x)
	case LocalDateTime:
		return time.Time(x)
	case time.Time:
		return x
	}
	return time.Time{}
}
