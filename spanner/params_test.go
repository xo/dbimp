package spanner //nolint:testpackage // The tests read the binding of the driver, which is not exported.

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// args numbers the values as database/sql does for positional arguments.
func args(vals ...any) []driver.NamedValue {
	out := make([]driver.NamedValue, len(vals))
	for i, v := range vals {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

// TestBindArgs holds D191 item 7: a ? becomes @p1, @p2 by the number of its
// argument, the type goes in paramTypes, an argument named with sql.Named keeps
// its name, a ? or an @ inside a literal or a comment is not a placeholder, and a
// statement with no argument goes as it is.
func TestBindArgs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		args  []driver.NamedValue
		text  string
		par   map[string]any
		types map[string]wireType
	}{
		{"none", "SELECT 1", nil, "SELECT 1", nil, nil},
		{"a question mark in a literal", "SELECT '?' AS q, \"?\" AS d, `?` AS b", nil, "SELECT '?' AS q, \"?\" AS d, `?` AS b", nil, nil},
		{"positional", "SELECT ?, ? FROM t WHERE c = ?", args(int64(1), "x", true), "SELECT @p1, @p2 FROM t WHERE c = @p3",
			map[string]any{"p1": "1", "p2": "x", "p3": true},
			map[string]wireType{"p1": {Code: wireInt64}, "p2": {Code: wireString}, "p3": {Code: wireBool}}},
		{"placeholders in text and comments", "SELECT ? /* ? */ -- ?\n, '?', ?", args(int64(1), int64(2)), "SELECT @p1 /* ? */ -- ?\n, '?', @p2",
			map[string]any{"p1": "1", "p2": "2"}, map[string]wireType{"p1": {Code: wireInt64}, "p2": {Code: wireInt64}}},
		{"named", "SELECT @id, @name", []driver.NamedValue{{Name: "id", Ordinal: 1, Value: int64(5)}, {Name: "name", Ordinal: 2, Value: "n"}}, "SELECT @id, @name",
			map[string]any{"id": "5", "name": "n"}, map[string]wireType{"id": {Code: wireInt64}, "name": {Code: wireString}}},
		{"a positional argument for @p1", "SELECT @p1", args(int64(9)), "SELECT @p1",
			map[string]any{"p1": "9"}, map[string]wireType{"p1": {Code: wireInt64}}},
		{"a NULL has no type", "SELECT ?", args(nil), "SELECT @p1", map[string]any{"p1": nil}, map[string]wireType{}},
		{"a system variable", "SELECT @@session.x, ?", args(int64(1)), "SELECT @@session.x, @p1", map[string]any{"p1": "1"}, map[string]wireType{"p1": {Code: wireInt64}}},
		{"a hint", "@{FORCE_INDEX=i} SELECT ?", args(int64(1)), "@{FORCE_INDEX=i} SELECT @p1", map[string]any{"p1": "1"}, map[string]wireType{"p1": {Code: wireInt64}}},
	} {
		b, err := bindArgs(tt.query, tt.args)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if b.text != tt.text || !reflect.DeepEqual(b.params, tt.par) || (tt.types != nil || b.paramTypes != nil) && !reflect.DeepEqual(b.paramTypes, tt.types) {
			t.Errorf("%s: got %q %v %v, want %q %v %v", tt.name, b.text, b.params, b.paramTypes, tt.text, tt.par, tt.types)
		}
	}
}

// TestBindArgsRefuses holds D191 item 7: a statement that mixes the two forms is
// an error, and so is a count of arguments that differs from the count of ?, and
// a statement that ends in a literal.
func TestBindArgsRefuses(t *testing.T) {
	t.Parallel()
	named := func(n string, v any) driver.NamedValue { return driver.NamedValue{Name: n, Ordinal: 1, Value: v} }
	for name, tt := range map[string]struct {
		query string
		args  []driver.NamedValue
		want  error
	}{
		"mixed in the statement":  {"SELECT ?, @a", []driver.NamedValue{named("a", int64(1))}, dbimp.ErrArguments},
		"mixed in the arguments":  {"SELECT ?, ?", []driver.NamedValue{named("a", int64(1)), {Ordinal: 2, Value: int64(2)}}, dbimp.ErrArguments},
		"too few arguments":       {"SELECT ?, ?", args(int64(1)), dbimp.ErrArguments},
		"too many arguments":      {"SELECT ?", args(int64(1), int64(2)), dbimp.ErrArguments},
		"no argument for a ?":     {"SELECT ?", nil, dbimp.ErrArguments},
		"named arguments for a ?": {"SELECT ?", []driver.NamedValue{named("a", int64(1))}, dbimp.ErrArguments},
		"an unterminated literal": {"SELECT 'abc", args(int64(1)), dbimp.ErrUnterminated},
		"an unterminated comment": {"SELECT /* abc", args(int64(1)), dbimp.ErrUnterminated},
		"an argument of no type":  {"SELECT ?", args(struct{}{}), dbimp.ErrArguments},
	} {
		_, err := bindArgs(tt.query, tt.args)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", name, err, tt.want)
		}
	}
}

