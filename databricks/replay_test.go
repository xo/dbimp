package databricks //nolint:testpackage // The tests point a connector at a fake server, and read the columns of a result, which only the package can do.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// These tests replay the exchanges that step 6 recorded from a workspace, under
// testdata/databricks/. Each one decodes a real answer through the driver. The
// request of each exchange holds the statement, so a test reads it from the
// recording and never writes it again. The fake server of dbimptest matches the
// body of the request with the recorded body, so a test fails when the driver
// sends another body.

// statementOf returns the statement of the recorded exchange n.
func statementOf(tb testing.TB, n int) string {
	tb.Helper()
	var body struct {
		Statement string `json:"statement"`
	}
	if err := json.Unmarshal(exchange(tb, n).Request.Content(), &body); err != nil {
		tb.Fatalf("reading the request of the exchange %d: %v", n, err)
	}
	return body.Statement
}

// local makes a date and a time with no zone.
func local(year int, month time.Month, day, hour, minute, second, nanos int) dbimp.LocalDateTime {
	return dbimp.LocalDateTime{
		Date: dbimp.Date{Year: year, Month: month, Day: day},
		Time: dbimp.LocalTime{Hour: hour, Minute: minute, Second: second, Nanosecond: nanos},
	}
}

// utc makes an instant in UTC.
func utc(year int, month time.Month, day, hour, minute, second, nanos int) time.Time {
	return time.Date(year, month, day, hour, minute, second, nanos, time.UTC)
}

func TestReplaySelect(t *testing.T) {
	t.Parallel()
	db := replay(t)
	query := statementOf(t, 21)
	cols, got, err := readAll(t, db, query)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cols, []string{"a", "b"}) {
		t.Errorf("the columns are %q, want a and b", cols)
	}
	checkRows(t, got, [][]any{{int64(1), "x"}})
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if got := cts[0].DatabaseTypeName(); got != "INT" {
		t.Errorf("the database type of a is %s, want INT", got)
	}
	if got := cts[1].DatabaseTypeName(); got != "STRING" {
		t.Errorf("the database type of b is %s, want STRING", got)
	}
	if nullable, ok := cts[0].Nullable(); !nullable || ok {
		t.Errorf("Nullable is %v and %v, want true and false, because the manifest names no nullability", nullable, ok)
	}
	if err := rows.Err(); err != nil {
		t.Error(err)
	}
}

// TestReplayColumns holds D18: the columns come in the order of the statement,
// with the names that the server gives, and a result with no rows has them too.
func TestReplayColumns(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		n    int
		want []string
		rows int
	}{
		{57, []string{"z", "y", "x", "b", "a"}, 1},
		{53, []string{"id", "s"}, 0},
		{59, []string{"id", "s"}, 0},
		{54, []string{"a", "a"}, 1},
		{55, []string{"1", "x", "2.5"}, 1},
		{56, []string{"a b", "c.d", "Mixed Case"}, 1},
		{58, []string{}, 0},
	} {
		cols, got, err := readAll(t, db, statementOf(t, tt.n))
		if err != nil {
			t.Errorf("exchange %d: %v", tt.n, err)
			continue
		}
		if !slices.Equal(cols, tt.want) {
			t.Errorf("exchange %d: the columns are %q, want %q", tt.n, cols, tt.want)
		}
		if len(got) != tt.rows {
			t.Errorf("exchange %d: %d rows, want %d", tt.n, len(got), tt.rows)
		}
	}
}

