package trino //nolint:testpackage // The test builds the error with its fields, which a caller reads from the exported type.

import "testing"

// TestErrorVerbose holds that the error is written as psql writes it for
// \errverbose, with the line of the query and a caret under the column (D231).
func TestErrorVerbose(t *testing.T) {
	t.Parallel()
	err := &Error{HTTPStatus: 200, Name: "SYNTAX_ERROR", Type: "USER_ERROR", Code: 1, Message: "line 1:8: mismatched input", Line: 1, Column: 8}
	want := "ERROR:  1: mismatched input\nLINE 1: select 1 frm\n               ^\nDETAIL:  SYNTAX_ERROR\nHTTP STATUS:  200\nTYPE:  USER_ERROR"
	if got := err.ErrVerbose("select 1 frm"); got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
}

// TestErrorVerboseKeepsThePositionOfTheMessage holds that the message keeps its
// "line n:m: " prefix when the text has no line of the query to show, and when
// the prefix names another place.
func TestErrorVerboseKeepsThePositionOfTheMessage(t *testing.T) {
	t.Parallel()
	err := &Error{Code: 1, Message: "line 1:8: mismatched input", Line: 1, Column: 8}
	if got, want := err.ErrVerbose("select\nfrom"), "ERROR:  1: mismatched input\nLINE 1: select\n               ^"; got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
	if got, want := (&Error{Code: 1, Message: "line 3:2: bad", Line: 1, Column: 8}).ErrVerbose("select 1"), "ERROR:  1: line 3:2: bad\nLINE 1: select 1\n               ^"; got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
	if got, want := err.ErrVerbose(""), "ERROR:  1: line 1:8: mismatched input"; got != want {
		t.Errorf("ErrVerbose with no query = %q, want %q", got, want)
	}
}
