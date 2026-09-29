package pinot_test

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/pinot"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/pinot/. Each one decodes a real answer through the driver.

const testdata = "../testdata/pinot"

// releases are the releases that step 6 recorded.
var releases = []string{"pinot-1.4.0", "pinot-1.5.1"}

// canon returns the canonical form of the body of POST /query/sql. Of the
// query options, it drops the id of the query, which the driver chooses each
// time, and useMultistageEngine=false, which the recordings of the
// single-stage engine left out, and it sorts the rest.
func canon(b []byte) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	if s, ok := m["queryOptions"].(string); ok {
		opts := slices.DeleteFunc(strings.Split(s, ";"), func(o string) bool {
			return strings.HasPrefix(o, "clientQueryId=") || o == "useMultistageEngine=false"
		})
		slices.Sort(opts)
		m["queryOptions"] = strings.Join(opts, ";")
	}
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	return string(out)
}

// replayOnly opens the driver against the recorded exchanges of release for
// which keep is true, or every one for a nil keep. The first exchange that
// matches answers, and the administrator comes first in each recording.
func replayOnly(t *testing.T, release, query string, keep func(*dbimptest.Exchange) bool) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		if r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path || r.URL.RawQuery != ex.Request.Query {
			return false
		}
		return (keep == nil || keep(ex)) && canon(body) == canon(ex.Request.Content())
	})
	db, err := sql.Open(pinot.Name, strings.Replace(srv.URL, "http://", "pinot://admin:secret@", 1)+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// replay opens the driver against every recorded exchange of release.
func replay(t *testing.T, release string) *sql.DB {
	t.Helper()
	return replayOnly(t, release, "", nil)
}

// readAll runs query, and reads its columns and every row into *any.
func readAll(t *testing.T, db *sql.DB, query string, args ...any) ([]string, [][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
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

// serverError returns err as a *pinot.Error, and fails the test if it is
// not one.
func serverError(t *testing.T, err error) *pinot.Error {
	t.Helper()
	e, ok := errors.AsType[*pinot.Error](err)
	if !ok {
		t.Fatalf("the error is %v (%T), want a *pinot.Error", err, err)
	}
	return e
}

func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func utc(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

// equal compares two rows, and a decimal by its value.
func equal(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		g, gok := got[i].(*apd.Decimal)
		w, wok := want[i].(*apd.Decimal)
		switch {
		case gok && wok:
			if g.Cmp(w) != 0 {
				return false
			}
		case !reflect.DeepEqual(got[i], want[i]):
			return false
		}
	}
	return true
}

// storedTypes is the statement of step 6 that reads every column of the
// table of every type.
const storedTypes = "SELECT id, i, l, f, d, bd, b, s, j, by, ia, la, fa, da, boa, sa, ta, m, ts FROM dbimp_types ORDER BY id"

// TestReplayTypes holds D130 for each type, as the table of every type
// stored it: a NULL is nil, an empty array reads as NULL, and a JSON column is
// text on the multi-stage engine, which names its type STRING (measured).
func TestReplayTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		for _, engine := range []string{"multi", "single"} {
			db := replayOnly(t, release, "?engine="+engine, nil)
			cols, got, err := readAll(t, db, storedTypes)
			if err != nil {
				t.Fatalf("%s %s: %v", release, engine, err)
			}
			if want := strings.Split("id i l f d bd b s j by ia la fa da boa sa ta m ts", " "); !slices.Equal(cols, want) {
				t.Errorf("%s %s: the columns are %q, want %q", release, engine, cols, want)
			}
			var j1, j2 any = `{"k":[1,null,"s"]}`, "[]"
			if engine == "single" {
				j1, j2 = map[string]any{"k": []any{int64(1), nil, "s"}}, []any{}
			}
			want := [][]any{
				{int64(1), int64(-2147483648), int64(math.MaxInt64), 1.5, 0.1, dec(t, "12345678901234567890.0123456789"), true,
					"é'\"\\ x", j1, []byte{0x00, 0xff}, []any{int64(1), int64(2), int64(3)},
					[]any{int64(math.MinInt64), int64(math.MaxInt64)}, []any{1.5, -0.25}, []any{0.1, math.MaxFloat64},
					[]any{true, false}, []any{"x", "y"}, []any{utc(1700000000123)}, map[string]any{"a": int64(1), "b": int64(2)},
					utc(1700000000123)},
				{int64(2), int64(2147483647), int64(math.MinInt64), -3.4e38, math.MaxFloat64, dec(t, "-1"), false, "", j2, []byte{},
					nil, nil, nil, nil, nil, nil, nil, map[string]any{}, utc(1650000000000)},
				{int64(3), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, utc(1600000000000)},
			}
			if len(got) != len(want) {
				t.Fatalf("%s %s: %d rows, want %d", release, engine, len(got), len(want))
			}
			for i := range want {
				if !equal(got[i], want[i]) {
					t.Errorf("%s %s: row %d is\n%#v\nwant\n%#v", release, engine, i+1, got[i], want[i])
				}
			}
		}
	}
}

// columnTypes returns the types of the columns of query.
func columnTypes(t *testing.T, db *sql.DB, query string) []*sql.ColumnType {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cts, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cts
}

// TestReplayColumnTypes holds that the type of each column is the one that
// dataSchema names, with the scan type of D130.
func TestReplayColumnTypes(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		cts := columnTypes(t, replay(t, release), storedTypes)
		var names, scans []string
		for _, ct := range cts {
			names = append(names, ct.DatabaseTypeName())
			scans = append(scans, ct.ScanType().String())
			if nullable, ok := ct.Nullable(); !nullable || !ok {
				t.Errorf("%s: the column %s cannot be NULL, want every column nullable (D130)", release, ct.Name())
			}
		}
		wantNames := strings.Split("INT INT LONG FLOAT DOUBLE BIG_DECIMAL BOOLEAN STRING STRING BYTES INT_ARRAY LONG_ARRAY FLOAT_ARRAY DOUBLE_ARRAY BOOLEAN_ARRAY STRING_ARRAY TIMESTAMP_ARRAY MAP TIMESTAMP", " ")
		array := "[]interface {}"
		wantScans := []string{"int64", "int64", "int64", "float64", "float64", "*apd.Decimal", "bool", "string", "string", "[]uint8",
			array, array, array, array, array, array, array, "map[string]interface {}", "time.Time"}
		if !slices.Equal(names, wantNames) {
			t.Errorf("%s: the types are %q, want %q", release, names, wantNames)
		}
		if !slices.Equal(scans, wantScans) {
			t.Errorf("%s: the scan types are %q, want %q", release, scans, wantScans)
		}
	}
}

