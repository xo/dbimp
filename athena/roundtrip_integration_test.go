package athena_test

import (
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// A table of Athena has the key k and the value v. Only an Iceberg table takes
// UPDATE and DELETE, and it holds few types, so each type has one of two homes:
//
//   - A column of its own type, in an Iceberg table when Iceberg has the type,
//     and in a Hive table of Parquet files when it does not. A Hive table takes
//     INSERT and SELECT only, so its round trip skips the update and the
//     delete, and leaves the row (the table is dropped at the end).
//   - A column of text, for a type that no table of Athena can hold: time, json,
//     ipaddress, uuid, the intervals, geometry and the types with a time zone.
//     The text is the cast of the value, and the select casts it back to the
//     type, so the column that the driver reads has the type, and the value goes
//     through the server in the form that the type writes. The test says so for
//     each of these types.

// typeCase is the round trip of one type.
type typeCase struct {
	// name is the name of the type in the table of types.
	name string
	// hive is true for a Hive table, and false for an Iceberg table.
	hive bool
	// col is the type of the column v.
	col string
	// ins writes the value in an insert and in an update: %s is the value, which
	// is a ? or a literal.
	ins string
	// sel is the expression that the select reads.
	sel string
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

// quoted writes s as a string literal.
func quoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// plain writes a value with fmt.Sprint.
func plain(v any) string {
	return fmt.Sprint(v)
}

// decimalOf returns the decimal that the text holds.
func decimalOf(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// sameDecimal compares two decimals by value.
func sameDecimal(got, want any) bool {
	g, ok1 := got.(*apd.Decimal)
	w, ok2 := want.(*apd.Decimal)
	return ok1 && ok2 && g.Cmp(w) == 0
}

// sameInstant compares two instants and the offsets of their zones.
func sameInstant(got, want any) bool {
	g, ok1 := got.(time.Time)
	w, ok2 := want.(time.Time)
	if !ok1 || !ok2 {
		return false
	}
	_, go1 := g.Zone()
	_, wo := w.Zone()
	return g.Equal(w) && go1 == wo
}

// floatLit writes a float as a literal of a double with every digit.
func floatLit(v any) string {
	f := as[float64](v)
	return "DOUBLE '" + strconv.FormatFloat(f, 'g', -1, 64) + "'"
}

// typeCases returns the round trip of each type that the survey marks yes, in
// the order of the table of types.
func typeCases(t *testing.T) []typeCase {
	t.Helper()
	sum := 0.1
	sum += 0.2
	date := func(y int, m time.Month, d int) dbimp.Date { return dbimp.Date{Year: y, Month: m, Day: d} }
	clock := func(h, m, s, ns int) dbimp.LocalTime {
		return dbimp.LocalTime{Hour: h, Minute: m, Second: s, Nanosecond: ns}
	}
	local := func(d dbimp.Date, c dbimp.LocalTime) dbimp.LocalDateTime {
		return dbimp.LocalDateTime{Date: d, Time: c}
	}
	zone := func(h, m int) *time.Location {
		sign := 1
		if h < 0 {
			sign = -1
		}
		return time.FixedZone("", h*3600+sign*m*60)
	}
	bigText := strings.Repeat("0123456789", 500)
	long := make([]byte, 256)
	for i := range long {
		long[i] = byte(i)
	}
	return []typeCase{
		{name: "TINYINT", hive: true, col: "tinyint", ins: "CAST(%s AS tinyint)", sel: "v", lit: plain,
			values: []dbimptest.Value{val("zero", int64(0)), val("min", int64(-128)), val("max", int64(127))}},
		{name: "SMALLINT", hive: true, col: "smallint", ins: "CAST(%s AS smallint)", sel: "v", lit: plain,
			values: []dbimptest.Value{val("zero", int64(0)), val("min", int64(-32768)), val("max", int64(32767))}},
		{name: "INTEGER", col: "int", ins: "CAST(%s AS integer)", sel: "v", lit: plain,
			values: []dbimptest.Value{val("zero", int64(0)), val("min", int64(math.MinInt32)), val("max", int64(math.MaxInt32))}},
		{name: "BIGINT", col: "bigint", ins: "CAST(%s AS bigint)", sel: "v",
			lit: func(v any) string {
				if as[int64](v) == math.MinInt64 {
					return "BIGINT '-9223372036854775808'"
				}
				return plain(v)
			},
			values: []dbimptest.Value{val("zero", int64(0)), val("min", int64(math.MinInt64)), val("max", int64(math.MaxInt64))}},
		{name: "FLOAT", col: "float", ins: "CAST(%s AS real)", sel: "v",
			lit:    floatLit,
			values: []dbimptest.Value{val("zero", float64(0)), val("negative", -1.5), val("large", float64(16777216))}},
		{name: "DOUBLE", col: "double", ins: "CAST(%s AS double)", sel: "v",
			lit:    floatLit,
			values: []dbimptest.Value{val("zero", float64(0)), val("max", math.MaxFloat64), val("smallest", math.SmallestNonzeroFloat64), val("every digit", sum)}},
		{name: "DECIMAL", col: "decimal(20,2)", ins: "CAST(%s AS decimal(20,2))", sel: "v", equal: sameDecimal,
			lit: func(v any) string { d := as[*apd.Decimal](v); return "DECIMAL '" + d.Text('f') + "'" },
			values: []dbimptest.Value{
				val("zero", decimalOf(t, "0.00")), val("min", decimalOf(t, "-999999999999999999.99")),
				val("max", decimalOf(t, "999999999999999999.99")), val("last digit", decimalOf(t, "0.01")),
			}},
		{name: "VARCHAR", col: "string", ins: "CAST(%s AS varchar)", sel: "v", lit: func(v any) string { return quoted(as[string](v)) },
			values: []dbimptest.Value{val("empty", ""), val("unicode", "it's héllo ✓ 日本語 \\ \"q\""), val("long", bigText)}},
		{name: "CHAR", hive: true, col: "char(10)", ins: "CAST(%s AS char(10))", sel: "v", lit: func(v any) string { return quoted(as[string](v)) },
			values: []dbimptest.Value{valTo("short", "ab", "ab        "), valTo("empty", "", "          "), val("full", "0123456789")}},
		{name: "BOOLEAN", col: "boolean", ins: "CAST(%s AS boolean)", sel: "v", lit: func(v any) string { return strings.ToUpper(plain(v)) },
			values: []dbimptest.Value{val("true", true), val("false", false)}},
		{name: "DATE", col: "date", ins: "CAST(%s AS date)", sel: "v", lit: func(v any) string { return "DATE '" + as[dbimp.Date](v).String() + "'" },
			values: []dbimptest.Value{val("today", date(2026, time.October, 10)), val("min", date(1, time.January, 1)), val("max", date(9999, time.December, 31))}},
		// A time has no column in Iceberg or in Hive, so it is text that is cast back.
		{name: "TIME", col: "string", ins: "CAST(CAST(%s AS time) AS varchar)", sel: "CAST(v AS time)", lit: func(v any) string { return "TIME '" + as[dbimp.LocalTime](v).String() + "'" },
			values: []dbimptest.Value{val("midnight", clock(0, 0, 0, 0)), val("end of the day", clock(23, 59, 59, 999e6)), val("fraction", clock(12, 34, 56, 123e6))}},
		{name: "TIMESTAMP", col: "timestamp", ins: "CAST(%s AS timestamp)", sel: "v",
			lit: func(v any) string {
				dt := as[dbimp.LocalDateTime](v)
				return "TIMESTAMP '" + dt.Date.String() + " " + dt.Time.String() + "'"
			},
			values: []dbimptest.Value{
				val("epoch", local(date(1970, time.January, 1), clock(0, 0, 0, 0))),
				val("fraction", local(date(2026, time.October, 10), clock(12, 34, 56, 123e6))),
				val("max", local(date(9999, time.December, 31), clock(23, 59, 59, 999e6))),
			}},
		// A time zone has no column either, so it is text that is cast back.
		{name: "TIMESTAMP WITH TIME ZONE", col: "string", ins: "CAST(CAST(%s AS timestamp with time zone) AS varchar)", sel: "CAST(v AS timestamp with time zone)", equal: sameInstant,
			lit: func(v any) string {
				tm := as[time.Time](v)
				_, off := tm.Zone()
				sign := "+"
				if off < 0 {
					sign, off = "-", -off
				}
				return fmt.Sprintf("TIMESTAMP '%s %s%02d:%02d'", tm.Format("2006-01-02 15:04:05.000"), sign, off/3600, off%3600/60)
			},
			values: []dbimptest.Value{
				val("east", time.Date(2026, 10, 10, 12, 34, 56, 123e6, zone(7, 0))),
				val("west", time.Date(2026, 10, 10, 12, 34, 56, 0, zone(-3, 30))),
				val("utc", time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)),
				val("max", time.Date(9999, 12, 31, 23, 59, 59, 999e6, zone(14, 0))),
			}},
		{name: "TIME WITH TIME ZONE", col: "string", ins: "CAST(CAST(%s AS time with time zone) AS varchar)", sel: "CAST(v AS time with time zone)",
			lit: func(v any) string {
				ot := as[dbimp.OffsetTime](v)
				sign, off := "+", ot.Offset
				if off < 0 {
					sign, off = "-", -off
				}
				return fmt.Sprintf("TIME '%s %s%02d:%02d'", ot.Time.String(), sign, off/3600, off%3600/60)
			},
			values: []dbimptest.Value{
				val("east", dbimp.OffsetTime{Time: clock(12, 34, 56, 123e6), Offset: 7 * 3600}),
				val("west", dbimp.OffsetTime{Time: clock(0, 0, 1, 0), Offset: -(5*3600 + 30*60)}),
				val("far east", dbimp.OffsetTime{Time: clock(23, 59, 59, 0), Offset: 14 * 3600}),
			}},
		{name: "VARBINARY", col: "binary", ins: "CAST(%s AS varbinary)", sel: "v", lit: func(v any) string { return fmt.Sprintf("X'%x'", as[[]byte](v)) },
			values: []dbimptest.Value{val("empty", []byte{}), val("zero byte", []byte{0}), val("deadbeef", []byte{0xde, 0xad, 0xbe, 0xef}), val("long", long)}},
		// The text of a container has no escape, so the driver returns it as it is.
		{name: "ARRAY", hive: true, col: "array<bigint>", ins: "CAST(%s AS array(bigint))", sel: "v",
			lit: func(v any) string {
				list, _ := v.([]int64)
				parts := make([]string, 0, len(list))
				for _, n := range list {
					parts = append(parts, plain(n))
				}
				return "ARRAY[" + strings.Join(parts, ", ") + "]"
			},
			values: []dbimptest.Value{valTo("three", []int64{1, 2, 3}, "[1, 2, 3]"), valTo("empty", []int64{}, "[]"), valTo("negative", []int64{-1, 0}, "[-1, 0]")}},
		{name: "MAP", hive: true, col: "map<string,bigint>", ins: "CAST(%s AS map(varchar, bigint))", sel: "v",
			lit: func(v any) string {
				m := as[map[string]any](v)
				var ks, vs []string
				for _, k := range []string{"a", "b", "k,1", "x"} {
					if n, ok := m[k]; ok {
						ks, vs = append(ks, quoted(k)), append(vs, plain(n))
					}
				}
				return "MAP(ARRAY[" + strings.Join(ks, ", ") + "], ARRAY[" + strings.Join(vs, ", ") + "])"
			},
			values: []dbimptest.Value{
				valTo("two", map[string]any{"a": int64(1), "b": int64(2)}, "{a=1, b=2}"),
				valTo("a comma", map[string]any{"k,1": int64(-1)}, "{k,1=-1}"),
				valTo("zero", map[string]any{"x": int64(0)}, "{x=0}"),
			}},
		// A row has no Go literal, so the value is the text of the second field.
		{name: "ROW", hive: true, col: "struct<a:int,b:string>", ins: "CAST(ROW(1, %s) AS ROW(a integer, b varchar))", sel: "v", lit: func(v any) string { return quoted(as[string](v)) },
			values: []dbimptest.Value{valTo("short", "x", "{a=1, b=x}"), valTo("unicode", "héllo ✓", "{a=1, b=héllo ✓}"), valTo("empty", "", "{a=1, b=}")}},
		// JSON_PARSE reads the text as a document, and json_format writes it back as
		// text. CAST of a JSON object to varchar fails, and CAST of a JSON string
		// drops its quotes.
		{name: "JSON", col: "string", ins: "json_format(JSON_PARSE(%s))", sel: "JSON_PARSE(v)", lit: func(v any) string { return quoted(as[string](v)) },
			values: []dbimptest.Value{
				valTo("object", `{"a": 1}`, map[string]any{"a": int64(1)}),
				valTo("array", `[1, "x", null]`, []any{int64(1), "x", nil}),
				valTo("text", `"héllo"`, "héllo"),
				valTo("fraction", `1.5`, float64(1.5)),
			}},
		{name: "IPADDRESS", col: "string", ins: "CAST(CAST(%s AS ipaddress) AS varchar)", sel: "CAST(v AS ipaddress)", lit: func(v any) string { return "IPADDRESS '" + as[netip.Addr](v).String() + "'" },
			values: []dbimptest.Value{
				val("zero", netip.MustParseAddr("0.0.0.0")), val("broadcast", netip.MustParseAddr("255.255.255.255")),
				val("version 6", netip.MustParseAddr("2001:db8::1")), val("loopback", netip.MustParseAddr("::1")),
			}},
		{name: "UUID", col: "string", ins: "CAST(CAST(%s AS uuid) AS varchar)", sel: "CAST(v AS uuid)", lit: func(v any) string { return "UUID '" + as[uuid.UUID](v).String() + "'" },
			values: []dbimptest.Value{
				val("nil", uuid.Nil()), val("max", uuid.Max()), val("fixed", uuid.MustParse("12345678-1234-5678-1234-567812345678")),
			}},
		// A varchar cannot be cast to an interval, and a Go zero Interval is written as
		// an interval of days and time, so each interval is stored as a number that
		// the select turns back into the interval: the milliseconds for days and time,
		// and the months for years and months.
		{name: "INTERVAL DAY TO SECOND", col: "bigint", ins: "to_milliseconds(%s)", sel: "INTERVAL '0.001' SECOND * v",
			lit: func(v any) string {
				iv := as[dbimp.Interval](v)
				ms := int64(iv.Days)*86400000 + iv.Nanoseconds/1e6
				sign := ""
				if ms < 0 {
					sign, ms = "- ", -ms
				}
				return fmt.Sprintf("INTERVAL %s'%d %02d:%02d:%02d.%03d' DAY TO SECOND", sign, ms/86400000, ms%86400000/3600000, ms%3600000/60000, ms%60000/1000, ms%1000)
			},
			values: []dbimptest.Value{
				val("zero", dbimp.Interval{}),
				val("day and time", dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute + 4*time.Second + 5*time.Millisecond)}),
				val("negative", dbimp.Interval{Days: -1, Nanoseconds: -int64(time.Hour)}),
			}},
		// A zero Interval has no months, so the driver writes it as days and time, and
		// the value of the zero month is not in the list.
		{name: "INTERVAL YEAR TO MONTH", col: "bigint", ins: "date_diff('month', DATE '2000-01-01', DATE '2000-01-01' + %s)", sel: "INTERVAL '1' MONTH * v",
			lit: func(v any) string {
				m := int64(as[dbimp.Interval](v).Months)
				sign := ""
				if m < 0 {
					sign, m = "- ", -m
				}
				return fmt.Sprintf("INTERVAL %s'%d-%d' YEAR TO MONTH", sign, m/12, m%12)
			},
			values: []dbimptest.Value{val("month", dbimp.Interval{Months: 1}), val("year and month", dbimp.Interval{Months: 14}), val("negative", dbimp.Interval{Months: -14})}},
		// UNKNOWN is the type of NULL, so the select reads a bare NULL, from a row
		// that holds NULL too.
		{name: "UNKNOWN", col: "string", ins: "CAST(%s AS varchar)", sel: "NULL", lit: func(any) string { return "NULL" },
			values: []dbimptest.Value{val("null", nil), val("null again", nil)}},
		{name: "GEOMETRY", col: "string", ins: "%s", sel: "ST_GeometryFromText(v)", lit: func(v any) string { return quoted(as[string](v)) },
			values: []dbimptest.Value{val("point", "POINT (1 2)"), val("negative", "POINT (-1.5 0)"), val("line", "LINESTRING (0 0, 1 1)")}},
		// The server has no wire type STRING for a column: a Hive column of the type
		// string is a varchar in a select, and the type STRING is the type of the
		// columns of DESCRIBE and SHOW (see TestIntegrationFeatures/describe_table).
		{name: "STRING", hive: true, col: "string", ins: "CAST(%s AS varchar)", sel: "v", lit: func(v any) string { return quoted(as[string](v)) },
			values: []dbimptest.Value{val("empty", ""), val("text", "x"), val("unicode", "héllo")}},
	}
}

