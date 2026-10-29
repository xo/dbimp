package maxcompute //nolint:testpackage // The tests read the types of the driver, which are not exported.

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// resultOf returns the CSV of the recorded result of the first statement whose
// query holds fragment, which succeeded with a result of the form csv.
func resultOf(tb testing.TB, fragment string) string {
	tb.Helper()
	for n := 1; n < exchangeCount; n++ {
		ex := exchange(tb, n)
		if ex.Request.Method != http.MethodPost || ex.Response.Status != http.StatusCreated || !strings.Contains(queryOf(string(ex.Request.Content())), fragment) {
			continue
		}
		res := exchange(tb, n+2)
		if strings.Contains(string(res.Response.Content()), `Format="csv"`) {
			return csvOf(tb, res)
		}
	}
	tb.Fatalf("no recorded csv result for %q", fragment)
	return ""
}

// cells reads the records of a CSV text.
func cells(tb testing.TB, text string) [][]cell {
	tb.Helper()
	r := newCSVReader(strings.NewReader(text))
	var out [][]cell
	for {
		rec, err := r.record()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			tb.Fatalf("reading the csv: %v", err)
		}
		out = append(out, rec)
	}
}

func TestDecodeRecordedValues(t *testing.T) {
	t.Parallel()
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("the machine has no time zone data: %v", err)
	}
	for _, tt := range []struct {
		fragment string
		typ      string
		lo, hi   string
	}{
		{"CAST(-128 AS TINYINT)", "TINYINT", "-128", "127"},
		{"CAST(-32768 AS SMALLINT)", "SMALLINT", "-32768", "32767"},
		{"CAST(-2147483648 AS INT)", "INT", "-2147483648", "2147483647"},
		{"CAST('-9223372036854775807' AS BIGINT)", "BIGINT", "-9223372036854775807", "9223372036854775807"},
		{"CAST(-3.4028235E38 AS FLOAT)", "FLOAT", "-3.402823e+38", "1.1"},
		{"CAST(-1.7976931348623157E308 AS DOUBLE)", "DOUBLE", "-1.7976931348623157e+308", "0.1"},
		{"CAST('-99999999999999999999.999999999999999999' AS DECIMAL(38,18))", "DECIMAL(38,18)", "-99999999999999999999.999999999999999999", "99999999999999999999.999999999999999999"},
		{"CAST('-99999999999999999999999999999999999999' AS DECIMAL(38,0))", "DECIMAL(38,0)", "-99999999999999999999999999999999999999", "99999999999999999999999999999999999999"},
		{"false AS lo, true AS hi", "BOOLEAN", "false", "true"},
		{"CAST('abc' AS VARCHAR(5))", "VARCHAR(5)", "abc", "abcde"},
		{"CAST('ab' AS CHAR(5))", "CHAR(5)", "ab   ", "abcde"},
		{"CAST('abc' AS BINARY) AS lo", "BINARY", "[97 98 99]", "[0 255 16]"},
		{"DATE'0001-01-01'", "DATE", "0001-01-01", "9999-12-31"},
		{"DATETIME'0001-01-01 00:00:00'", "DATETIME", "0001-01-01 00:00:00 +0805 LMT", "9999-12-31 23:59:59 +0800 CST"},
		{"TIMESTAMP'0001-01-01 00:00:00.000000001'", "TIMESTAMP", "0001-01-01 00:00:00.000000001 +0805 LMT", "9999-12-31 23:59:59.999999999 +0800 CST"},
		{"TIMESTAMP_NTZ'2026-10-29 01:02:03.123456789'", "TIMESTAMP_NTZ", "2026-10-29T01:02:03.123456789", "0001-01-01T00:00:00"},
		{"ARRAY('a,b', 'c\"d', NULL) AS lo", "ARRAY<STRING>", `[a,b, c"d, NULL]`, "[]"},
		{"MAP('k', 1L, 'z', CAST(NULL AS BIGINT)) AS lo", "MAP<STRING,BIGINT>", "{k:1, z:NULL}", "{}"},
		{"NAMED_STRUCT('x', 1L, 'y', 'a,b') AS lo", "STRUCT<x:BIGINT,y:STRING>", "{x:1, y:a,b}", "{x:NULL, y:NULL}"},
		{"JSON '{\"a\":[1,2,{\"b\":null}]}'", "JSON", "map[a:[1 2 map[b:<nil>]]]", "<nil>"},
	} {
		t.Run(tt.typ, func(t *testing.T) {
			t.Parallel()
			typ, err := parseType(tt.typ)
			if err != nil {
				t.Fatal(err)
			}
			recs := cells(t, resultOf(t, tt.fragment))
			if len(recs) != 2 || len(recs[1]) != 3 {
				t.Fatalf("the result has %d records, want a header and one row of three cells", len(recs))
			}
			for i, want := range []string{tt.lo, tt.hi} {
				v, err := decode(typ, recs[1][i], shanghai)
				if err != nil {
					t.Fatal(err)
				}
				if got := text(v); got != want {
					t.Errorf("the value %d is %q (%T), want %q", i, got, v, want)
				}
			}
			if v, err := decode(typ, recs[1][2], shanghai); err != nil || v != nil {
				t.Errorf("the NULL is %v and %v, want nil (hard rule 3)", v, err)
			}
		})
	}
}

