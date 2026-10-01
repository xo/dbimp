package avatica //nolint:testpackage // The replay opens the driver through the fake transport of fake_test.go.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/avatica/. Each one decodes a real answer through the driver.

const (
	testdata = "../testdata/avatica"
	hsqldb   = "avatica-1.28.0"
	phoenix  = "phoenix-2.0-5.0"
)

// fields returns the members of the body b of a call that say which
// recorded call it is. The ids of the connection and of the statement, and
// the rep of each parameter, differ from those that step 6 sent, so they are
// dropped.
func fields(b []byte) map[string]any {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	out := map[string]any{"request": m["request"]}
	switch m["request"] {
	case "prepareAndExecute":
		for _, k := range []string{"sql", "maxRowCount", "maxRowsInFirstFrame"} {
			out[k] = m[k]
		}
	case "prepare":
		out["sql"] = m["sql"]
	case "execute":
		h, _ := m["statementHandle"].(map[string]any)
		sig, _ := h["signature"].(map[string]any)
		out["sql"] = sig["sql"]
		var vals []any
		ps, _ := m["parameterValues"].([]any)
		for _, p := range ps {
			pm, _ := p.(map[string]any)
			vals = append(vals, pm["value"])
		}
		out["values"] = vals
	case "fetch":
		out["offset"], out["fetchMaxRowCount"] = m["offset"], m["fetchMaxRowCount"]
	case "connectionSync":
		props, _ := m["connProps"].(map[string]any)
		delete(props, "dirty")
		out["connProps"] = props
	}
	return out
}

// sameCall matches a request of the driver with a recorded one by fields,
// and a call that sets up a connection or a statement with one of the
// connection of the recording that the server took.
func sameCall(_ *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	got, want := fields(body), fields(ex.Request.Content())
	switch got["request"] {
	case "openConnection", "createStatement", "closeConnection":
		return got["request"] == want["request"] && ex.Response.Status == http.StatusOK && strings.Contains(string(ex.Request.Content()), `"dbimp-rec"`)
	}
	return reflect.DeepEqual(got, want)
}

// replay opens the driver against the recorded exchanges of release. The
// recordings hold no closeStatement, so the fake transport answers it.
func replay(t *testing.T, release string, match dbimptest.Match) *sql.DB {
	t.Helper()
	if match == nil {
		match = sameCall
	}
	srv := dbimptest.ReplayRelease(t, testdata, release, match)
	return openLocal(t, srv.URL, map[string]string{"closeStatement": setupAnswers["closeStatement"]})
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, err
		}
		out = append(out, vals)
	}
	return cols, out, rows.Err()
}

// decimal returns the decimal s.
func decimal(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// equal compares two rows, and a decimal by its value.
func equal(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		g, gok := got[i].(*apd.Decimal)
		w, wok := want[i].(*apd.Decimal)
		if gok && wok {
			if g.Cmp(w) != 0 {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(got[i], want[i]) {
			return false
		}
	}
	return true
}

// check compares the rows got with want.
func check(t *testing.T, got, want [][]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if !equal(got[i], want[i]) {
			t.Errorf("row %d is\n%#v\nwant\n%#v", i+1, got[i], want[i])
		}
	}
}

// TestReplayTypesHSQLDB holds D155 for each type of HSQLDB, as the table of
// every type stored it (recorded: "every type"). The server gave
// "0 00:00:00.000001" for the literal '-0 00:00:00.000001', so the sign of a
// day of 0 was lost before the driver read it.
func TestReplayTypesHSQLDB(t *testing.T) {
	t.Parallel()
	cols, got, err := readAll(t.Context(), t, replay(t, hsqldb, nil), "SELECT ID, TI, SI, N, B, D, F, R, BO, C, V, BIN, DT, TM, TS, IV, A, U FROM PUBLIC.DBIMP_TYPES ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Fields("ID TI SI N B D F R BO C V BIN DT TM TS IV A U"); !slices.Equal(cols, want) {
		t.Errorf("the columns are %q, want %q", cols, want)
	}
	day := dbimp.Date{Year: 2026, Month: time.October, Day: 1}
	check(t, got, [][]any{
		{int64(1), int64(-128), int64(-32768), int64(math.MinInt32), int64(math.MinInt64), decimal(t, "1234567890123456789012345678.0123456789"),
			0.1, 1.5, true, "ab ", "é'\"\\ x", []byte{0, 0xff}, day, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56},
			dbimp.LocalDateTime{Date: day, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123000000}},
			dbimp.Interval{Days: 1, Nanoseconds: int64(2*time.Hour + 3*time.Minute + 4500*time.Millisecond)},
			[]any{int64(1), nil, int64(3)}, uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")},
		{int64(2), int64(127), int64(32767), int64(math.MaxInt32), int64(math.MaxInt64), decimal(t, "-9999999999999999999999999999.9999999999"),
			math.MaxFloat64, -3.4e38, false, "   ", "", []byte{}, dbimp.Date{Year: 0, Month: time.December, Day: 30}, dbimp.LocalTime{},
			dbimp.LocalDateTime{Date: dbimp.Date{Year: 9999, Month: time.December, Day: 31}, Time: dbimp.LocalTime{Hour: 23, Minute: 59, Second: 59, Nanosecond: 999000000}},
			dbimp.Interval{Nanoseconds: 1000}, []any{}, uuid.UUID{}},
		{int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
	})
}

// TestReplayTypesPhoenix holds D155 for each type of Phoenix (recorded:
// "every type"). Phoenix stores an empty string as NULL.
func TestReplayTypesPhoenix(t *testing.T) {
	t.Parallel()
	_, got, err := readAll(t.Context(), t, replay(t, phoenix, nil), "SELECT * FROM DBIMP_TYPES ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	day := dbimp.Date{Year: 2026, Month: time.October, Day: 1}
	epoch := dbimp.Date{Year: 1970, Month: time.January, Day: 1}
	check(t, got, [][]any{
		{int64(1), int64(-128), int64(-32768), int64(math.MinInt32), int64(math.MinInt64), int64(0), int64(0), decimal(t, "1234567890123456789012345678.0123456789"),
			1.5, 0.1, true, "ab", "�'\"\\ x", []byte("ab"), []byte("xyz"), day, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000},
			dbimp.LocalDateTime{Date: day, Time: dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 789000000}}, []any{int64(1), int64(2), int64(3)}},
		{int64(2), int64(127), int64(32767), int64(math.MaxInt32), int64(math.MaxInt64), int64(math.MaxInt32), int64(math.MaxInt64), decimal(t, "-9999999999999999999999999999.9999999999"),
			3.3999999521443642e38, math.MaxFloat64, false, nil, nil, []byte("zz"), nil, epoch, dbimp.LocalTime{}, dbimp.LocalDateTime{Date: epoch}, []any{int64(0)}},
		{int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil},
	})
}