// as returns v as a T, or the zero value of T when v is another type.
func as[T any](v any) T {
	x, _ := v.(T)
	return x
}

// slots limits the round trips that run at once, so that they stay under the
// quota of queries that run at the same time for an account.
var slots = make(chan struct{}, 6)

// TestIntegrationRoundTrip stores each type in a column and reads it back, with
// dbimptest.RoundTrip, as a bound argument and as a literal, through Rows.Scan
// (step 14a). Each type has its own table.
func TestIntegrationRoundTrip(t *testing.T) {
	t.Parallel()
	db := connect(t)
	for _, tc := range typeCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			name := table("rt_" + strings.Map(func(r rune) rune {
				if r == ' ' {
					return '_'
				}
				return r
			}, strings.ToLower(tc.name)))
			c := dbimptest.RoundTripCase{
				Type:     tc.name,
				Setup:    []string{"DROP TABLE IF EXISTS " + name, createTable(t, tc, name)},
				Teardown: []string{"DROP TABLE IF EXISTS " + name},
				Insert:   "INSERT INTO " + name + " VALUES (?, " + fmt.Sprintf(tc.ins, "?") + ")",
				Literal: func(key string, v any) (string, error) {
					return "INSERT INTO " + name + " VALUES (" + quoted(key) + ", " + fmt.Sprintf(tc.ins, tc.lit(v)) + ")", nil
				},
				Select: "SELECT " + tc.sel + " FROM " + name + " WHERE k = ?",
				Update: "UPDATE " + name + " SET v = " + fmt.Sprintf(tc.ins, "?") + " WHERE k = ?",
				Delete: "DELETE FROM " + name + " WHERE k = ?",
				Values: tc.values,
				Equal:  tc.equal,
			}
			if tc.hive {
				// A Hive table takes INSERT and SELECT only (recorded: "an update of the
				// external table"), so the row stays until the table is dropped.
				c.Update, c.Delete = "", ""
				c.SkipUpdate = func(dbimptest.Value, dbimptest.Value) string { return "a Hive table cannot update a row" }
			}
			dbimptest.RoundTrip(t, db, c)
		})
	}
	// The wire types BINARY and STRUCT do not exist: the server names a Hive column
	// of the type binary varbinary, and one of the type struct row (recorded:
	// "select typed rows"). The entries are no, and each test reads the column and
	// holds the refusal.
	for _, tt := range []struct {
		name, col, value, wire string
	}{
		{"BINARY", "binary", "X'DEADBEEF'", "VARBINARY"},
		{"STRUCT", "struct<a:int,b:string>", "CAST(ROW(1,'x') AS ROW(a integer, b varchar))", "ROW"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			name := table("rt_" + strings.ToLower(tt.name))
			dropLater(t, db, name)
			exec(t, db, "CREATE EXTERNAL TABLE "+name+" (v "+tt.col+") STORED AS PARQUET LOCATION '"+location(t, "rt_"+strings.ToLower(tt.name))+"'")
			exec(t, db, "INSERT INTO "+name+" SELECT "+tt.value)
			rows, err := db.QueryContext(t.Context(), "SELECT v FROM "+name)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			types, err := rows.ColumnTypes()
			if err != nil {
				t.Fatal(err)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if got := types[0].DatabaseTypeName(); got != tt.wire {
				t.Errorf("a Hive column of the type %s has the wire type %s, want %s: the server has no wire type %s", tt.col, got, tt.wire, tt.name)
			}
			described := rowsOf(t, db, "DESCRIBE "+name)
			if len(described) != 1 || !strings.Contains(fmt.Sprint(described[0][0]), strings.SplitN(tt.col, "<", 2)[0]) {
				t.Errorf("DESCRIBE names the column %v, want the Hive type %s", described, tt.col)
			}
		})
	}
}

// createTable returns the statement that makes the table of the case.
func createTable(t *testing.T, tc typeCase, name string) string {
	t.Helper()
	dir := strings.TrimPrefix(name, prefix+"_")
	if tc.hive {
		return "CREATE EXTERNAL TABLE " + name + " (k string, v " + tc.col + ") STORED AS PARQUET LOCATION '" + location(t, dir) + "'"
	}
	return "CREATE TABLE " + name + " (k string, v " + tc.col + ") LOCATION '" + location(t, dir) + "' TBLPROPERTIES ('table_type'='ICEBERG')"
}
