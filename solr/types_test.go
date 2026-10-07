package solr //nolint:testpackage // The tests read the decoder, the literals and the schema, which are not exported.

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

// TestDecode holds D135 and D166: a value has the Go type of its column, and a
// NULL is nil for every type.
func TestDecode(t *testing.T) {
	t.Parallel()
	u := uuid.MustParse("8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b")
	big, _, err := apd.NewFromString("9223372036854775808")
	if err != nil {
		t.Fatal(err)
	}
	varchar := func(class string) column { return column{sql: typeVarchar, class: class} }
	for _, tt := range []struct {
		name string
		col  column
		in   string
		want any
	}{
		{"null of a string", varchar("StrField"), `null`, nil},
		{"null of an int", column{sql: typeBigint}, `null`, nil},
		{"null of an array", column{sql: typeAny}, `null`, nil},
		{"null of an aggregate", column{}, `null`, nil},
		{"string", varchar("StrField"), `"héllo 🙂"`, "héllo 🙂"},
		{"empty string", varchar("StrField"), `""`, ""},
		{"int", column{sql: typeBigint}, `-9223372036854775808`, int64(math.MinInt64)},
		{"float of an integer", column{sql: typeDouble}, `2`, 2.0},
		{"float", column{sql: typeDouble}, `3.4028235E38`, 3.4028235e38},
		{"float with a fraction", column{sql: typeDouble}, `1.7976931348623157E308`, math.MaxFloat64},
		{"bool true", varchar(classBool), `"true"`, true},
		{"bool false", varchar(classBool), `"false"`, false},
		{"bool as JSON", varchar(classBool), `true`, true},
		{"time", column{sql: typeTimestamp}, `"2026-10-01T12:34:56.123Z"`, time.Date(2026, 10, 1, 12, 34, 56, 123_000_000, time.UTC)},
		{"first time", column{sql: typeTimestamp}, `"0001-01-01T00:00:00Z"`, time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"last time", column{sql: typeTimestamp}, `"9999-12-31T23:59:59.999Z"`, time.Date(9999, 12, 31, 23, 59, 59, 999_000_000, time.UTC)},
		{"binary", varchar(classBinary), `"AAEC/w=="`, []byte{0, 1, 2, 0xff}},
		{"empty binary", varchar(classBinary), `""`, []byte{}},
		{"uuid", varchar(classUUID), `"8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b"`, u},
		{"array of ints", column{sql: typeAny}, `[9223372036854775807,-1]`, []any{int64(math.MaxInt64), int64(-1)}},
		{"array of bools", column{sql: typeAny}, `[true,false]`, []any{true, false}},
		{"array of strings", column{sql: typeAny}, `["a",""]`, []any{"a", ""}},
		{"empty array", column{sql: typeAny}, `[]`, []any{}},
		{"aggregate int", column{}, `303`, int64(303)},
		{"aggregate float", column{}, `8.988465674311579E307`, 8.988465674311579e307},
		{"aggregate big", column{}, `9223372036854775808`, big},
		{"aggregate string", column{}, `"x"`, "x"},
		{"a string where an int is typed", column{sql: typeBigint}, `"x"`, "x"},
		{"a number where a string is typed", varchar("StrField"), `1`, int64(1)},
	} {
		got, err := decode(tt.col, jsontext.Value(tt.in))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if d, ok := got.(*apd.Decimal); ok {
			if w, ok := tt.want.(*apd.Decimal); !ok || d.Cmp(w) != 0 {
				t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
			}
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %#v, want %#v", tt.name, got, tt.want)
		}
	}
}

// TestDecodeRefuses holds that a value that is not in the form of its type
// fails with dbimp.ErrInvalidValue.
func TestDecodeRefuses(t *testing.T) {
	t.Parallel()
	varchar := func(class string) column { return column{sql: typeVarchar, class: class} }
	for _, tt := range []struct {
		name string
		col  column
		in   string
	}{
		{"bool", varchar(classBool), `"yes"`},
		{"time", column{sql: typeTimestamp}, `"yesterday"`},
		{"binary", varchar(classBinary), `"not base64!"`},
		{"uuid", varchar(classUUID), `"nothing"`},
	} {
		if _, err := decode(tt.col, jsontext.Value(tt.in)); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("%s: the error is %v, want dbimp.ErrInvalidValue", tt.name, err)
		}
	}
}