// TestReplayInfinity holds D155: an infinity is the string "Infinity"
// (recorded: "an infinity").
func TestReplayInfinity(t *testing.T) {
	t.Parallel()
	_, got, err := readAll(t.Context(), t, replay(t, hsqldb, nil), "VALUES (CAST(1E308 AS DOUBLE) * 10)")
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], []any{math.Inf(1)}) {
		t.Errorf("the infinity gave %v %v, want +Inf", got, err)
	}
}

// TestReplayFrames holds D157: the driver reads the first frame, and fetches
// each next one from the offset after its last row (recorded: "a result of
// 250 rows, 100 in the first frame", "fetch the second frame" and "fetch the
// last frame").
func TestReplayFrames(t *testing.T) {
	t.Parallel()
	ctx := WithOptions(t.Context(), WithFrameSize(100))
	_, got, err := readAll(ctx, t, replay(t, hsqldb, nil), "SELECT V, 'padding padding padding padding' FROM UNNEST(SEQUENCE_ARRAY(1, 250, 1)) AS T(V)")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 250 {
		t.Fatalf("%d rows, want 250", len(got))
	}
	for i, r := range got {
		if r[0] != int64(i+1) {
			t.Fatalf("row %d holds %v, want %d", i+1, r[0], i+1)
		}
	}
}

// TestReplayErrors holds what the server answers for a statement that it
// cannot run: HTTP 500 with its message (recorded).
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db := replay(t, hsqldb, nil)
	for query, want := range map[string]string{
		"SELEC 1":                    "unexpected token: SELEC",
		"SELECT * FROM DBIMP_NOSUCH": "user lacks privilege or object not found: DBIMP_NOSUCH",
	} {
		_, err := db.ExecContext(t.Context(), query)
		e, ok := errors.AsType[*Error](err)
		if !ok || e.HTTPStatus != http.StatusInternalServerError || !strings.Contains(e.Message, want) || errors.Is(err, driver.ErrBadConn) {
			t.Errorf("%q gave %v, want HTTP 500 with %q", query, err, want)
		}
	}
}

// TestReplayErrorInRows holds what the server answers for an error in the
// second row of a frame of two: it fails the whole answer, and the driver
// reads no row (recorded: "an error inside the rows").
func TestReplayErrorInRows(t *testing.T) {
	t.Parallel()
	ctx := WithOptions(t.Context(), WithFrameSize(2))
	_, got, err := readAll(ctx, t, replay(t, hsqldb, nil), "SELECT CASE WHEN V < 4 THEN V ELSE 1 / (V - 4) END FROM UNNEST(SEQUENCE_ARRAY(1, 5, 1)) AS T(V)")
	e, ok := errors.AsType[*Error](err)
	if len(got) != 0 || !ok || !strings.Contains(e.Message, "division by zero") || errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("read %d rows and %v, want no row and the division by zero", len(got), err)
	}
}

