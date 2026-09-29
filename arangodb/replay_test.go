package arangodb_test

import (
	"bytes"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/xo/dbimp/arangodb"
	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from a real server,
// under testdata/arangodb/. Each one decodes a real answer through the
// driver.

const (
	testdata = "../testdata/arangodb"
	release  = "arangodb-3.12.12"
)

// tagRE matches the comment of each query of the driver, at its end or, for
// a long query, at its start (D90 and D99).
var tagRE = regexp.MustCompile(`\n// dbimp:[A-Z2-7]+-[0-9]+$|^// dbimp:[A-Z2-7]+-[0-9]+\n`)

// matcher returns a match that serves each recorded exchange once, in order,
// because the next batch of a cursor is the same request each time.
func matcher() dbimptest.Match {
	var mu sync.Mutex
	used := map[*dbimptest.Exchange]bool{}
	return func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		mu.Lock()
		defer mu.Unlock()
		if used[ex] || r.Method != ex.Request.Method || r.URL.Path != ex.Request.Path || r.URL.RawQuery != ex.Request.Query {
			return false
		}
		if canon(r.URL.Path, body) != canon(ex.Request.Path, ex.Request.Content()) {
			return false
		}
		used[ex] = true
		return true
	}
}

// canon returns the canonical form of the body of a request. It removes the
// tag of the driver, and the batchSize and the options, which the recording
// sent only where a request measured them. A collection with no type is a
// document collection, type 2.
func canon(path string, b []byte) string {
	if len(bytes.TrimSpace(b)) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b)
	}
	if q, ok := m["query"].(string); ok {
		m["query"] = tagRE.ReplaceAllString(q, "")
	}
	delete(m, "batchSize")
	delete(m, "options")
	if v, ok := m["bindVars"].(map[string]any); ok && len(v) == 0 {
		delete(m, "bindVars")
	}
	if strings.HasSuffix(path, "/collection") {
		if _, ok := m["type"]; !ok {
			m["type"] = 2.0
		}
	}
	out, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return string(b)
	}
	return string(out)
}

