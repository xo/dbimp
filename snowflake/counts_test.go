package snowflake //nolint:testpackage // The test points a connector at a fake server, which only the package can do.

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xo/dbimp"
)

// TestRowsAffectedOfAnswers holds the count of an answer that the live service
// gave (measured, 2026-10-10). The row of the answer names the count, and the
// stats follow only when a row changed.
func TestRowsAffectedOfAnswers(t *testing.T) {
	t.Parallel()
	const head = `{"resultSetMetaData":{"format":"jsonv2","rowType":[`
	col := func(name string) string { return `{"name":"` + name + `","type":"fixed","precision":18,"scale":0}` }
	for _, tt := range []struct {
		name   string
		answer string
		want   int64
		known  bool
	}{
		{"an update of no row", head + col("number of rows updated") + `,` + col("number of multi-joined rows updated") + `]},"data":[["0","0"]]}`, 0, true},
		{"an update that counts a multi-joined row", head + col("number of rows updated") + `,` + col("number of multi-joined rows updated") + `]},"data":[["2","5"]]}`, 2, true},
		{"a delete of no row", head + col("number of rows deleted") + `]},"data":[["0"]]}`, 0, true},
		{"an insert", head + col("number of rows inserted") + `]},"data":[["3"]],"stats":{"numRowsInserted":3,"numRowsDeleted":0,"numRowsUpdated":0}}`, 3, true},
		{"an insert overwrite", head + col("number of rows inserted") + `]},"data":[["1"]],"stats":{"numRowsInserted":1,"numRowsDeleted":3,"numRowsUpdated":0}}`, 1, true},
		{"a merge", head + col("number of rows inserted") + `,` + col("number of rows updated") + `]},"data":[["1","2"]]}`, 3, true},
		{"a truncate of rows", head + `{"name":"status","type":"text"}]},"data":[["Statement executed successfully."]],"stats":{"numRowsInserted":0,"numRowsDeleted":1,"numRowsUpdated":0}}`, 1, true},
		{"a truncate of an empty table", head + `{"name":"status","type":"text"}]},"data":[["Statement executed successfully."]]}`, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = io.WriteString(w, tt.answer)
			}))
			t.Cleanup(srv.Close)
			db := open(t, config(), srv.URL, false)
			res, err := db.ExecContext(t.Context(), "UPDATE t SET a = 1")
			if err != nil {
				t.Fatal(err)
			}
			n, err := res.RowsAffected()
			switch {
			case tt.known && (err != nil || n != tt.want):
				t.Errorf("RowsAffected gave %d and %v, want %d", n, err, tt.want)
			case !tt.known && !errors.Is(err, dbimp.ErrNotSupported):
				t.Errorf("RowsAffected gave %d and %v, want dbimp.ErrNotSupported", n, err)
			}
		})
	}
}

// TestRunTimeErrorHasTheHandle holds that a statement that fails while it runs,
// whose status answers with a code and a message only, gives an error that names
// the handle that the driver holds (measured, 2026-10-10).
func TestRunTimeErrorHasTheHandle(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"code":"333334","message":"in progress","statementHandle":"`+asyncHandle+`","statementStatusUrl":"/api/v2/statements/`+asyncHandle+`"}`)
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"code":"100051","message":"Division by zero"}`)
	}))
	t.Cleanup(srv.Close)
	db := open(t, config(), srv.URL, false)
	_, err := db.ExecContext(t.Context(), "SELECT 1/0")
	if err == nil {
		t.Fatal("the statement did not fail")
	}
	serr, ok := errors.AsType[*Error](err)
	if !ok || serr.Code != "100051" || serr.Handle != asyncHandle || serr.SQLState != "" {
		t.Errorf("the error is %v, want the code 100051, the handle %s and no SQLSTATE", err, asyncHandle)
	}
}
