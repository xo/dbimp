package drill_test

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/drill"
)

// serveBody returns a database on a fake server that answers each query with
// body, and each other request with HTTP 500.
func serveBody(t *testing.T, body string, cut bool) *sql.DB {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path != "/query.json" {
			http.Error(w, "no", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
		if cut {
			_ = http.NewResponseController(w).Flush()
			panic(http.ErrAbortHandler)
		}
	}))
	t.Cleanup(srv.Close)
	db, err := sql.Open(drill.Name, strings.Replace(srv.URL, "http://", "drill://", 1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestAnswersThatEndEarly holds D21 and D165: an answer that ends before its
// queryState is an error, and an error after a row wraps dbimp.ErrIncomplete
// (D107). An answer that ends in the middle of a row does too.
func TestAnswersThatEndEarly(t *testing.T) {
	t.Parallel()
	start := head("q")
	for _, tt := range []struct {
		name       string
		body       string
		cut        bool
		want       error
		incomplete bool
	}{
		{"the connection closes after a row", start + `{"a":1}` + "\n", true, io.ErrUnexpectedEOF, true},
		{"the connection closes in a row", start + `{"a":1}` + "\n" + `,{"a":`, true, io.ErrUnexpectedEOF, true},
		{"no queryState", start + `{"a":1}` + "\n]\n}\n", false, drill.ErrCut, true},
		{"a queryState that is unknown", start + `{"a":1}` + "\n]\n" + `,"queryState":"RUNNING"}`, false, dbimp.ErrInvalidValue, true},
		{"cancelled by someone else", start + `{"a":1}` + "\n]\n" + `,"queryState":"CANCELED"}`, false, drill.ErrCanceled, true},
		{"data after the object", start + `{"a":1}` + "\n]\n" + `,"queryState":"COMPLETED"}{}`, false, dbimp.ErrInvalidValue, true},
		{"a row with a column that is not named", start + `{"b":1}` + "\n]\n" + `,"queryState":"COMPLETED"}`, false, dbimp.ErrExtraColumn, false},
		{"a row that is not an object", start + `[1]` + "\n]\n" + `,"queryState":"COMPLETED"}`, false, dbimp.ErrInvalidValue, false},
		{"a value of the wrong type", start + `{"a":"x"}` + "\n]\n" + `,"queryState":"COMPLETED"}`, false, dbimp.ErrInvalidValue, false},
	} {
		db := serveBody(t, tt.body, tt.cut)
		res := run(t, db, "SELECT a FROM t")
		if !errors.Is(res.err, tt.want) || errors.Is(res.err, dbimp.ErrIncomplete) != tt.incomplete && tt.incomplete {
			t.Errorf("%s: the error is %v, want %v and dbimp.ErrIncomplete %v", tt.name, res.err, tt.want, tt.incomplete)
		}
	}
}

// TestAnswersBeforeTheRows holds the answers that end before the first row.
func TestAnswersBeforeTheRows(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		body string
		want error
	}{
		{"the connection closes in the head", `{"queryId":"q","columns":["a"],`, io.ErrUnexpectedEOF},
		{"not an object", `[1]`, dbimp.ErrInvalidValue},
		{"empty", ``, io.EOF},
		{"columns and metadata differ", `{"queryId":"q","columns":["a","b"],"metadata":["BIGINT"],"rows":[],"queryState":"COMPLETED"}`, dbimp.ErrColumnCount},
		{"no queryState", `{"queryId":"q","columns":["a"],"metadata":["BIGINT"],"rows":[]}`, drill.ErrCut},
	} {
		db := serveBody(t, tt.body, false)
		res := run(t, db, "SELECT a FROM t")
		if !errors.Is(res.err, tt.want) || errors.Is(res.err, dbimp.ErrIncomplete) {
			t.Errorf("%s: the error is %v, want %v and not dbimp.ErrIncomplete", tt.name, res.err, tt.want)
		}
	}
}

// TestAnswerWithNoRowsMember holds that an answer that completes with no rows
// member is an empty result, and one that fails reports its message.
func TestAnswerWithNoRowsMember(t *testing.T) {
	t.Parallel()
	db := serveBody(t, `{"queryId":"q","columns":["a"],"metadata":["BIGINT"],"queryState":"COMPLETED"}`, false)
	if res := run(t, db, "SELECT a FROM t"); res.err != nil || len(res.rows) != 0 || len(res.cols) != 1 {
		t.Errorf("an answer with no rows gave %d columns, %d rows and %v, want 1 column and no rows", len(res.cols), len(res.rows), res.err)
	}
	db = serveBody(t, `{"queryId":"q","exception":"x.Y","errorMessage":"PLAN ERROR: no plan","queryState":"FAILED"}`, false)
	res := run(t, db, "SELECT a FROM t")
	if e, ok := errors.AsType[*drill.Error](res.err); !ok || e.Kind != "PLAN ERROR" || e.Exception != "x.Y" || e.QueryID != "q" {
		t.Errorf("a failed answer gave %v, want a *drill.Error of kind PLAN ERROR", res.err)
	}
}

// TestAnswerSkipsUnknownMembers holds that a member that a later release adds
// does not break the read.
func TestAnswerSkipsUnknownMembers(t *testing.T) {
	t.Parallel()
	db := serveBody(t, `{"queryId":"q","new":{"x":[1]},"columns":["a"],"metadata":["BIGINT(0, 0)"],"attemptedAutoLimit":0,"rows":[{"a":1},{}],"later":1,"queryState":"COMPLETED"}`, false)
	res := run(t, db, "SELECT a FROM t")
	if res.err != nil || len(res.rows) != 2 || res.rows[0][0] != int64(1) || res.rows[1][0] != nil {
		t.Errorf("the rows are %v, %v, want 1 and a missing value as nil", res.rows, res.err)
	}
}