// TestReplayTypes holds D135 and D193: each type of the recordings has the Go
// type of its column, a NULL is nil, and the scan type of a column is the type of
// its values. The values are those that the server sent for the statement of
// each exchange.
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	db := replay(t)
	bin := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	ds := func(days int32, d time.Duration) dbimp.Interval {
		return dbimp.Interval{Days: days, Nanoseconds: int64(d)}
	}
	hms := 2*time.Hour + 3*time.Minute + 4*time.Second
	for _, tt := range []struct {
		n     int
		types string
		want  []any
	}{
		{69, "TINYINT TINYINT SMALLINT SMALLINT INT INT BIGINT BIGINT", []any{int64(-128), int64(127), int64(-32768), int64(32767), int64(math.MinInt32), int64(math.MaxInt32), int64(math.MinInt64), int64(math.MaxInt64)}},
		{70, "DECIMAL DECIMAL", []any{dec(t, "9223372036854775808"), dec(t, "18446744073709551615")}},
		{71, "FLOAT FLOAT FLOAT DOUBLE DOUBLE DOUBLE DOUBLE DOUBLE", []any{1.5, 0.1, 16777216.0, 1.5, 0.1, math.MaxFloat64, math.SmallestNonzeroFloat64, 0.0}},
		{72, "DOUBLE DOUBLE DOUBLE", []any{math.NaN(), math.Inf(1), math.Inf(-1)}},
		{73, "FLOAT FLOAT FLOAT", []any{math.NaN(), math.Inf(1), math.Inf(-1)}},
		{74, "DECIMAL DECIMAL DECIMAL DECIMAL", []any{dec(t, "1.50"), dec(t, "0.00000"), dec(t, "-1.5"), dec(t, "12345")}},
		{75, "DECIMAL DECIMAL DECIMAL DECIMAL DECIMAL", []any{
			dec(t, "99999999999999999999999999999999999999"), dec(t, "-99999999999999999999999999999999999999"),
			dec(t, "0.1234567890123456789012345678901234567"), dec(t, "0.99999999999999999999999999999999999999"),
			dec(t, "12345678901234567890.123456789012345678"),
		}},
		{76, "BOOLEAN BOOLEAN BOOLEAN", []any{true, false, nil}},
		{77, "STRING STRING STRING STRING STRING STRING STRING", []any{"hello", "", " ", "héllo 日本 😀", `a"b\c`, "line\nbreak", "tab\there"}},
		{78, "BINARY BINARY BINARY BINARY BINARY", []any{bin, []byte("abc"), []byte{}, []byte{0}, []byte("hé")}},
		{79, "DATE DATE DATE DATE DATE", []any{
			dbimp.Date{Year: 2024, Month: time.January, Day: 2}, dbimp.Date{Year: 1, Month: time.January, Day: 1},
			dbimp.Date{Year: 9999, Month: time.December, Day: 31}, dbimp.Date{Year: 1500, Month: time.June, Day: 15},
			dbimp.Date{Year: 1970, Month: time.January, Day: 1},
		}},
		{81, "TIMESTAMP TIMESTAMP TIMESTAMP TIMESTAMP TIMESTAMP TIMESTAMP", []any{
			utc(2024, time.January, 2, 3, 4, 5, 123000000), utc(2024, time.January, 1, 21, 34, 5, 123000000),
			utc(2024, time.January, 2, 3, 4, 5, 0), utc(1970, time.January, 1, 0, 0, 0, 0),
			utc(1, time.January, 1, 0, 0, 0, 0), utc(9999, time.December, 31, 23, 59, 59, 999000000),
		}},
		{82, "TIMESTAMP_NTZ TIMESTAMP_NTZ TIMESTAMP_NTZ TIMESTAMP_NTZ", []any{
			local(2024, time.January, 2, 3, 4, 5, 123000000), local(2024, time.January, 2, 3, 4, 5, 0),
			local(1, time.January, 1, 0, 0, 0, 0), local(9999, time.December, 31, 23, 59, 59, 999000000),
		}},
		{83, "INTERVAL INTERVAL INTERVAL INTERVAL", []any{dbimp.Interval{Months: 14}, dbimp.Interval{Months: -14}, dbimp.Interval{Months: 60}, dbimp.Interval{Months: 3}}},
		{84, "INTERVAL INTERVAL INTERVAL INTERVAL INTERVAL INTERVAL", []any{
			ds(1, hms+123456*time.Microsecond), ds(-1, -(hms + 500*time.Millisecond)), ds(5, 0),
			ds(0, 10*time.Hour), ds(0, 30*time.Minute), ds(0, 1500*time.Millisecond),
		}},
		{85, "ARRAY ARRAY ARRAY ARRAY ARRAY", []any{
			[]any{int64(1), int64(2), int64(3)}, []any{}, []any{nil, int64(1)}, []any{`a"b`, "c,d"},
			[]any{[]any{int64(1), int64(2)}, []any{int64(3)}},
		}},
		{86, "MAP MAP MAP MAP", []any{
			map[string]any{"a": int64(1), "b": int64(2)}, map[string]any{}, map[string]any{"1": "x"}, map[string]any{"a": nil},
		}},
		{87, "STRUCT STRUCT STRUCT", []any{
			map[string]any{"a": int64(1), "b": "x"}, map[string]any{"a": nil, "b": "y"}, map[string]any{"col1": int64(1), "col2": "x"},
		}},
		{88, "ARRAY ARRAY ARRAY ARRAY ARRAY", []any{
			[]any{map[string]any{"a": map[string]any{"k": []any{int64(1), int64(2)}}}},
			[]any{bin},
			[]any{utc(2024, time.January, 2, 3, 4, 5, 0)},
			[]any{dec(t, "1.50")},
			[]any{dbimp.Date{Year: 2024, Month: time.January, Day: 2}},
		}},
		{89, "VARIANT VARIANT VARIANT VARIANT VARIANT", []any{
			map[string]any{"a": int64(1), "b": []any{int64(1), int64(2), "x"}}, 1.5, "s", nil, nil,
		}},
		{90, "GEOMETRY STRING", []any{"POINT(1 2)", "POINT(1 2)"}},
		{91, "GEOGRAPHY STRING", []any{"SRID=4326;POINT(1 2)", "POINT(1 2)"}},
		{92, "TINYINT SMALLINT INT BIGINT FLOAT DOUBLE DECIMAL BOOLEAN STRING BINARY DATE TIMESTAMP TIMESTAMP_NTZ", make([]any, 13)},
		{93, "ARRAY MAP STRUCT INTERVAL INTERVAL VARIANT", make([]any, 6)},
	} {
		cts, vals, err := firstRow(t.Context(), db, statementOf(t, tt.n))
		if err != nil {
			t.Errorf("exchange %d: %v", tt.n, err)
			continue
		}
		types := strings.Fields(tt.types)
		if len(cts) != len(types) || len(vals) != len(tt.want) {
			t.Errorf("exchange %d: %d columns, want %d types and %d values", tt.n, len(cts), len(types), len(tt.want))
			continue
		}
		for i, ct := range cts {
			if ct.DatabaseTypeName() != types[i] {
				t.Errorf("exchange %d: the database type of %s is %s, want %s", tt.n, ct.Name(), ct.DatabaseTypeName(), types[i])
			}
			if !same(vals[i], tt.want[i]) {
				t.Errorf("exchange %d: %s is %#v (%T), want %#v (%T)", tt.n, ct.Name(), vals[i], vals[i], tt.want[i], tt.want[i])
			}
			if v := tt.want[i]; v != nil && types[i] != "VARIANT" {
				if got := reflect.TypeOf(v).String(); got != ct.ScanType().String() {
					t.Errorf("exchange %d: the scan type of %s is %s, and its value is %s (D135)", tt.n, ct.Name(), ct.ScanType(), got)
				}
			}
		}
	}
}

