package spanner //nolint:testpackage // The tests point a connector at a fake server, which only the package can do.

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// These tests replay the exchanges that step 6 recorded from the Cloud Spanner
// emulator of dbmeta, release spanneremulator-1.5.58, under testdata/spanner/.
// The emulator answers executeStreamingSql as one JSON object for each message,
// each wrapped in the member result, and not as one array (D196).

const emulatorRelease = "spanner-spanneremulator-1.5.58"

// sessionPart matches the session in a path.
var sessionPart = regexp.MustCompile(`/sessions/[^/:]+`)

// emulatorPath returns a path with the session written as one.
func emulatorPath(p string) string {
	return sessionPart.ReplaceAllString(p, "/sessions/SESSION")
}

// emulatorMatch reports whether the exchange ex answers the request r. It
// matches the method, the path and the work: the statement of a stream, the
// options of a transaction and the statements of a DDL call. It ignores the ids,
// because the exchanges came from other sessions and transactions.
func emulatorMatch(tb testing.TB) dbimptest.Match {
	tb.Helper()
	return func(r *http.Request, body []byte, ex *dbimptest.Exchange) bool {
		if r.Method != ex.Request.Method || emulatorPath(r.URL.Path) != emulatorPath(ex.Request.Path) {
			return false
		}
		eb := ex.Request.Content()
		switch _, verb := split(tb, r.URL.Path); {
		case sqlVerb(verb):
			return sameMember(body, eb, "sql") && sameMember(body, eb, "params") && sameMember(body, eb, "paramTypes")
		case verb == "beginTransaction":
			return sameMember(body, eb, "options")
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			return sameMember(body, eb, "session")
		case strings.HasSuffix(r.URL.Path, "/ddl") && r.Method == http.MethodPatch:
			return sameMember(body, eb, "statements")
		}
		return true
	}
}

// emulatorDB returns a database on a replay server of the emulator recordings.
func emulatorDB(t *testing.T) *sql.DB {
	t.Helper()
	srv := dbimptest.ReplayRelease(t, testdata, emulatorRelease, emulatorMatch(t))
	cfg := Config{Host: "127.0.0.1", Project: "dbmeta", Instance: "dbmeta", Database: "dbmeta"}
	return open(t, cfg, srv.URL, false)
}

// TestEmulatorStreamForm holds that the driver reads the stream of the emulator:
// the rows in the order of the message, every type, a result with no rows, and a
// result in one large message.
func TestEmulatorStreamForm(t *testing.T) {
	t.Parallel()
	db := emulatorDB(t)
	for name, tt := range map[string]struct {
		query string
		want  [][]any
	}{
		"two rows": {
			`SELECT 1 AS a, "x" AS b UNION ALL SELECT 2, "y"`,
			[][]any{{int64(2), "y"}, {int64(1), "x"}},
		},
		"no rows": {
			"SELECT x FROM UNNEST([1]) AS x WHERE FALSE",
			nil,
		},
		"two columns of one name": {
			"SELECT 1 AS a, 2 AS a",
			[][]any{{int64(1), int64(2)}},
		},
		"INT64 limits": {
			"SELECT CAST('9223372036854775807' AS INT64) AS hi, CAST('-9223372036854775808' AS INT64) AS lo",
			[][]any{{int64(9223372036854775807), int64(-9223372036854775808)}},
		},
		"a comment": {
			"-- a comment\nSELECT 1 AS a",
			[][]any{{int64(1)}},
		},
		"a trailing semicolon": {
			"SELECT 1 AS a;",
			[][]any{{int64(1)}},
		},
	} {
		got, err := query(t, db, tt.query)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s: read %d rows, want %d", name, len(got), len(tt.want))
			continue
		}
		for i := range tt.want {
			for j := range tt.want[i] {
				if !equalValue(got[i][j], tt.want[i][j]) {
					t.Errorf("%s: row %d column %d is %#v, want %#v", name, i, j, got[i][j], tt.want[i][j])
				}
			}
		}
	}
}

