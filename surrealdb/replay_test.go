package surrealdb_test

import (
	"database/sql"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
	"github.com/xo/dbimp/surrealdb"
)

// testdata holds the recorded exchanges of step 6.
const testdata = "../testdata/surrealdb"

// principalMatch is DefaultMatch that also tells the two principals apart,
// because only the ordinary user sends Surreal-Auth-Db (D51).
func principalMatch(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
	ordinary := r.Header.Get("Surreal-Auth-Db") != ""
	return ordinary == (ex.Request.Header.Get("Surreal-Auth-Db") != "") && dbimptest.DefaultMatch(r, body, ex)
}

// replayDB opens the driver against the recordings of release, with the
// query of a DSN.
func replayDB(t *testing.T, release, query string) *sql.DB {
	t.Helper()
	return replayWith(t, release, query, principalMatch)
}

func replayWith(t *testing.T, release, query string, match dbimptest.Match) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, match)
	db := openAt(t, srv.URL, query)
	t.Cleanup(func() { db.Close() })
	return db
}

// sets runs query and returns the columns and the rows of each result set,
// each value read into *any, and the error that ends them.
func sets(t *testing.T, db *sql.DB, query string, args ...any) ([][]string, [][][]any, error) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var cols [][]string
	var all [][][]any
	for {
		c, err := rows.Columns()
		if err != nil {
			return cols, all, err
		}
		cols = append(cols, c)
		var set [][]any
		for rows.Next() {
			row := make([]any, len(c))
			dest := make([]any, len(c))
			for i := range dest {
				dest[i] = &row[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return cols, append(all, set), err
			}
			set = append(set, row)
		}
		all = append(all, set)
		if err := rows.Err(); err != nil {
			return cols, all, err
		}
		if !rows.NextResultSet() {
			return cols, all, rows.Err()
		}
	}
}

func TestReplayColumnOrder(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{"surrealdb-2.7.0", "surrealdb-3.3.0"} {
		cols, rows, err := sets(t, replayDB(t, rel, ""), "SELECT b, a, c FROM [{a: 1, b: 2, c: 3}]")
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		// The server sorts the keys, and the driver keeps its order (D52).
		if !slices.Equal(cols[0], []string{"a", "b", "c"}) || !reflect.DeepEqual(rows[0], [][]any{{int64(1), int64(2), int64(3)}}) {
			t.Errorf("%s gave %q and %v, want the columns a, b and c", rel, cols, rows)
		}
	}
}

func TestReplayTypes(t *testing.T) {
	t.Parallel()
	const stmt = "RETURN [9223372036854775807, -9223372036854775808, 0.1, 1.5e300, " +
		"1.23456789012345678901234567890dec, 100dec, d'2026-09-27T10:00:00.123456789Z', 1h30m, 1y2w3d, " +
		"u'0192f1c4-3b5e-7a2c-9f00-000000000001', <bytes>'hi', dbimp_people:tobie, dbimp_people:['a', 1], " +
		"dbimp_people:{a: 1}, dbimp_people:123, <set>[3, 1, 2], (1.5, 2.5), 1..5, NONE, NULL, true, 's', type::table('dbimp_people')]"
	for _, rel := range []string{"surrealdb-2.7.0", "surrealdb-3.3.0"} {
		_, rows, err := sets(t, replayDB(t, rel, ""), stmt)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if len(rows[0]) != 23 {
			t.Fatalf("%s gave %d rows, want one for each of the 23 values", rel, len(rows[0]))
		}
		set := []any{int64(3), int64(1), int64(2)}
		if rel != "surrealdb-2.7.0" {
			set = []any{int64(1), int64(2), int64(3)}
		}
		for i, want := range []any{
			int64(9223372036854775807), int64(-9223372036854775808), 0.1, 1.5e300,
			"1.2345678901234567890123456789", "100",
			time.Date(2026, 9, 27, 10, 0, 0, 123456789, time.UTC), 90 * time.Minute, 382 * 24 * time.Hour,
			uuid.MustParse("0192f1c4-3b5e-7a2c-9f00-000000000001"), []byte("hi"),
			surrealdb.RecordID{Table: "dbimp_people", ID: "tobie"},
			surrealdb.RecordID{Table: "dbimp_people", ID: []any{"a", int64(1)}},
			surrealdb.RecordID{Table: "dbimp_people", ID: map[string]any{"a": int64(1)}},
			surrealdb.RecordID{Table: "dbimp_people", ID: int64(123)},
			set, map[string]any{"type": "Point", "coordinates": []any{1.5, 2.5}}, "1..5",
			nil, nil, true, "s", "dbimp_people",
		} {
			got := rows[0][i][0]
			if d, ok := got.(*apd.Decimal); ok {
				got = d.String()
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: value %d is %#v, want %#v", rel, i, got, want)
			}
		}
	}
}