// TestBindValue holds the JSON form of each type of an argument, as the recorded
// requests wrote it (docs/SPANNER.md, "Parameters").
func TestBindValue(t *testing.T) {
	t.Parallel()
	stamp := time.Date(2024, time.January, 2, 3, 4, 5, 123456789, time.FixedZone("", 7*3600))
	u := uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")
	for name, tt := range map[string]struct {
		in    any
		code  string
		value any
	}{
		"int64 is a string":          {int64(math.MaxInt64), wireInt64, "9223372036854775807"},
		"the least int64":            {int64(math.MinInt64), wireInt64, "-9223372036854775808"},
		"float64 is a number":        {1.5, wireFloat64, 1.5},
		"NaN is a string":            {math.NaN(), wireFloat64, "NaN"},
		"+Inf is a string":           {math.Inf(1), wireFloat64, "Infinity"},
		"-Inf is a string":           {math.Inf(-1), wireFloat64, "-Infinity"},
		"bool":                       {true, wireBool, true},
		"string":                     {"héllo", wireString, "héllo"},
		"the empty string":           {"", wireString, ""},
		"bytes are base64":           {[]byte("abc"), wireBytes, "YWJj"},
		"empty bytes":                {[]byte{}, wireBytes, ""},
		"a time is UTC":              {stamp, wireTimestamp, "2024-01-01T20:04:05.123456789Z"},
		"a date":                     {dbimp.Date{Year: 2024, Month: 1, Day: 2}, wireDate, "2024-01-02"},
		"the first date":             {dbimp.Date{Year: 1, Month: 1, Day: 1}, wireDate, "0001-01-01"},
		"a decimal":                  {dec(t, "99999999999999999999999999999.999999999"), wireNumeric, "99999999999999999999999999999.999999999"},
		"a decimal with an exponent": {dec(t, "1.5E+3"), wireNumeric, "1500"},
		"a uuid":                     {u, wireUUID, "f47ac10b-58cc-4372-a567-0e02b2c3d479"},
		"an interval":                {dbimp.Interval{Months: 14, Days: 3, Nanoseconds: (4*3600 + 5*60 + 6) * 1e9}, wireInterval, "P1Y2M3DT4H5M6S"},
		"the zero interval":          {dbimp.Interval{}, wireInterval, "P0Y"},
		"JSON text":                  {jsontext.Value(`{"a":1}`), wireJSON, `{"a":1}`},
		"a map is JSON":              {map[string]any{"b": []any{int64(1), nil}, "a": "x"}, wireJSON, `{"a":"x","b":[1,null]}`},
	} {
		typ, v, err := bindValue(tt.in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if typ.Code != tt.code {
			t.Errorf("%s: the type is %s, want %s", name, typ.Code, tt.code)
		}
		if f, ok := tt.value.(float64); ok && !math.IsNaN(f) {
			if g, ok := v.(float64); !ok || g != f {
				t.Errorf("%s: the value is %#v, want %v", name, v, f)
			}
			continue
		}
		if !reflect.DeepEqual(v, tt.value) {
			t.Errorf("%s: the value is %#v, want %#v", name, v, tt.value)
		}
	}
}

// TestBindValueRefuses holds that a value that no Spanner type holds is an
// error, and so is a date that is not valid, a number that is not finite as a
// decimal, and JSON that is not JSON.
func TestBindValueRefuses(t *testing.T) {
	t.Parallel()
	inf := new(apd.Decimal)
	inf.Form = apd.Infinite
	for name, v := range map[string]any{
		"a struct":              struct{}{},
		"a local time":          dbimp.LocalTime{Hour: 1},
		"a date out of range":   dbimp.Date{Year: 2024, Month: 13, Day: 1},
		"an infinite decimal":   inf,
		"JSON that is not JSON": jsontext.Value(`{`),
	} {
		if _, _, err := bindValue(v); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestCheckNamedValue holds W4: an Option, and the values that the driver binds
// with a type of its own, stay for the driver, and every other value goes to the
// converter of database/sql.
func TestCheckNamedValue(t *testing.T) {
	t.Parallel()
	c := &conn{}
	var nilDecimal *apd.Decimal
	for name, tt := range map[string]struct {
		in   any
		want any
		skip bool
	}{
		"an Option":       {in: WithReadonly(true)},
		"a decimal":       {in: dec(t, "1.5"), want: dec(t, "1.5")},
		"a nil decimal":   {in: nilDecimal, want: nil},
		"a decimal value": {in: *dec(t, "1.5"), want: dec(t, "1.5")},
		"a date":          {in: dbimp.Date{Year: 2024, Month: 1, Day: 2}, want: dbimp.Date{Year: 2024, Month: 1, Day: 2}},
		"an interval":     {in: dbimp.Interval{Days: 1}, want: dbimp.Interval{Days: 1}},
		"a uuid":          {in: uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"), want: uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")},
		"JSON":            {in: jsontext.Value(`1`), want: jsontext.Value(`1`)},
		"a map":           {in: map[string]any{"a": 1}, want: map[string]any{"a": 1}},
		"a float32":       {in: float32(1.5), want: float32(1.5)},
		"an int":          {in: 5, skip: true},
		"a string":        {in: "x", skip: true},
		"bytes":           {in: []byte("x"), skip: true},
		"a time":          {in: time.Now(), skip: true},
		"a NULL":          {in: nil, skip: true},
		"sql.NullString":  {in: sql.NullString{}, skip: true},
	} {
		nv := &driver.NamedValue{Value: tt.in}
		err := c.CheckNamedValue(nv)
		switch {
		case tt.skip:
			if !errors.Is(err, driver.ErrSkip) {
				t.Errorf("%s: the error is %v, want driver.ErrSkip", name, err)
			}
		case err != nil:
			t.Errorf("%s: %v", name, err)
		case name == "an Option":
			if !dbimp.IsOption[options](nv.Value) {
				t.Errorf("%s: the value is not an Option", name)
			}
		case !equalValue(nv.Value, tt.want) && !reflect.DeepEqual(nv.Value, tt.want):
			t.Errorf("%s: the value is %#v, want %#v", name, nv.Value, tt.want)
		}
	}
}

// TestSlicesAreArrays holds that a slice binds as an ARRAY: a slice of a type
// keeps its element type when it is empty and when it holds NULLs, a []any takes
// the type of its first element that is not nil, and a slice of mixed types or of
// NULLs only is an error.
func TestSlicesAreArrays(t *testing.T) {
	t.Parallel()
	c := &conn{}
	var nilDecimal *apd.Decimal
	for name, tt := range map[string]struct {
		in    any
		code  string
		value any
	}{
		"int64s":          {[]int64{1, 2}, wireInt64, []any{"1", "2"}},
		"ints":            {[]int{1, 2}, wireInt64, []any{"1", "2"}},
		"no int64":        {[]int64{}, wireInt64, []any{}},
		"strings":         {[]string{"a", ""}, wireString, []any{"a", ""}},
		"floats":          {[]float64{1.5, math.NaN()}, wireFloat64, []any{1.5, "NaN"}},
		"float32s":        {[]float32{1.5}, wireFloat32, []any{1.5}},
		"bools":           {[]bool{true}, wireBool, []any{true}},
		"byte slices":     {[][]byte{[]byte("abc"), nil}, wireBytes, []any{"YWJj", nil}},
		"times":           {[]time.Time{time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}, wireTimestamp, []any{"2024-01-02T03:04:05Z"}},
		"dates":           {[]dbimp.Date{{Year: 2024, Month: 1, Day: 2}}, wireDate, []any{"2024-01-02"}},
		"decimals":        {[]*apd.Decimal{dec(t, "1.5"), nilDecimal}, wireNumeric, []any{"1.5", nil}},
		"uuids":           {[]uuid.UUID{uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")}, wireUUID, []any{"f47ac10b-58cc-4372-a567-0e02b2c3d479"}},
		"JSON":            {[]jsontext.Value{jsontext.Value(`{"a":1}`)}, wireJSON, []any{`{"a":1}`}},
		"any with a NULL": {[]any{nil, "x"}, wireString, []any{nil, "x"}},
		"any with ints":   {[]any{1, int64(2), nil}, wireInt64, []any{"1", "2", nil}},
	} {
		nv := &driver.NamedValue{Value: tt.in}
		if err := c.CheckNamedValue(nv); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		typ, v, err := bindValue(nv.Value)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if typ.Code != wireArray || typ.Elem == nil || typ.Elem.Code != tt.code {
			t.Errorf("%s: the type is %+v, want ARRAY of %s", name, typ, tt.code)
		}
		if !equalValue(v, tt.value) && (v != nil || tt.value != nil) {
			t.Errorf("%s: the value is %#v, want %#v", name, v, tt.value)
		}
	}
	for name, in := range map[string]any{
		"mixed types":  []any{int64(1), "x"},
		"only NULLs":   []any{nil, nil},
		"empty any":    []any{},
		"nested lists": [][]int64{{1}},
		"structs":      []struct{}{{}},
	} {
		nv := &driver.NamedValue{Value: in}
		err := c.CheckNamedValue(nv)
		if err == nil {
			_, _, err = bindValue(nv.Value)
		}
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestParametersInTheBody holds D191 item 7 on the wire: the body has params and
// paramTypes by name, an INT64 is a string, and a NULL has no type.
func TestParametersInTheBody(t *testing.T) {
	t.Parallel()
	f := newFake(t, txHandler)
	db := f.db()
	var a int64
	err := db.QueryRowContext(t.Context(), "SELECT a FROM t WHERE a = ? AND s = ? AND n IS NOT DISTINCT FROM ?", int64(7), "x", nil).Scan(&a)
	if err != nil {
		t.Fatal(err)
	}
	body := f.last("executeStreamingSql")
	if body["sql"] != "SELECT a FROM t WHERE a = @p1 AND s = @p2 AND n IS NOT DISTINCT FROM @p3" {
		t.Errorf("the statement is %v", body["sql"])
	}
	wantParams := map[string]any{"p1": "7", "p2": "x", "p3": nil}
	wantTypes := map[string]any{"p1": map[string]any{"code": "INT64"}, "p2": map[string]any{"code": "STRING"}}
	if !reflect.DeepEqual(body["params"], wantParams) || !reflect.DeepEqual(body["paramTypes"], wantTypes) {
		t.Errorf("the body has params %v and paramTypes %v, want %v and %v", body["params"], body["paramTypes"], wantParams, wantTypes)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT a FROM t WHERE a IN UNNEST(@ids)", sql.Named("ids", []int64{1, 3})).Scan(&a); err != nil {
		t.Fatal(err)
	}
	body = f.last("executeStreamingSql")
	wantTypes = map[string]any{"ids": map[string]any{"code": "ARRAY", "arrayElementType": map[string]any{"code": "INT64"}}}
	if !reflect.DeepEqual(body["params"], map[string]any{"ids": []any{"1", "3"}}) || !reflect.DeepEqual(body["paramTypes"], wantTypes) {
		t.Errorf("the body has params %v and paramTypes %v", body["params"], body["paramTypes"])
	}
	if _, err := db.ExecContext(t.Context(), "SELECT ?, @a", 1, sql.Named("a", 2)); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a statement that mixes the forms gave %v, want dbimp.ErrArguments", err)
	}
	n := f.count("executeStreamingSql")
	if _, err := db.ExecContext(t.Context(), "SELECT ?, ?", 1); !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("one argument for two placeholders gave %v, want dbimp.ErrArguments", err)
	}
	if f.count("executeStreamingSql") != n {
		t.Error("the driver sent a statement whose arguments do not match")
	}
}

// TestReplayArrayParameters holds the recorded request "an array of each
// type": the driver writes the body that the recorder sent, and the recorded
// answer decodes into the arrays.
func TestReplayArrayParameters(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	stamp := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	_, got := readRows(t, db, "SELECT @i AS i, @s AS s, @f AS f, @b AS b, @d AS d, @ts AS ts, @n AS n, @y AS y, @j AS j",
		sql.Named("i", []any{int64(1), nil}), sql.Named("s", []any{"a", nil}), sql.Named("f", []float64{1.5, math.NaN()}),
		sql.Named("b", []any{true, nil}), sql.Named("d", []dbimp.Date{{Year: 2024, Month: 1, Day: 2}}), sql.Named("ts", []time.Time{stamp}),
		sql.Named("n", []*apd.Decimal{dec(t, "1.5")}), sql.Named("y", [][]byte{[]byte("abc")}), sql.Named("j", []jsontext.Value{jsontext.Value(`{"a":1}`)}))
	if len(got) != 1 {
		t.Fatalf("read %d rows, want 1", len(got))
	}
	want := []any{
		[]any{int64(1), nil}, []any{"a", nil}, []any{1.5, math.NaN()}, []any{true, nil}, []any{dbimp.Date{Year: 2024, Month: 1, Day: 2}},
		[]any{stamp}, []any{dec(t, "1.5")}, []any{[]byte("abc")}, []any{map[string]any{"a": int64(1)}},
	}
	for i := range want {
		if !equalValue(got[0][i], want[i]) {
			t.Errorf("column %d is %#v, want %#v", i, got[0][i], want[i])
		}
	}
}

// TestReplayNamedParameters holds the recorded requests with one named parameter,
// on the stream and not: the driver writes the body that the recorder sent.
func TestReplayNamedParameters(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	_, got := readRows(t, db, "SELECT @p AS p", sql.Named("p", int64(7)))
	if len(got) != 1 || got[0][0] != int64(7) {
		t.Errorf("the stream gave %v, want 7", got)
	}
	_, got = readRows(t, db, "SELECT id, s FROM dbimp_t_types WHERE id = @id", sql.Named("id", int64(1)))
	if len(got) != 1 || got[0][0] != int64(1) || got[0][1] != "x" {
		t.Errorf("the statement gave %v, want 1 and x", got)
	}
	_, got = readRows(t, db, "SELECT id FROM dbimp_t_types WHERE id IN UNNEST(@ids) ORDER BY id", sql.Named("ids", []int64{1, 3}))
	if len(got) != 2 || got[0][0] != int64(1) || got[1][0] != int64(3) {
		t.Errorf("the array parameter gave %v, want 1 and 3", got)
	}
	_, got = readRows(t, db, "SELECT @a AS a, @b AS b, @c AS c", sql.Named("a", math.NaN()), sql.Named("b", math.Inf(1)), sql.Named("c", math.Inf(-1)))
	if len(got) != 1 || !equalValue(got[0][0], math.NaN()) || !equalValue(got[0][1], math.Inf(1)) || !equalValue(got[0][2], math.Inf(-1)) {
		t.Errorf("the floats that are not finite gave %v", got)
	}
	_, got = readRows(t, db, "SELECT @ts AS ts", sql.Named("ts", time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)))
	if len(got) != 1 || !equalValue(got[0][0], time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)) {
		t.Errorf("the timestamp gave %v", got)
	}
	_, got = readRows(t, db, "SELECT @u AS u", sql.Named("u", uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479")))
	if len(got) != 1 || got[0][0] != uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479") {
		t.Errorf("the UUID gave %v", got)
	}
	_, got = readRows(t, db, "SELECT @iv AS iv", sql.Named("iv", dbimp.Interval{Months: 14, Days: 3, Nanoseconds: (4*3600 + 5*60 + 6) * 1e9}))
	if len(got) != 1 || got[0][0] != (dbimp.Interval{Months: 14, Days: 3, Nanoseconds: (4*3600 + 5*60 + 6) * 1e9}) {
		t.Errorf("the interval gave %v", got)
	}
	// The server refuses a parameter that the statement lacks.
	err := failure(t, db, "SELECT @p AS p")
	if serr, ok := errors.AsType[*Error](err); !ok || serr.HTTPStatus != http.StatusBadRequest {
		t.Errorf("a statement with no argument gave %v, want the error of the server", err)
	}
}
