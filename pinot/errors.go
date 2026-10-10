package pinot

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/xo/dbimp"
)

// Exception is one exception of an answer of the Broker, such as 150 for a
// statement that does not parse, or 190 for a table that does not exist
// (measured).
type Exception struct {
	// Code is the code of the error, such as 150.
	Code int `json:"errorCode"`
	// Message is the message of the server.
	Message string `json:"message"`
}

// Error is an error that Pinot reported. A query that fails has HTTP 200,
// and its exceptions in the body (measured). Code and Message are those of
// the first exception, or those of the HTTP status for a response that holds
// none, such as HTTP 401 for a wrong password.
type Error struct {
	// HTTPStatus is the status code of the response.
	HTTPStatus int
	// Code is the code of the first exception, or the HTTP status when the
	// response held none.
	Code int
	// Message is the message of the first exception, or of the response.
	Message string
	// Exceptions holds every exception of the answer, the first one too.
	Exceptions []Exception

	status *dbimp.StatusError
}

// Error satisfies the error interface. It names the first exception, and
// counts the others.
func (err *Error) Error() string {
	s := "pinot: " + strconv.Itoa(err.Code) + ": " + err.Message
	if n := len(err.Exceptions) - 1; n > 0 {
		s += " (and " + strconv.Itoa(n) + " more exceptions)"
	}
	return s
}

// Unwrap returns the *dbimp.StatusError of a response whose status is not
// 2xx, or nil.
func (err *Error) Unwrap() error {
	if err.status == nil {
		return nil
	}
	return err.status
}

// Is reports whether err matches target. It matches dbimp.ErrAuthentication
// for HTTP 401, which is the answer to a wrong password. HTTP 403, "Permission
// denied", is a missing permission and never matches (D197). The error of
// HTTP 401 also matches through its *dbimp.StatusError.
func (err *Error) Is(target error) bool {
	return target == dbimp.ErrAuthentication && err.HTTPStatus == http.StatusUnauthorized
}

// newError returns the *Error of the exceptions of an answer.
func newError(httpStatus int, exceptions []Exception) *Error {
	return &Error{HTTPStatus: httpStatus, Code: exceptions[0].Code, Message: exceptions[0].Message, Exceptions: exceptions}
}

// checkStatus returns nil if the status of res is 2xx. Otherwise it closes
// the body, and returns an *Error with the message of the body, which is
// {"code": 401, "error": "HTTP 401 Unauthorized"} for a wrong password and
// HTTP 403 for a table that the user cannot read, and text for HTTP 500
// (measured).
func checkStatus(res *http.Response) error {
	err := dbimp.CheckStatus(res)
	serr, ok := errors.AsType[*dbimp.StatusError](err)
	if !ok {
		return err
	}
	var body struct {
		Error string `json:"error"`
	}
	e := &Error{HTTPStatus: serr.Code, Code: serr.Code, status: serr, Message: strings.TrimSpace(serr.Body)}
	if json.Unmarshal([]byte(serr.Body), &body) == nil && body.Error != "" {
		e.Message = body.Error
	}
	if e.Message == "" {
		e.Message = http.StatusText(serr.Code)
	}
	return e
}
