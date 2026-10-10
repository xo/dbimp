package spanner //nolint:testpackage // The tests read the error types of the package, which build their values in the package.

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xo/dbimp"
)

// TestErrorOfAnAnswer holds the shapes of the error that the server writes
// (docs/SPANNER.md, "Errors"): an object, an array of one object for a stream, a
// page of HTML for a path that the server does not know, and a text.
func TestErrorOfAnAnswer(t *testing.T) {
	t.Parallel()
	notFound := file(t, 138).Response
	if notFound.Status != http.StatusNotFound || !strings.HasPrefix(strings.TrimSpace(notFound.Body), "<") {
		t.Fatalf("the recording has the status %d and %.20q, want an HTML page with HTTP 404", notFound.Status, notFound.Body)
	}
	for name, tt := range map[string]struct {
		status  int
		body    string
		wantMsg string
		code    int
		state   string
	}{
		"an object":          {http.StatusBadRequest, file(t, 100).Response.Body, "Syntax error", 400, "INVALID_ARGUMENT"},
		"an array":           {http.StatusBadRequest, file(t, 106).Response.Body, "division by zero", 400, "OUT_OF_RANGE"},
		"a page of HTML":     {notFound.Status, notFound.Body, "Not Found", 0, ""},
		"a text":             {http.StatusServiceUnavailable, "upstream connect error", "upstream connect error", 0, ""},
		"no body":            {http.StatusTooManyRequests, "", "Too Many Requests", 0, ""},
		"JSON of no error":   {http.StatusInternalServerError, `{"ok":false}`, `{"ok":false}`, 0, ""},
		"an empty array":     {http.StatusBadRequest, `[]`, "[]", 0, ""},
		"a permission error": {http.StatusForbidden, `{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`, "Permission denied", 403, "PERMISSION_DENIED"},
	} {
		f := newFake(t, func(call) (int, string) { return tt.status, tt.body })
		err := failure(t, f.db(), "SELECT 1")
		serr, ok := errors.AsType[*Error](err)
		if !ok {
			t.Errorf("%s: the error is %v, want an *Error", name, err)
			continue
		}
		if serr.HTTPStatus != tt.status || serr.Code != tt.code && tt.code != 0 || serr.Status != tt.state || !strings.Contains(serr.Message, tt.wantMsg) {
			t.Errorf("%s: the error is %+v, want HTTP %d, code %d, status %q and %q", name, *serr, tt.status, tt.code, tt.state, tt.wantMsg)
		}
		var status *dbimp.StatusError
		if !errors.As(err, &status) || status.Code != tt.status {
			t.Errorf("%s: the error does not wrap a *dbimp.StatusError with HTTP %d", name, tt.status)
		}
		if strings.HasPrefix(tt.body, "<") && strings.Contains(serr.Message, "<") {
			t.Errorf("%s: the message holds the page: %q", name, serr.Message)
		}
	}
}

// TestErrorSentinels holds that the status of an error names its sentinel, and
// that no other status does.
func TestErrorSentinels(t *testing.T) {
	t.Parallel()
	aborted := parseError(file(t, 324).Response.Body)
	if !errors.Is(aborted, ErrAborted) || errors.Is(aborted, ErrSessionNotFound) || errors.Is(aborted, ErrCanceled) {
		t.Errorf("the ABORTED error is %+v, want ErrAborted only", aborted)
	}
	if aborted.RetryDelay != 117856360*time.Nanosecond {
		t.Errorf("the delay is %v, want 0.117856360s", aborted.RetryDelay)
	}
	gone := parseError(file(t, 376).Response.Body)
	if !errors.Is(gone, ErrSessionNotFound) || errors.Is(gone, ErrAborted) {
		t.Errorf("the NOT_FOUND error of a session is %+v, want ErrSessionNotFound only", gone)
	}
	// A NOT_FOUND error for another resource is not a lost session.
	table := parseError(file(t, 114).Response.Body)
	if table == nil || table.Status != "NOT_FOUND" || errors.Is(table, ErrSessionNotFound) {
		t.Errorf("the NOT_FOUND error of a table is %+v, want a status NOT_FOUND that is no lost session", table)
	}
	op := newErr(wireError{Code: 1, Message: "Statement was cancelled by user request."})
	if !errors.Is(op, ErrCanceled) || op.Status != "CANCELLED" {
		t.Errorf("the error of a canceled operation is %+v, want ErrCanceled", op)
	}
	for code, name := range map[int]string{5: "NOT_FOUND", 12: "UNIMPLEMENTED", 6: "ALREADY_EXISTS", 3: "INVALID_ARGUMENT"} {
		if got := newErr(wireError{Code: code}).Status; got != name {
			t.Errorf("the status of the gRPC code %d is %q, want %q", code, got, name)
		}
	}
	if got := newErr(wireError{Code: 99}).Status; got != "" {
		t.Errorf("the status of the code 99 is %q, want none", got)
	}
	if got := (&Error{Status: "ABORTED", Message: "m"}).Error(); got != "spanner: ABORTED: m" {
		t.Errorf("Error() is %q", got)
	}
	if got := (&Error{Message: "m"}).Error(); got != "spanner: m" {
		t.Errorf("Error() with no status is %q", got)
	}
}
