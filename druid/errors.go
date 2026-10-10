package druid

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Error is an error that Druid reported. Druid answers an error that comes
// before any row with a status that is not 2xx and a JSON object, such as
// HTTP 400 with the category INVALID_INPUT for a syntax error, HTTP 500 with
// RUNTIME_FAILURE for a division by zero, and HTTP 504 with TIMEOUT for a
// query that passed its timeout (measured). A wrong password is HTTP 401
// with a page of HTML, and a refused privilege is HTTP 403 (measured).
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is errorCode, such as invalidInput or legacyQueryException, and
	// empty when the answer has none.
	Code string
	// Category is category, such as INVALID_INPUT, RUNTIME_FAILURE or
	// TIMEOUT, and empty when the answer has none.
	Category string
	// Persona is persona, USER or OPERATOR, the person who can fix the
	// fault, and empty when the answer has none.
	Persona string
	// Class is errorClass, the class of the exception on the server, such
	// as java.lang.ArithmeticException, and empty when the answer has none.
	Class string
	// Message is errorMessage, or the text of the answer when it has none.
	Message string

	status *dbimp.StatusError
}

// Error satisfies the error interface.
func (err *Error) Error() string {
	s := "druid: "
	if err.Category != "" {
		s += err.Category + ": "
	} else {
		s += strconv.Itoa(err.HTTPStatus) + ": "
	}
	s += err.Message
	if err.Class != "" {
		s += " (" + err.Class + ")"
	}
	return s
}

// Unwrap returns the *dbimp.StatusError of the response.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for HTTP 401, which is the answer to a wrong password. HTTP 403 with
// Access-Check-Result, and the task state FORBIDDEN, are a missing privilege
// and never match (D197). The error of HTTP 401 also matches through its
// *dbimp.StatusError.
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.HTTPStatus == http.StatusUnauthorized
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
// body is a JSON object with errorMessage, an object with
// Access-Check-Result for a refused privilege, or text such as a page of
// HTML (measured).
func newError(serr *dbimp.StatusError) *Error {
	e := &Error{HTTPStatus: serr.Code, status: serr}
	var body struct {
		Error       string `json:"error"`
		Code        string `json:"errorCode"`
		Category    string `json:"category"`
		Persona     string `json:"persona"`
		Class       string `json:"errorClass"`
		Message     string `json:"errorMessage"`
		AccessCheck string `json:"Access-Check-Result"`
	}
	text := strings.TrimSpace(serr.Body)
	if json.Unmarshal([]byte(text), &body) != nil {
		e.Message = http.StatusText(serr.Code)
		if text != "" && !strings.HasPrefix(text, "<") {
			e.Message = text
		}
		return e
	}
	e.Code, e.Category, e.Persona, e.Class = body.Code, body.Category, body.Persona, body.Class
	switch {
	case body.AccessCheck != "":
		e.Message = body.AccessCheck
	case body.Message != "" && body.Message != "null":
		e.Message = body.Message
	case body.Error != "":
		// 37.0.0 writes the errorMessage of a division by zero as "null"
		// (measured).
		e.Message = body.Error
	default:
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
