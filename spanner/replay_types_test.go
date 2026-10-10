package spanner //nolint:testpackage // The tests share the replay server of the package.

import (
	"bytes"
	"database/sql"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
)

// dec makes a decimal from text.
func dec(tb testing.TB, s string) *apd.Decimal {
	tb.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		tb.Fatal(err)
	}
	return d
}

// equalValue reports whether got is want. NaN is NaN, a decimal is its number, a
// byte slice is its bytes, and a list is its elements.
func equalValue(got, want any) bool {
	switch w := want.(type) {
	case float64:
		g, ok := got.(float64)
		return ok && (g == w && math.Signbit(g) == math.Signbit(w) || math.IsNaN(g) && math.IsNaN(w))
	case *apd.Decimal:
		g, ok := got.(*apd.Decimal)
		return ok && g.Cmp(w) == 0
	case []byte:
		g, ok := got.([]byte)
		return ok && bytes.Equal(g, w)
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !equalValue(g[i], w[i]) {
				return false
			}
		}
		return true
	case time.Time:
		g, ok := got.(time.Time)
		return ok && g.Equal(w) && g.Location() == time.UTC
	}
	return reflect.DeepEqual(got, want)
}

// readRows reads every row of the query into *any values.
func readRows(tb testing.TB, db *sql.DB, query string, args ...any) ([]string, [][]any) {
	tb.Helper()
	rows, err := db.QueryContext(tb.Context(), query, args...)
	if err != nil {
		tb.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		tb.Fatal(err)
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			tb.Fatal(err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		tb.Fatalf("%s: after %d rows: %v", query, len(out), err)
	}
	return cols, out
}

// TestReplayEveryType holds step 14: the rows of the table of every type, as the
// server wrote them to the stream, with the values before the metadata, decode
// to the Go type that the type table names for each column (D135 and D191).
func TestReplayEveryType(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	cols, got := readRows(t, db, "SELECT * FROM dbimp_t_types ORDER BY id")
	wantCols := []string{"id", "i64", "f64", "f32", "n", "b", "s", "y", "d", "ts", "j", "arr_i", "arr_s", "arr_f", "arr_n", "arr_b", "arr_y", "arr_d", "arr_ts", "arr_j"}
	if !reflect.DeepEqual(cols, wantCols) {
		t.Fatalf("the columns are %q, want %q", cols, wantCols)
	}
	day := dbimp.Date{Year: 2024, Month: time.January, Day: 2}
	stamp := time.Date(2024, time.January, 2, 3, 4, 5, 123456789, time.UTC)
	obj := map[string]any{"a": int64(1)}
	want := [][]any{
		{
			int64(1), int64(-5), 1.25, 1.5, dec(t, "1.5"), true, "x", []byte("abc"), day, stamp, obj,
			[]any{int64(1), int64(2)}, []any{"a", "b"}, []any{1.5, 2.5}, []any{dec(t, "1.5"), dec(t, "2.5")},
			[]any{true, false}, []any{[]byte("abc")}, []any{day}, []any{time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)}, []any{obj},
		},
		{
			int64(2), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		},
		{
			int64(3), int64(math.MaxInt64), math.NaN(), math.NaN(), dec(t, "99999999999999999999999999999.999999999"), false, "", []byte{},
			dbimp.Date{Year: 1, Month: time.January, Day: 1}, time.Date(9999, time.December, 31, 23, 59, 59, 999999999, time.UTC), nil,
			[]any{}, []any{nil, "x"}, []any{math.Inf(1), math.Inf(-1), math.NaN(), nil}, []any{nil}, []any{nil}, []any{nil}, []any{nil}, []any{nil}, []any{nil},
		},
	}
	if len(got) != len(want) {
		t.Fatalf("read %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		for j := range want[i] {
			if !equalValue(got[i][j], want[i][j]) {
				t.Errorf("row %d, column %s: got %#v, want %#v", i+1, cols[j], got[i][j], want[i][j])
			}
		}
	}
}

