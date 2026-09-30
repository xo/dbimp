package rqlite //nolint:testpackage // The tests read the decoder and the encoder, which are not exported.

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestClassOf holds the rules of affinity of SQLite in D140, in their order.
func TestClassOf(t *testing.T) {
	t.Parallel()
	for typ, want := range map[string]class{
		"integer":       classInteger,
		"bigint":        classInteger,
		"int":           classInteger,
		"point":         classInteger, // POINT holds INT, and INT comes first.
		"varchar(10)":   classText,
		"clob":          classText,
		"text":          classText,
		"blob":          classBlob,
		"real":          classReal,
		"double":        classReal,
		"float":         classReal,
		"numeric":       classNumeric,
		"decimal(10,2)": classNumeric,
		"uuid":          classNumeric,
		"json":          classNumeric,
		"boolean":       classBoolean,
		"date":          classDate,
		"datetime":      classDateTime,
		"timestamp":     classTimestamp,
		"any":           classNone,
		"":              classNone,
	} {
		if got := classOf(typ); got != want {
			t.Errorf("classOf(%q) = %d, want %d", typ, got, want)
		}
	}
}

// TestDecode holds D140: a value has the Go type of its class, and a value
// that SQLite stored with another storage class keeps the Go type of its JSON
// form. Each JSON value is one that the server sent (recorded: "every type").
func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		c    class
		v    string
		want any
	}{
		{classInteger, `-9223372036854775808`, int64(math.MinInt64)},
		{classInteger, `9223372036854775807`, int64(math.MaxInt64)},
		{classInteger, `1.5`, 1.5},
		{classInteger, `"abc"`, "abc"},
		{classInteger, `[1]`, []byte{1}},
		{classInteger, `null`, nil},
		{classReal, `1`, 1.0},
		{classReal, `-1.7976931348623157E+308`, -math.MaxFloat64},
		{classReal, `"abc"`, "abc"},
		{classText, `"é'\"\\ x"`, "é'\"\\ x"},
		{classText, `""`, ""},
		{classBlob, `[0,255]`, []byte{0, 255}},
		{classBlob, `[]`, []byte{}},
		{classBlob, `"text in a blob"`, "text in a blob"},
		{classNumeric, `12`, int64(12)},
		{classNumeric, `1.25`, 1.25},
		{classNumeric, `12345678901234567000`, 12345678901234567000.0},
		{classNumeric, `"abc"`, "abc"},
		{classBoolean, `true`, true},
		{classBoolean, `false`, false},
		{classDate, `"2026-09-30T00:00:00Z"`, dbimp.Date{Year: 2026, Month: time.September, Day: 30}},
		{classDate, `"0001-01-01T00:00:00Z"`, dbimp.Date{Year: 1, Month: time.January, Day: 1}},
		{classDateTime, `"2026-09-30T12:34:56.789Z"`, dbimp.LocalDateTime{
			Date: dbimp.Date{Year: 2026, Month: time.September, Day: 30},
			Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000},
		}},
		{classTimestamp, `"2024-09-30T12:34:56Z"`, time.Date(2024, time.September, 30, 12, 34, 56, 0, time.UTC)},
		{classTimestamp, `"not a time"`, "not a time"},
		{classNone, `7`, int64(7)},
		{classNone, `"any"`, "any"},
		{classNone, `[0]`, []byte{0}},
	} {
		got, err := decode(tt.c, jsontext.Value(tt.v))
		if err != nil {
			t.Errorf("decode(%d, %s): %v", tt.c, tt.v, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%d, %s) = %#v (%T), want %#v (%T)", tt.c, tt.v, got, got, tt.want, tt.want)
		}
	}
}

// TestDecodeTimestampKeepsItsOffset holds D140: a TIMESTAMP keeps the
// offset that the server wrote, and its nine digits (recorded: "every
// type").
func TestDecodeTimestampKeepsItsOffset(t *testing.T) {
	t.Parallel()
	got, err := decode(classTimestamp, jsontext.Value(`"2026-09-30T12:34:56.123456789+05:30"`))
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := got.(time.Time)
	if _, off := ts.Zone(); !ok || off != 5*3600+30*60 || ts.Nanosecond() != 123456789 {
		t.Errorf("decode gave %v, want 12:34:56.123456789 at +05:30", got)
	}
}

func TestDecodeRefusesABadBlob(t *testing.T) {
	t.Parallel()
	for _, v := range []string{`[256]`, `[-1]`, `["a"]`, `{}`} {
		if _, err := decode(classBlob, jsontext.Value(v)); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("decode(%s) = %v, want %v", v, err, dbimp.ErrInvalidValue)
		}
	}
}

