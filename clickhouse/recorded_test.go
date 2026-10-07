package clickhouse //nolint:testpackage // The tests decode the columns with the types of the driver, which are not exported.

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// findExchange returns the first exchange of release, in the order of its files,
// that keep accepts. It reads one file at a time and keeps none, because the
// recordings of a release are large, and a test of the memory of the driver
// counts the memory of the process.
func findExchange(t *testing.T, release string, keep func(*dbimptest.Exchange) bool) *dbimptest.Exchange {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(recorded, release+"-*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("finding the exchanges of %s: %v", release, err)
	}
	for _, path := range paths {
		if base := filepath.Base(path); base == dbimptest.ManifestName || base == dbimptest.RequestsName || base == dbimptest.FeaturesName {
			continue
		}
		ex, err := dbimptest.ReadExchange(path)
		if err != nil {
			t.Fatal(err)
		}
		if keep(ex) {
			return ex
		}
	}
	t.Fatalf("no exchange of %s matches", release)
	return nil
}

// withQuery accepts the exchanges whose request has the text in its query and
// the text in its statement.
func withQuery(query, statement string) func(*dbimptest.Exchange) bool {
	return func(ex *dbimptest.Exchange) bool {
		return strings.Contains(ex.Request.Query, query) && strings.Contains(ex.Request.Body, statement)
	}
}

// lines splits the answer in the format JSONCompactEachRowWithNamesAndTypes into
// the names, the types and the raw values of each row.
func lines(t *testing.T, body []byte) ([]string, []*typ, [][]jsontext.Value) {
	t.Helper()
	dec := jsontext.NewDecoder(bytes.NewReader(body), jsontext.AllowInvalidUTF8(true))
	var header [2][]string
	for i := range header {
		v, err := dec.ReadValue()
		if err != nil {
			t.Fatalf("reading the header: %v", err)
		}
		d := newDecoder(v)
		if _, err := d.ReadToken(); err != nil {
			t.Fatal(err)
		}
		for d.PeekKind() != ']' {
			sv, err := d.ReadValue()
			if err != nil {
				t.Fatal(err)
			}
			s, err := decodeString(sv)
			if err != nil {
				t.Fatal(err)
			}
			header[i] = append(header[i], s)
		}
	}
	types := make([]*typ, len(header[1]))
	for i, name := range header[1] {
		ty, err := parseType(name)
		if err != nil {
			t.Fatalf("the type of %s: %v", header[0][i], err)
		}
		types[i] = ty
	}
	var rows [][]jsontext.Value
	for dec.PeekKind() == '[' {
		v, err := dec.ReadValue()
		if err != nil {
			t.Fatalf("reading a row: %v", err)
		}
		d := newDecoder(v)
		if _, err := d.ReadToken(); err != nil {
			t.Fatal(err)
		}
		var row []jsontext.Value
		for d.PeekKind() != ']' {
			cv, err := d.ReadValue()
			if err != nil {
				t.Fatal(err)
			}
			row = append(row, cv.Clone())
		}
		rows = append(rows, row)
	}
	return header[0], types, rows
}

// skip is the value that the table of TestRecordedEveryType leaves unchecked.
type skipValue struct{}

var skip = skipValue{}