// TestReplayUnknownConnection holds hard rule 5: the error of a connection
// that the server does not know is driver.ErrBadConn, because the server ran
// nothing (recorded: "an unknown connection").
func TestReplayUnknownConnection(t *testing.T) {
	t.Parallel()
	db := replay(t, hsqldb, func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		if fields(body)["request"] == "createStatement" {
			return strings.Contains(string(ex.Request.Content()), `"dbimp-nosuch"`)
		}
		return sameCall(r, body, ex)
	})
	_, err := db.ExecContext(t.Context(), "VALUES (1)")
	if e, ok := errors.AsType[*Error](err); !ok || e.Exception != noSuchConnection || !errors.Is(err, driver.ErrBadConn) {
		t.Errorf("the unknown connection gave %v, want %s and driver.ErrBadConn", err, noSuchConnection)
	}
}

// TestReplayExec holds that Exec gives updateCount as the count of rows
// (recorded: "crud: insert", "crud: update" and "crud: delete").
func TestReplayExec(t *testing.T) {
	t.Parallel()
	db := replay(t, hsqldb, nil)
	for _, tt := range []struct {
		query string
		n     int64
	}{
		{"INSERT INTO PUBLIC.DBIMP_CRUD VALUES (1, 'a'), (2, 'b'), (3, 'c')", 3},
		{"UPDATE PUBLIC.DBIMP_CRUD SET V = 'B' WHERE K = 2", 1},
		{"DELETE FROM PUBLIC.DBIMP_CRUD WHERE K = 3", 1},
		{"CREATE INDEX DBIMP_S_IDX ON PUBLIC.DBIMP_S_PARENT (N)", 0},
	} {
		res, err := db.ExecContext(t.Context(), tt.query)
		if err != nil {
			t.Errorf("%s: %v", tt.query, err)
			continue
		}
		if n, err := res.RowsAffected(); n != tt.n || err != nil {
			t.Errorf("%s gave %d rows and %v, want %d", tt.query, n, err, tt.n)
		}
		if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("LastInsertId of %s gave %v, want dbimp.ErrNotSupported", tt.query, err)
		}
	}
}

// TestReplayParameters holds D158: the driver prepares the statement, and
// executes it with a TypedValue for each argument (recorded: "prepare a
// statement with parameters" and "execute with typed parameters"). The
// recording sent the decimal as a NUMBER, so this test sends a float64 of
// its value.
func TestReplayParameters(t *testing.T) {
	t.Parallel()
	day := dbimp.Date{Year: 2026, Month: time.October, Day: 1}
	_, got, err := readAll(t.Context(), t, replay(t, hsqldb, nil),
		"VALUES (CAST(? AS INTEGER), CAST(? AS VARCHAR(10)), CAST(? AS DOUBLE), CAST(? AS BIGINT), CAST(? AS DECIMAL(38,10)), CAST(? AS DATE), CAST(? AS TIMESTAMP(9)), CAST(? AS VARBINARY(4)), CAST(? AS BOOLEAN), CAST(? AS TIME(6)))",
		10, "é", 1.5, int64(math.MaxInt64), 1.2345678901234569e27, day,
		time.Date(2026, time.October, 1, 12, 28, 16, 123000000, time.UTC), []byte{0, 0xff}, true, dbimp.LocalTime{Hour: 12, Minute: 34, Second: 56, Nanosecond: 123000000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0][0] != int64(10) || got[0][1] != "é" || got[0][5] != day {
		t.Errorf("the statement gave %#v, want the arguments", got)
	}
}

// TestReplayParameterCount holds D158: a count of arguments that is not the
// count of the parameters is an error before execute, because the server
// binds a missing one as NULL.
func TestReplayParameterCount(t *testing.T) {
	t.Parallel()
	_, err := replay(t, hsqldb, nil).ExecContext(t.Context(),
		"VALUES (CAST(? AS INTEGER), CAST(? AS VARCHAR(10)), CAST(? AS DOUBLE), CAST(? AS BIGINT), CAST(? AS DECIMAL(38,10)), CAST(? AS DATE), CAST(? AS TIMESTAMP(9)), CAST(? AS VARBINARY(4)), CAST(? AS BOOLEAN), CAST(? AS TIME(6)))",
		10, "é")
	if !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("two arguments for ten parameters gave %v, want dbimp.ErrArguments", err)
	}
}

// TestReplayTransaction holds D159: BeginTx turns autoCommit off, Rollback
// sends rollback and turns it on again (recorded: "turn autocommit off",
// "rollback" and "turn autocommit on"). The read in the transaction and the
// read after it send one statement, so each statement matches the first
// exchange that no earlier statement matched.
func TestReplayTransaction(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	used := map[*dbimptest.Exchange]bool{}
	db := replay(t, hsqldb, func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		if fields(body)["request"] != "prepareAndExecute" {
			return sameCall(r, body, ex)
		}
		mu.Lock()
		defer mu.Unlock()
		if used[ex] || !sameCall(r, body, ex) {
			return false
		}
		used[ex] = true
		return true
	})
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO PUBLIC.DBIMP_TX VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	var in int64
	if err := tx.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM PUBLIC.DBIMP_TX").Scan(&in); err != nil || in != 1 {
		t.Errorf("in the transaction, the table holds %d rows and %v, want 1", in, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM PUBLIC.DBIMP_TX").Scan(&n); err != nil || n != 0 {
		t.Errorf("after the rollback, the table holds %d rows and %v, want 0", n, err)
	}
}