// TestReplayColumnTypes holds that each column reports the type that the
// type table names: its database type name, its scan type, that it can be NULL,
// and the precision and the scale of a NUMERIC.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM dbimp_t_types ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	defer func() {
		if err := rows.Err(); err != nil {
			t.Error(err)
		}
	}()
	types, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"INT64", "INT64", "FLOAT64", "FLOAT32", "NUMERIC", "BOOL", "STRING", "BYTES", "DATE", "TIMESTAMP", "JSON", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "ARRAY", "ARRAY"}
	wantScan := []reflect.Type{
		typeOfInt64, typeOfInt64, typeOfFloat64, typeOfFloat64, typeOfDecimal, typeOfBool, typeOfString, typeOfBytes, typeOfDate, typeOfTimestamp, typeOfJSON,
		typeOfList, typeOfList, typeOfList, typeOfList, typeOfList, typeOfList, typeOfList, typeOfList, typeOfList,
	}
	for i, ct := range types {
		if ct.DatabaseTypeName() != wantNames[i] {
			t.Errorf("the type name of %s is %q, want %q", ct.Name(), ct.DatabaseTypeName(), wantNames[i])
		}
		if ct.ScanType() != wantScan[i] {
			t.Errorf("the scan type of %s is %v, want %v", ct.Name(), ct.ScanType(), wantScan[i])
		}
		if nullable, ok := ct.Nullable(); !ok || !nullable {
			t.Errorf("Nullable of %s is %v, %v, want true, true", ct.Name(), nullable, ok)
		}
		p, s, ok := ct.DecimalSize()
		if isNumeric := ct.Name() == "n"; ok != isNumeric || isNumeric && (p != 38 || s != 9) {
			t.Errorf("DecimalSize of %s is %d, %d, %v", ct.Name(), p, s, ok)
		}
	}
}

// TestReplayScan holds that the row of the table of every type scans into the
// destinations that a caller writes: a basic type, a decimal, a date, a time, a
// byte slice, a list, and sql.Null of each, and that a NULL scans into a
// sql.Null as not valid and into a basic type as an error.
func TestReplayScan(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	var (
		id               int64
		i64              sql.Null[int64]
		f64              float64
		f32              sql.Null[float64]
		n                apd.Decimal
		b                bool
		str              string
		y                []byte
		d                sql.Null[dbimp.Date]
		ts               time.Time
		j                any
		arrI             []any
		arrS, arrF, arrN any
		arrB, arrY, arrD any
		arrTS, arrJ      any
	)
	err := db.QueryRowContext(t.Context(), "SELECT * FROM dbimp_t_types ORDER BY id").
		Scan(&id, &i64, &f64, &f32, &n, &b, &str, &y, &d, &ts, &j, &arrI, &arrS, &arrF, &arrN, &arrB, &arrY, &arrD, &arrTS, &arrJ)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 || !i64.Valid || i64.V != -5 || f64 != 1.25 || !f32.Valid || f32.V != 1.5 {
		t.Errorf("the numbers are %d %v %v %v", id, i64, f64, f32)
	}
	if n.Cmp(dec(t, "1.5")) != 0 || !equalValue(arrN, []any{dec(t, "1.5"), dec(t, "2.5")}) {
		t.Errorf("the decimals are %s %v", n.String(), arrN)
	}
	if !b || str != "x" || string(y) != "abc" || !d.Valid || d.V.String() != "2024-01-02" || !ts.Equal(time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)) {
		t.Errorf("the others are %v %q %q %v %v", b, str, y, d, ts)
	}
	if !equalValue(j, map[string]any{"a": int64(1)}) || !equalValue(arrI, []any{int64(1), int64(2)}) {
		t.Errorf("the JSON value is %#v and the array %#v", j, arrI)
	}
	// A decimal scans into a string too, as the text of its digits.
	var text string
	if err := db.QueryRowContext(t.Context(), "SELECT * FROM dbimp_t_types ORDER BY id").Scan(new(int64), new(any), new(any), new(any), &text, new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any), new(any)); err != nil || text != "1.5" {
		t.Errorf("a decimal scanned into a string is %q, error %v, want 1.5", text, err)
	}
	// The second row holds a NULL in every column but the first.
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM dbimp_t_types ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for range 2 {
		if !rows.Next() {
			t.Fatalf("reading the second row: %v", rows.Err())
		}
	}
	var null sql.Null[int64]
	rest := make([]any, 18)
	for i := range rest {
		rest[i] = new(any)
	}
	if err := rows.Scan(append([]any{&id, &null}, rest...)...); err != nil || null.Valid {
		t.Errorf("a NULL scans into a sql.Null as %v, error %v, want not valid", null, err)
	}
	var plain int64
	if err := rows.Scan(append([]any{&id, &plain}, rest...)...); err == nil {
		t.Error("a NULL scanned into an int64 without an error")
	}
}

