package exasol_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// dec parses a decimal for a value of a round trip.
func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// as returns v as a T, or the zero value of T if it is not one.
func as[T any](v any) T {
	t, _ := v.(T)
	return t
}

// quote writes s as a literal of text.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func day(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }

func local(d dbimp.Date, h, m, s, ns int) dbimp.LocalDateTime {
	return dbimp.LocalDateTime{Date: d, Time: dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: ns}}
}

// TestIntegrationRoundTrip stores values of each type that
// testdata/exasol/features.json marks yes, as a bound argument and as a literal,
// reads them back by their key, updates, deletes (step 14a). The ordinary user
// cannot write, so it reads what the administrator wrote and gets the refusal of
// the server for a write.
func TestIntegrationRoundTrip(t *testing.T) {
	forEach(t, func(t *testing.T, e env) {
		s := e.schema
		type tc struct {
			name   string
			column string
			values []dbimptest.Value
			lit    func(v any) string
			equal  func(got, want any) bool
		}
		nullOr := func(v any, f func() string) string {
			if v == nil {
				return "NULL"
			}
			return f()
		}
		sameTime := func(got, want any) bool {
			g, ok1 := got.(time.Time)
			w, ok2 := want.(time.Time)
			return ok1 && ok2 && g.Equal(w) || got == nil && want == nil
		}
		sameDecimal := func(got, want any) bool {
			g, ok1 := got.(*apd.Decimal)
			w, ok2 := want.(*apd.Decimal)
			return ok1 && ok2 && g.Cmp(w) == 0 || got == nil && want == nil
		}
		// An empty string is NULL on the server (D207), and a Value cannot name NULL
		// as its Want, because nil means the value that was written. So a typed nil
		// stands for NULL, and sameText reads it so.
		noText := (*string)(nil)
		sameText := func(got, want any) bool {
			if p, ok := want.(*string); ok && p == nil {
				return got == nil
			}
			return reflect.DeepEqual(got, want)
		}
		longText := strings.Repeat("x", 100000)
		// A DOUBLE of 17 significant digits is not always exact on the server: the
		// value 123456789.12345679 came back as 123456789.1234568 (measured on
		// 2026-10-11), so the values below are the ones that come back whole.
		cases := []tc{
			{"BOOLEAN", "BOOLEAN", []dbimptest.Value{{Name: "true", In: true}, {Name: "false", In: false}, {Name: "null", In: nil}},
				func(v any) string { return nullOr(v, func() string { return strings.ToUpper(fmt.Sprint(v)) }) }, nil},
			{"DECIMAL INTEGER", "DECIMAL(18,0)", []dbimptest.Value{
				{Name: "zero", In: int64(0)}, {Name: "max", In: int64(999999999999999999)}, {Name: "min", In: int64(-999999999999999999)},
				{Name: "beyond 2^53", In: int64(9007199254740993)}, {Name: "null", In: nil},
			}, func(v any) string { return nullOr(v, func() string { return fmt.Sprint(v) }) }, nil},
			{"DECIMAL", "DECIMAL(36,10)", []dbimptest.Value{
				{Name: "zero", In: dec(t, "0")}, {Name: "max", In: dec(t, "99999999999999999999999999.9999999999")},
				{Name: "min", In: dec(t, "-99999999999999999999999999.9999999999")}, {Name: "smallest step", In: dec(t, "0.0000000001")},
				{Name: "long", In: dec(t, "1234567890123456789012345.6789012345")}, {Name: "null", In: nil},
			}, func(v any) string {
				return nullOr(v, func() string {
					d, _ := v.(*apd.Decimal)
					return d.Text('f')
				})
			}, sameDecimal},
			{"DOUBLE", "DOUBLE", []dbimptest.Value{
				{Name: "zero", In: 0.0}, {Name: "large", In: 1.5e300}, {Name: "smallest", In: -1.7e308}, {Name: "denormal", In: 5e-324},
				{Name: "last digit", In: 0.30000000000000004}, {Name: "pi", In: 3.141592653589793}, {Name: "null", In: nil},
			}, func(v any) string {
				return nullOr(v, func() string { return strings.Replace(fmt.Sprintf("CAST('%v' AS DOUBLE)", v), "+", "", 1) })
			}, nil},
			{"CHAR", "CHAR(10)", []dbimptest.Value{
				{Name: "short", In: "ab", Want: "ab        "}, {Name: "unicode", In: "héllo ☃", Want: "héllo ☃   "},
				{Name: "empty", In: "", Want: noText}, {Name: "full", In: "0123456789"}, {Name: "null", In: nil},
			}, func(v any) string { return nullOr(v, func() string { return quote(as[string](v)) }) }, sameText},
			{"VARCHAR", "VARCHAR(2000000)", []dbimptest.Value{
				{Name: "plain", In: "hello"}, {Name: "unicode", In: "héllo ☃ \"quoted\" 'single' \\ back\nline\ttab"},
				{Name: "long", In: longText}, {Name: "empty", In: "", Want: noText}, {Name: "null", In: nil},
			}, func(v any) string { return nullOr(v, func() string { return quote(as[string](v)) }) }, sameText},
			{"DATE", "DATE", []dbimptest.Value{
				{Name: "min", In: day(1, 1, 1)}, {Name: "max", In: day(9999, 12, 31)}, {Name: "leap day", In: day(2024, 2, 29)}, {Name: "null", In: nil},
			}, func(v any) string {
				return nullOr(v, func() string { return "DATE " + quote(as[dbimp.Date](v).String()) })
			}, nil},
			{"TIMESTAMP", "TIMESTAMP(6)", []dbimptest.Value{
				{Name: "min", In: local(day(1, 1, 1), 0, 0, 0, 0)}, {Name: "max", In: local(day(9999, 12, 31), 23, 59, 59, 999999000)},
				{Name: "microsecond", In: local(day(2026, 10, 11), 12, 34, 56, 123456000)}, {Name: "null", In: nil},
			}, func(v any) string {
				return nullOr(v, func() string {
					return "TIMESTAMP " + quote(strings.Replace(as[dbimp.LocalDateTime](v).String(), "T", " ", 1))
				})
			}, nil},
			{"TIMESTAMP WITH LOCAL TIME ZONE", "TIMESTAMP(6) WITH LOCAL TIME ZONE", []dbimptest.Value{
				{Name: "smallest", In: time.Date(1, 1, 3, 0, 0, 0, 0, time.UTC)}, {Name: "largest", In: time.Date(9999, 12, 30, 23, 59, 59, 999999000, time.UTC)},
				{Name: "microsecond", In: time.Date(2026, 10, 11, 12, 34, 56, 123456000, time.UTC)},
				{Name: "offset", In: time.Date(2026, 10, 11, 12, 34, 56, 0, time.FixedZone("", 5*3600+1800))}, {Name: "null", In: nil},
			}, func(v any) string {
				return nullOr(v, func() string {
					return "CONVERT_TZ(TIMESTAMP " + quote(as[time.Time](v).UTC().Format("2006-01-02 15:04:05.000000")) + ", 'UTC', SESSIONTIMEZONE)"
				})
			}, sameTime},
			{"INTERVAL YEAR TO MONTH", "INTERVAL YEAR(2) TO MONTH", []dbimptest.Value{
				{Name: "zero", In: dbimp.Interval{}}, {Name: "positive", In: dbimp.Interval{Months: 63}}, {Name: "negative", In: dbimp.Interval{Months: -63}},
				{Name: "max", In: dbimp.Interval{Months: 99*12 + 11}}, {Name: "null", In: nil},
			}, func(v any) string {
				return nullOr(v, func() string {
					m := as[dbimp.Interval](v).Months
					sign := ""
					if m < 0 {
						sign, m = "-", -m
					}
					return fmt.Sprintf("INTERVAL '%s%d-%d' YEAR(2) TO MONTH", sign, m/12, m%12)
				})
			}, nil},
			{"INTERVAL DAY TO SECOND", "INTERVAL DAY(9) TO SECOND(3)", []dbimptest.Value{
				{Name: "zero", In: dbimp.Interval{}},
				{Name: "positive", In: dbimp.Interval{Days: 2, Nanoseconds: 3*3600e9 + 4*60e9 + 5e9 + 678e6}},
				{Name: "negative", In: dbimp.Interval{Days: -2, Nanoseconds: -(3*3600e9 + 4*60e9 + 5e9 + 678e6)}},
				{Name: "max", In: dbimp.Interval{Days: 999999999, Nanoseconds: 23*3600e9 + 59*60e9 + 59e9 + 999e6}},
				{Name: "millisecond", In: dbimp.Interval{Nanoseconds: 1e6}}, {Name: "null", In: nil},
			}, func(v any) string {
				return nullOr(v, func() string {
					iv := as[dbimp.Interval](v)
					return "INTERVAL '" + dsText(iv) + "' DAY(9) TO SECOND(3)"
				})
			}, nil},
			{"GEOMETRY", "GEOMETRY", []dbimptest.Value{
				{Name: "point", In: "POINT (1 2)"}, {Name: "polygon", In: "POLYGON ((0 0, 1 0, 1 1, 0 0))"},
				{Name: "line", In: "LINESTRING (0 0, 1 1)"}, {Name: "empty", In: "POINT EMPTY"}, {Name: "null", In: nil},
			}, func(v any) string { return nullOr(v, func() string { return quote(as[string](v)) }) }, nil},
			{"HASHTYPE", "HASHTYPE(16 BYTE)", []dbimptest.Value{
				{Name: "zero", In: make([]byte, 16)}, {Name: "ones", In: []byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}},
				{Name: "mixed", In: []byte{0x55, 0x0e, 0x84, 0x00, 0xe2, 0x9b, 0x41, 0xd4, 0xa7, 0x16, 0x44, 0x66, 0x55, 0x44, 0x00, 0x00}}, {Name: "null", In: nil},
			}, func(v any) string { return nullOr(v, func() string { return quote(fmt.Sprintf("%x", v)) }) }, nil},
		}
		for _, c := range cases {
			t.Run(strings.ReplaceAll(c.name, " ", "_"), func(t *testing.T) {
				table := fmt.Sprintf("%s.RT_%s", s, strings.ToUpper(suffix()))
				if e.p == ordinary {
					// The ordinary user cannot write, so it reads what the
					// administrator wrote, and the server refuses its write.
					e.ddl(t, "CREATE TABLE "+table+" (K VARCHAR(100), V "+c.column+")")
					forbidden(t, e.db, "INSERT INTO "+table+" VALUES ('k', NULL)")
					mustExec(t, e.adm, "INSERT INTO "+table+" VALUES ('k', NULL)")
					want(t, "the count that the ordinary user reads", read[int64](t, e.db, "SELECT COUNT(*) FROM "+table), []int64{1})
					return
				}
				dbimptest.RoundTrip(t, e.db, dbimptest.RoundTripCase{
					Type:     c.name,
					Setup:    []string{"CREATE TABLE " + table + " (K VARCHAR(100), V " + c.column + ")"},
					Teardown: []string{"DROP TABLE IF EXISTS " + table},
					Insert:   "INSERT INTO " + table + " VALUES (?, ?)",
					Literal: func(key string, v any) (string, error) {
						return "INSERT INTO " + table + " VALUES (" + quote(key) + ", " + c.lit(v) + ")", nil
					},
					Select: "SELECT V FROM " + table + " WHERE K = ?",
					Update: "UPDATE " + table + " SET V = ? WHERE K = ?",
					Delete: "DELETE FROM " + table + " WHERE K = ?",
					Values: c.values,
					Equal:  c.equal,
				})
			})
		}
	})
}

// dsText writes the days and the time of an interval as the literal of the server.
func dsText(iv dbimp.Interval) string {
	days, nanos := int64(iv.Days), iv.Nanoseconds
	sign := ""
	if days < 0 || nanos < 0 {
		sign, days, nanos = "-", -days, -nanos
	}
	secs := nanos / 1e9
	return fmt.Sprintf("%s%d %02d:%02d:%02d.%03d", sign, days, secs/3600, secs%3600/60, secs%60, nanos%1e9/1e6)
}
