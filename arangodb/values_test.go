package arangodb //nolint:testpackage // These tests read the decoder and the bind parameters, which are not exported.

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"
)

func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in   string
		want any
	}{
		{`null`, nil},
		{`true`, true},
		{`9223372036854775807`, int64(math.MaxInt64)},
		{`-9223372036854775808.0`, float64(math.MinInt64)},
		{`18446744073709552000`, float64(18446744073709552000)},
		{`1.5`, 1.5},
		{`"é"`, "é"},
		{`[1,"a",null]`, []any{int64(1), "a", nil}},
		{`{"b":1,"a":{"c":[]}}`, map[string]any{"b": int64(1), "a": map[string]any{"c": []any{}}}},
	} {
		got, err := decode(jsontext.Value(tt.in))
		if err != nil {
			t.Errorf("decode(%s): %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode(%s) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestBindVars(t *testing.T) {
	t.Parallel()
	d, _, err := apd.NewFromString("1.25")
	if err != nil {
		t.Fatal(err)
	}
	got, err := bindVars("FOR u IN @@c FILTER u.a == @a AND u.b IN @list RETURN [@4, @t, @d]", []driver.NamedValue{
		{Name: "c", Value: "types"},
		{Name: "a", Value: uint64(math.MaxUint64)},
		{Name: "list", Value: []string{"x", "y"}},
		{Ordinal: 4, Value: nil},
		{Name: "t", Value: time.Date(2024, 1, 2, 3, 4, 5, 6, time.FixedZone("", 3600))},
		{Name: "d", Value: d},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"@c": `"types"`, "a": `18446744073709551615`, "list": `["x","y"]`, "4": `null`,
		"t": `"2024-01-02T02:04:05.000000006Z"`, "d": `1.25`,
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Errorf("the bind parameter %s is %s, want %s", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("the bind parameters are %v, want only %v", got, want)
	}
	for _, v := range []any{[]byte("x"), math.NaN(), make(chan int)} {
		if _, err := bindVars("RETURN @x", []driver.NamedValue{{Name: "x", Value: v}}); err == nil {
			t.Errorf("bindVars took %T, which AQL or JSON cannot hold", v)
		}
	}
}

// TestStored holds the keys of a stored document and of a stored edge, which
// D89 reads as one column.
func TestStored(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		keys []string
		want bool
	}{
		{[]string{"_key", "_id", "_rev", "name"}, true},
		{[]string{"_key", "_id", "_from", "_to", "_rev", "w"}, true},
		{[]string{"_key", "_id", "_from", "_to"}, false},
		{[]string{"_key", "_id"}, false},
		{[]string{"_id", "_key", "_rev"}, false},
		{[]string{"a", "b"}, false},
	} {
		if got := stored(tt.keys); got != tt.want {
			t.Errorf("stored(%q) = %t, want %t", tt.keys, got, tt.want)
		}
	}
}

// TestTagged holds where the comment of cancel=tag goes: at the end of a
// query, and at the start of a query that would then pass the length that the
// list of running queries keeps (D99).
func TestTagged(t *testing.T) {
	t.Parallel()
	const tag = "dbimp:ABC-1"
	short := tagged("RETURN 1", tag)
	if short != "RETURN 1\n// "+tag || !isTagged(short, tag) {
		t.Errorf("tagged a short query as %q", short)
	}
	long := strings.Repeat("x", maxQueryText)
	got := tagged(long, tag)
	if got != "// "+tag+"\n"+long || !isTagged(got[:maxQueryText]+"... (22)", tag) {
		t.Errorf("tagged a long query as %.40q...", got)
	}
	if isTagged("RETURN 1\n// dbimp:ABC-12", tag) {
		t.Error("the tag dbimp:ABC-1 matched the query of dbimp:ABC-12")
	}
}

// TestBindVarsSkipsLiterals holds that a string or a comment that holds
// @@c does not make c a collection (D104).
func TestBindVarsSkipsLiterals(t *testing.T) {
	t.Parallel()
	got, err := bindVars("RETURN [@c, '@@c'] // @@c", []driver.NamedValue{{Name: "c", Value: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["@c"]; ok || string(got["c"]) != `"x"` {
		t.Errorf("bindVars gave %v, want only c as a value", got)
	}
}
