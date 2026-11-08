package couchbase_test

import (
	"testing"

	"github.com/xo/dbimp/couchbase"
)

// TestResponseErrorVerbose holds that the error of a response writes its
// status and each error with its fields, for the command \errverbose of usql
// (D230).
func TestResponseErrorVerbose(t *testing.T) {
	t.Parallel()
	err := &couchbase.ResponseError{
		HTTPStatus: 400,
		Status:     "fatal",
		Errs: []couchbase.Error{
			{Code: 3000, Msg: "syntax error"},
			{Msg: "no code"},
		},
	}
	want := "HTTP status: 400\nStatus: fatal\nsyntax error\nCode: 3000\ncouchbase: 0: no code"
	if got := err.ErrVerbose("select"); got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
}