// firstRow runs a statement, and returns the types of its columns and the first row,
// read into *any.
func firstRow(ctx context.Context, db *sql.DB, query string) ([]*sql.ColumnType, []any, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, err
	}
	vals := make([]any, len(cts))
	ptrs := make([]any, len(cts))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		return cts, nil, sql.ErrNoRows
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, nil, err
	}
	return cts, vals, rows.Err()
}

// same is equal, and a NaN is the same as a NaN.
func same(got, want any) bool {
	if g, ok := got.(float64); ok {
		if w, ok := want.(float64); ok && math.IsNaN(g) && math.IsNaN(w) {
			return true
		}
	}
	return equal(got, want)
}

// TestReplayEveryType reads the exchange of a table with a column of each type,
// through the real decoder (step 14a): a row of values, and a row of NULL.
func TestReplayEveryType(t *testing.T) {
	t.Parallel()
	db := replay(t)
	cols, got, err := readAll(t, db, statementOf(t, 97))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Fields("id ti si i bi f d dec dec0 b s y dt ts ntz arr m st"); !slices.Equal(cols, want) {
		t.Fatalf("the columns are %q, want %q", cols, want)
	}
	want := [][]any{
		{
			int64(1), int64(-128), int64(32767), int64(2147483647), int64(9223372036854775807), 1.5, 2.5,
			dec(t, "12345678901234567890.123456789012345678"), dec(t, "99999999999999999999999999999999999999"),
			true, "x", []byte{0xDE, 0xAD, 0xBE, 0xEF},
			dbimp.Date{Year: 2024, Month: time.January, Day: 2},
			// The server keeps three digits of the fraction (measured).
			utc(2024, time.January, 2, 3, 4, 5, 123000000),
			local(2024, time.January, 2, 3, 4, 5, 123000000),
			[]any{int64(1), int64(2)}, map[string]any{"a": int64(1)}, map[string]any{"a": int64(1), "b": "x"},
		},
		make([]any, 18),
	}
	want[1][0] = int64(2)
	checkRows(t, got, want)
	_, got, err = readAll(t, db, statementOf(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	checkRows(t, got, [][]any{{int64(1), map[string]any{"a": int64(1)}}})
}

// TestReplayScan holds that the real path of a caller works: each type scans
// into the destination that fits it, and a NULL into sql.Null.
func TestReplayScan(t *testing.T) {
	t.Parallel()
	db := replay(t)
	var (
		i  int64
		n  sql.Null[int64]
		f  float64
		s  string
		b  []byte
		ok bool
		d  dbimp.Date
		ts time.Time
		nt dbimp.LocalDateTime
		an any
	)
	// id, ti, si, i, bi, f, d, dec, dec0, b, s, y, dt, ts, ntz, arr, m, st
	rows, err := db.QueryContext(t.Context(), statementOf(t, 97))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	var decStr string
	var skip any
	if err := rows.Scan(&i, &n, &skip, &skip, &skip, &f, &skip, &decStr, &skip, &ok, &s, &b, &d, &ts, &nt, &an, &skip, &skip); err != nil {
		t.Fatal(err)
	}
	if i != 1 || !n.Valid || n.V != -128 || f != 1.5 || decStr != "12345678901234567890.123456789012345678" || !ok || s != "x" || !slices.Equal(b, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("scanned %d %v %v %q %v %q %x", i, n, f, decStr, ok, s, b)
	}
	if d != (dbimp.Date{Year: 2024, Month: time.January, Day: 2}) || !ts.Equal(utc(2024, time.January, 2, 3, 4, 5, 123000000)) || nt != local(2024, time.January, 2, 3, 4, 5, 123000000) {
		t.Errorf("scanned %v %v %v", d, ts, nt)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	// The second row is a row of NULL, except its id.
	var (
		id    int64
		nInt  sql.Null[int64]
		nF    sql.Null[float64]
		nB    sql.Null[bool]
		nS    sql.Null[string]
		nT    sql.Null[time.Time]
		nAny  any
		nByte []byte
	)
	if err := rows.Scan(&id, &nInt, &nInt, &nInt, &nInt, &nF, &nF, &nS, &nS, &nB, &nS, &nByte, &nT, &nT, &nAny, &nAny, &nAny, &nAny); err == nil {
		// A date and a local timestamp have no sql.Null[time.Time] form, so the
		// scan of their NULL into it is the one that database/sql can do.
		if id != 2 || nInt.Valid || nF.Valid || nB.Valid || nS.Valid || nT.Valid || nAny != nil || nByte != nil {
			t.Errorf("a row of NULL scanned as %d %v %v %v %v %v %v %v", id, nInt, nF, nB, nS, nT, nAny, nByte)
		}
	} else {
		t.Errorf("scanning a row of NULL: %v", err)
	}
}

// TestReplayErrors holds D193 item 9: a statement that fails in the engine is an
// *Error with its code, its SQLSTATE and its message, before any row, and so is
// a statement that fails after the engine ran some of it.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		n        int
		sqlState string
		contains string
	}{
		{154, "42601", "PARSE_SYNTAX_ERROR"},
		{156, "42P01", "TABLE_OR_VIEW_NOT_FOUND"},
		{157, "42703", "UNRESOLVED_COLUMN"},
		{158, "42883", "UNRESOLVED_ROUTINE"},
		{159, "22012", "DIVIDE_BY_ZERO"},
		{160, "22012", "DIVIDE_BY_ZERO"},
		{161, "22003", "ARITHMETIC_OVERFLOW"},
		{162, "22018", "CAST_INVALID_INPUT"},
		{165, "42P07", "TABLE_OR_VIEW_ALREADY_EXISTS"},
		{166, "22012", "DIVIDE_BY_ZERO"},
		{172, "P0001", "dbimp raised on purpose"},
		{128, "42P02", "UNBOUND_SQL_PARAMETER"},
		{38, "42617", "PARSE_EMPTY_STATEMENT"},
		{267, "42601", "extra input"},
		{275, "", ""},
		{199, "42501", "BROWSE"},
		{201, "42501", ""},
		{280, "22012", "DIVIDE_BY_ZERO"},
	} {
		err := queryError(t.Context(), db, statementOf(t, tt.n))
		if err == nil {
			t.Errorf("exchange %d: no error", tt.n)
			continue
		}
		e := serverError(t, err)
		if e.HTTPStatus != 200 || e.Code != "BAD_REQUEST" || e.Message == "" || e.StatementID == "" {
			t.Errorf("exchange %d: the error is %+v, want HTTP 200, BAD_REQUEST, a message and an id", tt.n, *e)
		}
		if tt.sqlState != "" && e.SQLState != tt.sqlState {
			t.Errorf("exchange %d: the SQLSTATE is %q, want %q", tt.n, e.SQLState, tt.sqlState)
		}
		if !strings.Contains(e.Message, tt.contains) {
			t.Errorf("exchange %d: the message is %q, want %q in it", tt.n, e.Message, tt.contains)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("exchange %d: an error before any row wraps dbimp.ErrIncomplete (D107)", tt.n)
		}
		if strings.HasPrefix(e.Message, "\n") || strings.HasSuffix(e.Message, "\n") {
			t.Errorf("exchange %d: the message has white space at its ends: %q", tt.n, e.Message)
		}
	}
}

// TestReplayStatusErrors holds the errors that a request gets before the server
// runs a statement, which carry an HTTP status. The fake server answers each
// request with the recorded answer.
func TestReplayStatusErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		n      int
		status int
		code   string
		msg    string
	}{
		{"a wait that is too long", 32, 400, "INVALID_PARAMETER_VALUE", "wait_timeout"},
		{"a body with no warehouse", 40, 400, "BAD_REQUEST", ""},
		{"a warehouse that the login cannot use", 173, 403, "PERMISSION_DENIED", "SQL Warehouse"},
		{"a warehouse id that is not valid", 174, 400, "INVALID_PARAMETER_VALUE", "not a valid endpoint id"},
		{"a wrong token", 195, 403, "403", "Invalid access token"},
		{"a method that the API lacks", 300, 404, "ENDPOINT_NOT_FOUND", "PUT"},
		{"a path with an empty body", 306, 404, "", "Not Found"},
		{"a body that is not JSON", 42, 400, "MALFORMED_REQUEST", ""},
	} {
		_, srv := newScript(t, tt.n)
		db := open(t, recordedConfig(), srv.URL)
		_, err := db.ExecContext(t.Context(), "SELECT 1")
		if err == nil {
			t.Errorf("%s: no error", tt.name)
			continue
		}
		e := serverError(t, err)
		if e.HTTPStatus != tt.status || e.Code != tt.code || !strings.Contains(e.Message, tt.msg) {
			t.Errorf("%s: the error is %+v, want HTTP %d, %q and %q in the message", tt.name, *e, tt.status, tt.code, tt.msg)
		}
		var se *dbimp.StatusError
		if !errors.As(err, &se) || se.Code != tt.status {
			t.Errorf("%s: the error does not unwrap to a *dbimp.StatusError with HTTP %d", tt.name, tt.status)
		}
		if errors.Is(err, driver.ErrBadConn) {
			t.Errorf("%s: an HTTP status is no bad connection", tt.name)
		}
	}
}