// TestReplayLiterals holds D132: the driver writes each argument as the
// literal that the recording sent, and the server matched the stored row.
func TestReplayLiterals(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		var id int64
		err := db.QueryRowContext(t.Context(),
			"SELECT id FROM dbimp_types WHERE s = ? AND bd = ? AND by = ? AND ts = ? AND l = ? AND d = ? AND b = ?",
			"é'\"\\ x", dec(t, "12345678901234567890.0123456789"), []byte{0x00, 0xff}, utc(1700000000123),
			int64(math.MaxInt64), 0.1, true).Scan(&id)
		if err != nil || id != 1 {
			t.Errorf("%s: the literals selected %d, %v, want the row 1", release, id, err)
		}
	}
}

// TestReplayLiteralsOfSelect holds the types of the literals of a SELECT,
// and that a NULL literal, of the type UNKNOWN, is nil.
func TestReplayLiteralsOfSelect(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		_, got, err := readAll(t, db, "SELECT CAST('NaN' AS DOUBLE) AS nan, CAST('Infinity' AS DOUBLE) AS inf, CAST('-Infinity' AS DOUBLE) AS ninf FROM baseballStats LIMIT 1")
		if err != nil {
			t.Fatal(err)
		}
		f := make([]float64, 3)
		for i := range f {
			f[i], _ = got[0][i].(float64)
		}
		if !math.IsNaN(f[0]) || !math.IsInf(f[1], 1) || !math.IsInf(f[2], -1) {
			t.Errorf("%s: NaN and the infinities are %v", release, got[0])
		}
		const literals = "SELECT CAST(2147483647 AS INT) AS i, CAST(-9223372036854775808 AS LONG) AS l, CAST(1.5 AS FLOAT) AS f, " +
			"CAST(0.1 AS DOUBLE) AS d, CAST('-12345678901234567890.01234567890123456789' AS BIG_DECIMAL) AS bd, " +
			"true AS b, 'x' AS s, CAST(1700000000123 AS TIMESTAMP) AS ts, hexToBytes('00ff') AS by, " +
			"NULL AS n, ARRAY[1, 2] AS ia, ARRAY['x', 'y'] AS sa FROM baseballStats LIMIT 1"
		if name := columnTypes(t, db, literals)[9].DatabaseTypeName(); name != "UNKNOWN" {
			t.Errorf("%s: the type of NULL is %s, want UNKNOWN", release, name)
		}
		_, got, err = readAll(t, db, literals)
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: the literals gave %v, %v", release, got, err)
		}
		vals := got[0]
		// The cast to TIMESTAMP loses the milliseconds (measured).
		want := []any{int64(2147483647), int64(math.MinInt64), 1.5, 0.1, dec(t, "-12345678901234567890.01234567890123456789"),
			true, "x", utc(1700000000000), []byte{0x00, 0xff}, nil, []any{int64(1), int64(2)}, []any{"x", "y"}}
		if !equal(vals, want) {
			t.Errorf("%s: the literals are\n%#v\nwant\n%#v", release, vals, want)
		}
	}
}