func TestReplayTypesInJSON(t *testing.T) {
	t.Parallel()
	_, rows, err := sets(t, replayDB(t, "surrealdb-3.3.0", "?encoding=json"), "RETURN [9223372036854775807, -9223372036854775808, 0.1, 1.5e300, "+
		"1.23456789012345678901234567890dec, 100dec, d'2026-09-27T10:00:00.123456789Z', 1h30m, 1y2w3d, "+
		"u'0192f1c4-3b5e-7a2c-9f00-000000000001', <bytes>'hi', dbimp_people:tobie, dbimp_people:['a', 1], "+
		"dbimp_people:{a: 1}, dbimp_people:123, <set>[3, 1, 2], (1.5, 2.5), 1..5, NONE, NULL, true, 's', type::table('dbimp_people')]")
	if err != nil {
		t.Fatal(err)
	}
	// JSON keeps no type of SurrealDB, so each one is a string (D49).
	for i, want := range map[int]any{
		4:  "1.2345678901234567890123456789",
		6:  "2026-09-27T10:00:00.123456789Z",
		7:  "1h30m",
		9:  "0192f1c4-3b5e-7a2c-9f00-000000000001",
		10: []any{int64(104), int64(105)},
		11: "dbimp_people:tobie",
		18: nil,
	} {
		if got := rows[0][i][0]; !reflect.DeepEqual(got, want) {
			t.Errorf("value %d in JSON is %#v, want %#v", i, got, want)
		}
	}
}

func TestReplayScanDestinations(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "surrealdb-3.3.0", "")
	rows, err := db.QueryContext(t.Context(), "RETURN [9223372036854775807, -9223372036854775808, 0.1, 1.5e300, "+
		"1.23456789012345678901234567890dec, 100dec, d'2026-09-27T10:00:00.123456789Z', 1h30m, 1y2w3d, "+
		"u'0192f1c4-3b5e-7a2c-9f00-000000000001', <bytes>'hi', dbimp_people:tobie, dbimp_people:['a', 1], "+
		"dbimp_people:{a: 1}, dbimp_people:123, <set>[3, 1, 2], (1.5, 2.5), 1..5, NONE, NULL, true, 's', type::table('dbimp_people')]")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for i := 0; rows.Next(); i++ {
		switch i {
		case 4:
			var d apd.Decimal
			if err := rows.Scan(&d); err != nil {
				t.Fatal(err)
			}
			got = append(got, d.String())
		case 6:
			var ts time.Time
			if err := rows.Scan(&ts); err != nil {
				t.Fatal(err)
			}
			got = append(got, ts.Format(time.RFC3339Nano))
		case 9:
			var u uuid.UUID
			if err := rows.Scan(&u); err != nil {
				t.Fatal(err)
			}
			got = append(got, u.String())
		case 18:
			var s sql.Null[string]
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			got = append(got, "valid="+map[bool]string{true: "true", false: "false"}[s.Valid])
		case 7, 11, 12:
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			got = append(got, s)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"1.2345678901234567890123456789", "2026-09-27T10:00:00.123456789Z", "1h30m", "0192f1c4-3b5e-7a2c-9f00-000000000001", "dbimp_people:tobie", "dbimp_people:['a', 1]", "valid=false"}
	if !slices.Equal(got, want) {
		t.Errorf("the scans gave %q, want %q", got, want)
	}
}

func TestReplayMissingField(t *testing.T) {
	t.Parallel()
	cols, rows, err := sets(t, replayDB(t, "surrealdb-3.3.0", ""), "SELECT a, b FROM [{a: 1}]")
	if err != nil {
		t.Fatal(err)
	}
	// The server sends NONE for b, which is nil (D52).
	if !slices.Equal(cols[0], []string{"a", "b"}) || !reflect.DeepEqual(rows[0], [][]any{{int64(1), nil}}) {
		t.Errorf("gave %q and %v, want a = 1 and b = nil", cols, rows)
	}
}

