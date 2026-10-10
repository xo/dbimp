package athena //nolint:testpackage // The tests write literals with the writer of the driver.

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// TestReplayParameters binds arguments with ExecutionParameters, with the
// requests that the recording holds: the text of the statement keeps its ?, and
// each argument is the text of an SQL literal (recorded: "a parameter of the type
// integer", "a parameter that is a string literal", "a null parameter", "a
// timestamp parameter", D192 item 4). The fake server matches the whole body, so
// a literal that differs fails the test.
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		name  string
		query string
		args  []any
		want  []string
	}{
		{"an integer", "SELECT ?", []any{int64(42)}, []string{"int64(42)"}},
		{"a string", "SELECT ?", []any{"x"}, []string{`string("x")`}},
		{"two parameters", "SELECT ?, ?", []any{1, "y"}, []string{"int64(1)", `string("y")`}},
		{"a parameter in a comparison", "SELECT id FROM dbimp_it_ext WHERE id = ?", []any{2}, []string{"int64(2)"}},
		{"a NULL", "SELECT ?", []any{nil}, []string{"nil"}},
		{"a timestamp", "SELECT ?", []any{dbimp.LocalDateTime{Date: dbimp.Date{Year: 2026, Month: time.October, Day: 10}, Time: dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3}}}, []string{"dbimp.LocalDateTime(2026-10-10T01:02:03)"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, rows, err := read(t, db, tt.query, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 0 || len(rows[0]) != len(tt.want) {
				t.Fatalf("rows %q, want a row of %q", rows, tt.want)
			}
			for i, w := range tt.want {
				if rows[0][i] != w {
					t.Errorf("value %d is %s, want %s", i, rows[0][i], w)
				}
			}
		})
	}
}

// TestReplayParametersCountIsTheServers holds that the driver does not count the
// placeholders. The server fails a query with too few or too many parameters
// (recorded: "too few parameters", "too many parameters"), and the driver sends
// the arguments as they are.
func TestReplayParametersCountIsTheServers(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, err := db.ExecContext(t.Context(), "SELECT ?, ?", 1)
	var aerr *Error
	if !errors.As(err, &aerr) || aerr.ErrorType != 1100 || aerr.State != stateFailed {
		t.Errorf("too few parameters gave %v, want the failure of the query with the type 1100", err)
	}
	_, err = db.ExecContext(t.Context(), "SELECT ?", 1, 2)
	if !errors.As(err, &aerr) || aerr.ErrorType != 1100 {
		t.Errorf("too many parameters gave %v, want the failure of the query with the type 1100", err)
	}
}

