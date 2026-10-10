package elasticsearch

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that Elasticsearch reported. Elasticsearch answers an
// error with a status that is not 2xx and a JSON object, such as HTTP 400
// with parsing_exception for a syntax error, HTTP 500 with
// arithmetic_exception for a division by zero, and HTTP 404 with
// search_context_missing_exception for a cursor that is gone. A statement
// that passed its request_timeout gives HTTP 504 on 8.19.22 and HTTP 429 on
// 9.4.6 and 9.5.3, so HTTP 429 does not always mean a limit on the rate. A
// wrong password is HTTP 401, and a missing privilege is HTTP 403 with
// security_exception (measured). No error arrived with HTTP 200 (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Type is the type of the error, such as parsing_exception or
	// verification_exception, and empty when the answer has none.
	Type string
	// Reason is the reason of the error, or the text of the answer when it
	// has no JSON object.
	Reason string
	// RootType and RootReason are the type and the reason of the first root
	// cause, and empty when the answer has none. The root cause names the
	// fault itself where the error wraps another, such as
	// search_timeout_exception inside search_phase_execution_exception.
	RootType   string
	RootReason string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "elasticsearch: "
	typ, reason := err.Type, err.Reason
	if err.RootType != "" || err.RootReason != "" {
		typ, reason = err.RootType, err.RootReason
	}
	if typ != "" {
		s += typ + ": "
	} else {
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	return s + reason
}

// Unwrap returns the *dbimp.StatusError of the response.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// when the status is HTTP 401, which is a wrong password with security_exception (recorded: "a wrong password"). Every other refusal does not match, such as HTTP 403 for a missing
// permission (D197).
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.authRefused()
}

// authRefused reports whether the status is HTTP 401.
func (err *Error) authRefused() bool {
	return err.HTTPStatus == http.StatusUnauthorized
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with what the body says.
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	return newError(serr)
}

// newError returns the *Error of a response whose status is not 2xx. The
// body is a JSON object whose error is an object with type, reason and
// root_cause, or a string, as in HTTP 406 and in a path with no handler
// (measured).
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var body struct {
		Error struct {
			Type      string `json:"type"`
			Reason    string `json:"reason"`
			RootCause []struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"root_cause"`
		} `json:"error"`
	}
	text := strings.TrimSpace(serr.Body)
	if json.Unmarshal([]byte(text), &body) != nil {
		e.Reason = bodyText(serr.Code, text)
		var plain struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(text), &plain) == nil && plain.Error != "" {
			e.Reason = plain.Error
		}
		return e
	}
	e.Type, e.Reason = body.Error.Type, body.Error.Reason
	if len(body.Error.RootCause) > 0 {
		e.RootType, e.RootReason = body.Error.RootCause[0].Type, body.Error.RootCause[0].Reason
	}
	if e.Type == "" && e.Reason == "" {
		e.Reason = bodyText(serr.Code, text)
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