// TestReplayStreamValues holds the values that the recorder read from literals
// on the stream: the three values of FLOAT64 that JSON lacks, and a TIMESTAMP
// with nanoseconds.
func TestReplayStreamValues(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	_, got := readRows(t, db, "SELECT CAST('NaN' AS FLOAT64) AS nan, CAST('inf' AS FLOAT64) AS pinf")
	if len(got) != 1 || !equalValue(got[0][0], math.NaN()) || !equalValue(got[0][1], math.Inf(1)) {
		t.Errorf("the floats are %#v", got)
	}
	_, got = readRows(t, db, "SELECT TIMESTAMP '2024-01-02T03:04:05.123456789Z' AS nanos")
	if len(got) != 1 || !equalValue(got[0][0], time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.UTC)) {
		t.Errorf("the timestamp is %#v", got)
	}
}

// TestReplayWholeResults holds the values that the recorder read with
// executeSql, which the fake server sends as a stream of one message: the
// limits of INT64, DATE, TIMESTAMP, the forms of NUMERIC, BYTES, UUID, INTERVAL
// and JSON, a list of structs, and a NULL of every type.
func TestReplayWholeResults(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	for _, tt := range []struct {
		query string
		want  []any
	}{
		{"SELECT CAST('9223372036854775807' AS INT64) AS hi, CAST('-9223372036854775808' AS INT64) AS lo, 0 AS z", []any{int64(math.MaxInt64), int64(math.MinInt64), int64(0)}},
		{"SELECT 1.5 AS a, 1e308 AS big, 5e-324 AS tiny, -0.0 AS negzero, 0.1 + 0.2 AS sum", []any{1.5, 1e308, 5e-324, math.Copysign(0, -1), 0.30000000000000004}},
		{"SELECT TRUE AS t, FALSE AS f", []any{true, false}},
		{"SELECT b'' AS empty, b'\\x00\\xff' AS binary, b'abc' AS abc", []any{[]byte{}, []byte{0, 0xff}, []byte("abc")}},
		{"SELECT DATE '0001-01-01' AS lo, DATE '9999-12-31' AS hi, DATE '1970-01-01' AS epoch", []any{
			dbimp.Date{Year: 1, Month: 1, Day: 1}, dbimp.Date{Year: 9999, Month: 12, Day: 31}, dbimp.Date{Year: 1970, Month: 1, Day: 1}}},
		{"SELECT CAST('f47ac10b-58cc-4372-a567-0e02b2c3d479' AS UUID) AS u, CAST(NULL AS UUID) AS n", []any{uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"), nil}},
		{"SELECT INTERVAL 1 DAY AS d, INTERVAL -5 MONTH AS neg, MAKE_INTERVAL(1, 2, 3, 4, 5, 6) AS made", []any{
			dbimp.Interval{Days: 1}, dbimp.Interval{Months: -5}, dbimp.Interval{Months: 14, Days: 3, Nanoseconds: (4*3600 + 5*60 + 6) * 1e9}}},
		{"SELECT NULL AS v", []any{nil}},
		{"SELECT [1, 2, 3] AS a, ARRAY<INT64>[] AS empty, [CAST(NULL AS INT64), 1] AS withnull, CAST(NULL AS ARRAY<INT64>) AS nullarray", []any{
			[]any{int64(1), int64(2), int64(3)}, []any{}, []any{nil, int64(1)}, nil}},
		{"SELECT ARRAY(SELECT AS STRUCT 1 AS a, 'x' AS b UNION ALL SELECT AS STRUCT 2, 'y') AS s", []any{
			[]any{[]any{int64(1), "x"}, []any{int64(2), "y"}}}},
	} {
		_, got := readRows(t, db, tt.query)
		if len(got) != 1 {
			t.Errorf("%s: read %d rows, want 1", tt.query, len(got))
			continue
		}
		for i := range tt.want {
			if !equalValue(got[0][i], tt.want[i]) {
				t.Errorf("%s: column %d is %#v, want %#v", tt.query, i, got[0][i], tt.want[i])
			}
		}
	}
}

// TestReplayJSONValues holds D191 item 9: a JSON column is a decoded value. The
// number of 20 digits stays exact, and the text null is nil, as a SQL NULL is.
func TestReplayJSONValues(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	_, got := readRows(t, db, "SELECT JSON '{\"a\":[1,2,{\"b\":null}]}' AS obj, JSON 'null' AS jnull, JSON '1' AS num, JSON '\"s\"' AS str, CAST(NULL AS JSON) AS sqlnull, JSON '12345678901234567890' AS big, JSON '1.0' AS dec")
	if len(got) != 1 {
		t.Fatalf("read %d rows, want 1", len(got))
	}
	want := []any{map[string]any{"a": []any{int64(1), int64(2), map[string]any{"b": nil}}}, nil, int64(1), "s", nil}
	for i := range want {
		if !equalValue(got[0][i], want[i]) {
			t.Errorf("column %d is %#v, want %#v", i, got[0][i], want[i])
		}
	}
	if d, ok := got[0][5].(*apd.Decimal); !ok || d.String() != "12345678901234567890" {
		t.Errorf("the number of 20 digits is %#v, want a decimal with the same digits", got[0][5])
	}
}

// TestReplayChunkedValues holds D191 item 11 with the recorded streams in which
// the server splits a value across messages: the pieces join as text before the
// base64 decode, and a value that the server split keeps its row.
func TestReplayChunkedValues(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)

	// 5000 rows of a number and a string of 200 characters. The first message
	// ends in the middle of the string of row 4905, and the second message
	// carries the 5 last characters.
	_, got := readRows(t, db, "SELECT x, REPEAT('y', 200) AS pad FROM UNNEST(GENERATE_ARRAY(1, 5000)) AS x ORDER BY x")
	if len(got) != 5000 {
		t.Fatalf("read %d rows, want 5000", len(got))
	}
	pad := strings.Repeat("y", 200)
	for i, row := range got {
		if row[0] != int64(i+1) || row[1] != pad {
			t.Fatalf("row %d is %v and %#v, want %d and a string of 200 characters", i, row[0], row[1], i+1)
		}
	}

	// A string of 3,000,000 characters in three messages.
	_, got = readRows(t, db, "SELECT ARRAY_TO_STRING(ARRAY(SELECT REPEAT('x', 1000000) FROM UNNEST(GENERATE_ARRAY(1, 3))), '') AS big")
	if s, ok := got[0][0].(string); !ok || len(s) != 3000000 || strings.Trim(s, "x") != "" {
		t.Errorf("the big string has %d characters, want 3000000 of x", len(s))
	}

	// A BYTES value of 2,000,000 bytes, which is 2,666,668 characters of
	// base64, in three messages. The first piece is not a multiple of four, so a
	// driver that decodes each piece alone fails.
	_, got = readRows(t, db, "SELECT id, y FROM dbimp_t_big WHERE id = 101")
	if b, ok := got[0][1].([]byte); !ok || len(b) != 2000000 {
		t.Errorf("the BYTES value has %d bytes, want 2000000", len(b))
	}

	// Twelve rows of a number and a string of 1,000,000 characters, in twelve
	// messages that hold 4, 3, 3, 3, 1, 4, 3, 3, 3, 1, 4 and 1 values.
	_, got = readRows(t, db, "SELECT id, v FROM dbimp_t_big WHERE id <= 12 ORDER BY id")
	if len(got) != 12 {
		t.Fatalf("read %d rows, want 12", len(got))
	}
	for i, row := range got {
		if s, ok := row[1].(string); row[0] != int64(i+1) || !ok || len(s) != 1000000 {
			t.Errorf("row %d is %v and a string of %d characters, want %d and 1000000", i, row[0], len(s), i+1)
		}
	}
}

// TestReplayErrorsBeforeRows holds D21 and D191 item 3: an error of HTTP 400
// comes from QueryContext, before any row, as an *Error with the status and the
// message of the server, and it does not wrap dbimp.ErrIncomplete.
func TestReplayErrorsBeforeRows(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	for _, tt := range []struct {
		query  string
		status string
		http   int
		text   string
	}{
		{"SELEC 1", "INVALID_ARGUMENT", 400, "Syntax error"},
		{"SELECT * FROM dbimp_t_nosuch", "INVALID_ARGUMENT", 400, "Table not found: dbimp_t_nosuch"},
		{"SELECT 1 / 0 AS v", "OUT_OF_RANGE", 400, "division by zero"},
		{"SELECT x, 1 / (x - 5000) AS v FROM UNNEST(GENERATE_ARRAY(1, 6000)) AS x ORDER BY x", "OUT_OF_RANGE", 400, "division by zero"},
	} {
		err := failure(t, db, tt.query)
		if err == nil {
			t.Errorf("%s: no error", tt.query)
			continue
		}
		serr, ok := errors.AsType[*Error](err)
		if !ok {
			t.Errorf("%s: the error is %v, want an *Error", tt.query, err)
			continue
		}
		if serr.Status != tt.status || serr.HTTPStatus != tt.http || !strings.Contains(serr.Message, tt.text) {
			t.Errorf("%s: the error is %+v, want %s, HTTP %d and %q", tt.query, *serr, tt.status, tt.http, tt.text)
		}
		if errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: an error before any row wraps dbimp.ErrIncomplete", tt.query)
		}
	}
}