// TestEmulatorColumns holds that the metadata of the emulator names the columns in
// the order of the statement, and the rows of every type decode.
func TestEmulatorColumns(t *testing.T) {
	t.Parallel()
	db := emulatorDB(t)
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM emu_t ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "s", "n", "f", "b", "ts", "d", "bo", "nu", "j"}
	if strings.Join(cols, ",") != strings.Join(want, ",") {
		t.Errorf("the columns are %v, want %v", cols, want)
	}
	n := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		if n++; n == 2 {
			for i, v := range vals[1:] {
				if v != nil {
					t.Errorf("the NULL in column %s is %#v, want nil", cols[i+1], v)
				}
			}
		}
	}
	if err := rows.Err(); err != nil || n != 2 {
		t.Errorf("read %d rows with the error %v, want 2 rows", n, err)
	}
}

// TestEmulatorLargeResults holds that the driver reads a result of 5000 rows,
// and a string of 100 KB, which the emulator sends in one message.
func TestEmulatorLargeResults(t *testing.T) {
	t.Parallel()
	db := emulatorDB(t)
	got, err := query(t, db, "SELECT x FROM UNNEST(GENERATE_ARRAY(1, 5000)) AS x ORDER BY x")
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, row := range got {
		n, ok := row[0].(int64)
		if !ok {
			t.Fatalf("a value is %#v, want an int64", row[0])
		}
		sum += n
	}
	if len(got) != 5000 || sum != 5000*5001/2 {
		t.Errorf("read %d rows with the sum %d, want 5000 rows with the sum %d", len(got), sum, 5000*5001/2)
	}
	got, err = query(t, db, "SELECT REPEAT('x', 100000) AS big")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("read %d rows, want 1", len(got))
	}
	if str, ok := got[0][0].(string); !ok || len(str) != 100000 {
		t.Errorf("the value is %T of the wrong length, want a string of 100000 bytes", got[0][0])
	}
}

// TestEmulatorErrors holds that an error of the emulator reaches the caller as
// an *Error with the HTTP status and the text of the emulator, whatever form it
// has: the member error with a code that has no name, an empty body, and a
// refusal that the emulator has not built.
func TestEmulatorErrors(t *testing.T) {
	t.Parallel()
	db := emulatorDB(t)
	for name, tt := range map[string]struct {
		query   string
		status  int
		code    string
		message string
	}{
		"a division by zero":            {"SELECT 1 / 0 AS v", http.StatusBadRequest, "OUT_OF_RANGE", "division by zero: 1 / 0"},
		"a division by zero after rows": {"SELECT x, 1 / (x - 5000) AS v FROM UNNEST(GENERATE_ARRAY(1, 6000)) AS x ORDER BY x", http.StatusBadRequest, "OUT_OF_RANGE", "division by zero: 1 / 0"},
		"a syntax error":                {"SELEC 1", http.StatusBadRequest, "", "Bad Request"},
		"an unknown table":              {"SELECT * FROM emu_nosuch", http.StatusBadRequest, "", "Bad Request"},
		"two statements":                {"SELECT 1 AS a; SELECT 2 AS b", http.StatusBadRequest, "", "Bad Request"},
		"a limit of the emulator":       {"SELECT x FROM UNNEST(GENERATE_ARRAY(1, 16001)) AS x", http.StatusBadRequest, "OUT_OF_RANGE", "Cannot generate arrays with more than 16000 elements."},
		"a struct column":               {"SELECT [1, 2, 3] AS a, ARRAY<STRING>[] AS e, [CAST(NULL AS INT64), 1] AS n, STRUCT(1 AS a, 'x' AS b) AS s, ARRAY(SELECT AS STRUCT 1 AS a, 'x' AS b) AS sa", http.StatusNotImplemented, "UNIMPLEMENTED", "Unsupported query shape"},
		"a bad parameter":               {"SELECT @p AS p", http.StatusBadRequest, "INVALID_ARGUMENT", "Incomplete query parameters p"},
	} {
		err := failure(t, db, tt.query)
		serr, ok := errors.AsType[*Error](err)
		switch {
		case !ok:
			t.Errorf("%s: the error is %v, want an *Error", name, err)
		case serr.HTTPStatus != tt.status || serr.Status != tt.code || !strings.Contains(serr.Message, tt.message):
			t.Errorf("%s: the error is %d %q %q, want %d %q %q", name, serr.HTTPStatus, serr.Status, serr.Message, tt.status, tt.code, tt.message)
		}
		if errors.Is(err, dbimp.ErrInvalidValue) {
			t.Errorf("%s: the error is a parse error: %v", name, err)
		}
	}
}