func TestReplayRecordsWithDifferentFields(t *testing.T) {
	t.Parallel()
	_, rows, err := sets(t, replayDB(t, "surrealdb-3.3.0", ""),
		"CREATE dbimp_people:o1 SET name = 'Ada', age = 36; CREATE dbimp_people:o2 SET name = 'Bo', city = 'Oslo'; "+
			"SELECT * FROM dbimp_people WHERE id IN [dbimp_people:o1, dbimp_people:o2] ORDER BY id; DELETE dbimp_people:o1, dbimp_people:o2")
	// The second record has city, which the first lacks (D52).
	if !errors.Is(err, dbimp.ErrExtraColumn) {
		t.Fatalf("the error is %v, want ErrExtraColumn", err)
	}
	if len(rows) != 3 || len(rows[2]) != 1 {
		t.Errorf("read %v before the error, want two sets and one row of the third", rows)
	}
}

func TestReplayResultSets(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{"surrealdb-2.7.0", "surrealdb-3.3.0"} {
		_, rows, err := sets(t, replayDB(t, rel, ""), "RETURN 1; THROW 'boom'; RETURN 3")
		var e surrealdb.Error
		if !errors.As(err, &e) || !strings.Contains(e.Msg, "boom") {
			t.Errorf("%s: the error is %v, want the error of THROW", rel, err)
		}
		if !reflect.DeepEqual(rows, [][][]any{{{int64(1)}}}) {
			t.Errorf("%s: read %v before the error, want the row of RETURN 1", rel, rows)
		}
	}
}

func TestReplayExec(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "surrealdb-3.3.0", "")
	_, err := db.ExecContext(t.Context(), "RETURN 1; THROW 'boom'; RETURN 3")
	var e surrealdb.Error
	if !errors.As(err, &e) || e.Kind != "Thrown" {
		t.Errorf("ExecContext gave %v, want the error of THROW, of the kind Thrown", err)
	}
	res, err := db.ExecContext(t.Context(), "RETURN 1; RETURN 2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.RowsAffected(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("RowsAffected gave %v, want ErrNotSupported (D55)", err)
	}
	if _, err := res.LastInsertId(); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("LastInsertId gave %v, want ErrNotSupported", err)
	}
}

func TestReplaySyntaxError(t *testing.T) {
	t.Parallel()
	_, _, err := sets(t, replayDB(t, "surrealdb-3.3.0", ""), "SELEC 1")
	var e surrealdb.Error
	if !errors.As(err, &e) || e.Code != -32000 || e.Kind != "Validation" || !strings.Contains(e.Msg, "Parse error") {
		t.Errorf("the error is %#v, want the parse error of the RPC call", err)
	}
}

// The server refuses a regex in CBOR with HTTP 400 and a body of JSON, which
// becomes a ResponseError, as the error of a statement does (D55).
func TestReplayRefusedRequest(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{"surrealdb-2.7.0", "surrealdb-3.3.0"} {
		_, _, err := sets(t, replayDB(t, rel, ""), "RETURN /a.b/")
		var re *surrealdb.ResponseError
		var e surrealdb.Error
		if !errors.As(err, &re) || re.HTTPStatus != http.StatusBadRequest || !errors.As(err, &e) || e.Code != http.StatusBadRequest || e.Msg == "" {
			t.Errorf("%s: the error is %#v, want a ResponseError of HTTP 400 with its message", rel, err)
		}
	}
}

// A statement that failed is a ResponseError of the status ERR.
func TestReplayStatementError(t *testing.T) {
	t.Parallel()
	_, _, err := sets(t, replayDB(t, "surrealdb-3.3.0", ""), "SELECT * FROM dbimp_nothing")
	var re *surrealdb.ResponseError
	if !errors.As(err, &re) || re.Status != "ERR" || re.HTTPStatus != http.StatusOK || len(re.Errs) != 1 || re.Errs[0].Kind != "NotFound" {
		t.Errorf("the error is %#v, want one Error of the kind NotFound, and the status ERR", err)
	}
	if got, want := err.Error(), "surrealdb: NotFound: The table 'dbimp_nothing' does not exist"; got != want {
		t.Errorf("the message is %q, want %q", got, want)
	}
}

func TestReplayWrongPassword(t *testing.T) {
	t.Parallel()
	// The recording holds no password, so the match picks the exchange whose
	// response is HTTP 401.
	db := replayWith(t, "surrealdb-3.3.0", "?encoding=json", func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		return ex.Response.Status == http.StatusUnauthorized && dbimptest.DefaultMatch(r, body, ex)
	})
	_, _, err := sets(t, db, "RETURN 1")
	var se *dbimp.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusUnauthorized {
		t.Errorf("the error is %v, want HTTP 401", err)
	}
}

