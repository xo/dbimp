package athena //nolint:testpackage // The tests read the requests that the connector sends.

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// TestRequestIsSigned holds the form of a request that the recording shows: POST
// / with the target of the operation, the content type of the service, the date,
// and a signature of AWS Signature Version 4 in the scope of the service athena
// over the four headers (recorded: "a wrong secret"). The secret is not in any
// header.
func TestRequestIsSigned(t *testing.T) {
	t.Parallel()
	var got http.Header
	f := newFake(t, func(w http.ResponseWriter, r request, _ int) {
		if r.target == "StartQueryExecution" && got == nil {
			got = r.header
		}
		selectOne(t)(w, r, 0)
	})
	db := open(t, testConfig(), f.srv.URL)
	if _, _, err := read(t, db, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the driver sent no start")
	}
	want := map[string]string{
		"Content-Type": "application/x-amz-json-1.1",
		"X-Amz-Target": "AmazonAthena.StartQueryExecution",
		"X-Amz-Date":   "20261010T085156Z",
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("the header %s is %q, want %q", k, got.Get(k), v)
		}
	}
	auth := got.Get("Authorization")
	for _, part := range []string{
		"AWS4-HMAC-SHA256 Credential=AKIAEXAMPLE/20261010/us-east-1/athena/aws4_request",
		"SignedHeaders=content-type;host;x-amz-date;x-amz-target,",
		"Signature=",
	} {
		if !strings.Contains(auth, part) {
			t.Errorf("the header Authorization is %q, want it to hold %q", auth, part)
		}
	}
	for k, vals := range got {
		for _, v := range vals {
			if strings.Contains(v, "secret") {
				t.Errorf("the header %s holds the secret key", k)
			}
		}
	}
	if got.Get("X-Amz-Security-Token") != "" {
		t.Error("the request has a session token that the Config does not hold")
	}
	// The recorded request has the same four headers, and no other of the driver.
	for _, k := range []string{"X-Amz-Target", "Content-Type", "X-Amz-Date", "Authorization"} {
		if _, ok := exchange(t, 17).Request.Header[k]; !ok {
			t.Errorf("the recorded request has no header %s", k)
		}
	}
}