// TestRecordedEveryType decodes the table of every type that step 6 recorded, on
// each release, through the types of the driver (D135, D176 and D177). Each value
// has the scan type of its column. The columns of times hold the text in the zone
// of the column, which the driver never asks for, since it asks for
// date_time_output_format=iso (TestRecordedInstants), so each such value that is
// not NULL is an error. The extremes of the integers, the decimal of 76 digits,
// the bytes that are not UTF-8 and the state of an aggregate are checked.
func TestRecordedEveryType(t *testing.T) {
	t.Parallel()
	bigOf := func(s string) *big.Int {
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			t.Fatalf("%q is not an integer", s)
		}
		return n
	}
	dec := func(s string) *apd.Decimal {
		d, _, err := apd.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	u256 := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	want := map[string][3]any{
		"i8":     {int64(-128), int64(0), int64(127)},
		"u8":     {int64(255), int64(0), int64(1)},
		"i64":    {int64(math.MinInt64), int64(0), int64(math.MaxInt64)},
		"u64":    {uint64(math.MaxUint64), uint64(0), uint64(1)},
		"i128":   {bigOf("-170141183460469231731687303715884105728"), bigOf("0"), bigOf("170141183460469231731687303715884105727")},
		"u128":   {bigOf("340282366920938463463374607431768211455"), bigOf("0"), bigOf("1")},
		"i256":   {bigOf("-57896044618658097711785492504343953926634992332820282019728792003956564819968"), bigOf("0"), bigOf("57896044618658097711785492504343953926634992332820282019728792003956564819967")},
		"u256":   {bigOf(u256), bigOf("0"), bigOf("1")},
		"f64":    {math.MaxFloat64, float64(0), nil},
		"d32":    {dec("99999.9999"), dec("0"), dec("-0.0001")},
		"d256":   {dec("123456789012345678901234567890123456.123456789012345678901234567890123456789"), dec("0"), dec("-0.0000000000000000000000000000000000000001")},
		"b":      {true, false, true},
		"s":      {"héllo € 日本", "", "x"},
		"sb":     {"\xff\x80", "", "\x00"},
		"fs":     {"abcd", "\x00\x00\x00\x00", "ab\x00\x00"},
		"dt":     {dbimp.Date{Year: 2149, Month: 6, Day: 6}, dbimp.Date{Year: 1970, Month: 1, Day: 1}, dbimp.Date{Year: 2026, Month: 2, Day: 28}},
		"dt32":   {dbimp.Date{Year: 2299, Month: 12, Day: 31}, dbimp.Date{Year: 1900, Month: 1, Day: 1}, dbimp.Date{Year: 1969, Month: 12, Day: 31}},
		"e8":     {"a", "b", "a"},
		"e16":    {"y", "x", "y"},
		"uu":     {uuid.MustParse("61f0c404-5cb3-11e7-907b-a6006ad3dba0"), uuid.UUID{}, uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")},
		"ip4":    {netip.MustParseAddr("192.168.0.1"), netip.MustParseAddr("0.0.0.0"), netip.MustParseAddr("255.255.255.255")},
		"ip6":    {netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("::"), netip.MustParseAddr("::ffff:1.2.3.4")},
		"arr":    {[]any{int64(1), int64(2), int64(3)}, []any{}, []any{int64(-1)}},
		"arr2":   {[]any{[]any{"a", "b"}, []any{}, []any{"c"}}, []any{}, []any{[]any{""}}},
		"arrn":   {[]any{int64(1), nil, int64(3)}, []any{}, []any{nil}},
		"tup":    {[]any{int64(1), "x"}, []any{int64(0), ""}, []any{int64(-1), "z"}},
		"tupn":   {[]any{int64(2), "y"}, []any{int64(0), ""}, []any{int64(-1), "z"}},
		"mp":     {map[string]any{"k": int64(1), "l": int64(2)}, map[string]any{}, map[string]any{"": int64(0)}},
		"mpn":    {map[string]any{"1": []any{"p", "q"}}, map[string]any{}, map[string]any{"0": []any{""}}},
		"lc":     {"lc", "", "lc2"},
		"lcn":    {"lcn", "", nil},
		"nl":     {int64(42), int64(0), nil},
		"nls":    {"text", "", nil},
		"nld":    {skip, skip, nil},
		"var":    {int64(42), "s", []any{int64(1), int64(2)}},
		"dyn":    {int64(7), "dyn", []any{int64(1), int64(2)}},
		"js":     {map[string]any{"a": int64(1), "b": map[string]any{"c": []any{int64(1), int64(2), "x"}}}, map[string]any{}, map[string]any{"a": "str"}},
		"nest.a": {[]any{int64(1), int64(2)}, []any{}, []any{int64(7)}},
		"nest.b": {[]any{"p", "q"}, []any{}, []any{"w"}},
		"sagg":   {int64(5), int64(0), int64(-5)},
		"agg":    {[]byte{5, 0, 0, 0, 0, 0, 0, 0}, make([]byte, 8), []byte{1, 0, 0, 0, 0, 0, 0, 0}},
	}
	times := map[string]bool{"dtm": true, "dtm64": true, "dtz": true, "dt64z": true, "nld": true}
	for _, rel := range releases {
		ex := findExchange(t, rel.name, func(ex *dbimptest.Exchange) bool {
			return strings.Contains(ex.Request.Query, "output_format_json_quote_decimals=0") &&
				strings.Contains(ex.Request.Query, "output_format_json_quote_64bit_integers=0") &&
				strings.HasPrefix(ex.Request.Body, "SELECT * FROM dbimp.dbimp_types") && ex.Response.Status == 200
		})
		names, types, rows := lines(t, ex.Response.Content())
		if len(rows) != 3 || len(names) != 53 {
			t.Fatalf("%s: the answer has %d columns and %d rows, want 53 and 3", rel.name, len(names), len(rows))
		}
		for c, name := range names {
			for r, row := range rows {
				got, err := types[c].decode(row[c])
				if times[name] && !dbimp.IsNull(row[c]) {
					if !errors.Is(err, dbimp.ErrInvalidValue) {
						t.Errorf("%s: %s row %d: the text of a time in the zone of its column gave %v, %v, want an error", rel.name, name, r, got, err)
					}
					continue
				}
				if err != nil {
					t.Errorf("%s: %s row %d (%s): %v", rel.name, name, r, row[c], err)
					continue
				}
				if got != nil && types[c].scanType() != typeOfAny && reflect.TypeOf(got) != types[c].scanType() {
					t.Errorf("%s: %s row %d is %T, want the scan type %v (D135)", rel.name, name, r, got, types[c].scanType())
				}
				// 25.3 writes the integers inside a JSON column as strings, even with
				// the setting that turns the quotes off (measured).
				jsonOf25 := name == "js" && rel.name == "clickhouse-25.3" && r == 0
				if w, ok := want[name]; ok && w[r] != skip && !jsonOf25 {
					if !equal(got, w[r]) {
						t.Errorf("%s: %s row %d is %#v, want %#v", rel.name, name, r, got, w[r])
					}
				}
			}
		}
	}
}

// TestRecordedInstants holds D177: with date_time_output_format=iso, a DateTime
// and a DateTime64 are the instant that the server holds, in the zone of the
// column. 2026-11-01 01:30 in America/New_York is the text of two instants, and
// the text in UTC names the one that the server holds (measured).
func TestRecordedInstants(t *testing.T) {
	t.Parallel()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("the host has no tzdata")
	}
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skip("the host has no tzdata")
	}
	for _, rel := range releases {
		ex := findExchange(t, rel.name, withQuery("date_time_output_format=iso", "toDateTime("))
		_, types, rows := lines(t, ex.Response.Content())
		got := make([]any, len(types))
		for i, ty := range types {
			v, err := ty.decode(rows[0][i])
			if err != nil {
				t.Fatalf("%s: %v", rel.name, err)
			}
			got[i] = v
		}
		want := []any{time.Date(2026, 10, 7, 12, 34, 56, 0, jakarta), time.Date(2026, 11, 1, 1, 30, 0, 0, ny)}
		for i := range want {
			if !equal(got[i], want[i]) {
				t.Errorf("%s: the instant %d is %v, want %v", rel.name, i, got[i], want[i])
			}
		}
		if got[1].(time.Time).Sub(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)) != 0 { //nolint:forcetypeassert // The value was checked by equal.
			t.Errorf("%s: the instant is %v, want 05:30 UTC", rel.name, got[1])
		}
	}
}