// TestReplayLimit holds D131: the multi-stage engine returns every row of a
// query with no LIMIT, and the single-stage engine returns 10.
func TestReplayLimit(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		for engine, n := range map[string]int{"multi": 143, "single": 10} {
			db := replayOnly(t, release, "?engine="+engine, nil)
			_, got, err := readAll(t, db, "SELECT DISTINCT yearID FROM baseballStats ORDER BY yearID")
			if err != nil || len(got) != n {
				t.Errorf("%s %s: %d rows, %v, want %d", release, engine, len(got), err, n)
			}
		}
		// The engine of one statement.
		db := replay(t, release)
		_, got, err := readAll(t, db, "SELECT DISTINCT yearID FROM baseballStats ORDER BY yearID", pinot.WithEngine(pinot.EngineSingle))
		if err != nil || len(got) != 10 {
			t.Errorf("%s: WithEngine gave %d rows, %v, want 10", release, len(got), err)
		}
	}
}

// TestReplayErrors holds that an error of the query, which arrives with HTTP
// 200, is the error of the query, with its code and its HTTP status.
func TestReplayErrors(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			code  int
		}{
			{"SELECT * FROM dbimp_none", 190},
			{"SELEC 1", 150},
			{"SELECT playerName FROM baseballStats WHERE playerID = ? LIMIT 1", 450},
			{"SELECT id, JSON_EXTRACT_SCALAR(j, '$.k[0]', 'INT') AS x FROM dbimp_types", 200},
			{"SELECT version() FROM baseballStats LIMIT 1", 700},
			{"SELECT 1 FROM baseballStats LIMIT 1; SELECT 2 FROM baseballStats LIMIT 1", 150},
			{"INSERT INTO dbimp_types (id, ts) VALUES (9, 1700000000000)", 150},
			{"CREATE TABLE dbimp_new (k INT)", 150},
			{"BEGIN", 150},
		} {
			_, _, err := readAll(t, db, tt.query)
			e := serverError(t, err)
			if e.Code != tt.code || e.HTTPStatus != http.StatusOK || errors.Is(err, dbimp.ErrIncomplete) {
				t.Errorf("%s: %q gave %v, HTTP %d, want the code %d with HTTP 200, before any row", release, tt.query, err, e.HTTPStatus, tt.code)
			}
		}
		// Exec runs a write too, and gets the same refusal (D128).
		if _, err := db.ExecContext(t.Context(), "UPDATE dbimp_types SET i = 1 WHERE id = 1"); serverError(t, err).Code != 150 && serverError(t, err).Code != 700 {
			t.Errorf("%s: an UPDATE gave %v", release, err)
		}
	}
}

// TestReplayPartial holds D133: rows that partialResult follows reach the
// caller, and then the error wraps ErrPartial and dbimp.ErrIncomplete.
func TestReplayPartial(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		_, got, err := readAll(t, db, "SELECT count(*) FROM baseballStats a JOIN baseballStats b ON a.yearID = b.yearID",
			pinot.WithParameter("queryOptions", "useMultistageEngine=true;enableNullHandling=true;maxRowsInJoin=1000;joinOverflowMode=BREAK"))
		if len(got) != 1 || !errors.Is(err, pinot.ErrPartial) || !errors.Is(err, dbimp.ErrIncomplete) {
			t.Errorf("%s: %d rows and %v, want 1 row and then ErrPartial and ErrIncomplete", release, len(got), err)
		}
		_, _, err = readAll(t, db, "SELECT count(*) FROM baseballStats a JOIN baseballStats b ON a.yearID = b.yearID",
			pinot.WithParameter("queryOptions", "useMultistageEngine=true;enableNullHandling=true;maxRowsInJoin=1000"))
		if e := serverError(t, err); e.Code != 245 {
			t.Errorf("%s: a join over maxRowsInJoin gave %v, want 245", release, err)
		}
	}
}

