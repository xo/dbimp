package couchbase_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xo/dbimp"
)

// TestKindOfTheSignature holds D135 and D136: a value whose JSON kind is not
// the kind that the signature names is an error, and a value of the kind
// json, or of the kind that the signature names, reads as its Go value.
func TestKindOfTheSignature(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		body string
		ok   bool
	}{
		{`{"signature":{"a":"string","b":"json"},"results":[{"a":"x","b":1}],"status":"success"}`, true},
		{`{"signature":{"a":"boolean"},"results":[{"a":false},{"a":null}],"status":"success"}`, true},
		{`{"signature":{"a":"string"},"results":[{"a":1}],"status":"success"}`, false},
		{`{"signature":{"a":"number"},"results":[{"a":"1"}],"status":"success"}`, false},
	} {
		scanErr := scanBody(t, tt.body)
		switch {
		case tt.ok && scanErr != nil:
			t.Errorf("%s gave %v", tt.body, scanErr)
		case !tt.ok && !errors.Is(scanErr, dbimp.ErrInvalidValue):
			t.Errorf("%s gave %v, want dbimp.ErrInvalidValue", tt.body, scanErr)
		}
	}
}

// scanBody answers one query with body, and scans every row into *any. It
// returns the error of the scan.
func scanBody(t *testing.T, body string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	db := openAt(t, srv.URL)
	defer db.Close()
	rows, err := db.QueryContext(t.Context(), "SELECT a FROM t")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
	}
	return rows.Err()
}
