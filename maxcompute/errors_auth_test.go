package maxcompute //nolint:testpackage // The tests read the errors of the driver, which are not exported.

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/xo/dbimp"
)

// recordedError returns the error that the driver makes of the first recorded
// answer whose status is status and whose body holds text.
func recordedError(tb testing.TB, status int, text string) error {
	tb.Helper()
	for n := 1; n < exchangeCount; n++ {
		ex := exchange(tb, n)
		if ex.Response.Status == status && strings.Contains(string(ex.Response.Content()), text) {
			req, _ := http.NewRequestWithContext(tb.Context(), ex.Request.Method, "http://example.test"+ex.Request.Path, nil)
			err := checkStatus(respond(req, ex), http.StatusOK, http.StatusCreated)
			if err == nil {
				tb.Fatalf("the answer %d is no error", n)
			}
			return err
		}
	}
	tb.Fatalf("no recorded answer with the status %d and %q", status, text)
	return nil
}

// TestClassifiesAuthentication holds each recorded refusal (D197): a wrong secret
// is a refusal of the credential, and nothing else is.
func TestClassifiesAuthentication(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		err    error
		refuse bool
	}{
		{"a wrong secret", recordedError(t, http.StatusForbidden, "SignatureNotMatch"), true},
		{"a request with no credentials", recordedError(t, http.StatusForbidden, "MissingArgument"), false},
		{"a project that does not exist", recordedError(t, http.StatusNotFound, "Project not found"), false},
		{"an instance that does not exist", recordedError(t, http.StatusNotFound, "Instance not found"), false},
		{"a statement that does not parse", recordedError(t, http.StatusBadRequest, "ParseError"), false},
		{"a task that failed", taskError(stateFailed, "ODPS-0130131:[1,15] Table not found"), false},
	} {
		if got := errors.Is(tt.err, dbimp.ErrAuthentication); got != tt.refuse {
			t.Errorf("%s: errors.Is(err, dbimp.ErrAuthentication) is %t, want %t (%v)", tt.name, got, tt.refuse, tt.err)
		}
	}
}

func TestErrorFields(t *testing.T) {
	t.Parallel()
	e, ok := errors.AsType[*Error](recordedError(t, http.StatusForbidden, "SignatureNotMatch"))
	if !ok {
		t.Fatal("the error is no *Error")
	}
	if e.HTTPStatus != http.StatusForbidden || e.Code != "SignatureNotMatch" || !strings.HasPrefix(e.Message, "ODPS-0410042") || e.RequestID == "" {
		t.Errorf("the error is %+v", *e)
	}
	if _, ok := errors.AsType[*dbimp.StatusError](e); !ok {
		t.Error("the error does not unwrap to the *dbimp.StatusError of the response")
	}
	if !strings.HasPrefix(e.Error(), "maxcompute: SignatureNotMatch: ODPS-0410042") {
		t.Errorf("the text is %q", e.Error())
	}
}

func TestACanceledTaskMatchesErrCanceled(t *testing.T) {
	t.Parallel()
	err := taskError(stateCancelled, "ODPS-0424171: the instance was terminated")
	if !errors.Is(err, ErrCanceled) {
		t.Errorf("%v does not match ErrCanceled", err)
	}
	if errors.Is(taskError(stateFailed, "x"), ErrCanceled) {
		t.Error("a failed task matches ErrCanceled")
	}
}