// TestRecordedSettings decodes the answers that the five settings of D176 make:
// a NaN and the infinities as strings, and a named tuple as an array.
func TestRecordedSettings(t *testing.T) {
	t.Parallel()
	for _, rel := range releases {
		ex := findExchange(t, rel.name, withQuery("output_format_json_quote_denormals=1", "nan AS n"))
		_, types, rows := lines(t, ex.Response.Content())
		var vals []float64
		for i, ty := range types {
			v, err := ty.decode(rows[0][i])
			if err != nil {
				t.Fatalf("%s: %v", rel.name, err)
			}
			vals = append(vals, v.(float64)) //nolint:forcetypeassert // The type is Float.
		}
		if !math.IsNaN(vals[0]) || !math.IsInf(vals[1], 1) || !math.IsInf(vals[2], -1) || !math.IsNaN(vals[3]) {
			t.Errorf("%s: the values are %v, want NaN, +Inf, -Inf and NaN", rel.name, vals)
		}
		ex = findExchange(t, rel.name, withQuery("output_format_json_named_tuples_as_objects=0", "Tuple(x Int32, y String)"))
		_, types, rows = lines(t, ex.Response.Content())
		v, err := types[0].decode(rows[0][0])
		if err != nil || !reflect.DeepEqual(v, []any{int64(1), "a"}) {
			t.Errorf("%s: the named tuple is %#v, %v, want [1 a]", rel.name, v, err)
		}
	}
}