// TestLiteral holds D34 and D166: each argument is a literal of Solr SQL.
func TestLiteral(t *testing.T) {
	t.Parallel()
	dec, _, err := apd.NewFromString("-0.000001")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		in   any
		want string
	}{
		{nil, "NULL"},
		{"", "''"},
		{"it's", "'it''s'"},
		{`a\b"c`, `'a\b"c'`},
		{int64(math.MaxInt64), "9223372036854775807"},
		{1.0, "1"},
		{0.1, "0.1"},
		{1e21, "1e+21"},
		{true, "TRUE"},
		{false, "FALSE"},
		{[]byte{}, "''"},
		{[]byte("hi"), "'aGk='"},
		{time.Date(2026, 10, 1, 12, 34, 56, 123_456_789, time.UTC), "'2026-10-01T12:34:56.123Z'"},
		{uuid.MustParse("8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b"), "'8a3f1c2e-0b7d-4f6e-9a1b-2c3d4e5f6a7b'"},
		{dec, "-0.000001"},
		{(*apd.Decimal)(nil), "NULL"},
		{dbimp.Date{Year: 2026, Month: 10, Day: 1}, "'2026-10-01T00:00:00.000Z'"},
	} {
		got, err := literal(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("literal(%#v) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []any{math.NaN(), math.Inf(1), struct{}{}, []int{1}, &apd.Decimal{Form: apd.Infinite}} {
		if _, err := literal(in); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("literal(%#v) gave %v, want dbimp.ErrNotSupported", in, err)
		}
	}
}

// TestTables holds that the driver finds the tables that a statement reads.
func TestTablesOf(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		stmt string
		want []string
	}{
		{"SELECT id FROM dbimp", []string{"dbimp"}},
		{"select id from dbimp where id = 'from x'", []string{"dbimp"}},
		{"SELECT id FROM `dbimp` WHERE id = 1", []string{"dbimp"}},
		{`SELECT id FROM "a b"`, []string{"a b"}},
		{"SELECT id FROM `a``b`", []string{"a`b"}},
		{"SELECT a.id FROM dbimp a JOIN dbmeta AS b ON a.id = b.id LIMIT 5", []string{"dbimp", "dbmeta"}},
		{"SELECT * FROM a, b WHERE 1 = 1", []string{"a", "b"}},
		{"SELECT * FROM a x, b y", []string{"a", "b"}},
		{"SELECT columnName FROM metadata.COLUMNS WHERE tableName = 'dbimp'", nil},
		{"SELECT * FROM (SELECT id FROM inner1) t JOIN outer1 ON 1 = 1", []string{"inner1", "outer1"}},
		{"-- from x\nSELECT id /* from y */ FROM dbimp", []string{"dbimp"}},
		{"SELECT id FROM dbimp UNION SELECT id FROM other", []string{"dbimp", "other"}},
		{"EXPLAIN PLAN FOR SELECT id FROM dbimp ORDER BY id LIMIT 1", []string{"dbimp"}},
		{"SELECT b, a, c", nil},
		{"SELECT 'it''s from x' FROM t", []string{"t"}},
		{"SELECT id FROM", nil},
		{"SELECT id FROM 'x", nil},
	} {
		if got := tables(tt.stmt); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("tables(%q) = %q, want %q", tt.stmt, got, tt.want)
		}
	}
}

// TestSchemaColumn holds that the schema matches a name as the server does:
// exactly, and then without regard to case, and that a dynamic field gives
// the class of its pattern.
func TestSchemaColumn(t *testing.T) {
	t.Parallel()
	s := &schema{
		sql:     map[string]string{"id": typeVarchar, "n_b": typeVarchar, "n_x_b": typeVarchar, "attr_x": typeVarchar},
		class:   map[string]string{"id": "StrField", "n_b": classBool},
		dynamic: []dynamicField{{"*_b", classBool}, {"attr_*", "TextField"}},
	}
	for _, tt := range []struct {
		name string
		want column
		ok   bool
	}{
		{"id", column{typeVarchar, "StrField"}, true},
		{"ID", column{typeVarchar, "StrField"}, true},
		{"N_B", column{typeVarchar, classBool}, true},
		{"n_x_b", column{typeVarchar, classBool}, true},
		{"attr_x", column{typeVarchar, "TextField"}, true},
		{"nothing", column{}, false},
		{"EXPR$0", column{}, false},
	} {
		got, ok := s.column(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("column(%q) = %v, %v, want %v, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
	var none *schema
	if _, ok := none.column("id"); ok {
		t.Error("a nil schema knows a column")
	}
}

// TestKind holds the Go type of each column that the schema types (D166).
func TestKind(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		col  column
		want string
	}{
		{column{typeVarchar, "StrField"}, "string"},
		{column{typeVarchar, classBool}, "bool"},
		{column{typeVarchar, classBinary}, "[]uint8"},
		{column{typeVarchar, classUUID}, "uuid.UUID"},
		{column{typeBigint, "IntPointField"}, "int64"},
		{column{typeDouble, "FloatPointField"}, "float64"},
		{column{typeTimestamp, "DatePointField"}, "time.Time"},
		{column{typeAny, "StringsField"}, "[]interface {}"},
		{column{}, "interface {}"},
	} {
		if got := scanType(tt.col).String(); got != tt.want {
			t.Errorf("the scan type of %v is %s, want %s", tt.col, got, tt.want)
		}
	}
}
