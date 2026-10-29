package tablestore_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/tablestore"
)

// quoted writes s as a string literal of Tablestore SQL.
func quoted(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s) + "'"
}

// typeCase is the round trip of one type.
type typeCase struct {
	// name is the name of the type in the table of types, and of the subtest.
	name string
	// column is the type of the column v of the mapping.
	column string
	// lit writes a value as the text of a literal, for the insert with a literal.
	lit func(v any) string
	// values are the values to store.
	values []dbimptest.Value
	// equal compares a value that was read with the value wanted, or is nil for
	// reflect.DeepEqual.
	equal func(got, want any) bool
}

// val makes a value whose read is the value itself.
func val(name string, in any) dbimptest.Value {
	return dbimptest.Value{Name: name, In: in}
}

// valTo makes a value whose read is want.
func valTo(name string, in, want any) dbimptest.Value {
	return dbimptest.Value{Name: name, In: in, Want: want}
}

// zone is the zone of the server, which reads a time as text in Asia/Shanghai.
var zone = mustZone("Asia/Shanghai")

// mustZone returns the zone of the name.
func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// typeCases are the types that the survey marks yes. A table has a key id of the
// type VARCHAR, because RoundTrip names a row with a string, and the column v.
func typeCases() []typeCase {
	plain := func(v any) string { return fmt.Sprint(v) }
	boolean := func(v any) string { return strings.ToUpper(fmt.Sprint(v)) }
	text := func(v any) string { s, _ := v.(string); return quoted(s) }
	date := func(v any) string { d, _ := v.(dbimp.Date); return quoted(d.String()) }
	stamp := func(v any) string { ts, _ := v.(time.Time); return quoted(ts.In(zone).Format("2006-01-02 15:04:05")) }
	local := func(v any) string {
		dt, _ := v.(dbimp.LocalDateTime)
		return quoted(strings.Replace(dt.String(), "T", " ", 1))
	}
	hex := func(v any) string { b, _ := v.([]byte); return fmt.Sprintf("X'%x'", b) }
	instant := func(got, want any) bool {
		g, ok1 := got.(time.Time)
		w, ok2 := want.(time.Time)
		return ok1 && ok2 && g.Equal(w)
	}
	at := func(y int, m time.Month, d, h, mi, s int) time.Time { return time.Date(y, m, d, h, mi, s, 0, time.UTC) }
	wall := func(y int, m time.Month, d, h, mi, s int) dbimp.LocalDateTime {
		return dbimp.LocalDateTimeOf(time.Date(y, m, d, h, mi, s, 0, time.UTC))
	}
	boolValues := []dbimptest.Value{val("true", true), val("false", false)}
	return []typeCase{
		{name: "BIGINT", column: "BIGINT", lit: plain, values: []dbimptest.Value{
			val("max", int64(math.MaxInt64)), val("min", int64(math.MinInt64)), val("zero", int64(0)), val("one", int64(1)),
		}},
		{name: "DOUBLE", column: "DOUBLE", lit: plain, values: []dbimptest.Value{
			val("pi", 3.141592653589793), val("negative", -0.5), val("large", 1.7976931348623157e308), val("small", 5e-324), val("zero", 0.0),
		}},
		{name: "BOOL", column: "BOOL", lit: boolean, values: boolValues},
		{name: "BOOLEAN", column: "BOOLEAN", lit: boolean, values: boolValues},
		{name: "MEDIUMTEXT", column: "MEDIUMTEXT", lit: text, values: []dbimptest.Value{
			val("plain", "hello"), val("empty", ""), val("quotes", `it's a "test" \ with ? and -- and ;`),
			val("unicode", "héllo ✓ 😀 日本語"), val("newline", "a\nb\tc"), val("long", strings.Repeat("0123456789", 20000)),
		}},
		{name: "MEDIUMBLOB", column: "MEDIUMBLOB", lit: hex, values: []dbimptest.Value{
			val("bytes", []byte{0, 1, 2, 255, 254}), val("empty", []byte{}), val("text", []byte("it's")), val("long", make([]byte, 100000)),
		}},
		{name: "DATE", column: "DATE", lit: date, values: []dbimptest.Value{
			val("today", dbimp.Date{Year: 2026, Month: 10, Day: 29}), val("epoch", dbimp.Date{Year: 1970, Month: 1, Day: 1}),
			val("last", dbimp.Date{Year: 9999, Month: 12, Day: 31}), val("first", dbimp.Date{Year: 1000, Month: 1, Day: 2}),
		}},
		{name: "DATETIME", column: "DATETIME", lit: local, values: []dbimptest.Value{
			val("now", wall(2026, 10, 29, 1, 2, 3)), val("midnight", wall(2000, 1, 1, 0, 0, 0)), val("late", wall(2038, 1, 19, 3, 14, 7)),
			val("day end", wall(2026, 12, 31, 23, 59, 59)),
		}},
		{name: "TIMESTAMP", column: "TIMESTAMP", lit: stamp, equal: instant, values: []dbimptest.Value{
			val("now", at(2026, 10, 29, 1, 2, 3)), val("y2k", at(2000, 1, 1, 0, 0, 0)), val("late", at(2038, 1, 19, 3, 14, 7)),
			// A time in another zone is the same instant.
			valTo("other zone", time.Date(2026, 10, 29, 9, 2, 3, 0, zone), at(2026, 10, 29, 1, 2, 3)),
		}},
	}
}

// TestIntegrationRoundTrip stores each value of each type that the survey marks
// yes, as an argument and as a literal, reads it back, updates it and deletes it
// (step 14a of docs/DRIVER.md). A fraction of a second is not in the values,
// because the server keeps whole seconds (docs/TABLESTORE.md, "Types"). Each
// subtest is one entry of testdata/tablestore/features.json.
func TestIntegrationRoundTrip(t *testing.T) {
	db := connect(t)
	for _, tc := range typeCases() {
		t.Run(tc.name, func(t *testing.T) {
			tbl := makeTable(t, db, "rt_"+strings.ToLower(tc.name), tablestore.KeyString, "v "+tc.column)
			dbimptest.RoundTrip(t, db, dbimptest.RoundTripCase{
				Type:   tc.name,
				Insert: "INSERT INTO " + tbl + " (id, v) VALUES (?, ?)",
				Literal: func(key string, v any) (string, error) {
					return "INSERT INTO " + tbl + " (id, v) VALUES (" + quoted(key) + ", " + tc.lit(v) + ")", nil
				},
				Select: "SELECT v FROM " + tbl + " WHERE id = ?",
				Update: "UPDATE " + tbl + " SET v = ? WHERE id = ?",
				Delete: "DELETE FROM " + tbl + " WHERE id = ?",
				Values: tc.values,
				Equal:  tc.equal,
			})
		})
	}
}