// TestEmulatorGatewayFault holds that the body that the gateway of the emulator
// writes for a failed executeSql and for a duplicate key, HTTP 500 with the
// members code and message and no member error, becomes an *Error with the text.
func TestEmulatorGatewayFault(t *testing.T) {
	t.Parallel()
	res := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"code": 13, "message": "failed to marshal error message"}`)),
	}
	serr, ok := errors.AsType[*Error](checkStatus(res))
	if !ok || serr.HTTPStatus != http.StatusInternalServerError || serr.Status != "INTERNAL" || serr.Message != "failed to marshal error message" {
		t.Errorf("the error is %#v, want an *Error INTERNAL with HTTP 500 and the message", serr)
	}
	// The error of a DDL call has the same form with a name that the code gives.
	res = &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"code":5,"message":"Table not found: emu_nosuch"}`)),
	}
	serr, ok = errors.AsType[*Error](checkStatus(res))
	if !ok || serr.Status != "NOT_FOUND" || serr.Message != "Table not found: emu_nosuch" {
		t.Errorf("the error is %#v, want an *Error NOT_FOUND with the message", serr)
	}
}

// TestEmulatorDML holds that the statements of a transaction read the count of
// rows from stats, that THEN RETURN gives rows with the count, that a rollback
// and a DDL statement work, and that the transaction that the emulator aborts
// gives ErrAborted (D191 item 6).
func TestEmulatorDML(t *testing.T) {
	t.Parallel()
	db := emulatorDB(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		stmt string
		want int64
	}{
		{"INSERT INTO emu_t (id, s, n) VALUES (100, 'i', 1)", 1},
		{"UPDATE emu_t SET s = 'u' WHERE id >= 100", 1},
		{"UPDATE emu_t SET n = 0 WHERE id = 9999", 0},
		{"DELETE FROM emu_t WHERE id = 100", 1},
	} {
		res, err := tx.ExecContext(t.Context(), tt.stmt)
		if err != nil {
			t.Fatalf("%s: %v", tt.stmt, err)
		}
		if n, err := res.RowsAffected(); err != nil || n != tt.want {
			t.Errorf("%s: %d rows with the error %v, want %d", tt.stmt, n, err, tt.want)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Errorf("committing: %v", err)
	}
	tx, err = db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	var s string
	if err := tx.QueryRowContext(t.Context(), "INSERT INTO emu_t (id, s) VALUES (102, 'ret') THEN RETURN id, s").Scan(&id, &s); err != nil || id != 102 || s != "ret" {
		t.Errorf("THEN RETURN gave %d %q with the error %v, want 102 and ret", id, s, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Errorf("rolling back: %v", err)
	}
	if _, err := db.ExecContext(t.Context(), "DROP TABLE emu_t"); err != nil {
		t.Errorf("dropping a table: %v", err)
	}
}

// TestEmulatorAbort holds that the second statement of a transaction that
// another transaction aborts, which the emulator does with HTTP 409 and the code 10,
// is ErrAborted.
func TestEmulatorAbort(t *testing.T) {
	t.Parallel()
	db := emulatorDB(t)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(t.Context(), "UPDATE emu_t SET n = 2 WHERE id = 1")
	serr, ok := errors.AsType[*Error](err)
	if !errors.Is(err, ErrAborted) || !ok || serr.HTTPStatus != http.StatusConflict {
		t.Errorf("the error is %v, want ErrAborted with HTTP 409", err)
	}
}

// TestEmulatorStreamErrorAfterRows holds the error element of the sequence
// form: a message, then {"error": ...}. The emulator never sent one, because it
// checks the whole statement before it answers, so the body is a fake. The error
// wraps dbimp.ErrIncomplete after a row, as the array form does (D107).
func TestEmulatorStreamErrorAfterRows(t *testing.T) {
	t.Parallel()
	int64s := intField("a")
	text := `{"result":{` + metaOf(int64s) + `,"values":["1"]}}` + "\n" + `{"error":{"code":11,"message":"division by zero"}}`
	got, err := query(t, body(t, text), "SELECT 1")
	if len(got) != 1 || got[0][0] != int64(1) {
		t.Errorf("read %v, want the row 1 only", got)
	}
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.Status != "OUT_OF_RANGE" || !errors.Is(err, dbimp.ErrIncomplete) {
		t.Errorf("the error is %v, want an *Error OUT_OF_RANGE that wraps dbimp.ErrIncomplete", err)
	}
}

// TestEmulatorSequenceForms holds the shapes of the sequence form: a value cut
// across two wrappers, with chunkedValue, a row of values across wrappers, the
// members after result, stats and a missing last.
func TestEmulatorSequenceForms(t *testing.T) {
	t.Parallel()
	int64s, str := intField("id"), stringField
	for name, tt := range map[string]struct {
		body string
		want [][]any
	}{
		"chunkedValue": {
			`{"result":{"values":["1","ab"],` + metaOf(int64s, str) + `,"chunkedValue":true}}` + "\n" + `{"result":{"values":["cd","2","x"]}}`,
			[][]any{{int64(1), "abcd"}, {int64(2), "x"}},
		},
		"rows in two wrappers": {
			`{"result":{` + metaOf(int64s, str) + `,"values":["1","a"]}}{"result":{"values":["2","b"]}}`,
			[][]any{{int64(1), "a"}, {int64(2), "b"}},
		},
		"a member after the result": {
			`{"result":{` + metaOf(int64s) + `,"values":["1"]},"other":{"a":[1,2]}}`,
			[][]any{{int64(1)}},
		},
	} {
		got, err := query(t, body(t, tt.body), "SELECT 1")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s: read %d rows, want %d", name, len(got), len(tt.want))
			continue
		}
		for i := range tt.want {
			for j := range tt.want[i] {
				if !equalValue(got[i][j], tt.want[i][j]) {
					t.Errorf("%s: row %d column %d is %#v, want %#v", name, i, j, got[i][j], tt.want[i][j])
				}
			}
		}
	}
}

// TestBrokenSequences holds that a sequence that stops in a wrapper or in a
// message, or that holds a value that is not an object, is an error and not an
// end of the result.
func TestBrokenSequences(t *testing.T) {
	t.Parallel()
	int64s := intField("a")
	for name, tt := range map[string]struct {
		body string
		rows int
		want error
	}{
		"a body that stops in a wrapper": {
			`{"result":{` + metaOf(int64s) + `,"values":["1"]}}` + "\n" + `{"result":{"values":["2"`, 1, io.ErrUnexpectedEOF},
		"a wrapper that is not closed": {
			`{"result":{` + metaOf(int64s) + `,"values":["1"]}`, 1, io.ErrUnexpectedEOF},
		"a value after the sequence that is not an object": {
			`{"result":{` + metaOf(int64s) + `,"values":["1"]}} []`, 1, dbimp.ErrInvalidValue},
		"a row that stays short": {
			`{"result":{` + metaOf(int64s, stringField) + `,"values":["1","a","2"]}}`, 1, dbimp.ErrColumnCount},
		"a sequence with no message": {
			`{"other":1}`, 0, dbimp.ErrInvalidValue},
	} {
		got, err := query(t, body(t, tt.body), "SELECT 1")
		if err == nil || !errors.Is(err, tt.want) {
			t.Errorf("%s: the error is %v, want one that wraps %v", name, err, tt.want)
			continue
		}
		if len(got) != tt.rows {
			t.Errorf("%s: read %d rows, want %d", name, len(got), tt.rows)
		}
	}
}
