package trino //nolint:testpackage // The test builds the error with its fields, which a caller reads from the exported type.

import "testing"

// TestErrorVerbose holds that the error writes every field that the server
// sent, with the line and the column of the fault, for the command \errverbose
// of usql (D230).
func TestErrorVerbose(t *testing.T) {
	t.Parallel()
	err := &Error{HTTPStatus: 200, Name: "SYNTAX_ERROR", Type: "USER_ERROR", Code: 1, Message: "line 1:8: mismatched input", Line: 1, Column: 8}
	want := "line 1:8: mismatched input\nHTTP status: 200\nName: SYNTAX_ERROR\nType: USER_ERROR\nCode: 1\nLine: 1\nColumn: 8"
	if got := err.ErrVerbose("select 1 frm"); got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
}
