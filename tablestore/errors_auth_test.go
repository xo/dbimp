package tablestore //nolint:testpackage // The tests read the error types of the package, which build their values in the package.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/dbimptest"
)

// recordedError returns the error that the driver makes of the recorded response
// whose status is status and whose body holds text.
func recordedError(t *testing.T, status int, text string) error {
	t.Helper()
	paths, err := filepath.Glob(testdata + "/tablestore-*.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		ex, err := dbimptest.ReadExchange(path)
		if err != nil {
			t.Fatal(err)
		}
		body := ex.Response.Content()
		if ex.Response.Status != status || !strings.Contains(string(body), text) {
			continue
		}
		res := &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}
		if err := checkStatus(res); err != nil {
			return err
		}
		t.Fatalf("%s has the status %d, want an error", path, status)
	}
	t.Fatalf("no recorded response has the status %d and the text %q", status, text)
	return nil
}

// TestAuthentication holds that an error matches dbimp.ErrAuthentication for a
// refused credential only (D197). The wrong secret is recorded, and the others
// are the answers that the server gave to a request that it understood.
func TestAuthentication(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{`recorded: "a wrong secret"`, recordedError(t, 403, "Signature mismatch"), true},
		{`recorded: "no credentials"`, recordedError(t, 400, "OTSUnsupportOperation"), false},
		{`recorded: "a missing table"`, recordedError(t, 400, "doesn't exist"), false},
		{`recorded: "a syntax error"`, recordedError(t, 400, "error in your SQL syntax"), false},
		{`recorded: "a protobuf body that is not valid"`, recordedError(t, 400, "Failed to parse the ProtoBuf"), false},
		{"a status of 401 with no code", &Error{HTTPStatus: 401, status: &dbimp.StatusError{Code: 401}}, true},
		{"a missing permission, not recorded", &Error{HTTPStatus: 403, Code: "OTSNoPermission", Message: "x"}, false},
	}
	for _, tt := range tests {
		forms := map[string]error{
			"plain":   tt.err,
			"wrapped": fmt.Errorf("x: %w", tt.err),
			"joined":  errors.Join(errors.New("other"), tt.err),
		}
		for form, err := range forms {
			if got := errors.Is(err, dbimp.ErrAuthentication); got != tt.want {
				t.Errorf("%s (%s): errors.Is(err, dbimp.ErrAuthentication) is %t, want %t", tt.name, form, got, tt.want)
			}
		}
	}
}

// TestErrorFields reads the fields of the recorded errors: the code and the text
// of a protobuf error, and the code, the text and the id of the XML error that a
// request with no credentials gets.
func TestErrorFields(t *testing.T) {
	t.Parallel()
	var e *Error
	if err := recordedError(t, 403, "Signature mismatch"); !errors.As(err, &e) || e.Code != "OTSAuthFailed" || e.Message != "Signature mismatch." || e.HTTPStatus != 403 {
		t.Errorf("the wrong secret gave %v", err)
	}
	if err := recordedError(t, 400, "OTSUnsupportOperation"); !errors.As(err, &e) || e.Code != "OTSUnsupportOperation" || e.RequestID == "" || e.Message == "" {
		t.Errorf("the XML error gave %v", err)
	}
	if !strings.HasPrefix(e.Error(), "tablestore: OTSUnsupportOperation: ") {
		t.Errorf("the text of the error is %q", e.Error())
	}
	if (&Error{HTTPStatus: http.StatusBadGateway, Message: "x"}).Error() != "tablestore: 502: x" {
		t.Error("an error with no code names its status")
	}
	var se *dbimp.StatusError
	if !errors.As(recordedError(t, 403, "Signature mismatch"), &se) || se.Code != 403 {
		t.Error("the error does not unwrap to the *dbimp.StatusError of the response")
	}
	// A body that is neither protobuf nor XML, as a proxy sends, keeps the status.
	res := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("bad gateway"))}
	if err := checkStatus(res); !errors.As(err, &e) || e.HTTPStatus != http.StatusBadGateway || e.Message == "" {
		t.Errorf("a plain body gave %v", err)
	}
}
