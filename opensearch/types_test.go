package opensearch_test

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestDecodeTypes holds D135 and D168 for the forms of each type, which the
// recorded answers do not all hold: the wire type and the JSON of a value, and
// the Go value that a scan into *any gives.
func TestDecodeTypes(t *testing.T) {
	t.Parallel()
	utc := func(y int, m time.Month, d, h, mi, s, ns int) time.Time {
		return time.Date(y, m, d, h, mi, s, ns, time.UTC)
	}
	for _, tt := range []struct {
		name string
		typ  string
		json string
		want any
	}{
		{"null of any type", "integer", `null`, nil},
		{"undefined", "undefined", `null`, nil},
		{"a boolean", "boolean", `true`, true},
		{"a byte", "byte", `-128`, int64(-128)},
		{"a long", "long", `9223372036854775807`, int64(9223372036854775807)},
		{"a float", "float", `3.4028235e+38`, 3.4028235e+38},
		{"a float that is a whole number", "double", `1`, float64(1)},
		{"a half_float of the legacy engine", "half_float", `65504.0`, 65504.0},
		{"a scaled_float of the legacy engine", "scaled_float", `12.345`, 12.345},
		{"a keyword", "keyword", `"é'\"\\ x"`, "é'\"\\ x"},
		{"an empty text", "text", `""`, ""},
		{"an ip", "ip", `"::1"`, "::1"},
		{"a binary", "binary", `"AP8="`, []byte{0, 255}},
		{"an empty binary", "binary", `""`, []byte{}},
		{"a date", "date", `"0001-01-01"`, dbimp.Date{Year: 1, Month: 1, Day: 1}},
		{"a time of day", "time", `"23:59:59"`, dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59}},
		{"a time of day with a fraction", "time", `"12:34:56.789"`, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789e6}},
		{"a timestamp", "timestamp", `"2026-10-01 07:04:56.123"`, utc(2026, 10, 1, 7, 4, 56, 123e6)},
		{"a timestamp with nine digits", "timestamp", `"2262-04-11 23:47:16.854775807"`, utc(2262, 4, 11, 23, 47, 16, 854775807)},
		{"a timestamp with no fraction", "timestamp", `"2026-10-01 12:00:00"`, utc(2026, 10, 1, 12, 0, 0, 0)},
		{"a datetime", "datetime", `"2026-10-01 12:34:56.5"`, utc(2026, 10, 1, 12, 34, 56, 5e8)},
		{"a timestamp in ISO 8601 with an offset", "timestamp", `"2026-10-01T12:34:56.123+05:30"`, utc(2026, 10, 1, 7, 4, 56, 123e6)},
		{"a geo_point", "geo_point", `{"lat":41.12,"lon":-71.34}`, map[string]any{"lat": 41.12, "lon": -71.34}},
		{"an object", "object", `{"a":1,"b":"x"}`, map[string]any{"a": int64(1), "b": "x"}},
		{"a nested field", "nested", `[{"a":1},{"a":2}]`, []any{map[string]any{"a": int64(1)}, map[string]any{"a": int64(2)}}},
		{"a nested field with no object", "nested", `[]`, []any{}},
		{"an integer_range", "integer_range", `{"gte":1,"lte":5}`, map[string]any{"gte": int64(1), "lte": int64(5)}},
		{"a field with several integers", "integer", `[1,2,3]`, []any{int64(1), int64(2), int64(3)}},
		{"a field with several keywords", "keyword", `["a","b"]`, []any{"a", "b"}},
		{"a field with a null among doubles", "double", `[1.5,null,2.5]`, []any{1.5, nil, 2.5}},
		{"a field with several timestamps", "timestamp", `["2026-10-01 07:04:56.123","1970-01-01 00:00:00"]`, []any{utc(2026, 10, 1, 7, 4, 56, 123e6), utc(1970, 1, 1, 0, 0, 0, 0)}},
		{"a field with several dates", "date", `["2026-10-01"]`, []any{dbimp.Date{Year: 2026, Month: 10, Day: 1}}},
		{"a field with several objects", "object", `[{"a":1},{"a":2}]`, []any{map[string]any{"a": int64(1)}, map[string]any{"a": int64(2)}}},
		{"an empty field with several values", "long", `[]`, []any{}},
		{"a type that the driver does not know", "struct", `{"x":[1,"a"]}`, map[string]any{"x": []any{int64(1), "a"}}},
		{"an unknown type with a string", "interval", `"P1D"`, "P1D"},
	} {
		f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			reply(w, http.StatusOK, `{"schema":[{"name":"v","type":"`+tt.typ+`"}],"datarows":[[`+tt.json+`]],"total":1,"size":1,"status":200}`)
		}}
		db := f.open(t, "", "")
		_, got, err := readAll(t, db, "SELECT v FROM t")
		if err != nil || len(got) != 1 {
			t.Errorf("%s: read %v and %v", tt.name, got, err)
			continue
		}
		if !reflect.DeepEqual(got[0][0], tt.want) {
			t.Errorf("%s: the value is %#v, want %#v", tt.name, got[0][0], tt.want)
		}
	}
}

