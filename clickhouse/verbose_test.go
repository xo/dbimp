package clickhouse //nolint:testpackage // The test builds an error with the function of the driver, which is not exported.

import "testing"

// TestErrorVerbose holds that the error is written as psql writes it for
// \errverbose, with the code on the first line (D231).
func TestErrorVerbose(t *testing.T) {
	t.Parallel()
	err := newError("Code: 60. DB::Exception: Table t does not exist. (UNKNOWN_TABLE) (version 26.9.2.8 (official build))", "")
	err.HTTPStatus = 404
	want := "ERROR:  60: Table t does not exist.\nDETAIL:  UNKNOWN_TABLE\nHTTP STATUS:  404"
	if got := err.ErrVerbose("select * from t"); got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
}