// TestBody holds D143: each Go value goes as the JSON value that binds as its
// type.
func TestBody(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, time.September, 30, 12, 0, 0, 5, time.FixedZone("", 19800))
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  string
	}{
		{"SELECT 1", nil, `["SELECT 1"]`},
		{"SELECT ?, ?, ?, ?, ?", pos(nil, true, int64(-1), uint64(math.MaxInt64), "a"), `[["SELECT ?, ?, ?, ?, ?",null,true,-1,9223372036854775807,"a"]]`},
		{"SELECT ?, ?, ?", pos(1.0, 1e300, 0.5), `[["SELECT ?, ?, ?",1.0,1e+300,0.5]]`},
		{"SELECT ?, ?", pos([]byte{0, 255}, []byte{}), `[["SELECT ?, ?",[0,255],[]]]`},
		{"SELECT ?", pos(ts), `[["SELECT ?","2026-09-30T12:00:00.000000005+05:30"]]`},
		{"SELECT ?, ?, ?", pos(dbimp.Date{Year: 2026, Month: 9, Day: 30}, dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3}, dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: 9, Day: 30}}),
			`[["SELECT ?, ?, ?","2026-09-30","01:02:03","2026-09-30T00:00:00"]]`},
		{"SELECT ?, @b", []driver.NamedValue{{Ordinal: 1, Value: int64(1)}, {Name: "b", Ordinal: 2, Value: "x"}}, `[["SELECT ?, @b",1,{"b":"x"}]]`},
		// A string of the form x'...' goes into the text as a literal (D143).
		{"SELECT ?, ?", pos("x'41'", "y"), `[["SELECT 'x''41''', ?","y"]]`},
		{"SELECT @a -- ?", []driver.NamedValue{{Name: "a", Ordinal: 1, Value: " X'00' "}}, `["SELECT ' X''00'' ' -- ?"]`},
		{"SELECT '?', ?", pos("x''"), `["SELECT '?', 'x'''''"]`},
	} {
		got, err := body(tt.query, tt.args)
		if err != nil {
			t.Errorf("body(%q): %v", tt.query, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("body(%q) = %s, want %s", tt.query, got, tt.want)
		}
	}
}

func TestBodyRefuses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		query string
		args  []driver.NamedValue
		want  error
	}{
		{"SELECT ?", pos(uint64(math.MaxInt64 + 1)), dbimp.ErrInvalidValue},
		{"SELECT ?", pos(math.NaN()), dbimp.ErrInvalidValue},
		{"SELECT ?", pos(math.Inf(-1)), dbimp.ErrInvalidValue},
		{"SELECT ?", pos(dbimp.Interval{Days: 1}), dbimp.ErrNotSupported},
		// The parser of the root package reads a ? and an @name only (D143).
		{"SELECT ?1", pos("x'41'"), dbimp.ErrNotSupported},
		{"SELECT :a", []driver.NamedValue{{Name: "a", Ordinal: 1, Value: "x'41'"}}, dbimp.ErrNotSupported},
		{"SELECT ?, ?", pos("x'41'"), dbimp.ErrArguments},
	} {
		if _, err := body(tt.query, tt.args); !errors.Is(err, tt.want) {
			t.Errorf("body(%q, %v) = %v, want %v", tt.query, tt.args, err, tt.want)
		}
	}
}

// TestCountsRows holds D142: the counts of the server are those of a
// statement that counts rows, and 0 for any other.
func TestCountsRows(t *testing.T) {
	t.Parallel()
	for q, want := range map[string]bool{
		"INSERT INTO t VALUES (1)":           true,
		"  update t SET a = 1":               true,
		"-- a comment\nDELETE FROM t":        true,
		"/* a */ REPLACE INTO t VALUES (1)":  true,
		"WITH x AS (SELECT 1) INSERT INTO t": true,
		"(SELECT 1)":                         false,
		"CREATE TABLE t (a)":                 false,
		"DROP TABLE t":                       false,
		"BEGIN":                              false,
		"SELECT 1":                           false,
		"-- only a comment":                  false,
		"/* never closed":                    false,
		"":                                   false,
	} {
		if got := countsRows(q); got != want {
			t.Errorf("countsRows(%q) = %v, want %v", q, got, want)
		}
	}
}

// pos returns vals as positional arguments.
func pos(vals ...any) []driver.NamedValue {
	args := make([]driver.NamedValue, len(vals))
	for i, v := range vals {
		args[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return args
}