// TestLiterals holds the text of the literal of each Go type that the driver
// writes. The server reads each one as an expression (recorded: "a parameter of
// the type integer", "a timestamp parameter"), and Trino has the form of each
// literal that is not measured here.
func TestLiterals(t *testing.T) {
	t.Parallel()
	dec, _, err := apd.NewFromString("-12345678.90")
	if err != nil {
		t.Fatal(err)
	}
	date := dbimp.Date{Year: 2026, Month: time.October, Day: 10}
	// The sum is made at run time, because a constant sum is exact.
	sum := 0.1
	sum += 0.2
	for _, tt := range []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "NULL"},
		{"true", true, "TRUE"},
		{"false", false, "FALSE"},
		{"an integer", int64(-7), "-7"},
		{"the largest integer", int64(math.MaxInt64), "9223372036854775807"},
		{"the smallest integer", int64(math.MinInt64), "BIGINT '-9223372036854775808'"},
		{"a float", 1.5, "DOUBLE '1.5'"},
		{"a float with every digit", sum, "DOUBLE '0.30000000000000004'"},
		{"NaN", math.NaN(), "nan()"},
		{"an infinity", math.Inf(1), "infinity()"},
		{"a negative infinity", math.Inf(-1), "-infinity()"},
		{"a string", "it's", "'it''s'"},
		{"a string with a backslash", `a\b`, `'a\b'`},
		{"a string outside ASCII", "héllo", "'héllo'"},
		{"an empty string", "", "''"},
		{"bytes", []byte{0xde, 0xad}, "X'dead'"},
		{"no bytes", []byte{}, "X''"},
		{"a decimal", dec, "DECIMAL '-12345678.90'"},
		{"a nil decimal", (*apd.Decimal)(nil), "NULL"},
		{"a date", date, "DATE '2026-10-10'"},
		{"the first date", dbimp.Date{Year: 1, Month: time.January, Day: 1}, "DATE '0001-01-01'"},
		{"a time", dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123e6}, "TIME '12:34:56.123'"},
		{"a time with no fraction", dbimp.LocalTime{Hour: 1, Minute: 2, Second: 3}, "TIME '01:02:03'"},
		{"a time with an offset", dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56}, Offset: 7 * 3600}, "TIME '12:34:56 +07:00'"},
		{"a date and a time", dbimp.LocalDateTime{Date: date, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 5e8}}, "TIMESTAMP '2026-10-10 12:34:56.5'"},
		{"an instant", time.Date(2026, 10, 10, 12, 34, 56, 123e6, time.FixedZone("", 7*3600)), "TIMESTAMP '2026-10-10 12:34:56.123 +07:00'"},
		{"an instant in UTC", time.Date(2026, 10, 10, 12, 34, 56, 0, time.UTC), "TIMESTAMP '2026-10-10 12:34:56 +00:00'"},
		{"an instant west of UTC", time.Date(2026, 10, 10, 12, 34, 56, 0, time.FixedZone("", -(3*3600+30*60))), "TIMESTAMP '2026-10-10 12:34:56 -03:30'"},
		{"a day interval", dbimp.Interval{Days: 1}, "INTERVAL '1 00:00:00.000' DAY TO SECOND"},
		{"a negative interval", dbimp.Interval{Days: -1, Nanoseconds: -int64(time.Hour)}, "INTERVAL - '1 01:00:00.000' DAY TO SECOND"},
		{"a month interval", dbimp.Interval{Months: 14}, "INTERVAL '1-2' YEAR TO MONTH"},
		{"a negative month interval", dbimp.Interval{Months: -14}, "INTERVAL - '1-2' YEAR TO MONTH"},
		{"an interval of zero", dbimp.Interval{}, "INTERVAL '0 00:00:00.000' DAY TO SECOND"},
		{"a UUID", uuid.MustParse("12345678-1234-5678-1234-567812345678"), "UUID '12345678-1234-5678-1234-567812345678'"},
		{"an address", netip.MustParseAddr("2001:db8::1"), "IPADDRESS '2001:db8::1'"},
		{"a list", []any{int64(1), "a", nil}, "ARRAY[1, 'a', NULL]"},
		{"an empty list", []any{}, "ARRAY[]"},
		{"a list of strings", []string{"a", "b"}, "ARRAY['a', 'b']"},
		{"a map", map[string]any{"b": int64(2), "a": int64(1)}, "MAP(ARRAY['a', 'b'], ARRAY[1, 2])"},
		{"an empty map", map[string]any{}, "MAP()"},
	} {
		got, err := literal(tt.v)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: the literal is %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestLiteralsRefuse holds that a value that has no literal is an error that
// names the kind of the failure, and is never written as a wrong literal (D8).
func TestLiteralsRefuse(t *testing.T) {
	t.Parallel()
	inf := new(apd.Decimal)
	inf.Form = apd.Infinite
	for _, tt := range []struct {
		name string
		v    any
		want error
	}{
		{"an infinite decimal", inf, dbimp.ErrInvalidValue},
		{"a date that is not valid", dbimp.Date{Year: 2026, Month: 2, Day: 30}, dbimp.ErrInvalidValue},
		{"a time that is not valid", dbimp.LocalTime{Hour: 25}, dbimp.ErrInvalidValue},
		{"an offset time that is not valid", dbimp.OffsetTime{Time: dbimp.LocalTime{Hour: 25}}, dbimp.ErrInvalidValue},
		{"a timestamp that is not valid", dbimp.LocalDateTime{}, dbimp.ErrInvalidValue},
		{"a year outside the range", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), dbimp.ErrInvalidValue},
		{"an offset with seconds", time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("", 30)), dbimp.ErrInvalidValue},
		{"an interval of both kinds", dbimp.Interval{Months: 1, Days: 1}, dbimp.ErrInvalidValue},
		{"an interval finer than a millisecond", dbimp.Interval{Nanoseconds: 1}, dbimp.ErrInvalidValue},
		{"an address that is not valid", netip.Addr{}, dbimp.ErrInvalidValue},
		{"a struct", struct{}{}, dbimp.ErrNotSupported},
		{"a list of structs", []any{struct{}{}}, dbimp.ErrNotSupported},
		{"a map of structs", map[string]any{"a": struct{}{}}, dbimp.ErrNotSupported},
	} {
		if got, err := literal(tt.v); !errors.Is(err, tt.want) {
			t.Errorf("%s: the literal is %q and the error is %v, want %v", tt.name, got, err, tt.want)
		}
	}
}

