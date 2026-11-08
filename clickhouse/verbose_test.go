package clickhouse //nolint:testpackage // The test builds an error with the function of the driver, which is not exported.

import "testing"

// TestErrorVerbose holds that the error writes every field that the server
// sent, for the command \errverbose of usql (D230).
func TestErrorVerbose(t *testing.T) {
	t.Parallel()
	err := newError("Code: 60. DB::Exception: Table t does not exist. (UNKNOWN_TABLE) (version 26.9.2.8 (official build))", "")
	err.HTTPStatus = 404
	want := "Table t does not exist.\nHTTP status: 404\nCode: 60\nName: UNKNOWN_TABLE"
	if got := err.ErrVerbose("select * from t"); got != want {
		t.Errorf("ErrVerbose = %q, want %q", got, want)
	}
}