// TestReplayPrincipals holds that a wrong password is HTTP 401, and that a
// table that the ordinary user cannot read is HTTP 403, each an error before
// any row.
func TestReplayPrincipals(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusUnauthorized
		})
		_, _, err := readAll(t, db, "SELECT playerName FROM baseballStats LIMIT 1")
		if e := serverError(t, err); e.HTTPStatus != http.StatusUnauthorized {
			t.Errorf("%s: a wrong password gave %v", release, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: the error %v holds the password", release, err)
		}
		db = replayOnly(t, release, "", func(ex *dbimptest.Exchange) bool {
			return ex.Response.Status == http.StatusForbidden
		})
		_, _, err = readAll(t, db, "SELECT count(*) FROM dbimp_idx")
		if e := serverError(t, err); e.HTTPStatus != http.StatusForbidden || !strings.Contains(e.Message, "dbimp_idx") {
			t.Errorf("%s: the ordinary user gave %v, want HTTP 403 for dbimp_idx", release, err)
		}
	}
}

// TestReplayStatements holds that comments reach the server, and that the
// parser for placeholders leaves them and the literals alone.
func TestReplayStatements(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		if err := db.PingContext(t.Context()); err != nil {
			t.Errorf("%s: Ping: %v", release, err)
		}
		_, got, err := readAll(t, db, "-- a comment\nSELECT /* a comment */ playerName FROM baseballStats ORDER BY playerID LIMIT 1")
		if err != nil || len(got) != 1 || got[0][0] != "David Allan" {
			t.Errorf("%s: comments gave %v, %v", release, got, err)
		}
		_, got, err = readAll(t, db, "SET timeoutMs = 5000; SELECT count(*) FROM baseballStats")
		if err != nil || len(got) != 1 || got[0][0] != int64(97889) {
			t.Errorf("%s: SET gave %v, %v", release, got, err)
		}
		cols, got, err := readAll(t, db, "SELECT yearID, yearID FROM baseballStats LIMIT 1")
		if err != nil || !slices.Equal(cols, []string{"yearID", "yearID"}) || len(got) != 1 {
			t.Errorf("%s: a column named twice gave %q, %v, %v", release, cols, got, err)
		}
		cols, got, err = readAll(t, db, "SELECT playerName, yearID FROM baseballStats WHERE yearID < 0")
		if err != nil || !slices.Equal(cols, []string{"playerName", "yearID"}) || len(got) != 0 {
			t.Errorf("%s: a result with no rows gave %q, %v, %v", release, cols, got, err)
		}
		if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
			t.Errorf("%s: BeginTx gave %v, want dbimp.ErrNotSupported (D133)", release, err)
		}
	}
}

// single runs a statement on the single-stage engine.
var single = []any{pinot.WithEngine(pinot.EngineSingle)}

// TestReplayFeatures holds the features of step 5a that a query shows, on
// the recorded answers.
func TestReplayFeatures(t *testing.T) {
	t.Parallel()
	for _, release := range releases {
		db := replay(t, release)
		for _, tt := range []struct {
			query string
			want  [][]any
			opts  []any
		}{
			{"SELECT /*+ joinOptions(join_strategy='lookup') */ i.k, d.label FROM dbimp_idx i JOIN dbimp_dim d ON i.s = d.s WHERE i.k < 3 ORDER BY i.k", [][]any{{int64(1), "one"}}, nil},
			{"SELECT k, SUM(n) OVER (ORDER BY k) AS c FROM dbimp_idx WHERE k < 4 ORDER BY k", [][]any{{int64(1), int64(10)}, {int64(2), int64(30)}, {int64(3), int64(60)}}, nil},
			{"SELECT k, JSON_EXTRACT_SCALAR(j, '$.a', 'INT', -1) AS a FROM dbimp_idx WHERE k < 4 ORDER BY k", [][]any{{int64(1), int64(1)}, {int64(2), int64(2)}, {int64(3), int64(0)}}, nil},
			// Step 6 read the indexes on the single-stage engine.
			{"SELECT count(*) FROM dbimp_idx WHERE TEXT_MATCH(t, 'fox')", [][]any{{int64(25)}}, single},
			{"SELECT count(*) FROM dbimp_idx WHERE ts >= 1702000000000", [][]any{{int64(27)}}, nil},
		} {
			_, got, err := readAll(t, db, tt.query, tt.opts...)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: %q gave %v, %v, want %v", release, tt.query, got, err, tt.want)
			}
		}
	}
}