// TestReplayParameters holds D193 item 9: each argument is the parameter that its
// Go type names, and the body is the body that the recorder sent, which the fake
// server matches.
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	db := replay(t)
	query := statementOf(t, 102)
	for _, tt := range []struct {
		n   int
		arg any
	}{
		{102, "abc"},
		{104, int64(9223372036854775807)},
		{106, 1.5},
		{108, dec(t, "12345678901234567890.123456789012345678")},
		{109, true},
		{111, dbimp.Date{Year: 2024, Month: time.January, Day: 2}},
		{112, utc(2024, time.January, 2, 3, 4, 5, 123456000)},
		{113, local(2024, time.January, 2, 3, 4, 5, 123456000)},
		{114, dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute + 4*time.Second)}},
		{115, dbimp.Interval{Months: 14}},
		{123, ""},
		{125, "a'b; DROP TABLE x --"},
	} {
		if statementOf(t, tt.n) != query {
			t.Fatalf("the statement of the exchange %d is not the one of the others", tt.n)
		}
		if _, _, err := readAll(t, db, query, sql.Named("p", tt.arg)); err != nil {
			t.Errorf("exchange %d: %v", tt.n, err)
		}
	}
}

// TestReplayRowsAffected holds D193 item 8: RowsAffected is num_affected_rows,
// and an answer with no count gives an error that wraps dbimp.ErrNotSupported.
func TestReplayRowsAffected(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, tt := range []struct {
		n    int
		want int64
	}{
		{212, 3},
		{213, 3},
		{214, 5},
		{215, 1},
		{216, 2},
		{218, 2},
	} {
		res, err := db.ExecContext(t.Context(), statementOf(t, tt.n))
		if err != nil {
			t.Fatalf("exchange %d: %v", tt.n, err)
		}
		if n, err := res.RowsAffected(); err != nil || n != tt.want {
			t.Errorf("exchange %d: RowsAffected is %d, %v, want %d", tt.n, n, err, tt.want)
		}
	}
	// A truncate, a create table, a create view and a create table as select
	// have no count (measured).
	for _, n := range []int{217, 58, 223, 222, 226} {
		res, err := db.ExecContext(t.Context(), statementOf(t, n))
		if err != nil {
			t.Fatalf("exchange %d: %v", n, err)
		}
		if n, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) || n != 0 {
			t.Errorf("exchange %d: RowsAffected is %d, %v, want an error that wraps dbimp.ErrNotSupported", n, n, err)
		}
	}
	res, err := db.ExecContext(t.Context(), statementOf(t, 21))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want an error that wraps dbimp.ErrNotSupported", err)
	}
}