// TestReplayMessageHasLineBreaks holds that the message of an error has real
// line breaks, because the member message holds a backslash and an n, and the
// detail LocalizedMessage holds the line break (docs/SPANNER.md, "Errors").
func TestReplayMessageHasLineBreaks(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	err := failure(t, db, "SELEC 1")
	serr, ok := errors.AsType[*Error](err)
	if !ok {
		t.Fatalf("the error is %v, want an *Error", err)
	}
	if !strings.Contains(serr.Message, "\n") || strings.Contains(serr.Message, `\n`) {
		t.Errorf("the message is %q, want real line breaks and no backslash", serr.Message)
	}
}

// TestReplayErrorAfterRows holds D191 item 11: the server answers HTTP 200,
// sends rows, and then an element that holds error. The last message before the
// error held 5999 values and chunkedValue, so the row of its last values was not
// complete. The driver returns the 1999 rows that were, drops the row that was
// not, and ends with the error, which wraps dbimp.ErrIncomplete (D107).
func TestReplayErrorAfterRows(t *testing.T) {
	t.Parallel()
	db, _, _ := replayDB(t)
	rows, err := db.QueryContext(t.Context(), "SELECT x, REPEAT('y', 500) AS pad, 1 / (x - 3500) AS v FROM UNNEST(GENERATE_ARRAY(1, 4000)) AS x")
	if err != nil {
		t.Fatalf("the error after the rows came from the query: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var (
			x   int64
			pad string
			v   float64
		)
		if err := rows.Scan(&x, &pad, &v); err != nil {
			t.Fatal(err)
		}
		if n++; x != int64(n) || len(pad) != 500 {
			t.Fatalf("row %d is %d and %d characters", n, x, len(pad))
		}
	}
	if n != 1999 {
		t.Errorf("read %d rows, want 1999", n)
	}
	err = rows.Err()
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.Status != "OUT_OF_RANGE" || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want an *Error OUT_OF_RANGE that wraps dbimp.ErrIncomplete", err)
	}
}

