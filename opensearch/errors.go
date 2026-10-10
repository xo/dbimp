package opensearch

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that OpenSearch reported. The SQL plugin answers an
// error as a JSON object, {"error": {"reason": ..., "details": ..., "type":
// ...}, "status": ...}, with the content type text/plain, such as HTTP 400
// with SemanticCheckException for an unknown column, HTTP 404 with
// IndexNotFoundException for an unknown index, and HTTP 404 with
// SearchContextMissingException for a cursor that is read already. Some
// errors arrive with HTTP 200, and then only the status of the body says that
// the statement failed, such as UNION with status 500 and a refusal of the
// security plugin in the legacy engine with status 403. A wrong password is
// HTTP 401 with a body of the plain text Unauthorized (measured).
type Error struct {
	// HTTPStatus is the status code of the response, which is 200 for an
	// error that only the body reports.
	HTTPStatus int
	// Status is the status that the body names, and the HTTP status when the
	// body names none.
	Status int
	// Type is the name of the exception of the server, such as
	// SemanticCheckException or NullPointerException, and empty when the
	// answer has none.
	Type string
	// Reason is the reason of the error, or the text of the answer when it
	// has no JSON object.
	Reason string
	// Details is the text that the server gives with the reason, and empty
	// when the answer has none.
	Details string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "opensearch: "
	if err.Type != "" {
		s += err.Type + ": "
	} else {
		s += strconv.Itoa(err.Status) + ": "
	}
	if err.Reason == "" {
		first, _, _ := strings.Cut(err.Details, "\n")
		return s + first
	}
	return s + err.Reason
}

// Unwrap returns the *dbimp.StatusError of the response, and nil for an
// error that came with HTTP 200.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// when the status is 401, whether it is the HTTP status or the status of a body that came with HTTP 200. HTTP 401 is a wrong password (recorded: "a wrong password"). Every other refusal does not match, such as HTTP 403 for a missing
// permission (D197).
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// authRefused reports whether the status of the error is 401.
func (err *Error) authRefused() bool {
	return err.Status == http.StatusUnauthorized
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with what the body says.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	return newError(serr.Code, serr.Body, serr)
}

// newError returns the *Error of an answer with the HTTP status code and the
// body. The body is a JSON object whose error is an object with reason,
// details and type, or a string, as in HTTP 406 and in a path with no handler,
// or it is plain text, as in HTTP 401 (measured). serr is the error of the
// response, or nil for an answer with HTTP 200.
func newError(code int, body string, serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: code, Status: code, status: serr}
	text := strings.TrimSpace(body)
	var obj struct {
		Error  jsontext.Value `json:"error"`
		Status int            `json:"status"`
	}
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		e.Reason = bodyText(code, text)
		return e
	}
	if obj.Status != 0 {
		e.Status = obj.Status
	}
	var detail struct {
		Type    string `json:"type"`
		Reason  string `json:"reason"`
		Details string `json:"details"`
	}
	var plain string
	switch {
	case json.Unmarshal(obj.Error, &detail) == nil && (detail.Type != "" || detail.Reason != "" || detail.Details != ""):
		e.Type, e.Reason, e.Details = detail.Type, detail.Reason, detail.Details
	case json.Unmarshal(obj.Error, &plain) == nil && plain != "":
		e.Reason = plain
	default:
		e.Reason = bodyText(code, text)
	}
	return e
}

// bodyText returns the text of an answer that holds no error object: the
// text itself, or the name of the status when it is empty or a page of HTML.
func bodyText(code int, text string) string {
	if text == "" || strings.HasPrefix(text, "<") {
		return http.StatusText(code)
	}
	return text
}