// TestNamedArgumentsAreRefused holds that Athena has no named parameter, so an
// argument with a name fails with dbimp.ErrArguments, and nothing is sent.
func TestNamedArgumentsAreRefused(t *testing.T) {
	t.Parallel()
	f := newFake(t, func(_ http.ResponseWriter, _ request, _ int) { t.Error("the driver sent a request") })
	db := open(t, testConfig(), f.srv.URL)
	_, err := db.ExecContext(t.Context(), "SELECT :a", sql.Named("a", 1))
	if !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a named argument gave %v, want dbimp.ErrArguments", err)
	}
	_, err = db.ExecContext(t.Context(), "SELECT ?", struct{ A int }{1})
	if err == nil {
		t.Error("an argument of a struct gave no error")
	}
	_, err = db.ExecContext(t.Context(), "SELECT ?", dbimp.Date{Year: 2026, Month: 13, Day: 1})
	if !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("a date that is not valid gave %v, want dbimp.ErrInvalidValue", err)
	}
}

// TestArgumentsKeepTheirTypes holds that CheckNamedValue keeps the types that the
// driver writes with a literal of their own, and converts the others the way
// database/sql does.
func TestArgumentsKeepTheirTypes(t *testing.T) {
	t.Parallel()
	c := &conn{}
	for _, tt := range []struct {
		v    any
		keep bool
	}{
		{dbimp.Date{}, true},
		{dbimp.LocalTime{}, true},
		{dbimp.OffsetTime{}, true},
		{dbimp.LocalDateTime{}, true},
		{dbimp.Interval{}, true},
		{uuid.UUID{}, true},
		{netip.Addr{}, true},
		{[]any{}, true},
		{map[string]any{}, true},
		{new(apd.Decimal), true},
		{WithTimeout(0), true},
		{int32(1), false},
		{"s", false},
		{time.Time{}, false},
	} {
		nv := namedValue(tt.v)
		err := c.CheckNamedValue(&nv)
		if tt.keep != (err == nil) {
			t.Errorf("CheckNamedValue(%T) = %v, want keep %v", tt.v, err, tt.keep)
		}
	}
	// A decimal that is not a pointer becomes one, and a nil pointer of a Valuer
	// becomes nil.
	nv := namedValue(apd.Decimal{})
	if err := c.CheckNamedValue(&nv); err != nil {
		t.Fatal(err)
	}
	if _, ok := nv.Value.(*apd.Decimal); !ok {
		t.Errorf("a decimal became a %T, want *apd.Decimal", nv.Value)
	}
	nv = namedValue((*dbimp.Interval)(nil))
	if err := c.CheckNamedValue(&nv); err != nil || nv.Value != nil {
		t.Errorf("a nil pointer of a Valuer gave %v and %v, want nil and no error", nv.Value, err)
	}
}

// namedValue returns the argument v as the first argument of a statement.
func namedValue(v any) driver.NamedValue {
	return driver.NamedValue{Ordinal: 1, Value: v}
}

// TestLongArgumentGoesIntoTheText holds that the server refuses a parameter of
// more than 1024 characters, so a statement with such an argument has the text of
// every argument in its own text, with the ? replaced and no ExecutionParameters.
func TestLongArgumentGoesIntoTheText(t *testing.T) {
	t.Parallel()
	f := newFake(t, selectOne(t))
	db := open(t, testConfig(), f.srv.URL)
	long := strings.Repeat("x", 2000)
	if _, _, err := read(t, db, "SELECT '?', ?, ? -- ?", long, 7); err != nil {
		t.Fatal(err)
	}
	var body struct {
		QueryString string
		Params      []string `json:"ExecutionParameters"`
	}
	if err := json.Unmarshal([]byte(f.requests()[0].body), &body); err != nil {
		t.Fatal(err)
	}
	if want := "SELECT '?', '" + long + "', 7 -- ?"; body.QueryString != want {
		t.Errorf("the text is %.60q, want the arguments in it", body.QueryString)
	}
	if body.Params != nil {
		t.Errorf("ExecutionParameters is %v, want none", body.Params)
	}
	// A short argument stays a parameter.
	f2 := newFake(t, selectOne(t))
	db2 := open(t, testConfig(), f2.srv.URL)
	if _, _, err := read(t, db2, "SELECT ?", "short"); err != nil {
		t.Fatal(err)
	}
	if got := f2.requests()[0].body; !strings.Contains(got, `"ExecutionParameters":["'short'"]`) {
		t.Errorf("the request is %s, want the parameter", got)
	}
}