func TestReplayOrdinaryUser(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "surrealdb-3.3.0", "?encoding=json&auth=database")
	_, _, err := sets(t, db, "DEFINE USER dbimp_nobody ON DATABASE PASSWORD 'x' ROLES VIEWER; REMOVE USER IF EXISTS dbimp_nobody ON DATABASE")
	var e surrealdb.Error
	if !errors.As(err, &e) || !strings.Contains(e.Msg, "Not enough permissions") {
		t.Errorf("the error is %v, want the refusal of IAM", err)
	}
}

func TestReplayManyRecords(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{"surrealdb-2.7.0", "surrealdb-3.3.0"} {
		cols, rows, err := sets(t, replayDB(t, rel, ""), "SELECT * FROM dbimp_orders")
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if !slices.Equal(cols[0], []string{"id"}) || len(rows[0]) != 5000 {
			t.Fatalf("%s gave %q and %d rows, want 5000 rows of id", rel, cols, len(rows[0]))
		}
		if id, ok := rows[0][0][0].(surrealdb.RecordID); !ok || id.Table != "dbimp_orders" {
			t.Errorf("%s: the first id is %#v, want a RecordID of dbimp_orders", rel, rows[0][0][0])
		}
	}
}

func TestReplayTransactionInOneRequest(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]int{"surrealdb-2.7.0": 3, "surrealdb-3.3.0": 5} {
		_, rows, err := sets(t, replayDB(t, rel, ""), "BEGIN; CREATE dbimp_people:tx SET v = 1; COMMIT; SELECT * FROM dbimp_people:tx; DELETE dbimp_people:tx")
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		// 3.x returns an entry for BEGIN and for COMMIT, and 2.7 does not.
		if len(rows) != want {
			t.Errorf("%s gave %d result sets, want %d", rel, len(rows), want)
		}
	}
	db := replayDB(t, "surrealdb-3.3.0", "")
	if _, err := db.BeginTx(t.Context(), nil); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("BeginTx gave %v, want ErrNotSupported (D54)", err)
	}
}

func TestReplayNoStatement(t *testing.T) {
	t.Parallel()
	// BEGIN alone returns no entry on 2.7.
	cols, rows, err := sets(t, replayDB(t, "surrealdb-2.7.0", ""), "BEGIN")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || len(cols[0]) != 0 || len(rows[0]) != 0 {
		t.Errorf("BEGIN on 2.7 gave %q and %v, want one result set with no columns and no rows", cols, rows)
	}
}

func TestReplayPing(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{"surrealdb-2.7.0", "surrealdb-3.3.0"} {
		if err := replayDB(t, rel, "?encoding=json").PingContext(t.Context()); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

func TestReplayVersion(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]string{"surrealdb-2.7.0": "surrealdb-2.7.0", "surrealdb-3.1.6": "surrealdb-3.1.6+20260813.cfbaec4"} {
		c, err := replayDB(t, rel, "?encoding=json").Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		var v string
		err = c.Raw(func(dc any) error {
			v, err = surrealdb.Version(t.Context(), dc)
			return err
		})
		if err != nil || v != want {
			t.Errorf("%s: the version is %q, %v, want %q", rel, v, err, want)
		}
	}
	if _, err := surrealdb.Version(t.Context(), "not a connection"); !errors.Is(err, dbimp.ErrNotSupported) {
		t.Errorf("Version of another value gave %v, want ErrNotSupported", err)
	}
}

func TestReplayNamedParameters(t *testing.T) {
	t.Parallel()
	db := replayDB(t, "surrealdb-3.3.0", "?encoding=json")
	_, rows, err := sets(t, db, "RETURN [$s, $i, $f, $b, $n, $o]",
		sql.Named("s", "it's"), sql.Named("i", 42), sql.Named("f", 1.5), sql.Named("b", true), sql.Named("n", nil), sql.Named("o", map[string]any{"k": []any{1}}))
	if err != nil {
		t.Fatal(err)
	}
	want := [][]any{{"it's"}, {int64(42)}, {1.5}, {true}, {nil}, {map[string]any{"k": []any{int64(1)}}}}
	if !reflect.DeepEqual(rows[0], want) {
		t.Errorf("gave %v, want %v", rows[0], want)
	}
}

func TestPositionalArgumentsAreRefused(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("surrealdb", "surrealdb://127.0.0.1:1/a/b")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), "RETURN $x", 1)
	if err == nil {
		defer rows.Close()
		err = rows.Err()
	}
	if !errors.Is(err, dbimp.ErrArguments) {
		t.Errorf("a positional argument gave %v, want ErrArguments (D50)", err)
	}
}
