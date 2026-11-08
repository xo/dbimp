package trino //nolint:testpackage // The test builds the error with its fields, which a caller reads from the exported type.

import "testing"

// TestErrorVerbose holds that the error is written as psql writes it for
// \errverbose, with the line of the query and a caret under the column (D231).
func TestErrorVerbose(t *testing.T) {
	t.Parallel()
	err := &Error{HTTPStatus: 200, Name: "SYNTAX_ERROR", Type: "USER_ERROR", Code: 1, Message: "line 1:8: mismatched input", Line: 1, Column: 8}
	want := "ERROR:  1: line 1:8: mismatched input\nLINE 1: select 1 frm\n               ^\nDETAIL:  SYNTAX_ERROR\nHTTP STATUS:  200\nTYPE:  USER_ERROR"
	if got := err.ErrVerbose("select 1 frm"); got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
}