// TestDecodeFailures holds that a value that does not fit its type is an error
// that wraps dbimp.ErrInvalidValue, and never a wrong value (D8). The legacy
// engine sends such values for some types (recorded: "every type on the legacy
// engine").
func TestDecodeFailures(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		typ  string
		json string
	}{
		{"a string in a long", "long", `"42"`},
		{"a fraction in a long", "long", `1.5`},
		{"a string in a double", "double", `"g1"`},
		{"a number in a boolean", "boolean", `1`},
		{"a string in a boolean", "boolean", `"true"`},
		{"a number in a keyword", "keyword", `1`},
		{"a base64 that is not", "binary", `"%%"`},
		{"a timestamp in a date", "date", `"2026-10-01T12:34:56.123+05:30"`},
		{"a text in a date", "date", `"x"`},
		{"a text in a time", "time", `"x"`},
		{"a text in a timestamp", "timestamp", `"2026-13-01 00:00:00"`},
		{"a number in a timestamp", "timestamp", `1790858096123`},
		{"a bad element of a field with several values", "long", `[1,"x"]`},
	} {
		f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			reply(w, http.StatusOK, `{"schema":[{"name":"v","type":"`+tt.typ+`"}],"datarows":[[`+tt.json+`]],"total":1,"size":1,"status":200}`)
		}}
		db := f.open(t, "", "")
		_, got, err := readAll(t, db, "SELECT v FROM t")
		if !errors.Is(err, dbimp.ErrInvalidValue) || len(got) != 0 {
			t.Errorf("%s: read %v and %v, want no row and dbimp.ErrInvalidValue", tt.name, got, err)
		}
	}
}

// TestLabelOfAColumn holds that a column has the label of its alias, the text
// of its expression when it has no alias, and the text of its expression when
// the alias is empty, as the legacy engine writes it for an object (recorded:
// "a column with an alias" and "every type on the legacy engine").
func TestLabelOfAColumn(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"schema":[{"name":"n","alias":"x","type":"integer"},{"name":"n + 1","type":"integer"},{"name":"o","alias":"","type":"text"}],"datarows":[],"total":0,"size":0,"status":200}`)
	}}
	db := f.open(t, "", "")
	cols, _, err := readAll(t, db, "SELECT n AS x, n + 1, o FROM t")
	if err != nil || !reflect.DeepEqual(cols, []string{"x", "n + 1", "o"}) {
		t.Errorf("the columns are %v, and the error %v", cols, err)
	}
}

// TestLegacyObjectInATextColumn holds that the legacy engine, which names an
// object and a nested field text and sends each value as an object or an array
// (recorded: "every type on the legacy engine"), gives an error and not the text
// of JSON, because no decision names another form (hard rule 3, and the open
// questions of docs/OPENSEARCH.md).
func TestLegacyObjectInATextColumn(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"schema":[{"name":"o","alias":"","type":"text"}],"datarows":[[{"a":1}]],"total":1,"size":1,"status":200}`)
	}}
	db := f.open(t, "", "")
	_, got, err := readAll(t, db, "SELECT o FROM t")
	if !errors.Is(err, dbimp.ErrInvalidValue) || len(got) != 0 {
		t.Errorf("read %v and %v, want no row and dbimp.ErrInvalidValue", got, err)
	}
}

// TestLegacyDescribeNumberInAKeywordColumn holds D178, item 17. The legacy
// DESCRIBE TABLES of 2.19.6 names every column keyword and sends numbers in
// some of them. The driver gives the number that arrived, an int64 for an
// integer and a float64 for a fraction, and no error. The scan type stays the
// type of the schema.
func TestLegacyDescribeNumberInAKeywordColumn(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"schema":[{"name":"COLUMN_NAME","type":"keyword"},{"name":"NUM_PREC_RADIX","type":"keyword"},{"name":"NULLABLE","type":"keyword"},{"name":"SCALE","type":"keyword"}],"datarows":[["n",10,2,1.5],["s",null,2,null]],"total":2,"size":2,"status":200}`)
	}}
	db := f.open(t, "", "")
	_, got, err := readAll(t, db, "DESCRIBE TABLES LIKE t")
	want := [][]any{{"n", int64(10), int64(2), float64(1.5)}, {"s", nil, int64(2), nil}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("read %#v and %v, want %#v", got, err, want)
	}
	rows, err := db.QueryContext(t.Context(), "DESCRIBE TABLES LIKE t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil || types[1].ScanType() != reflect.TypeFor[string]() {
		t.Fatalf("the column types are %v and the error %v, want the scan type string", types, err)
	}
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestNumberInAKeywordColumnOfASelect holds that the rule of D178, item 17 is
// for DESCRIBE alone: a number in a keyword column of a SELECT still fails the
// row.
func TestNumberInAKeywordColumnOfASelect(t *testing.T) {
	t.Parallel()
	f := &fake{handle: func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		reply(w, http.StatusOK, `{"schema":[{"name":"k","alias":"","type":"keyword"}],"datarows":[[10]],"total":1,"size":1,"status":200}`)
	}}
	db := f.open(t, "", "")
	_, got, err := readAll(t, db, "SELECT k FROM t")
	if !errors.Is(err, dbimp.ErrInvalidValue) || len(got) != 0 {
		t.Errorf("read %v and %v, want no row and dbimp.ErrInvalidValue", got, err)
	}
}