// TestRecordedTrailer holds the framing of an error after rows in the form of
// 26.9: the marker, a tag, the text, its length, the tag and the marker. The test
// takes the trailer that the server wrote after the rows of a Native answer, and
// puts it after the rows of an answer in the format of the driver.
func TestRecordedTrailer(t *testing.T) {
	t.Parallel()
	ex := findExchange(t, "clickhouse-26.9", func(ex *dbimptest.Exchange) bool {
		return ex.Response.Status == 200 && bytes.Contains(ex.Response.Content(), []byte("\r\n"+marker+"\r\n"))
	})
	body := ex.Response.Content()
	start := bytes.Index(body, []byte("\r\n"+marker))
	if start < 0 {
		t.Fatal("the recorded answer has no marker")
	}
	trailer := string(body[start:])
	tag := ex.Response.Header.Get(tagHeader)
	if tag == "" || !strings.Contains(trailer, tag) {
		t.Fatalf("the recorded trailer %q has no tag %q", trailer, tag)
	}
	for _, tt := range []struct {
		name    string
		tag     string
		version string
	}{
		{"the tag in the header", tag, ""},
		{"no header, and the version says 26.9", "", "26.9.2.8"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newStream(t, tt.version, tt.tag, "["+`"a"`+"]\n"+`["Int64"]`+"\n[1]\n[2]\n"+trailer)
			n, err := s.count(t)
			e := serverError(t, err)
			if n != 2 || e.Code != 395 || e.HTTPStatus != 200 || e.Name != "FUNCTION_THROW_IF_VALUE_IS_NON_ZERO" || !errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("read %d rows and the error %+v (%v), want 2 rows and the error 395 that wraps dbimp.ErrIncomplete", n, e, err)
			}
			if strings.Contains(e.Message, tag) || strings.Contains(e.Message, marker) || strings.Contains(e.Message, "version") {
				t.Errorf("the message %q holds the tag, the marker or the version", e.Message)
			}
		})
	}
	// The trailer is cut or wrong in each of these. None of them is the end of a
	// result that the server completed.
	cut := trailer[:len(trailer)-len("\r\n"+marker+"\r\n")]
	nl := strings.LastIndex(trailer[:len(trailer)-len("\r\n"+marker+"\r\n")], "\n")
	for name, bad := range map[string]string{
		"no closing marker":      cut,
		"a cut closing marker":   trailer[:len(trailer)-4],
		"a wrong tag at the end": strings.Replace(trailer, tag+"\r\n"+marker+"\r\n", "abcdefghijklmnop\r\n"+marker+"\r\n", 1),
		"a wrong length":         trailer[:nl+1] + "1" + trailer[nl+1:],
		"no text":                "\r\n" + marker + "\r\n" + tag + "\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newStream(t, "", tag, `["a"]`+"\n"+`["Int64"]`+"\n[1]\n"+bad)
			n, err := s.count(t)
			if err == nil || n != 1 || !errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("read %d rows and %v, want 1 row and an error that wraps dbimp.ErrIncomplete", n, err)
			}
		})
	}
	_ = fmt.Sprint
}