// TestReplayLostSession holds D191 item 3: a statement whose session the server
// does not know answers HTTP 404 NOT_FOUND, with the type of the session. The
// connector drops the session, the error wraps driver.ErrBadConn, so database/sql
// sends the statement again, and the retry makes a new session and runs.
func TestReplayLostSession(t *testing.T) {
	t.Parallel()
	db, s, c := replayDB(t)
	gone := file(t, 376).Request.Path
	gone = strings.TrimSuffix(strings.TrimPrefix(gone, "/v1/"), ":executeStreamingSql")
	c.sessions[target{database: testDatabase}] = gone
	var n int64
	if err := db.QueryRowContext(t.Context(), "SELECT 1").Scan(&n); err != nil {
		t.Fatalf("the statement after a lost session: %v", err)
	}
	want := []string{"POST executeStreamingSql", "GET dbimp_test", "POST sessions", "POST executeStreamingSql"}
	if got := s.requests(); !equalStrings(got, want) {
		t.Errorf("the requests are %q, want %q", got, want)
	}
	if c.sessions[target{database: testDatabase}] == gone || c.sessions[target{database: testDatabase}] == "" {
		t.Errorf("the connector holds the session %q, want a new one", c.sessions[target{database: testDatabase}])
	}
}

func equalStrings(a, b []string) bool {
	return reflect.DeepEqual(a, b)
}