// replay opens the driver against the recorded exchanges, as the principal
// that the DSN names, which only the recording tells apart.
func replay(t *testing.T, query string) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, release, matcher())
	db, err := sql.Open(arangodb.Name, strings.Replace(srv.URL, "http://", "arangodb://root:secret@", 1)+"/dbmeta?"+query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// readAll runs query, and reads its columns and every row.
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

// check runs query, and compares its columns and its rows.
func check(t *testing.T, db *sql.DB, query string, wantCols []string, want [][]any, args ...any) {
	t.Helper()
	cols, got, err := readAll(t, db, query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if !reflect.DeepEqual(cols, wantCols) {
		t.Errorf("%s: the columns are %q, want %q", query, cols, wantCols)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: the rows are\n%#v\nwant\n%#v", query, got, want)
	}
}

func TestReplayShapes(t *testing.T) {
	t.Parallel()
	db := replay(t, "cancel=none")
	// An object keeps the order of the query (D89).
	check(t, db, "RETURN {b: 1, a: 2, c: null}", []string{"b", "a", "c"}, [][]any{{int64(1), int64(2), nil}})
	// A projection reads a missing attribute as null (measured).
	check(t, db, "FOR u IN types FILTER u._key == 'c' RETURN {n: u.n, s: u.s}", []string{"n", "s"}, [][]any{{int64(3), nil}})
	check(t, db, "FOR x IN [1, {a: 1}, [2], 's', null] RETURN x", []string{""},
		[][]any{{int64(1)}, {map[string]any{"a": int64(1)}}, {[]any{int64(2)}}, {"s"}, {nil}})
	check(t, db, "FOR u IN types SORT u.n RETURN u.n", []string{""}, [][]any{{int64(1)}, {int64(2)}, {int64(3)}})
	check(t, db, "FOR u IN types FILTER u.n > 100 RETURN u", []string{}, nil)
}

func TestReplayDocuments(t *testing.T) {
	t.Parallel()
	db := replay(t, "cancel=none")
	// A document is one column, and documents keep their own attributes
	// (D89).
	cols, got, err := readAll(t, db, "FOR u IN types SORT u.n RETURN u")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cols, []string{""}) || len(got) != 3 {
		t.Fatalf("documents gave the columns %q and %d rows, want one column and 3 rows", cols, len(got))
	}
	c, ok := got[2][0].(map[string]any)
	if !ok || c["extra"] != "only here" || c["_key"] != "c" {
		t.Errorf("the third document is %#v, want the document c with its own attribute", got[2][0])
	}
}

func TestReplayTypes(t *testing.T) {
	t.Parallel()
	db := replay(t, "cancel=none")
	cols, got, err := readAll(t, db, "RETURN {nil: null, t: true, f: false, zero: 0, neg: -1, imax: 9223372036854775807, "+
		"imin: -9223372036854775808, big: 18446744073709551615, twoTo53: 9007199254740993, twoTo60: 1152921504606846976, "+
		"half: 1.5, max: 1.7976931348623157e308, tiny: 5e-324, s: 'é 日本 🙂', empty: '', arr: [], obj: {}, "+
		"date: DATE_ISO8601(0), now: DATE_NOW()}")
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{}
	for i, c := range cols {
		row[c] = got[0][i]
	}
	for name, want := range map[string]any{
		"nil": nil, "t": true, "f": false, "zero": int64(0), "neg": int64(-1), "imax": int64(math.MaxInt64),
		// A number above the range of int64 is a double in AQL (measured).
		"imin": float64(math.MinInt64), "big": float64(math.MaxUint64), "twoTo53": int64(9007199254740993),
		"twoTo60": int64(1152921504606846976), "half": 1.5, "max": math.MaxFloat64, "tiny": math.SmallestNonzeroFloat64,
		"s": "é 日本 🙂", "empty": "", "arr": []any{}, "obj": map[string]any{}, "date": "1970-01-01T00:00:00.000Z",
	} {
		if !reflect.DeepEqual(row[name], want) {
			t.Errorf("%s is %#v, want %#v", name, row[name], want)
		}
	}
	if _, ok := row["now"].(int64); !ok {
		t.Errorf("DATE_NOW() is %#v, want an int64 of milliseconds", row["now"])
	}
	// A division by zero is null, with a warning (measured).
	check(t, db, "RETURN 1 / 0", []string{""}, [][]any{{nil}})
}

func TestReplayParameters(t *testing.T) {
	t.Parallel()
	db := replay(t, "cancel=none")
	check(t, db, "RETURN [@a, @b, @c]", []string{""}, [][]any{{[]any{int64(1), "x", nil}}},
		sql.Named("a", 1), sql.Named("b", "x"), sql.Named("c", nil))
	check(t, db, "RETURN @1", []string{""}, [][]any{{int64(5)}}, 5)
	// A name that the query uses as @@c names a collection.
	check(t, db, "FOR u IN @@c FILTER u._key == 'a' RETURN u.n", []string{""}, [][]any{{int64(1)}}, sql.Named("c", "types"))
	_, _, err := readAll(t, db, "RETURN @a")
	if !isNum(err, 1551) {
		t.Errorf("a missing parameter gave %v, want error 1551", err)
	}
	_, _, err = readAll(t, db, "RETURN 1", sql.Named("a", 1))
	if !isNum(err, 1552) {
		t.Errorf("an extra parameter gave %v, want error 1552", err)
	}
}

// isNum reports whether err is an *arangodb.Error with errorNum num.
func isNum(err error, num int) bool {
	e, ok := errors.AsType[*arangodb.Error](err)
	return ok && e.Num == num
}

func TestReplayBatches(t *testing.T) {
	t.Parallel()
	db := replay(t, "cancel=none&batch=2")
	// Three batches of 2, 2 and 1 (measured).
	check(t, db, "FOR i IN 1..5 RETURN i", []string{""}, [][]any{{int64(1)}, {int64(2)}, {int64(3)}, {int64(4)}, {int64(5)}})
	// The stream, with the same batches.
	check(t, db, "FOR i IN 1..5 RETURN i", []string{""}, [][]any{{int64(1)}, {int64(2)}, {int64(3)}, {int64(4)}, {int64(5)}})
	db = replay(t, "cancel=none")
	_, got, err := readAll(t, db, "FOR u IN big SORT u.n RETURN u.n")
	if err != nil || len(got) != 3000 || got[2999][0] != int64(3000) {
		t.Errorf("3000 rows in batches of 1000 gave %d rows and %v", len(got), err)
	}
}

func TestReplayErrors(t *testing.T) {
	t.Parallel()
	db := replay(t, "cancel=none&batch=1")
	_, _, err := readAll(t, db, "RETUR 1")
	if !isNum(err, 1501) {
		t.Errorf("a statement that does not parse gave %v, want error 1501", err)
	}
	_, _, err = readAll(t, db, "FOR u IN nothere RETURN u")
	if !isNum(err, 1203) {
		t.Errorf("a collection that does not exist gave %v, want error 1203", err)
	}
	q := "FOR i IN 1..4 RETURN ASSERT(i < 3, 'too big')"
	// The first time, the recording holds the query with no stream, which
	// fails before any row.
	_, _, err = readAll(t, db, q)
	if !isNum(err, 1593) {
		t.Errorf("an error before any rows gave %v, want error 1593", err)
	}
	// The second time, the stream gives a row, and the next fetch fails.
	_, got, err := readAll(t, db, q)
	if !isNum(err, 1593) || len(got) != 1 {
		t.Errorf("an error after some rows gave %d rows and %v, want 1 row and error 1593", len(got), err)
	}
}

func TestReplayExec(t *testing.T) {
	t.Parallel()
	db := replay(t, "cancel=none")
	res, err := db.ExecContext(t.Context(), "INSERT {_key: 'crud', v: 1} INTO types RETURN NEW.v")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		t.Errorf("the insert affected %d rows and %v, want 1 from writesExecuted", n, err)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Errorf("ping: %v", err)
	}
}

func TestReplayDDL(t *testing.T) {
	t.Parallel()
	db := replay(t, "")
	for _, q := range []string{"CREATE COLLECTION dbimp_made", "DROP COLLECTION dbimp_made", "CREATE COLLECTION `dbimp_edge` EDGE"} {
		if _, err := db.ExecContext(t.Context(), q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
}
