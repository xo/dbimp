package hive_test

import (
	"strings"
	"testing"

	"github.com/xo/dbimp/hive"
)

// TestErrVerbose holds D230 and D231 for the recorded errors of the servers.
func TestErrVerbose(t *testing.T) {
	t.Parallel()
	hiveErr := &hive.Error{
		SQLState: "42S02", Code: 10001, HTTPStatus: 0,
		Message: "Error while compiling statement: FAILED: SemanticException [Error 10001]: Line 1:17 Table not found 'nothing'",
		Info:    []string{"Server-side error; please check HS2 logs."},
	}
	got := hiveErr.ErrVerbose("select nope from nothing")
	for _, want := range []string{
		"ERROR:  42S02: Error while compiling statement",
		"ERROR CODE:  10001",
		"DETAIL:  Server-side error; please check HS2 logs.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ErrVerbose() = %q, want it to hold %q", got, want)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("ErrVerbose() = %q, want no newline at its end", got)
	}
	impalaErr := &hive.Error{SQLState: "HY000", Message: "Query abc failed:\nAnalysisException: Could not resolve table reference: 'nothing'\n"}
	if got := impalaErr.ErrVerbose(""); !strings.HasPrefix(got, "ERROR:  HY000: Query abc failed:") {
		t.Errorf("ErrVerbose() = %q, want the SQLSTATE and the message on the first line", got)
	}
	exception := &hive.Error{Message: "Invalid method name: 'GetQueryId'"}
	if got := exception.ErrVerbose(""); !strings.HasPrefix(got, "ERROR:  Invalid method name") {
		t.Errorf("ErrVerbose() = %q, want no code", got)
	}
}