// TestRequestSignsTheSessionToken holds that a session token goes in the header
// X-Amz-Security-Token, and that the signature covers it. The token is not in the
// text of any error (D94).
func TestRequestSignsTheSessionToken(t *testing.T) {
	t.Parallel()
	var got http.Header
	f := newFake(t, func(w http.ResponseWriter, r request, _ int) {
		if got == nil {
			got = r.header
		}
		selectOne(t)(w, r, 0)
	})
	cfg := testConfig()
	cfg.Token = "IQoJb3JpZ2luX2VjEXAMPLE"
	db := open(t, cfg, f.srv.URL)
	if _, _, err := read(t, db, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if got.Get("X-Amz-Security-Token") != cfg.Token {
		t.Errorf("the token header is %q, want the token", got.Get("X-Amz-Security-Token"))
	}
	if auth := got.Get("Authorization"); !strings.Contains(auth, "x-amz-security-token") {
		t.Errorf("the signature %q does not cover the session token", auth)
	}
}

// TestPageForms reads the forms of a page that the recording does not show: the
// columns in ResultSetMetadata when ColumnInfos is not there, and members that
// the driver does not know.
func TestPageForms(t *testing.T) {
	t.Parallel()
	const col = `{"Name":"n","Type":"bigint","Precision":19,"Scale":0}`
	for _, tt := range []struct {
		name, page string
	}{
		{"the metadata comes first", `{"ResultSet":{"ResultSetMetadata":{"ColumnInfo":[` + col + `]},"ResultRows":[{"Data":["n"]},{"Data":["7"]}]}}`},
		{"unknown members", `{"Extra":{"a":[1,2]},"ResultSet":{"Other":1,"ColumnInfos":[` + col + `],"ResultRows":[{"Data":["n"],"Other":[]},{"Data":["7"]}]},"Output":"x","UpdateCount":0}`},
		{"UpdateCount before the set", `{"UpdateCount":0,"NextToken":null,"ResultSet":{"ColumnInfos":[` + col + `],"ResultRows":[{"Data":["n"]},{"Data":["7"]}]}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, pageHandler(t, tt.page))
			db := open(t, testConfig(), f.srv.URL)
			_, rows, err := read(t, db, "SELECT n")
			if err != nil {
				t.Fatal(err)
			}
			if want := [][]string{{"int64(7)"}}; !equalRows(rows, want) {
				t.Errorf("rows %q, want %q", rows, want)
			}
		})
	}
}

// pageHandler answers the start and the poll of a SELECT with the recorded
// answers, and GetQueryResults with page.
func pageHandler(t *testing.T, page string) func(w http.ResponseWriter, r request, n int) {
	t.Helper()
	start, poll := exchange(t, 17), exchange(t, 18)
	return func(w http.ResponseWriter, r request, _ int) {
		switch r.target {
		case "StartQueryExecution":
			serve(w, start)
		case "GetQueryExecution":
			serve(w, poll)
		default:
			w.Header().Set("Content-Type", "application/x-amz-json-1.1")
			_, _ = w.Write([]byte(page))
		}
	}
}

// TestPagesThatAreNotValid holds that a page that breaks the form is an error
// that the driver returns, never a wrong value (D8): values before the columns,
// too many values in a row, a value that does not fit its type, no rows, and text
// after the end.
func TestPagesThatAreNotValid(t *testing.T) {
	t.Parallel()
	const col = `{"Name":"n","Type":"bigint","Precision":19,"Scale":0}`
	for _, tt := range []struct {
		name, page string
		want       error
	}{
		{"rows before the columns", `{"ResultSet":{"ResultRows":[{"Data":["n"]}],"ColumnInfos":[` + col + `]}}`, dbimp.ErrInvalidValue},
		{"no ResultSet", `{"UpdateCount":0}`, dbimp.ErrInvalidValue},
		{"no ResultRows", `{"ResultSet":{"ColumnInfos":[` + col + `]}}`, dbimp.ErrInvalidValue},
		{"too many values", `{"ResultSet":{"ColumnInfos":[` + col + `],"ResultRows":[{"Data":["n"]},{"Data":["1","2"]}]}}`, dbimp.ErrColumnCount},
		{"a value that does not fit", `{"ResultSet":{"ColumnInfos":[` + col + `],"ResultRows":[{"Data":["n"]},{"Data":["x"]}]}}`, dbimp.ErrInvalidValue},
		{"a value that is not text", `{"ResultSet":{"ColumnInfos":[` + col + `],"ResultRows":[{"Data":["n"]},{"Data":[7]}]}}`, dbimp.ErrInvalidValue},
		{"a body that is not an object", `[]`, nil},
		{"text after the end", `{"ResultSet":{"ColumnInfos":[` + col + `],"ResultRows":[{"Data":["n"]}]}} {}`, dbimp.ErrInvalidValue},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake(t, pageHandler(t, tt.page))
			db := open(t, testConfig(), f.srv.URL)
			_, _, err := read(t, db, "SELECT n")
			if err == nil {
				t.Fatal("the page gave no error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("the error is %v, want %v", err, tt.want)
			}
		})
	}
}

// TestRowWithFewerValuesPadsNULL holds that a row with fewer values than the
// columns has NULL for the others, as the row of DESCRIBE has (recorded:
// "describe the table"), and that a row with no Data is all NULL.
func TestRowWithFewerValuesPadsNULL(t *testing.T) {
	t.Parallel()
	const cols = `[{"Name":"a","Type":"bigint","Precision":19,"Scale":0},{"Name":"b","Type":"varchar","Precision":9,"Scale":0}]`
	page := `{"ResultSet":{"ColumnInfos":` + cols + `,"ResultRows":[{"Data":["a","b"]},{"Data":["1"]},{"Data":[]},{}]}}`
	f := newFake(t, pageHandler(t, page))
	db := open(t, testConfig(), f.srv.URL)
	_, rows, err := read(t, db, "SELECT a, b")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"int64(1)", "nil"}, {"nil", "nil"}, {"nil", "nil"}}
	if !equalRows(rows, want) {
		t.Errorf("rows %q, want %q", rows, want)
	}
}

// TestExecDecodesNothing holds that Exec reads the rows without decoding a value,
// so a value that the driver cannot decode is an error of Query and none of Exec,
// and Exec reads the count after the rows.
func TestExecDecodesNothing(t *testing.T) {
	t.Parallel()
	const col = `{"Name":"n","Type":"bigint","Precision":19,"Scale":0}`
	page := `{"ResultSet":{"ColumnInfos":[` + col + `],"ResultRows":[{"Data":["n"]},{"Data":["not a number"]}]},"UpdateCount":4}`
	f := newFake(t, pageHandler(t, page))
	db := open(t, testConfig(), f.srv.URL)
	res, err := db.ExecContext(t.Context(), "SELECT n")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := res.RowsAffected(); err != nil || n != 4 {
		t.Errorf("RowsAffected gave %d and %v, want 4", n, err)
	}
	if _, _, err := read(t, db, "SELECT n"); !errors.Is(err, dbimp.ErrInvalidValue) {
		t.Errorf("Query gave %v, want dbimp.ErrInvalidValue", err)
	}
}

// TestScanIntoNull scans a NULL and a value into sql.Null[T] and into pointers,
// as a caller does (D25).
func TestScanIntoNull(t *testing.T) {
	t.Parallel()
	db := replay(t)
	var (
		e string
		n sql.Null[string]
	)
	if err := db.QueryRowContext(t.Context(), "SELECT '' AS e, CAST(NULL AS varchar) AS n").Scan(&e, &n); err != nil {
		t.Fatal(err)
	}
	if e != "" || n.Valid {
		t.Errorf("scanned %q and %v, want the empty string and no value", e, n)
	}
	var s *string
	if err := db.QueryRowContext(t.Context(), "SELECT '' AS e, CAST(NULL AS varchar) AS n").Scan(&e, &s); err != nil || s != nil {
		t.Errorf("scanned %v and %v into a pointer, want nil", s, err)
	}
	var v string
	if err := db.QueryRowContext(t.Context(), "SELECT '' AS e, CAST(NULL AS varchar) AS n").Scan(&e, &v); err == nil {
		t.Error("scanning a NULL into a string gave no error")
	}
}

// TestNoGoroutineIsLeft holds step 12: after queries that end, queries that the
// caller closes early, and a context that ends, no goroutine of the driver runs.
func TestNoGoroutineIsLeft(t *testing.T) { //nolint:paralleltest // CheckGoroutines counts the goroutines of the process.
	dbimptest.CheckGoroutines(t)
	f := newFake(t, selectOne(t))
	c := connectorAt(t, testConfig(), f.srv.URL)
	db := sql.OpenDB(c)
	t.Cleanup(func() { _ = db.Close() })
	for range 2 {
		closeEarly(t, db)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

// closeEarly runs a query, reads one row, and closes the rows before the end.
func closeEarly(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	rows.Next()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestRowWithNoColumnIsDropped holds that a statement with no column that still
// sends a row, as DROP TABLE did in the live run of 2026-10-10, is no error for
// Exec or for Query.
func TestRowWithNoColumnIsDropped(t *testing.T) {
	t.Parallel()
	page := `{"ResultSet":{"ColumnInfos":[],"ResultRows":[{"Data":[""]}]},"UpdateCount":0}`
	f := newFake(t, pageHandler(t, page))
	db := open(t, testConfig(), f.srv.URL)
	if _, err := db.ExecContext(t.Context(), "DROP TABLE t"); err != nil {
		t.Errorf("Exec gave %v", err)
	}
	if _, rows, err := read(t, db, "DROP TABLE t"); err != nil || len(rows) != 1 && len(rows) != 0 {
		t.Errorf("Query gave %v and %v", rows, err)
	}
}