// text writes a decoded value as text, so that a test compares the value and not
// its pointer.
func text(v driver.Value) string {
	switch v := v.(type) {
	case time.Time:
		return v.Format("2006-01-02 15:04:05.999999999 -0700 MST")
	case fmt.Stringer:
		return v.String()
	}
	return fmt.Sprint(v)
}

func TestIntervalsAndTheStringOfBackslashN(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		fragment, typ string
		want          dbimp.Interval
	}{
		{"INTERVAL '1 02:03:04' DAY TO SECOND", "INTERVAL_DAY_TIME", dbimp.Interval{Days: 1, Nanoseconds: ((2*60+3)*60 + 4) * int64(time.Second)}},
		{"INTERVAL '1-2' YEAR TO MONTH", "INTERVAL_YEAR_MONTH", dbimp.Interval{Months: 14}},
	} {
		typ, _ := parseType(tt.typ)
		recs := cells(t, resultOf(t, tt.fragment))
		v, err := decode(typ, recs[1][0], time.UTC)
		if err != nil || v != tt.want {
			t.Errorf("%s is %v and %v, want %v", tt.typ, v, err, tt.want)
		}
	}
	recs := cells(t, resultOf(t, "CONCAT(CHR(92), 'N')"))
	if c := recs[1][0]; c.null || c.text != nullText {
		t.Errorf("a string of a backslash and N is %+v, want text \\N that is no NULL", c)
	}
	if c := recs[1][1]; !c.null {
		t.Errorf("the NULL cell is %+v, want a NULL", c)
	}
}

func TestParseType(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in        string
		name      string
		k         kind
		length    int64
		prec, sc  int64
		shouldErr bool
	}{
		{"BIGINT", "BIGINT", kindInteger, 0, -1, -1, false},
		{"DECIMAL(38,18)", "DECIMAL", kindDecimal, 0, 38, 18, false},
		{"DECIMAL(10,2)", "DECIMAL", kindDecimal, 0, 10, 2, false},
		{"VARCHAR(5)", "VARCHAR", kindString, 5, -1, -1, false},
		{"CHAR(5)", "CHAR", kindString, 5, -1, -1, false},
		{"struct<x:BIGINT>", "STRUCT", kindText, 0, -1, -1, false},
		{"MAP<STRING,BIGINT>", "MAP", kindText, 0, -1, -1, false},
		{"ARRAY<INT>", "ARRAY", kindText, 0, -1, -1, false},
		{"VOID", "VOID", kindVoid, 0, -1, -1, false},
		{"GEOGRAPHY", "", 0, 0, 0, 0, true},
		{"VARCHAR(x)", "", 0, 0, 0, 0, true},
	} {
		typ, err := parseType(tt.in)
		if tt.shouldErr {
			if !errors.Is(err, dbimp.ErrNotSupported) && !errors.Is(err, dbimp.ErrInvalidValue) {
				t.Errorf("parseType(%q) gave %v, want an error", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseType(%q): %v", tt.in, err)
			continue
		}
		if typ.name != tt.name || typ.kind != tt.k || typ.length != tt.length || typ.precision != tt.prec || typ.scale != tt.sc {
			t.Errorf("parseType(%q) = %+v", tt.in, *typ)
		}
	}
}

func TestParseTypesOfExplainOutput(t *testing.T) {
	t.Parallel()
	f := recordedFlow(t, "EXPLAIN OUTPUT SELECT CAST(1 AS TINYINT) a, CAST(1 AS SMALLINT) b, CAST(1 AS INT) c, CAST(1 AS BIGINT) d, CAST(1 AS FLOAT) e, CAST(1 AS DOUBLE) f, CAST(1 AS DECIMAL(38,18)) g, CAST(1 AS DECIMAL(10,2)) g2, true h, 'x' i, CAST('x' AS VARCHAR(5)) j, CAST('x' AS CHAR(5)) k, unhex('00') l, DATE'2026-01-01' m, DATETIME'2026-01-01 00:00:00' n, TIMESTAMP'2026-01-01 00:00:00' o, TIMESTAMP_NTZ'2026-01-01 00:00:00' p, ARRAY(1,2) q, MAP('k',1L) r, NAMED_STRUCT('x',1L) s, JSON '{}' t, INTERVAL '1 02:03:04' DAY TO SECOND u, INTERVAL '1-2' YEAR TO MONTH v, NULL w", "Success")
	types, err := parseTypes(csvOf(t, f.result))
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 24 {
		t.Fatalf("%d types, want 24", len(types))
	}
	var names []string
	for _, ty := range types {
		names = append(names, ty.name)
	}
	if got, want := strings.Join(names, " "), "TINYINT SMALLINT INT BIGINT FLOAT DOUBLE DECIMAL DECIMAL BOOLEAN STRING VARCHAR CHAR BINARY DATE DATETIME TIMESTAMP TIMESTAMP_NTZ ARRAY MAP STRUCT JSON INTERVAL_DAY_TIME INTERVAL_YEAR_MONTH VOID"; got != want {
		t.Errorf("the types are %s, want %s", got, want)
	}
}

func TestUnescapeBinary(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"abc": "abc", "=00=FF=10": "\x00\xff\x10", "a=3Db": "a=b", "": ""} {
		got, err := unescapeBinary(in)
		if err != nil || string(got) != want {
			t.Errorf("unescapeBinary(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"=0", "=ZZ", "ab="} {
		if _, err := unescapeBinary(in); !errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("unescapeBinary(%q) gave %v, want dbimp.ErrInvalidValue", in, err)
		}
	}
}
