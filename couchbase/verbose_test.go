package couchbase_test

import (
	"testing"

	"github.com/xo/dbimp/couchbase"
)

// TestResponseErrorVerbose holds that the error of a response writes each error
// as psql writes it, and then the status of the response (D231).
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
	want := "ERROR:  3000: syntax error\nERROR:  no code\nHTTP STATUS:  400\nSTATUS:  fatal"
	if got := err.ErrVerbose("select"); got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
}