// TestReplayTransactions holds D193 item 5: BeginTx fails with
// dbimp.ErrNotSupported, and sends no request.
func TestReplayTransactions(t *testing.T) {
	t.Parallel()
	s, srv := newScript(t, 21)
	db := open(t, recordedConfig(), srv.URL)
	tx, err := db.BeginTx(t.Context(), nil)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("BeginTx returned a transaction")
	}
	if !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("the error is %v, want dbimp.ErrNotSupported", err)
	}
	if got := s.requests(); len(got) != 0 {
		t.Errorf("BeginTx sent %v", got)
	}
}

// TestReplayStatements holds that a statement is sent as the caller wrote it:
// a comment before and after it, a trailing semicolon, a compound statement and a
// prepared statement.
func TestReplayStatements(t *testing.T) {
	t.Parallel()
	db := replay(t)
	for _, n := range []int{270, 271, 272, 273, 274} {
		if _, got, err := readAll(t, db, statementOf(t, n)); err != nil || len(got) != 1 {
			t.Errorf("exchange %d: %d rows, %v", n, len(got), err)
		}
	}
	_, got, err := readAll(t, db, statementOf(t, 278))
	if err != nil {
		t.Fatal(err)
	}
	checkRows(t, got, [][]any{{int64(2)}})
	// The server refuses a statement that holds only a comment, only a semicolon
	// or only white space, and two statements (measured).
	for _, n := range []int{275, 276, 277, 267, 268} {
		if _, _, err := readAll(t, db, statementOf(t, n)); err == nil {
			t.Errorf("exchange %d: no error", n)
		}
	}
	stmt, err := db.PrepareContext(t.Context(), statementOf(t, 21))
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	var a int64
	var b string
	if err := stmt.QueryRowContext(t.Context()).Scan(&a, &b); err != nil {
		t.Fatal(err)
	}
	if a != 1 || b != "x" {
		t.Errorf("a prepared statement gave %d and %q", a, b)
	}
}

// TestReplayVersion holds D181: the driver gives no answer of its own to a
// version request, so SELECT version() goes to the server, which answers it
// (measured). Ping runs.
func TestReplayVersion(t *testing.T) {
	t.Parallel()
	db := replay(t)
	var version string
	if err := db.QueryRowContext(t.Context(), statementOf(t, 203)).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "4.2.0 ") {
		t.Errorf("the version is %q, want 4.2.0 and a hash", version)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping: %v", err)
	}
}

// TestReplayNamespace holds that the catalog and the schema of the DSN are in
// each body, and the server answers them (measured).
func TestReplayNamespace(t *testing.T) {
	t.Parallel()
	db := replay(t)
	_, got, err := readAll(t, db, statementOf(t, 204))
	if err != nil {
		t.Fatal(err)
	}
	checkRows(t, got, [][]any{{"workspace", "dbimp", "dbimp"}})
}
